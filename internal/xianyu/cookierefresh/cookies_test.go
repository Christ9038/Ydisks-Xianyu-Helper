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

func TestCookieHeaderForURLOrdersLongerPathFirst(t *testing.T) {
	now := time.Now()
	snapshot := []BrowserCookie{
		{Name: "_m_h5_tk", Value: "root_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "im_2", Domain: "www.goofish.com", Path: "/im", Secure: true},
	}
	got := CookieHeaderForURL(snapshot, "https://www.goofish.com/im", now)
	if !strings.HasPrefix(got, "_m_h5_tk=im_2; _m_h5_tk=root_1") {
		t.Fatalf("longer-path cookie must be first, got %q", got)
	}
}

func TestCookieHeaderForURLKeepsCreationOrderForEqualPaths(t *testing.T) {
	now := time.Now()
	snapshot := []BrowserCookie{
		{Name: "third", Value: "3", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "first", Value: "1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "second", Value: "2", Domain: ".goofish.com", Path: "/", Secure: true},
	}
	if got := CookieHeaderForURL(snapshot, "https://www.goofish.com/im", now); got != "third=3; first=1; second=2" {
		t.Fatalf("equal-path creation order lost: %q", got)
	}
}

func TestScopedCookieHeaderDistinguishesEmptySnapshotFromUnavailable(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	if got, ok := ScopedCookieHeaderForURL(nil, "https://www.goofish.com/im", now); ok || got != "" {
		t.Fatalf("nil snapshot=(%q,%v), want unavailable", got, ok)
	}
	if got, ok := ScopedCookieHeaderForURL([]BrowserCookie{}, "https://www.goofish.com/im", now); !ok || got != "" {
		t.Fatalf("empty snapshot=(%q,%v), want authoritative empty", got, ok)
	}
	snapshot := []BrowserCookie{{
		Name: "partitioned", Value: "1", Domain: ".goofish.com", Path: "/",
		Secure: true, PartitionKey: "https://example.com",
	}}
	if got, ok := ScopedCookieHeaderForURL(snapshot, "https://www.goofish.com/im", now); !ok || got != "" {
		t.Fatalf("partitionless request=(%q,%v)", got, ok)
	}
	if got, ok := ScopedCookieHeaderForRequest(snapshot, "https://www.goofish.com/im", "https://example.com", now); !ok || got != "partitioned=1" {
		t.Fatalf("partitioned request=(%q,%v)", got, ok)
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

func TestApplySetCookiesAcceptsDomainAttributeWithoutLeadingDot(t *testing.T) {
	updated := ApplySetCookies(nil, "https://passport.goofish.com/ivCheckLogin.htm", []string{
		"unb=777; Domain=goofish.com; Path=/; Secure; HttpOnly",
	}, time.Now(), "https://goofish.com")
	if len(updated) != 1 || updated[0].Domain != ".goofish.com" || !updated[0].Secure || !updated[0].HTTPOnly {
		t.Fatalf("Domain 属性未按域 Cookie 处理: %+v", updated)
	}
	if got := CookieHeaderForURL(updated, "https://www.goofish.com/im", time.Now()); got != "unb=777" {
		t.Fatalf("跨子域 Cookie header=%q", got)
	}
}

func TestApplySetCookiesMaxAgeOverridesExpires(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	updated := ApplySetCookies(nil, "https://www.goofish.com/im", []string{
		"sid=fresh; Domain=.goofish.com; Path=/; Max-Age=3600; Expires=Thu, 01 Jan 1970 00:00:00 GMT",
	}, now)
	if len(updated) != 1 {
		t.Fatalf("snapshot=%+v", updated)
	}
	if got, want := int64(updated[0].Expires), now.Add(time.Hour).Unix(); got != want {
		t.Fatalf("expires=%d want %d", got, want)
	}

	deleted := ApplySetCookies(updated, "https://www.goofish.com/im", []string{
		"sid=stale; Domain=.goofish.com; Path=/; Max-Age=0; Expires=Thu, 01 Jan 2099 00:00:00 GMT",
	}, now)
	if len(deleted) != 0 {
		t.Fatalf("Max-Age=0 must delete cookie: %+v", deleted)
	}
}

func TestApplySetCookiesReplacementAndRecreationOrder(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	initial := []BrowserCookie{
		{Name: "a", Value: "1", Domain: ".goofish.com", Path: "/"},
		{Name: "b", Value: "2", Domain: ".goofish.com", Path: "/"},
		{Name: "c", Value: "3", Domain: ".goofish.com", Path: "/"},
	}
	replaced := ApplySetCookies(initial, "https://www.goofish.com/im", []string{
		"b=fresh; Domain=.goofish.com; Path=/",
	}, now)
	if got := CookieHeaderForURL(replaced, "https://www.goofish.com/im", now); got != "a=1; b=fresh; c=3" {
		t.Fatalf("replacement moved cookie: %q", got)
	}

	recreated := ApplySetCookies(replaced, "https://www.goofish.com/im", []string{
		"b=; Domain=.goofish.com; Path=/; Max-Age=0",
		"b=recreated; Domain=.goofish.com; Path=/",
	}, now)
	if got := CookieHeaderForURL(recreated, "https://www.goofish.com/im", now); got != "a=1; c=3; b=recreated" {
		t.Fatalf("recreated cookie was not appended: %q", got)
	}
}

func TestApplySetCookiesRejectsUnrepresentablePartitionedCookie(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	raw := []string{"chip=value; Domain=.goofish.com; Path=/; Secure; SameSite=None; Partitioned"}
	if got := ApplySetCookies(nil, "https://www.goofish.com/im", raw, now); len(got) != 0 {
		t.Fatalf("partitioned cookie without key must be rejected: %+v", got)
	}
	if got := ApplySetCookies(nil, "https://www.goofish.com/im", raw, now, "  "); len(got) != 0 {
		t.Fatalf("partitioned cookie with empty key must be rejected: %+v", got)
	}
	got := ApplySetCookies(nil, "https://www.goofish.com/im", raw, now, "https://goofish.com")
	if len(got) != 1 || got[0].PartitionKey != "https://goofish.com" {
		t.Fatalf("valid partitioned cookie missing: %+v", got)
	}
}

func TestApplySetCookiesEnforcesSameSiteAndCookiePrefixes(t *testing.T) {
	now := time.Unix(1_800_000_000, 0)
	updated := ApplySetCookies(nil, "https://www.goofish.com/im", []string{
		"none_insecure=1; Domain=.goofish.com; Path=/; SameSite=None",
		"none_secure=1; Domain=.goofish.com; Path=/; SameSite=None; Secure",
		"__Secure-bad=1; Domain=.goofish.com; Path=/",
		"__Secure-good=1; Domain=.goofish.com; Path=/; Secure",
		"__Host-domain=1; Domain=www.goofish.com; Path=/; Secure",
		"__Host-default-path=1; Secure",
		"__Host-good=1; Path=/; Secure",
	}, now)
	values := make(map[string]BrowserCookie, len(updated))
	for _, cookie := range updated {
		values[cookie.Name] = cookie
	}
	for _, rejected := range []string{"none_insecure", "__Secure-bad", "__Host-domain", "__Host-default-path"} {
		if _, exists := values[rejected]; exists {
			t.Fatalf("invalid cookie %s was accepted: %+v", rejected, updated)
		}
	}
	for _, accepted := range []string{"none_secure", "__Secure-good", "__Host-good"} {
		if _, exists := values[accepted]; !exists {
			t.Fatalf("valid cookie %s was rejected: %+v", accepted, updated)
		}
	}
}

func TestSameNameCookiesKeepScopeAndPartitionIdentity(t *testing.T) {
	snapshot := []BrowserCookie{
		{Name: "sid", Value: "root", Domain: ".goofish.com", Path: "/"},
		{Name: "sid", Value: "im", Domain: ".goofish.com", Path: "/im"},
		{Name: "sid", Value: "partitioned", Domain: ".goofish.com", Path: "/", PartitionKey: "https://example.com"},
	}
	if got := CookieStringFromSnapshot(snapshot); got != "sid=root; sid=im; sid=partitioned" {
		t.Fatalf("flat snapshot lost scoped duplicate: %q", got)
	}
	reconciled := ReconcileSnapshotWithCookieString(snapshot, "sid=new-flat")
	if len(reconciled) != 3 {
		t.Fatalf("reconciled=%+v", reconciled)
	}
	for _, cookie := range reconciled {
		if cookie.Value == "new-flat" {
			t.Fatalf("ambiguous flat value overwrote scoped cookie: %+v", reconciled)
		}
	}

	updated := ApplySetCookies(snapshot, "https://www.goofish.com/", []string{
		"sid=new-root; Domain=.goofish.com; Path=/",
	}, time.Unix(1_800_000_000, 0))
	if len(updated) != 3 {
		t.Fatalf("partition identity collapsed: %+v", updated)
	}
}

func TestSynthesizedSnapshotsRemainDeterministic(t *testing.T) {
	fromFlat := SnapshotFromCookieString("z=3; a=1; m=2", ".goofish.com")
	if got := CookieStringFromSnapshot(fromFlat); got != "a=1; m=2; z=3" {
		t.Fatalf("flat snapshot order=%q", got)
	}
	reconciled := ReconcileSnapshotWithCookieString(
		[]BrowserCookie{{Name: "keep", Value: "old", Domain: ".goofish.com", Path: "/"}},
		"z=3; keep=new; a=1",
	)
	if got := CookieStringFromSnapshot(reconciled); got != "keep=new; a=1; z=3" {
		t.Fatalf("reconciled snapshot order=%q", got)
	}
}

func TestSnapshotMetadataReportsAuthoritativeEmptyAndPreservesPartitionKey(t *testing.T) {
	if got, ok := SnapshotFromMetadataOK(`{"other":true}`); ok || got != nil {
		t.Fatalf("missing snapshot=(%+v,%v)", got, ok)
	}
	metadata := MetadataWithSnapshot(`{"other":true}`, []BrowserCookie{})
	if got, ok := SnapshotFromMetadataOK(metadata); !ok || got == nil || len(got) != 0 {
		t.Fatalf("empty snapshot=(%+v,%v) metadata=%s", got, ok, metadata)
	}
	metadata = MetadataWithSnapshot("", []BrowserCookie{{
		Name: "chip", Value: "secret", Domain: ".goofish.com", Path: "/", PartitionKey: "https://example.com",
	}})
	got, ok := SnapshotFromMetadataOK(metadata)
	if !ok || len(got) != 1 || got[0].PartitionKey != "https://example.com" {
		t.Fatalf("partition snapshot=(%+v,%v) metadata=%s", got, ok, metadata)
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
