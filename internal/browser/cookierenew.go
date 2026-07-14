package browser

import (
	"context"
	"errors"
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
)

var (
	ErrInteractiveLoginRequired = errors.New("浏览器会话已退出登录，需要交互式登录")
	ErrSecurityVerification     = errors.New("闲鱼要求安全验证，需要人工处理")
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

func (m *Manager) newPersistentPasswordContext(ctx context.Context, cookieID, userDataDir string, headless bool) (playwright.BrowserContext, func(), error) {
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
	userDataDir, err = resolvePersistentUserDataDir(userDataDir)
	if err != nil {
		releaseSlot()
		unlock()
		return nil, nil, err
	}
	cleanSingletonFiles(userDataDir)
	var bctx playwright.BrowserContext
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		bctx, err = m.pw.Chromium.LaunchPersistentContext(userDataDir, passwordPersistentContextOptions(headless))
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
	release := func() {
		_ = bctx.Close()
		releaseSlot()
		unlock()
	}
	return bctx, release, nil
}

func passwordPersistentContextOptions(headless bool) playwright.BrowserTypeLaunchPersistentContextOptions {
	return playwright.BrowserTypeLaunchPersistentContextOptions{
		Headless:          playwright.Bool(headless),
		Args:              chromiumLaunchArgs(),
		ExecutablePath:    chromiumExecutablePath(),
		Viewport:          &playwright.Size{Width: 1980, Height: 1024},
		Locale:            playwright.String(defaultLang),
		TimezoneId:        playwright.String(defaultTZ),
		AcceptDownloads:   playwright.Bool(true),
		IgnoreHttpsErrors: playwright.Bool(true),
		ExtraHttpHeaders: map[string]string{
			"Accept-Language": "zh-CN,zh;q=0.9,en;q=0.8",
		},
		Timeout: playwright.Float(quickRenewTimeoutMS),
	}
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

	hasQuickEnter := false
	if err := verifyHomeLoginState(page); err != nil {
		if errors.Is(err, ErrSecurityVerification) {
			return "", err
		}
		hasQuickEnter = clickQuickEnter(page)
		if !hasQuickEnter {
			return "", err
		}
		time.Sleep(quickRenewAfterClick)
		if err := verifyHomeLoginState(page); err != nil {
			return "", err
		}
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

// CookiesRefreshSnapshot executes the optional browser-backed renewal in the
// account's persistent profile. One ordinary page load is enough to run the
// site's own auto-login plugin; repeated reloads create an avoidable risk
// signal and are deliberately not used.
func (m *Manager) CookiesRefreshSnapshot(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool) (string, []cookierefresh.BrowserCookie, error) {
	if cookieStr == "" && len(snapshot) == 0 {
		return "", nil, fmt.Errorf("Cookie为空，且无完整Cookie快照，无法执行续期")
	}
	bctx, release, err := m.newPersistentRenewContext(ctx, cookieID, cookieStr, snapshot, cookiesRefreshHeadless(headless))
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
	time.Sleep(quickRenewPageLoadWait)
	if err := verifyHomeLoginState(page); err != nil {
		return "", nil, err
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

func checkAlreadyLoggedIn(page playwright.Page) bool {
	return verifyHomeLoginState(page) == nil
}

func verifyHomeLoginState(page playwright.Page) error {
	if pageHasSecurityVerification(page) {
		return ErrSecurityVerification
	}
	result, err := page.Evaluate(`() => {
		const visible = (el) => {
			const rect = el.getBoundingClientRect();
			const style = getComputedStyle(el);
			return rect.width > 0 && rect.height > 0 && style.display !== 'none' && style.visibility !== 'hidden';
		};
		const links = Array.from(document.querySelectorAll('a'));
		const personal = links.some((el) => {
			if (!visible(el)) return false;
			try { return new URL(el.href, location.href).pathname === '/personal'; } catch (_) { return false; }
		});
		const login = Array.from(document.querySelectorAll('a,button,[role="button"]')).some((el) => {
			if (!visible(el) || (el.textContent || '').trim() !== '登录') return false;
			return el.getBoundingClientRect().top < 120;
		});
		return { personal, login };
	}`)
	if err != nil {
		return fmt.Errorf("读取浏览器登录状态失败: %w", err)
	}
	signals, ok := result.(map[string]any)
	if !ok {
		return fmt.Errorf("浏览器登录状态返回格式异常")
	}
	personal, _ := signals["personal"].(bool)
	login, _ := signals["login"].(bool)
	if personal && !login {
		return nil
	}
	if login {
		return ErrInteractiveLoginRequired
	}
	return fmt.Errorf("%w: 页面未出现个人主页入口", ErrInteractiveLoginRequired)
}

func pageHasSecurityVerification(page playwright.Page) bool {
	for _, frame := range page.Frames() {
		lowerURL := strings.ToLower(frame.URL())
		for _, marker := range []string{"photoverify", "normal_validate", "identity_verify", "baxia-punish", "/punish"} {
			if strings.Contains(lowerURL, marker) {
				return true
			}
		}
		content, err := frame.Content()
		if err != nil {
			continue
		}
		for _, marker := range []string{"拍摄脸部", "请拖动下方滑块完成验证", "请按住滑块", "安全验证未通过"} {
			if strings.Contains(content, marker) {
				return true
			}
		}
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

func (m *Manager) newPersistentRenewContext(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool, captchaProfile ...bool) (playwright.BrowserContext, func(), error) {
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

	userDataDir, err := resolvePersistentUserDataDir(filepath.Join("browser_data", "user_"+pureUserID(cookieID)))
	if err != nil {
		releaseSlot()
		unlock()
		return nil, nil, err
	}
	cleanSingletonFiles(userDataDir)
	viewport := &playwright.Size{Width: 1280, Height: 720}
	if len(captchaProfile) > 0 && captchaProfile[0] {
		viewport = &playwright.Size{Width: 1980, Height: 1024}
	}
	var bctx playwright.BrowserContext
	var lastErr error
	for attempt := 1; attempt <= 2; attempt++ {
		bctx, err = m.pw.Chromium.LaunchPersistentContext(userDataDir, playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless:       playwright.Bool(headless),
			Args:           chromiumLaunchArgs(),
			ExecutablePath: chromiumExecutablePath(),
			Viewport:       viewport,
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
		if err := bctx.ClearCookies(); err != nil {
			_ = bctx.Close()
			releaseSlot()
			unlock()
			return nil, nil, fmt.Errorf("浏览器续期清理旧 Cookie 失败: %w", err)
		}
		if err := bctx.AddCookies(snapshotToOptionalCookies(snapshot)); err != nil {
			_ = bctx.Close()
			releaseSlot()
			unlock()
			return nil, nil, fmt.Errorf("浏览器续期注入 Cookie 快照失败: %w", err)
		}
	} else if cookieStr != "" {
		if err := addCookieStr(bctx, cookieStr); err != nil {
			_ = bctx.Close()
			releaseSlot()
			unlock()
			return nil, nil, fmt.Errorf("浏览器续期注入 Cookie 字符串失败: %w", err)
		}
	}

	release := func() {
		_ = bctx.Close()
		releaseSlot()
		unlock()
	}
	return bctx, release, nil
}

func quickRenewHeadless(headless bool) bool {
	return resolveHeadlessRequest(headless)
}

// ResolveHeadless returns the browser headless mode from account ShowBrowser plus
// the optional BROWSER_HEADLESS override. All browser-backed login/renewal flows
// use this resolver so headed/headless only changes visibility, not behavior.
func ResolveHeadless(showBrowser bool) bool {
	return resolveHeadlessRequest(!showBrowser)
}

func resolveHeadlessRequest(headless bool) bool {
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
