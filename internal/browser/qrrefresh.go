package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"xianyu-go/internal/logsafe"
)

// QRCookieRefresh 用扫码临时 cookie + 风控验证 URL，通过浏览器换取真实 cookie（含 unb）。
// verificationURL 为空时跳过验证步骤（无风控场景）。
func (m *Manager) QRCookieRefresh(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (realCookies string, unb string, err error) {
	if tmpCookies == "" {
		return "", "", fmt.Errorf("扫码临时 cookie 为空")
	}
	if err := m.init(); err != nil {
		return "", "", err
	}

	m.logger.Info("开始用临时 cookie 换取真实 cookie", "tmp_cookie_count", len(parseCookieStr(tmpCookies)))

	// QR 刷新不复用账号池：一次性上下文，注入临时 cookie，访问后提取。
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: playwright.Bool(true),
		Args:     chromiumLaunchArgs(),
	})
	if err != nil {
		return "", "", fmt.Errorf("启动 chromium 失败: %w", err)
	}
	defer func() { _ = browser.Close() }()

	bctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent:  playwright.String(defaultUA),
		Viewport:   &playwright.Size{Width: 400, Height: 400},
		Locale:     playwright.String(defaultLang),
		TimezoneId: playwright.String(defaultTZ),
	})
	if err != nil {
		return "", "", fmt.Errorf("创建 context 失败: %w", err)
	}
	defer func() { _ = bctx.Close() }()

	if err := bctx.AddInitScript(playwright.Script{Content: playwright.String(stealthScript())}); err != nil {
		m.logger.Warn("注入 stealth 脚本失败", "err", err)
	}
	if err := addCookieStr(bctx, tmpCookies); err != nil {
		return "", "", fmt.Errorf("注入临时 cookie 失败: %w", err)
	}

	page, err := bctx.NewPage()
	if err != nil {
		return "", "", fmt.Errorf("新建 page 失败: %w", err)
	}

	if verificationURL != "" {
		// 关键：Chromium 打开验证页面并持有该 session。
		// 用户在手机完成验证后，mini_login_check.htm 会自动 redirect 到 ivCheckLogin.htm。
		// Chromium 跟随这个 redirect，服务端会在此 session 里写入认证态。
		m.logger.Info("Chromium 打开验证等待页", "verification_url", logsafe.URL(verificationURL))
		if _, err := page.Goto(verificationURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			Timeout:   playwright.Float(15000),
		}); err != nil {
			m.logger.Warn("打开验证页面异常", "err", err)
		}

		// 等待用户完成手机验证，页面会 redirect 到 ivCheckLogin.htm（最多3分钟）。
		// 每2秒截图一次并通过 onScreenshot 回调发给前端，用户可直接看到验证页面。
		m.logger.Info("等待验证完成（监测页面 redirect + 截图推送）...")
		deadline := time.Now().Add(3 * time.Minute)
		for time.Now().Before(deadline) {
			u := page.URL()
			if strings.Contains(u, "ivCheckLogin") {
				m.logger.Info("验证通过，页面已跳转", "url", logsafe.URL(u))
				break
			}
			// 截图并回调给前端。
			if onScreenshot != nil {
				if img, err2 := page.Screenshot(); err2 == nil {
					onScreenshot("data:image/png;base64," + base64.StdEncoding.EncodeToString(img))
				}
			}
			sleep(2 * time.Second)
		}
		if !strings.Contains(page.URL(), "ivCheckLogin") {
			return "", "", fmt.Errorf("等待验证超时（3分钟），请重新扫码")
		}
		sleep(2 * time.Second)
	}

	// 访问 /im 触发 unb 写入。
	if _, err := page.Goto(goofishIMURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(20000),
	}); err != nil {
		m.logger.Warn("访问 goofish.com/im 异常", "err", err)
	}
	m.logger.Info("goto /im 完成", "current_url", logsafe.URL(page.URL()))

	// reload 后再等网络空闲，确保 unb 写入。
	if _, err := page.Reload(playwright.PageReloadOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(15000),
	}); err != nil {
		m.logger.Warn("reload 异常", "err", err)
	}
	m.logger.Info("reload 完成", "current_url", logsafe.URL(page.URL()))

	// 轮询直到 unb 出现或超时（最长15秒）。
	var all []playwright.Cookie
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		var err2 error
		all, err2 = bctx.Cookies()
		if err2 == nil {
			if m2 := cookiesToMap(all); m2["unb"] != "" {
				m.logger.Info("轮询拿到 unb")
				break
			}
		}
		sleep(500 * time.Millisecond)
	}

	if len(all) == 0 {
		all, err = bctx.Cookies()
		if err != nil {
			return "", "", fmt.Errorf("提取 cookie 失败: %w", err)
		}
	}
	cookieMap := cookiesToMap(all)
	cookieStr := cookieMarshal(cookieMap)
	unb = cookieMap["unb"]

	// 关键字段日志，便于排查。
	for _, k := range []string{"unb", "_m_h5_tk", "_m_h5_tk_enc", "cookie2", "t", "sgcookie", "cna"} {
		if v, ok := cookieMap[k]; ok {
			m.logger.Info("cookie字段", "name", k, "len", len(v))
		} else {
			m.logger.Info("cookie字段缺失", "name", k)
		}
	}
	m.logger.Info("提取 cookie 完成", "count", len(all), "has_unb", unb != "")

	if cookieStr == "" || unb == "" {
		return "", "", fmt.Errorf("浏览器提取后仍未获取到 unb，可能验证未完成或临时 cookie 已失效")
	}
	return cookieStr, unb, nil
}

// gotoWithFallback 访问 URL，超时降级（domcontentloaded→load→无等待）。
func gotoWithFallback(page playwright.Page, url string) error {
	to := func(ms float64) *float64 { return &ms }
	if _, err := page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   to(15000),
	}); err == nil {
		return nil
	}
	if _, err := page.Goto(url, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateLoad,
		Timeout:   to(20000),
	}); err == nil {
		return nil
	}
	_, err := page.Goto(url, playwright.PageGotoOptions{Timeout: to(25000)})
	return err
}

func reloadWithFallback(page playwright.Page) error {
	to := func(ms float64) *float64 { return &ms }
	if _, err := page.Reload(playwright.PageReloadOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   to(12000),
	}); err == nil {
		return nil
	}
	_, err := page.Reload(playwright.PageReloadOptions{
		WaitUntil: playwright.WaitUntilStateLoad,
		Timeout:   to(15000),
	})
	return err
}

// sleep 可被测试替换。
var sleep = time.Sleep

func cookieNames(m map[string]string) []string {
	names := make([]string, 0, len(m))
	for k := range m {
		names = append(names, k)
	}
	return names
}
