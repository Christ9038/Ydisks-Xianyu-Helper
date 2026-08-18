package browser

import (
	"context"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mxschmitt/playwright-go"
)

// loginFormSelectors 登录表单选择器，主页面和 iframe 中都要找。
var loginIDSelectors = []string{
	"#fm-login-id", `input[name="fm-login-id"]`,
	`input[placeholder*="手机号"]`, `input[placeholder*="邮箱"]`,
	".fm-login-id",
}

// loginPwdSelectors 用于本次流程后续判断的登录PwdSelectors
var loginPwdSelectors = []string{
	"#fm-login-password", `input[type="password"]`,
}

// loginBtnSelectors 用于本次流程后续判断的登录BtnSelectors
var loginBtnSelectors = []string{
	"button.password-login", `button[type="submit"]`,
}

// loginSuccessSelectors 用于本次流程后续判断的登录SuccessSelectors
var loginSuccessSelectors = []string{
	".rc-virtual-list-holder-inner", // IM 页面侧边栏有子元素则已登录
}

// passwordVerificationWaitInterval 用于本次流程后续判断的密码VerificationWaitInterval
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
// PasswordLogin 封装密码登录业务协调。
func (m *Manager) PasswordLogin(ctx context.Context, account, password, cookieID, userDataDir string, headless bool) (map[string]string, error) {
	return m.passwordLogin(ctx, account, password, cookieID, userDataDir, headless, nil)
}

// PasswordLoginWithEvents 在密码登录过程中上报中间状态。
func (m *Manager) PasswordLoginWithEvents(ctx context.Context, account, password, cookieID, userDataDir string, headless bool, onEvent PasswordLoginEventHandler) (map[string]string, error) {
	return m.passwordLogin(ctx, account, password, cookieID, userDataDir, headless, onEvent)
}

// passwordLogin 封装密码登录业务协调。
func (m *Manager) passwordLogin(ctx context.Context, account, password, cookieID, userDataDir string, headless bool, onEvent PasswordLoginEventHandler) (map[string]string, error) {
	if strings.TrimSpace(account) == "" || strings.TrimSpace(password) == "" {
		return nil, fmt.Errorf("账号或密码不能为空")
	}

	if strings.TrimSpace(userDataDir) == "" {
		userDataDir = filepath.Join("browser_data", "user_"+pureUserID(cookieID))
	}
	headless = quickRenewHeadless(headless)

	// bctx、release、err 用于本次流程后续判断的bctx、release、err
	bctx, release, err := m.newPersistentPasswordContext(ctx, cookieID, userDataDir, headless)
	if err != nil {
		return nil, err
	}
	defer release()

	// page 在无头密码登录时会在导航前覆盖 UA 和 Client Hints，避免暴露 HeadlessChrome。
	page, err := m.newBrowserPage(bctx, headless)
	if err != nil {
		return nil, fmt.Errorf("新建 page 失败: %w", err)
	}

	if // err 用于本次流程后续判断的err
	_, err := page.Goto(goofishIMURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	}); err != nil {
		m.logger.Warn("访问 goofish.com/im 超时", "err", err)
	}
	time.Sleep(passwordLoginPageLoadWait)

	if clickQuickEnter(page) {
		m.logger.Info("密码登录：已点击[快速进入]，等待页面刷新", "cookieID", cookieID)
		time.Sleep(quickRenewAfterClick)
		// cookies、err 用于本次流程后续判断的cookies、err
		cookies, err := extractPageCookies(page)
		if err == nil && quickEnterCookiesUsable(cookies) {
			m.logger.Info("密码登录：快速进入成功，跳过账号密码输入", "cookieID", cookieID)
			return cookies, nil
		}
		m.logger.Info("密码登录：快速进入未获取到有效 Cookie，继续账号密码登录", "cookieID", cookieID)
		if // err 用于本次流程后续判断的err
		_, err := page.Goto(goofishIMURL, playwright.PageGotoOptions{
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
		if // handled 用于本次流程后续判断的handled
		handled := detectAndHandlePasswordSlider(page, m.logger); handled {
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
		// checked 用于本次流程后续判断的checked
		checked, _ := cb.Evaluate(`el => Boolean(el.checked)`)
		if // isChecked 用于本次流程后续判断的isChecked
		isChecked, _ := checked.(bool); !isChecked {
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

// findPasswordElement 封装find密码Element业务协调。
func findPasswordElement(page playwright.Page, selectors []string) playwright.ElementHandle {
	// frames 用于本次流程后续判断的frames
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	// frame 表示当前遍历过程中的frame
	for _, frame := range frames {
		if // el 用于本次流程后续判断的el
		el := queryFirst(frame, selectors); el != nil {
			return el
		}
	}
	return nil
}

// clickPasswordLoginTab 封装click密码登录Tab业务协调。
func clickPasswordLoginTab(page playwright.Page) bool {
	// el 用于本次流程后续判断的el
	el := findPasswordElement(page, []string{"a.password-login-tab-item"})
	return el != nil && el.Click() == nil
}

// detectAndHandlePasswordSlider 封装detectAndHandle密码Slider业务协调。
func detectAndHandlePasswordSlider(page playwright.Page, logger sliderLogger) bool {
	// content 用于本次流程后续判断的内容
	content, _ := page.Content()
	if !strings.Contains(content, "nc-container") && !strings.Contains(content, "scratch-captcha") {
		// 参考实现把“未发现滑块”也视为检测流程成功；调用方据此保持相同的等待节奏。
		return true
	}
	if // err 用于本次流程后续判断的err
	err := solveSlider(page, isScratchCaptcha(content), logger); err != nil {
		logger.Warn("密码登录滑块处理失败", "err", err)
		return false
	}
	return true
}

// quickEnterCookiesUsable 封装quickEnterCookiesUsable业务协调。
func quickEnterCookiesUsable(cookies map[string]string) bool {
	if len(cookies) == 0 {
		return false
	}
	return strings.TrimSpace(cookies["unb"]) != ""
}

// handlePasswordLoginPending 封装handle密码登录Pending业务协调。
func (m *Manager) handlePasswordLoginPending(ctx context.Context, page playwright.Page, onEvent PasswordLoginEventHandler) (map[string]string, error) {
	if // msg 用于本次流程后续判断的msg
	msg := passwordLoginErrorFromPage(page); msg != "" {
		if onEvent != nil {
			onEvent(PasswordLoginEventFromMessage(msg))
		}
		return nil, fmt.Errorf("密码登录失败: %s", msg)
	}
	if // event、ok 用于本次流程后续判断的event、ok
	event, ok := passwordBaxiaEventFromPage(page); ok {
		if onEvent != nil {
			onEvent(event)
		}
		return nil, fmt.Errorf("密码登录失败: %s", event.Message)
	}
	if // event、ok 用于本次流程后续判断的event、ok
	event, ok := passwordVerificationEventFromPage(page); ok {
		if onEvent != nil {
			onEvent(event)
		}
		// cookies、err 用于本次流程后续判断的cookies、err
		cookies, err := waitPasswordVerification(ctx, page, onEvent)
		if err == nil {
			m.logger.Info("密码登录人工验证完成")
			return cookies, nil
		}
		return nil, err
	}
	// errMsg 用于本次流程后续判断的errMsg
	errMsg := "登录失败"
	if onEvent != nil {
		onEvent(PasswordLoginEventFromMessage(errMsg))
	}
	return nil, fmt.Errorf("密码登录失败: %s", errMsg)
}

// waitPasswordVerification 封装wait密码Verification业务协调。
func waitPasswordVerification(ctx context.Context, page playwright.Page, onEvent PasswordLoginEventHandler) (map[string]string, error) {
	// ticker 用于本次流程后续判断的ticker
	ticker := time.NewTicker(passwordVerificationWaitInterval)
	defer ticker.Stop()
	// timer 用于本次流程后续判断的定时器
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
		if // msg 用于本次流程后续判断的msg
		msg := passwordLoginErrorFromPage(page); msg != "" {
			return nil, fmt.Errorf("密码登录失败: %s", msg)
		}
		if // event、ok 用于本次流程后续判断的event、ok
		event, ok := passwordBaxiaEventFromPage(page); ok {
			return nil, fmt.Errorf("密码登录失败: %s", event.Message)
		}
		if // event、ok 用于本次流程后续判断的event、ok
		event, ok := passwordVerificationEventFromPage(page); ok && onEvent != nil {
			onEvent(event)
		}
	}
}

// passwordLoginErrorFromPage 封装密码登录错误From页码业务协调。
func passwordLoginErrorFromPage(page playwright.Page) string {
	if // el、err 用于本次流程后续判断的el、err
	el, err := page.QuerySelector(".login-error-msg"); err == nil && el != nil {
		if // msg 用于本次流程后续判断的msg
		msg, _ := el.TextContent(); strings.TrimSpace(msg) != "" {
			return strings.TrimSpace(msg)
		}
	}
	// htmlText 表示当前遍历过程中的html文本
	for _, htmlText := range passwordLoginHTMLs(page) {
		if // msg 用于本次流程后续判断的msg
		msg := detectPasswordLoginErrorHTML(htmlText); msg != "" {
			return msg
		}
	}
	return ""
}

// passwordBaxiaEventFromPage 封装密码BaxiaEventFrom页码业务协调。
func passwordBaxiaEventFromPage(page playwright.Page) (PasswordLoginEvent, bool) {
	// htmlText 表示当前遍历过程中的html文本
	for _, htmlText := range passwordLoginHTMLs(page) {
		if // event、ok 用于本次流程后续判断的event、ok
		event, ok := detectPasswordBaxiaPunishHTML(htmlText); ok {
			return event, true
		}
	}
	return PasswordLoginEvent{}, false
}

// passwordVerificationEventFromPage 封装密码VerificationEventFrom页码业务协调。
func passwordVerificationEventFromPage(page playwright.Page) (PasswordLoginEvent, bool) {
	if // event、ok 用于本次流程后续判断的event、ok
	event, ok := passwordVerificationEventFromContent(pageContent(page), page.URL()); ok {
		event.ScreenshotPath = firstNonEmptyString(event.ScreenshotPath, passwordVerificationScreenshot(page))
		return event, true
	}
	// frame 表示当前遍历过程中的frame
	for _, frame := range page.Frames() {
		if // event、ok 用于本次流程后续判断的event、ok
		event, ok := passwordVerificationEventFromContent(frameContent(frame), frame.URL()); ok {
			event.ScreenshotPath = firstNonEmptyString(event.ScreenshotPath, passwordVerificationScreenshot(page))
			return event, true
		}
	}
	return PasswordLoginEvent{}, false
}

// passwordVerificationEventFromContent 封装密码VerificationEventFrom内容业务协调。
func passwordVerificationEventFromContent(htmlText, frameURL string) (PasswordLoginEvent, bool) {
	// event、ok 用于本次流程后续判断的event、ok
	event, ok := detectPasswordVerificationHTML(htmlText)
	if !ok {
		return PasswordLoginEvent{}, false
	}
	if event.VerificationURL == "" && looksLikeVerificationURL(frameURL) {
		event.VerificationURL = frameURL
	}
	return event, true
}

// passwordLoginHTMLs 封装密码登录HTMLs业务协调。
func passwordLoginHTMLs(page playwright.Page) []string {
	// htmls 用于本次流程后续判断的htmls
	htmls := []string{pageContent(page)}
	// frame 表示当前遍历过程中的frame
	for _, frame := range page.Frames() {
		htmls = append(htmls, frameContent(frame))
	}
	return htmls
}

// pageContent 封装页码内容业务协调。
func pageContent(page playwright.Page) string {
	// content 用于本次流程后续判断的内容
	content, _ := page.Content()
	return content
}

// frameContent 封装frame内容业务协调。
func frameContent(frame playwright.Frame) string {
	// content 用于本次流程后续判断的内容
	content, _ := frame.Content()
	return content
}

// passwordVerificationScreenshot 封装密码VerificationScreenshot业务协调。
func passwordVerificationScreenshot(page playwright.Page) string {
	// raw、err 用于本次流程后续判断的raw、err
	raw, err := page.Screenshot(playwright.PageScreenshotOptions{
		FullPage: playwright.Bool(true),
		Timeout:  playwright.Float(5000),
	})
	if err != nil || len(raw) == 0 {
		return ""
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(raw)
}

// looksLikeVerificationURL 封装looksLikeVerificationURL业务协调。
func looksLikeVerificationURL(raw string) bool {
	// lower 用于本次流程后续判断的lower
	lower := strings.ToLower(raw)
	return containsAny(lower, "passport", "verify", "photo", "iv/", "identity", "login", "qrcode")
}

// firstNonEmptyString 封装firstNonEmptyString业务协调。
func firstNonEmptyString(values ...string) string {
	// value 表示当前遍历过程中的值
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// checkLoginSuccess 封装check登录Success业务协调。
func checkLoginSuccess(page playwright.Page) bool {
	// sel 表示当前遍历过程中的sel
	for _, sel := range loginSuccessSelectors {
		// el、err 用于本次流程后续判断的el、err
		el, err := page.QuerySelector(sel)
		if err != nil || el == nil {
			continue
		}
		// 子元素数 > 0 则已登录。
		count, err := page.Evaluate(`(sel) => {
			const el = document.querySelector(sel);
			return el ? el.children.length : 0;
		}`, sel)
		if // countText 保存跨 Playwright 数值类型的子元素数量文本。
		countText := fmt.Sprint(count); err == nil && count != nil && countText != "0" {
			return true
		}
	}
	return false
}

// findLoginForm 封装find登录表单业务协调。
func findLoginForm(page playwright.Page) (idEl, pwdEl, submitEl playwright.ElementHandle) {
	// frames 用于本次流程后续判断的frames
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	// f 表示当前遍历过程中的f
	for _, f := range frames {
		// id 用于本次流程后续判断的标识
		id := queryFirst(f, loginIDSelectors)
		if id == nil {
			continue
		}
		// pwd 用于本次流程后续判断的pwd
		pwd := queryFirst(f, loginPwdSelectors)
		// submit 用于本次流程后续判断的submit
		submit := queryFirst(f, loginBtnSelectors)
		return id, pwd, submit
	}
	return nil, nil, nil
}

// sanitize 封装sanitize业务协调。
func sanitize(s string) string {
	// r 用于本次流程后续判断的r
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_", " ", "_")
	return r.Replace(s)
}
