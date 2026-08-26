package cookierefresh

import (
	"testing"
	"time"
)

// TestCookieStringAndSnapshotBoundaryInputs 覆盖 Cookie 头非法片段、旧快照键和作用域标签边界。
func TestCookieStringAndSnapshotBoundaryInputs(t *testing.T) {
	// parsed 保存过滤非法片段并覆盖重复名称后的 Cookie 映射。
	parsed := ParseCookieString("; no-equals; a=1; a=2; =bad; spaced = value ")
	if len(parsed) != 2 || parsed["a"] != "2" || parsed["spaced"] != "value" {
		t.Fatalf("parsed cookie=%v", parsed)
	}
	// legacySnapshot、legacyOK 保存旧 metadata 键的兼容读取结果。
	legacySnapshot, legacyOK := SnapshotFromMetadataOK(`{"cookie_refresh_snapshot":[{"name":"sid","value":"v"}]}`)
	if !legacyOK || len(legacySnapshot) != 1 || legacySnapshot[0].Path != "/" {
		t.Fatalf("legacy snapshot=%+v ok=%v", legacySnapshot, legacyOK)
	}
	// labels 保存新增、删除和分区 Cookie 的变化标签。
	labels := ChangedSnapshotLabels(
		[]BrowserCookie{{Name: "old", Domain: ".example.com"}, {Name: "gone", Domain: ".example.com"}},
		[]BrowserCookie{{Name: "old", Domain: ".example.com", PartitionKey: "partition"}, {Name: "new"}},
	)
	if len(labels) != 4 || labels[0] != "gone@.example.com/" || labels[1] != "new" || labels[2] != "old@.example.com/" || labels[3] != "old@.example.com/#partition" {
		t.Fatalf("changed labels=%v", labels)
	}
}

// TestCookieRefreshFilteringBranches 覆盖 Cookie 作用域、属性校验、删除和异常输入分支。
func TestCookieRefreshFilteringBranches(t *testing.T) {
	// merged 保存空名称 Set-Cookie 被过滤后的结果。
	merged := MergeSetCookies("a=1", []string{" =ignored"})
	if merged != "a=1" {
		t.Fatalf("空名称 Cookie 未过滤: %q", merged)
	}
	// reconciled 保存扁平 Cookie 缺失字段后的快照结果。
	reconciled := ReconcileSnapshotWithCookieString([]BrowserCookie{{Name: "sid", Value: "old"}}, "other=v")
	if len(reconciled) != 1 || reconciled[0].Name != "other" {
		t.Fatalf("缺失 Cookie 未移除: %+v", reconciled)
	}
	// invalidHeader、invalidHeaderOK 保存非法请求地址的作用域结果。
	invalidHeader, invalidHeaderOK := ScopedCookieHeaderForRequest([]BrowserCookie{}, "%", "", time.Now())
	if invalidHeaderOK || invalidHeader != "" {
		t.Fatalf("非法请求地址不应生成 Cookie 头: %q %v", invalidHeader, invalidHeaderOK)
	}
	// insecureHeader、insecureHeaderOK 保存 HTTP 请求跳过 Secure Cookie 后的结果。
	insecureHeader, insecureHeaderOK := ScopedCookieHeaderForRequest([]BrowserCookie{{Name: "secure", Value: "v", Domain: ".goofish.com", Path: "/", Secure: true}}, "http://www.goofish.com/", "", time.Now())
	if !insecureHeaderOK || insecureHeader != "" {
		t.Fatalf("HTTP 请求不应携带 Secure Cookie: %q %v", insecureHeader, insecureHeaderOK)
	}
	// invalidSnapshot 保存非法请求地址下 ApplySetCookies 的原快照结果。
	invalidSnapshot := []BrowserCookie{{Name: "sid", Value: "v"}}
	// got 保存非法请求地址下未被修改的 Cookie 快照。
	if got := ApplySetCookies(invalidSnapshot, "%", []string{"new=v"}, time.Now()); len(got) != 1 || got[0].Name != "sid" {
		t.Fatalf("非法请求地址不应修改快照: %+v", got)
	}
	// now 保存 Set-Cookie 过期判断使用的固定时刻。
	now := time.Unix(1_800_000_000, 0)
	// filtered := ApplySetCookies 过滤非法 Set-Cookie、跨域 Cookie、危险前缀和不安全属性。
	filtered := ApplySetCookies(nil, "https://www.goofish.com/path/item", []string{
		"not a cookie",
		"cross=v; Domain=example.com",
		"none=v; SameSite=None",
		"__Secure-bad=v",
		"__Host-bad=v; Secure; Domain=.goofish.com; Path=/",
		"part=v; Secure; Partitioned",
	}, now)
	if len(filtered) != 0 {
		t.Fatalf("非法 Cookie 未全部过滤: %+v", filtered)
	}
	// partitioned 保存带顶级站点键的合法分区 Cookie。
	partitioned := ApplySetCookies(nil, "https://www.goofish.com/path/item", []string{"part=v; Secure; Partitioned"}, now, "https://goofish.com")
	if len(partitioned) != 1 || partitioned[0].PartitionKey != "https://goofish.com" {
		t.Fatalf("合法分区 Cookie 未保留: %+v", partitioned)
	}
	// replaced 保存同一作用域 Cookie 替换后的值。
	replaced := ApplySetCookies(partitioned, "https://www.goofish.com/path/item", []string{"part=new; Secure; Partitioned"}, now, "https://goofish.com")
	if len(replaced) != 1 || replaced[0].Value != "new" {
		t.Fatalf("同作用域 Cookie 未替换: %+v", replaced)
	}
	// deleted 保存删除不存在 Cookie 后的稳定结果。
	deleted := ApplySetCookies(replaced, "https://www.goofish.com/path/item", []string{"missing=; Max-Age=0"}, now)
	if len(deleted) != 1 {
		t.Fatalf("删除不存在 Cookie 不应影响已有快照: %+v", deleted)
	}
	// duplicateState 保存重复作用域快照经过归一化后的替换结果。
	duplicateState := ApplySetCookies([]BrowserCookie{{Name: "dup", Value: "old", Domain: ".goofish.com", Path: "/"}, {Name: "dup", Value: "new", Domain: ".goofish.com", Path: "/"}}, "https://www.goofish.com/", nil, now)
	if len(duplicateState) != 1 || duplicateState[0].Value != "new" {
		t.Fatalf("重复作用域快照未收敛: %+v", duplicateState)
	}
	// unchangedLabels 保存未变化 Cookie 不产生变化标签的结果。
	if labels := ChangedSnapshotLabels([]BrowserCookie{{Name: "same", Value: "v"}}, []BrowserCookie{{Name: "same", Value: "v"}}); len(labels) != 0 {
		t.Fatalf("未变化 Cookie 不应生成标签: %v", labels)
	}
	// pathMismatch 表示请求路径不以 Cookie 路径开头的情况。
	if cookiePathMatches("/x", "/a") {
		t.Fatal("路径前缀不匹配时不应发送 Cookie")
	}
	// domainCases 覆盖空域名、根域名和子域名的匹配判定。
	domainCases := []struct {
		// host 是请求主机名。
		host string
		// domain 是 Cookie 域属性。
		domain string
		// want 表示是否应匹配。
		want bool
	}{
		{host: "www.goofish.com", domain: "", want: false},
		{host: "www.goofish.com", domain: ".goofish.com", want: true},
		{host: "www.goofish.com", domain: "goofish.com", want: false},
	}
	// domainCase 表示当前域名匹配边界。
	for _, domainCase := range domainCases {
		// got 保存当前域名匹配边界的实际结果。
		if got := cookieDomainMatches(domainCase.host, domainCase.domain); got != domainCase.want {
			t.Fatalf("域名匹配错误: %+v got=%v", domainCase, got)
		}
	}
	// invalidMetadata 保存快照字段 JSON 损坏时的兼容结果。
	if got, ok := SnapshotFromMetadataOK(`{"cookie_refresh_snapshot":"["}`); ok || got != nil {
		t.Fatalf("损坏快照字段不应成功: %+v %v", got, ok)
	}
	// nilSnapshot、nilSnapshotOK 保存合法 null 快照被规范化为空切片后的结果。
	nilSnapshot, nilSnapshotOK := SnapshotFromMetadataOK(`{"cookie_refresh_snapshot":null}`)
	if !nilSnapshotOK || nilSnapshot == nil {
		t.Fatalf("null 快照应返回非 nil 空切片: %+v %v", nilSnapshot, nilSnapshotOK)
	}
	// invalidCookieString 保存空名称快照被压缩后的结果。
	if got := CookieStringFromSnapshot([]BrowserCookie{{Name: "", Value: "ignored"}}); got != "" {
		t.Fatalf("空名称快照不应进入 Cookie 头: %q", got)
	}
}
