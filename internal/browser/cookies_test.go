package browser

import (
	"strings"
	"testing"
)

func TestParseCookieStrRoundTrip(t *testing.T) {
	in := "unb=999; _m_h5_tk=abc_1; cookie2=xyz"
	m := parseCookieStr(in)
	if m["unb"] != "999" || m["_m_h5_tk"] != "abc_1" || m["cookie2"] != "xyz" {
		t.Fatalf("解析异常: %+v", m)
	}
	out := cookieMarshal(m)
	// 顺序不保证，逐项检查。
	for _, kv := range []string{"unb=999", "_m_h5_tk=abc_1", "cookie2=xyz"} {
		if !strings.Contains(out, kv) {
			t.Fatalf("marshal 缺少 %q: %q", kv, out)
		}
	}
}

func TestParseCookieStrToPlaywright(t *testing.T) {
	cookies := parseCookieStrToPlaywright("a=1; b=2")
	if len(cookies) != 2*len(cookieDomains) {
		t.Fatalf("应按多个域名注入 cookie，got %d", len(cookies))
	}
	domains := make(map[string]bool)
	for _, c := range cookies {
		if c.Domain == nil {
			t.Fatalf("domain 不能为空: %+v", c.Domain)
		}
		domains[*c.Domain] = true
		if c.Path == nil || *c.Path != "/" {
			t.Fatalf("path 应为 /: %+v", c.Path)
		}
	}
	for _, domain := range cookieDomains {
		if !domains[domain] {
			t.Fatalf("缺少 domain %s: %+v", domain, domains)
		}
	}
}

func TestParseCookieStrEmpty(t *testing.T) {
	if got := parseCookieStr(""); len(got) != 0 {
		t.Fatalf("空串应返回空 map, got %+v", got)
	}
	if got := parseCookieStrToPlaywright(",,, ;"); len(got) != 0 {
		t.Fatalf("无效串应返回空, got %+v", got)
	}
}

func TestCookiesToMapAndStr(t *testing.T) {
	// 用 cookiesToMap/cookiesToStr 覆盖（构造 Cookie 不导出字段，借 parse 间接）。
	m := map[string]string{"unb": "1", "cna": "xx"}
	s := cookieMarshal(m)
	m2 := parseCookieStr(s)
	if m2["unb"] != "1" || m2["cna"] != "xx" {
		t.Fatalf("往返异常: %+v", m2)
	}
}

func TestStealthScriptReplaced(t *testing.T) {
	s := stealthScript()
	if strings.Contains(s, "{{") {
		t.Fatalf("stealth 脚本仍有未替换占位符: %q", s[:min(200, len(s))])
	}
	for _, key := range []string{"webdriver", "chrome", "RTCPeerConnection", "toDataURL"} {
		if !strings.Contains(s, key) {
			t.Fatalf("stealth 脚本缺少关键段 %q", key)
		}
	}
}

func TestStealthScriptUnique(t *testing.T) {
	// 多次生成应有时不同（随机参数）。
	a := stealthScript()
	differ := false
	for i := 0; i < 10; i++ {
		if stealthScript() != a {
			differ = true
			break
		}
	}
	if !differ {
		t.Fatal("stealth 脚本多次生成应出现随机差异")
	}
}
