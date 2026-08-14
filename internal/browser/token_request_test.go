package browser

import (
	"testing"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestCredentialCookieSnapshotPreservesChromiumAttributes 负责TestCredential登录凭证SnapshotPreservesChromiumAttributes相关处理。
func TestCredentialCookieSnapshotPreservesChromiumAttributes(t *testing.T) {
	// existing 保存existing，供当前处理流程使用
	existing := []cookierefresh.BrowserCookie{
		{Name: "cookie2", Value: "old", Domain: ".taobao.com", Path: "/", Expires: 12345, HTTPOnly: true, Secure: true, SameSite: "None"},
		{Name: "stale", Value: "remove", Domain: ".goofish.com", Path: "/"},
	}
	// got 保存got，供当前处理流程使用
	got := credentialCookieSnapshot(existing, map[string]string{"cookie2": "new", "unb": "1"})
	if len(got) != 2 {
		t.Fatalf("snapshot length=%d want=2: %+v", len(got), got)
	}
	// byName 保存by名称，供当前处理流程使用
	byName := map[string]cookierefresh.BrowserCookie{}
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range got {
		byName[cookie.Name] = cookie
	}
	// cookie2 保存cookie2，供当前处理流程使用
	cookie2 := byName["cookie2"]
	if cookie2.Value != "new" || cookie2.Domain != ".taobao.com" || cookie2.Expires != 12345 || !cookie2.HTTPOnly || !cookie2.Secure || cookie2.SameSite != "None" {
		t.Fatalf("preserved cookie=%+v", cookie2)
	}
	if // unb 保存unb，供当前处理流程使用
	unb := byName["unb"]; unb.Domain != goofishDot || unb.Path != "/" {
		t.Fatalf("new cookie defaults=%+v", unb)
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := byName["stale"]; ok {
		t.Fatal("cookie absent from current snapshot must be removed")
	}
}
