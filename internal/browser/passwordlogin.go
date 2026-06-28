package browser

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"
)

// loginFormSelectors 登录表单选择器，主页面和 iframe 中都要找。
var loginIDSelectors = []string{
	"#fm-login-id", `input[name="fm-login-id"]`,
	`input[placeholder*="手机号"]`, `input[placeholder*="邮箱"]`,
	".fm-login-id",
}
var loginPwdSelectors = []string{
	"#fm-login-password", `input[type="password"]`,
}
var loginBtnSelectors = []string{
	"button.password-login", `button[type="submit"]`,
}
var loginSuccessSelectors = []string{
	".rc-virtual-list-holder-inner", // IM 页面侧边栏有子元素则已登录
}

// PasswordLogin 用账号密码通过浏览器登录闲鱼，返回完整 cookie map。
// 移植自 xianyu_slider_stealth.login_with_password_playwright。
// userDataDir：空字符串用临时目录，非空则持久化（跨次复用 session）。
func (m *Manager) PasswordLogin(ctx context.Context, account, password, cookieID, userDataDir string, headless bool) (map[string]string, error) {
	if err := m.init(); err != nil {
		return nil, err
	}

	if userDataDir == "" {
		userDataDir = filepath.Join("browser_data", "user_"+sanitize(cookieID))
	}

	bctx, err := m.pw.Chromium.LaunchPersistentContext(userDataDir, playwright.BrowserTypeLaunchPersistentContextOptions{
		Headless:   playwright.Bool(headless),
		Args:       chromiumLaunchArgs(),
		UserAgent:  playwright.String(defaultUA),
		Viewport:   &playwright.Size{Width: 1980, Height: 1024},
		Locale:     playwright.String(defaultLang),
		TimezoneId: playwright.String(defaultTZ),
	})
	if err != nil {
		return nil, fmt.Errorf("启动持久化浏览器失败: %w", err)
	}
	defer func() { _ = bctx.Close() }()

	if err := bctx.AddInitScript(playwright.Script{Content: playwright.String(stealthScript())}); err != nil {
		m.logger.Warn("注入 stealth 脚本失败", "err", err)
	}

	page, err := bctx.NewPage()
	if err != nil {
		return nil, fmt.Errorf("新建 page 失败: %w", err)
	}

	if _, err := page.Goto(goofishIMURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateNetworkidle,
		Timeout:   playwright.Float(60000),
	}); err != nil {
		m.logger.Warn("访问 goofish.com/im 超时", "err", err)
	}

	// 检查是否已经登录。
	if checkLoginSuccess(page) {
		m.logger.Info("密码登录：已有有效 session，无需重新登录", "cookieID", cookieID)
		return extractPageCookies(page)
	}

	// 在主页和所有 iframe 中找登录表单。
	idEl, pwdEl, submitEl := findLoginForm(page)
	if idEl == nil {
		return nil, fmt.Errorf("未找到登录表单，可能页面结构已变更")
	}

	if err := idEl.Click(); err == nil {
		_ = page.Fill(getCSSSelector(idEl, loginIDSelectors), account)
	}
	if err := pwdEl.Click(); err == nil {
		_ = page.Fill(getCSSSelector(pwdEl, loginPwdSelectors), password)
	}
	// 同意协议复选框（若存在）。
	if cb, err := page.QuerySelector("#fm-agreement-checkbox"); err == nil && cb != nil {
		if checked, _ := cb.GetAttribute("checked"); checked == "" {
			_ = cb.Click()
		}
	}
	if submitEl != nil {
		_ = submitEl.Click()
	}
	time.Sleep(3 * time.Second)

	// 登录后可能出现滑块。
	content, _ := page.Content()
	if strings.Contains(content, "nc-container") || strings.Contains(content, "scratch-captcha") {
		scratch := isScratchCaptcha(content)
		m.logger.Info("密码登录后出现滑块，自动处理")
		if err := solveSlider(page, scratch, m.logger); err != nil {
			m.logger.Warn("密码登录滑块处理失败", "err", err)
		}
		time.Sleep(2 * time.Second)
	}

	if !checkLoginSuccess(page) {
		// 采集错误信息。
		errMsg := ""
		if el, err := page.QuerySelector(".login-error-msg"); err == nil && el != nil {
			errMsg, _ = el.TextContent()
		}
		if errMsg == "" {
			c, _ := page.Content()
			for _, kw := range []string{"账密错误", "账号密码错误", "用户名或密码错误", "密码错误"} {
				if strings.Contains(c, kw) {
					errMsg = kw
					break
				}
			}
		}
		if errMsg == "" {
			errMsg = "登录失败"
		}
		return nil, fmt.Errorf("密码登录失败: %s", errMsg)
	}

	m.logger.Info("密码登录成功", "cookieID", cookieID)
	return extractPageCookies(page)
}

func checkLoginSuccess(page playwright.Page) bool {
	for _, sel := range loginSuccessSelectors {
		el, err := page.QuerySelector(sel)
		if err != nil || el == nil {
			continue
		}
		// 子元素数 > 0 则已登录。
		count, err := page.Evaluate(`(sel) => {
			const el = document.querySelector(sel);
			return el ? el.children.length : 0;
		}`, sel)
		if err == nil {
			if n, ok := count.(float64); ok && n > 0 {
				return true
			}
		}
	}
	return false
}

func findLoginForm(page playwright.Page) (idEl, pwdEl, submitEl playwright.ElementHandle) {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, f := range frames {
		id := queryFirst(f, loginIDSelectors)
		if id == nil {
			continue
		}
		pwd := queryFirst(f, loginPwdSelectors)
		submit := queryFirst(f, loginBtnSelectors)
		return id, pwd, submit
	}
	return nil, nil, nil
}

// getCSSSelector 返回元素对应的第一个匹配选择器（用于 page.Fill）。
func getCSSSelector(el playwright.ElementHandle, selectors []string) string {
	if el == nil {
		return selectors[0]
	}
	return selectors[0] // page.Fill 用首选器即可，元素已 Click
}

func sanitize(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return r.Replace(s)
}
