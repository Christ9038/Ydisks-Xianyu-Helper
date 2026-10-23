package mtop

import (
	"context"
	"math/rand/v2"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

const (
	// DefaultPacerGap 是同一账号任意两次 MTOP 请求起点之间的最短间隔；所有业务路径共用，防止任何入口形成高频访问。
	DefaultPacerGap = 2 * time.Second
	// BackgroundPacerGap 是后台批量任务（评价、擦亮、分页同步等）使用的更长间隔，避免批量动作触发风控。
	BackgroundPacerGap = 5 * time.Second
	// pacerJitterMax 是叠加在基础间隔上的随机抖动上限，避免形成固定节拍。
	pacerJitterMax = time.Second
	// pacerIdleRetention 是账号节拍记录的保留时长；超过该时长的记录必然已过期，可以安全清理。
	pacerIdleRetention = 10 * time.Minute
)

// pacingContextKey 是上下文中标记“后台批量请求”的私有键，避免与其他包的键冲突。
type pacingContextKey struct{}

// WithBackgroundPacing 标记 ctx 派生的 MTOP 请求属于后台批量任务，使用更长的账号级请求间隔。
func WithBackgroundPacing(ctx context.Context) context.Context {
	return context.WithValue(ctx, pacingContextKey{}, true)
}

// Pacer 为每个账号维护 MTOP 请求的最小间隔，是进程内所有 MTOP 访问的唯一频率闸门。
//
// 并发契约：mu 保护 next 与 lastSweep；Wait 只在持锁时预约时隙，睡眠发生在锁外。
// 预约先于睡眠完成，因此并发调用方按预约顺序被串行拉开，不会同时放行。
type Pacer struct {
	// mu 保护 next 与 lastSweep。
	mu sync.Mutex
	// next 记录每个账号键下一次允许发起请求的时刻。
	next map[string]time.Time
	// lastSweep 记录上次清理过期账号记录的时刻。
	lastSweep time.Time
	// now 返回当前时间，测试可注入固定时钟。
	now func() time.Time
	// sleep 在 ctx 约束内等待 d；返回非空错误表示被取消，测试可注入记录型实现。
	sleep func(ctx context.Context, d time.Duration) error
	// jitter 返回叠加的随机抖动，测试可注入确定值。
	jitter func() time.Duration
	// skipLoopback 为 true 时不对回环地址限速：本地测试服务不会触达平台。
	skipLoopback bool
}

// NewPacer 构造生产使用的节拍器：真实时钟、随机抖动，并跳过回环地址。
func NewPacer() *Pacer {
	return &Pacer{
		next:         make(map[string]time.Time),
		now:          time.Now,
		sleep:        sleepContext,
		jitter:       func() time.Duration { return time.Duration(rand.Int64N(int64(pacerJitterMax))) },
		skipLoopback: true,
	}
}

// defaultPacer 是进程级共享节拍器。多个 NewClient 实例必须共享它，才能让同一账号的请求被统一排队。
var defaultPacer = NewPacer()

// sleepContext 等待 d 或 ctx 取消，取消时返回 ctx 的错误。
func sleepContext(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	// timer 只服务本次等待，函数返回前停止。
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

// Wait 为 key 对应账号预约下一个请求时隙并等待到点；gap 是本次请求之后下一个请求至少需要的间隔基数。
// 间隔以请求起点计算，等待被取消时已预约的时隙保持占用，避免取消-重试形成突发。
func (p *Pacer) Wait(ctx context.Context, key string, gap time.Duration) error {
	// now 是预约时刻，allowedAt 是该账号最早允许发起请求的时刻。
	now := p.now()
	p.mu.Lock()
	p.sweepLocked(now)
	// allowedAt 取账号已预约时隙与当前时刻中较晚者，保证请求起点单调递增。
	allowedAt := p.next[key]
	if allowedAt.Before(now) {
		allowedAt = now
	}
	p.next[key] = allowedAt.Add(gap + p.jitter())
	p.mu.Unlock()
	return p.sleep(ctx, allowedAt.Sub(now))
}

// sweepLocked 清理长期空闲账号的记录，调用方必须持有 mu。
func (p *Pacer) sweepLocked(now time.Time) {
	if now.Sub(p.lastSweep) < pacerIdleRetention {
		return
	}
	p.lastSweep = now
	// key、at 遍历账号记录；已过期超过保留时长的条目不再影响节拍。
	for key, at := range p.next {
		if now.Sub(at) > pacerIdleRetention {
			delete(p.next, key)
		}
	}
}

// pacerAccountKey 从请求 Cookie 中提取账号标识 unb 作为节拍键；缺失时落入共享的匿名键，仍然被限速。
func pacerAccountKey(header http.Header) string {
	// part 遍历 Cookie 头中以分号分隔的键值对。
	for _, part := range strings.Split(header.Get("Cookie"), ";") {
		// name、value 是单个 Cookie 的名与值。
		name, value, found := strings.Cut(strings.TrimSpace(part), "=")
		if found && name == "unb" && value != "" {
			return value
		}
	}
	return ""
}

// isLoopbackHost 判断请求目标是否为本机回环地址。
func isLoopbackHost(host string) bool {
	// hostname 去掉端口后用于判定。
	hostname, _, err := net.SplitHostPort(host)
	if err != nil {
		hostname = host
	}
	if hostname == "localhost" {
		return true
	}
	// ip 为空表示不是字面 IP，视为远端域名。
	ip := net.ParseIP(hostname)
	return ip != nil && ip.IsLoopback()
}

// pacingTransport 在每个出站 MTOP 请求前通过 Pacer 取得账号级时隙，覆盖全部业务入口和 token 重试。
type pacingTransport struct {
	// base 是实际发送请求的下层传输。
	base http.RoundTripper
	// pacer 是账号级节拍器。
	pacer *Pacer
}

// RoundTrip 先等待账号时隙再发送请求；等待被取消时请求不会发出。
func (t pacingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.pacer == nil || (t.pacer.skipLoopback && isLoopbackHost(req.URL.Host)) {
		return t.base.RoundTrip(req)
	}
	// gap 默认使用交互式间隔，后台批量请求使用更长间隔。
	gap := DefaultPacerGap
	// background 表示请求上下文被标记为后台批量访问。
	background, _ := req.Context().Value(pacingContextKey{}).(bool)
	if background {
		gap = BackgroundPacerGap
	}
	// waitErr 是等待账号时隙时的取消错误，非空时请求不会发出。
	if waitErr := t.pacer.Wait(req.Context(), pacerAccountKey(req.Header), gap); waitErr != nil {
		return nil, waitErr
	}
	return t.base.RoundTrip(req)
}

// pacedTransport 用账号级节拍器包装下层传输。
// 显式注入 HTTPClient 表示调用方自带传输（仅限本地测试替身，由 TestProductionNeverInjectsHTTPClient 守护），此时不限速。
func (c *ClientImpl) pacedTransport(logged http.RoundTripper) http.RoundTripper {
	// pacer 是本客户端使用的账号级节拍器；生产客户端的所有出站请求必须先经过它。
	pacer := c.Pacer
	if pacer == nil && c.HTTPClient == nil {
		pacer = defaultPacer
	}
	if pacer == nil {
		return logged
	}
	return pacingTransport{base: logged, pacer: pacer}
}
