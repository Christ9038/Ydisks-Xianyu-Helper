package browser

import (
	"testing"

	"xianyu-go/internal/xianyu/cookierefresh"
)

func TestCredentialCookieSnapshotPreservesChromiumAttributes(t *testing.T) {
	existing := []cookierefresh.BrowserCookie{
		{Name: "cookie2", Value: "old", Domain: ".taobao.com", Path: "/", Expires: 12345, HTTPOnly: true, Secure: true, SameSite: "None"},
		{Name: "stale", Value: "remove", Domain: ".goofish.com", Path: "/"},
	}
	got := credentialCookieSnapshot(existing, map[string]string{"cookie2": "new", "unb": "1"})
	if len(got) != 2 {
		t.Fatalf("snapshot length=%d want=2: %+v", len(got), got)
	}
	byName := map[string]cookierefresh.BrowserCookie{}
	for _, cookie := range got {
		byName[cookie.Name] = cookie
	}
	cookie2 := byName["cookie2"]
	if cookie2.Value != "new" || cookie2.Domain != ".taobao.com" || cookie2.Expires != 12345 || !cookie2.HTTPOnly || !cookie2.Secure || cookie2.SameSite != "None" {
		t.Fatalf("preserved cookie=%+v", cookie2)
	}
	if unb := byName["unb"]; unb.Domain != goofishDot || unb.Path != "/" {
		t.Fatalf("new cookie defaults=%+v", unb)
	}
	if _, ok := byName["stale"]; ok {
		t.Fatal("cookie absent from current snapshot must be removed")
	}
}
