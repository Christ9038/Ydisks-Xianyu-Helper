package adapter

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// remoteCaptchaBrowserTimeout 保存remoteCaptcha浏览器Timeout，供当前处理流程使用
const (
	remoteCaptchaBrowserTimeout = 20
	remoteCaptchaResponseLimit  = 1 << 20
)

// remoteCaptchaConfig 保存remoteCaptcha配置，供当前处理流程使用
type remoteCaptchaConfig struct {
	URL         string
	Secret      string
	PassCookies bool
}

// remoteCaptchaStatus 保存remoteCaptcha状态，供当前处理流程使用
type remoteCaptchaStatus uint8

// remoteCaptchaFallback 保存remoteCaptchaFallback，供当前处理流程使用
const (
	remoteCaptchaFallback remoteCaptchaStatus = iota
	remoteCaptchaOK
	remoteCaptchaFailed
	remoteCaptchaURLExpired
)

// remoteCaptchaResult 保存remoteCaptcha结果，供当前处理流程使用
type remoteCaptchaResult struct {
	status  remoteCaptchaStatus
	cookies map[string]string
	err     error
}

// loadRemoteCaptchaConfig 负责loadRemoteCaptcha配置相关处理。
func (a *Adapter) loadRemoteCaptchaConfig(ctx context.Context) *remoteCaptchaConfig {
	if a.store == nil || a.store.Settings == nil {
		return nil
	}
	// urlValue、err 保存地址Value、err，供当前处理流程使用
	urlValue, err := a.store.Settings.Get(ctx, "captcha.remote_service_url")
	if err != nil {
		a.logger.Warn("读取远程过滑块地址失败，回退本机逻辑", "err", err)
		return nil
	}
	// secret、err 保存secret、err，供当前处理流程使用
	secret, err := a.store.Settings.Get(ctx, "captcha.remote_secret_key")
	if err != nil {
		a.logger.Warn("读取远程过滑块密钥失败，回退本机逻辑", "err", err)
		return nil
	}
	urlValue, secret = strings.TrimSpace(urlValue), strings.TrimSpace(secret)
	if urlValue == "" || secret == "" {
		return nil
	}
	// passCookies、err 保存passCookies、err，供当前处理流程使用
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

// newRemoteCaptchaHTTPClient 负责newRemoteCaptchaHTTPClient相关处理。
func newRemoteCaptchaHTTPClient() *http.Client {
	// dialer 保存dialer，供当前处理流程使用
	dialer := &net.Dialer{Timeout: 8 * time.Second, KeepAlive: 30 * time.Second}
	return &http.Client{
		Transport: &http.Transport{
			Proxy:                 http.ProxyFromEnvironment,
			DialContext:           dialer.DialContext,
			ForceAttemptHTTP2:     true,
			MaxIdleConns:          20,
			MaxIdleConnsPerHost:   4,
			IdleConnTimeout:       30 * time.Second,
			TLSHandshakeTimeout:   8 * time.Second,
			ResponseHeaderTimeout: 90 * time.Second,
		},
		Timeout: 90 * time.Second,
	}
}

// callRemoteCaptcha 负责callRemoteCaptcha相关处理。
func callRemoteCaptcha(ctx context.Context, client *http.Client, cfg remoteCaptchaConfig, accountID, verificationURL, cookies, deviceID string) remoteCaptchaResult {
	// payload 保存请求载荷，供当前处理流程使用
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
	// raw、err 保存raw、err，供当前处理流程使用
	raw, err := json.Marshal(payload)
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFailed, err: err}
	}
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(raw))
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFailed, err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := client.Do(req)
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFallback, err: err}
	}
	defer resp.Body.Close()
	// body、err 保存body、err，供当前处理流程使用
	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteCaptchaResponseLimit+1))
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFallback, err: err}
	}
	if len(body) > remoteCaptchaResponseLimit {
		return remoteCaptchaResult{status: remoteCaptchaFailed, err: fmt.Errorf("远程响应超过 %d 字节", remoteCaptchaResponseLimit)}
	}
	// decoded 保存decoded，供当前处理流程使用
	var decoded struct {
		Success bool `json:"success"`
		Data    struct {
			Cookies    map[string]string `json:"cookies"`
			URLExpired bool              `json:"url_expired"`
		} `json:"data"`
	}
	if // err 保存err，供当前处理流程使用
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

// solveRemoteCaptcha 负责solveRemoteCaptcha相关处理。
func solveRemoteCaptcha(
	ctx context.Context,
	client *http.Client,
	cfg remoteCaptchaConfig,
	accountID, verificationURL, cookieStr, deviceID string,
	provider func(context.Context, string) (string, bool, string, error),
) (cookies string, handled bool, err error) {
	// currentCookies 保存currentCookies，供当前处理流程使用
	currentCookies := cookieStr
	// currentURL 保存currentURL，供当前处理流程使用
	currentURL := verificationURL
	for // refreshCount 保存refresh数量，供当前处理流程使用
	refreshCount := 0; ; {
		// result 保存结果，供当前处理流程使用
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
			// freshURL、tokenOK、updatedCookies、providerErr 保存freshURL、tokenOK、updatedCookies、providerErr，供当前处理流程使用
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

// hasX5Cookies 负责hasX5Cookies相关处理。
func hasX5Cookies(cookies map[string]string) bool {
	// name、value 表示当前遍历过程中的name、value
	for name, value := range cookies {
		// lower 保存lower，供当前处理流程使用
		lower := strings.ToLower(strings.TrimSpace(name))
		if strings.TrimSpace(value) != "" && (strings.HasPrefix(lower, "x5") || strings.Contains(lower, "x5sec")) {
			return true
		}
	}
	return false
}

// mergeX5Cookies 负责mergeX5Cookies相关处理。
func mergeX5Cookies(original string, incoming map[string]string) string {
	// merged 保存merged，供当前处理流程使用
	merged := cookierefresh.ParseCookieString(original)
	// name、value 表示当前遍历过程中的name、value
	for name, value := range incoming {
		// lower 保存lower，供当前处理流程使用
		lower := strings.ToLower(strings.TrimSpace(name))
		if strings.HasPrefix(lower, "x5") || strings.Contains(lower, "x5sec") {
			merged[name] = value
		}
	}
	return cookierefresh.MarshalCookieString(merged)
}
