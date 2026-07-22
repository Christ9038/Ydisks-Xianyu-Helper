package cookierefresh

import (
	"strings"
	"testing"
	"time"
)

func TestMetadataSnapshotKeyCompatibility(t *testing.T) {
	oldMeta := `{"cookie_refresh_snapshot":[{"name":"a","value":"1","domain":".goofish.com","path":"/"}]}`
	snapshot := SnapshotFromMetadata(oldMeta)
	if len(snapshot) != 1 || snapshot[0].Name != "a" {
		t.Fatalf("旧 key 快照读取失败: %+v", snapshot)
	}
	newMeta := MetadataWithSnapshot(oldMeta, []BrowserCookie{{Name: "b", Value: "2", Domain: ".taobao.com", Path: "/"}})
	if SnapshotFromMetadata(newMeta)[0].Name != "b" {
		t.Fatalf("新 key 快照写入失败: %s", newMeta)
	}
	if got := MetadataWithoutSnapshot(newMeta); len(SnapshotFromMetadata(got)) != 0 {
		t.Fatalf("快照应被清除: %s", got)
	}
}

func TestCookieHeaderForURLScopesDomainPathAndSecure(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	snapshot := []BrowserCookie{
		{Name: "root", Value: "1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "passport", Value: "2", Domain: "passport.goofish.com", Path: "/newlogin", Secure: true},
		{Name: "other", Value: "3", Domain: "h5api.m.goofish.com", Path: "/", Secure: true},
		{Name: "expired", Value: "4", Domain: ".goofish.com", Path: "/", Expires: float64(now.Add(-time.Hour).Unix())},
	}
	got := CookieHeaderForURL(snapshot, "https://passport.goofish.com/newlogin/silentHasLogin.do", now)
	if got != "passport=2; root=1" {
		t.Fatalf("CookieHeaderForURL=%q", got)
	}
}

func TestApplySetCookiesPreservesAttributesAndDeletesExactScope(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	snapshot := []BrowserCookie{
		{Name: "sid", Value: "root", Domain: ".goofish.com", Path: "/"},
		{Name: "sid", Value: "login", Domain: ".goofish.com", Path: "/newlogin"},
	}
	updated := ApplySetCookies(snapshot, "https://passport.goofish.com/newlogin/silentHasLogin.do", []string{
		"sid=; Domain=.goofish.com; Path=/newlogin; Max-Age=0",
		"fresh=ok; Domain=.goofish.com; Path=/; Secure; HttpOnly; SameSite=None",
	}, now)
	if got := CookieHeaderForURL(updated, "https://www.goofish.com/", now); !strings.Contains(got, "sid=root") || !strings.Contains(got, "fresh=ok") {
		t.Fatalf("updated header=%q snapshot=%+v", got, updated)
	}
	for _, cookie := range updated {
		if cookie.Name == "sid" && cookie.Path == "/newlogin" {
			t.Fatalf("精确作用域删除失败: %+v", updated)
		}
		if cookie.Name == "fresh" && (!cookie.Secure || !cookie.HTTPOnly || cookie.SameSite != "None") {
			t.Fatalf("Cookie 属性未保留: %+v", cookie)
		}
	}
}

func TestChangedSnapshotLabels(t *testing.T) {
	before := []BrowserCookie{{Name: "a", Value: "1", Domain: ".goofish.com", Path: "/"}}
	after := []BrowserCookie{{Name: "a", Value: "2", Domain: ".goofish.com", Path: "/"}}
	got := ChangedSnapshotLabels(before, after)
	if len(got) != 1 || got[0] != "a@.goofish.com/" {
		t.Fatalf("ChangedSnapshotLabels=%v", got)
	}
}
