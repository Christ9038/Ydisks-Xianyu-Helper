package protocol

import (
	"net/url"
	"strings"
	"testing"
)

// TestTransCookies_Table 表驱动覆盖 cookie 解析的边界。
func TestTransCookies_Table(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := []struct {
		name string
		in   string
		want map[string]string
	}{
		{"empty string", "", map[string]string{}},
		{"single cookie", "a=1", map[string]string{"a": "1"}},
		{"multiple cookies", "a=1; b=2; c=3", map[string]string{"a": "1", "b": "2", "c": "3"}},
		{"no separator on a fragment", "garbage", map[string]string{}},
		{"mix of valid and invalid", "a=1; junk; b=2", map[string]string{"a": "1", "b": "2"}},
		{"duplicate key last wins", "k=1; k=2; k=3", map[string]string{"k": "3"}},
		{"empty value", "k=", map[string]string{"k": ""}},
		{"value with equals sign", "k=a=b=c", map[string]string{"k": "a=b=c"}},
		{"url encoded value", "k=" + url.QueryEscape("a b&c=d"), map[string]string{"k": "a+b%26c%3Dd"}},
		{"leading/trailing spaces in value", "k= v ", map[string]string{"k": "v"}},
		{"only separator", "=", map[string]string{}},
		{"single fragment no space separator", "a=1;b=2", map[string]string{"a": "1", "b": "2"}},
		{"trailing separator", "a=1; b=2;", map[string]string{"a": "1", "b": "2"}},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 保存got，供当前处理流程使用
			got := TransCookies(tc.in)
			if len(got) != len(tc.want) {
				t.Fatalf("TransCookies(%q) = %v (len %d), want %v (len %d)", tc.in, got, len(got), tc.want, len(tc.want))
			}
			// k、v 表示当前遍历过程中的k、v
			for k, v := range tc.want {
				if got[k] != v {
					t.Fatalf("TransCookies(%q)[%q] = %q, want %q", tc.in, k, got[k], v)
				}
			}
		})
	}
}

// TestSignToken 表驱动覆盖从 cookie 串提取 _m_h5_tk token 的边界。
func TestSignToken(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"no _m_h5_tk", "a=1; b=2", ""},
		{"empty _m_h5_tk", "_m_h5_tk=", ""},
		{"token with timestamp", "_m_h5_tk=abc_1700000000000", "abc"},
		{"token without underscore", "_m_h5_tk=plain", "plain"},
		{"token is just underscore", "_m_h5_tk=_", ""},
		{"token starts with underscore", "_m_h5_tk=_abc", ""},
		{"token with multiple underscores takes first", "_m_h5_tk=a_b_c", "a"},
		{"token among other cookies", "a=1; _m_h5_tk=tken_123; b=2", "tken"},
	}
	// tc 表示当前遍历过程中的tc
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 保存got，供当前处理流程使用
			got := SignToken(tc.in)
			if got != tc.want {
				t.Fatalf("SignToken(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestSignTokenUsesFirstScopedDuplicate 负责TestSign令牌UsesFirstScopedDuplicate相关处理。
func TestSignTokenUsesFirstScopedDuplicate(t *testing.T) {
	// got 保存got，供当前处理流程使用
	got := SignToken("_m_h5_tk=narrow_1; other=x; _m_h5_tk=wide_2")
	if got != "narrow" {
		t.Fatalf("SignToken duplicate=%q want narrow", got)
	}
}

// TestSignToken_ConsistentWithTransCookies SignToken 必须基于 TransCookies 的解析结果。
func TestSignToken_ConsistentWithTransCookies(t *testing.T) {
	// cookies 保存cookies，供当前处理流程使用
	cookies := "x=1; _m_h5_tk=mytoken_999; y=2"
	// want 保存want，供当前处理流程使用
	want := strings.SplitN(TransCookies(cookies)["_m_h5_tk"], "_", 2)[0]
	if // got 保存got，供当前处理流程使用
	got := SignToken(cookies); got != want {
		t.Fatalf("SignToken = %q, want %q derived from TransCookies", got, want)
	}
}
