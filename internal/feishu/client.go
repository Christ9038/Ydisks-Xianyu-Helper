// Package feishu 实现飞书/Lark 个人应用扫码注册、租户令牌获取与文本消息发送。
// 该包只负责平台 HTTP 协议与协议错误分类，不接触业务数据库、通知渠道存储或 HTTP transport。
package feishu

import (
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"xianyu-go/internal/logsafe"
)

const (
	// DomainFeishu 表示飞书中国站域名，对应 accounts.feishu.cn 与 open.feishu.cn。
	DomainFeishu = "feishu"
	// DomainLark 表示 Lark 国际站域名，对应 accounts.larksuite.com 与 open.larksuite.com。
	DomainLark = "lark"
	// registrationPath 是飞书设备码扫码注册的统一路径，begin 与 poll 共用。
	registrationPath = "/oauth/v1/app/registration"
	// defaultAccountsBaseURL 是飞书中国站账号服务源站。
	defaultAccountsBaseURL = "https://accounts.feishu.cn"
	// defaultLarkAccountsBaseURL 是 Lark 国际站账号服务源站。
	defaultLarkAccountsBaseURL = "https://accounts.larksuite.com"
	// defaultOpenBaseURL 是飞书中国站开放平台源站。
	defaultOpenBaseURL = "https://open.feishu.cn"
	// defaultLarkOpenBaseURL 是 Lark 国际站开放平台源站。
	defaultLarkOpenBaseURL = "https://open.larksuite.com"
	// maxResponseBytes 限制单个平台响应体读取量，避免异常响应耗尽内存。
	maxResponseBytes = 1 << 20
	// defaultClientTimeout 是调用方未注入 HTTP 客户端时使用的有界请求超时。
	defaultClientTimeout = 15 * time.Second
	// defaultDeviceCodeExpiresIn 是平台未返回有效期时的设备码兜底有效期。
	defaultDeviceCodeExpiresIn = 5 * time.Minute
	// defaultDevicePollInterval 是平台未返回轮询间隔时的兜底间隔。
	defaultDevicePollInterval = 5 * time.Second
)

// DeviceCode 是扫码注册 begin 阶段的非敏感结果，只保存设备码、扫码地址和时间约束。
type DeviceCode struct {
	// DeviceCode 是平台颁发的设备码，用于后续 poll；它属于短期授权材料，禁止写入普通日志。
	DeviceCode string
	// VerificationURIComplete 是已经追加 SDK 查询参数的完整扫码地址，可直接编码为二维码。
	VerificationURIComplete string
	// ExpiresIn 是设备码有效期，零值表示调用方应使用默认有效期。
	ExpiresIn time.Duration
	// Interval 是平台建议的最小轮询间隔，零值表示调用方应使用默认间隔。
	Interval time.Duration
}

// PollStatus 是设备码 poll 阶段的协议状态，避免调用方解析平台错误字符串。
type PollStatus string

const (
	// PollStatusPending 表示用户尚未完成扫码确认，调用方应稍后再次 poll。
	PollStatusPending PollStatus = "pending"
	// PollStatusSlowDown 表示平台要求降低轮询频率，调用方应增大下一次轮询间隔。
	PollStatusSlowDown PollStatus = "slow_down"
	// PollStatusSuccess 表示扫码授权成功，结果携带应用凭据与用户 open_id。
	PollStatusSuccess PollStatus = "success"
	// PollStatusDenied 表示用户明确拒绝授权，本次绑定应终止。
	PollStatusDenied PollStatus = "denied"
	// PollStatusExpired 表示设备码已失效，本次绑定应终止并要求用户重新扫码。
	PollStatusExpired PollStatus = "expired"
	// PollStatusError 表示平台返回了无法归类或非法的授权结果，本次绑定应终止。
	PollStatusError PollStatus = "error"
)

// PollRequest 是一次设备码轮询所需的最小输入。
type PollRequest struct {
	// Domain 是当前设备码所属站点域名；空值按飞书中国站处理。
	Domain string
	// DeviceCode 是 begin 阶段返回的设备码，仅在本次轮询请求内使用。
	DeviceCode string
}

// PollResult 是设备码轮询的分类结果；只有成功状态才包含应用凭据。
type PollResult struct {
	// Status 是平台授权阶段分类。
	Status PollStatus
	// Domain 是轮询后生效的站点域名；识别到 Lark 租户后为 lark，否则沿用请求域名。
	Domain string
	// AppID 是扫码创建的个人应用标识，仅成功状态非空。
	AppID string
	// AppSecret 是扫码创建的个人应用密钥，仅成功状态非空；明文只允许进入加密存储边界。
	AppSecret string
	// OpenID 是授权用户的飞书 open_id，仅成功状态非空。
	OpenID string
	// Message 是可直接展示给用户的稳定中文说明，不包含平台原始响应或凭据。
	Message string
	// Interval 是 slow_down 状态要求的新轮询间隔；其他状态为零值。
	Interval time.Duration
}

// Client 执行飞书账号服务与开放平台 HTTP 调用，可安全并发使用；HTTP 客户端和令牌缓存由构造期注入。
type Client struct {
	// HTTPClient 是执行平台请求的客户端；空值在构造时替换为带超时的默认客户端。
	HTTPClient *http.Client
	// accountsBaseURL 是飞书中国站账号服务源站，测试可替换为本地 httptest 地址。
	accountsBaseURL string
	// larkAccountsBaseURL 是 Lark 国际站账号服务源站，测试可替换为本地 httptest 地址。
	larkAccountsBaseURL string
	// openBaseURL 是飞书中国站开放平台源站，测试可替换为本地 httptest 地址。
	openBaseURL string
	// larkOpenBaseURL 是 Lark 国际站开放平台源站，测试可替换为本地 httptest 地址。
	larkOpenBaseURL string
	// tokens 保存进程内租户访问令牌缓存；字段本身线程安全。
	tokens *tokenCache
}

// NewClient 使用给定 HTTP 客户端构造飞书协议客户端；nil 客户端会得到有界超时的默认实现。
// 返回值使用进程共享的令牌缓存，使不同通知发送复用同一租户令牌。
func NewClient(httpClient *http.Client) *Client {
	if httpClient == nil {
		httpClient = &http.Client{Timeout: defaultClientTimeout}
	}
	return &Client{
		HTTPClient:          httpClient,
		accountsBaseURL:     defaultAccountsBaseURL,
		larkAccountsBaseURL: defaultLarkAccountsBaseURL,
		openBaseURL:         defaultOpenBaseURL,
		larkOpenBaseURL:     defaultLarkOpenBaseURL,
		tokens:              sharedTokenCache,
	}
}

// BeginRegistration 发起设备码扫码注册，返回带 SDK 查询参数的扫码地址与轮询参数。
// ctx 控制单次平台请求生命周期；平台业务错误会转换为不含凭据的诊断错误。
func (c *Client) BeginRegistration(ctx context.Context) (DeviceCode, error) {
	// beginForm 是 begin 阶段要求的表单参数，字段名与飞书设备码协议一致。
	beginForm := url.Values{}
	beginForm.Set("action", "begin")
	beginForm.Set("archetype", "PersonalAgent")
	beginForm.Set("auth_method", "client_secret")
	beginForm.Set("request_user_info", "open_id")
	// responseBody、requestErr 保存 begin 响应原始字节和协议或传输错误。
	responseBody, requestErr := c.postForm(ctx, c.accountsBaseURL+registrationPath, beginForm)
	if requestErr != nil {
		return DeviceCode{}, requestErr
	}
	// beginResult 保存 begin 响应的协议字段。
	var beginResult beginRegistrationResponse
	// decodeErr 是 begin 响应 JSON 解析失败原因；nil 表示响应格式符合协议。
	if decodeErr := json.Unmarshal(responseBody, &beginResult); decodeErr != nil {
		return DeviceCode{}, fmt.Errorf("解析飞书扫码注册响应失败")
	}
	if strings.TrimSpace(beginResult.Error) != "" {
		return DeviceCode{}, fmt.Errorf("飞书扫码注册失败: %s", sanitizePlatformMessage(beginResult.ErrorDescription, beginResult.Error))
	}
	if strings.TrimSpace(beginResult.DeviceCode) == "" || strings.TrimSpace(beginResult.VerificationURIComplete) == "" {
		return DeviceCode{}, fmt.Errorf("飞书扫码注册响应缺少设备码或扫码地址")
	}
	// qrURL、addonsErr 保存追加 SDK 参数后的扫码地址及追加失败原因。
	qrURL, addonsErr := addSDKQuery(beginResult.VerificationURIComplete)
	if addonsErr != nil {
		return DeviceCode{}, addonsErr
	}
	// expiresIn 是设备码有效期。
	expiresIn := time.Duration(beginResult.ExpiresIn) * time.Second
	// interval 是平台建议轮询间隔。
	interval := time.Duration(beginResult.Interval) * time.Second
	if expiresIn <= 0 {
		expiresIn = defaultDeviceCodeExpiresIn
	}
	if interval <= 0 {
		interval = defaultDevicePollInterval
	}
	return DeviceCode{
		DeviceCode:              beginResult.DeviceCode,
		VerificationURIComplete: qrURL,
		ExpiresIn:               expiresIn,
		Interval:                interval,
	}, nil
}

// Poll 执行一次设备码轮询，并把平台错误字符串归一为 PollStatus。
// ctx 控制单次平台请求；req.Domain 决定账号服务源站，识别到 Lark 租户后结果会返回 lark 域名。
func (c *Client) Poll(ctx context.Context, req PollRequest) (PollResult, error) {
	// domain 是把空值和未知值归一后的可请求站点域名。
	domain, domainErr := resolveDomain(req.Domain)
	if domainErr != nil {
		return PollResult{}, domainErr
	}
	if strings.TrimSpace(req.DeviceCode) == "" {
		return PollResult{}, fmt.Errorf("飞书设备码不能为空")
	}
	// pollForm 是 poll 阶段要求的表单参数。
	pollForm := url.Values{}
	pollForm.Set("action", "poll")
	pollForm.Set("device_code", req.DeviceCode)
	// responseBody、requestErr 保存 poll 响应原始字节和协议或传输错误。
	responseBody, requestErr := c.postForm(ctx, c.accountsBaseURLFor(domain)+registrationPath, pollForm)
	if requestErr != nil {
		return PollResult{}, requestErr
	}
	// pollResponse 保存 poll 响应的协议字段。
	var pollResponse pollRegistrationResponse
	// decodeErr 是 poll 响应 JSON 解析失败原因；nil 表示响应格式符合协议。
	if decodeErr := json.Unmarshal(responseBody, &pollResponse); decodeErr != nil {
		return PollResult{}, fmt.Errorf("解析飞书扫码轮询响应失败")
	}
	// platformCode 是归一化后的平台错误码，用于分类 pending、slow_down、拒绝和过期。
	platformCode := strings.ToLower(strings.TrimSpace(pollResponse.Error))
	switch platformCode {
	case "":
		// 成功响应必须同时包含应用凭据和 open_id，缺少任一字段都视为非法响应。
		if strings.TrimSpace(pollResponse.ClientID) == "" || strings.TrimSpace(pollResponse.ClientSecret) == "" || strings.TrimSpace(pollResponse.UserInfo.OpenID) == "" {
			return PollResult{Status: PollStatusError, Domain: domain, Message: "飞书授权响应缺少应用凭据"}, nil
		}
		// effectiveDomain 是租户品牌识别后用于开放平台调用的站点域名。
		effectiveDomain := domain
		if strings.EqualFold(strings.TrimSpace(pollResponse.UserInfo.TenantBrand), DomainLark) {
			effectiveDomain = DomainLark
		}
		return PollResult{
			Status:    PollStatusSuccess,
			Domain:    effectiveDomain,
			AppID:     pollResponse.ClientID,
			AppSecret: pollResponse.ClientSecret,
			OpenID:    pollResponse.UserInfo.OpenID,
			Message:   "飞书应用绑定成功",
		}, nil
	case "authorization_pending":
		return PollResult{Status: PollStatusPending, Domain: pollResponseDomain(domain, pollResponse.UserInfo.TenantBrand), Message: "等待飞书扫码确认"}, nil
	case "slow_down":
		// interval 是平台要求的新轮询间隔；零值由应用层按当前间隔递增。
		interval := time.Duration(pollResponse.Interval) * time.Second
		return PollResult{Status: PollStatusSlowDown, Domain: pollResponseDomain(domain, pollResponse.UserInfo.TenantBrand), Interval: interval, Message: "飞书要求降低轮询频率"}, nil
	case "access_denied":
		return PollResult{Status: PollStatusDenied, Domain: domain, Message: "用户已拒绝飞书应用授权"}, nil
	case "expired_token":
		return PollResult{Status: PollStatusExpired, Domain: domain, Message: "飞书扫码授权已过期，请重新扫码"}, nil
	default:
		return PollResult{Status: PollStatusError, Domain: domain, Message: "飞书授权失败，请重新扫码"}, nil
	}
}

// accountsBaseURLFor 返回指定域名对应的账号服务源站；未知域名已由 resolveDomain 拒绝。
func (c *Client) accountsBaseURLFor(domain string) string {
	if domain == DomainLark {
		return c.larkAccountsBaseURL
	}
	return c.accountsBaseURL
}

// openBaseURLFor 返回指定域名对应的开放平台源站。
func (c *Client) openBaseURLFor(domain string) string {
	if domain == DomainLark {
		return c.larkOpenBaseURL
	}
	return c.openBaseURL
}

// postForm 向飞书账号服务提交表单并读取有界响应体；HTTP 与传输错误只返回脱敏诊断。
func (c *Client) postForm(ctx context.Context, endpoint string, values url.Values) ([]byte, error) {
	// request、buildErr 保存表单 POST 请求及构造错误。
	request, buildErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(values.Encode()))
	if buildErr != nil {
		return nil, fmt.Errorf("构造飞书扫码请求失败: %s", logsafe.ExternalError(buildErr))
	}
	request.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	// response、requestErr 保存平台响应及传输错误。
	response, requestErr := c.HTTPClient.Do(request)
	if requestErr != nil {
		return nil, fmt.Errorf("飞书扫码请求失败: %s", logsafe.ExternalError(requestErr))
	}
	defer response.Body.Close()
	// body、readErr 保存有界响应体及读取错误。
	body, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if readErr != nil {
		return nil, fmt.Errorf("读取飞书扫码响应失败: %s", logsafe.ExternalError(readErr))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		// platformError 保存设备码协议以 HTTP 400 返回的带内错误体；此时响应仍是可解析的业务结果。
		var platformError struct {
			// Error 是 authorization_pending、slow_down 等协议错误码。
			Error string `json:"error"`
		}
		if json.Unmarshal(body, &platformError) == nil && strings.TrimSpace(platformError.Error) != "" {
			return body, nil
		}
		return nil, fmt.Errorf("飞书扫码服务返回状态码 %d", response.StatusCode)
	}
	return body, nil
}

// addSDKQuery 在平台扫码地址上追加 SDK 来源参数与压缩 addons，保持原有查询参数不变。
// rawURL 是平台返回的完整扫码地址；返回值可直接编码为二维码，错误不会包含原始地址。
func addSDKQuery(rawURL string) (string, error) {
	// parsed、parseErr 保存解析后的扫码地址及解析错误。
	parsed, parseErr := url.Parse(strings.TrimSpace(rawURL))
	if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "", fmt.Errorf("飞书扫码地址无效")
	}
	// addons 是 gzip 后 base64url 编码的 scopes 声明，用于向平台申请消息收发权限。
	addons, addonsErr := encodeAddons()
	if addonsErr != nil {
		return "", addonsErr
	}
	// query 保存追加 SDK 参数后的查询集合。
	query := parsed.Query()
	query.Set("from", "sdk")
	query.Set("source", "ydisks-xianyu-helper")
	query.Set("tp", "sdk")
	query.Set("addons", addons)
	parsed.RawQuery = query.Encode()
	return parsed.String(), nil
}

// encodeAddons 将请求的租户消息 scopes 压缩为飞书 SDK 扫码地址要求的 base64url 字符串。
func encodeAddons() (string, error) {
	// addonsPayload 是扫码注册时向平台声明的权限集合，与飞书个人应用实际发送能力一致。
	addonsPayload := struct {
		// Scopes 按权限主体分组声明扫码后应用需要的权限。
		Scopes map[string][]string `json:"scopes"`
	}{
		Scopes: map[string][]string{
			"tenant": {"im:message", "im:message.send_as_bot", "im:chat", "im:resource"},
		},
	}
	// payloadJSON、marshalErr 保存 addons 的 JSON 表达及序列化错误。
	payloadJSON, marshalErr := json.Marshal(addonsPayload)
	if marshalErr != nil {
		return "", fmt.Errorf("编码飞书扫码权限声明失败")
	}
	// buffer 保存 gzip 压缩后的 addons 字节。
	var buffer bytes.Buffer
	// gzipWriter 是写入 buffer 的 gzip 编码器；closeErr 保存关闭编码器的错误。
	gzipWriter := gzip.NewWriter(&buffer)
	// writeErr 是 gzip 写入压缩数据失败原因；nil 表示权限声明已完整写入。
	if _, writeErr := gzipWriter.Write(payloadJSON); writeErr != nil {
		_ = gzipWriter.Close()
		return "", fmt.Errorf("压缩飞书扫码权限声明失败")
	}
	// closeErr 是关闭 gzip 编码器失败原因；nil 表示压缩尾部已写完。
	if closeErr := gzipWriter.Close(); closeErr != nil {
		return "", fmt.Errorf("压缩飞书扫码权限声明失败")
	}
	return base64.RawURLEncoding.EncodeToString(buffer.Bytes()), nil
}

// resolveDomain 归一站点域名；空值按飞书中国站处理，未知值返回协议错误避免误连其他站点。
func resolveDomain(domain string) (string, error) {
	// normalized 是去除空白并转小写后的域名参数。
	normalized := strings.ToLower(strings.TrimSpace(domain))
	switch normalized {
	case "", DomainFeishu:
		return DomainFeishu, nil
	case DomainLark:
		return DomainLark, nil
	default:
		return "", fmt.Errorf("飞书域名参数无效")
	}
}

// pollResponseDomain 在轮询响应中识别 Lark 租户品牌，保证后续轮询和开放平台调用一次性切换到国际站。
func pollResponseDomain(currentDomain, tenantBrand string) string {
	if strings.EqualFold(strings.TrimSpace(tenantBrand), DomainLark) {
		return DomainLark
	}
	return currentDomain
}

// sanitizePlatformMessage 选择可安全展示的平台错误描述；缺少描述时退回错误码。
func sanitizePlatformMessage(description, code string) string {
	// message 是去除空白后的平台错误描述。
	message := strings.TrimSpace(description)
	if message == "" {
		message = strings.TrimSpace(code)
	}
	if message == "" {
		return "未知错误"
	}
	return message
}

// beginRegistrationResponse 是 begin 阶段响应的内部协议模型；字段只在本包内使用。
type beginRegistrationResponse struct {
	// DeviceCode 是平台颁发的短期设备码。
	DeviceCode string `json:"device_code"`
	// VerificationURIComplete 是平台返回的完整扫码地址。
	VerificationURIComplete string `json:"verification_uri_complete"`
	// ExpiresIn 是设备码有效期秒数。
	ExpiresIn int `json:"expires_in"`
	// Interval 是平台建议轮询间隔秒数。
	Interval int `json:"interval"`
	// Error 是平台错误码；成功时为空。
	Error string `json:"error"`
	// ErrorDescription 是平台错误描述；可能为空。
	ErrorDescription string `json:"error_description"`
}

// pollRegistrationResponse 是 poll 阶段响应的内部协议模型；成功后携带应用凭据与用户信息。
type pollRegistrationResponse struct {
	// Error 是平台错误码；成功时为空。
	Error string `json:"error"`
	// ErrorDescription 是平台错误描述；可能为空。
	ErrorDescription string `json:"error_description"`
	// ClientID 是扫码创建的应用标识。
	ClientID string `json:"client_id"`
	// ClientSecret 是扫码创建的应用密钥，禁止日志输出。
	ClientSecret string `json:"client_secret"`
	// UserInfo 是授权用户与租户品牌信息。
	UserInfo struct {
		// OpenID 是授权用户的飞书 open_id。
		OpenID string `json:"open_id"`
		// TenantBrand 是租户品牌；为 lark 时需要切换到国际站域名。
		TenantBrand string `json:"tenant_brand"`
	} `json:"user_info"`
	// Interval 是 slow_down 状态下平台要求的新轮询间隔秒数。
	Interval int `json:"interval"`
}
