package automation

import (
	"context"
	"errors"
	"testing"
)

// TestRulePureValidationHelpers 验证金额、触发动作组合、默认名称和动作顺序的纯业务边界。
func TestRulePureValidationHelpers(t *testing.T) {
	// priceCases 描述改价金额的合法与非法文本。
	priceCases := []struct {
		value string
		valid bool
	}{
		{value: `{"target_price":"0.01"}`, valid: true},
		{value: `{"target_price":"10.5"}`, valid: true},
		{value: `{"target_price":"1000000.00"}`, valid: true},
		{value: `{"target_price":""}`},
		{value: `{"target_price":"0"}`},
		{value: `{"target_price":"1.234"}`},
		{value: `{"target_price":"1000000.01"}`},
		{value: `{"target_price":"abc"}`},
		{value: "not-json"},
	}
	// priceCase 表示当前遍历的改价金额校验用例。
	for _, priceCase := range priceCases {
		// err 保存当前金额文本的校验结果。
		err := validateAdjustPriceConfig(priceCase.value)
		if (err == nil) != priceCase.valid {
			t.Fatalf("金额校验错误 value=%q err=%v valid=%v", priceCase.value, err, priceCase.valid)
		}
	}
	// triggerCases 描述各触发类型的必需动作和互斥动作。
	triggerCases := []struct {
		trigger string
		flags   ruleActionFlags
		valid   bool
	}{
		{trigger: TriggerOrderCreated, flags: ruleActionFlags{hasAdjustPrice: true}, valid: true},
		{trigger: TriggerOrderCreated, flags: ruleActionFlags{hasSendText: true}},
		{trigger: TriggerOrderCreated, flags: ruleActionFlags{hasConfirmShipment: true}},
		{trigger: TriggerOrderCreated, flags: ruleActionFlags{}},
		{trigger: TriggerOrderPaid, flags: ruleActionFlags{hasSendCard: true}, valid: true},
		{trigger: TriggerOrderPaid, flags: ruleActionFlags{hasAdjustPrice: true}},
		{trigger: TriggerOrderPaid, flags: ruleActionFlags{}},
		{trigger: TriggerBuyerReviewed, flags: ruleActionFlags{hasSendText: true}, valid: true},
		{trigger: TriggerBuyerReviewed, flags: ruleActionFlags{hasConfirmShipment: true}},
		{trigger: TriggerBuyerReviewed, flags: ruleActionFlags{hasAdjustPrice: true}},
		{trigger: TriggerBuyerReviewed, flags: ruleActionFlags{}},
		{trigger: TriggerReviewMissingTimeout, flags: ruleActionFlags{hasSendText: true}, valid: true},
		{trigger: TriggerReviewMissingTimeout, flags: ruleActionFlags{hasConfirmShipment: true}},
		{trigger: TriggerReviewMissingTimeout, flags: ruleActionFlags{hasSendCard: true}},
		{trigger: "unknown", flags: ruleActionFlags{}, valid: true},
	}
	// triggerCase 表示当前遍历的触发动作组合用例。
	for _, triggerCase := range triggerCases {
		// err 保存当前动作组合的校验结果。
		err := validateTriggerActionCombination(triggerCase.trigger, triggerCase.flags)
		if (err == nil) != triggerCase.valid {
			t.Fatalf("触发组合校验错误 trigger=%q flags=%+v err=%v valid=%v", triggerCase.trigger, triggerCase.flags, err, triggerCase.valid)
		}
	}
	// got 保存带商品标识的规则默认名称。
	if got := defaultRuleName(TriggerOrderCreated, " item-1 "); got != "拍下未付款自动改价 - item-1" {
		t.Fatalf("规则默认名称错误: %q", got)
	}
	// got 保存未知触发类型的规则默认名称。
	if got := defaultRuleName("unknown", ""); got != "自动化规则" {
		t.Fatalf("未知触发类型默认名称错误: %q", got)
	}
	// got 保存显式动作顺序及缺省动作顺序的校验结果。
	if got := firstRuleNonZero(7, 1); got != 7 || firstRuleNonZero(0, 2) != 2 {
		t.Fatalf("动作顺序默认值错误")
	}
}

// TestValidateTemplateBindingsBranches 验证模板变量绑定的完整性、归属类型和启用状态校验。
func TestValidateTemplateBindingsBranches(t *testing.T) {
	// ownership 保存允许文本卡密组绑定的归属端口替身。
	ownership := &ruleOwnershipFake{cardType: "text", cardEnabled: true}
	// validErr 保存完整模板变量绑定的校验结果。
	validErr := validateTemplateBindings(context.Background(), ownership, 7, []string{"first", "second"}, []TemplateBinding{{VariableKey: "first", CardID: 1}, {VariableKey: "second", CardID: 2}})
	if validErr != nil {
		t.Fatalf("完整模板变量绑定不应失败: %v", validErr)
	}
	// cases 描述模板绑定校验的拒绝边界。
	cases := []struct {
		name     string
		keys     []string
		bindings []TemplateBinding
		fake     *ruleOwnershipFake
	}{
		{name: "missing", keys: []string{"first"}, bindings: nil, fake: ownership},
		{name: "duplicate", keys: []string{"first", "second"}, bindings: []TemplateBinding{{VariableKey: "first", CardID: 1}, {VariableKey: "first", CardID: 2}}, fake: ownership},
		{name: "wrong type", keys: []string{"first"}, bindings: []TemplateBinding{{VariableKey: "first", CardID: 1}}, fake: &ruleOwnershipFake{cardType: "api", cardEnabled: true}},
		{name: "disabled", keys: []string{"first"}, bindings: []TemplateBinding{{VariableKey: "first", CardID: 1}}, fake: &ruleOwnershipFake{cardType: "text"}},
		{name: "repository error", keys: []string{"first"}, bindings: []TemplateBinding{{VariableKey: "first", CardID: 1}}, fake: &ruleOwnershipFake{cardType: "text", cardEnabled: true, cardErr: errors.New("card lookup failed")}},
	}
	// testCase 表示当前遍历的模板绑定拒绝用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// err 保存当前模板绑定用例的校验错误。
			err := validateTemplateBindings(context.Background(), testCase.fake, 7, testCase.keys, testCase.bindings)
			if err == nil {
				t.Fatal("非法模板变量绑定不应通过")
			}
		})
	}
}
