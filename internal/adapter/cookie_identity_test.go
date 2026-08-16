package adapter

import "testing"

// TestAccountIDFromCookie 验证扫码 Cookie 中账号标识的解析及缺失字段行为。
func TestAccountIDFromCookie(t *testing.T) {
	// got 是从完整 Cookie 字符串提取出的平台账号标识。
	if got := AccountIDFromCookie("sid=value; unb=account-1; foo=bar"); got != "account-1" {
		t.Fatalf("账号标识解析错误: %q", got)
	}
	// got 是缺少 unb 字段时的空账号标识结果。
	if got := AccountIDFromCookie("sid=value"); got != "" {
		t.Fatalf("缺少 unb 时应返回空字符串: %q", got)
	}
}
