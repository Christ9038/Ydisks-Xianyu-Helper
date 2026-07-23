// Package mtop 实现闲鱼 mtop H5 API 客户端。
// 关键：签名只覆盖 (t, token, data_val)，与 URL query 参数无关；token 取自 cookie _m_h5_tk 前半段。
package mtop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
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

const (
	MTopRetryGap         = time.Second
	ItemPageGap          = time.Second
	maxMTopResponseBytes = 8 << 20
)

// Client 是 mtop API 的最小契约，供 server/automation/engine 依赖注入与测试 mock。
// 具体实现 *ClientImpl 走 HTTP；测试可注入自定义实现以隔离网络。
type Client interface {
	FetchUserProfile(ctx context.Context, cookiesStr string) (*UserProfileResult, error)
	ConsignContext(ctx context.Context, cookiesStr, orderID string) (ok bool, ret []string, updatedCookies string, err error)
	FetchItemsPage(ctx context.Context, cookiesStr string, pageNumber, pageSize int) (*ItemListResult, error)
	FetchAllItems(ctx context.Context, cookiesStr string, pageSize, maxPages int) (*ItemListResult, error)
	PublishItem(ctx context.Context, cookiesStr string, req PublishItemRequest) (*PublishItemResult, error)
	RefreshTokenWithDeviceIDContext(ctx context.Context, cookiesStr, deviceID string) (*RefreshResult, error)
}

// ClientImpl 是 Client 接口的 HTTP 实现。零值可用；HTTP 超时默认 30s。
// 仍导出 HTTPClient/TokenURL 等字段以便调用方覆盖（如测试注入 RoundTripper）。
type ClientImpl struct {
	HTTPClient     *http.Client
	TokenURL       string
	ConsignURL     string
	OrderDetailURL string
	SoldOrdersURL  string
	ItemDetailURL  string
	LoginUserURL   string
}

// NewClient 构造纯 Go HTTP 的 MTOP 客户端。Chromium 只用于读取本机指纹
// 和处理滑块，不能成为登录、续期、token 或 WebSocket 的传输层。
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

func hasMTopSuccess(ret []string) bool {
	for _, r := range ret {
		if strings.Contains(r, "SUCCESS::调用成功") {
			return true
		}
	}
	return false
}

func isTokenExpiredRet(ret []string) bool {
	for _, r := range ret {
		if strings.Contains(r, "FAIL_SYS_TOKEN_EXOIRED") ||
			strings.Contains(r, "FAIL_SYS_TOKEN_EXPIRED") ||
			strings.Contains(r, "FAIL_SYS_TOKEN_EMPTY") ||
			strings.Contains(r, "FAIL_SYS_SESSION_EXPIRED") {
			return true
		}
	}
	return false
}

// RiskVerificationError 表示闲鱼服务端要求用户验证（滑块/人脸/风控页）。
// 这类错误不能按普通 token 过期快速重试，否则会持续打接口并放大风控。
type RiskVerificationError struct {
	Ret             []string
	VerificationURL string
}

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
	var riskErr *RiskVerificationError
	if errors.As(err, &riskErr) {
		return true
	}
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fail_sys_user_validate") ||
		strings.Contains(msg, "rgv587") ||
		strings.Contains(msg, "punish") ||
		strings.Contains(msg, "captcha") ||
		strings.Contains(msg, "x5secdata")
}

func isRiskVerificationRet(ret []string) bool {
	for _, r := range ret {
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
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "fail_sys_session_expired") ||
		strings.Contains(msg, "session过期") ||
		strings.Contains(msg, "登录凭证已失效")
}

func mtopString(v any) string {
	switch x := v.(type) {
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

func mtopInt(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(x)
		return n
	case json.Number:
		n, _ := strconv.Atoi(x.String())
		return n
	default:
		return 0
	}
}

func setCommonHeaders(req *http.Request, cookiesStr string) {
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

func readMTopBody(resp *http.Response) ([]byte, error) {
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
	setCookies := resp.Header["Set-Cookie"]
	if len(setCookies) == 0 {
		return orig
	}
	changed := false
	for _, sc := range setCookies {
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
	var b strings.Builder
	first := true
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

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
