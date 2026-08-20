// Package mtop 实现闲鱼 mtop H5 API 客户端。
// 关键：签名只覆盖 (t, token, data_val)，与 URL query 参数无关；token 取自 cookie _m_h5_tk 前半段。
package mtop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// RegAppKey 是 WS 注册用的 appKey（与签名用的 protocol.SignAppKey 不同）。
const RegAppKey = "444e9908a51d1cb236a27862abc769c9"

// TokenAPI 取 access token 的端点。
const TokenAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idlemessage.pc.login.token/1.0/"

// ConsignAPI 是虚拟商品确认发货端点。
const ConsignAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.logistic.consign.dummy/1.0/"

// OrderDetailAPI 是卖家订单详情端点。
const OrderDetailAPI = "https://h5api.m.goofish.com/h5/mtop.idle.web.trade.order.detail/1.0/"

// UserPageNavAPI 是 PC 站当前登录账号资料端点。
const UserPageNavAPI = "https://h5api.m.goofish.com/h5/mtop.idle.web.user.page.nav/1.0/"

// ItemListAPI 是卖家商品列表端点。
const ItemListAPI = "https://h5api.m.goofish.com/h5/mtop.idle.web.xyh.item.list/1.0/"

// SoldOrdersAPI 是闲鱼卖家工作台的已售订单列表端点。
const SoldOrdersAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.trade.merchant.sold.get/1.0/"

// ItemDetailAPI 是闲鱼 PC 商品详情端点。
const ItemDetailAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.pc.detail/1.0/"

// MTopRetryGap 用于本次流程后续判断的MTop重试Gap
const (
	MTopRetryGap         = time.Second
	ItemPageGap          = time.Second
	maxMTopResponseBytes = 8 << 20
)

// Client 是 mtop API 的最小契约，供 server/automation/engine 依赖注入与测试 mock。
// 具体实现 *ClientImpl 走 HTTP；测试可注入自定义实现以隔离网络。
// Client 用于本次流程后续判断的Client
type Client interface {
	FetchUserProfile(ctx context.Context, cookiesStr string) (*UserProfileResult, error)
	ConsignContext(ctx context.Context, cookiesStr, orderID string) (ok bool, ret []string, updatedCookies string, err error)
	AdjustOrderPriceContext(ctx context.Context, cookiesStr, orderID string, priceCents int64) (ok bool, ret []string, updatedCookies string, err error)
	FetchItemsPage(ctx context.Context, cookiesStr string, pageNumber, pageSize int) (*ItemListResult, error)
	FetchAllItems(ctx context.Context, cookiesStr string, pageSize, maxPages int) (*ItemListResult, error)
	PublishItem(ctx context.Context, cookiesStr string, req PublishItemRequest) (*PublishItemResult, error)
	RefreshTokenWithDeviceIDContext(ctx context.Context, cookiesStr, deviceID string) (*RefreshResult, error)
}

// ClientImpl 是 Client 接口的 HTTP 实现。零值可用；HTTP 超时默认 30s。
// 仍导出 HTTPClient/TokenURL 等字段以便调用方覆盖（如测试注入 RoundTripper）。
// ClientImpl 用于本次流程后续判断的ClientImpl
type ClientImpl struct {
	HTTPClient *http.Client
	// Logger 记录 MTOP 请求的安全摘要（不会输出 Cookie、签名或响应正文）。
	// 未设置时使用 slog.Default，测试可传入丢弃日志的 logger。
	Logger              *slog.Logger
	TokenURL   string
	ConsignURL string
	// AdjustPriceURL 覆盖订单改价端点，仅供测试注入本地 HTTP 服务。
	AdjustPriceURL      string
	OrderDetailURL      string
	SoldOrdersURL       string
	ItemDetailURL       string
	LoginUserURL        string
	RateCreateURL       string
	RateListURL         string
	PolishItemURL       string
	PolishItemBackupURL string
	ChatUserQueryURL    string
}

// httpClient 返回带统一请求/响应日志的 HTTP 客户端副本。统一放在传输层，
// 确保 token、续期、订单、商品和发布等所有 MTOP 调用都具备一致的可观测性。
// httpClient 封装httpClient业务协调。
func (c *ClientImpl) httpClient() *http.Client {
	return c.httpClientWithTimeout(30 * time.Second)
}

// httpClientWithTimeout 封装httpClientWithTimeout业务协调。
func (c *ClientImpl) httpClientWithTimeout(defaultTimeout time.Duration) *http.Client {
	// hc 用于本次流程后续判断的hc
	hc := c.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: defaultTimeout}
	}
	// clone 用于本次流程后续判断的clone
	clone := *hc
	// transport 用于本次流程后续判断的transport
	transport := hc.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clone.Transport = loggingTransport{base: transport, logger: c.Logger}
	return &clone
}

// loggingTransport 用于本次流程后续判断的loggingTransport
type loggingTransport struct {
	base   http.RoundTripper
	logger *slog.Logger
}

// RoundTrip 封装RoundTrip业务协调。
func (t loggingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// logger 用于本次流程后续判断的logger
	logger := t.logger
	if logger == nil {
		logger = slog.Default()
	}
	// started 用于本次流程后续判断的started
	started := time.Now()
	logger.Debug("MTOP 请求开始", "method", req.Method, "path", req.URL.Path)
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := t.base.RoundTrip(req)
	// attrs 用于本次流程后续判断的attrs
	attrs := []any{"method", req.Method, "path", req.URL.Path, "duration", time.Since(started).Round(time.Millisecond)}
	if err != nil {
		if errors.Is(err, context.Canceled) {
			logger.Debug("MTOP 请求取消", append(attrs, "err", err)...)
		} else {
			logger.Warn("MTOP 请求失败", append(attrs, "err", err)...)
		}
		return nil, err
	}
	// responseAttrs 用于本次流程后续判断的响应Attrs
	responseAttrs := append(attrs, "status", resp.StatusCode, "content_length", resp.ContentLength)
	if resp.StatusCode >= http.StatusBadRequest {
		logger.Warn("MTOP HTTP 响应异常", responseAttrs...)
	} else {
		logger.Debug("MTOP 响应收到", responseAttrs...)
	}
	return resp, nil
}

// NewClient 构造纯 Go HTTP 的 MTOP 客户端。Chromium 只用于读取本机指纹
// 和处理滑块，不能成为登录、续期、token 或 WebSocket 的传输层。
// NewClient 封装NewClient业务协调。
func NewClient() *ClientImpl {
	return &ClientImpl{}
}

// 编译期保证 *ClientImpl 实现 Client 接口。
var _ Client = (*ClientImpl)(nil)

// RefreshResult 是刷新 token 的结果。
type RefreshResult struct {
	AccessToken            string                        // 用于 WS /reg 注册
	AccessTokenExpireAt    int64                         // 服务端 accessTokenExpiredTime 归一化后的 Unix 秒
	UpdatedCookies         string                        // 合并 Set-Cookie 后的新 cookie 字符串（无变化则与入参相同）
	CookieSnapshot         []cookierefresh.BrowserCookie // token 请求后的完整 Cookie Jar
	CookieSnapshotComplete bool                          // true 表示快照权威完整；空切片代表 Cookie Jar 已被清空
	CookieStateChanged     bool                          // true 表示本次响应明确更新或删除了 Cookie（包括更新后为空）
}

// FreshCaptchaResult 是重取 token 风控验证链接的结果。
type FreshCaptchaResult struct {
	TokenOK             bool
	AccessToken         string
	AccessTokenExpireAt int64
	UpdatedCookies      string
	VerificationURL     string
	Ret                 []string
}

// UserProfileResult 是 mtop.idle.web.user.page.nav 返回的当前账号资料。
type UserProfileResult struct {
	Nickname       string
	DisplayNick    string
	AvatarURL      string
	UpdatedCookies string
}

// ItemListResult 是卖家商品列表结果。
type ItemListResult struct {
	Items          []ItemListItem
	PageNumber     int
	PageSize       int
	CurrentCount   int
	TotalCount     int
	TotalPages     int
	SavedCountHint int
	UpdatedCookies string
}

// ItemListItem 是 mtop.idle.web.xyh.item.list 的核心商品信息。
type ItemListItem struct {
	ID          string
	Title       string
	Price       string
	PriceText   string
	CategoryID  string
	DetailURL   string
	WebURL      string
	PicURL      string
	ItemDetail  string
	AuctionType string
	ItemStatus  int
	IsMultiSpec bool
}

// hasMTopSuccess 封装hasMTopSuccess业务协调。
func hasMTopSuccess(ret []string) bool {
	// r 表示当前遍历过程中的r
	for _, r := range ret {
		if strings.Contains(r, "SUCCESS::调用成功") {
			return true
		}
	}
	return false
}

// isTokenExpiredRet 封装is令牌ExpiredRet业务协调。
func isTokenExpiredRet(ret []string) bool {
	// r 表示当前遍历过程中的r
	for _, r := range ret {
		if strings.Contains(r, "FAIL_SYS_TOKEN_EXOIRED") ||
			strings.Contains(r, "FAIL_SYS_TOKEN_EXPIRED") ||
			strings.Contains(r, "FAIL_SYS_TOKEN_EMPTY") {
			return true
		}
	}
	return false
}

// SessionExpiredError 表示平台已经明确判定整个登录 Session 失效。
// 它与可通过响应 Set-Cookie 重签的普通 MTOP token 过期不同：调用方必须
// 立刻停止业务 API 重试，转入账号级 Session 续期流程。
// SessionExpiredError 用于本次流程后续判断的会话Expired错误
type SessionExpiredError struct {
	API string
	Ret []string
}

// Error 封装错误业务协调。
func (e *SessionExpiredError) Error() string {
	if e == nil {
		return ""
	}
	if e.API == "" {
		return fmt.Sprintf("Session 过期: ret=%v", e.Ret)
	}
	return fmt.Sprintf("%s Session 过期: ret=%v", e.API, e.Ret)
}

// isSessionExpiredRet 封装is会话ExpiredRet业务协调。
func isSessionExpiredRet(ret []string) bool {
	// value 表示当前遍历过程中的值
	for _, value := range ret {
		// lower 用于本次流程后续判断的lower
		lower := strings.ToLower(value)
		if strings.Contains(lower, "fail_sys_session_expired") ||
			strings.Contains(lower, "session过期") ||
			strings.Contains(lower, "session expired") ||
			strings.Contains(lower, "会话过期") {
			return true
		}
	}
	return false
}

// sessionExpiredError 封装会话Expired错误业务协调。
func sessionExpiredError(api string, ret []string) error {
	return &SessionExpiredError{API: api, Ret: append([]string(nil), ret...)}
}

// RiskVerificationError 表示闲鱼服务端要求用户验证（滑块/人脸/风控页）。
// 这类错误不能按普通 token 过期快速重试，否则会持续打接口并放大风控。
// RiskVerificationError 用于本次流程后续判断的RiskVerification错误
type RiskVerificationError struct {
	Ret             []string
	VerificationURL string
}

// Error 封装错误业务协调。
func (e *RiskVerificationError) Error() string {
	if e == nil {
		return ""
	}
	if e.VerificationURL != "" {
		return fmt.Sprintf("闲鱼要求安全验证: ret=%v url=%s", e.Ret, e.VerificationURL)
	}
	return fmt.Sprintf("闲鱼要求安全验证: ret=%v", e.Ret)
}

// IsRiskVerificationErr 判断错误是否是闲鱼风控验证要求。
func IsRiskVerificationErr(err error) bool {
	// riskErr 用于本次流程后续判断的riskErr
	var riskErr *RiskVerificationError
	if errors.As(err, &riskErr) {
		return true
	}
	if err == nil {
		return false
	}
	// msg 用于本次流程后续判断的msg
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fail_sys_user_validate") ||
		strings.Contains(msg, "rgv587") ||
		strings.Contains(msg, "punish") ||
		strings.Contains(msg, "captcha") ||
		strings.Contains(msg, "x5secdata")
}

// isRiskVerificationRet 封装isRiskVerificationRet业务协调。
func isRiskVerificationRet(ret []string) bool {
	// r 表示当前遍历过程中的r
	for _, r := range ret {
		// lower 用于本次流程后续判断的lower
		lower := strings.ToLower(r)
		if strings.Contains(lower, "fail_sys_user_validate") ||
			strings.Contains(lower, "rgv587") ||
			strings.Contains(lower, "punish") ||
			strings.Contains(lower, "captcha") ||
			strings.Contains(lower, "x5secdata") {
			return true
		}
	}
	return false
}

// IsSessionExpiredErr 判断错误是否表示 cookie/session 已彻底失效（需密码登录刷新）。
func IsSessionExpiredErr(err error) bool {
	if err == nil {
		return false
	}
	// sessionErr 用于本次流程后续判断的会话Err
	var sessionErr *SessionExpiredError
	if errors.As(err, &sessionErr) {
		return true
	}
	// msg 用于本次流程后续判断的msg
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fail_sys_session_expired") ||
		strings.Contains(msg, "session过期") ||
		strings.Contains(msg, "session expired") ||
		strings.Contains(msg, "会话过期") ||
		strings.Contains(msg, "登录凭证已失效")
}

// mtopString 封装mtopString业务协调。
func mtopString(v any) string {
	switch // x 用于本次流程后续判断的x
	x := v.(type) {
	case string:
		return x
	case float64:
		return strconv.FormatInt(int64(x), 10)
	case int:
		return strconv.Itoa(x)
	case json.Number:
		return x.String()
	default:
		return ""
	}
}

// mtopInt 封装mtopInt业务协调。
func mtopInt(v any) int {
	switch // x 用于本次流程后续判断的x
	x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		// n 用于本次流程后续判断的n
		n, _ := strconv.Atoi(x)
		return n
	case json.Number:
		// n 用于本次流程后续判断的n
		n, _ := strconv.Atoi(x.String())
		return n
	default:
		return 0
	}
}

// setCommonHeaders 封装setCommonHeaders业务协调。
func setCommonHeaders(req *http.Request, cookiesStr string) {
	// h 用于本次流程后续判断的h
	h := req.Header
	h.Set("accept", "application/json")
	h.Set("accept-language", "zh-CN,zh;q=0.9,en;q=0.8")
	h.Set("cache-control", "no-cache")
	h.Set("content-type", "application/x-www-form-urlencoded")
	h.Set("pragma", "no-cache")
	h.Set("priority", "u=1, i")
	xianyu.ApplyBrowserFingerprint(h)
	h.Set("sec-fetch-dest", "empty")
	h.Set("sec-fetch-mode", "cors")
	h.Set("sec-fetch-site", "same-site")
	h.Set("referer", "https://www.goofish.com/")
	h.Set("origin", "https://www.goofish.com")
	h.Set("cookie", cookiesStr)
}

// readMTopBody 封装readMTop请求体业务协调。
func readMTopBody(resp *http.Response) ([]byte, error) {
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := io.ReadAll(io.LimitReader(resp.Body, maxMTopResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(raw) > maxMTopResponseBytes {
		return nil, fmt.Errorf("mtop 响应体超过 %d MiB", maxMTopResponseBytes>>20)
	}
	return raw, nil
}

// mergeSetCookie 把响应的 Set-Cookie 合并回 cookie 字符串。
func mergeSetCookie(orig string, current map[string]string, resp *http.Response) string {
	// setCookies 用于本次流程后续判断的setCookies
	setCookies := resp.Header["Set-Cookie"]
	if len(setCookies) == 0 {
		return orig
	}
	// changed 用于本次流程后续判断的changed
	changed := false
	// sc 表示当前遍历过程中的sc
	for _, sc := range setCookies {
		// parsed、err 用于本次流程后续判断的parsed、err
		parsed, err := http.ParseSetCookie(sc)
		if err != nil || strings.TrimSpace(parsed.Name) == "" {
			continue
		}
		if parsed.MaxAge < 0 || (parsed.MaxAge == 0 && !parsed.Expires.IsZero() && !parsed.Expires.After(time.Now())) {
			delete(current, parsed.Name)
		} else {
			current[parsed.Name] = parsed.Value
		}
		changed = true
	}
	if !changed {
		return orig
	}
	// b 用于本次流程后续判断的b
	var b strings.Builder
	// first 用于本次流程后续判断的first
	first := true
	// k、v 表示当前遍历过程中的k、v
	for k, v := range current {
		if !first {
			b.WriteString("; ")
		}
		first = false
		b.WriteString(k)
		b.WriteByte('=')
		b.WriteString(v)
	}
	return b.String()
}

// truncate 封装truncate业务协调。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// sleepCtx 封装sleepCtx业务协调。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	// t 用于本次流程后续判断的t
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
