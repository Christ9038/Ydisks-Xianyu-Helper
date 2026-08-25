package server

import "testing"

// TestNormalizeOrderTimestamp 明确订单无时区数据库文本必须按 UTC 输出。
func TestNormalizeOrderTimestamp(t *testing.T) {
	// cases 覆盖数据库常规格式、带小数秒格式、已有时区格式和非法历史值。
	cases := []struct {
		// name 是当前时间格式场景的可读名称。
		name string
		// input 是订单接口或数据库可能提供的原始时间文本。
		input string
		// want 是 HTTP 订单 DTO 应输出的带时区时间文本。
		want string
	}{
		{name: "database UTC text", input: "2026-08-25 16:00:00", want: "2026-08-25T16:00:00Z"},
		{name: "database fractional UTC text", input: "2026-08-25 16:00:00.123456", want: "2026-08-25T16:00:00.123456Z"},
		{name: "offset timestamp", input: "2026-08-26T00:00:00+08:00", want: "2026-08-25T16:00:00Z"},
		{name: "invalid legacy value", input: "created", want: "created"},
	}
	// testCase 是当前遍历的订单时间格式场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got 是统一订单时间函数转换后的 HTTP 时间文本。
			got := normalizeOrderTimestamp(testCase.input)
			if got != testCase.want {
				t.Fatalf("normalizeOrderTimestamp(%q)=%q, want %q", testCase.input, got, testCase.want)
			}
		})
	}
}
