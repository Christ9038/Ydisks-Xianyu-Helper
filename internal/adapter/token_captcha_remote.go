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

const (
	remoteCaptchaBrowserTimeout = 20
	remoteCaptchaResponseLimit  = 1 << 20
)

type remoteCaptchaConfig struct {
	URL         string
	Secret      string
	PassCookies bool
}

type remoteCaptchaStatus uint8

const (
	remoteCaptchaFallback remoteCaptchaStatus = iota
	remoteCaptchaOK
	remoteCaptchaFailed
	remoteCaptchaURLExpired
)

type remoteCaptchaResult struct {
	status  remoteCaptchaStatus
	cookies map[string]string
	err     error
}

func (a *Adapter) loadRemoteCaptchaConfig(ctx context.Context) *remoteCaptchaConfig {
	if a.store == nil || a.store.Settings == nil {
		return nil
	}
	urlValue, err := a.store.Settings.Get(ctx, "captcha.remote_service_url")
	if err != nil {
		a.logger.Warn("读取远程过滑块地址失败，回退本机逻辑", "err", err)
		return nil
	}
	secret, err := a.store.Settings.Get(ctx, "captcha.remote_secret_key")
	if err != nil {
		a.logger.Warn("读取远程过滑块密钥失败，回退本机逻辑", "err", err)
		return nil
	}
	urlValue, secret = strings.TrimSpace(urlValue), strings.TrimSpace(secret)
	if urlValue == "" || secret == "" {
		return nil
	}
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

func newRemoteCaptchaHTTPClient() *http.Client {
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

func callRemoteCaptcha(ctx context.Context, client *http.Client, cfg remoteCaptchaConfig, accountID, verificationURL, cookies, deviceID string) remoteCaptchaResult {
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
	raw, err := json.Marshal(payload)
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFailed, err: err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, cfg.URL, bytes.NewReader(raw))
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFailed, err: err}
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFallback, err: err}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, remoteCaptchaResponseLimit+1))
	if err != nil {
		return remoteCaptchaResult{status: remoteCaptchaFallback, err: err}
	}
	if len(body) > remoteCaptchaResponseLimit {
		return remoteCaptchaResult{status: remoteCaptchaFailed, err: fmt.Errorf("远程响应超过 %d 字节", remoteCaptchaResponseLimit)}
	}
	var decoded struct {
		Success bool `json:"success"`
		Data    struct {
			Cookies    map[string]string `json:"cookies"`
			URLExpired bool              `json:"url_expired"`
		} `json:"data"`
	}
	if err := json.Unmarshal(body, &decoded); err != nil {
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

func solveRemoteCaptcha(
	ctx context.Context,
	client *http.Client,
	cfg remoteCaptchaConfig,
	accountID, verificationURL, cookieStr, deviceID string,
	provider func(context.Context, string) (string, bool, string, error),
) (cookies string, handled bool, err error) {
	currentCookies := cookieStr
	currentURL := verificationURL
	for refreshCount := 0; ; {
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

func hasX5Cookies(cookies map[string]string) bool {
	for name, value := range cookies {
		lower := strings.ToLower(strings.TrimSpace(name))
		if strings.TrimSpace(value) != "" && (strings.HasPrefix(lower, "x5") || strings.Contains(lower, "x5sec")) {
			return true
		}
	}
	return false
}

func mergeX5Cookies(original string, incoming map[string]string) string {
	merged := cookierefresh.ParseCookieString(original)
	for name, value := range incoming {
		lower := strings.ToLower(strings.TrimSpace(name))
		if strings.HasPrefix(lower, "x5") || strings.Contains(lower, "x5sec") {
			merged[name] = value
		}
	}
	return cookierefresh.MarshalCookieString(merged)
}
