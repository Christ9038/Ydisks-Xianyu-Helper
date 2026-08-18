package notify

import (
	"context"
	"net"
	"net/http"
	"time"
)

// init 封装init业务协调。
func init() {
	// 通知单测使用 httptest 的 loopback 服务；生产构造器仍使用 netguard。
	newOutboundHTTPClient = func() *http.Client { return &http.Client{Timeout: 10 * time.Second} }
	dialPublicSMTP = func(ctx context.Context, network, address string, timeout time.Duration) (net.Conn, error) {
		return (&net.Dialer{Timeout: timeout}).DialContext(ctx, network, address)
	}
}
