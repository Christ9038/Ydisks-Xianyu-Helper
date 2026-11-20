package feishu

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/logsafe"
)

const (
	// tenantTokenPath 是获取 tenant_access_token 的开放平台路径。
	tenantTokenPath = "/open-apis/auth/v3/tenant_access_token/internal"
	// sendMessagePath 是发送应用消息的开放平台路径，接收者类型通过查询参数声明。
	sendMessagePath = "/open-apis/im/v1/messages"
	// tokenCacheSafetyWindow 是缓存令牌提前失效的时间，避免在令牌即将过期时发起发送。
	tokenCacheSafetyWindow = 5 * time.Minute
	// defaultTokenTTL 是平台未返回有效令牌时长时的保守缓存时长。
	defaultTokenTTL = 2 * time.Hour
)

// cachedToken 是进程内缓存的一条 tenant_access_token。
type cachedToken struct {
	// Token 是平台颁发的租户访问令牌明文，只允许作为 Authorization 请求头使用，禁止日志输出。
	Token string
	// ExpiresAt 是本进程停止使用该令牌的时间，通常早于平台真实过期时间。
	ExpiresAt time.Time
}

// tokenCache 是按站点、应用和密钥指纹隔离的线程安全租户令牌缓存。
type tokenCache struct {
	// mu 保护 entries；锁内只执行内存读写，不执行任何网络或数据库 I/O。
	mu sync.Mutex
	// entries 保存当前进程已经获取且仍可使用的令牌。
	entries map[string]cachedToken
}

// newTokenCache 构造空令牌缓存，供包级共享缓存与测试隔离缓存使用。
func newTokenCache() *tokenCache {
	return &tokenCache{entries: make(map[string]cachedToken)}
}

// sharedTokenCache 是所有生产客户端共享的租户令牌缓存；令牌只在内存中短期保存。
var sharedTokenCache = newTokenCache()

// get 读取未过期令牌；ok 表示缓存命中且尚未进入安全失效窗口。
func (c *tokenCache) get(key string, now time.Time) (token string, ok bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	// entry、exists 保存缓存条目及键是否存在。
	entry, exists := c.entries[key]
	if !exists || entry.Token == "" || !now.Before(entry.ExpiresAt) {
		return "", false
	}
	return entry.Token, true
}

// set 写入令牌缓存及其本进程有效截止时间。
func (c *tokenCache) set(key, token string, expiresAt time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.entries == nil {
		c.entries = make(map[string]cachedToken)
	}
	c.entries[key] = cachedToken{Token: token, ExpiresAt: expiresAt}
}

// TenantAccessToken 返回应用到站点的 tenant_access_token，优先使用内存缓存。
// ctx 控制单次令牌请求；appSecret 只进入请求体，不进入错误、日志或缓存键明文。
func (c *Client) TenantAccessToken(ctx context.Context, appID, appSecret, domain string) (string, error) {
	// resolvedDomain 是归一后的开放平台站点域名。
	resolvedDomain, domainErr := resolveDomain(domain)
	if domainErr != nil {
		return "", domainErr
	}
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(appSecret) == "" {
		return "", fmt.Errorf("飞书应用凭据不完整")
	}
	// now 是本次缓存查询时间。
	now := time.Now()
	// cacheKey 是隔离站点、应用和密钥指纹的缓存键。
	cacheKey := tenantTokenCacheKey(resolvedDomain, appID, appSecret)
	// token、ok 保存缓存命中的令牌与是否命中；命中时直接复用。
	if token, ok := c.tokens.get(cacheKey, now); ok {
		return token, nil
	}
	// requestPayload 是获取租户令牌的应用凭据请求体，只出现在 HTTP 请求中。
	requestPayload := struct {
		// AppID 是飞书应用标识。
		AppID string `json:"app_id"`
		// AppSecret 是飞书应用密钥，禁止日志输出。
		AppSecret string `json:"app_secret"`
	}{AppID: appID, AppSecret: appSecret}
	// payloadBytes、marshalErr 保存序列化后的请求体及错误。
	payloadBytes, marshalErr := json.Marshal(requestPayload)
	if marshalErr != nil {
		return "", fmt.Errorf("编码飞书租户令牌请求失败")
	}
	// request、buildErr 保存租户令牌请求及构造错误。
	request, buildErr := http.NewRequestWithContext(ctx, http.MethodPost, c.openBaseURLFor(resolvedDomain)+tenantTokenPath, bytes.NewReader(payloadBytes))
	if buildErr != nil {
		return "", fmt.Errorf("构造飞书租户令牌请求失败: %s", logsafe.ExternalError(buildErr))
	}
	request.Header.Set("Content-Type", "application/json")
	// response、requestErr 保存开放平台响应及传输错误。
	response, requestErr := c.HTTPClient.Do(request)
	if requestErr != nil {
		return "", fmt.Errorf("飞书租户令牌请求失败: %s", logsafe.ExternalError(requestErr))
	}
	defer response.Body.Close()
	// responseBody、readErr 保存有界响应体及读取错误。
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if readErr != nil {
		return "", fmt.Errorf("读取飞书租户令牌响应失败: %s", logsafe.ExternalError(readErr))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return "", fmt.Errorf("飞书租户令牌服务返回状态码 %d", response.StatusCode)
	}
	// tokenResponse 保存租户令牌响应字段。
	var tokenResponse tenantTokenResponse
	// decodeErr 是租户令牌响应 JSON 解析失败原因；nil 表示响应格式符合协议。
	if decodeErr := json.Unmarshal(responseBody, &tokenResponse); decodeErr != nil {
		return "", fmt.Errorf("解析飞书租户令牌响应失败")
	}
	if tokenResponse.Code != 0 {
		return "", fmt.Errorf("飞书租户令牌获取失败 code=%d: %s", tokenResponse.Code, sanitizePlatformMessage(tokenResponse.Msg, ""))
	}
	// token 是平台颁发的访问令牌明文；空值表示响应非法。
	token := strings.TrimSpace(tokenResponse.TenantAccessToken)
	if token == "" {
		return "", fmt.Errorf("飞书租户令牌响应缺少访问令牌")
	}
	// ttl 是平台返回的有效秒数，异常值退回保守默认时长。
	ttl := time.Duration(tokenResponse.Expire) * time.Second
	if ttl <= 0 {
		ttl = defaultTokenTTL
	}
	// effectiveTTL 是提前安全窗口后的本进程缓存时长，极短令牌至少保留一半有效期。
	effectiveTTL := ttl - tokenCacheSafetyWindow
	if effectiveTTL <= 0 {
		effectiveTTL = ttl / 2
	}
	if effectiveTTL <= 0 {
		effectiveTTL = time.Second
	}
	c.tokens.set(cacheKey, token, time.Now().Add(effectiveTTL))
	return token, nil
}

// SendText 通过扫码创建的个人应用向指定 open_id 发送文本消息。
// ctx 控制令牌与消息请求；appSecret 只进入令牌请求，text 作为消息正文发送，不记录到日志。
func (c *Client) SendText(ctx context.Context, appID, appSecret, openID, domain, text string) error {
	// resolvedDomain 是归一后的开放平台站点域名。
	resolvedDomain, domainErr := resolveDomain(domain)
	if domainErr != nil {
		return domainErr
	}
	if strings.TrimSpace(appID) == "" || strings.TrimSpace(appSecret) == "" {
		return fmt.Errorf("飞书应用凭据不完整")
	}
	if strings.TrimSpace(openID) == "" {
		return fmt.Errorf("飞书接收用户 open_id 不能为空")
	}
	if strings.TrimSpace(text) == "" {
		return fmt.Errorf("飞书消息正文不能为空")
	}
	// token、tokenErr 保存可用的租户访问令牌及获取错误。
	token, tokenErr := c.TenantAccessToken(ctx, appID, appSecret, resolvedDomain)
	if tokenErr != nil {
		return tokenErr
	}
	// contentJSON 是飞书消息 content 字段要求的 JSON 字符串。
	contentJSON, contentErr := json.Marshal(map[string]string{"text": text})
	if contentErr != nil {
		return fmt.Errorf("编码飞书消息正文失败")
	}
	// messagePayload 是发送文本消息的请求体；content 必须是二次编码后的 JSON 字符串。
	messagePayload := struct {
		// ReceiveID 是接收者 open_id。
		ReceiveID string `json:"receive_id"`
		// MsgType 是飞书消息类型，文本固定为 text。
		MsgType string `json:"msg_type"`
		// Content 是消息内容的 JSON 字符串。
		Content string `json:"content"`
	}{ReceiveID: openID, MsgType: "text", Content: string(contentJSON)}
	// payloadBytes、marshalErr 保存消息请求体序列化结果及错误。
	payloadBytes, marshalErr := json.Marshal(messagePayload)
	if marshalErr != nil {
		return fmt.Errorf("编码飞书消息请求失败")
	}
	// endpoint 是带 open_id 接收者类型的消息发送地址。
	endpoint := c.openBaseURLFor(resolvedDomain) + sendMessagePath + "?receive_id_type=open_id"
	// request、buildErr 保存消息请求及构造错误。
	request, buildErr := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payloadBytes))
	if buildErr != nil {
		return fmt.Errorf("构造飞书消息请求失败: %s", logsafe.ExternalError(buildErr))
	}
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+token)
	// response、requestErr 保存消息响应及传输错误。
	response, requestErr := c.HTTPClient.Do(request)
	if requestErr != nil {
		return fmt.Errorf("飞书消息请求失败: %s", logsafe.ExternalError(requestErr))
	}
	defer response.Body.Close()
	// responseBody、readErr 保存有界响应体及读取错误。
	responseBody, readErr := io.ReadAll(io.LimitReader(response.Body, maxResponseBytes))
	if readErr != nil {
		return fmt.Errorf("读取飞书消息响应失败: %s", logsafe.ExternalError(readErr))
	}
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("飞书消息服务返回状态码 %d", response.StatusCode)
	}
	// messageResponse 保存消息发送响应字段。
	var messageResponse messageResponse
	// decodeErr 是消息发送响应 JSON 解析失败原因；nil 表示响应格式符合协议。
	if decodeErr := json.Unmarshal(responseBody, &messageResponse); decodeErr != nil {
		return fmt.Errorf("解析飞书消息响应失败")
	}
	if messageResponse.Code != 0 {
		return fmt.Errorf("飞书消息发送失败 code=%d: %s", messageResponse.Code, sanitizePlatformMessage(messageResponse.Msg, ""))
	}
	return nil
}

// SendText 使用进程共享令牌缓存执行一次飞书文本消息发送，供通知器在每次投递时复用缓存。
// httpClient 是调用方持有的有界 HTTP 客户端；其余参数与 Client.SendText 一致。
func SendText(ctx context.Context, httpClient *http.Client, appID, appSecret, openID, domain, text string) error {
	return NewClient(httpClient).SendText(ctx, appID, appSecret, openID, domain, text)
}

// tenantTokenCacheKey 生成不包含密钥明文的缓存键，站点与应用标识相同但密钥变化时不会复用旧令牌。
func tenantTokenCacheKey(domain, appID, appSecret string) string {
	// secretDigest 是应用密钥的 SHA-256 摘要，只用于隔离缓存而不保存明文。
	secretDigest := sha256.Sum256([]byte(appSecret))
	return domain + "\x00" + appID + "\x00" + hex.EncodeToString(secretDigest[:])
}

// tenantTokenResponse 是获取租户令牌响应的内部协议模型。
type tenantTokenResponse struct {
	// Code 是开放平台业务码；0 表示成功。
	Code int `json:"code"`
	// Msg 是开放平台错误描述；成功时为空。
	Msg string `json:"msg"`
	// TenantAccessToken 是租户访问令牌明文，禁止日志输出。
	TenantAccessToken string `json:"tenant_access_token"`
	// Expire 是令牌有效秒数。
	Expire int `json:"expire"`
}

// messageResponse 是发送消息响应的内部协议模型；业务码非零表示发送失败。
type messageResponse struct {
	// Code 是开放平台业务码；0 表示成功。
	Code int `json:"code"`
	// Msg 是开放平台错误描述；成功时为空。
	Msg string `json:"msg"`
}
