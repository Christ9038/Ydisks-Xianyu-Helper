package automation

import (
	"context"
	"strings"
	"testing"
)

// reviewRuleDraft 构造一条求评价规则草稿，动作由调用方提供。
func reviewRuleDraft(actions ...ActionDraft) RuleDraft {
	return RuleDraft{CookieID: "account-1", TriggerType: TriggerReviewMissingTimeout, Actions: actions}
}

// TestRuleServiceReviewCopyThreeActions 验证“引导语 + 好评文案 + 复制提示”三段规则可被规范化并保留抖动配置。
func TestRuleServiceReviewCopyThreeActions(t *testing.T) {
	// service 是绑定测试端口的规则应用服务。
	service := NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{})
	// input 是规范化后的规则输入；err 是三段规则的校验错误，应为空。
	input, err := service.Normalize(context.Background(), 42, reviewRuleDraft(
		ActionDraft{ActionType: ActionSendText, MessageTemplate: "亲，满意的话麻烦给个评价"},
		ActionDraft{ActionType: ActionSendReviewCopy, DelaySeconds: 5, ConfigJSON: `{"source":"mixed","pool":["不错"],"ai_ratio":30,"delay_jitter_seconds":5}`},
		ActionDraft{ActionType: ActionSendText, MessageTemplate: "直接复制上面的内容就行", DelaySeconds: 5, ConfigJSON: `{"delay_jitter_seconds":5}`},
	))
	if err != nil || len(input.Actions) != 3 || input.Actions[1].ActionType != ActionSendReviewCopy {
		t.Fatalf("三段规则应通过校验: %v %+v", err, input.Actions)
	}
}

// TestRuleServiceReviewCopyOnlyIsValid 验证只有好评文案动作也满足求评价规则的最小要求。
func TestRuleServiceReviewCopyOnlyIsValid(t *testing.T) {
	// service 是绑定测试端口的规则应用服务。
	service := NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{})
	// err 是只含好评文案动作的规则的校验错误，应为空。
	if _, err := service.Normalize(context.Background(), 42, reviewRuleDraft(
		ActionDraft{ActionType: ActionSendReviewCopy, ConfigJSON: `{"source":"ai"}`})); err != nil {
		t.Fatalf("仅好评文案动作应通过: %v", err)
	}
}

// TestRuleServiceRejectsInvalidReviewCopy 覆盖好评文案与抖动的全部非法输入。
func TestRuleServiceRejectsInvalidReviewCopy(t *testing.T) {
	// manyPool 是 101 条模板的 JSON 数组片段。
	manyPool := `"` + strings.Repeat(`x","`, 100) + `x"`
	// longText 是超长单条模板。
	longText := strings.Repeat("好", maxReviewCopyLength+1)
	// copyAction 构造带配置的好评文案动作。
	copyAction := func(config string) ActionDraft {
		return ActionDraft{ActionType: ActionSendReviewCopy, ConfigJSON: config}
	}
	// cases 保存每个非法输入场景的名称、触发类型、动作和期望的错误片段。
	cases := []struct {
		name    string
		trigger string
		actions []ActionDraft
		want    string
	}{
		{"非求评价规则", TriggerBuyerReviewed, []ActionDraft{copyAction(`{"source":"ai"}`)}, "仅支持"},
		{"重复文案动作", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"ai"}`), copyAction(`{"source":"ai"}`)}, "最多只能有一个"},
		{"配置不是对象", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`[]`)}, "配置"},
		{"池来源为空", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"pool","pool":[" "]}`)}, "至少需要一条"},
		{"混合缺池", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"mixed","ai_ratio":50}`)}, "兜底"},
		{"混合比例越界", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"mixed","pool":["a"],"ai_ratio":101}`)}, "0 到 100"},
		{"混合缺比例", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"mixed","pool":["a"]}`)}, "0 到 100"},
		{"未知来源", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"x"}`)}, "pool、ai 或 mixed"},
		{"模板过多", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"pool","pool":[` + manyPool + `]}`)}, "最多 100 条"},
		{"模板过长", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"pool","pool":["` + longText + `"]}`)}, "500"},
		{"最少字数越界", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"ai","ai_min_chars":2}`)}, "最少字数"},
		{"最多字数越界", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"ai","ai_max_chars":300}`)}, "最多字数"},
		{"字数区间颠倒", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"ai","ai_min_chars":60,"ai_max_chars":20}`)}, "不能大于"},
		{"风格非法", TriggerReviewMissingTimeout, []ActionDraft{copyAction(`{"source":"ai","ai_style":"x"}`)}, "风格"},
		{"抖动为负", TriggerReviewMissingTimeout, []ActionDraft{{ActionType: ActionSendText, MessageTemplate: "a", ConfigJSON: `{"delay_jitter_seconds":-1}`}}, "0 到 600"},
		{"抖动过大", TriggerReviewMissingTimeout, []ActionDraft{{ActionType: ActionSendText, MessageTemplate: "a", ConfigJSON: `{"delay_jitter_seconds":601}`}}, "0 到 600"},
		{"延迟加抖动超限", TriggerReviewMissingTimeout, []ActionDraft{{ActionType: ActionSendText, MessageTemplate: "a", DelaySeconds: 3500, ConfigJSON: `{"delay_jitter_seconds":200}`}}, "3600"},
		{"非文本动作带抖动", TriggerOrderPaid, []ActionDraft{{ActionType: ActionSendCard, CardID: 1, ConfigJSON: `{"delay_jitter_seconds":3}`}}, "文本类"},
	}
	for /* tc 是一个非法输入场景。 */ _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			// service 是允许使用卡密组的规则应用服务，供非文本动作场景使用。
			service := NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{cardEnabled: true})
			// err 是非法输入应触发的校验错误。
			_, err := service.Normalize(context.Background(), 42, RuleDraft{CookieID: "account-1", TriggerType: tc.trigger, Actions: tc.actions})
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("期望错误包含 %q，得到 %v", tc.want, err)
			}
		})
	}
}

// TestRuleServiceReviewRuleStillNeedsText 验证求评价规则只有停用的文本动作时仍被拒绝。
func TestRuleServiceReviewRuleStillNeedsText(t *testing.T) {
	// service 是绑定测试端口的规则应用服务。
	service := NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{})
	// disabled 表示停用的好评文案动作。
	disabled := false
	// err 是只有停用好评文案动作时的校验错误，应被拒绝。
	_, err := service.Normalize(context.Background(), 42, reviewRuleDraft(
		ActionDraft{ActionType: ActionSendReviewCopy, Enabled: &disabled, ConfigJSON: `{"source":"ai"}`}))
	if err == nil || !strings.Contains(err.Error(), "至少需要") {
		t.Fatalf("没有启用的文本或文案动作应被拒绝: %v", err)
	}
}
