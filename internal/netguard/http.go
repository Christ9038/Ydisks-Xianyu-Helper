// Package netguard 按目标信任级别约束出站连接并防止 DNS rebinding。
package netguard

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync/atomic"
	"time"
)

// ConfiguredResponseBodyLimit 是用户可配置 HTTP 客户端允许读取的最大响应体大小。
const ConfiguredResponseBodyLimit int64 = 10 << 20

// OutboundPolicy 保存服务端用户可配置 HTTP 请求的公网访问策略。
type OutboundPolicy struct {
	// publicOnly 表示是否只允许解析到公网 IP 的目标地址。
	publicOnly atomic.Bool
}

// defaultOutboundPolicy 是进程内所有用户可配置 HTTP 客户端共享的策略快照。
var defaultOutboundPolicy = NewOutboundPolicy(false)

// NewOutboundPolicy 创建一个出站访问策略；默认值由调用方明确传入。
func NewOutboundPolicy(publicOnly bool) *OutboundPolicy {
	// policy 保存新建的原子出站策略对象。
	policy := &OutboundPolicy{}
	policy.publicOnly.Store(publicOnly)
	return policy
}

// SetPublicOnly 原子切换是否只允许访问公网地址。
func (p *OutboundPolicy) SetPublicOnly(publicOnly bool) {
	if p != nil {
		p.publicOnly.Store(publicOnly)
	}
}

// PublicOnly 返回当前是否启用公网地址限制。
func (p *OutboundPolicy) PublicOnly() bool {
	return p != nil && p.publicOnly.Load()
}

// SetDefaultPublicOnly 更新默认策略，供进程组合根和系统设置保存后调用。
func SetDefaultPublicOnly(publicOnly bool) {
	defaultOutboundPolicy.SetPublicOnly(publicOnly)
}

// DefaultPolicy 返回进程默认出站策略，供组合根注入应用服务而非让仓储修改全局状态。
func DefaultPolicy() *OutboundPolicy {
	return defaultOutboundPolicy
}

// DefaultPublicOnly 返回默认策略当前是否只允许公网地址。
func DefaultPublicOnly() bool {
	return defaultOutboundPolicy.PublicOnly()
}

// nonPublicPrefixes 用于本次流程后续判断的nonPublicPrefixes
var nonPublicPrefixes = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"), netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"), netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"), netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"), netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"), netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"), netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"), netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"), netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("fc00::/7"), netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"), netip.MustParsePrefix("2001:db8::/32"),
}

// PublicHTTPClient 返回只允许连接公网 IP 的客户端。DNS 与每次重定向都会重新校验。
func PublicHTTPClient(timeout time.Duration) *http.Client {
	return policyHTTPClient(NewOutboundPolicy(true), timeout, 10*time.Second, 10*time.Second)
}

// ConfiguredHTTPClient 返回遵循默认运行时策略的用户配置 HTTP 客户端。
func ConfiguredHTTPClient(timeout time.Duration) *http.Client {
	return policyHTTPClient(defaultOutboundPolicy, timeout, 10*time.Second, 10*time.Second)
}

// ConfiguredEndpointHTTPClient 校验用户配置的 HTTP(S) 基础地址并返回策略客户端。
func ConfiguredEndpointHTTPClient(rawBaseURL string, timeout time.Duration) (*http.Client, error) {
	// err 表示用户配置地址的格式校验错误；地址不合法时不创建客户端。
	if err := validateConfiguredEndpointURL(rawBaseURL); err != nil {
		return nil, err
	}
	return ConfiguredHTTPClient(timeout), nil
}

// PolicyHTTPClient 返回绑定指定运行时策略的 HTTP 客户端，测试和组合根可使用它隔离全局默认值。
func PolicyHTTPClient(policy *OutboundPolicy, timeout time.Duration) *http.Client {
	if policy == nil {
		policy = defaultOutboundPolicy
	}
	return policyHTTPClient(policy, timeout, 10*time.Second, 10*time.Second)
}

// PolicyHTTPClientWithTimeouts 返回可保留特殊响应头和 TLS 超时约束的策略客户端。
func PolicyHTTPClientWithTimeouts(policy *OutboundPolicy, timeout, responseHeaderTimeout, tlsHandshakeTimeout time.Duration) *http.Client {
	if policy == nil {
		policy = defaultOutboundPolicy
	}
	return policyHTTPClient(policy, timeout, responseHeaderTimeout, tlsHandshakeTimeout)
}

// ConfiguredEndpointSessionHTTPClient 校验用户配置的 HTTP(S) 地址，返回供一次短时会话（如一轮 Agent 对话）
// 内部复用连接的策略客户端。与 ConfiguredEndpointHTTPClient 的区别只在连接复用：
//   - 新连接仍在拨号时按当前公网策略解析并校验目标地址；
//   - 每次请求前若发现公网开关自上次请求后发生变化，先关闭全部空闲连接，迫使本次请求重新拨号校验；
//   - 空闲连接最多保留 1 条且 30 秒后过期，调用方必须在会话结束时调用 CloseIdleConnections 释放。
//
// 它只适合顺序发送、生命周期有明确上限的会话；长期共享的客户端仍必须使用不复用连接的 ConfiguredEndpointHTTPClient。
func ConfiguredEndpointSessionHTTPClient(rawBaseURL string, timeout time.Duration) (*http.Client, error) {
	// err 表示用户配置地址的格式校验错误；地址不合法时不创建客户端。
	if err := validateConfiguredEndpointURL(rawBaseURL); err != nil {
		return nil, err
	}
	return policySessionHTTPClient(defaultOutboundPolicy, timeout), nil
}

// validateConfiguredEndpointURL 校验用户配置地址必须是带主机名的 http(s) 绝对地址。
func validateConfiguredEndpointURL(rawBaseURL string) error {
	// baseURL、err 保存用户配置地址的解析结果及格式错误。
	baseURL, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil || baseURL.Hostname() == "" {
		return fmt.Errorf("服务地址无效")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return fmt.Errorf("服务地址只支持 http 或 https")
	}
	return nil
}

// sessionIdleConnTimeout 是会话客户端空闲连接的最长保留时间；Agent 轮询间隔最长 10 秒，30 秒足以覆盖且不会长期占用连接。
const sessionIdleConnTimeout = 30 * time.Second

// policySessionHTTPClient 创建允许连接复用、但在公网策略变化时丢弃空闲连接的策略客户端。
func policySessionHTTPClient(policy *OutboundPolicy, timeout time.Duration) *http.Client {
	// client 复用统一策略客户端的拨号、代理、重定向和响应体上限规则，只额外开启连接复用。
	client := newPolicyHTTPClient(policy, timeout, 10*time.Second, 10*time.Second, true)
	// limited 是外层响应体上限传输；这里把策略变化检测插在它与底层 Transport 之间。
	limited := client.Transport.(limitedResponseBodyTransport)
	// guard 记录创建时的公网策略，供首次请求前比较。
	guard := &policyChangeTransport{base: limited.base.(*http.Transport), policy: policy}
	guard.lastPublicOnly.Store(policy.PublicOnly())
	client.Transport = limitedResponseBodyTransport{base: guard}
	return client
}

// policyChangeTransport 在公网策略变化后先关闭空闲连接，保证复用连接不会跨越一次策略切换。
//
// 并发：lastPublicOnly 是原子值；并发请求同时观察到变化时可能重复关闭空闲连接，关闭操作幂等，不影响正确性。
type policyChangeTransport struct {
	// base 是实际持有连接池的底层 Transport。
	base *http.Transport
	// policy 是客户端遵循的运行时公网策略。
	policy *OutboundPolicy
	// lastPublicOnly 是上一次请求时观察到的公网开关值。
	lastPublicOnly atomic.Bool
}

// RoundTrip 先比较公网开关，变化时关闭空闲连接，再交给底层 Transport 发送。
func (t *policyChangeTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// current 是本次请求发起时的公网开关值。
	current := t.policy.PublicOnly()
	if t.lastPublicOnly.Swap(current) != current {
		t.base.CloseIdleConnections()
	}
	return t.base.RoundTrip(req)
}

// CloseIdleConnections 释放底层连接池中的空闲连接，供会话结束时调用。
func (t *policyChangeTransport) CloseIdleConnections() { t.base.CloseIdleConnections() }

// policyHTTPClient 创建每次连接和重定向都遵循同一公网策略、且不复用连接的 HTTP 客户端。
func policyHTTPClient(policy *OutboundPolicy, timeout, responseHeaderTimeout, tlsHandshakeTimeout time.Duration) *http.Client {
	return newPolicyHTTPClient(policy, timeout, responseHeaderTimeout, tlsHandshakeTimeout, false)
}

// newPolicyHTTPClient 创建遵循公网策略的 HTTP 客户端；keepAlive 为 false 时禁用连接复用，
// 为 true 时只保留 1 条空闲连接并在 sessionIdleConnTimeout 后过期，仅供 policySessionHTTPClient 使用。
func newPolicyHTTPClient(policy *OutboundPolicy, timeout, responseHeaderTimeout, tlsHandshakeTimeout time.Duration, keepAlive bool) *http.Client {
	// transport 保存统一出站策略客户端的连接和重定向规则。
	transport := &http.Transport{
		Proxy: func(request *http.Request) (*url.URL, error) {
			if policy.PublicOnly() {
				return nil, nil
			}
			return http.ProxyFromEnvironment(request)
		},
		// 默认禁用连接复用，确保公网开关切换后每个新请求都重新解析并校验目标地址；
		// 会话客户端开启复用时由 policyChangeTransport 在开关变化后丢弃空闲连接。
		DisableKeepAlives:     !keepAlive,
		ForceAttemptHTTP2:     false,
		TLSHandshakeTimeout:   tlsHandshakeTimeout,
		ResponseHeaderTimeout: responseHeaderTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			if policy.PublicOnly() {
				return DialPublicContext(ctx, network, address, 10*time.Second)
			}
			// dialer 保存允许内网模式下的普通 TCP 拨号器。
			dialer := &net.Dialer{Timeout: 10 * time.Second, KeepAlive: 30 * time.Second}
			return dialer.DialContext(ctx, network, address)
		},
	}
	if keepAlive {
		// 会话客户端只顺序发送请求，保留 1 条空闲连接即可复用，且限时过期避免长期占用。
		transport.MaxIdleConns, transport.MaxIdleConnsPerHost, transport.IdleConnTimeout = 1, 1, sessionIdleConnTimeout
	}
	return &http.Client{
		Transport: limitedResponseBodyTransport{base: transport},
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("重定向次数过多")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("重定向协议不安全")
			}
			if len(via) > 0 && req.URL.Host != via[0].URL.Host {
				return fmt.Errorf("不允许跨主机重定向")
			}
			return nil
		},
	}
}

// limitedResponseBody 把用户可配置端点的响应体限制在固定大小以内。
type limitedResponseBody struct {
	// body 是底层远端响应体，关闭责任仍由当前 HTTP 客户端持有。
	body io.ReadCloser
	// remaining 是尚未读取的允许字节数。
	remaining int64
}

// Read 读取响应体并在尝试读取超限内容时返回错误。
func (b *limitedResponseBody) Read(p []byte) (int, error) {
	if b.remaining == 0 {
		// probe 用于探测底层响应是否仍有超出上限的字节。
		var probe [1]byte
		// n、err 保存探测读取的字节数及底层读取结果。
		n, err := b.body.Read(probe[:])
		if n > 0 {
			return 0, errors.New("HTTP 响应超过大小限制")
		}
		return 0, err
	}
	if int64(len(p)) > b.remaining {
		p = p[:int(b.remaining)]
	}
	// n、err 保存本次受限读取的字节数及底层读取结果。
	n, err := b.body.Read(p)
	b.remaining -= int64(n)
	return n, err
}

// Close 关闭底层响应体并释放远端连接资源。
func (b *limitedResponseBody) Close() error { return b.body.Close() }

// limitedResponseBodyTransport 为响应体附加统一大小边界。
type limitedResponseBodyTransport struct {
	// base 是执行 DNS、连接和重定向策略的底层 Transport。
	base http.RoundTripper
}

// RoundTrip 执行请求并包装远端响应体，不改变状态码和响应头语义。
func (t limitedResponseBodyTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// response、err 保存底层传输返回的响应和网络错误。
	response, err := t.base.RoundTrip(req)
	if err != nil || response == nil || response.Body == nil {
		return response, err
	}
	response.Body = &limitedResponseBody{body: response.Body, remaining: ConfiguredResponseBodyLimit}
	return response, nil
}

// CloseIdleConnections 把空闲连接释放请求转交给底层传输；底层不持有连接池时不做任何事。
func (t limitedResponseBodyTransport) CloseIdleConnections() {
	// closer、ok 保存底层传输的空闲连接释放能力及其是否可用。
	if closer, ok := t.base.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
}

// TrustedEndpointHTTPClient 用于管理员明确配置的集成端点。目标地址完全由管理员
// 信任并负责，因此不限制 IP 类型、DNS 结果或 HTTP 重定向；仅校验 HTTP(S) URL。
// TrustedEndpointHTTPClient 封装TrustedEndpointHTTPClient业务协调。
func TrustedEndpointHTTPClient(rawBaseURL string, timeout time.Duration) (*http.Client, error) {
	// baseURL、err 用于本次流程后续判断的baseURL、err
	baseURL, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil || baseURL.Hostname() == "" {
		return nil, fmt.Errorf("服务地址无效")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("服务地址只支持 http 或 https")
	}
	// transport 用于本次流程后续判断的transport
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}

// IsPublicIP 封装IsPublicIP业务协调。
func IsPublicIP(ip net.IP) bool {
	// addr、ok 用于本次流程后续判断的addr、ok
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() {
		return false
	}
	// prefix 表示当前遍历过程中的prefix
	for _, prefix := range nonPublicPrefixes {
		if prefix.Contains(addr) {
			return false
		}
	}
	return true
}

// DialPublicContext 解析目标主机并直接连接已验证的公网 IP，避免校验后再次解析造成 DNS rebinding。
func DialPublicContext(ctx context.Context, network, address string, timeout time.Duration) (net.Conn, error) {
	return dialContextWithPolicy(ctx, network, address, timeout, IsPublicIP,
		"拒绝访问非公网地址", "连接公网地址失败")
}

// dialContextWithPolicy 封装dial上下文WithPolicy业务协调。
func dialContextWithPolicy(ctx context.Context, network, address string, timeout time.Duration,
	allow func(net.IP) bool, deniedMessage, connectErrorMessage string,
) (net.Conn, error) {
	// host、port、err 用于本次流程后续判断的host、port、err
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	// ips、err 用于本次流程后续判断的ips、err
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	// dialer 用于本次流程后续判断的dialer
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	// lastErr 用于本次流程后续判断的lastErr
	var lastErr error
	// foundAllowed 用于本次流程后续判断的foundAllowed
	foundAllowed := false
	// resolved 表示当前遍历过程中的resolved
	for _, resolved := range ips {
		if allow(resolved.IP) {
			foundAllowed = true
			// conn、dialErr 用于本次流程后续判断的conn、dialErr
			conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(resolved.IP.String(), port))
			if dialErr == nil {
				return conn, nil
			}
			lastErr = dialErr
		}
	}
	if foundAllowed && lastErr != nil {
		return nil, fmt.Errorf("%s: %w", connectErrorMessage, lastErr)
	}
	return nil, errors.New(deniedMessage)
}
