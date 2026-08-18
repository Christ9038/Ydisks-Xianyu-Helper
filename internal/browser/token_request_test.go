package browser

import (
	"testing"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestCredentialCookieSnapshotPreservesChromiumAttributes 封装TestCredential登录凭证SnapshotPreservesChromiumAttributes业务协调。
func TestCredentialCookieSnapshotPreservesChromiumAttributes(t *testing.T) {
	// existing 用于本次流程后续判断的existing
	existing := []cookierefresh.BrowserCookie{
		{Name: "cookie2", Value: "old", Domain: ".taobao.com", Path: "/", Expires: 12345, HTTPOnly: true, Secure: true, SameSite: "None"},
		{Name: "stale", Value: "remove", Domain: ".goofish.com", Path: "/"},
	}
	// got 用于本次流程后续判断的got
	got := credentialCookieSnapshot(existing, map[string]string{"cookie2": "new", "unb": "1"})
	if len(got) != 2 {
		t.Fatalf("snapshot length=%d want=2: %+v", len(got), got)
	}
	// byName 用于本次流程后续判断的by名称
	byName := map[string]cookierefresh.BrowserCookie{}
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range got {
		byName[cookie.Name] = cookie
	}
	// cookie2 用于本次流程后续判断的cookie2
	cookie2 := byName["cookie2"]
	if cookie2.Value != "new" || cookie2.Domain != ".taobao.com" || cookie2.Expires != 12345 || !cookie2.HTTPOnly || !cookie2.Secure || cookie2.SameSite != "None" {
		t.Fatalf("preserved cookie=%+v", cookie2)
	}
	if // unb 用于本次流程后续判断的unb
	unb := byName["unb"]; unb.Domain != goofishDot || unb.Path != "/" {
		t.Fatalf("new cookie defaults=%+v", unb)
	}
	if // ok 用于本次流程后续判断的ok
	_, ok := byName["stale"]; ok {
		t.Fatal("cookie absent from current snapshot must be removed")
	}
}
