package browser

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/playwright-community/playwright-go"

	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
)

// ExecuteTokenRequest sends the signed token request from a real Chromium page
// so TLS, UA, Client Hints, Origin, Referer and the browser cookie jar all agree.
func (m *Manager) ExecuteTokenRequest(ctx context.Context, req mtop.TokenBrowserRequest) (*mtop.TokenBrowserResponse, error) {
	if strings.TrimSpace(req.URL) == "" || strings.TrimSpace(req.Cookies) == "" {
		return nil, fmt.Errorf("浏览器 token 请求参数不完整")
	}
	unb := protocol.TransCookies(req.Cookies)["unb"]
	if unb == "" {
		return nil, fmt.Errorf("浏览器 token 请求缺少 unb")
	}
	credentialID := "token_credential_" + unb
	lock := m.accountRenewLock(credentialID)
	lock.Lock()
	defer lock.Unlock()

	page, release, err := m.newPage(ctx, credentialID, req.Cookies, true)
	if err != nil {
		return nil, err
	}
	defer release()
	bctx := page.Context()
	if err := syncCredentialCookies(bctx, req.Cookies); err != nil {
		m.evict(credentialID)
		return nil, fmt.Errorf("浏览器 token 请求同步 Cookie: %w", err)
	}
	if _, err := page.Goto(goofishHomeURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(15000),
	}); err != nil {
		return nil, fmt.Errorf("浏览器 token 请求初始化站点: %w", err)
	}
	value, err := page.Evaluate(`async ({url, body}) => {
		const response = await fetch(url, {
			method: 'POST',
			credentials: 'include',
			headers: {
				'accept': 'application/json',
				'content-type': 'application/x-www-form-urlencoded'
			},
			body
		});
		return {status: response.status, body: await response.text()};
	}`, map[string]any{"url": req.URL, "body": req.Body})
	if err != nil {
		return nil, fmt.Errorf("浏览器执行 token fetch: %w", err)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return nil, err
	}
	var result struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("解析浏览器 token 响应: %w", err)
	}
	all, err := bctx.Cookies()
	if err != nil {
		return nil, fmt.Errorf("读取浏览器 token Cookie: %w", err)
	}
	browserCookies := cookierefresh.CookieStringFromSnapshot(cookieSnapshotFromPlaywright(all))
	return &mtop.TokenBrowserResponse{
		Status:         result.Status,
		Body:           []byte(result.Body),
		UpdatedCookies: cookierefresh.MergeOriginalFields(req.Cookies, browserCookies),
	}, nil
}

// syncCredentialCookies updates a reused credential context without flattening
// attributes already observed from Chromium. Unknown cookies use the narrow
// goofish default only on their first import; later server updates retain their
// real domain, path, expiry, SameSite, Secure and HttpOnly fields.
func syncCredentialCookies(bctx playwright.BrowserContext, cookieStr string) error {
	incoming := parseCookieStr(cookieStr)
	if len(incoming) == 0 {
		return fmt.Errorf("Cookie为空或格式错误")
	}
	existing, err := bctx.Cookies()
	if err != nil {
		return err
	}
	preserved := credentialCookieSnapshot(cookieSnapshotFromPlaywright(existing), incoming)
	if err := bctx.ClearCookies(); err != nil {
		return err
	}
	return bctx.AddCookies(snapshotToOptionalCookies(preserved))
}

func credentialCookieSnapshot(existing []cookierefresh.BrowserCookie, incoming map[string]string) []cookierefresh.BrowserCookie {
	preserved := make([]cookierefresh.BrowserCookie, 0, len(incoming))
	matched := make(map[string]bool, len(incoming))
	for _, cookie := range existing {
		value, ok := incoming[cookie.Name]
		if !ok {
			continue
		}
		cookie.Value = value
		preserved = append(preserved, cookie)
		matched[cookie.Name] = true
	}
	for name, value := range incoming {
		if matched[name] {
			continue
		}
		preserved = append(preserved, cookierefresh.BrowserCookie{
			Name: name, Value: value, Domain: goofishDot, Path: "/",
		})
	}
	return cookierefresh.NormalizeSnapshot(preserved)
}
