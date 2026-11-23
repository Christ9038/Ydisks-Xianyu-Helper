package ws

import (
	"net/http"
	"strings"
	"testing"
)

// TestExtractSendReason 覆盖原因字段优先级、脱敏、截断、换行折叠与无原因字段时的结构线索。
func TestExtractSendReason(t *testing.T) {
	// cases 保存响应到期望文本片段的映射；want 为子串，notWant 为必须不出现的内容。
	cases := []struct {
		name    string
		resp    map[string]any
		want    string
		notWant string
	}{
		{"headers优先", map[string]any{"headers": map[string]any{"reason": "会话受限"}, "body": map[string]any{"msg": "other"}}, "reason=会话受限", "other"},
		{"body字段", map[string]any{"body": map[string]any{"message": "频率过高"}}, "message=频率过高", ""},
		{"凭证脱敏", map[string]any{"body": map[string]any{"msg": "bad token=abc123 see https://a.com/p?k=v"}}, "token=<redacted>", "abc123"},
		{"换行折叠", map[string]any{"body": map[string]any{"msg": "a\nb\r\nc"}}, "msg=a b c", "\n"},
		{"空白字符串跳过", map[string]any{"body": map[string]any{"reason": "  ", "msg": "x"}}, "msg=x", ""},
		{"无原因字段给键名", map[string]any{"headers": map[string]any{"mid": "SECRET-MID", "app-key": "k"}, "body": map[string]any{"z": 1, "a": 2}}, "headers=[app-key,mid] body=[a,z]", "SECRET-MID"},
		{"空响应", map[string]any{}, "headers=[] body=[]", ""},
	}
	// tc 是当前子用例，包含输入响应和期望断言。
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 保存提取并脱敏后的原因文本。
			got := extractSendReason(tc.resp)
			if !strings.Contains(got, tc.want) {
				t.Fatalf("got=%q want contains %q", got, tc.want)
			}
			if tc.notWant != "" && strings.Contains(got, tc.notWant) {
				t.Fatalf("got=%q must not contain %q", got, tc.notWant)
			}
		})
	}
	// long 是超过上限的中文原因，截断必须按字符且不产生乱码。
	long := extractSendReason(map[string]any{"body": map[string]any{"msg": strings.Repeat("拒", 500)}})
	// runes 是截断结果的字符序列，用于校验按字符截断。
	runes := []rune(long)
	if len(runes) != sendReasonMaxRunes+1 || !strings.HasSuffix(long, "…") {
		t.Fatalf("truncate len=%d", len(runes))
	}
}

// TestSendChatContentRejectedCarriesReason 覆盖 400 拒绝时平台原因进入 SendError 文本，且分类与状态码不变。
func TestSendChatContentRejectedCarriesReason(t *testing.T) {
	// connection、requests 是返回 400 与原因的本地平台替身；requests 在此不需检查。
	connection, _ := newAPIResponseConn(t, map[string]any{"reason": "对方会话受限 cookie=SECRET"}, http.StatusBadRequest)
	// sendErr 保存发送结果。
	sendErr := connection.SendText(t.Context(), "me", "cid", "peer", "hi")
	if SendResultKind(sendErr) != SendRejected {
		t.Fatalf("kind=%q err=%v", SendResultKind(sendErr), sendErr)
	}
	// text 是错误文本，必须含原因而不含凭证值。
	text := sendErr.Error()
	if !strings.Contains(text, "code=400") || !strings.Contains(text, "reason=对方会话受限") || strings.Contains(text, "SECRET") {
		t.Fatalf("text=%q", text)
	}
}
