package browser

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/mxschmitt/playwright-go"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// quickRenewPageLoadWait 保存quickRenew页码LoadWait，供当前处理流程使用
const (
	quickRenewPageLoadWait = 3 * time.Second
	quickRenewAfterClick   = 5 * time.Second
	quickRenewTimeoutMS    = 30000
	officialAutoLoginWait  = 10 * time.Second
	officialFetchDrainWait = 30 * time.Second
	officialReloadWait     = 5 * time.Second
)

// ErrInteractiveLoginRequired 保存ErrInteractive登录Required，供当前处理流程使用
var (
	ErrInteractiveLoginRequired = errors.New("浏览器会话已退出登录，需要交互式登录")
	ErrSecurityVerification     = errors.New("闲鱼要求安全验证，需要人工处理")
)

// CookieRenew 用现有 Cookie 打开闲鱼页面，尝试通过“快速进入”刷新浏览器登录态。
//
// 这个方法位于密码登录之前：如果 Cookie 仍保留可续期的浏览器会话，页面通常会给出
// 免输密码的快速进入入口。成功点击后提取完整 Cookie，可避免更重的账号密码登录。
// CookieRenew 负责登录凭证Renew相关处理。
func (m *Manager) CookieRenew(ctx context.Context, cookieID, cookieStr string, headless bool) (map[string]string, error) {
	// newCookies、err 保存newCookies、err，供当前处理流程使用
	newCookies, err := m.BrowserQuickRenew(ctx, cookieID, cookieStr, headless)
	if err != nil {
		return nil, err
	}
	return cookierefresh.ParseCookieString(newCookies), nil
}

// newPersistentPasswordContext 负责newPersistent密码上下文相关处理。
func (m *Manager) newPersistentPasswordContext(ctx context.Context, cookieID, userDataDir string, headless bool) (playwright.BrowserContext, func(), error) {
	// err 表示管理器拒绝新建持久化上下文的原因，并用于下面的短路返回。
	if err := m.beginOperation(ctx); err != nil {
		return nil, nil, err
	}
	// operationOnce 保证重复调用 release 时只减少一次活动调用计数。
	var operationOnce sync.Once
	// finishOperation 在持久化上下文释放或创建失败时结束生命周期登记。
	finishOperation := func() {
		operationOnce.Do(m.endOperation)
	}
	// lock 保存锁，供当前处理流程使用
	lock := m.accountRenewLock(cookieID)
	lock.Lock()
	// unlock 保存unlock，供当前处理流程使用
	unlock := func() { lock.Unlock() }

	// releaseSlot、err 保存releaseSlot、err，供当前处理流程使用
	releaseSlot, err := m.acquireRenewSlot(ctx)
	if err != nil {
		unlock()
		finishOperation()
		return nil, nil, err
	}
	if // err 保存err，供当前处理流程使用
	err := m.init(); err != nil {
		releaseSlot()
		unlock()
		finishOperation()
		return nil, nil, err
	}
	userDataDir, err = resolvePersistentUserDataDir(userDataDir)
	if err != nil {
		releaseSlot()
		unlock()
		finishOperation()
		return nil, nil, err
	}
	cleanSingletonFiles(userDataDir)
	// bctx 保存bctx，供当前处理流程使用
	var bctx playwright.BrowserContext
	// lastErr 保存lastErr，供当前处理流程使用
	var lastErr error
	// attempt 表示当前持久化 Chromium 的重试序号；启动失败时会先清理 profile 锁文件再重试一次。
	for attempt := 1; attempt <= 2; attempt++ {
		bctx, err = m.pw.Chromium.LaunchPersistentContext(userDataDir, passwordPersistentContextOptions(headless, m.headlessUserAgent()))
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
		finishOperation()
		return nil, nil, fmt.Errorf("启动持久化浏览器失败: %w", lastErr)
	}
	// release 保存release，供当前处理流程使用
	release := func() {
		_ = bctx.Close()
		releaseSlot()
		unlock()
		finishOperation()
	}
	return bctx, release, nil
}

// passwordPersistentContextOptions 为密码登录构造持久化上下文；无头模式仅使用实测运行时并去除 HeadlessChrome 标记的 UA。
func passwordPersistentContextOptions(headless bool, headlessUserAgent ...*string) playwright.BrowserTypeLaunchPersistentContextOptions {
	// options 是密码登录持久化 context 的完整启动配置，避免把 UA 覆盖泄漏到有头会话。
	options := playwright.BrowserTypeLaunchPersistentContextOptions{
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
	if headless && len(headlessUserAgent) > 0 {
		options.UserAgent = headlessUserAgent[0]
	}
	return options
}

// BrowserQuickRenew 使用持久化浏览器上下文执行“快速进入”Cookie 续期。
func (m *Manager) BrowserQuickRenew(ctx context.Context, cookieID, cookieStr string, headless bool) (string, error) {
	// newCookies、err 保存newCookies、err，供当前处理流程使用
	newCookies, _, err := m.BrowserQuickRenewSnapshot(ctx, cookieID, cookieStr, nil, headless)
	return newCookies, err
}

// BrowserQuickRenewSnapshot is the full-fidelity variant of BrowserQuickRenew.
// Callers that persist cookie metadata should use it directly: Chromium's final
// jar is authoritative for both deletions and attributes such as expiry/path.
// BrowserQuickRenewSnapshot 负责浏览器QuickRenewSnapshot相关处理。
func (m *Manager) BrowserQuickRenewSnapshot(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool) (string, []cookierefresh.BrowserCookie, error) {
	if cookieStr == "" && snapshot == nil {
		return "", nil, fmt.Errorf("Cookie为空，且无完整Cookie快照，无法浏览器续期")
	}
	// 持久化的扁平值始终以消息页为 canonical scope；这里也必须按 /im
	// 解释，避免漏掉或错误映射 Path=/im 的连接凭证。
	// effectiveSnapshot 保存effectiveSnapshot，供当前处理流程使用
	effectiveSnapshot := reconcileSnapshotWithCurrentCookie(snapshot, cookieStr, goofishIMURL)

	// headless 是经环境覆盖后的本次续期浏览器可见性，并决定是否必须应用运行时指纹。
	headless = quickRenewHeadless(headless)
	// bctx 是持久化续期上下文；release 释放 profile 锁和并发槽位；err 仅表示上下文创建失败。
	bctx, release, err := m.newPersistentRenewContext(ctx, cookieID, cookieStr, effectiveSnapshot, headless)
	if err != nil {
		return "", nil, err
	}
	defer release()

	// page 是已在导航前附加无头 UA 和 Client Hints 覆盖的续期页面。
	page, err := m.newBrowserPage(bctx, headless)
	if err != nil {
		return "", nil, fmt.Errorf("新建 page 失败: %w", err)
	}
	defer func() { _ = page.Close() }()

	if // err 保存err，供当前处理流程使用
	_, err := page.Goto(goofishHomeURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(quickRenewTimeoutMS),
	}); err != nil {
		m.logger.Warn("浏览器续期访问首页异常", "cookieID", cookieID, "err", err)
	}
	time.Sleep(quickRenewPageLoadWait)

	// hasQuickEnter 保存hasQuickEnter，供当前处理流程使用
	hasQuickEnter := false
	// renewErr 保存renewErr，供当前处理流程使用
	var renewErr error
	if // err 保存err，供当前处理流程使用
	err := verifyHomeLoginState(page); err != nil {
		if errors.Is(err, ErrSecurityVerification) {
			renewErr = err
		} else {
			hasQuickEnter = clickQuickEnter(page)
			if !hasQuickEnter {
				renewErr = err
			} else {
				time.Sleep(quickRenewAfterClick)
				renewErr = verifyHomeLoginState(page)
			}
		}
	}

	// newCookies、newSnapshot、err 保存newCookies、newSnapshot、err，供当前处理流程使用
	newCookies, newSnapshot, err := readAuthoritativeCookieJar(bctx, goofishIMURL)
	if err != nil {
		return "", nil, errors.Join(renewErr, err)
	}
	if len(newSnapshot) == 0 {
		renewErr = errors.Join(renewErr, fmt.Errorf("点击[快速进入]后浏览器 Cookie Jar 为空"))
	}
	if renewErr != nil {
		return newCookies, newSnapshot, renewErr
	}
	m.logger.Info("浏览器续期成功", "cookieID", cookieID, "has_quick_enter", hasQuickEnter, "cookie_count", len(newSnapshot))
	return newCookies, newSnapshot, nil
}

// CookiesRefreshSnapshot executes the optional browser-backed renewal in the
// account's persistent profile. One ordinary page load is enough to run the
// site's own auto-login plugin; repeated reloads create an avoidable risk
// signal and are deliberately not used.
// CookiesRefreshSnapshot 负责CookiesRefreshSnapshot相关处理。
func (m *Manager) CookiesRefreshSnapshot(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool) (string, []cookierefresh.BrowserCookie, bool, error) {
	if cookieStr == "" && snapshot == nil {
		return "", nil, false, fmt.Errorf("Cookie为空，且无完整Cookie快照，无法执行续期")
	}
	// effectiveSnapshot 保存effectiveSnapshot，供当前处理流程使用
	effectiveSnapshot := reconcileSnapshotWithCurrentCookie(snapshot, cookieStr, goofishIMURL)
	// headless 是经环境覆盖后的定时续期浏览器可见性，并决定页面级指纹覆盖。
	headless = cookiesRefreshHeadless(headless)
	// bctx 是定时续期使用的持久化上下文；release 归还 profile 资源；err 表示启动或注入 Cookie 失败。
	bctx, release, err := m.newPersistentRenewContext(ctx, cookieID, cookieStr, effectiveSnapshot, headless)
	if err != nil {
		return "", nil, false, err
	}
	defer release()

	// page 是已在导航前附加无头 UA 和 Client Hints 覆盖的官方续期页。
	page, err := m.newBrowserPage(bctx, headless)
	if err != nil {
		return "", nil, false, fmt.Errorf("新建 page 失败: %w", err)
	}
	defer func() { _ = page.Close() }()

	// reloaded、renewErr 保存reloaded、renewErr，供当前处理流程使用
	reloaded, renewErr := navigateOfficialRenewPage(ctx, page)
	if renewErr == nil {
		renewErr = verifyHomeLoginState(page)
	}

	// newCookies、newSnapshot、err 保存newCookies、newSnapshot、err，供当前处理流程使用
	newCookies, newSnapshot, err := readAuthoritativeCookieJar(bctx, goofishIMURL)
	if err != nil {
		return "", nil, reloaded, errors.Join(renewErr, err)
	}
	if len(newSnapshot) == 0 {
		renewErr = errors.Join(renewErr, fmt.Errorf("消息页执行后浏览器 Cookie Jar 为空"))
	}
	if renewErr != nil {
		return newCookies, newSnapshot, reloaded, renewErr
	}
	m.logger.Info("COOKIES续期成功", "cookieID", cookieID, "cookie_count", len(newSnapshot), "official_reload", reloaded)
	return newCookies, newSnapshot, reloaded, nil
}

// navigateOfficialRenewPage 负责navigateOfficialRenew页码相关处理。
func navigateOfficialRenewPage(ctx context.Context, page playwright.Page) (bool, error) {
	// silentStarted 保存silentStarted，供当前处理流程使用
	silentStarted := make(chan struct{}, 1)
	// silentSettled 保存silentSettled，供当前处理流程使用
	silentSettled := make(chan bool, 1)
	// reloadCommitted 保存reloadCommitted，供当前处理流程使用
	reloadCommitted := make(chan struct{}, 1)
	// navigationArmed 保存navigationArmed，供当前处理流程使用
	var navigationArmed atomic.Bool
	page.OnRequest(func(request playwright.Request) {
		if isSilentHasLoginURL(request.URL()) {
			select {
			case silentStarted <- struct{}{}:
			default:
			}
		}
	})
	page.OnFrameNavigated(func(frame playwright.Frame) {
		if navigationArmed.Load() && frame.ParentFrame() == nil && strings.HasPrefix(frame.URL(), goofishIMURL) {
			select {
			case reloadCommitted <- struct{}{}:
			default:
			}
		}
	})
	page.OnRequestFinished(func(request playwright.Request) {
		if isSilentHasLoginURL(request.URL()) {
			select {
			case silentSettled <- true:
			default:
			}
		}
	})
	page.OnRequestFailed(func(request playwright.Request) {
		if isSilentHasLoginURL(request.URL()) {
			select {
			case silentSettled <- false:
			default:
			}
		}
	})

	if // err 保存err，供当前处理流程使用
	_, err := page.Goto(goofishIMURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(60000),
	}); err != nil {
		return false, fmt.Errorf("COOKIES续期访问消息页: %w", err)
	}
	navigationArmed.Store(true)
	// The plugin starts after the SPA hydrates, then waits 2s before issuing
	// silentHasLogin. Observe the actual request instead of assuming hydration
	// completed at DOMContentLoaded; navigation Set-Cookie may also change the
	// sdkSilent/long-login branch.
	// observeTimer 保存observe定时器，供当前处理流程使用
	observeTimer := time.NewTimer(officialAutoLoginWait)
	// requestStarted 保存请求Started，供当前处理流程使用
	requestStarted := false
	select {
	case <-ctx.Done():
		observeTimer.Stop()
		return false, ctx.Err()
	case <-silentStarted:
		requestStarted = true
		observeTimer.Stop()
	case <-observeTimer.C:
	}
	if requestStarted {
		// The plugin's 2s Promise timeout does not abort fetch. Keep the page
		// alive until Chromium finishes the response body (and applies Set-Cookie), while
		// leaving reload/no-reload entirely to the official JavaScript.
		// drainTimer 保存drain定时器，供当前处理流程使用
		drainTimer := time.NewTimer(officialFetchDrainWait)
		// requestFinished 保存请求Finished，供当前处理流程使用
		requestFinished := false
		select {
		case requestFinished = <-silentSettled:
			drainTimer.Stop()
		case <-ctx.Done():
			drainTimer.Stop()
			return false, ctx.Err()
		case <-drainTimer.C:
		}
		if requestFinished {
			// reloadTimer 保存reload定时器，供当前处理流程使用
			reloadTimer := time.NewTimer(officialReloadWait)
			select {
			case <-reloadCommitted:
				reloadTimer.Stop()
				if // err 保存err，供当前处理流程使用
				err := page.WaitForLoadState(playwright.PageWaitForLoadStateOptions{
					State: playwright.LoadStateDomcontentloaded, Timeout: playwright.Float(10000),
				}); err != nil {
					return true, fmt.Errorf("等待官网续期 reload 完成: %w", err)
				}
				return true, nil
			case <-ctx.Done():
				reloadTimer.Stop()
				return false, ctx.Err()
			case <-reloadTimer.C:
			}
		}
	}
	// Absence of a reload is the official outcome for fatigue, timeout or a
	// non-100 business result.
	return false, nil
}

// readAuthoritativeCookieJar 负责readAuthoritative登录凭证Jar相关处理。
func readAuthoritativeCookieJar(bctx playwright.BrowserContext, rawURL string) (string, []cookierefresh.BrowserCookie, error) {
	// all、err 保存all、err，供当前处理流程使用
	all, err := bctx.Cookies()
	if err != nil {
		return "", nil, fmt.Errorf("提取浏览器 Cookie Jar: %w", err)
	}
	// snapshot 保存snapshot，供当前处理流程使用
	snapshot := cookieSnapshotFromPlaywright(all)
	if snapshot == nil {
		snapshot = []cookierefresh.BrowserCookie{}
	}
	return currentCookieHeader(snapshot, rawURL), snapshot, nil
}

// isSilentHasLoginURL 负责isSilentHas登录URL相关处理。
func isSilentHasLoginURL(rawURL string) bool {
	return strings.Contains(rawURL, "/newlogin/silentHasLogin.do")
}

// reconcileSnapshotWithCurrentCookie 负责reconcileSnapshotWithCurrent登录凭证相关处理。
func reconcileSnapshotWithCurrentCookie(snapshot []cookierefresh.BrowserCookie, cookieStr, rawURL string) []cookierefresh.BrowserCookie {
	if snapshot == nil {
		return nil
	}
	if len(snapshot) == 0 {
		return []cookierefresh.BrowserCookie{}
	}
	if strings.TrimSpace(cookieStr) == "" {
		return cookierefresh.NormalizeSnapshot(snapshot)
	}
	return credentialCookieSnapshotForURL(snapshot, parseCookieStr(cookieStr), rawURL)
}

// CookieRenewSnapshot 兼容旧调用，等价于定时 COOKIES 快照续期。
func (m *Manager) CookieRenewSnapshot(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool) (string, []cookierefresh.BrowserCookie, error) {
	// cookies、refreshed、err 保存cookies、refreshed、err，供当前处理流程使用
	cookies, refreshed, _, err := m.CookiesRefreshSnapshot(ctx, cookieID, cookieStr, snapshot, headless)
	return cookies, refreshed, err
}

// clickQuickEnter 负责clickQuickEnter相关处理。
func clickQuickEnter(page playwright.Page) bool {
	// frames 保存frames，供当前处理流程使用
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	// f 表示当前遍历过程中的f
	for _, f := range frames {
		// sel 表示当前遍历过程中的sel
		for _, sel := range quickEnterSelectors {
			// el、err 保存el、err，供当前处理流程使用
			el, err := f.QuerySelector(sel)
			if err == nil && el != nil && elementVisible(el) {
				if // err 保存err，供当前处理流程使用
				err := el.Click(); err == nil {
					return true
				}
			}
		}
		// buttons、err 保存buttons、err，供当前处理流程使用
		buttons, err := f.QuerySelectorAll("button")
		if err != nil {
			continue
		}
		// btn 表示当前遍历过程中的btn
		for _, btn := range buttons {
			// text 保存文本，供当前处理流程使用
			text, _ := btn.TextContent()
			if strings.Contains(strings.TrimSpace(text), "快速进入") && elementVisible(btn) {
				_ = btn.Click()
				return true
			}
		}
	}
	return false
}

// quickEnterSelectors 保存quickEnterSelectors，供当前处理流程使用
var quickEnterSelectors = []string{
	`button:has-text("快速进入")`,
	`button[type="submit"]:has-text("快速进入")`,
	`.fm-button:has-text("快速进入")`,
	`.fn-button:has-text("快速进入")`,
}

// verifyHomeLoginState 负责verifyHome登录状态相关处理。
func verifyHomeLoginState(page playwright.Page) error {
	if pageHasSecurityVerification(page) {
		return ErrSecurityVerification
	}
	// result、err 保存result、err，供当前处理流程使用
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
	// signals、ok 保存signals、ok，供当前处理流程使用
	signals, ok := result.(map[string]any)
	if !ok {
		return fmt.Errorf("浏览器登录状态返回格式异常")
	}
	// personal 保存personal，供当前处理流程使用
	personal, _ := signals["personal"].(bool)
	// login 保存登录，供当前处理流程使用
	login, _ := signals["login"].(bool)
	if personal && !login {
		return nil
	}
	if login {
		return ErrInteractiveLoginRequired
	}
	return fmt.Errorf("%w: 页面未出现个人主页入口", ErrInteractiveLoginRequired)
}

// pageHasSecurityVerification 负责页码HasSecurityVerification相关处理。
func pageHasSecurityVerification(page playwright.Page) bool {
	// frame 表示当前遍历过程中的frame
	for _, frame := range page.Frames() {
		// lowerURL 保存lowerURL，供当前处理流程使用
		lowerURL := strings.ToLower(frame.URL())
		// marker 表示当前遍历过程中的marker
		for _, marker := range []string{"photoverify", "normal_validate", "identity_verify", "baxia-punish", "/punish"} {
			if strings.Contains(lowerURL, marker) {
				return true
			}
		}
		// content、err 保存content、err，供当前处理流程使用
		content, err := frame.Content()
		if err != nil {
			continue
		}
		// marker 表示当前遍历过程中的marker
		for _, marker := range []string{"拍摄脸部", "请拖动下方滑块完成验证", "请按住滑块", "安全验证未通过"} {
			if strings.Contains(content, marker) {
				return true
			}
		}
	}
	return false
}

// elementVisible 负责elementVisible相关处理。
func elementVisible(el playwright.ElementHandle) bool {
	// visible、err 保存visible、err，供当前处理流程使用
	visible, err := el.IsVisible()
	return err == nil && visible
}

// cleanSingletonFiles 负责cleanSingleton文件列表相关处理。
func cleanSingletonFiles(userDataDir string) {
	// name 表示当前遍历过程中的名称
	for _, name := range []string{"SingletonLock", "SingletonSocket", "SingletonCookie"} {
		_ = os.Remove(filepath.Join(userDataDir, name))
	}
}

// newPersistentRenewContext 负责newPersistentRenew上下文相关处理。
func (m *Manager) newPersistentRenewContext(ctx context.Context, cookieID, cookieStr string, snapshot []cookierefresh.BrowserCookie, headless bool, captchaProfile ...bool) (playwright.BrowserContext, func(), error) {
	// err 表示管理器拒绝新建续期上下文的原因，并用于下面的短路返回。
	if err := m.beginOperation(ctx); err != nil {
		return nil, nil, err
	}
	// operationOnce 保证重复调用 release 时只减少一次活动调用计数。
	var operationOnce sync.Once
	// finishOperation 在续期上下文释放或创建失败时结束生命周期登记。
	finishOperation := func() {
		operationOnce.Do(m.endOperation)
	}
	// lock 保存锁，供当前处理流程使用
	lock := m.accountRenewLock(cookieID)
	lock.Lock()
	// unlock 保存unlock，供当前处理流程使用
	unlock := func() { lock.Unlock() }

	// releaseSlot、err 保存releaseSlot、err，供当前处理流程使用
	releaseSlot, err := m.acquireRenewSlot(ctx)
	if err != nil {
		unlock()
		finishOperation()
		return nil, nil, err
	}

	if // err 保存err，供当前处理流程使用
	err := m.init(); err != nil {
		releaseSlot()
		unlock()
		finishOperation()
		return nil, nil, err
	}

	// userDataDir、err 保存用户数据Dir、err，供当前处理流程使用
	userDataDir, err := resolvePersistentUserDataDir(filepath.Join("browser_data", "user_"+pureUserID(cookieID)))
	if err != nil {
		releaseSlot()
		unlock()
		finishOperation()
		return nil, nil, err
	}
	cleanSingletonFiles(userDataDir)
	// viewport 保存viewport，供当前处理流程使用
	viewport := &playwright.Size{Width: 1280, Height: 720}
	if len(captchaProfile) > 0 && captchaProfile[0] {
		viewport = &playwright.Size{Width: 1980, Height: 1024}
	}
	// bctx 保存bctx，供当前处理流程使用
	var bctx playwright.BrowserContext
	// lastErr 保存lastErr，供当前处理流程使用
	var lastErr error
	// attempt 表示当前持久化 profile 的启动序号；失败后只进行一次清理后的重试。
	for attempt := 1; attempt <= 2; attempt++ {
		// options 保存本次启动的独立配置，避免重试时复用可能被 Playwright 修改的对象。
		options := playwright.BrowserTypeLaunchPersistentContextOptions{
			Headless:       playwright.Bool(headless),
			Args:           chromiumLaunchArgs(),
			ExecutablePath: chromiumExecutablePath(),
			Viewport:       viewport,
			Locale:         playwright.String(defaultLang),
			TimezoneId:     playwright.String(defaultTZ),
			Timeout:        playwright.Float(quickRenewTimeoutMS),
		}
		if headless {
			options.UserAgent = m.headlessUserAgent()
		}
		bctx, err = m.pw.Chromium.LaunchPersistentContext(userDataDir, options)
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
		finishOperation()
		return nil, nil, fmt.Errorf("启动持久化浏览器失败: %w", lastErr)
	}

	if // err 保存err，供当前处理流程使用
	err := bctx.AddInitScript(playwright.Script{Content: playwright.String(stealthScript())}); err != nil {
		m.logger.Warn("注入 stealth 脚本失败", "err", err)
	}
	if snapshot != nil {
		if // err 保存err，供当前处理流程使用
		err := bctx.ClearCookies(); err != nil {
			_ = bctx.Close()
			releaseSlot()
			unlock()
			finishOperation()
			return nil, nil, fmt.Errorf("浏览器续期清理旧 Cookie 失败: %w", err)
		}
		if // err 保存err，供当前处理流程使用
		err := bctx.AddCookies(snapshotToOptionalCookies(snapshot)); err != nil {
			_ = bctx.Close()
			releaseSlot()
			unlock()
			finishOperation()
			return nil, nil, fmt.Errorf("浏览器续期注入 Cookie 快照失败: %w", err)
		}
	} else if cookieStr != "" {
		if // err 保存err，供当前处理流程使用
		err := addCookieStr(bctx, cookieStr); err != nil {
			_ = bctx.Close()
			releaseSlot()
			unlock()
			finishOperation()
			return nil, nil, fmt.Errorf("浏览器续期注入 Cookie 字符串失败: %w", err)
		}
	}

	// release 保存release，供当前处理流程使用
	release := func() {
		_ = bctx.Close()
		releaseSlot()
		unlock()
		finishOperation()
	}
	return bctx, release, nil
}

// quickRenewHeadless 负责quickRenewHeadless相关处理。
func quickRenewHeadless(headless bool) bool {
	return resolveHeadlessRequest(headless)
}

// ResolveHeadless returns the browser headless mode from account ShowBrowser plus
// the optional BROWSER_HEADLESS override. All browser-backed login/renewal flows
// use this resolver so headed/headless only changes visibility, not behavior.
// ResolveHeadless 处理Headless。
func ResolveHeadless(showBrowser bool) bool {
	return resolveHeadlessRequest(!showBrowser)
}

// resolveHeadlessRequest 负责resolveHeadless请求相关处理。
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

// cookiesRefreshHeadless 负责cookiesRefreshHeadless相关处理。
func cookiesRefreshHeadless(headless bool) bool {
	return quickRenewHeadless(headless)
}

// pureUserID 负责pure用户ID相关处理。
func pureUserID(cookieID string) string {
	cookieID = sanitize(cookieID)
	// parts 保存parts，供当前处理流程使用
	parts := strings.Split(cookieID, "_")
	if len(parts) >= 2 {
		// last 保存last，供当前处理流程使用
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

// allDigits 负责allDigits相关处理。
func allDigits(s string) bool {
	if s == "" {
		return false
	}
	// r 表示当前遍历过程中的r
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}
