package browser

import (
	"strings"
	"testing"
)

// TestParseCookieStrRoundTrip 负责TestParse登录凭证StrRoundTrip相关处理。
func TestParseCookieStrRoundTrip(t *testing.T) {
	// in 保存in，供当前处理流程使用
	in := "unb=999; _m_h5_tk=abc_1; cookie2=xyz"
	// m 保存m，供当前处理流程使用
	m := parseCookieStr(in)
	if m["unb"] != "999" || m["_m_h5_tk"] != "abc_1" || m["cookie2"] != "xyz" {
		t.Fatalf("解析异常: %+v", m)
	}
	// out 保存out，供当前处理流程使用
	out := cookieMarshal(m)
	// 顺序不保证，逐项检查。
	for _, kv := range []string{"unb=999", "_m_h5_tk=abc_1", "cookie2=xyz"} {
		if !strings.Contains(out, kv) {
			t.Fatalf("marshal 缺少 %q: %q", kv, out)
		}
	}
}

// TestParseCookieStrToPlaywright 负责TestParse登录凭证StrToPlaywright相关处理。
func TestParseCookieStrToPlaywright(t *testing.T) {
	// cookies 保存cookies，供当前处理流程使用
	cookies := parseCookieStrToPlaywright("a=1; b=2")
	if len(cookies) != 2 {
		t.Fatalf("每个 Cookie 只应注入一次，got %d", len(cookies))
	}
	// domains 保存domains，供当前处理流程使用
	domains := make(map[string]bool)
	// c 表示当前遍历过程中的c
	for _, c := range cookies {
		if c.Domain == nil {
			t.Fatalf("domain 不能为空: %+v", c.Domain)
		}
		domains[*c.Domain] = true
		if c.Path == nil || *c.Path != "/" {
			t.Fatalf("path 应为 /: %+v", c.Path)
		}
	}
	if len(domains) != 1 || !domains[goofishDot] {
		t.Fatalf("Cookie 只能注入 %s: %+v", goofishDot, domains)
	}
}

// TestParseCookieStrEmpty 负责TestParse登录凭证StrEmpty相关处理。
func TestParseCookieStrEmpty(t *testing.T) {
	if // got 保存got，供当前处理流程使用
	got := parseCookieStr(""); len(got) != 0 {
		t.Fatalf("空串应返回空 map, got %+v", got)
	}
	if // got 保存got，供当前处理流程使用
	got := parseCookieStrToPlaywright(",,, ;"); len(got) != 0 {
		t.Fatalf("无效串应返回空, got %+v", got)
	}
}

// TestCookiesToMapAndStr 负责TestCookiesToMapAndStr相关处理。
func TestCookiesToMapAndStr(t *testing.T) {
	// 用 cookiesToMap/cookiesToStr 覆盖（构造 Cookie 不导出字段，借 parse 间接）。
	m := map[string]string{"unb": "1", "cna": "xx"}
	// s 保存s，供当前处理流程使用
	s := cookieMarshal(m)
	// m2 保存m2，供当前处理流程使用
	m2 := parseCookieStr(s)
	if m2["unb"] != "1" || m2["cna"] != "xx" {
		t.Fatalf("往返异常: %+v", m2)
	}
}

// TestStealthScriptKeepsNativeFingerprint 负责TestStealthScriptKeepsNativeFingerprint相关处理。
func TestStealthScriptKeepsNativeFingerprint(t *testing.T) {
	// s 保存s，供当前处理流程使用
	s := stealthScript()
	if strings.Contains(s, "{{") {
		t.Fatalf("stealth 脚本仍有未替换占位符: %q", s[:min(200, len(s))])
	}
	if !strings.Contains(s, "webdriver") {
		t.Fatal("stealth 脚本应只规范化 webdriver")
	}
	// forbidden 表示当前遍历过程中的forbidden
	for _, forbidden := range []string{"toDataURL", "WebGL", "hardwareConcurrency", "deviceMemory", "RTCPeerConnection", "Math.random", "navigator.platform"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("stealth 脚本不应伪造 %q", forbidden)
		}
	}
}

// TestStealthScriptStable 负责TestStealthScriptStable相关处理。
func TestStealthScriptStable(t *testing.T) {
	if stealthScript() != stealthScript() {
		t.Fatal("同一浏览器配置不应产生漂移的指纹脚本")
	}
}
