package feishu

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// newTestClient 构造把账号服务与开放平台地址全部指向同一测试服务器的客户端，并隔离令牌缓存。
func newTestClient(t *testing.T, serverURL string) *Client {
	t.Helper()
	// client 是测试使用的飞书协议客户端，HTTP 客户端由测试请求直接使用。
	client := NewClient(&http.Client{Timeout: 5 * time.Second})
	client.accountsBaseURL = serverURL
	client.larkAccountsBaseURL = serverURL
	client.openBaseURL = serverURL
	client.larkOpenBaseURL = serverURL
	client.tokens = newTokenCache()
	return client
}

// TestBeginRegistrationAddsSDKQueryAndParsesParams 验证 begin 表单、SDK 查询参数与 addons 压缩协议。
func TestBeginRegistrationAddsSDKQueryAndParsesParams(t *testing.T) {
	// requestedForm 保存 begin 请求解析出的表单参数。
	var requestedForm url.Values
	// requestedContentType 保存 begin 请求的 Content-Type。
	var requestedContentType string
	// server 是模拟飞书设备码 begin 接口的本地服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != registrationPath {
			t.Errorf("begin 请求异常: method=%s path=%s", r.Method, r.URL.Path)
		}
		requestedContentType = r.Header.Get("Content-Type")
		// parseErr 表示 begin 表单无法解析的原因。
		if parseErr := r.ParseForm(); parseErr != nil {
			t.Errorf("解析 begin 表单失败: %v", parseErr)
		}
		requestedForm = r.PostForm
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"device_code":"device-1","verification_uri_complete":"https://accounts.feishu.cn/oauth/authorize?user_code=abc","expires_in":600,"interval":5}`)
	}))
	defer server.Close()
	// client 是绑定测试服务器的飞书客户端。
	client := newTestClient(t, server.URL)
	// code、beginErr 保存 begin 结果及错误。
	code, beginErr := client.BeginRegistration(context.Background())
	if beginErr != nil {
		t.Fatalf("BeginRegistration: %v", beginErr)
	}
	if code.DeviceCode != "device-1" || code.ExpiresIn != 10*time.Minute || code.Interval != 5*time.Second {
		t.Fatalf("设备码参数异常: %+v", code)
	}
	if requestedContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("Content-Type=%q", requestedContentType)
	}
	// expectedForm 保存 begin 阶段必须提交的协议参数。
	expectedForm := map[string]string{"action": "begin", "archetype": "PersonalAgent", "auth_method": "client_secret", "request_user_info": "open_id"}
	// key、want 表示当前待校验的表单字段及期望值。
	for key, want := range expectedForm {
		if requestedForm.Get(key) != want {
			t.Errorf("表单 %s=%q want %q", key, requestedForm.Get(key), want)
		}
	}
	// qrURL、parseErr 保存追加 SDK 参数后的扫码地址及解析错误。
	qrURL, parseErr := url.Parse(code.VerificationURIComplete)
	if parseErr != nil {
		t.Fatalf("解析扫码地址失败: %v", parseErr)
	}
	// query 是扫码地址的查询参数。
	query := qrURL.Query()
	if query.Get("from") != "sdk" || query.Get("source") != "ydisks-xianyu-helper" || query.Get("tp") != "sdk" {
		t.Fatalf("SDK 查询参数缺失: %s", qrURL.RawQuery)
	}
	if query.Get("user_code") != "abc" {
		t.Fatalf("原有查询参数被破坏: %s", qrURL.RawQuery)
	}
	// addons 是扫码地址中的压缩权限声明。
	addons := query.Get("addons")
	if addons == "" {
		t.Fatal("扫码地址缺少 addons")
	}
	// addonsJSON 保存解压后的权限声明字节。
	addonsJSON := decodeAddonsForTest(t, addons)
	// expectedAddons 保存协议要求的权限声明 JSON。
	expectedAddons := `{"scopes":{"tenant":["im:message","im:message.send_as_bot","im:chat","im:resource"]}}`
	if string(addonsJSON) != expectedAddons {
		t.Fatalf("addons=%s want=%s", addonsJSON, expectedAddons)
	}
}

// decodeAddonsForTest 解压并返回扫码 addons 的 JSON 字节。
func decodeAddonsForTest(t *testing.T, addons string) []byte {
	t.Helper()
	// compressed 是 base64url 解码后的 gzip 字节。
	compressed, decodeErr := base64.RawURLEncoding.DecodeString(addons)
	if decodeErr != nil {
		t.Fatalf("addons base64 解码失败: %v", decodeErr)
	}
	// reader、gzipErr 保存 gzip 读取器及构造错误。
	reader, gzipErr := gzip.NewReader(bytes.NewReader(compressed))
	if gzipErr != nil {
		t.Fatalf("addons gzip 打开失败: %v", gzipErr)
	}
	defer reader.Close()
	// payload 是解压后的 JSON 字节。
	payload, readErr := io.ReadAll(reader)
	if readErr != nil {
		t.Fatalf("addons 读取失败: %v", readErr)
	}
	return payload
}

// TestBeginRegistrationRejectsInvalidResponses 验证 begin 出现平台错误、缺字段、非 2xx 和非法 JSON 时都会失败。
func TestBeginRegistrationRejectsInvalidResponses(t *testing.T) {
	// cases 保存 begin 异常响应的测试样例。
	cases := []struct {
		// name 是测试样例名称。
		name string
		// status 是模拟 HTTP 状态码。
		status int
		// body 是模拟响应体。
		body string
	}{
		{name: "平台错误", status: http.StatusOK, body: `{"error":"invalid_request","error_description":"bad request"}`},
		{name: "缺少设备码", status: http.StatusOK, body: `{"verification_uri_complete":"https://example.com/qr"}`},
		{name: "非 2xx", status: http.StatusBadGateway, body: `{"error":"upstream"}`},
		{name: "非法 JSON", status: http.StatusOK, body: `not-json`},
	}
	// testCase 表示当前 begin 异常样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// server 是返回当前异常响应的本地服务。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(testCase.status)
				_, _ = io.WriteString(w, testCase.body)
			}))
			defer server.Close()
			// client 是绑定异常服务的飞书客户端。
			client := newTestClient(t, server.URL)
			// _, beginErr 保存 begin 失败原因。
			if _, beginErr := client.BeginRegistration(context.Background()); beginErr == nil {
				t.Fatal("异常 begin 响应应返回错误")
			}
		})
	}
}

// TestPollMapsPlatformStates 验证 poll 平台状态到协议分类的映射及 Lark 域名切换。
func TestPollMapsPlatformStates(t *testing.T) {
	// cases 保存 poll 响应与期望分类的测试样例。
	cases := []struct {
		// name 是测试样例名称。
		name string
		// body 是模拟 poll 响应体。
		body string
		// wantStatus 是期望的协议状态。
		wantStatus PollStatus
		// wantDomain 是期望的结果域名。
		wantDomain string
		// wantInterval 是 slow_down 期望的轮询间隔。
		wantInterval time.Duration
		// httpStatus 是模拟的真实 HTTP 状态码；0 表示默认 200。
		httpStatus int
	}{
		{name: "等待授权", body: `{"error":"authorization_pending"}`, wantStatus: PollStatusPending, wantDomain: DomainFeishu, httpStatus: http.StatusBadRequest},
		{name: "降低频率", body: `{"error":"slow_down","interval":10}`, wantStatus: PollStatusSlowDown, wantDomain: DomainFeishu, wantInterval: 10 * time.Second, httpStatus: http.StatusBadRequest},
		{name: "用户拒绝", body: `{"error":"access_denied"}`, wantStatus: PollStatusDenied, wantDomain: DomainFeishu, httpStatus: http.StatusBadRequest},
		{name: "设备码过期", body: `{"error":"expired_token"}`, wantStatus: PollStatusExpired, wantDomain: DomainFeishu, httpStatus: http.StatusBadRequest},
		{name: "未知错误", body: `{"error":"invalid_grant"}`, wantStatus: PollStatusError, wantDomain: DomainFeishu, httpStatus: http.StatusBadRequest},
		{name: "飞书成功", body: `{"client_id":"cli-1","client_secret":"secret-1","user_info":{"open_id":"ou-1","tenant_brand":"feishu"}}`, wantStatus: PollStatusSuccess, wantDomain: DomainFeishu},
		{name: "Lark 成功", body: `{"client_id":"cli-2","client_secret":"secret-2","user_info":{"open_id":"ou-2","tenant_brand":"lark"}}`, wantStatus: PollStatusSuccess, wantDomain: DomainLark},
		{name: "成功但缺凭据", body: `{"client_id":"cli-3","user_info":{"open_id":"ou-3"}}`, wantStatus: PollStatusError, wantDomain: DomainFeishu},
	}
	// testCase 表示当前 poll 测试样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// server 是返回当前 poll 响应的本地服务。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != registrationPath {
					t.Errorf("poll 路径=%s", r.URL.Path)
				}
				// parseErr 表示 poll 表单解析失败原因。
				if parseErr := r.ParseForm(); parseErr != nil || r.PostForm.Get("action") != "poll" || r.PostForm.Get("device_code") != "device-1" {
					t.Errorf("poll 表单异常: form=%v err=%v", r.PostForm, parseErr)
				}
				if testCase.httpStatus != 0 {
					w.WriteHeader(testCase.httpStatus)
				}
				_, _ = io.WriteString(w, testCase.body)
			}))
			defer server.Close()
			// client 是绑定 poll 服务的飞书客户端。
			client := newTestClient(t, server.URL)
			// result、pollErr 保存 poll 分类结果及协议错误。
			result, pollErr := client.Poll(context.Background(), PollRequest{Domain: DomainFeishu, DeviceCode: "device-1"})
			if pollErr != nil {
				t.Fatalf("Poll: %v", pollErr)
			}
			if result.Status != testCase.wantStatus || result.Domain != testCase.wantDomain || result.Interval != testCase.wantInterval {
				t.Fatalf("poll 结果=%+v want status=%s domain=%s interval=%s", result, testCase.wantStatus, testCase.wantDomain, testCase.wantInterval)
			}
		})
	}
}

// TestPollSwitchesToLarkAccountsBaseAfterTenantBrand 验证识别到 Lark 租户后下一次轮询改走国际站账号服务。
func TestPollSwitchesToLarkAccountsBaseAfterTenantBrand(t *testing.T) {
	// feishuRequests 统计飞书中国站账号服务收到的轮询次数。
	var feishuRequests int
	// larkRequests 统计 Lark 国际站账号服务收到的轮询次数。
	var larkRequests int
	// feishuServer 模拟飞书中国站设备码 poll。
	feishuServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		feishuRequests++
		_, _ = io.WriteString(w, `{"error":"authorization_pending","user_info":{"tenant_brand":"lark"}}`)
	}))
	defer feishuServer.Close()
	// larkServer 模拟 Lark 国际站设备码 poll。
	larkServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		larkRequests++
		_, _ = io.WriteString(w, `{"error":"authorization_pending"}`)
	}))
	defer larkServer.Close()
	// client 绑定两个不同源站的测试服务器。
	client := newTestClient(t, feishuServer.URL)
	client.larkAccountsBaseURL = larkServer.URL
	// first、firstErr 保存第一次轮询结果。
	first, firstErr := client.Poll(context.Background(), PollRequest{Domain: DomainFeishu, DeviceCode: "device-1"})
	if firstErr != nil || first.Status != PollStatusPending || first.Domain != DomainLark {
		t.Fatalf("首次轮询异常: result=%+v err=%v", first, firstErr)
	}
	// second、secondErr 保存使用返回域名进行的第二次轮询结果。
	second, secondErr := client.Poll(context.Background(), PollRequest{Domain: first.Domain, DeviceCode: "device-1"})
	if secondErr != nil || second.Status != PollStatusPending {
		t.Fatalf("二次轮询异常: result=%+v err=%v", second, secondErr)
	}
	if larkRequests != 1 {
		t.Fatalf("Lark 轮询次数=%d want=1", larkRequests)
	}
	if feishuRequests != 1 {
		t.Fatalf("飞书轮询次数=%d want=1", feishuRequests)
	}
}

// TestBeginRegistrationUsesDefaultTimes 验证 begin 响应缺少有效期和间隔时使用协议包默认值。
func TestBeginRegistrationUsesDefaultTimes(t *testing.T) {
	// server 是只返回设备码与扫码地址的本地服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"device_code":"device-default","verification_uri_complete":"https://accounts.feishu.cn/qr?code=abc"}`)
	}))
	defer server.Close()
	// client 是绑定本地服务的飞书客户端。
	client := newTestClient(t, server.URL)
	// code、beginErr 保存使用默认时间参数的 begin 结果。
	code, beginErr := client.BeginRegistration(context.Background())
	if beginErr != nil {
		t.Fatalf("BeginRegistration: %v", beginErr)
	}
	if code.ExpiresIn != defaultDeviceCodeExpiresIn || code.Interval != defaultDevicePollInterval {
		t.Fatalf("默认时间异常: expires=%s interval=%s", code.ExpiresIn, code.Interval)
	}
}

// TestNewClientUsesDefaultHTTPClient 验证 nil HTTP 客户端会替换为有界超时实现并共享令牌缓存。
func TestNewClientUsesDefaultHTTPClient(t *testing.T) {
	// client 是使用默认 HTTP 客户端构造的协议客户端。
	client := NewClient(nil)
	if client.HTTPClient == nil || client.HTTPClient.Timeout != defaultClientTimeout {
		t.Fatalf("默认 HTTP 客户端异常: %+v", client.HTTPClient)
	}
	if client.tokens == nil {
		t.Fatal("默认客户端必须使用共享令牌缓存")
	}
}

// TestPollRejectsInvalidInputAndTransport 验证空设备码、未知域名和传输错误不会返回可继续状态。
func TestPollRejectsInvalidInputAndTransport(t *testing.T) {
	// client 是请求路径有效但会主动关闭连接的测试客户端。
	client := newTestClient(t, "http://127.0.0.1:1")
	// err 是空设备码输入校验错误，必须阻止平台请求。
	if _, err := client.Poll(context.Background(), PollRequest{Domain: DomainFeishu}); err == nil {
		t.Fatal("空设备码应返回错误")
	}
	// err 是未知域名输入校验错误，必须阻止请求其他站点。
	if _, err := client.Poll(context.Background(), PollRequest{Domain: "unknown", DeviceCode: "device-1"}); err == nil {
		t.Fatal("未知域名应返回错误")
	}
	// err 是传输失败错误，必须向上返回而不能伪装成等待状态。
	if _, err := client.Poll(context.Background(), PollRequest{Domain: DomainFeishu, DeviceCode: "device-1"}); err == nil {
		t.Fatal("不可达服务应返回错误")
	}
}

// TestAddSDKQueryRejectsMalformedURL 验证非法扫码地址不会被静默接受。
func TestAddSDKQueryRejectsMalformedURL(t *testing.T) {
	// cases 保存非法扫码地址样例。
	cases := []string{"", "not-a-url", "https:///missing-host"}
	// raw 表示当前待验证的非法地址。
	for _, raw := range cases {
		// err 是非法扫码地址的解析或参数追加错误。
		if _, err := addSDKQuery(raw); err == nil {
			t.Fatalf("非法地址 %q 应返回错误", raw)
		}
	}
}

// TestSanitizePlatformMessageFallsBack 验证平台错误描述为空时仍返回稳定文本。
func TestSanitizePlatformMessageFallsBack(t *testing.T) {
	// got 是空描述但带错误码时的稳定展示文本。
	if got := sanitizePlatformMessage("", "invalid_request"); got != "invalid_request" {
		t.Fatalf("got=%q", got)
	}
	// got 是描述和错误码都为空时的兜底文本。
	if got := sanitizePlatformMessage("", ""); got != "未知错误" {
		t.Fatalf("got=%q", got)
	}
	// got 是去除首尾空白后的平台描述。
	if got := sanitizePlatformMessage("  描述  ", "code"); got != "描述" {
		t.Fatalf("got=%q", got)
	}
	// trimmed 是去除空白后的稳定错误文本，用于确认非空兜底。
	trimmed := sanitizePlatformMessage("  ", "  ")
	if strings.TrimSpace(trimmed) == "" {
		t.Fatal("错误描述不应为空")
	}
}

// TestEncodeAddonsRoundTrip 验证 addons 编码可被标准 base64url 与 gzip 解压。
func TestEncodeAddonsRoundTrip(t *testing.T) {
	// encoded、encodeErr 保存 addons 编码结果及错误。
	encoded, encodeErr := encodeAddons()
	if encodeErr != nil {
		t.Fatalf("encodeAddons: %v", encodeErr)
	}
	// payload 保存解码后的 addons JSON。
	payload := decodeAddonsForTest(t, encoded)
	// parsed 保存解析后的 addons 结构；scopesOK 表示租户权限列表符合协议。
	var parsed struct {
		// Scopes 是按权限主体分组的 scopes。
		Scopes map[string][]string `json:"scopes"`
	}
	// unmarshalErr 表示 addons JSON 无法解析的原因。
	if unmarshalErr := json.Unmarshal(payload, &parsed); unmarshalErr != nil {
		t.Fatalf("解析 addons 失败: %v", unmarshalErr)
	}
	// scopesOK 表示 addons 中声明的租户权限数量是否符合协议。
	scopesOK := len(parsed.Scopes["tenant"]) == 4
	if !scopesOK {
		t.Fatalf("addons scopes=%v", parsed.Scopes)
	}
}
