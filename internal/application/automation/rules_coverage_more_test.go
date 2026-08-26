package automation

import (
	"context"
	"errors"
	"testing"
)

// ruleTemplateOwnershipFake 为模板动作补充模板读取能力。
type ruleTemplateOwnershipFake struct {
	// ruleOwnershipFake 保存账号、商品和卡密组归属行为。
	*ruleOwnershipFake
	// template 保存模板摘要。
	template TemplateInfo
	// templateErr 保存模板读取错误。
	templateErr error
}

// GetDeliveryTemplate 返回测试模板摘要或预设错误。
func (f *ruleTemplateOwnershipFake) GetDeliveryTemplate(context.Context, int64, int64) (TemplateInfo, error) {
	if f.templateErr != nil {
		return TemplateInfo{}, f.templateErr
	}
	return f.template, nil
}

// TestRuleServiceNormalizesAllActionKinds 验证各类动作的默认值、启用状态与持久化配置。
func TestRuleServiceNormalizesAllActionKinds(t *testing.T) {
	// disabled 表示显式停用的动作状态。
	disabled := false
	// service 是使用卡密归属替身的规则服务。
	service := NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{cardType: "data", cardEnabled: true})
	// actions 保存需要一次性规范化的多种合法动作。
	actions, flags, err := service.normalizeDraftActions(context.Background(), 7, TriggerBuyerReviewed, []ActionDraft{
		{ActionType: ActionConfirmShipment, Enabled: &disabled},
		{ActionType: ActionSendCard, CardID: 1, SortOrder: 3},
		{ActionType: ActionSendText, MessageTemplate: " hello ", ConfigJSON: `{}`},
		{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"1.20"}`},
	})
	if err != nil {
		t.Fatalf("合法动作规范化失败: %v", err)
	}
	if len(actions) != 4 || flags.hasConfirmShipment || !flags.hasSendCard || !flags.hasSendText || !flags.hasAdjustPrice {
		t.Fatalf("动作结果错误: actions=%+v flags=%+v", actions, flags)
	}
	if actions[0].Enabled || actions[0].DeliveryCount != 1 || actions[1].SortOrder != 3 || actions[2].SortOrder != 3 {
		t.Fatalf("动作默认值错误: %+v", actions)
	}
}

// TestRuleServiceRejectsActionValidationBranches 验证动作类型、模板、延时和配置错误分支。
func TestRuleServiceRejectsActionValidationBranches(t *testing.T) {
	// templateError 是模板读取失败时应透传的底层错误。
	templateError := errors.New("template lookup failed")
	// cases 保存每个动作校验分支及预期错误片段。
	cases := []struct {
		// name 是当前动作校验场景名称。
		name string
		// service 是当前场景使用的规则服务。
		service *RuleService
		// trigger 是动作所属的规则触发类型。
		trigger string
		// action 是待校验动作。
		action ActionDraft
		// want 是预期错误提示片段。
		want string
	}{
		{name: "card id", service: NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{}), trigger: TriggerOrderPaid, action: ActionDraft{ActionType: ActionSendCard}, want: "必须选择卡密组"},
		{name: "card not found", service: NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{cardErr: ErrRuleNotFound}), trigger: TriggerOrderPaid, action: ActionDraft{ActionType: ActionSendCard, CardID: 1}, want: "卡密组不存在"},
		{name: "api not ready", service: NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{cardType: "api", cardEnabled: true}), trigger: TriggerOrderPaid, action: ActionDraft{ActionType: ActionSendCard, CardID: 1}, want: "API 卡券配置无效"},
		{name: "template trigger", service: NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{}), trigger: TriggerOrderCreated, action: ActionDraft{ActionType: ActionSendTemplate, DeliveryTemplateID: 1}, want: "仅支持付款发货或评价赠品"},
		{name: "template id", service: NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{}), trigger: TriggerOrderPaid, action: ActionDraft{ActionType: ActionSendTemplate}, want: "必须选择发货模板"},
		{name: "template capability", service: NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{}), trigger: TriggerOrderPaid, action: ActionDraft{ActionType: ActionSendTemplate, DeliveryTemplateID: 1}, want: "能力未装配"},
		{name: "template lookup", service: NewRuleService(&ruleRepositoryFake{}, &ruleTemplateOwnershipFake{ruleOwnershipFake: &ruleOwnershipFake{}, templateErr: templateError}), trigger: TriggerOrderPaid, action: ActionDraft{ActionType: ActionSendTemplate, DeliveryTemplateID: 1}, want: "template lookup failed"},
		{name: "template disabled", service: NewRuleService(&ruleRepositoryFake{}, &ruleTemplateOwnershipFake{ruleOwnershipFake: &ruleOwnershipFake{}, template: TemplateInfo{}}), trigger: TriggerOrderPaid, action: ActionDraft{ActionType: ActionSendTemplate, DeliveryTemplateID: 1}, want: "不存在或已停用"},
		{name: "template binding", service: NewRuleService(&ruleRepositoryFake{}, &ruleTemplateOwnershipFake{ruleOwnershipFake: &ruleOwnershipFake{}, template: TemplateInfo{Enabled: true, Keys: []string{"key"}}}), trigger: TriggerOrderPaid, action: ActionDraft{ActionType: ActionSendTemplate, DeliveryTemplateID: 1}, want: "绑定不完整"},
		{name: "template custom", service: NewRuleService(&ruleRepositoryFake{}, &ruleTemplateOwnershipFake{ruleOwnershipFake: &ruleOwnershipFake{}, template: TemplateInfo{Enabled: true, CustomKeys: []string{"name"}}}), trigger: TriggerOrderPaid, action: ActionDraft{ActionType: ActionSendTemplate, DeliveryTemplateID: 1, CustomVariables: map[string]string{}}, want: "自定义变量"},
		{name: "text message", service: NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{}), trigger: TriggerBuyerReviewed, action: ActionDraft{ActionType: ActionSendText}, want: "必须填写文案"},
		{name: "delay", service: NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{}), trigger: TriggerBuyerReviewed, action: ActionDraft{ActionType: ActionSendText, MessageTemplate: "x", DelaySeconds: 3601}, want: "动作延时"},
		{name: "config", service: NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{}), trigger: TriggerBuyerReviewed, action: ActionDraft{ActionType: ActionSendText, MessageTemplate: "x", ConfigJSON: "[]"}, want: "动作配置"},
	}
	// testCase 表示当前动作校验场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// _, err 保存当前动作校验结果。
			_, _, err := testCase.service.normalizeDraftActions(context.Background(), 7, testCase.trigger, []ActionDraft{testCase.action})
			if err == nil || !containsRuleError(err, testCase.want) {
				t.Fatalf("错误=%v，期望包含=%q", err, testCase.want)
			}
		})
	}
}

// TestRuleServiceNormalizesTemplateAction 验证模板动作的变量绑定、自定义变量和配置合并。
func TestRuleServiceNormalizesTemplateAction(t *testing.T) {
	// service 是提供启用模板和文本卡密组的规则服务。
	service := NewRuleService(&ruleRepositoryFake{}, &ruleTemplateOwnershipFake{
		ruleOwnershipFake: &ruleOwnershipFake{cardType: "text", cardEnabled: true},
		template:          TemplateInfo{Enabled: true, Keys: []string{"key"}, CustomKeys: []string{"name"}},
	})
	// actions 是包含模板绑定和自定义变量的合法动作列表。
	actions, flags, err := service.normalizeDraftActions(context.Background(), 7, TriggerOrderPaid, []ActionDraft{{
		ActionType:         ActionSendTemplate,
		DeliveryTemplateID: 8,
		TemplateBindings:   []TemplateBinding{{VariableKey: "key", CardID: 2}},
		CustomVariables:    map[string]string{"name": " Alice "},
		ConfigJSON:         `{"existing":true}`,
	}})
	if err != nil {
		t.Fatalf("模板动作规范化失败: %v", err)
	}
	if len(actions) != 1 || !flags.hasSendTemplate || actions[0].DeliveryTemplateID != 8 || actions[0].CustomVariables["name"] != " Alice " {
		t.Fatalf("模板动作结果错误: actions=%+v flags=%+v", actions, flags)
	}
	if !containsRuleError(errors.New(actions[0].ConfigJSON), "custom_variables") {
		t.Fatalf("模板自定义变量未写入配置: %s", actions[0].ConfigJSON)
	}
}

// TestRuleServiceRejectsInvalidNormalizeInputs 验证规则级输入、归属和空动作边界。
func TestRuleServiceRejectsInvalidNormalizeInputs(t *testing.T) {
	// notOwned 是拒绝账号和商品归属的规则归属替身。
	notOwned := &ruleOwnershipFake{}
	// invalidCases 保存规则级校验场景。
	cases := []struct {
		// name 是当前规则级场景名称。
		name string
		// draft 是待规范化规则草稿。
		draft RuleDraft
		// want 是预期错误提示片段。
		want string
	}{
		{name: "trigger", draft: RuleDraft{CookieID: "a", TriggerType: "unknown", Actions: []ActionDraft{{ActionType: ActionSendText, MessageTemplate: "x"}}}, want: "不支持的触发类型"},
		{name: "empty actions", draft: RuleDraft{CookieID: "a", TriggerType: TriggerBuyerReviewed}, want: "至少需要一个自动化动作"},
		{name: "config", draft: RuleDraft{CookieID: "a", TriggerType: TriggerBuyerReviewed, ConfigJSON: "[]", Actions: []ActionDraft{{ActionType: ActionSendText, MessageTemplate: "x"}}}, want: "规则配置"},
	}
	// testCase 表示当前规则级输入场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// service 是当前规则级场景使用的规则服务。
			service := NewRuleService(&ruleRepositoryFake{}, notOwned)
			// _, err 保存规则规范化错误。
			_, err := service.Normalize(context.Background(), 7, testCase.draft)
			if err == nil || !containsRuleError(err, testCase.want) {
				t.Fatalf("错误=%v，期望包含=%q", err, testCase.want)
			}
		})
	}
}

// TestRuleServiceRejectsUninitializedListAndNormalize 验证规则服务未装配仓储、归属端口或接收非法用户时快速失败。
func TestRuleServiceRejectsUninitializedListAndNormalize(t *testing.T) {
	// ownership 是规则归属校验使用的最小替身。
	ownership := &ruleOwnershipFake{}
	// validService 是具备全部规则依赖的服务。
	validService := NewRuleService(&ruleRepositoryFake{}, ownership)
	// invalidPageErr 保存零用户分页查询的输入错误。
	if _, _, invalidPageErr := validService.ListPageForUser(context.Background(), RuleFilter{UserID: 0}); !errors.Is(invalidPageErr, ErrInvalidInput) {
		t.Fatalf("零用户分页错误=%v", invalidPageErr)
	}
	// noRepositoryService 是缺少规则仓储的服务。
	noRepositoryService := NewRuleService(nil, ownership)
	// noRepositoryPageErr、noRepositoryNormalizeErr 保存缺少仓储时的两个用例错误。
	if _, _, noRepositoryPageErr := noRepositoryService.ListPageForUser(context.Background(), RuleFilter{UserID: 7}); !errors.Is(noRepositoryPageErr, ErrInvalidInput) {
		t.Fatalf("缺少规则仓储分页错误=%v", noRepositoryPageErr)
	}
	// noRepositoryNormalizeErr 保存缺少规则仓储时规范化草稿的错误。
	if _, noRepositoryNormalizeErr := noRepositoryService.Normalize(context.Background(), 7, RuleDraft{}); !errors.Is(noRepositoryNormalizeErr, ErrInvalidInput) {
		t.Fatalf("缺少规则仓储规范化错误=%v", noRepositoryNormalizeErr)
	}
	// noOwnershipService 是缺少归属端口的规则服务。
	noOwnershipService := NewRuleService(&ruleRepositoryFake{}, nil)
	// noOwnershipNormalizeErr 保存缺少归属端口时的规范化错误。
	if _, noOwnershipNormalizeErr := noOwnershipService.Normalize(context.Background(), 7, RuleDraft{}); !errors.Is(noOwnershipNormalizeErr, ErrInvalidInput) {
		t.Fatalf("缺少归属端口规范化错误=%v", noOwnershipNormalizeErr)
	}
	// nilService 表示未初始化的规则服务指针。
	var nilService *RuleService
	// nilPageErr、nilNormalizeErr 保存空规则服务的分页和规范化错误。
	if _, _, nilPageErr := nilService.ListPageForUser(context.Background(), RuleFilter{UserID: 7}); !errors.Is(nilPageErr, ErrInvalidInput) {
		t.Fatalf("空规则服务分页错误=%v", nilPageErr)
	}
	// nilNormalizeErr 保存空规则服务规范化草稿的错误。
	if _, nilNormalizeErr := nilService.Normalize(context.Background(), 7, RuleDraft{}); !errors.Is(nilNormalizeErr, ErrInvalidInput) {
		t.Fatalf("空规则服务规范化错误=%v", nilNormalizeErr)
	}
}
