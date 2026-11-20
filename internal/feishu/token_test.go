package feishu

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// TestSendTextCachesTenantToken 验证同一应用连续发送时只获取一次租户令牌，并使用协议要求的消息字段。
func TestSendTextCachesTenantToken(t *testing.T) {
	// tokenRequests 统计租户令牌请求次数。
	var tokenRequests int
	// messageRequests 统计消息发送请求次数。
	var messageRequests int
	// messageBody 保存最近一次消息请求体。
	var messageBody map[string]string
	// authorization 保存最近一次消息请求的 Authorization 头。
	var authorization string
	// server 是同时模拟令牌和消息接口的本地服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case tenantTokenPath:
			tokenRequests++
			w.Header().Set("Content-Type", "application/json")
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"token-1","expire":7200}`)
		case sendMessagePath:
			messageRequests++
			if r.URL.Query().Get("receive_id_type") != "open_id" {
				t.Errorf("receive_id_type=%q", r.URL.Query().Get("receive_id_type"))
			}
			authorization = r.Header.Get("Authorization")
			// decodeErr 表示消息请求体无法解析的原因。
			if decodeErr := json.NewDecoder(r.Body).Decode(&messageBody); decodeErr != nil {
				t.Errorf("解析消息请求体失败: %v", decodeErr)
			}
			_, _ = io.WriteString(w, `{"code":0}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	// client 绑定本地令牌与消息服务，并隔离令牌缓存。
	client := newTestClient(t, server.URL)
	// sendErr 保存第一次发送结果。
	if sendErr := client.SendText(context.Background(), "cli-1", "secret-1", "ou-1", DomainFeishu, "通知正文"); sendErr != nil {
		t.Fatalf("首次发送失败: %v", sendErr)
	}
	// secondErr 保存第二次发送结果。
	if secondErr := client.SendText(context.Background(), "cli-1", "secret-1", "ou-1", DomainFeishu, "第二条"); secondErr != nil {
		t.Fatalf("第二次发送失败: %v", secondErr)
	}
	if tokenRequests != 1 {
		t.Fatalf("令牌请求次数=%d want=1", tokenRequests)
	}
	if messageRequests != 2 {
		t.Fatalf("消息请求次数=%d want=2", messageRequests)
	}
	if authorization != "Bearer token-1" {
		t.Fatalf("Authorization=%q", authorization)
	}
	if messageBody["receive_id"] != "ou-1" || messageBody["msg_type"] != "text" {
		t.Fatalf("消息字段异常: %+v", messageBody)
	}
	// content 保存二次编码的消息内容字符串。
	content := messageBody["content"]
	if !strings.Contains(content, `\"text\":\"第二条\"`) && !strings.Contains(content, `"text":"第二条"`) {
		t.Fatalf("消息 content=%q", content)
	}
}

// TestTenantTokenRefetchesAfterCacheExpiry 验证缓存过期后会重新向平台获取令牌。
func TestTenantTokenRefetchesAfterCacheExpiry(t *testing.T) {
	// tokenRequests 统计租户令牌请求次数。
	var tokenRequests int
	// server 是只模拟租户令牌接口的本地服务。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		tokenRequests++
		_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"token-exp","expire":7200}`)
	}))
	defer server.Close()
	// client 绑定本地令牌服务。
	client := newTestClient(t, server.URL)
	// key 是当前应用对应的缓存键。
	key := tenantTokenCacheKey(DomainFeishu, "cli-exp", "secret-exp")
	client.tokens.set(key, "stale", time.Now().Add(-time.Minute))
	// token、tokenErr 保存重新获取的令牌及错误。
	token, tokenErr := client.TenantAccessToken(context.Background(), "cli-exp", "secret-exp", DomainFeishu)
	if tokenErr != nil || token != "token-exp" {
		t.Fatalf("token=%q err=%v", token, tokenErr)
	}
	if tokenRequests != 1 {
		t.Fatalf("令牌请求次数=%d want=1", tokenRequests)
	}
}

// TestTenantTokenAndMessageErrors 验证令牌与消息业务码非零、HTTP 错误和非法 JSON 都会返回失败。
func TestTenantTokenAndMessageErrors(t *testing.T) {
	// cases 保存令牌或消息接口的异常响应样例。
	cases := []struct {
		// name 是测试样例名称。
		name string
		// path 是返回异常响应的方法路径。
		path string
		// status 是模拟 HTTP 状态码。
		status int
		// body 是模拟响应体。
		body string
		// send 表示是否直接验证消息发送而不是令牌获取。
		send bool
	}{
		{name: "令牌业务码", path: tenantTokenPath, status: http.StatusOK, body: `{"code":99991663,"msg":"invalid app"}`},
		{name: "令牌非 2xx", path: tenantTokenPath, status: http.StatusUnauthorized, body: `{}`},
		{name: "令牌缺令牌值", path: tenantTokenPath, status: http.StatusOK, body: `{"code":0}`},
		{name: "消息业务码", path: sendMessagePath, status: http.StatusOK, body: `{"code":230001,"msg":"invalid receive_id"}`, send: true},
		{name: "消息非 2xx", path: sendMessagePath, status: http.StatusBadRequest, body: `{}`, send: true},
		{name: "消息非法 JSON", path: sendMessagePath, status: http.StatusOK, body: `not-json`, send: true},
	}
	// testCase 表示当前异常样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// server 是返回当前异常响应的本地服务；令牌接口在消息场景中正常工作。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path == testCase.path {
					w.WriteHeader(testCase.status)
					_, _ = io.WriteString(w, testCase.body)
					return
				}
				_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"token-ok","expire":7200}`)
			}))
			defer server.Close()
			// client 绑定异常服务。
			client := newTestClient(t, server.URL)
			// operationErr 保存当前样例触发的结果错误。
			var operationErr error
			if testCase.send {
				operationErr = client.SendText(context.Background(), "cli-err", "secret-err", "ou-err", DomainFeishu, "正文")
			} else {
				_, operationErr = client.TenantAccessToken(context.Background(), "cli-err", "secret-err", DomainFeishu)
			}
			if operationErr == nil {
				t.Fatal("异常响应应返回错误")
			}
			if strings.Contains(operationErr.Error(), "secret-err") {
				t.Fatalf("错误泄漏应用密钥: %v", operationErr)
			}
		})
	}
}

// TestSendTextRejectsInvalidInput 验证缺凭据、缺 open_id、空正文和未知域名都会在请求前失败。
func TestSendTextRejectsInvalidInput(t *testing.T) {
	// client 是使用不可达地址的协议客户端；输入校验失败时不应发起请求。
	client := newTestClient(t, "http://127.0.0.1:1")
	// cases 保存输入校验失败的测试样例。
	cases := []struct {
		// name 是测试样例名称。
		name string
		// appID 是应用标识。
		appID string
		// appSecret 是应用密钥。
		appSecret string
		// openID 是接收者 open_id。
		openID string
		// domain 是站点域名。
		domain string
		// text 是消息正文。
		text string
	}{
		{name: "缺凭据", appID: "", appSecret: "", openID: "ou-1", domain: DomainFeishu, text: "正文"},
		{name: "缺 open_id", appID: "cli-1", appSecret: "secret-1", openID: " ", domain: DomainFeishu, text: "正文"},
		{name: "空正文", appID: "cli-1", appSecret: "secret-1", openID: "ou-1", domain: DomainFeishu, text: " "},
		{name: "未知域名", appID: "cli-1", appSecret: "secret-1", openID: "ou-1", domain: "unknown", text: "正文"},
	}
	// testCase 表示当前输入校验样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// sendErr 保存当前样例的输入校验错误。
			if sendErr := client.SendText(context.Background(), testCase.appID, testCase.appSecret, testCase.openID, testCase.domain, testCase.text); sendErr == nil {
				t.Fatal("非法输入应返回错误")
			}
		})
	}
}

// TestTokenCacheConcurrentAccess 验证令牌缓存并发读写不会竞态或破坏条目。
func TestTokenCacheConcurrentAccess(t *testing.T) {
	// cache 是测试专用的空令牌缓存。
	cache := newTokenCache()
	// workers 是并发读写令牌缓存的 goroutine 数量。
	const workers = 8
	// waitGroup 等待全部并发读写结束。
	var waitGroup sync.WaitGroup
	// workerIndex 表示当前启动的并发 worker 序号。
	for workerIndex := 0; workerIndex < workers; workerIndex++ {
		waitGroup.Add(1)
		go func(index int) {
			defer waitGroup.Done()
			// key 是当前 worker 使用的缓存键。
			key := "key-" + time.Duration(index).String()
			cache.set(key, "token", time.Now().Add(time.Hour))
			// token、ok 保存并发读取到的缓存令牌与命中状态。
			if token, ok := cache.get(key, time.Now()); !ok || token != "token" {
				t.Errorf("缓存读取异常: token=%q ok=%v", token, ok)
			}
		}(workerIndex)
	}
	waitGroup.Wait()
}

// localFeishuTransport 把协议包硬编码的飞书源站重定向到本地 httptest 服务器。
type localFeishuTransport struct {
	// target 是本地测试服务器的 host:port。
	target string
}

// RoundTrip 保留请求路径与查询参数，仅替换 scheme 和 host，避免访问真实飞书域名。
// req 是待重定向的飞书协议请求；返回值来自默认传输。
func (t localFeishuTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// cloned 是绑定测试服务器地址的请求副本。
	cloned := req.Clone(req.Context())
	cloned.URL.Scheme = "http"
	cloned.URL.Host = t.target
	cloned.Host = t.target
	return http.DefaultTransport.RoundTrip(cloned)
}

// TestPackageSendTextUsesSharedCacheAndLarkDomain 验证包级发送入口使用共享缓存并支持 Lark 开放平台域名。
func TestPackageSendTextUsesSharedCacheAndLarkDomain(t *testing.T) {
	// tokenRequests 统计租户令牌请求次数。
	var tokenRequests int
	// messageRequests 统计消息发送请求次数。
	var messageRequests int
	// server 同时模拟 Lark 租户令牌与消息接口。
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case tenantTokenPath:
			tokenRequests++
			_, _ = io.WriteString(w, `{"code":0,"tenant_access_token":"pkg-token","expire":7200}`)
		case sendMessagePath:
			messageRequests++
			_, _ = io.WriteString(w, `{"code":0}`)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	// httpClient 是把协议包默认源站重定向到本地服务器的客户端。
	httpClient := &http.Client{Transport: localFeishuTransport{target: server.Listener.Addr().String()}}
	// firstErr 是第一次包级发送结果。
	firstErr := SendText(context.Background(), httpClient, "cli-pkg-lark", "secret-pkg-lark", "ou-pkg-lark", DomainLark, "第一条")
	if firstErr != nil {
		t.Fatalf("首次包级发送失败: %v", firstErr)
	}
	// secondErr 是第二次包级发送结果；共享缓存应复用令牌。
	secondErr := SendText(context.Background(), httpClient, "cli-pkg-lark", "secret-pkg-lark", "ou-pkg-lark", DomainLark, "第二条")
	if secondErr != nil {
		t.Fatalf("第二次包级发送失败: %v", secondErr)
	}
	if tokenRequests != 1 || messageRequests != 2 {
		t.Fatalf("包级发送请求异常: token=%d message=%d", tokenRequests, messageRequests)
	}
}

// TestTenantTokenCacheHonorsPlatformExpiry 验证平台有效期和安全窗口决定令牌缓存截止时间。
func TestTenantTokenCacheHonorsPlatformExpiry(t *testing.T) {
	// cases 保存平台有效期与期望缓存时长的测试样例。
	cases := []struct {
		// name 是测试样例名称。
		name string
		// expire 是平台返回的有效秒数。
		expire int
		// minRemaining 是缓存剩余时长下界。
		minRemaining time.Duration
		// maxRemaining 是缓存剩余时长上界；零值表示不设置上界。
		maxRemaining time.Duration
	}{
		{name: "默认有效期", expire: 0, minRemaining: time.Hour},
		{name: "短有效期减半", expire: 60, minRemaining: 20 * time.Second, maxRemaining: time.Minute},
	}
	// testCase 表示当前有效期样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// tokenRequests 统计当前样例的令牌请求次数。
			var tokenRequests int
			// server 返回当前样例的令牌有效期。
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				tokenRequests++
				_, _ = io.WriteString(w, fmt.Sprintf(`{"code":0,"tenant_access_token":"ttl-token","expire":%d}`, testCase.expire))
			}))
			defer server.Close()
			// client 绑定当前样例服务。
			client := newTestClient(t, server.URL)
			// _, tokenErr 保存令牌获取结果。
			_, tokenErr := client.TenantAccessToken(context.Background(), "cli-ttl-"+testCase.name, "secret-ttl", DomainFeishu)
			if tokenErr != nil {
				t.Fatalf("TenantAccessToken: %v", tokenErr)
			}
			// key 是当前样例的缓存键。
			key := tenantTokenCacheKey(DomainFeishu, "cli-ttl-"+testCase.name, "secret-ttl")
			client.tokens.mu.Lock()
			// entry 是当前样例写入的缓存条目。
			entry := client.tokens.entries[key]
			client.tokens.mu.Unlock()
			// remaining 是缓存条目剩余有效时长。
			remaining := time.Until(entry.ExpiresAt)
			if remaining < testCase.minRemaining {
				t.Fatalf("缓存剩余时长=%s 低于下界 %s", remaining, testCase.minRemaining)
			}
			if testCase.maxRemaining > 0 && remaining > testCase.maxRemaining {
				t.Fatalf("缓存剩余时长=%s 超过上界 %s", remaining, testCase.maxRemaining)
			}
		})
	}
}

// TestTenantTokenCacheKeyDoesNotContainSecret 验证缓存键不包含密钥明文，密钥变化会产生不同键。
func TestTenantTokenCacheKeyDoesNotContainSecret(t *testing.T) {
	// firstKey 是第一个密钥对应的缓存键。
	firstKey := tenantTokenCacheKey(DomainFeishu, "cli-1", "secret-a")
	// secondKey 是另一个密钥对应的缓存键。
	secondKey := tenantTokenCacheKey(DomainFeishu, "cli-1", "secret-b")
	if strings.Contains(firstKey, "secret-a") || strings.Contains(secondKey, "secret-b") {
		t.Fatal("缓存键不应包含应用密钥明文")
	}
	if firstKey == secondKey {
		t.Fatal("不同密钥应生成不同缓存键")
	}
}
