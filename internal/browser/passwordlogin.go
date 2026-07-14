package browser

import (
	"context"
	"encoding/base64"
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

const (
	passwordVerificationWaitInterval = 10 * time.Second
	passwordVerificationMaxWait      = 5 * time.Minute
	passwordLoginPageLoadWait        = 2 * time.Second
	passwordLoginTabWait             = 1500 * time.Millisecond
	passwordLoginAfterSubmitWait     = 3 * time.Second
	passwordLoginCompletionWait      = 5 * time.Second
)

// PasswordLogin 用账号密码通过浏览器登录闲鱼，返回完整 cookie map。
// 移植自 xianyu_slider_stealth.login_with_password_playwright。
// userDataDir：空字符串使用按账号划分的默认持久化目录。
func (m *Manager) PasswordLogin(ctx context.Context, account, password, cookieID, userDataDir string, headless bool) (map[string]string, error) {
	return m.passwordLogin(ctx, account, password, cookieID, userDataDir, headless, nil)
}

// PasswordLoginWithEvents 在密码登录过程中上报中间状态。
func (m *Manager) PasswordLoginWithEvents(ctx context.Context, account, password, cookieID, userDataDir string, headless bool, onEvent PasswordLoginEventHandler) (map[string]string, error) {
	return m.passwordLogin(ctx, account, password, cookieID, userDataDir, headless, onEvent)
}

func (m *Manager) passwordLogin(ctx context.Context, account, password, cookieID, userDataDir string, headless bool, onEvent PasswordLoginEventHandler) (map[string]string, error) {
	if strings.TrimSpace(account) == "" || strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("账号或密码不能为空")
	}

	if strings.TrimSpace(userDataDir) == "" {
		userDataDir = filepath.Join("browser_data", "user_"+pureUserID(cookieID))
	}
	headless = quickRenewHeadless(headless)

	bctx, release, err := m.newPersistentPasswordContext(ctx, cookieID, userDataDir, headless)
	if err != nil {
		return nil, err
	}
	defer release()

	page, err := bctx.NewPage()
	if err != nil {
		return nil, fmt.Errorf("新建 page 失败: %w", err)
	}

	if _, err := page.Goto(goofishIMURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	}); err != nil {
		m.logger.Warn("访问 goofish.com/im 超时", "err", err)
	}
	time.Sleep(passwordLoginPageLoadWait)

	if clickQuickEnter(page) {
		m.logger.Info("密码登录：已点击[快速进入]，等待页面刷新", "cookieID", cookieID)
		time.Sleep(quickRenewAfterClick)
		cookies, err := extractPageCookies(page)
		if err == nil && quickEnterCookiesUsable(cookies) {
			m.logger.Info("密码登录：快速进入成功，跳过账号密码输入", "cookieID", cookieID)
			return cookies, nil
		}
		m.logger.Info("密码登录：快速进入未获取到有效 Cookie，继续账号密码登录", "cookieID", cookieID)
		if _, err := page.Goto(goofishIMURL, playwright.PageGotoOptions{
			WaitUntil: playwright.WaitUntilStateDomcontentloaded,
			Timeout:   playwright.Float(30000),
		}); err != nil {
			m.logger.Warn("密码登录：快速进入失败后重新访问 IM 页面异常", "cookieID", cookieID, "err", err)
		}
		time.Sleep(2 * time.Second)
	}

	if clickPasswordLoginTab(page) {
		time.Sleep(passwordLoginTabWait)
	}

	// 在主页和所有 iframe 中找登录表单。
	idEl, pwdEl, submitEl := findLoginForm(page)
	if idEl == nil {
		time.Sleep(2 * time.Second)
		if handled := detectAndHandlePasswordSlider(page, m.logger); handled {
			m.logger.Info("密码登录：未找到表单时已处理滑块", "cookieID", cookieID)
		}
		time.Sleep(3 * time.Second)
		if checkLoginSuccess(page) {
			return extractPageCookies(page)
		}
		return nil, fmt.Errorf("未找到登录表单且未检测到登录状态")
	}
	if pwdEl == nil {
		return nil, fmt.Errorf("未找到密码输入框，可能页面结构已变更")
	}

	time.Sleep(time.Second)
	_ = idEl.Fill(account)
	time.Sleep(secondsDuration(randomFloat(0.5, 1.0)))
	_ = pwdEl.Fill(password)
	time.Sleep(secondsDuration(randomFloat(0.5, 1.0)))
	// 同意协议复选框（若存在）。
	if cb := findPasswordElement(page, []string{"#fm-agreement-checkbox"}); cb != nil {
		checked, _ := cb.Evaluate(`el => Boolean(el.checked)`)
		if isChecked, _ := checked.(bool); !isChecked {
			_ = cb.Click()
			time.Sleep(300 * time.Millisecond)
		}
	}
	time.Sleep(time.Second)
	if submitEl != nil {
		_ = submitEl.Click()
	}
	time.Sleep(passwordLoginAfterSubmitWait)

	// 登录后可能出现滑块。
	if detectAndHandlePasswordSlider(page, m.logger) {
		m.logger.Info("密码登录后滑块处理完成")
	}
	time.Sleep(passwordLoginCompletionWait)
	time.Sleep(time.Second)
	if detectAndHandlePasswordSlider(page, m.logger) {
		time.Sleep(3 * time.Second)
	}
	time.Sleep(time.Second)

	if !checkLoginSuccess(page) {
		return m.handlePasswordLoginPending(ctx, page, onEvent)
	}

	m.logger.Info("密码登录成功", "cookieID", cookieID)
	return extractPageCookies(page)
}

func findPasswordElement(page playwright.Page, selectors []string) playwright.ElementHandle {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, frame := range frames {
		if el := queryFirst(frame, selectors); el != nil {
			return el
		}
	}
	return nil
}

func clickPasswordLoginTab(page playwright.Page) bool {
	el := findPasswordElement(page, []string{"a.password-login-tab-item"})
	return el != nil && el.Click() == nil
}

func detectAndHandlePasswordSlider(page playwright.Page, logger sliderLogger) bool {
	content, _ := page.Content()
	if !strings.Contains(content, "nc-container") && !strings.Contains(content, "scratch-captcha") {
		// 参考实现把“未发现滑块”也视为检测流程成功；调用方据此保持相同的等待节奏。
		return true
	}
	if err := solveSlider(page, isScratchCaptcha(content), logger); err != nil {
		logger.Warn("密码登录滑块处理失败", "err", err)
		return false
	}
	return true
}

func quickEnterCookiesUsable(cookies map[string]string) bool {
	if len(cookies) == 0 {
		return false
	}
	return strings.TrimSpace(cookies["unb"]) != ""
}

func (m *Manager) handlePasswordLoginPending(ctx context.Context, page playwright.Page, onEvent PasswordLoginEventHandler) (map[string]string, error) {
	if msg := passwordLoginErrorFromPage(page); msg != "" {
		if onEvent != nil {
			onEvent(PasswordLoginEventFromMessage(msg))
		}
		return nil, fmt.Errorf("密码登录失败: %s", msg)
	}
	if event, ok := passwordBaxiaEventFromPage(page); ok {
		if onEvent != nil {
			onEvent(event)
		}
		return nil, fmt.Errorf("密码登录失败: %s", event.Message)
	}
	if event, ok := passwordVerificationEventFromPage(page); ok {
		if onEvent != nil {
			onEvent(event)
		}
		cookies, err := waitPasswordVerification(ctx, page, onEvent)
		if err == nil {
			m.logger.Info("密码登录人工验证完成")
			return cookies, nil
		}
		return nil, err
	}
	errMsg := "登录失败"
	if onEvent != nil {
		onEvent(PasswordLoginEventFromMessage(errMsg))
	}
	return nil, fmt.Errorf("密码登录失败: %s", errMsg)
}

func waitPasswordVerification(ctx context.Context, page playwright.Page, onEvent PasswordLoginEventHandler) (map[string]string, error) {
	ticker := time.NewTicker(passwordVerificationWaitInterval)
	defer ticker.Stop()
	timer := time.NewTimer(passwordVerificationMaxWait)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, fmt.Errorf("密码登录失败: 人工验证超时")
		case <-ticker.C:
		}
		if checkLoginSuccess(page) {
			return extractPageCookies(page)
		}
		if msg := passwordLoginErrorFromPage(page); msg != "" {
			return nil, fmt.Errorf("密码登录失败: %s", msg)
		}
		if event, ok := passwordBaxiaEventFromPage(page); ok {
			return nil, fmt.Errorf("密码登录失败: %s", event.Message)
		}
		if event, ok := passwordVerificationEventFromPage(page); ok && onEvent != nil {
			onEvent(event)
		}
	}
}

func passwordLoginErrorFromPage(page playwright.Page) string {
	if el, err := page.QuerySelector(".login-error-msg"); err == nil && el != nil {
		if msg, _ := el.TextContent(); strings.TrimSpace(msg) != "" {
			return strings.TrimSpace(msg)
		}
	}
	for _, htmlText := range passwordLoginHTMLs(page) {
		if msg := detectPasswordLoginErrorHTML(htmlText); msg != "" {
			return msg
		}
	}
	return ""
}

func passwordBaxiaEventFromPage(page playwright.Page) (PasswordLoginEvent, bool) {
	for _, htmlText := range passwordLoginHTMLs(page) {
		if event, ok := detectPasswordBaxiaPunishHTML(htmlText); ok {
			return event, true
		}
	}
	return PasswordLoginEvent{}, false
}

func passwordVerificationEventFromPage(page playwright.Page) (PasswordLoginEvent, bool) {
	if event, ok := passwordVerificationEventFromContent(pageContent(page), page.URL()); ok {
		event.ScreenshotPath = firstNonEmptyString(event.ScreenshotPath, passwordVerificationScreenshot(page))
		return event, true
	}
	for _, frame := range page.Frames() {
		if event, ok := passwordVerificationEventFromContent(frameContent(frame), frame.URL()); ok {
			event.ScreenshotPath = firstNonEmptyString(event.ScreenshotPath, passwordVerificationScreenshot(page))
			return event, true
		}
	}
	return PasswordLoginEvent{}, false
}

func passwordVerificationEventFromContent(htmlText, frameURL string) (PasswordLoginEvent, bool) {
	event, ok := detectPasswordVerificationHTML(htmlText)
	if !ok {
		return PasswordLoginEvent{}, false
	}
	if event.VerificationURL == "" && looksLikeVerificationURL(frameURL) {
		event.VerificationURL = frameURL
	}
	return event, true
}

func passwordLoginHTMLs(page playwright.Page) []string {
	htmls := []string{pageContent(page)}
	for _, frame := range page.Frames() {
		htmls = append(htmls, frameContent(frame))
	}
	return htmls
}

func pageContent(page playwright.Page) string {
	content, _ := page.Content()
	return content
}

func frameContent(frame playwright.Frame) string {
	content, _ := frame.Content()
	return content
}

func passwordVerificationScreenshot(page playwright.Page) string {
	raw, err := page.Screenshot(playwright.PageScreenshotOptions{
		FullPage: playwright.Bool(true),
		Timeout:  playwright.Float(5000),
	})
	if err != nil || len(raw) == 0 {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
}

func looksLikeVerificationURL(raw string) bool {
	lower := strings.ToLower(raw)
	return containsAny(lower, "passport", "verify", "photo", "iv/", "identity", "login", "qrcode")
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
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

func sanitize(s string) string {
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return r.Replace(s)
}
