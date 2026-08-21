package netguard

import (
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIsPublicIP 封装TestIsPublicIP业务协调。
func TestIsPublicIP(t *testing.T) {
	// raw 表示当前遍历过程中的原始
	for _, raw := range []string{
		"127.0.0.1", "10.0.0.1", "172.16.1.1", "192.168.1.1", "169.254.169.254", "::1",
		"100.64.0.1", "198.18.0.1", "192.0.2.1", "198.51.100.1", "203.0.113.1", "2001:db8::1",
	} {
		if IsPublicIP(net.ParseIP(raw)) {
			t.Fatalf("%s must be rejected", raw)
		}
	}
	if !IsPublicIP(net.ParseIP("8.8.8.8")) {
		t.Fatal("public IP should be allowed")
	}
}

// TestPublicHTTPClientRejectsLoopback 封装TestPublicHTTPClientRejectsLoopback业务协调。
func TestPublicHTTPClientRejectsLoopback(t *testing.T) {
	// client 用于本次流程后续判断的client
	client := PublicHTTPClient(0)
	// req 用于本次流程后续判断的req
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:1", nil)
	if // err 用于本次流程后续判断的err
	_, err := client.Do(req); err == nil {
		t.Fatal("loopback request must be rejected")
	}
}

// TestTrustedEndpointHTTPClientAllowsLoopbackAndUnspecifiedAddress 封装TestTrustedEndpointHTTPClientAllowsLoopbackAndUnspecifiedAddress业务协调。
func TestTrustedEndpointHTTPClientAllowsLoopbackAndUnspecifiedAddress(t *testing.T) {
	// server 用于本次流程后续判断的server
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	// port、err 用于本次流程后续判断的port、err
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	// host 表示当前遍历过程中的host
	for _, host := range []string{"127.0.0.1", "0.0.0.0"} {
		// baseURL 用于本次流程后续判断的baseURL
		baseURL := "http://" + net.JoinHostPort(host, port)
		// client、clientErr 用于本次流程后续判断的client、clientErr
		client, clientErr := TrustedEndpointHTTPClient(baseURL+"/v1", 0)
		if clientErr != nil {
			t.Fatal(clientErr)
		}
		// resp、requestErr 用于本次流程后续判断的resp、requestErr
		resp, requestErr := client.Get(baseURL + "/v1/models")
		if requestErr != nil {
			t.Fatalf("trusted endpoint should reach %s: %v", host, requestErr)
		}
		resp.Body.Close()
		if resp.StatusCode != http.StatusNoContent {
			t.Fatalf("unexpected status from %s: %d", host, resp.StatusCode)
		}
	}
}

// TestTrustedEndpointHTTPClientDoesNotApplyAddressPolicy 封装TestTrustedEndpointHTTPClientDoesNotApplyAddressPolicy业务协调。
func TestTrustedEndpointHTTPClientDoesNotApplyAddressPolicy(t *testing.T) {
	// raw 表示当前遍历过程中的原始
	for _, raw := range []string{
		"http://0.0.0.0:8080/v1", "http://127.0.0.1:8080/v1", "http://169.254.169.254/v1",
		"http://192.168.0.220/v1", "http://[::1]:8080/v1", "https://user:pass@ai.internal/v1",
	} {
		// client、err 用于本次流程后续判断的client、err
		client, err := TrustedEndpointHTTPClient(raw, 0)
		if err != nil {
			t.Fatalf("admin-configured address should be accepted (%s): %v", raw, err)
		}
		if client.CheckRedirect != nil {
			t.Fatalf("admin-configured client should use standard redirect behavior: %s", raw)
		}
	}
}

// TestTrustedEndpointHTTPClientValidatesBaseURL 封装TestTrustedEndpointHTTPClientValidatesBaseURL业务协调。
func TestTrustedEndpointHTTPClientValidatesBaseURL(t *testing.T) {
	// raw 表示当前遍历过程中的原始
	for _, raw := range []string{"", "file:///tmp/model", "://bad"} {
		if // err 用于本次流程后续判断的err
		_, err := TrustedEndpointHTTPClient(raw, 0); err == nil {
			t.Fatalf("invalid base URL should fail: %q", raw)
		}
	}
}

// TestConfiguredHTTPClientSwitchesRuntimePolicy 验证用户配置 HTTP 客户端可在运行时即时切换公网限制。
func TestConfiguredHTTPClientSwitchesRuntimePolicy(t *testing.T) {
	// server 是本地测试端点，用于代表默认关闭时允许的内网地址。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)
	// policy 是本次测试独立持有的运行时策略。
	policy := NewOutboundPolicy(false)
	// allowedClient 是关闭公网限制时创建的策略客户端。
	allowedClient := PolicyHTTPClient(policy, 0)
	// allowedResponse、allowedErr 保存关闭公网限制时的本地请求结果。
	allowedResponse, allowedErr := allowedClient.Get(server.URL)
	if allowedErr != nil {
		t.Fatalf("关闭公网限制时本地端点应可访问: %v", allowedErr)
	}
	allowedResponse.Body.Close()
	policy.SetPublicOnly(true)
	// blockedResponse、blockedErr 保存同一 HTTP 客户端在开启公网限制后的本地请求结果。
	blockedResponse, blockedErr := allowedClient.Get(server.URL)
	if blockedResponse != nil {
		blockedResponse.Body.Close()
	}
	if blockedErr == nil {
		t.Fatal("打开公网限制后回环端点必须被拒绝")
	}
}

// TestConfiguredHTTPClientRejectsCrossHostRedirect 验证用户配置端点不能借助跳转切换到另一主机。
func TestConfiguredHTTPClientRejectsCrossHostRedirect(t *testing.T) {
	// target 是不应被跨主机跳转访问的第二个本地端点。
	target := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		writer.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(target.Close)
	// redirector 是返回跨主机跳转响应的第一个本地端点。
	redirector := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		http.Redirect(writer, request, target.URL, http.StatusFound)
	}))
	t.Cleanup(redirector.Close)
	// client 是使用独立运行时策略的用户配置客户端。
	client := PolicyHTTPClient(NewOutboundPolicy(false), 0)
	// response、err 保存跳转请求的响应和错误。
	response, err := client.Get(redirector.URL)
	if response != nil {
		response.Body.Close()
	}
	// errText 保存跳转错误的非敏感文本，用于稳定断言拒绝原因。
	if err == nil || !strings.Contains(err.Error(), "不允许跨主机重定向") {
		t.Fatalf("跨主机跳转应被拒绝 response=%v err=%v", response, err)
	}
}

// TestConfiguredHTTPClientLimitsResponseBody 验证统一用户配置客户端拒绝超出响应体上限的内容。
func TestConfiguredHTTPClientLimitsResponseBody(t *testing.T) {
	// server 是返回刚好超过统一响应上限的本地端点。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write(make([]byte, ConfiguredResponseBodyLimit+1))
	}))
	t.Cleanup(server.Close)
	// client 是使用关闭公网限制的用户配置客户端。
	client := PolicyHTTPClient(NewOutboundPolicy(false), 0)
	// response、err 保存读取超大响应的结果。
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	// err 表示读取超大响应时返回的大小限制错误。
	if _, err := io.ReadAll(response.Body); err == nil {
		t.Fatal("超大响应应返回大小限制错误")
	}
}
