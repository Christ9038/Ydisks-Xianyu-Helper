package netguard

import (
	"context"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestIsPublicIP(t *testing.T) {
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

func TestPublicHTTPClientRejectsLoopback(t *testing.T) {
	client := PublicHTTPClient(0)
	req, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://127.0.0.1:1", nil)
	if _, err := client.Do(req); err == nil {
		t.Fatal("loopback request must be rejected")
	}
}

func TestTrustedEndpointHTTPClientAllowsLoopbackAndUnspecifiedAddress(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(server.Close)

	_, port, err := net.SplitHostPort(strings.TrimPrefix(server.URL, "http://"))
	if err != nil {
		t.Fatal(err)
	}
	for _, host := range []string{"127.0.0.1", "0.0.0.0"} {
		baseURL := "http://" + net.JoinHostPort(host, port)
		client, clientErr := TrustedEndpointHTTPClient(baseURL+"/v1", 0)
		if clientErr != nil {
			t.Fatal(clientErr)
		}
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

func TestTrustedEndpointHTTPClientDoesNotApplyAddressPolicy(t *testing.T) {
	for _, raw := range []string{
		"http://0.0.0.0:8080/v1", "http://127.0.0.1:8080/v1", "http://169.254.169.254/v1",
		"http://192.168.0.220/v1", "http://[::1]:8080/v1", "https://user:pass@ai.internal/v1",
	} {
		client, err := TrustedEndpointHTTPClient(raw, 0)
		if err != nil {
			t.Fatalf("admin-configured address should be accepted (%s): %v", raw, err)
		}
		if client.CheckRedirect != nil {
			t.Fatalf("admin-configured client should use standard redirect behavior: %s", raw)
		}
	}
}

func TestTrustedEndpointHTTPClientValidatesBaseURL(t *testing.T) {
	for _, raw := range []string{"", "file:///tmp/model", "://bad"} {
		if _, err := TrustedEndpointHTTPClient(raw, 0); err == nil {
			t.Fatalf("invalid base URL should fail: %q", raw)
		}
	}
}
