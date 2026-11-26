package adapter

import (
	"strings"
	"testing"

	"xianyu-go/internal/automation"
)

// TestReviewCopySystemPrompt 验证风格、字数范围与附加要求被写入提示词，且不含隐私字段名。
func TestReviewCopySystemPrompt(t *testing.T) {
	// cases 覆盖风格映射、字数缺省与修正、附加要求。
	cases := []struct {
		name   string
		prompt automation.ReviewCopyPrompt
		wants  []string
	}{
		{"默认", automation.ReviewCopyPrompt{}, []string{"真诚自然", "25 到 50"}},
		{"热情带附加", automation.ReviewCopyPrompt{Style: "warm", MinChars: 20, MaxChars: 80, Extra: "提到包装"}, []string{"热情洋溢", "20 到 80", "补充要求：提到包装"}},
		{"简洁且最小值非法", automation.ReviewCopyPrompt{Style: "concise", MinChars: 90, MaxChars: 30}, []string{"简洁务实", "15 到 30"}},
	}
	for /* tc 是一个提示词场景。 */ _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// got 是按场景拼装出的系统提示词。
			got := reviewCopySystemPrompt(tc.prompt)
			for /* want 是必须出现的片段。 */ _, want := range tc.wants {
				if !strings.Contains(got, want) {
					t.Fatalf("提示词缺少 %q: %s", want, got)
				}
			}
			if strings.Contains(got, "昵称") || strings.Contains(got, "订单号") {
				t.Fatalf("提示词不应包含买家或订单信息: %s", got)
			}
		})
	}
}
