package netguard

import (
	"context"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

// TestOutboundPolicyAndDefaultAccessors 验证运行时策略和默认策略访问器的原子语义。
func TestOutboundPolicyAndDefaultAccessors(t *testing.T) {
	// original 保存测试前的全局默认值，避免影响同一进程中的其他测试。
	original := DefaultPublicOnly()
	t.Cleanup(func() { SetDefaultPublicOnly(original) })
	if DefaultPolicy() == nil {
		t.Fatal("默认策略不能为空")
	}
	SetDefaultPublicOnly(!original)
	if DefaultPublicOnly() == original {
		t.Fatal("默认策略开关未更新")
	}
	// nilPolicy 验证可选策略指针为空时的安全默认行为。
	var nilPolicy *OutboundPolicy
	if nilPolicy.PublicOnly() {
		t.Fatal("空策略不应启用公网限制")
	}
	nilPolicy.SetPublicOnly(true)
}

// TestConfiguredEndpointHTTPClientValidatesAndUsesDefaultPolicy 验证用户配置端点的协议校验和默认客户端创建。
func TestConfiguredEndpointHTTPClientValidatesAndUsesDefaultPolicy(t *testing.T) {
	// raw 表示待校验的用户配置地址。
	for _, raw := range []string{"", "localhost:8080", "file:///tmp/model", "ftp://example.test", "://bad"} {
		// err 保存无效地址的校验错误。
		if _, err := ConfiguredEndpointHTTPClient(raw, time.Second); err == nil {
			t.Fatalf("无效服务地址应被拒绝: %q", raw)
		}
	}
	// raw 表示待校验的有效 HTTP(S) 地址。
	for _, raw := range []string{"http://localhost:8080/v1", "https://localhost/v1"} {
		// client、err 保存有效地址对应的客户端和校验错误。
		if client, err := ConfiguredEndpointHTTPClient(raw, time.Second); err != nil || client == nil {
			t.Fatalf("有效服务地址应创建客户端: %q, %v", raw, err)
		}
	}
	// client 保存默认策略创建的客户端。
	if client := ConfiguredHTTPClient(time.Second); client == nil {
		t.Fatal("默认策略客户端不能为空")
	}
	// nilPolicyClient 保存 nil 策略回退默认策略的客户端。
	if nilPolicyClient := PolicyHTTPClient(nil, time.Second); nilPolicyClient == nil {
		t.Fatal("nil 策略客户端不能为空")
	}
}

// TestPolicyHTTPClientWithTimeoutsAndRedirectRules 验证自定义超时和重定向保护规则。
func TestPolicyHTTPClientWithTimeoutsAndRedirectRules(t *testing.T) {
	// client 是使用独立策略和短超时的客户端。
	client := PolicyHTTPClientWithTimeouts(nil, 3*time.Second, 4*time.Second, 5*time.Second)
	// wrapper 是统一响应体包装传输层，用于读取内部的网络传输配置。
	wrapper, ok := client.Transport.(limitedResponseBodyTransport)
	if !ok {
		t.Fatalf("客户端传输层类型错误: %T", client.Transport)
	}
	// transport 是策略客户端实际使用的底层网络传输层。
	transport, ok := wrapper.base.(*http.Transport)
	if !ok || transport.ResponseHeaderTimeout != 4*time.Second || transport.TLSHandshakeTimeout != 5*time.Second {
		t.Fatalf("自定义超时未传递: %#v", wrapper.base)
	}
	// request 是用于直接验证重定向回调的请求。
	request := &http.Request{URL: &url.URL{Scheme: "ftp", Host: "same"}}
	// err 保存不安全协议重定向的校验错误。
	if err := client.CheckRedirect(request, nil); err == nil || !strings.Contains(err.Error(), "协议") {
		t.Fatal("非 HTTP(S) 重定向应被拒绝")
	}
	request.URL = &url.URL{Scheme: "http", Host: "same"}
	// via 保存已经发生的同主机重定向链。
	via := []*http.Request{{URL: &url.URL{Scheme: "http", Host: "same"}}}
	// err 保存同主机重定向的校验结果。
	if err := client.CheckRedirect(request, via); err != nil {
		t.Fatalf("同主机 HTTP 重定向应允许: %v", err)
	}
	via = append(via, via[0], via[0], via[0], via[0])
	// err 保存超过重定向次数上限的校验错误。
	if err := client.CheckRedirect(request, via); err == nil || !strings.Contains(err.Error(), "次数") {
		t.Fatal("超过重定向上限应被拒绝")
	}
	// proxyRequest 是用于覆盖公网与普通模式代理选择分支的请求。
	// err 保存请求地址解析错误。
	proxyRequest, err := http.NewRequest(http.MethodGet, "http://example.invalid", nil)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存公网模式代理选择的结果。
	if _, err := transport.Proxy(proxyRequest); err != nil {
		t.Fatalf("公网模式代理选择不应报错: %v", err)
	}
	// policy 保存普通模式客户端使用的独立策略。
	policy := NewOutboundPolicy(false)
	// ordinary 保存普通模式客户端。
	ordinary := PolicyHTTPClient(policy, time.Second)
	// ordinaryTransport 保存普通模式客户端的底层传输层。
	ordinaryTransport := ordinary.Transport.(limitedResponseBodyTransport).base.(*http.Transport)
	// err 保存普通模式代理选择的结果。
	if _, err := ordinaryTransport.Proxy(proxyRequest); err != nil {
		t.Fatalf("普通模式代理选择不应报错: %v", err)
	}
}

// TestLimitedResponseBodyReadBranches 验证响应体达到上限和正常结束时的读取分支。
func TestLimitedResponseBodyReadBranches(t *testing.T) {
	// body 是恰好达到自定义上限的内存响应体。
	body := &limitedResponseBody{body: io.NopCloser(strings.NewReader("ab")), remaining: 2}
	// buffer 是接收受限响应体数据的缓冲区。
	buffer := make([]byte, 8)
	// n、err 保存首次读取的字节数和底层结果。
	if n, err := body.Read(buffer); n != 2 || err != nil {
		t.Fatalf("首次受限读取错误: n=%d err=%v", n, err)
	}
	// n、err 保存达到上限后探测底层 EOF 的结果。
	if n, err := body.Read(buffer); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("达到上限后的 EOF 分支错误: n=%d err=%v", n, err)
	}
	// err 保存关闭内存响应体时的系统错误。
	if err := body.Close(); err != nil {
		t.Fatalf("关闭响应体失败: %v", err)
	}
}

// TestDialContextWithPolicyBranches 验证地址格式、拒绝和连接失败路径均返回安全错误。
func TestDialContextWithPolicyBranches(t *testing.T) {
	// alwaysDeny 是拒绝所有解析地址的策略函数。
	alwaysDeny := func(net.IP) bool { return false }
	// err 保存缺少端口时的地址解析错误。
	if _, err := dialContextWithPolicy(context.Background(), "tcp", "localhost", time.Second, alwaysDeny, "拒绝", "连接"); err == nil {
		t.Fatal("缺少端口的地址应被拒绝")
	}
	// err 保存策略拒绝本地地址的错误。
	if _, err := dialContextWithPolicy(context.Background(), "tcp", "localhost:1", time.Second, alwaysDeny, "拒绝", "连接"); err == nil || err.Error() != "拒绝" {
		t.Fatalf("不允许地址应返回策略错误: %v", err)
	}
	// listener 用于获得一个随后关闭的本地端口，以稳定触发拨号失败分支。
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	// address 保存临时监听器释放前绑定的本地地址。
	address := listener.Addr().String()
	// err 保存关闭临时监听器时的系统错误。
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	// alwaysAllow 是允许所有解析地址的测试策略。
	alwaysAllow := func(net.IP) bool { return true }
	// err 保存允许地址但连接失败时的包装错误。
	if _, err := dialContextWithPolicy(context.Background(), "tcp", address, time.Second, alwaysAllow, "拒绝", "连接失败"); err == nil || !strings.Contains(err.Error(), "连接失败") {
		t.Fatalf("允许地址的拨号失败应带连接错误: %v", err)
	}
	// lookupErr 保存无法解析保留域名时的 DNS 错误。
	if _, lookupErr := dialContextWithPolicy(context.Background(), "tcp", "does-not-exist.invalid:80", time.Second, alwaysAllow, "拒绝", "连接失败"); lookupErr == nil {
		t.Fatal("无法解析主机应返回 DNS 错误")
	}
	// successListener 保存用于覆盖公网策略允许连接分支的本地监听器。
	successListener, successListenErr := net.Listen("tcp", "127.0.0.1:0")
	if successListenErr != nil {
		t.Fatal(successListenErr)
	}
	defer successListener.Close()
	// successConn、successDialErr 保存允许策略成功建立的连接。
	successConn, successDialErr := dialContextWithPolicy(context.Background(), "tcp", successListener.Addr().String(), time.Second, alwaysAllow, "拒绝", "连接失败")
	if successDialErr != nil || successConn == nil {
		t.Fatalf("允许地址应成功连接: conn=%v err=%v", successConn, successDialErr)
	}
	successConn.Close()
}

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
	if IsPublicIP(net.IP{1, 2, 3}) {
		t.Fatal("invalid IP should be rejected")
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
	for _, raw := range []string{"", "file:///tmp/model", "ftp://example.test", "://bad"} {
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
