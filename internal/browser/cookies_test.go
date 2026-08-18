package browser

import (
	"strings"
	"testing"
)

// TestParseCookieStrRoundTrip 封装TestParse登录凭证StrRoundTrip业务协调。
func TestParseCookieStrRoundTrip(t *testing.T) {
	// in 用于本次流程后续判断的in
	in := "unb=999; _m_h5_tk=abc_1; cookie2=xyz"
	// m 用于本次流程后续判断的m
	m := parseCookieStr(in)
	if m["unb"] != "999" || m["_m_h5_tk"] != "abc_1" || m["cookie2"] != "xyz" {
		t.Fatalf("解析异常: %+v", m)
	}
	// out 用于本次流程后续判断的out
	out := cookieMarshal(m)
	// 顺序不保证，逐项检查。
	for _, kv := range []string{"unb=999", "_m_h5_tk=abc_1", "cookie2=xyz"} {
		if !strings.Contains(out, kv) {
			t.Fatalf("marshal 缺少 %q: %q", kv, out)
		}
	}
}

// TestParseCookieStrToPlaywright 封装TestParse登录凭证StrToPlaywright业务协调。
func TestParseCookieStrToPlaywright(t *testing.T) {
	// cookies 用于本次流程后续判断的cookies
	cookies := parseCookieStrToPlaywright("a=1; b=2")
	if len(cookies) != 2 {
		t.Fatalf("每个 Cookie 只应注入一次，got %d", len(cookies))
	}
	// domains 用于本次流程后续判断的domains
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

// TestParseCookieStrEmpty 封装TestParse登录凭证StrEmpty业务协调。
func TestParseCookieStrEmpty(t *testing.T) {
	if // got 用于本次流程后续判断的got
	got := parseCookieStr(""); len(got) != 0 {
		t.Fatalf("空串应返回空 map, got %+v", got)
	}
	if // got 用于本次流程后续判断的got
	got := parseCookieStrToPlaywright(",,, ;"); len(got) != 0 {
		t.Fatalf("无效串应返回空, got %+v", got)
	}
}

// TestCookiesToMapAndStr 封装TestCookiesToMapAndStr业务协调。
func TestCookiesToMapAndStr(t *testing.T) {
	// 用 cookiesToMap/cookiesToStr 覆盖（构造 Cookie 不导出字段，借 parse 间接）。
	m := map[string]string{"unb": "1", "cna": "xx"}
	// s 用于本次流程后续判断的s
	s := cookieMarshal(m)
	// m2 用于本次流程后续判断的m2
	m2 := parseCookieStr(s)
	if m2["unb"] != "1" || m2["cna"] != "xx" {
		t.Fatalf("往返异常: %+v", m2)
	}
}

// TestStealthScriptKeepsNativeFingerprint 封装TestStealthScriptKeepsNativeFingerprint业务协调。
func TestStealthScriptKeepsNativeFingerprint(t *testing.T) {
	// s 用于本次流程后续判断的s
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

// TestStealthScriptStable 封装TestStealthScriptStable业务协调。
func TestStealthScriptStable(t *testing.T) {
	if stealthScript() != stealthScript() {
		t.Fatal("同一浏览器配置不应产生漂移的指纹脚本")
	}
}
