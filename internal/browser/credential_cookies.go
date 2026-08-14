package browser

import (
	"context"
	"fmt"
	"time"

	"github.com/mxschmitt/playwright-go"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// TokenCaptchaCookieSnapshot 在滑块引擎退出并释放持久化 Profile 后，重新打开
// 同一 Profile 读取最终 Cookie Jar。它不访问任何业务页面，只用于把滑块验证
// 已经产生的精确 Domain/Path/HttpOnly/Expires 属性交回 Go 凭证层。
// TokenCaptchaCookieSnapshot 负责令牌Captcha登录凭证Snapshot相关处理。
func (m *Manager) TokenCaptchaCookieSnapshot(ctx context.Context, cookieID string, headless bool) (string, []cookierefresh.BrowserCookie, error) {
	// bctx、release、err 保存bctx、release、err，供当前处理流程使用
	bctx, release, err := m.newPersistentRenewContext(ctx, cookieID, "", nil, quickRenewHeadless(headless), true)
	if err != nil {
		return "", nil, err
	}
	defer release()
	return readAuthoritativeCookieJar(bctx, goofishIMURL)
}

// syncCredentialCookies 为 token 滑块上下文同步 Cookie。普通登录、
// 续期、MTOP、Token 和 WebSocket 生产流程不得调用 Chromium。
// syncCredentialCookies 负责syncCredentialCookies相关处理。
func syncCredentialCookies(bctx playwright.BrowserContext, cookieStr string, snapshots ...[]cookierefresh.BrowserCookie) error {
	if len(snapshots) > 0 && snapshots[0] != nil {
		// preserved 保存preserved，供当前处理流程使用
		preserved := cookierefresh.NormalizeSnapshot(snapshots[0])
		if // err 保存err，供当前处理流程使用
		err := bctx.ClearCookies(); err != nil {
			return err
		}
		if len(preserved) == 0 {
			return nil
		}
		return bctx.AddCookies(snapshotToOptionalCookies(preserved))
	}
	// incoming 保存incoming，供当前处理流程使用
	incoming := parseCookieStr(cookieStr)
	if len(incoming) == 0 {
		return fmt.Errorf("Cookie为空或格式错误")
	}
	// existing、err 保存existing、err，供当前处理流程使用
	existing, err := bctx.Cookies()
	if err != nil {
		return err
	}
	// preserved 保存preserved，供当前处理流程使用
	preserved := credentialCookieSnapshotForURL(cookieSnapshotFromPlaywright(existing), incoming, goofishIMURL)
	if // err 保存err，供当前处理流程使用
	err := bctx.ClearCookies(); err != nil {
		return err
	}
	return bctx.AddCookies(snapshotToOptionalCookies(preserved))
}

// credentialCookieSnapshot 负责credential登录凭证Snapshot相关处理。
func credentialCookieSnapshot(existing []cookierefresh.BrowserCookie, incoming map[string]string) []cookierefresh.BrowserCookie {
	return credentialCookieSnapshotForURL(existing, incoming, goofishIMURL)
}

// credentialCookieSnapshotForURL 负责credential登录凭证SnapshotForURL相关处理。
func credentialCookieSnapshotForURL(existing []cookierefresh.BrowserCookie, incoming map[string]string, rawURL string) []cookierefresh.BrowserCookie {
	// preserved 保存preserved，供当前处理流程使用
	preserved := make([]cookierefresh.BrowserCookie, 0, len(existing)+len(incoming))
	// matched 保存matched，供当前处理流程使用
	matched := make(map[string]bool, len(incoming))
	// counts 保存counts，供当前处理流程使用
	counts := make(map[string]int, len(existing))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range cookierefresh.NormalizeSnapshot(existing) {
		counts[cookie.Name]++
	}
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range existing {
		if !cookieScopeMatches(cookie, rawURL) {
			if // value、ok 保存value、ok，供当前处理流程使用
			value, ok := incoming[cookie.Name]; ok {
				if counts[cookie.Name] == 1 && cookie.PartitionKey == "" {
					cookie.Value = value
				}
				matched[cookie.Name] = true
			}
			preserved = append(preserved, cookie)
			continue
		}
		// value、ok 保存value、ok，供当前处理流程使用
		value, ok := incoming[cookie.Name]
		if !ok {
			continue
		}
		if counts[cookie.Name] == 1 {
			cookie.Value = value
		}
		preserved = append(preserved, cookie)
		matched[cookie.Name] = true
	}
	// name、value 表示当前遍历过程中的name、value
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

// cookieScopeMatches 负责登录凭证ScopeMatches相关处理。
func cookieScopeMatches(cookie cookierefresh.BrowserCookie, rawURL string) bool {
	// header 保存header，供当前处理流程使用
	header, _ := cookierefresh.ScopedCookieHeaderForRequest([]cookierefresh.BrowserCookie{cookie}, rawURL, "https://goofish.com", time.Unix(0, 0))
	return header != ""
}

// currentCookieHeader 负责current登录凭证Header相关处理。
func currentCookieHeader(snapshot []cookierefresh.BrowserCookie, rawURL string) string {
	// header 保存header，供当前处理流程使用
	header, _ := cookierefresh.ScopedCookieHeaderForRequest(snapshot, rawURL, "https://goofish.com", time.Now())
	return header
}
