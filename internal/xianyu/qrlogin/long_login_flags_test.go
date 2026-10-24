package qrlogin

import (
	"testing"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestLongLoginCookieFlagsLocked 校验诊断只依赖权威快照；仅旧会话无快照时读取兼容映射。
func TestLongLoginCookieFlagsLocked(t *testing.T) {
	// cases 覆盖空会话、两种 Cookie 属性、权威空快照与旧会话兼容路径。
	cases := []struct {
		// name 标识当前凭证存储场景。
		name string
		// session 只由本用例使用，不与后台轮询并发共享。
		session *Session
		// want 按 havana 存在性/HttpOnly、cookie3 存在性/HttpOnly 排列。
		want [4]bool
	}{
		{name: "missing session"},
		{name: "legacy cookies", session: &Session{cookies: map[string]string{"havana_lgc_exp": "test-expiry", "cookie3_bak_exp": "test-backup"}}, want: [4]bool{true, false, true, false}},
		{name: "authoritative empty snapshot", session: &Session{cookies: map[string]string{"havana_lgc_exp": "stale"}, cookieSnapshot: []cookierefresh.BrowserCookie{}}},
		{name: "snapshot attributes", session: &Session{cookieSnapshot: []cookierefresh.BrowserCookie{
			{Name: "havana_lgc_exp", Value: "test-expiry", HTTPOnly: true},
			{Name: "cookie3_bak_exp", Value: "test-backup", HTTPOnly: false},
			{Name: "unrelated", Value: "test-only"},
		}}, want: [4]bool{true, true, true, false}},
		{name: "cookie3 httponly", session: &Session{cookieSnapshot: []cookierefresh.BrowserCookie{{Name: "cookie3_bak_exp", Value: "test-backup", HTTPOnly: true}}}, want: [4]bool{false, false, true, true}},
	}
	// testCase 是当前需要验证的 Cookie 快照和预期诊断结果。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.session != nil {
				testCase.session.mu.Lock()
				defer testCase.session.mu.Unlock()
			}
			// hasHavana、havanaHTTPOnly、hasCookie3、cookie3HTTPOnly 是脱敏诊断的四个布尔值。
			hasHavana, havanaHTTPOnly, hasCookie3, cookie3HTTPOnly := longLoginCookieFlagsLocked(testCase.session)
			if [4]bool{hasHavana, havanaHTTPOnly, hasCookie3, cookie3HTTPOnly} != testCase.want {
				t.Fatalf("长登录诊断与快照不符: got=%v want=%v", [4]bool{hasHavana, havanaHTTPOnly, hasCookie3, cookie3HTTPOnly}, testCase.want)
			}
		})
	}
}
