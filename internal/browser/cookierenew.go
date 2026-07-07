package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/playwright-community/playwright-go"
)

// CookieRenew 用现有 Cookie 打开闲鱼页面，尝试通过“快速进入”刷新浏览器登录态。
//
// 这个方法位于密码登录之前：如果 Cookie 仍保留可续期的浏览器会话，页面通常会给出
// 免输密码的快速进入入口。成功点击后提取完整 Cookie，可避免更重的账号密码登录。
func (m *Manager) CookieRenew(ctx context.Context, cookieID, cookieStr string, headless bool) (map[string]string, error) {
	if cookieStr == "" {
		return nil, fmt.Errorf("Cookie为空，无法浏览器续期")
	}
	page, release, err := m.newPage(ctx, cookieID, cookieStr, headless)
	if err != nil {
		return nil, err
	}
	defer release()

	if _, err := page.Goto(goofishIMURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(30000),
	}); err != nil {
		m.logger.Warn("浏览器续期访问 IM 页面异常", "cookieID", cookieID, "err", err)
	}
	if checkLoginSuccess(page) {
		m.logger.Info("浏览器续期：已有有效登录态", "cookieID", cookieID)
		return extractPageCookies(page)
	}

	if !clickQuickEnter(page) {
		return nil, fmt.Errorf("未找到快速进入入口，需要密码登录")
	}
	time.Sleep(5 * time.Second)
	if !checkLoginSuccess(page) {
		return nil, fmt.Errorf("快速进入后仍未检测到登录态")
	}
	m.logger.Info("浏览器续期：快速进入成功", "cookieID", cookieID)
	return extractPageCookies(page)
}

func clickQuickEnter(page playwright.Page) bool {
	frames := append([]playwright.Frame{page.MainFrame()}, page.Frames()...)
	for _, f := range frames {
		for _, sel := range loginBtnSelectors {
			el, err := f.QuerySelector(sel)
			if err == nil && el != nil {
				_ = el.Click()
				return true
			}
		}
	}
	return false
}
