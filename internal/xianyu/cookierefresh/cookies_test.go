package cookierefresh

import (
	"strings"
	"testing"
	"time"
)

// TestCookieStringHelpers 验证扁平 Cookie 合并、排序、变化检测和快照压缩的边界语义。
func TestCookieStringHelpers(t *testing.T) {
	if MarshalCookieString(nil) != "" {
		t.Fatal("nil cookie map should serialize to empty string")
	}
	// marshaled 保存过滤空键并按名称排序后的 Cookie 头。
	marshaled := MarshalCookieString(map[string]string{"b": "2", " ": "ignored", "a": "1"})
	if marshaled != "a=1; b=2" {
		t.Fatalf("marshaled=%q", marshaled)
	}
	// merged 保存 Set-Cookie 合并后的扁平 Cookie 头。
	merged := MergeSetCookies("a=1", []string{"b=2; Path=/", "invalid", "=empty"})
	if merged != "a=1; b=2" {
		t.Fatalf("merged=%q", merged)
	}
	// changed 保存前后 Cookie 字符串的字段变化。
	changed := ChangedCookieNames("a=1; b=2", "a=1; c=3")
	if len(changed) != 2 || changed[0] != "b" || changed[1] != "c" {
		t.Fatalf("changed=%v", changed)
	}
	// originalMerged 保存浏览器结果补回旧字段后的 Cookie 头。
	originalMerged := MergeOriginalFields("a=1; old=0", "a=2; new=3")
	if originalMerged != "a=2; new=3; old=0" {
		t.Fatalf("originalMerged=%q", originalMerged)
	}
	// snapshot 保存含空名称和缺省路径的浏览器 Cookie 快照。
	snapshot := NormalizeSnapshot([]BrowserCookie{{Name: "", Value: "ignored"}, {Name: "a", Value: "1"}})
	if len(snapshot) != 1 || snapshot[0].Path != "/" {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if CookieStringFromSnapshot(snapshot) != "a=1" {
		t.Fatalf("snapshot string=%q", CookieStringFromSnapshot(snapshot))
	}
	// inferred 保存从扁平 Cookie 字符串生成的默认域快照。
	inferred := SnapshotFromCookieString("b=2; a=1", "")
	if len(inferred) != 2 || inferred[0].Name != "a" || inferred[0].Domain != ".goofish.com" {
		t.Fatalf("inferred=%+v", inferred)
	}
}

// TestMetadataSnapshotKeyCompatibility 封装TestMetadataSnapshotKeyCompatibility业务协调。
func TestMetadataSnapshotKeyCompatibility(t *testing.T) {
	// oldMeta 用于本次流程后续判断的oldMeta
	oldMeta := `{"cookie_refresh_snapshot":[{"name":"a","value":"1","domain":".goofish.com","path":"/"}]}`
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := SnapshotFromMetadata(oldMeta)
	if len(snapshot) != 1 || snapshot[0].Name != "a" {
		t.Fatalf("旧 key 快照读取失败: %+v", snapshot)
	}
	// newMeta 用于本次流程后续判断的newMeta
	newMeta := MetadataWithSnapshot(oldMeta, []BrowserCookie{{Name: "b", Value: "2", Domain: ".taobao.com", Path: "/"}})
	if SnapshotFromMetadata(newMeta)[0].Name != "b" {
		t.Fatalf("新 key 快照写入失败: %s", newMeta)
	}
	if // got 用于本次流程后续判断的got
	got := MetadataWithoutSnapshot(newMeta); len(SnapshotFromMetadata(got)) != 0 {
		t.Fatalf("快照应被清除: %s", got)
	}
}

// TestCookieHeaderForURLScopesDomainPathAndSecure 封装Test登录凭证HeaderForURLScopesDomain路径AndSecure业务协调。
func TestCookieHeaderForURLScopesDomainPathAndSecure(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Unix(1_800_000_000, 0)
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []BrowserCookie{
		{Name: "root", Value: "1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "passport", Value: "2", Domain: "passport.goofish.com", Path: "/newlogin", Secure: true},
		{Name: "other", Value: "3", Domain: "h5api.m.goofish.com", Path: "/", Secure: true},
		{Name: "expired", Value: "4", Domain: ".goofish.com", Path: "/", Expires: float64(now.Add(-time.Hour).Unix())},
	}
	// got 用于本次流程后续判断的got
	got := CookieHeaderForURL(snapshot, "https://passport.goofish.com/newlogin/silentHasLogin.do", now)
	if got != "passport=2; root=1" {
		t.Fatalf("CookieHeaderForURL=%q", got)
	}
}

// TestCookieHeaderForURLOrdersLongerPathFirst 封装Test登录凭证HeaderForURL订单列表Longer路径First业务协调。
func TestCookieHeaderForURLOrdersLongerPathFirst(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Now()
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []BrowserCookie{
		{Name: "_m_h5_tk", Value: "root_1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "_m_h5_tk", Value: "im_2", Domain: "www.goofish.com", Path: "/im", Secure: true},
	}
	// got 用于本次流程后续判断的got
	got := CookieHeaderForURL(snapshot, "https://www.goofish.com/im", now)
	if !strings.HasPrefix(got, "_m_h5_tk=im_2; _m_h5_tk=root_1") {
		t.Fatalf("longer-path cookie must be first, got %q", got)
	}
}

// TestCookieHeaderForURLKeepsCreationOrderForEqualPaths 封装Test登录凭证HeaderForURLKeepsCreation订单ForEqualPaths业务协调。
func TestCookieHeaderForURLKeepsCreationOrderForEqualPaths(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Now()
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []BrowserCookie{
		{Name: "third", Value: "3", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "first", Value: "1", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "second", Value: "2", Domain: ".goofish.com", Path: "/", Secure: true},
	}
	if // got 用于本次流程后续判断的got
	got := CookieHeaderForURL(snapshot, "https://www.goofish.com/im", now); got != "third=3; first=1; second=2" {
		t.Fatalf("equal-path creation order lost: %q", got)
	}
}

// TestScopedCookieHeaderDistinguishesEmptySnapshotFromUnavailable 封装TestScoped登录凭证HeaderDistinguishesEmptySnapshotFromUnavailable业务协调。
func TestScopedCookieHeaderDistinguishesEmptySnapshotFromUnavailable(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Unix(1_800_000_000, 0)
	if // got、ok 用于本次流程后续判断的got、ok
	got, ok := ScopedCookieHeaderForURL(nil, "https://www.goofish.com/im", now); ok || got != "" {
		t.Fatalf("nil snapshot=(%q,%v), want unavailable", got, ok)
	}
	if // got、ok 用于本次流程后续判断的got、ok
	got, ok := ScopedCookieHeaderForURL([]BrowserCookie{}, "https://www.goofish.com/im", now); !ok || got != "" {
		t.Fatalf("empty snapshot=(%q,%v), want authoritative empty", got, ok)
	}
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []BrowserCookie{{
		Name: "partitioned", Value: "1", Domain: ".goofish.com", Path: "/",
		Secure: true, PartitionKey: "https://example.com",
	}}
	if // got、ok 用于本次流程后续判断的got、ok
	got, ok := ScopedCookieHeaderForURL(snapshot, "https://www.goofish.com/im", now); !ok || got != "" {
		t.Fatalf("partitionless request=(%q,%v)", got, ok)
	}
	if // got、ok 用于本次流程后续判断的got、ok
	got, ok := ScopedCookieHeaderForRequest(snapshot, "https://www.goofish.com/im", "https://example.com", now); !ok || got != "partitioned=1" {
		t.Fatalf("partitioned request=(%q,%v)", got, ok)
	}
}

// TestApplySetCookiesPreservesAttributesAndDeletesExactScope 封装TestApplySetCookiesPreservesAttributesAndDeletesExactScope业务协调。
func TestApplySetCookiesPreservesAttributesAndDeletesExactScope(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Unix(1_800_000_000, 0)
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []BrowserCookie{
		{Name: "sid", Value: "root", Domain: ".goofish.com", Path: "/"},
		{Name: "sid", Value: "login", Domain: ".goofish.com", Path: "/newlogin"},
	}
	// updated 用于本次流程后续判断的updated
	updated := ApplySetCookies(snapshot, "https://passport.goofish.com/newlogin/silentHasLogin.do", []string{
		"sid=; Domain=.goofish.com; Path=/newlogin; Max-Age=0",
		"fresh=ok; Domain=.goofish.com; Path=/; Secure; HttpOnly; SameSite=None",
	}, now)
	if // got 用于本次流程后续判断的got
	got := CookieHeaderForURL(updated, "https://www.goofish.com/", now); !strings.Contains(got, "sid=root") || !strings.Contains(got, "fresh=ok") {
		t.Fatalf("updated header=%q snapshot=%+v", got, updated)
	}
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range updated {
		if cookie.Name == "sid" && cookie.Path == "/newlogin" {
			t.Fatalf("精确作用域删除失败: %+v", updated)
		}
		if cookie.Name == "fresh" && (!cookie.Secure || !cookie.HTTPOnly || cookie.SameSite != "None") {
			t.Fatalf("Cookie 属性未保留: %+v", cookie)
		}
	}
}

// TestApplySetCookiesAcceptsDomainAttributeWithoutLeadingDot 封装TestApplySetCookiesAcceptsDomainAttributeWithoutLeadingDot业务协调。
func TestApplySetCookiesAcceptsDomainAttributeWithoutLeadingDot(t *testing.T) {
	// updated 用于本次流程后续判断的updated
	updated := ApplySetCookies(nil, "https://passport.goofish.com/ivCheckLogin.htm", []string{
		"unb=777; Domain=goofish.com; Path=/; Secure; HttpOnly",
	}, time.Now(), "https://goofish.com")
	if len(updated) != 1 || updated[0].Domain != ".goofish.com" || !updated[0].Secure || !updated[0].HTTPOnly {
		t.Fatalf("Domain 属性未按域 Cookie 处理: %+v", updated)
	}
	if // got 用于本次流程后续判断的got
	got := CookieHeaderForURL(updated, "https://www.goofish.com/im", time.Now()); got != "unb=777" {
		t.Fatalf("跨子域 Cookie header=%q", got)
	}
}

// TestApplySetCookiesMaxAgeOverridesExpires 封装TestApplySetCookiesMaxAgeOverridesExpires业务协调。
func TestApplySetCookiesMaxAgeOverridesExpires(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Unix(1_800_000_000, 0)
	// updated 用于本次流程后续判断的updated
	updated := ApplySetCookies(nil, "https://www.goofish.com/im", []string{
		"sid=fresh; Domain=.goofish.com; Path=/; Max-Age=3600; Expires=Thu, 01 Jan 1970 00:00:00 GMT",
	}, now)
	if len(updated) != 1 {
		t.Fatalf("snapshot=%+v", updated)
	}
	if // got、want 用于本次流程后续判断的got、want
	got, want := int64(updated[0].Expires), now.Add(time.Hour).Unix(); got != want {
		t.Fatalf("expires=%d want %d", got, want)
	}

	// deleted 用于本次流程后续判断的deleted
	deleted := ApplySetCookies(updated, "https://www.goofish.com/im", []string{
		"sid=stale; Domain=.goofish.com; Path=/; Max-Age=0; Expires=Thu, 01 Jan 2099 00:00:00 GMT",
	}, now)
	if len(deleted) != 0 {
		t.Fatalf("Max-Age=0 must delete cookie: %+v", deleted)
	}
}

// TestApplySetCookiesReplacementAndRecreationOrder 封装TestApplySetCookiesReplacementAndRecreation订单业务协调。
func TestApplySetCookiesReplacementAndRecreationOrder(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Unix(1_800_000_000, 0)
	// initial 用于本次流程后续判断的initial
	initial := []BrowserCookie{
		{Name: "a", Value: "1", Domain: ".goofish.com", Path: "/"},
		{Name: "b", Value: "2", Domain: ".goofish.com", Path: "/"},
		{Name: "c", Value: "3", Domain: ".goofish.com", Path: "/"},
	}
	// replaced 用于本次流程后续判断的replaced
	replaced := ApplySetCookies(initial, "https://www.goofish.com/im", []string{
		"b=fresh; Domain=.goofish.com; Path=/",
	}, now)
	if // got 用于本次流程后续判断的got
	got := CookieHeaderForURL(replaced, "https://www.goofish.com/im", now); got != "a=1; b=fresh; c=3" {
		t.Fatalf("replacement moved cookie: %q", got)
	}

	// recreated 用于本次流程后续判断的recreated
	recreated := ApplySetCookies(replaced, "https://www.goofish.com/im", []string{
		"b=; Domain=.goofish.com; Path=/; Max-Age=0",
		"b=recreated; Domain=.goofish.com; Path=/",
	}, now)
	if // got 用于本次流程后续判断的got
	got := CookieHeaderForURL(recreated, "https://www.goofish.com/im", now); got != "a=1; c=3; b=recreated" {
		t.Fatalf("recreated cookie was not appended: %q", got)
	}
}

// TestApplySetCookiesRejectsUnrepresentablePartitionedCookie 封装TestApplySetCookiesRejectsUnrepresentablePartitioned登录凭证业务协调。
func TestApplySetCookiesRejectsUnrepresentablePartitionedCookie(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Unix(1_800_000_000, 0)
	// raw 用于本次流程后续判断的原始
	raw := []string{"chip=value; Domain=.goofish.com; Path=/; Secure; SameSite=None; Partitioned"}
	if // got 用于本次流程后续判断的got
	got := ApplySetCookies(nil, "https://www.goofish.com/im", raw, now); len(got) != 0 {
		t.Fatalf("partitioned cookie without key must be rejected: %+v", got)
	}
	if // got 用于本次流程后续判断的got
	got := ApplySetCookies(nil, "https://www.goofish.com/im", raw, now, "  "); len(got) != 0 {
		t.Fatalf("partitioned cookie with empty key must be rejected: %+v", got)
	}
	// got 用于本次流程后续判断的got
	got := ApplySetCookies(nil, "https://www.goofish.com/im", raw, now, "https://goofish.com")
	if len(got) != 1 || got[0].PartitionKey != "https://goofish.com" {
		t.Fatalf("valid partitioned cookie missing: %+v", got)
	}
}

// TestApplySetCookiesEnforcesSameSiteAndCookiePrefixes 封装TestApplySetCookiesEnforcesSameSiteAnd登录凭证Prefixes业务协调。
func TestApplySetCookiesEnforcesSameSiteAndCookiePrefixes(t *testing.T) {
	// now 用于本次流程后续判断的now
	now := time.Unix(1_800_000_000, 0)
	// updated 用于本次流程后续判断的updated
	updated := ApplySetCookies(nil, "https://www.goofish.com/im", []string{
		"none_insecure=1; Domain=.goofish.com; Path=/; SameSite=None",
		"none_secure=1; Domain=.goofish.com; Path=/; SameSite=None; Secure",
		"__Secure-bad=1; Domain=.goofish.com; Path=/",
		"__Secure-good=1; Domain=.goofish.com; Path=/; Secure",
		"__Host-domain=1; Domain=www.goofish.com; Path=/; Secure",
		"__Host-default-path=1; Secure",
		"__Host-good=1; Path=/; Secure",
	}, now)
	// values 用于本次流程后续判断的values
	values := make(map[string]BrowserCookie, len(updated))
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range updated {
		values[cookie.Name] = cookie
	}
	// rejected 表示当前遍历过程中的rejected
	for _, rejected := range []string{"none_insecure", "__Secure-bad", "__Host-domain", "__Host-default-path"} {
		if // exists 用于本次流程后续判断的exists
		_, exists := values[rejected]; exists {
			t.Fatalf("invalid cookie %s was accepted: %+v", rejected, updated)
		}
	}
	// accepted 表示当前遍历过程中的accepted
	for _, accepted := range []string{"none_secure", "__Secure-good", "__Host-good"} {
		if // exists 用于本次流程后续判断的exists
		_, exists := values[accepted]; !exists {
			t.Fatalf("valid cookie %s was rejected: %+v", accepted, updated)
		}
	}
}

// TestSameNameCookiesKeepScopeAndPartitionIdentity 封装TestSame名称CookiesKeepScopeAndPartitionIdentity业务协调。
func TestSameNameCookiesKeepScopeAndPartitionIdentity(t *testing.T) {
	// snapshot 用于本次流程后续判断的snapshot
	snapshot := []BrowserCookie{
		{Name: "sid", Value: "root", Domain: ".goofish.com", Path: "/"},
		{Name: "sid", Value: "im", Domain: ".goofish.com", Path: "/im"},
		{Name: "sid", Value: "partitioned", Domain: ".goofish.com", Path: "/", PartitionKey: "https://example.com"},
	}
	if // got 用于本次流程后续判断的got
	got := CookieStringFromSnapshot(snapshot); got != "sid=root; sid=im; sid=partitioned" {
		t.Fatalf("flat snapshot lost scoped duplicate: %q", got)
	}
	// reconciled 用于本次流程后续判断的reconciled
	reconciled := ReconcileSnapshotWithCookieString(snapshot, "sid=new-flat")
	if len(reconciled) != 3 {
		t.Fatalf("reconciled=%+v", reconciled)
	}
	// cookie 表示当前遍历过程中的登录凭证
	for _, cookie := range reconciled {
		if cookie.Value == "new-flat" {
			t.Fatalf("ambiguous flat value overwrote scoped cookie: %+v", reconciled)
		}
	}

	// updated 用于本次流程后续判断的updated
	updated := ApplySetCookies(snapshot, "https://www.goofish.com/", []string{
		"sid=new-root; Domain=.goofish.com; Path=/",
	}, time.Unix(1_800_000_000, 0))
	if len(updated) != 3 {
		t.Fatalf("partition identity collapsed: %+v", updated)
	}
}

// TestSynthesizedSnapshotsRemainDeterministic 封装TestSynthesizedSnapshotsRemainDeterministic业务协调。
func TestSynthesizedSnapshotsRemainDeterministic(t *testing.T) {
	// fromFlat 用于本次流程后续判断的fromFlat
	fromFlat := SnapshotFromCookieString("z=3; a=1; m=2", ".goofish.com")
	if // got 用于本次流程后续判断的got
	got := CookieStringFromSnapshot(fromFlat); got != "a=1; m=2; z=3" {
		t.Fatalf("flat snapshot order=%q", got)
	}
	// reconciled 用于本次流程后续判断的reconciled
	reconciled := ReconcileSnapshotWithCookieString(
		[]BrowserCookie{{Name: "keep", Value: "old", Domain: ".goofish.com", Path: "/"}},
		"z=3; keep=new; a=1",
	)
	if // got 用于本次流程后续判断的got
	got := CookieStringFromSnapshot(reconciled); got != "keep=new; a=1; z=3" {
		t.Fatalf("reconciled snapshot order=%q", got)
	}
}

// TestSnapshotMetadataReportsAuthoritativeEmptyAndPreservesPartitionKey 封装TestSnapshotMetadataReportsAuthoritativeEmptyAndPreservesPartitionKey业务协调。
func TestSnapshotMetadataReportsAuthoritativeEmptyAndPreservesPartitionKey(t *testing.T) {
	if // got、ok 用于本次流程后续判断的got、ok
	got, ok := SnapshotFromMetadataOK(`{"other":true}`); ok || got != nil {
		t.Fatalf("missing snapshot=(%+v,%v)", got, ok)
	}
	// metadata 用于本次流程后续判断的metadata
	metadata := MetadataWithSnapshot(`{"other":true}`, []BrowserCookie{})
	if // got、ok 用于本次流程后续判断的got、ok
	got, ok := SnapshotFromMetadataOK(metadata); !ok || got == nil || len(got) != 0 {
		t.Fatalf("empty snapshot=(%+v,%v) metadata=%s", got, ok, metadata)
	}
	metadata = MetadataWithSnapshot("", []BrowserCookie{{
		Name: "chip", Value: "secret", Domain: ".goofish.com", Path: "/", PartitionKey: "https://example.com",
	}})
	// got、ok 用于本次流程后续判断的got、ok
	got, ok := SnapshotFromMetadataOK(metadata)
	if !ok || len(got) != 1 || got[0].PartitionKey != "https://example.com" {
		t.Fatalf("partition snapshot=(%+v,%v) metadata=%s", got, ok, metadata)
	}
}

// TestChangedSnapshotLabels 封装TestChangedSnapshotLabels业务协调。
func TestChangedSnapshotLabels(t *testing.T) {
	// before 用于本次流程后续判断的before
	before := []BrowserCookie{{Name: "a", Value: "1", Domain: ".goofish.com", Path: "/"}}
	// after 用于本次流程后续判断的after
	after := []BrowserCookie{{Name: "a", Value: "2", Domain: ".goofish.com", Path: "/"}}
	// got 用于本次流程后续判断的got
	got := ChangedSnapshotLabels(before, after)
	if len(got) != 1 || got[0] != "a@.goofish.com/" {
		t.Fatalf("ChangedSnapshotLabels=%v", got)
	}
}
