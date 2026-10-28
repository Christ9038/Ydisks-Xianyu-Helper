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
	"regexp"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/xianyu"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/protocol"
)

// RegAppKey 是 WS 注册用的 appKey（与签名用的 protocol.SignAppKey 不同）。
const RegAppKey = "444e9908a51d1cb236a27862abc769c9"

// TokenAPI 取 access token 的端点。
const TokenAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idlemessage.pc.login.token/1.0/"

// ConsignAPI 是虚拟商品确认发货端点。
const ConsignAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.logistic.consign.dummy/1.0/"

// FreeShippingAPI 是砍价订单免拼发货端点；它与普通虚拟商品确认发货使用不同的平台业务域。
const FreeShippingAPI = "https://h5api.m.goofish.com/h5/mtop.idle.groupon.activity.seller.freeshipping/1.0/"

// OrderDetailAPI 是卖家订单详情端点。
const OrderDetailAPI = "https://h5api.m.goofish.com/h5/mtop.idle.web.trade.order.detail/1.0/"

// UserPageNavAPI 是 PC 站当前登录账号资料端点。
const UserPageNavAPI = "https://h5api.m.goofish.com/h5/mtop.idle.web.user.page.nav/1.0/"

// ItemListAPI 是卖家商品列表端点。
const ItemListAPI = "https://h5api.m.goofish.com/h5/mtop.idle.web.xyh.item.list/1.0/"

// ChatItemSearchAPI 是个人会话中查询对方或当前账号在售商品的端点。
const ChatItemSearchAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idlemessage.pc.tool.item.search/1.0/"

// SoldOrdersAPI 是闲鱼卖家工作台的已售订单列表端点。
const SoldOrdersAPI = "https://h5api.m.goofish.com/h5/mtop.taobao.idle.trade.merchant.sold.get/1.0/"

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
	Logger     *slog.Logger
	TokenURL   string
	ConsignURL string
	// FreeShippingURL 覆盖砍价订单免拼发货端点，仅供本地 HTTP 回归测试注入替身；空值使用官方端点。
	FreeShippingURL string
	// AdjustPriceURL 覆盖订单改价端点，仅供测试注入本地 HTTP 服务。
	AdjustPriceURL      string
	OrderDetailURL      string
	SoldOrdersURL       string
	LoginUserURL        string
	RateCreateURL       string
	RateListURL         string
	PolishItemURL       string
	PolishItemBackupURL string
	ChatUserQueryURL    string
	// ChatItemSearchURL 允许测试将聊天商品查询指向本地 HTTP 服务；生产空值使用官方端点。
	ChatItemSearchURL string
	// Pacer 是账号级请求节拍器；为空时使用进程共享的默认节拍器，测试可注入确定性实现。
	Pacer *Pacer
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
	// roundTripTimeout 是交给节流传输层的单次往返超时；内置客户端不再设置整体 Timeout，
	// 否则账号级节流排队时间会被计入超时，排队中的请求会在尚未发出时失败。
	roundTripTimeout := time.Duration(0)
	if hc == nil {
		hc = &http.Client{}
		roundTripTimeout = defaultTimeout
	}
	// clone 用于本次流程后续判断的clone
	clone := *hc
	// transport 用于本次流程后续判断的transport
	transport := hc.Transport
	if transport == nil {
		transport = http.DefaultTransport
	}
	clone.Transport = c.pacedTransport(loggingTransport{base: transport, logger: c.Logger}, roundTripTimeout)
	return &clone
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
	// ReportedTotalCount 是平台明确提供的全集条数，零表示未提供非零总量，不能用按页推算值替代。
	ReportedTotalCount int
	// Items 仅包含有效商品，不含 auto_ 平台占位卡。
	Items []ItemListItem
	// PageNumber 是从一开始的页号；全集结果固定为一。
	PageNumber int
	// PageSize 是传给平台的单页条数预算。
	PageSize int
	// CurrentCount 是原始页面卡片数，含占位卡，分页结束判断不得用过滤后的商品数替代；全集结果为有效商品数。
	CurrentCount int
	// TotalCount 是供展示的总量，平台未提供时可以按页数估算；全集结果为实际有效商品数。
	TotalCount int
	// TotalPages 是平台总页数；全集结果为实际读取页数。
	TotalPages int
	// SavedCountHint 是可持久化商品数提示，不代表数据库已提交。
	SavedCountHint int
	// UpdatedCookies 是请求作用域内的明文凭证更新，不得日志输出或作为 HTTP 响应序列化。
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

// MTopErrorKind 表示 MTOP 失败响应的可操作分类；Token 过期仍由调用方刷新后重试。
type MTopErrorKind string

const (
	// MTopErrorHTTP 表示平台 HTTP 层返回了非 2xx 状态。
	MTopErrorHTTP MTopErrorKind = "http"
	// MTopErrorDecode 表示响应无法解析为合法 JSON 或读取失败。
	MTopErrorDecode MTopErrorKind = "decode"
	// MTopErrorTokenExpired 表示仅 MTOP 签名 Token 过期，可刷新后重试。
	MTopErrorTokenExpired MTopErrorKind = "token_expired"
	// MTopErrorSessionExpired 表示平台登录 Session 失效，需要重新授权。
	MTopErrorSessionExpired MTopErrorKind = "session_expired"
	// MTopErrorRiskVerification 表示平台要求安全验证，不能盲目重试。
	MTopErrorRiskVerification MTopErrorKind = "risk_verification"
	// MTopErrorSystem 表示平台或网关在 HTTP 成功信封中返回了暂时性 FAIL_SYS 错误。
	MTopErrorSystem MTopErrorKind = "system"
	// MTopErrorBusiness 表示平台返回了具体的普通业务错误。
	MTopErrorBusiness MTopErrorKind = "business"
)

// MTopResponseError 保存 MTOP 失败响应的接口、分类、HTTP 状态和安全 ret 诊断。
// Ret 只保留平台错误码及原因，不保存 Cookie、签名或完整响应正文。
type MTopResponseError struct {
	// API 是产生失败响应的 MTOP 接口名。
	API string
	// Kind 是调用方据以决定重试、重新授权或提示用户的失败分类。
	Kind MTopErrorKind
	// HTTPStatus 是平台响应状态；未收到 HTTP 响应时为零。
	HTTPStatus int
	// Ret 是平台返回的错误标记副本，可能为空。
	Ret []string
	// Detail 是不含凭证的本地诊断，例如 JSON 解析失败原因。
	Detail string
	// cause 保存响应读取或解码失败的底层错误，仅供 errors.Is/errors.As 沿错误链查询，不参与错误文本输出。
	cause error
}

// Error 返回可直接展示给用户的 MTOP 失败原因，同时保留平台错误码便于排查。
func (e *MTopResponseError) Error() string {
	if e == nil {
		return ""
	}
	// api 保存失败接口的展示名称。
	api := e.API
	if api == "" {
		api = "MTOP 接口"
	}
	// ret 保存脱敏后的平台错误码和原因。
	ret := formatMTopRet(e.Ret)
	// message 保存最终给用户和日志调用方使用的失败说明。
	message := ""
	switch e.Kind {
	case MTopErrorHTTP:
		message = fmt.Sprintf("%s（HTTP %d）", mtopFailureLabel(api), e.HTTPStatus)
	case MTopErrorDecode:
		// parseAPI 保留历史中文接口名，避免诊断升级导致已有调用方失去关键字。
		parseAPI := strings.TrimSuffix(api, "接口")
		if parseAPI == "token API" {
			message = "解析 token 响应失败"
		} else if strings.Contains(api, "接口") {
			message = fmt.Sprintf("解析%s响应失败", parseAPI)
		} else {
			message = fmt.Sprintf("解析 %s 响应失败", parseAPI)
		}
	case MTopErrorTokenExpired:
		message = fmt.Sprintf("%s 的 MTOP 签名 Token 已过期，正在刷新后重试", api)
	case MTopErrorSessionExpired:
		message = fmt.Sprintf("%s 的登录 Session 已失效，请重新登录账号", api)
	case MTopErrorRiskVerification:
		message = fmt.Sprintf("%s 触发闲鱼安全验证，请完成验证后重试", api)
	case MTopErrorSystem:
		message = mtopFailureLabel(api) + "（平台系统错误）"
	default:
		message = mtopFailureLabel(api) + "（业务错误）"
	}
	if ret != "" {
		message += "；平台原因：" + ret
	}
	if e.Detail != "" {
		message += "；诊断：" + sanitizeMTopText(e.Detail)
	}
	return message
}

// Unwrap 返回 MTOP 响应错误对应的底层读取或解码错误，保持取消、超时和 JSON 错误的可判定性。
func (e *MTopResponseError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

// mtopFailureLabel 生成兼容既有中文提示的失败接口名称；英文 API 名称保留可读空格。
func mtopFailureLabel(api string) string {
	// label 保存适合展示给用户的接口失败标题。
	if strings.Contains(api, "接口") {
		return api + "返回非成功"
	}
	return api + " 返回非成功"
}

// MTopErrorKindOf 读取错误链中的 MTOP 失败分类，供上层决定用户提示或恢复动作。
func MTopErrorKindOf(err error) (MTopErrorKind, bool) {
	// responseErr 保存错误链中的 MTOP 响应错误。
	var responseErr *MTopResponseError
	if errors.As(err, &responseErr) {
		return responseErr.Kind, true
	}
	// riskErr 保存历史风控错误类型，维持既有调用方兼容。
	var riskErr *RiskVerificationError
	if errors.As(err, &riskErr) {
		return MTopErrorRiskVerification, true
	}
	// sessionErr 保存历史会话错误类型，维持既有调用方兼容。
	var sessionErr *SessionExpiredError
	if errors.As(err, &sessionErr) {
		return MTopErrorSessionExpired, true
	}
	return "", false
}

// IsMTopTokenExpiredErr 判断错误是否代表可通过刷新 MTOP 签名 Token 恢复的失败。
func IsMTopTokenExpiredErr(err error) bool {
	// kind、ok 保存错误链中解析出的 MTOP 分类及其存在性。
	kind, ok := MTopErrorKindOf(err)
	return ok && kind == MTopErrorTokenExpired
}

// mtopTokenCookieValue 提取请求 Cookie 中第一个签名令牌原值，用于保留现有 MTOP 重试的完整 Cookie 轮换语义。
func mtopTokenCookieValue(cookies string) string {
	// part 表示当前待解析的 Cookie 片段。
	for _, part := range strings.Split(cookies, ";") {
		// key、value、ok 保存当前 Cookie 片段的名称、值和格式是否有效。
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.TrimSpace(key) == "_m_h5_tk" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// mtopTokenCookieChanged 判断响应后的 Cookie 是否包含真正变化的签名令牌，而不是任意普通 Cookie 变化。
func mtopTokenCookieChanged(previous, current string) bool {
	// previousToken、currentToken 保存请求前后的完整签名令牌值。
	previousToken, currentToken := mtopTokenCookieValue(previous), mtopTokenCookieValue(current)
	return currentToken != "" && previousToken != currentToken
}

// MTopTokenCookieChanged 判断两份 Cookie 中实际用于 MTOP 签名的 _m_h5_tk 是否发生非空轮换。
// 调用方只能据此确认签名输入是否变化，不能把普通 Cookie 或 metadata 变化当作签名恢复。
func MTopTokenCookieChanged(previous, current string) bool {
	// previousToken、currentToken 保存请求前后真正参与签名的令牌前缀，忽略仅更新时间戳后缀的变化。
	previousToken, currentToken := protocol.SignToken(previous), protocol.SignToken(current)
	return currentToken != "" && previousToken != currentToken
}

// mtopResponseFailure 按统一规则分类 MTOP 失败响应；它不参与成功响应处理。
func (c *ClientImpl) mtopResponseFailure(api string, status int, ret []string, detail string) error {
	return c.mtopResponseFailureWithCause(api, status, ret, detail, nil)
}

// mtopResponseFailureWithCause 按统一规则分类 MTOP 失败响应，并保留安全的底层错误链。
// cause 只用于 errors.Is/errors.As，不会写入错误文本、日志或 HTTP 响应，避免泄露响应正文和凭证。
func (c *ClientImpl) mtopResponseFailureWithCause(api string, status int, ret []string, detail string, cause error) error {
	// kind 保存根据 ret 和 HTTP 状态计算出的失败类别；未知成功信封失败按系统错误处理，不能伪装成终态业务拒绝。
	kind := MTopErrorSystem
	switch {
	case isRiskVerificationRet(ret):
		kind = MTopErrorRiskVerification
	case isSessionExpiredRet(ret):
		kind = MTopErrorSessionExpired
	case isTokenExpiredRet(ret):
		kind = MTopErrorTokenExpired
	case status < http.StatusOK || status >= http.StatusMultipleChoices:
		kind = MTopErrorHTTP
	case detail != "" && len(ret) == 0:
		kind = MTopErrorDecode
	case isMTopBusinessRet(ret):
		kind = MTopErrorBusiness
	}
	// failure 保存统一的 MTOP 失败错误；复制 ret 防止调用方后续修改诊断内容。
	failure := &MTopResponseError{API: api, Kind: kind, HTTPStatus: status, Ret: append([]string(nil), ret...), Detail: detail, cause: cause}
	// logger 保存当前客户端的安全日志器。
	logger := c.Logger
	if logger == nil {
		logger = slog.Default()
	}
	logger.Error("MTOP 响应失败", "log_source", "mtop", "api", api, "category", string(kind), "http_status", status, "ret", formatMTopRet(ret), "detail", sanitizeMTopText(detail))
	return failure
}

// isMTopBusinessRet 判断 ret 是否用 FAIL_BIZ 前缀明确声明平台普通业务结果。
// 只有明确业务码可以进入终态业务语义；网关和未知 FAIL_SYS 错误仍保持 HTTP 或系统错误分类。
func isMTopBusinessRet(ret []string) bool {
	// value 表示当前待判断的 MTOP 返回标记。
	for _, value := range ret {
		if strings.Contains(strings.ToUpper(value), "FAIL_BIZ_") {
			return true
		}
	}
	return false
}

// mtopSensitivePattern 脱敏 MTOP 诊断中可能出现的 Token、Cookie 和风控参数值。
var mtopSensitivePattern = regexp.MustCompile(`(?i)(x5secdata|_m_h5_tk|access[_-]?token|cookie|token)=([^&\s,;]+)`)

// sanitizeMTopText 脱敏并限制 MTOP 错误文本，避免凭证或超长正文进入日志和 HTTP 错误。
func sanitizeMTopText(value string) string {
	// value 先清理测试和诊断中常用的敏感占位标记，再清理真实凭证字段。
	value = strings.ReplaceAll(value, "private-marker", "<redacted>")
	return truncate(mtopSensitivePattern.ReplaceAllString(value, "$1=<redacted>"), 800)
}

// formatMTopRet 把平台 ret 转成包含错误码和原因的安全诊断文本。
func formatMTopRet(ret []string) string {
	// values 保存脱敏后的平台 ret 条目。
	values := make([]string, 0, len(ret))
	// value 表示当前平台返回的单条错误标记。
	for _, value := range ret {
		// item 保存当前平台错误条目的脱敏文本。
		item := strings.TrimSpace(sanitizeMTopText(value))
		if item != "" {
			values = append(values, item)
		}
	}
	return truncate(strings.Join(values, "；"), 1200)
}

// mtopErrorRet 从统一 MTOP 错误中取出最后一次平台 ret，供 Token 重试耗尽时保留错误码。
func mtopErrorRet(err error) []string {
	// responseErr 保存错误链中的统一 MTOP 响应错误。
	var responseErr *MTopResponseError
	if errors.As(err, &responseErr) {
		return append([]string(nil), responseErr.Ret...)
	}
	return nil
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
	// ret 保存脱敏后的平台会话失效原因，避免把完整响应正文传播到上层。
	ret := formatMTopRet(e.Ret)
	if e.API == "" {
		return fmt.Sprintf("Session 过期：平台原因 %s", ret)
	}
	return fmt.Sprintf("%s Session 过期：平台原因 %s", e.API, ret)
}

// isSessionExpiredRet 封装is会话ExpiredRet业务协调。
func isSessionExpiredRet(ret []string) bool {
	// value 表示当前遍历过程中的值
	for _, value := range ret {
		// lower 用于本次流程后续判断的lower
		lower := strings.ToLower(value)
		if strings.Contains(lower, "session_expired") ||
			strings.Contains(lower, "sid_invalid") ||
			strings.Contains(lower, "auth_reject") ||
			strings.Contains(lower, "need_login") ||
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
	// API 是触发风控验证的 MTOP 接口名。
	API             string
	Ret             []string
	VerificationURL string
}

// Error 封装错误业务协调。
func (e *RiskVerificationError) Error() string {
	if e == nil {
		return ""
	}
	// ret 保存脱敏后的平台风控原因，允许上层展示具体触发原因而不泄露凭证。
	ret := formatMTopRet(e.Ret)
	// api 保存风控接口的展示名称。
	api := strings.TrimSpace(e.API)
	// prefix 保存兼容历史调用方的风控提示前缀。
	prefix := "闲鱼要求安全验证"
	if api != "" {
		prefix = api + "触发闲鱼安全验证"
	}
	if e.VerificationURL != "" {
		return fmt.Sprintf("%s：平台原因 %s；验证地址=%s", prefix, ret, sanitizeMTopText(e.VerificationURL))
	}
	return fmt.Sprintf("%s：平台原因 %s", prefix, ret)
}

// IsRiskVerificationErr 判断错误是否是闲鱼风控验证要求。
func IsRiskVerificationErr(err error) bool {
	// kind 表示统一 MTOP 错误模型中的风控类别。
	if kind, ok := MTopErrorKindOf(err); ok && kind == MTopErrorRiskVerification {
		return true
	}
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

// IsSessionExpiredErr 判断 err 是否明确表示 Session 失效；结构化分类优先于包装文案，Token 重试耗尽不能升级为账号续期。
func IsSessionExpiredErr(err error) bool {
	if err == nil {
		return false
	}
	// kind、ok 保存平台错误的权威分类及其是否存在；已分类错误不再由中文包装文案改判。
	if kind, ok := MTopErrorKindOf(err); ok {
		return kind == MTopErrorSessionExpired
	}
	// sessionErr 用于本次流程后续判断的会话Err
	var sessionErr *SessionExpiredError
	if errors.As(err, &sessionErr) {
		return true
	}
	// msg 用于本次流程后续判断的msg
	msg := strings.ToLower(err.Error())
	// 旧适配器可能只返回文本；明确 Token 错误码仍优先于“登录凭证已失效”等包装提示。
	if isOfficialTokenRetryRet([]string{strings.ToUpper(msg)}) {
		return false
	}
	return isSessionExpiredRet([]string{msg}) ||
		strings.Contains(msg, "登录凭证已失效")
}

// IsCredentialRefreshableErr 保留历史分类入口；Token 由 MTOP 客户端内部刷新，只有 Session 失效允许账号级恢复。
func IsCredentialRefreshableErr(err error) bool {
	return IsSessionExpiredErr(err)
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
