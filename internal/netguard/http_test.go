package netguard

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestIsPublicIP 负责TestIsPublicIP相关处理。
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

// TestPublicHTTPClientRejectsLoopback 负责TestPublicHTTPClientRejectsLoopback相关处理。
func TestPublicHTTPClientRejectsLoopback(t *testing.T) {
	// client 保存client，供当前处理流程使用
	client := PublicHTTPClient(0)
	// req 保存req，供当前处理流程使用
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:1", nil)
	if // err 保存err，供当前处理流程使用
	_, err := client.Do(req); err == nil {
		t.Fatal("loopback request must be rejected")
	}
}

// TestTrustedEndpointHTTPClientAllowsLoopbackAndUnspecifiedAddress 负责TestTrustedEndpointHTTPClientAllowsLoopbackAndUnspecifiedAddress相关处理。
func TestTrustedEndpointHTTPClientAllowsLoopbackAndUnspecifiedAddress(t *testing.T) {
	// server 保存server，供当前处理流程使用
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	// port、err 保存port、err，供当前处理流程使用
	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	// host 表示当前遍历过程中的host
	for _, host := range []string{"127.0.0.1", "0.0.0.0"} {
		// baseURL 保存baseURL，供当前处理流程使用
		baseURL := "http://" + net.JoinHostPort(host, port)
		// client、clientErr 保存client、clientErr，供当前处理流程使用
		client, clientErr := TrustedEndpointHTTPClient(baseURL+"/v1", 0)
		if clientErr != nil {
			t.Fatal(clientErr)
		}
		// resp、requestErr 保存resp、requestErr，供当前处理流程使用
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

// TestTrustedEndpointHTTPClientDoesNotApplyAddressPolicy 负责TestTrustedEndpointHTTPClientDoesNotApplyAddressPolicy相关处理。
func TestTrustedEndpointHTTPClientDoesNotApplyAddressPolicy(t *testing.T) {
	// raw 表示当前遍历过程中的原始
	for _, raw := range []string{
		"http://0.0.0.0:8080/v1", "http://127.0.0.1:8080/v1", "http://169.254.169.254/v1",
		"http://192.168.0.220/v1", "http://[::1]:8080/v1", "https://user:pass@ai.internal/v1",
	} {
		// client、err 保存client、err，供当前处理流程使用
		client, err := TrustedEndpointHTTPClient(raw, 0)
		if err != nil {
			t.Fatalf("admin-configured address should be accepted (%s): %v", raw, err)
		}
		if client.CheckRedirect != nil {
			t.Fatalf("admin-configured client should use standard redirect behavior: %s", raw)
		}
	}
}

// TestTrustedEndpointHTTPClientValidatesBaseURL 负责TestTrustedEndpointHTTPClientValidatesBaseURL相关处理。
func TestTrustedEndpointHTTPClientValidatesBaseURL(t *testing.T) {
	// raw 表示当前遍历过程中的原始
	for _, raw := range []string{"", "file:///tmp/model", "://bad"} {
		if // err 保存err，供当前处理流程使用
		_, err := TrustedEndpointHTTPClient(raw, 0); err == nil {
			t.Fatalf("invalid base URL should fail: %q", raw)
		}
	}
}
