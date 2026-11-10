package netguard

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// newConnCountingServer 启动一个记录新建 TCP 连接数的本地测试端点，返回服务和连接计数器。
func newConnCountingServer(t *testing.T) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	// newConns 统计服务端观察到的新连接数，用于判断客户端是否复用连接。
	newConns := &atomic.Int32{}
	// server 是只返回固定短正文的本地端点。
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
		_, _ = writer.Write([]byte("ok"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			newConns.Add(1)
		}
	}
	server.Start()
	t.Cleanup(server.Close)
	return server, newConns
}

// getAndDrain 用 client 请求 rawURL 并读完、关闭响应体，使连接可回到空闲池；返回请求错误。
func getAndDrain(client *http.Client, rawURL string) error {
	// response、err 保存请求结果。
	response, err := client.Get(rawURL)
	if err != nil {
		return err
	}
	_, _ = io.Copy(io.Discard, response.Body)
	return response.Body.Close()
}

// TestSessionHTTPClientReusesConnection 验证会话客户端在连续顺序请求间复用同一条连接，CloseIdleConnections 后重新拨号。
func TestSessionHTTPClientReusesConnection(t *testing.T) {
	// server、newConns 是本地端点及其新连接计数。
	server, newConns := newConnCountingServer(t)
	// client 是允许内网访问的会话客户端。
	client := policySessionHTTPClient(NewOutboundPolicy(false), time.Second)
	// i 是第几次顺序请求。
	for i := 0; i < 3; i++ {
		// err 保存第 i 次请求错误。
		if err := getAndDrain(client, server.URL); err != nil {
			t.Fatalf("第 %d 次请求失败: %v", i+1, err)
		}
	}
	// got 是三次顺序请求期间新建的连接数。
	if got := newConns.Load(); got != 1 {
		t.Fatalf("三次顺序请求应复用一条连接，实际新建 %d 条", got)
	}
	client.CloseIdleConnections()
	// err 保存关闭空闲连接后再次请求的错误。
	if err := getAndDrain(client, server.URL); err != nil {
		t.Fatal(err)
	}
	// got 是关闭空闲连接后累计的新建连接数。
	if got := newConns.Load(); got != 2 {
		t.Fatalf("关闭空闲连接后应重新拨号，实际新建 %d 条", got)
	}
}

// TestSessionHTTPClientDropsIdleConnOnPolicyChange 验证公网开关变化后不会复用旧连接：开启限制后回环端点立即被拒绝，
// 关闭限制后重新拨号成功。
func TestSessionHTTPClientDropsIdleConnOnPolicyChange(t *testing.T) {
	// server、newConns 是本地端点及其新连接计数。
	server, newConns := newConnCountingServer(t)
	// policy 是本测试独立持有的运行时策略，初始允许内网。
	policy := NewOutboundPolicy(false)
	// client 是基于该策略的会话客户端。
	client := policySessionHTTPClient(policy, time.Second)
	// err 保存允许内网时首次请求的错误。
	if err := getAndDrain(client, server.URL); err != nil {
		t.Fatal(err)
	}
	policy.SetPublicOnly(true)
	// 若仍复用旧连接，这里会绕过公网校验而成功；正确行为是丢弃空闲连接、重新拨号并被拒绝。
	if err := getAndDrain(client, server.URL); err == nil {
		t.Fatal("开启公网限制后复用连接绕过了地址校验")
	}
	policy.SetPublicOnly(false)
	// err 保存关闭公网限制后重新拨号请求的错误。
	if err := getAndDrain(client, server.URL); err != nil {
		t.Fatalf("关闭公网限制后应重新拨号成功: %v", err)
	}
	// got 是策略切换全过程累计的新建连接数。
	if got := newConns.Load(); got != 2 {
		t.Fatalf("策略切换前后应各新建一条连接，实际 %d 条", got)
	}
}

// TestConfiguredEndpointSessionHTTPClient 验证会话客户端的地址校验沿用普通端点规则，并使用默认策略。
func TestConfiguredEndpointSessionHTTPClient(t *testing.T) {
	// rawURL 是逐个校验的非法地址。
	for _, rawURL := range []string{"", "ftp://example.com", "http://"} {
		// err 保存非法地址创建会话客户端的错误，期望非空。
		if _, err := ConfiguredEndpointSessionHTTPClient(rawURL, time.Second); err == nil {
			t.Fatalf("非法地址 %q 应被拒绝", rawURL)
		}
	}
	// client、err 保存合法地址创建的会话客户端。
	client, err := ConfiguredEndpointSessionHTTPClient("https://agent.example.com/v1", time.Second)
	if err != nil || client == nil || client.Timeout != time.Second {
		t.Fatalf("合法地址应创建会话客户端: client=%v err=%v", client, err)
	}
}

// noIdleRoundTripper 是不提供空闲连接释放能力的传输替身。
type noIdleRoundTripper struct{}

// RoundTrip 不会被本测试调用。
func (noIdleRoundTripper) RoundTrip(*http.Request) (*http.Response, error) { return nil, nil }

// TestLimitedResponseBodyTransportCloseIdleWithoutPool 验证底层传输不支持释放空闲连接时转发调用是安全空操作。
func TestLimitedResponseBodyTransportCloseIdleWithoutPool(t *testing.T) {
	limitedResponseBodyTransport{base: noIdleRoundTripper{}}.CloseIdleConnections()
}
