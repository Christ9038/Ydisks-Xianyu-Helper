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

// TestStealthScriptKeepsNativeFingerprint 验证脚本包含授权的补齐项，且不触碰 Canvas、内存容量、WebRTC、随机数与平台字段。
func TestStealthScriptKeepsNativeFingerprint(t *testing.T) {
	// s 用于本次流程后续判断的s
	s := stealthScript()
	if strings.Contains(s, "{{") {
		t.Fatalf("stealth 脚本仍有未替换占位符: %q", s[:min(200, len(s))])
	}
	if !strings.Contains(s, "webdriver") {
		t.Fatal("stealth 脚本应规范化 webdriver")
	}
	// required 是用户授权加入的图形厂商、逻辑核数、编解码和插件补齐，必须保留在脚本中。
	for _, required := range []string{"WebGLRenderingContext", "hardwareConcurrency", "canPlayType", "PluginArray"} {
		if !strings.Contains(s, required) {
			t.Fatalf("stealth 脚本应包含授权的 %q 处理", required)
		}
	}
	// forbidden 仍未获授权改写的指纹项：Canvas、内存容量、WebRTC、随机数与平台字段。
	for _, forbidden := range []string{"toDataURL", "deviceMemory", "RTCPeerConnection", "Math.random", "navigator.platform"} {
		if strings.Contains(s, forbidden) {
			t.Fatalf("stealth 脚本不应伪造 %q", forbidden)
		}
	}
}

// TestStealthScriptNeverTouchesUserAgentFingerprint 验证 stealth 脚本不读取或改写 UA、Client Hints 与平台字段，保证与主引擎 UA 归一化互不冲突。
func TestStealthScriptNeverTouchesUserAgentFingerprint(t *testing.T) {
	// script 是注入每个浏览器上下文的初始化脚本。
	script := stealthScript()
	// token 是脚本中不得出现的 UA 相关标识。
	for _, token := range []string{"userAgent", "userAgentData", "Sec-CH", "HeadlessChrome", "appVersion", "oscpu", "vendor"} {
		if strings.Contains(script, token) {
			t.Fatalf("stealth 脚本不应触碰 UA 相关字段 %q", token)
		}
	}
}

// TestStealthScriptStable 封装TestStealthScriptStable业务协调。
func TestStealthScriptStable(t *testing.T) {
	if stealthScript() != stealthScript() {
		t.Fatal("同一浏览器配置不应产生漂移的指纹脚本")
	}
}
