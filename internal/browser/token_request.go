package browser

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/playwright-community/playwright-go"

	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
)

const tokenRequestTimeoutMS = 20000

var tokenPageURLPattern = regexp.MustCompile(`^https://www\.goofish\.com/im(?:[?#].*)?$`)

// ExecuteTokenRequest sends the signed token request from a real Chromium page
// so TLS, UA, Client Hints, Origin, Referer and the browser cookie jar all agree.
func (m *Manager) ExecuteTokenRequest(ctx context.Context, req mtop.TokenBrowserRequest) (*mtop.TokenBrowserResponse, error) {
	if strings.TrimSpace(req.URL) == "" || (strings.TrimSpace(req.Cookies) == "" && req.CookieSnapshot == nil) {
		return nil, fmt.Errorf("浏览器 token 请求参数不完整")
	}
	credentialCookies := req.Cookies
	if scoped, authoritative := cookierefresh.ScopedCookieHeaderForRequest(req.CookieSnapshot, goofishIMURL, "https://goofish.com", time.Now()); authoritative {
		credentialCookies = scoped
	}
	unb := protocol.TransCookies(credentialCookies)["unb"]
	if unb == "" {
		return nil, fmt.Errorf("浏览器 token 请求缺少 unb")
	}
	credentialID := "token_credential_" + unb
	lock := m.accountRenewLock(credentialID)
	lock.Lock()
	defer lock.Unlock()

	page, bctx, releaseEntry, err := m.tokenRequestPage(ctx, credentialID, req.Cookies, req.CookieSnapshot)
	if err != nil {
		return nil, err
	}
	defer releaseEntry()
	m.touch(credentialID)
	value, err := page.Evaluate(`async ({url, body, timeout}) => {
		const controller = new AbortController();
		const timer = setTimeout(() => controller.abort(), timeout);
		try {
			const response = await fetch(url, {
				method: 'POST',
				credentials: 'include',
				signal: controller.signal,
				headers: {
					'accept': 'application/json',
					'content-type': 'application/x-www-form-urlencoded'
				},
				body
			});
			return {status: response.status, body: await response.text()};
		} finally {
			clearTimeout(timer);
		}
	}`, map[string]any{"url": req.URL, "body": req.Body, "timeout": tokenRequestTimeoutMS})
	evalErr := err
	all, cookieErr := bctx.Cookies()
	if cookieErr != nil {
		return nil, errors.Join(wrapTokenBrowserEvalError(evalErr), fmt.Errorf("读取浏览器 token Cookie: %w", cookieErr))
	}
	snapshot := cookieSnapshotFromPlaywright(all)
	if snapshot == nil {
		snapshot = []cookierefresh.BrowserCookie{}
	}
	browserCookies := currentCookieHeader(snapshot, goofishIMURL)
	response := &mtop.TokenBrowserResponse{
		UpdatedCookies:         browserCookies,
		CookieSnapshot:         snapshot,
		CookieSnapshotComplete: true,
	}
	if evalErr != nil {
		return response, fmt.Errorf("浏览器执行 token fetch: %w", evalErr)
	}
	raw, err := json.Marshal(value)
	if err != nil {
		return response, err
	}
	var result struct {
		Status int    `json:"status"`
		Body   string `json:"body"`
	}
	if err := json.Unmarshal(raw, &result); err != nil {
		return response, fmt.Errorf("解析浏览器 token 响应: %w", err)
	}
	response.Status = result.Status
	response.Body = []byte(result.Body)
	return response, nil
}

func wrapTokenBrowserEvalError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("浏览器执行 token fetch: %w", err)
}

// tokenRequestPage keeps one inert /im document for each account credential.
// Its URL and origin are identical to the official message page, so Chromium
// supplies the same Referer/Origin/Cookie/Client-Hints context for token fetches.
// The main document is fulfilled locally on first use: loading the full SPA on
// every reconnect would start another official WebSocket and rerun auto-login,
// neither of which happens for a normal reconnect in an already-open /im tab.
func (m *Manager) tokenRequestPage(ctx context.Context, credentialID, cookieStr string, snapshot []cookierefresh.BrowserCookie) (playwright.Page, playwright.BrowserContext, func(), error) {
	if err := ctx.Err(); err != nil {
		return nil, nil, nil, err
	}
	if err := m.init(); err != nil {
		return nil, nil, nil, err
	}
	entry, err := m.acquireEntry(credentialID, cookieStr, true)
	if err != nil {
		return nil, nil, nil, err
	}
	release := func() { m.releaseEntry(credentialID, entry) }
	if err := syncCredentialCookies(entry.context, cookieStr, snapshot); err != nil {
		release()
		m.evict(credentialID)
		return nil, nil, nil, fmt.Errorf("浏览器 token 请求同步 Cookie: %w", err)
	}
	for _, candidate := range entry.context.Pages() {
		if !candidate.IsClosed() && tokenPageURLPattern.MatchString(candidate.URL()) {
			return candidate, entry.context, release, nil
		}
	}

	page, err := entry.context.NewPage()
	if err != nil {
		release()
		m.evict(credentialID)
		return nil, nil, nil, fmt.Errorf("浏览器 token 请求新建消息页: %w", err)
	}
	route := func(route playwright.Route) {
		request := route.Request()
		if request.IsNavigationRequest() && request.ResourceType() == "document" {
			_ = route.Fulfill(playwright.RouteFulfillOptions{
				Status:      playwright.Int(200),
				ContentType: playwright.String("text/html; charset=utf-8"),
				Body:        playwright.String("<!doctype html><html><head><meta charset=\"utf-8\"><title>Goofish IM</title></head><body></body></html>"),
			})
			return
		}
		_ = route.Continue()
	}
	if err := page.Route(tokenPageURLPattern, route, 1); err != nil {
		_ = page.Close()
		release()
		m.evict(credentialID)
		return nil, nil, nil, fmt.Errorf("浏览器 token 请求初始化消息页路由: %w", err)
	}
	if _, err := page.Goto(goofishIMURL, playwright.PageGotoOptions{
		WaitUntil: playwright.WaitUntilStateDomcontentloaded,
		Timeout:   playwright.Float(15000),
	}); err != nil {
		_ = page.Close()
		release()
		m.evict(credentialID)
		return nil, nil, nil, fmt.Errorf("浏览器 token 请求初始化消息页: %w", err)
	}
	return page, entry.context, release, nil
}

// syncCredentialCookies updates a reused credential context without flattening
// attributes already observed from Chromium. Unknown cookies use the narrow
// goofish default only on their first import; later server updates retain their
// real domain, path, expiry, SameSite, Secure and HttpOnly fields.
func syncCredentialCookies(bctx playwright.BrowserContext, cookieStr string, snapshots ...[]cookierefresh.BrowserCookie) error {
	if len(snapshots) > 0 && snapshots[0] != nil {
		// A Chromium snapshot is the complete authoritative jar, including an
		// explicitly empty jar. Do not flatten it through cookieStr: doing so
		// loses scopes and can resurrect cookies Chromium already deleted.
		preserved := cookierefresh.NormalizeSnapshot(snapshots[0])
		if err := bctx.ClearCookies(); err != nil {
			return err
		}
		if len(preserved) == 0 {
			return nil
		}
		return bctx.AddCookies(snapshotToOptionalCookies(preserved))
	}
	incoming := parseCookieStr(cookieStr)
	if len(incoming) == 0 {
		return fmt.Errorf("Cookie为空或格式错误")
	}
	existing, err := bctx.Cookies()
	if err != nil {
		return err
	}
	basis := cookieSnapshotFromPlaywright(existing)
	preserved := credentialCookieSnapshotForURL(basis, incoming, goofishIMURL)
	if err := bctx.ClearCookies(); err != nil {
		return err
	}
	return bctx.AddCookies(snapshotToOptionalCookies(preserved))
}

func credentialCookieSnapshot(existing []cookierefresh.BrowserCookie, incoming map[string]string) []cookierefresh.BrowserCookie {
	return credentialCookieSnapshotForURL(existing, incoming, goofishIMURL)
}

func credentialCookieSnapshotForURL(existing []cookierefresh.BrowserCookie, incoming map[string]string, rawURL string) []cookierefresh.BrowserCookie {
	preserved := make([]cookierefresh.BrowserCookie, 0, len(existing)+len(incoming))
	matched := make(map[string]bool, len(incoming))
	counts := make(map[string]int, len(existing))
	for _, cookie := range cookierefresh.NormalizeSnapshot(existing) {
		counts[cookie.Name]++
	}
	for _, cookie := range existing {
		// A flat Cookie header only describes cookies applicable to its source
		// URL. Keep absent cookies for passport/other paths untouched; otherwise
		// a goofish header would silently erase the rest of Chromium's jar. When
		// the flat representation does carry the same name, update its value for
		// backward compatibility with historical all-domain flattened snapshots.
		if !cookieScopeMatches(cookie, rawURL) {
			if value, ok := incoming[cookie.Name]; ok {
				if counts[cookie.Name] == 1 && cookie.PartitionKey == "" {
					cookie.Value = value
				}
				matched[cookie.Name] = true
			}
			preserved = append(preserved, cookie)
			continue
		}
		value, ok := incoming[cookie.Name]
		if !ok {
			continue
		}
		// A flat string cannot identify which Domain/Path/PartitionKey owns a
		// value when the same name occurs more than once. Preserve those scoped
		// values instead of corrupting every variant with one ambiguous value.
		if counts[cookie.Name] == 1 {
			cookie.Value = value
		}
		preserved = append(preserved, cookie)
		matched[cookie.Name] = true
	}
	for name, value := range incoming {
		if matched[name] {
			continue
		}
		preserved = append(preserved, cookierefresh.BrowserCookie{
			Name: name, Value: value, Domain: goofishDot, Path: "/", Secure: true,
		})
	}
	return cookierefresh.NormalizeSnapshot(preserved)
}

func cookieScopeMatches(cookie cookierefresh.BrowserCookie, rawURL string) bool {
	header, _ := cookierefresh.ScopedCookieHeaderForRequest([]cookierefresh.BrowserCookie{cookie}, rawURL, "https://goofish.com", time.Unix(0, 0))
	return header != ""
}

func currentCookieHeader(snapshot []cookierefresh.BrowserCookie, rawURL string) string {
	header, _ := cookierefresh.ScopedCookieHeaderForRequest(snapshot, rawURL, "https://goofish.com", time.Now())
	return header
}
