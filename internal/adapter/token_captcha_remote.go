package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"xianyu-go/internal/netguard"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// remoteCaptchaBrowserTimeout 用于本次流程后续判断的remoteCaptcha浏览器Timeout
const (
	remoteCaptchaBrowserTimeout = 20
	remoteCaptchaResponseLimit  = 1 << 20
)

// remoteCaptchaConfig 用于本次流程后续判断的remoteCaptcha配置
type remoteCaptchaConfig struct {
	URL         string
	Secret      string
	PassCookies bool
}

// remoteCaptchaStatus 用于本次流程后续判断的remoteCaptcha状态
type remoteCaptchaStatus uint8

// remoteCaptchaFallback 用于本次流程后续判断的remoteCaptchaFallback
const (
	remoteCaptchaFallback remoteCaptchaStatus = iota
	remoteCaptchaOK
	remoteCaptchaFailed
	remoteCaptchaURLExpired
)

// remoteCaptchaResult 用于本次流程后续判断的remoteCaptcha结果
type remoteCaptchaResult struct {
	status  remoteCaptchaStatus
	cookies map[string]string
	err     error
}

// loadRemoteCaptchaConfig 封装loadRemoteCaptcha配置业务协调。
func (a *Adapter) loadRemoteCaptchaConfig(ctx context.Context, cookieID string) *remoteCaptchaConfig {
	if a.store == nil || a.store.Settings == nil {
		return nil
	}
	// urlValue、err 用于本次流程后续判断的地址Value、err
	urlValue, err := a.store.Settings.Get(ctx, "captcha.remote_service_url")
	if err != nil {
		a.logger.Warn("读取远程过滑块地址失败，回退本机逻辑", "err", err)
		return nil
	}
	// secret、err 用于本次流程后续判断的secret、err
	secret, err := a.store.ReadSensitiveSettingForAccount(ctx, cookieID, "captcha.remote_secret_key", "settings.use", "captcha_remote")
	if err != nil {
		a.logger.Warn("读取远程过滑块密钥失败，回退本机逻辑", "err", err)
		return nil
	}
	urlValue, secret = strings.TrimSpace(urlValue), strings.TrimSpace(secret)
	if urlValue == "" || secret == "" {
		return nil
	}
	// passCookies、err 用于本次流程后续判断的passCookies、err
	passCookies, err := a.store.Settings.Get(ctx, "captcha.remote_pass_cookies")
	if err != nil {
		a.logger.Warn("读取远程过滑块 Cookie 开关失败，按关闭处理", "err", err)
	}
	return &remoteCaptchaConfig{
		URL:         urlValue,
		Secret:      secret,
		PassCookies: strings.EqualFold(strings.TrimSpace(passCookies), "true"),
	}
}

// newRemoteCaptchaHTTPClient 封装newRemoteCaptchaHTTPClient业务协调。
func newRemoteCaptchaHTTPClient() *http.Client {
	return netguard.PolicyHTTPClientWithTimeouts(nil, 90*time.Second, 90*time.Second, 8*time.Second)
}

// callRemoteCaptcha 封装callRemoteCaptcha业务协调。
func callRemoteCaptcha(ctx context.Context, client *http.Client, cfg remoteCaptchaConfig, accountID, verificationURL, cookies, deviceID string) remoteCaptchaResult {
	// payload 用于本次流程后续判断的请求载荷
	payload := map[string]any{
		"secret_key":      cfg.Secret,
		"account_id":      accountID,
		"url":             verificationURL,
		"browser_timeout": remoteCaptchaBrowserTimeout,
	}
	if cfg.PassCookies {
		payload["cookies"] = cookies
		payload["device_id"] = deviceID
	}
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := json.Marshal(payload)
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFailed, err: err}
	}
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(raw))
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFailed, err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := client.Do(req)
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFallback, err: err}
	}
	defer resp.Body.Close()
	// body、err 用于本次流程后续判断的body、err
	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteCaptchaResponseLimit+1))
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFallback, err: err}
	}
	if len(body) > remoteCaptchaResponseLimit {
		return remoteCaptchaResult{status: remoteCaptchaFailed, err: fmt.Errorf("远程响应超过 %d 字节", remoteCaptchaResponseLimit)}
	}
	// decoded 用于本次流程后续判断的decoded
	var decoded struct {
		Success bool `json:"success"`
		Data    struct {
			Cookies    map[string]string `json:"cookies"`
			URLExpired bool              `json:"url_expired"`
		} `json:"data"`
	}
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal(body, &decoded); err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFailed, err: fmt.Errorf("解析远程响应: %w", err)}
	}
	if decoded.Success && hasX5Cookies(decoded.Data.Cookies) {
		return remoteCaptchaResult{status: remoteCaptchaOK, cookies: decoded.Data.Cookies}
	}
	if decoded.Data.URLExpired {
		return remoteCaptchaResult{status: remoteCaptchaURLExpired}
	}
	return remoteCaptchaResult{status: remoteCaptchaFailed, err: fmt.Errorf("远程过滑块未通过（HTTP %d）", resp.StatusCode)}
}

// solveRemoteCaptcha 封装solveRemoteCaptcha业务协调。
func solveRemoteCaptcha(
	ctx context.Context,
	client *http.Client,
	cfg remoteCaptchaConfig,
	accountID, verificationURL, cookieStr, deviceID string,
	provider func(context.Context, string) (string, bool, string, error),
) (cookies string, handled bool, err error) {
	// currentCookies 用于本次流程后续判断的currentCookies
	currentCookies := cookieStr
	// currentURL 用于本次流程后续判断的currentURL
	currentURL := verificationURL
	for // refreshCount 用于本次流程后续判断的refresh数量
	refreshCount := 0; ; {
		// result 用于本次流程后续判断的结果
		result := callRemoteCaptcha(ctx, client, cfg, accountID, currentURL, currentCookies, deviceID)
		switch result.status {
		case remoteCaptchaFallback:
			return "", false, result.err
		case remoteCaptchaOK:
			return mergeX5Cookies(currentCookies, result.cookies), true, nil
		case remoteCaptchaFailed:
			return "", true, result.err
		case remoteCaptchaURLExpired:
			if provider == nil || refreshCount >= 2 {
				return "", true, fmt.Errorf("远程反馈验证链接已过期且无法重取")
			}
			refreshCount++
			// freshURL、tokenOK、updatedCookies、providerErr 用于本次流程后续判断的freshURL、tokenOK、updatedCookies、providerErr
			freshURL, tokenOK, updatedCookies, providerErr := provider(ctx, currentCookies)
			if providerErr != nil {
				return "", true, fmt.Errorf("远程验证链接过期后重取失败: %w", providerErr)
			}
			if strings.TrimSpace(updatedCookies) != "" {
				currentCookies = updatedCookies
			}
			if tokenOK {
				return currentCookies, true, nil
			}
			if strings.TrimSpace(freshURL) == "" {
				return "", true, fmt.Errorf("远程验证链接过期后未获取到新链接")
			}
			currentURL = freshURL
		}
	}
}

// hasX5Cookies 封装hasX5Cookies业务协调。
func hasX5Cookies(cookies map[string]string) bool {
	// name、value 表示当前遍历过程中的name、value
	for name, value := range cookies {
		// lower 用于本次流程后续判断的lower
		lower := strings.ToLower(strings.TrimSpace(name))
		if strings.TrimSpace(value) != "" && (strings.HasPrefix(lower, "x5") || strings.Contains(lower, "x5sec")) {
			return true
		}
	}
	return false
}

// mergeX5Cookies 封装mergeX5Cookies业务协调。
func mergeX5Cookies(original string, incoming map[string]string) string {
	// merged 用于本次流程后续判断的merged
	merged := cookierefresh.ParseCookieString(original)
	// name、value 表示当前遍历过程中的name、value
	for name, value := range incoming {
		// lower 用于本次流程后续判断的lower
		lower := strings.ToLower(strings.TrimSpace(name))
		if strings.HasPrefix(lower, "x5") || strings.Contains(lower, "x5sec") {
			merged[name] = value
		}
	}
	return cookierefresh.MarshalCookieString(merged)
}
