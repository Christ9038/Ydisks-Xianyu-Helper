package browser

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

// TokenCaptchaURLProvider is called after Chromium is ready, so the token API
// can return a fresh punish URL instead of using one that expired while waiting.
type TokenCaptchaURLProvider func(ctx context.Context, currentCookies string) (url string, tokenOK bool, updatedCookies string, err error)

// TokenCaptchaRecover solves a token-refresh captcha and returns cookies with x5sec merged in.
func (m *Manager) TokenCaptchaRecover(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider TokenCaptchaURLProvider) (string, error) {
	if strings.TrimSpace(cookieStr) == "" {
		return "", fmt.Errorf("Cookie为空，无法处理 token 风控验证")
	}
	if strings.TrimSpace(verificationURL) == "" {
		return "", fmt.Errorf("验证链接为空")
	}

	currentCookies := cookieStr
	bctx, release, err := m.newPersistentRenewContext(ctx, cookieID, currentCookies, nil, quickRenewHeadless(headless))
	if err != nil {
		return "", err
	}
	defer release()

	targetURL := verificationURL
	if provider != nil {
		freshURL, tokenOK, updated, err := provider(ctx, currentCookies)
		if err != nil {
			m.logger.Warn("token风控验证前重取链接失败，沿用原链接", "cookieID", cookieID, "err", err)
		}
		if strings.TrimSpace(updated) != "" && updated != currentCookies {
			currentCookies = updated
			if err := addCookieStr(bctx, currentCookies); err != nil {
				m.logger.Warn("token风控验证注入重取后的 Cookie 失败", "cookieID", cookieID, "err", err)
			}
		}
		if tokenOK {
			m.logger.Info("token风控验证前重取 token 已成功，无需滑块", "cookieID", cookieID)
			return currentCookies, nil
		}
		if strings.TrimSpace(freshURL) != "" {
			targetURL = freshURL
		}
	}

	page, err := bctx.NewPage()
	if err != nil {
		return "", fmt.Errorf("新建 page 失败: %w", err)
	}
	defer func() { _ = page.Close() }()

	if _, err := page.Goto(targetURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	}); err != nil {
		m.logger.Warn("token风控验证页面访问异常", "cookieID", cookieID, "err", err)
	}
	time.Sleep(time.Second)

	content, _ := page.Content()
	scratch := isScratchCaptcha(content)
	if err := solveSlider(page, scratch, m.logger); err != nil {
		return "", err
	}
	time.Sleep(2 * time.Second)

	all, err := bctx.Cookies()
	if err != nil {
		return "", fmt.Errorf("提取 token 风控 cookie 失败: %w", err)
	}
	x5 := make(map[string]string)
	hasX5Sec := false
	for _, c := range all {
		name := strings.ToLower(c.Name)
		if strings.HasPrefix(name, "x5") || strings.Contains(name, "x5sec") {
			x5[c.Name] = c.Value
			if name == "x5sec" && strings.TrimSpace(c.Value) != "" {
				hasX5Sec = true
			}
		}
	}
	if !hasX5Sec {
		return "", fmt.Errorf("滑块验证未获取到 x5sec cookie")
	}
	merged := parseCookieStr(currentCookies)
	for k, v := range x5 {
		merged[k] = v
	}
	m.logger.Info("token风控验证成功", "cookieID", cookieID, "x5_cookie_count", len(x5))
	return cookieMarshal(merged), nil
}
