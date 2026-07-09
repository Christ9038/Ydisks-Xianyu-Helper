package browser

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"xianyu-go/internal/xianyu/cookierefresh"
)

const (
	quickRenewPageLoadWait = 3 * time.Second
	quickRenewAfterClick   = 5 * time.Second
	quickRenewTimeoutMS    = 30000
	quickRenewUA           = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/146.0.0.0 Safari/537.36"
)

// CookieRenew 用现有 Cookie 打开闲鱼页面，尝试通过“快速进入”刷新浏览器登录态。
//
// 这个方法位于密码登录之前：如果 Cookie 仍保留可续期的浏览器会话，页面通常会给出
// 免输密码的快速进入入口。成功点击后提取完整 Cookie，可避免更重的账号密码登录。
func (m *Manager) CookieRenew(ctx context.Context, cookieID, cookieStr string, headless bool) (map[string]string, error) {
	newCookies, err := m.BrowserQuickRenew(ctx, cookieID, cookieStr, headless)
	if err != nil {
		return nil, err
	}
	return cookierefresh.ParseCookieString(newCookies), nil
}

// BrowserQuickRenew 使用持久化浏览器上下文执行“快速进入”Cookie 续期。
func (m *Manager) BrowserQuickRenew(ctx context.Context, cookieID, cookieStr string, headless bool) (string, error) {
	if cookieStr == "" {
		return "", fmt.Errorf("Cookie为空，无法浏览器续期")
	}

	bctx, release, err := m.newPersistentRenewContext(ctx, cookieID, cookieStr, nil, quickRenewHeadless(headless))
	if err != nil {
		return "", err
	}
	defer release()

	page, err := bctx.NewPage()
	if err != nil {
		return "", fmt.Errorf("新建 page 失败: %w", err)
	}
	defer func() { _ = page.Close() }()

	if _, err := page.Goto(goofishHomeURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(quickRenewTimeoutMS),
	}); err != nil {
		m.logger.Warn("浏览器续期访问首页异常", "cookieID", cookieID, "err", err)
	}
	time.Sleep(quickRenewPageLoadWait)

	hasQuickEnter := clickQuickEnter(page)
	if hasQuickEnter {
		time.Sleep(quickRenewAfterClick)
	} else if !checkAlreadyLoggedIn(page) {
		return "", fmt.Errorf("未找到[快速进入]按钮且未检测到登录状态，需要账号密码登录")
	}

	all, err := bctx.Cookies()
	if err != nil {
		return "", fmt.Errorf("提取 cookie 失败: %w", err)
	}
	newSnapshot := cookieSnapshotFromPlaywright(all)
	if len(newSnapshot) == 0 {
		return "", fmt.Errorf("点击[快速进入]后未获取到浏览器Cookie")
	}
	cookieFromBrowser := cookierefresh.CookieStringFromSnapshot(newSnapshot)
	merged := cookierefresh.MergeOriginalFields(cookieStr, cookieFromBrowser)
	m.logger.Info("浏览器续期成功", "cookieID", cookieID, "has_quick_enter", hasQuickEnter, "cookie_count", len(newSnapshot))
	return merged, nil
}

// CookiesRefreshSnapshot 执行定时 COOKIES 续期：打开首页、刷新页面并返回完整 Cookie 快照。
func (m *Manager) CookiesRefreshSnapshot(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool) (string, []cookierefresh.BrowserCookie, error) {
	if cookieStr == "" && len(snapshot) == 0 {
		return "", nil, fmt.Errorf("Cookie为空，且无完整Cookie快照，无法执行续期")
	}
	bctx, release, err := m.newEphemeralRefreshContext(ctx, cookieID, cookieStr, snapshot, cookiesRefreshHeadless(headless))
	if err != nil {
		return "", nil, err
	}
	defer release()

	page, err := bctx.NewPage()
	if err != nil {
		return "", nil, fmt.Errorf("新建 page 失败: %w", err)
	}
	defer func() { _ = page.Close() }()

	if _, err := page.Goto(goofishHomeURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(60000),
	}); err != nil {
		m.logger.Warn("COOKIES续期访问首页异常", "cookieID", cookieID, "err", err)
	}
	time.Sleep(2 * time.Second)

	for i := 0; i < 3; i++ {
		if _, err := page.Reload(playwright.PageReloadOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			Timeout:   playwright.Float(60000),
		}); err != nil {
			m.logger.Warn("COOKIES续期 reload 异常", "cookieID", cookieID, "attempt", i+1, "err", err)
		}
		time.Sleep(2 * time.Second)
	}

	if modal, err := page.QuerySelector(".ant-modal-body"); err == nil && modal != nil {
		text, _ := modal.TextContent()
		if len(text) > 120 {
			text = text[:120]
		}
		if text != "" {
			return "", nil, fmt.Errorf("页面存在 ant-modal-body，判定续期失败: %s", text)
		}
		return "", nil, fmt.Errorf("页面存在 ant-modal-body，判定续期失败")
	}

	all, err := bctx.Cookies()
	if err != nil {
		return "", nil, fmt.Errorf("提取 cookie 失败: %w", err)
	}
	newSnapshot := cookieSnapshotFromPlaywright(all)
	if len(newSnapshot) == 0 {
		return "", nil, fmt.Errorf("页面刷新完成，但未获取到浏览器Cookie")
	}
	newCookies := cookierefresh.CookieStringFromSnapshot(newSnapshot)
	m.logger.Info("COOKIES续期成功", "cookieID", cookieID, "cookie_count", len(newSnapshot))
	return newCookies, newSnapshot, nil
}

// CookieRenewSnapshot 兼容旧调用，等价于定时 COOKIES 快照续期。
func (m *Manager) CookieRenewSnapshot(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool) (string, []cookierefresh.BrowserCookie, error) {
	return m.CookiesRefreshSnapshot(ctx, cookieID, cookieStr, snapshot, headless)
}

func clickQuickEnter(page playwright.Page) bool {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, f := range frames {
		for _, sel := range quickEnterSelectors {
			el, err := f.QuerySelector(sel)
			if err == nil && el != nil && elementVisible(el) {
				if err := el.Click(); err == nil {
					return true
				}
			}
		}
		buttons, err := f.QuerySelectorAll("button")
		if err != nil {
			continue
		}
		for _, btn := range buttons {
			text, _ := btn.TextContent()
			if strings.Contains(strings.TrimSpace(text), "快速进入") && elementVisible(btn) {
				_ = btn.Click()
				return true
			}
		}
	}
	return false
}

var quickEnterSelectors = []string{
	`button:has-text("快速进入")`,
	`button[type="submit"]:has-text("快速进入")`,
	`.fm-button:has-text("快速进入")`,
	`.fn-button:has-text("快速进入")`,
}

var loggedInSelectors = []string{
	"div.nick",
	".header-right .nick",
	".rc-virtual-list-holder-inner",
	`img[src*="img.alicdn.com"][class*="avatar"]`,
	`img[src*="img.alicdn.com"][style*="border-radius"]`,
	`.header-container img[src*="img.alicdn.com"]`,
	"#nc_1_n1z",
	".nc-container",
	".nc_scale",
	`div:has-text("请拖动下方滑块完成验证")`,
	`div:has-text("请按住滑块")`,
}

func checkAlreadyLoggedIn(page playwright.Page) bool {
	for _, sel := range loggedInSelectors {
		el, err := page.QuerySelector(sel)
		if err == nil && el != nil && elementVisible(el) {
			return true
		}
	}
	bodyText, err := page.TextContent("body")
	if err == nil && strings.Contains(bodyText, "消息") && (strings.Contains(bodyText, "订单") || strings.Contains(bodyText, "发闲置")) {
		return true
	}
	return false
}

func elementVisible(el playwright.ElementHandle) bool {
	visible, err := el.IsVisible()
	return err == nil && visible
}

func cleanSingletonFiles(userDataDir string) {
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_ = os.Remove(filepath.Join(userDataDir, name))
	}
}

func (m *Manager) newPersistentRenewContext(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool) (playwright.BrowserContext, func(), error) {
	lock := m.accountRenewLock(cookieID)
	lock.Lock()
	unlock := func() { lock.Unlock() }

	releaseSlot, err := m.acquireRenewSlot(ctx)
	if err != nil {
		unlock()
		return nil, nil, err
	}

	if err := m.init(); err != nil {
		releaseSlot()
		unlock()
		return nil, nil, err
	}

	userDataDir := filepath.Join("browser_data", "user_"+pureUserID(cookieID))
	cleanSingletonFiles(userDataDir)
	var bctx playwright.BrowserContext
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		bctx, err = m.pw.Chromium.LaunchPersistentContext(userDataDir, playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless:       playwright.Bool(headless),
			Args:           chromiumLaunchArgs(),
			ExecutablePath: chromiumExecutablePath(),
			UserAgent:      playwright.String(quickRenewUA),
			Viewport:       &playwright.Size{Width: 1280, Height: 720},
			Locale:         playwright.String(defaultLang),
			TimezoneId:     playwright.String(defaultTZ),
			Timeout:        playwright.Float(quickRenewTimeoutMS),
		})
		if err == nil {
			break
		}
		lastErr = err
		cleanSingletonFiles(userDataDir)
		time.Sleep(time.Second)
	}
	if bctx == nil {
		releaseSlot()
		unlock()
		return nil, nil, fmt.Errorf("启动持久化浏览器失败: %w", lastErr)
	}

	if err := bctx.AddInitScript(playwright.Script{Content: playwright.String(stealthScript())}); err != nil {
		m.logger.Warn("注入 stealth 脚本失败", "err", err)
	}
	if len(snapshot) > 0 {
		if err := bctx.AddCookies(snapshotToOptionalCookies(snapshot)); err != nil {
			m.logger.Warn("浏览器续期注入 Cookie 快照失败", "cookieID", cookieID, "err", err)
		}
	} else if cookieStr != "" {
		if err := addCookieStr(bctx, cookieStr); err != nil {
			m.logger.Warn("浏览器续期注入 Cookie 字符串失败", "cookieID", cookieID, "err", err)
		}
	}

	release := func() {
		_ = bctx.Close()
		releaseSlot()
		unlock()
	}
	return bctx, release, nil
}

func (m *Manager) newEphemeralRefreshContext(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool) (playwright.BrowserContext, func(), error) {
	releaseSlot, err := m.acquireRenewSlot(ctx)
	if err != nil {
		return nil, nil, err
	}
	if err := m.init(); err != nil {
		releaseSlot()
		return nil, nil, err
	}
	browser, err := m.pw.Chromium.Launch(playwright.BrowserTypeLaunchOptions{
		Headless:       playwright.Bool(headless),
		Args:           chromiumLaunchArgs(),
		ExecutablePath: chromiumExecutablePath(),
	})
	if err != nil {
		releaseSlot()
		return nil, nil, fmt.Errorf("启动 chromium 失败: %w", err)
	}
	bctx, err := browser.NewContext(playwright.BrowserNewContextOptions{
		UserAgent:  playwright.String("Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36"),
		Viewport:   &playwright.Size{Width: 1280, Height: 720},
		Locale:     playwright.String(defaultLang),
		TimezoneId: playwright.String(defaultTZ),
	})
	if err != nil {
		_ = browser.Close()
		releaseSlot()
		return nil, nil, fmt.Errorf("创建 context 失败: %w", err)
	}
	if err := bctx.AddInitScript(playwright.Script{Content: playwright.String(`Object.defineProperty(navigator, 'webdriver', { get: () => undefined }); Object.defineProperty(navigator, 'languages', { get: () => ['zh-CN', 'zh', 'en'] }); window.chrome = { runtime: {} };`)}); err != nil {
		m.logger.Warn("注入 COOKIES 续期 stealth 脚本失败", "cookieID", cookieID, "err", err)
	}
	if len(snapshot) > 0 {
		if err := bctx.AddCookies(snapshotToOptionalCookies(snapshot)); err != nil {
			m.logger.Warn("COOKIES续期注入 Cookie 快照失败", "cookieID", cookieID, "err", err)
		}
	} else if cookieStr != "" {
		if err := addCookieStr(bctx, cookieStr); err != nil {
			m.logger.Warn("COOKIES续期注入 Cookie 字符串失败", "cookieID", cookieID, "err", err)
		}
	}
	release := func() {
		_ = bctx.Close()
		_ = browser.Close()
		releaseSlot()
	}
	return bctx, release, nil
}

func quickRenewHeadless(headless bool) bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv("BROWSER_HEADLESS"))) {
	case "true", "1", "yes", "on":
		return true
	case "false", "0", "no", "off":
		return false
	default:
		return headless
	}
}

func cookiesRefreshHeadless(headless bool) bool {
	return quickRenewHeadless(headless)
}

func pureUserID(cookieID string) string {
	cookieID = sanitize(cookieID)
	parts := strings.Split(cookieID, "_")
	if len(parts) >= 2 {
		last := parts[len(parts)-1]
		if len(last) >= 10 && allDigits(last) {
			return strings.Join(parts[:len(parts)-1], "_")
		}
	}
	if cookieID == "" {
		return "unknown"
	}
	return cookieID
}

func allDigits(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
