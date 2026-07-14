// Package netguard 按目标信任级别约束出站连接并防止 DNS rebinding。
package netguard

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

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
	transport := &http.Transport{
		DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
			return DialPublicContext(ctx, network, address, 10*time.Second)
		},
		TLSHandshakeTimeout: 10 * time.Second,
	}
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 5 {
				return fmt.Errorf("重定向次数过多")
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("重定向协议不安全")
			}
			return nil
		},
	}
}

// TrustedEndpointHTTPClient 用于管理员明确配置的集成端点。目标地址完全由管理员
// 信任并负责，因此不限制 IP 类型、DNS 结果或 HTTP 重定向；仅校验 HTTP(S) URL。
func TrustedEndpointHTTPClient(rawBaseURL string, timeout time.Duration) (*http.Client, error) {
	baseURL, err := url.Parse(strings.TrimSpace(rawBaseURL))
	if err != nil || baseURL.Hostname() == "" {
		return nil, fmt.Errorf("服务地址无效")
	}
	if baseURL.Scheme != "http" && baseURL.Scheme != "https" {
		return nil, fmt.Errorf("服务地址只支持 http 或 https")
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	return &http.Client{
		Transport: transport,
		Timeout:   timeout,
	}, nil
}

func IsPublicIP(ip net.IP) bool {
	addr, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	addr = addr.Unmap()
	if !addr.IsGlobalUnicast() {
		return false
	}
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

func dialContextWithPolicy(ctx context.Context, network, address string, timeout time.Duration,
	allow func(net.IP) bool, deniedMessage, connectErrorMessage string,
) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, err
	}
	ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
	if err != nil {
		return nil, err
	}
	dialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	var lastErr error
	foundAllowed := false
	for _, resolved := range ips {
		if allow(resolved.IP) {
			foundAllowed = true
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
