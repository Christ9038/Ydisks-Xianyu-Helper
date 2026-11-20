package adapter

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/feishu"
)

// feishuTestTransport 把所有飞书协议请求重定向到本地 httptest 服务器，保留原路径与查询。
type feishuTestTransport struct {
	// target 是本地测试服务器的 host:port。
	target string
}

// RoundTrip 将请求 scheme 和 host 替换为测试服务器，避免访问真实飞书域名。
// req 是待重定向的飞书协议请求；返回值来自默认传输。
func (t feishuTestTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// cloned 是保留原请求头和查询参数的重定向副本。
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = t.target
	cloned.Host = t.target
	return http.DefaultTransport.RoundTrip(cloned)
}

// TestFeishuRegistrarMapsProtocolResults 验证适配器把协议包结果映射为应用层 plain 类型。
func TestFeishuRegistrarMapsProtocolResults(t *testing.T) {
	// server 模拟飞书设备码 begin 与成功 poll。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// parseErr 表示表单解析失败原因；action 是当前注册阶段。
		if parseErr := r.ParseForm(); parseErr != nil {
			t.Errorf("解析表单失败: %v", parseErr)
			return
		}
		// action 是当前设备码注册阶段。
		action := r.PostForm.Get("action")
		switch action {
		case "begin":
			_, _ = io.WriteString(w, `{"device_code":"device-adapter","verification_uri_complete":"https://accounts.feishu.cn/qr?code=abc","expires_in":600,"interval":5}`)
		case "poll":
			_, _ = io.WriteString(w, `{"client_id":"cli-adapter","client_secret":"secret-adapter","user_info":{"open_id":"ou-adapter","tenant_brand":"lark"}}`)
		default:
			http.Error(w, "unexpected action", http.StatusBadRequest)
		}
	}))
	defer server.Close()
	// target 是测试服务器的 host:port。
	target := server.URL[len("http://"):]
	// client 是重定向到本地服务器的协议客户端。
	client := feishu.NewClient(&http.Client{Transport: feishuTestTransport{target: target}})
	// registrar 是待验证的适配器。
	registrar := newFeishuRegistrarWithClient(client)
	// deviceCode、beginErr 保存适配后的 begin 结果。
	deviceCode, beginErr := registrar.BeginRegistration(context.Background())
	if beginErr != nil {
		t.Fatalf("BeginRegistration: %v", beginErr)
	}
	if deviceCode.DeviceCode != "device-adapter" || deviceCode.QRCodeURL == "" || deviceCode.ExpiresIn <= 0 || deviceCode.Interval <= 0 {
		t.Fatalf("适配结果异常: %+v", deviceCode)
	}
	// pollResult、pollErr 保存适配后的成功轮询结果。
	pollResult, pollErr := registrar.PollRegistration(context.Background(), notificationsapp.FeishuPollRequest{Domain: "feishu", DeviceCode: deviceCode.DeviceCode})
	if pollErr != nil {
		t.Fatalf("PollRegistration: %v", pollErr)
	}
	if pollResult.Status != notificationsapp.FeishuPollSuccess || pollResult.Domain != "lark" || pollResult.AppID != "cli-adapter" || pollResult.AppSecret != "secret-adapter" || pollResult.OpenID != "ou-adapter" {
		t.Fatalf("轮询适配结果异常: %+v", pollResult)
	}
}

// TestFeishuRegistrarReturnsProtocolErrorsAndRejectsNilClient 验证适配器透传协议错误并拒绝未初始化客户端。
func TestFeishuRegistrarReturnsProtocolErrorsAndRejectsNilClient(t *testing.T) {
	// server 始终返回服务端错误，模拟协议失败。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
	}))
	defer server.Close()
	// target 是测试服务器的 host:port。
	target := server.URL[len("http://"):]
	// client 是重定向到失败服务的协议客户端。
	client := feishu.NewClient(&http.Client{Transport: feishuTestTransport{target: target}})
	// registrar 是待验证的适配器。
	registrar := newFeishuRegistrarWithClient(client)
	// beginErr 是协议失败时适配器返回的 begin 错误。
	if _, beginErr := registrar.BeginRegistration(context.Background()); beginErr == nil {
		t.Fatal("协议失败应返回 begin 错误")
	}
	// pollErr 是协议失败时适配器返回的 poll 错误。
	if _, pollErr := registrar.PollRegistration(context.Background(), notificationsapp.FeishuPollRequest{Domain: "feishu", DeviceCode: "device"}); pollErr == nil {
		t.Fatal("协议失败应返回 poll 错误")
	}
	// nilRegistrar 是没有协议客户端的适配器。
	nilRegistrar := FeishuRegistrar{}
	// beginErr 是 nil 客户端返回的 begin 错误。
	if _, beginErr := nilRegistrar.BeginRegistration(context.Background()); beginErr == nil {
		t.Fatal("nil 客户端应返回 begin 错误")
	}
	// pollErr 是 nil 客户端返回的 poll 错误。
	if _, pollErr := nilRegistrar.PollRegistration(context.Background(), notificationsapp.FeishuPollRequest{}); pollErr == nil {
		t.Fatal("nil 客户端应返回 poll 错误")
	}
	// productionRegistrar 是生产构造结果，必须非空且可赋值给应用端口。
	productionRegistrar := NewFeishuRegistrar()
	if productionRegistrar == nil {
		t.Fatal("生产飞书注册适配器不能为空")
	}
	// interfaceAssertion 验证生产适配器满足消费者接口。
	var interfaceAssertion = productionRegistrar
	if interfaceAssertion == nil {
		t.Fatal("生产适配器未满足应用端口")
	}
}
