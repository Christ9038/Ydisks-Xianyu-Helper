package cookierefresh

import (
	"net/http"
	"testing"
)

// TestCookieScopeHelpers 验证 Cookie 路径、默认路径和 SameSite 标签的边界映射。
func TestCookieScopeHelpers(t *testing.T) {
	// cases 描述请求路径与 Cookie 路径之间的匹配边界。
	cases := []struct {
		requestPath string
		cookiePath  string
		want        bool
	}{
		{requestPath: "", cookiePath: "", want: true},
		{requestPath: "/a", cookiePath: "/a", want: true},
		{requestPath: "/abc", cookiePath: "/a", want: false},
		{requestPath: "/a/b", cookiePath: "/a", want: true},
		{requestPath: "/a/b", cookiePath: "/a/", want: true},
	}
	// testCase 表示当前遍历的 Cookie 路径匹配用例。
	for _, testCase := range cases {
		// got 保存当前请求路径是否匹配 Cookie 作用域的结果。
		if got := cookiePathMatches(testCase.requestPath, testCase.cookiePath); got != testCase.want {
			t.Fatalf("路径匹配错误 request=%q cookie=%q got=%v", testCase.requestPath, testCase.cookiePath, got)
		}
	}
	// pathCases 描述请求地址对应的默认 Cookie 路径。
	pathCases := map[string]string{"": "/", "/": "/", "plain": "/", "/item": "/", "/item/detail": "/item"}
	// requestPath、want 表示当前遍历的默认路径用例。
	for requestPath, want := range pathCases {
		// got 保存当前请求路径推导出的默认 Cookie 路径。
		if got := defaultCookiePath(requestPath); got != want {
			t.Fatalf("默认路径错误 request=%q got=%q want=%q", requestPath, got, want)
		}
	}
	// labels 保存各 SameSite 枚举对应的协议标签。
	labels := map[http.SameSite]string{
		http.SameSiteStrictMode:  "Strict",
		http.SameSiteLaxMode:     "Lax",
		http.SameSiteNoneMode:    "None",
		http.SameSiteDefaultMode: "",
	}
	// sameSite、want 表示当前遍历的 SameSite 标签用例。
	for sameSite, want := range labels {
		// got 保存当前 SameSite 枚举对应的协议标签。
		if got := sameSiteLabel(sameSite); got != want {
			t.Fatalf("SameSite 标签错误 value=%v got=%q want=%q", sameSite, got, want)
		}
	}
}

// TestCookieMetadataInvalidInputs 验证损坏 metadata 和空输入不会伪造可用快照。
func TestCookieMetadataInvalidInputs(t *testing.T) {
	// metadataCases 描述 metadata 解析失败和快照结构失败的输入。
	metadataCases := []string{"{", `{"cookies_refresh_snapshot":"bad"}`}
	// metadata 表示当前遍历的损坏 metadata。
	for _, metadata := range metadataCases {
		// got、ok 保存损坏 metadata 的解析结果。
		got, ok := SnapshotFromMetadataOK(metadata)
		if ok || got != nil {
			t.Fatalf("损坏 metadata 不应解析成功: metadata=%q got=%v ok=%v", metadata, got, ok)
		}
	}
	// got 保存损坏 JSON 经过兼容读取后的空结果。
	if got := SnapshotFromMetadata("{"); got != nil {
		t.Fatalf("损坏 metadata 应返回 nil 快照: %v", got)
	}
	// got 保存空 metadata 的清理结果。
	if got := MetadataWithoutSnapshot(""); got != "" {
		t.Fatalf("空 metadata 清理结果错误: %q", got)
	}
	// got 保存损坏 metadata 的原样保留结果。
	if got := MetadataWithoutSnapshot("{"); got != "{" {
		t.Fatalf("损坏 metadata 应原样保留: %q", got)
	}
	// got 保存空 Cookie 快照的压缩结果。
	if got := CookieStringFromSnapshot(nil); got != "" {
		t.Fatalf("空快照字符串应为空: %q", got)
	}
}
