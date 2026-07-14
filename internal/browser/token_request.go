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
	bctx, release, err := m.newPersistentRenewContext(ctx, unb, req.Cookies, nil, true, true)
	if err != nil {
		return nil, err
	}
	defer release()
	page, err := bctx.NewPage()
	if err != nil {
		return nil, fmt.Errorf("浏览器 token 请求新建页面: %w", err)
	}
	defer func() { _ = page.Close() }()
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
