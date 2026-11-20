package notify

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// feishuAppTestChannel 构造配置完整的飞书应用通知渠道，敏感字段只用于本地测试断言。
func feishuAppTestChannel(appID, appSecret, openID, domain string) db.NotificationChannel {
	// configFields 是飞书应用渠道配置字段。
	configFields := map[string]string{"app_id": appID, "app_secret": appSecret, "open_id": openID, "domain": domain}
	// configJSON、marshalErr 保存序列化后的渠道配置及错误。
	configJSON, marshalErr := json.Marshal(configFields)
	if marshalErr != nil {
		panic(marshalErr)
	}
	return db.NotificationChannel{ID: 1, Name: "飞书应用", Type: "feishu_app", Config: string(configJSON)}
}

// TestSendFeishuAppChannelUsesTenantTokenAndTextMessage 验证 feishu_app 分派获取租户令牌并发送文本消息。
func TestSendFeishuAppChannelUsesTenantTokenAndTextMessage(t *testing.T) {
	// tokenRequests 统计租户令牌请求次数。
	var tokenRequests int
	// messageRequest 保存最近一次消息请求体。
	var messageRequest map[string]string
	// messageAuth 保存最近一次消息请求的 Authorization 头。
	var messageAuth string
	// server 模拟飞书租户令牌与消息接口。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/open-apis/auth/v3/tenant_access_token/internal":
			tokenRequests++
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"notify-token","expire":7200}`)
		case "/open-apis/im/v1/messages":
			if r.URL.Query().Get("receive_id_type") != "open_id" {
				t.Errorf("receive_id_type=%q", r.URL.Query().Get("receive_id_type"))
			}
			messageAuth = r.Header.Get("Authorization")
			// decodeErr 是消息请求体解析失败原因；nil 表示请求结构符合协议。
			if decodeErr := json.NewDecoder(r.Body).Decode(&messageRequest); decodeErr != nil {
				t.Errorf("解析消息请求失败: %v", decodeErr)
			}
			_, _ = io.WriteString(w, `{"code":0}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	// notifier 是把飞书硬编码源站重定向到本地服务器的通知器。
	notifier := notifierWithRedirectClient(server.Listener.Addr().String())
	// channel 是配置完整的飞书应用渠道。
	channel := feishuAppTestChannel("cli-notify-app", "secret-notify-app", "ou-notify-app", "lark")
	// sendErr 保存渠道发送结果。
	if sendErr := notifier.send(channel, "飞书应用通知正文"); sendErr != nil {
		t.Fatalf("发送失败: %v", sendErr)
	}
	if tokenRequests != 1 {
		t.Fatalf("令牌请求次数=%d want=1", tokenRequests)
	}
	if messageAuth != "Bearer notify-token" {
		t.Fatalf("Authorization=%q", messageAuth)
	}
	if messageRequest["receive_id"] != "ou-notify-app" || messageRequest["msg_type"] != "text" {
		t.Fatalf("消息字段异常: %+v", messageRequest)
	}
	// content 是飞书消息内容字符串，必须包含转义后的正文。
	content := messageRequest["content"]
	if !strings.Contains(content, "飞书应用通知正文") {
		t.Fatalf("消息正文=%q", content)
	}
}

// TestSendFeishuAppChannelRejectsIncompleteConfig 验证缺少凭据或 open_id 时不发起平台请求。
func TestSendFeishuAppChannelRejectsIncompleteConfig(t *testing.T) {
	// cases 保存配置不完整的渠道。
	cases := []db.NotificationChannel{
		feishuAppTestChannel("", "secret", "ou", "feishu"),
		feishuAppTestChannel("cli", "", "ou", "feishu"),
		feishuAppTestChannel("cli", "secret", "", "feishu"),
	}
	// channel 表示当前配置不完整的渠道。
	for _, channel := range cases {
		// notifier 是禁止访问网络的测试通知器。
		notifier := notifierWithRedirectClient("http://127.0.0.1:1")
		// sendErr 是配置不完整渠道的发送错误，必须阻止请求平台。
		if sendErr := notifier.send(channel, "正文"); sendErr == nil {
			t.Fatalf("不完整配置应失败: %+v", channel)
		}
	}
}

// TestSendFeishuAppChannelMapsPlatformErrors 验证业务码和 HTTP 错误转换为脱敏发送错误。
func TestSendFeishuAppChannelMapsPlatformErrors(t *testing.T) {
	// cases 保存令牌或消息接口异常响应样例。
	cases := []struct {
		// name 是测试样例名称。
		name string
		// path 是返回异常响应的方法路径。
		path string
		// status 是模拟 HTTP 状态码。
		status int
		// body 是模拟响应体。
		body string
	}{
		{name: "令牌业务错误", path: "/open-apis/auth/v3/tenant_access_token/internal", status: http.StatusOK, body: `{"code":99991663,"msg":"invalid app"}`},
		{name: "消息业务错误", path: "/open-apis/im/v1/messages", status: http.StatusOK, body: `{"code":230001,"msg":"invalid receive id"}`},
		{name: "消息 HTTP 错误", path: "/open-apis/im/v1/messages", status: http.StatusBadRequest, body: `{}`},
	}
	// testCase 表示当前异常样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// server 返回当前异常响应；非目标路径的正常令牌接口保证错误来自目标接口。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == testCase.path {
					w.WriteHeader(testCase.status)
					_, _ = io.WriteString(w, testCase.body)
					return
				}
				_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"notify-error-token","expire":7200}`)
			}))
			defer server.Close()
			// notifier 是把飞书请求重定向到异常服务器的通知器。
			notifier := notifierWithRedirectClient(server.Listener.Addr().String())
			// channel 是配置完整的飞书应用渠道。
			channel := feishuAppTestChannel("cli-notify-error-"+testCase.name, "secret-notify-error", "ou-notify-error", "feishu")
			// sendErr 保存异常发送结果。
			sendErr := notifier.send(channel, "正文")
			if sendErr == nil {
				t.Fatal("异常响应应返回发送错误")
			}
			if strings.Contains(sendErr.Error(), "secret-notify-error") {
				t.Fatalf("发送错误泄漏应用密钥: %v", sendErr)
			}
			// expectedCode 是异常响应中应出现在错误文本里的业务码。
			expectedCode := ""
			if testCase.name == "令牌业务错误" {
				expectedCode = "99991663"
			}
			if testCase.name == "消息业务错误" {
				expectedCode = "230001"
			}
			if expectedCode != "" && !strings.Contains(sendErr.Error(), expectedCode) {
				t.Fatalf("错误缺少业务码 %s: %v", expectedCode, sendErr)
			}
		})
	}
}
