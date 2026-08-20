package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// ruleRepositoryFake 保存规则应用服务测试所需的最小仓储替身。
type ruleRepositoryFake struct {
	// created 保存最近一次创建输入。
	created RuleInput
	// listedFilter 保存最近一次分页查询条件。
	listedFilter RuleFilter
}

// ListForUser 返回空规则列表。
func (r *ruleRepositoryFake) ListForUser(context.Context, int64) ([]Rule, error) { return nil, nil }

// ListPageForUser 记录分页条件并返回空结果。
func (r *ruleRepositoryFake) ListPageForUser(_ context.Context, filter RuleFilter) ([]Rule, int, error) {
	r.listedFilter = filter
	return nil, 0, nil
}

// CountByTriggerForUser 返回空触发统计。
func (r *ruleRepositoryFake) CountByTriggerForUser(context.Context, RuleFilter) (map[string]int, error) {
	return map[string]int{}, nil
}

// Create 记录规则输入并返回固定标识。
func (r *ruleRepositoryFake) Create(_ context.Context, input RuleInput) (int64, error) {
	r.created = input
	return 7, nil
}

// Update 接受测试更新调用。
func (r *ruleRepositoryFake) Update(context.Context, int64, int64, RuleInput) error { return nil }

// Delete 接受测试删除调用。
func (r *ruleRepositoryFake) Delete(context.Context, int64, int64) error { return nil }

// ruleOwnershipFake 保存规则归属和卡密组测试结果。
type ruleOwnershipFake struct {
	// cardType 是卡密组类型。
	cardType string
	// accountErr 是账号归属查询需要返回的基础设施错误。
	accountErr error
	// itemErr 是商品归属查询需要返回的基础设施错误。
	itemErr error
	// cardErr 是卡密组查询需要返回的基础设施错误。
	cardErr error
	// aiEnabled 表示测试账号是否启用了 AI 议价。
	aiEnabled bool
}

// OwnsAccount 返回账号归属通过。
func (r *ruleOwnershipFake) OwnsAccount(context.Context, int64, string) (bool, error) {
	if r.accountErr != nil {
		return false, r.accountErr
	}
	return true, nil
}

// OwnsItem 返回商品归属通过。
func (r *ruleOwnershipFake) OwnsItem(context.Context, int64, string, string) (bool, error) {
	if r.itemErr != nil {
		return false, r.itemErr
	}
	return true, nil
}

// GetCard 返回预设卡密组类型。
func (r *ruleOwnershipFake) GetCard(context.Context, int64, int64) (CardInfo, error) {
	if r.cardErr != nil {
		return CardInfo{}, r.cardErr
	}
	if r.cardType == "" {
		return CardInfo{Type: "data"}, nil
	}
	return CardInfo{Type: r.cardType}, nil
}

// AIReplyEnabled 返回测试账号的 AI 议价开关。
func (r *ruleOwnershipFake) AIReplyEnabled(context.Context, string) (bool, error) {
	return r.aiEnabled, nil
}

// TestRuleServiceRejectsAdjustPriceWhenAIEnabled 验证启用 AI 议价的账号不能再启用固定规则改价。
func TestRuleServiceRejectsAdjustPriceWhenAIEnabled(t *testing.T) {
	// service 是注入 AI 议价开启状态的规则应用服务。
	service := NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{aiEnabled: true})
	// draft 是准备启用的拍下固定价格规则。
	draft := RuleDraft{CookieID: "account-1", TriggerType: TriggerOrderCreated, Enabled: true, Actions: []ActionDraft{{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.90"}`}}}
	// err 是启用冲突规则时返回的互斥错误。
	if _, err := service.Normalize(context.Background(), 7, draft); !errors.Is(err, ErrPricingModeConflict) {
		t.Fatalf("AI 议价与固定改价规则冲突应被拒绝: %v", err)
	}
	draft.Enabled = false
	// err 是保留停用规则时不应出现的校验错误。
	if _, err := service.Normalize(context.Background(), 7, draft); err != nil {
		t.Fatalf("停用的固定规则应允许保留: %v", err)
	}
}

// TestRuleServiceNormalizePropagatesOwnershipErrors 验证归属端口的基础设施错误不会伪装成用户输入错误。
func TestRuleServiceNormalizePropagatesOwnershipErrors(t *testing.T) {
	// backendErr 是归属查询模拟的底层数据库故障。
	backendErr := errors.New("database unavailable")
	// cases 保存不同归属阶段及其预期错误。
	cases := []struct {
		// name 是当前归属阶段测试名称。
		name string
		// ownership 是注入指定底层故障的归属替身。
		ownership *ruleOwnershipFake
		// draft 是触发当前归属查询的最小规则草稿。
		draft RuleDraft
	}{
		{name: "account", ownership: &ruleOwnershipFake{accountErr: backendErr}, draft: RuleDraft{CookieID: "account-1", TriggerType: TriggerOrderPaid}},
		{name: "item", ownership: &ruleOwnershipFake{itemErr: backendErr}, draft: RuleDraft{CookieID: "account-1", ItemID: "item-1", TriggerType: TriggerOrderPaid}},
		{name: "card", ownership: &ruleOwnershipFake{cardErr: backendErr}, draft: RuleDraft{CookieID: "account-1", TriggerType: TriggerOrderPaid, Actions: []ActionDraft{{ActionType: ActionSendCard, CardID: 1}}}},
	}
	// testCase 是当前归属阶段及预期底层错误的测试样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// service 是当前底层故障场景使用的规则应用服务。
			service := NewRuleService(&ruleRepositoryFake{}, testCase.ownership)
			// _, err 保存规范化过程中透传的底层错误。
			_, err := service.Normalize(context.Background(), 42, testCase.draft)
			if !errors.Is(err, backendErr) {
				t.Fatalf("应透传底层错误，err=%v", err)
			}
		})
	}
}

// TestRuleServiceNormalizeAppliesDefaults 验证规则输入规范化和默认值。
func TestRuleServiceNormalizeAppliesDefaults(t *testing.T) {
	// repository 保存规范化后的规则输入。
	repository := &ruleRepositoryFake{}
	// service 是绑定测试端口的规则应用服务。
	service := NewRuleService(repository, &ruleOwnershipFake{})
	// enabled 保存动作的启用状态指针。
	enabled := true
	// input 保存规范化后的规则输入。
	input, err := service.Normalize(context.Background(), 42, RuleDraft{
		CookieID: " account-1 ", TriggerType: TriggerOrderPaid, Actions: []ActionDraft{{
			ActionType: ActionSendCard, CardID: 9, Enabled: &enabled,
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if input.CookieID != "account-1" || input.Priority != 100 || input.Name == "" || input.Actions[0].DeliveryCount != 1 {
		t.Fatalf("规则默认值错误: %+v", input)
	}
}

// TestRuleServiceRejectsInvalidAction 验证不支持的动作和 API 卡密被拒绝。
func TestRuleServiceRejectsInvalidAction(t *testing.T) {
	// cases 保存非法规则输入和预期错误。
	cases := []struct {
		// name 是测试分支名称。
		name string
		// ownership 是当前分支的卡密组替身。
		ownership *ruleOwnershipFake
		// draft 是待校验规则。
		draft RuleDraft
		// want 是预期错误片段。
		want string
	}{
		{name: "unknown action", ownership: &ruleOwnershipFake{}, draft: RuleDraft{CookieID: "a", TriggerType: TriggerBuyerReviewed, Actions: []ActionDraft{{ActionType: "unknown"}}}, want: "不支持的动作"},
		{name: "api card", ownership: &ruleOwnershipFake{cardType: "api"}, draft: RuleDraft{CookieID: "a", TriggerType: TriggerOrderPaid, Actions: []ActionDraft{{ActionType: ActionSendCard, CardID: 1}}}, want: "API 卡密"},
	}
	// testCase 是当前非法规则分支及其预期错误的测试样例。
	for _, testCase /* testCase 是当前非法规则分支及其预期错误的测试样例。 */ := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// service 是当前非法规则分支使用的应用服务。
			service := NewRuleService(&ruleRepositoryFake{}, testCase.ownership)
			// _, err 保存规则校验错误。
			_, err := service.Normalize(context.Background(), 42, testCase.draft)
			if err == nil || !containsRuleError(err, testCase.want) {
				t.Fatalf("错误=%v，期望包含=%q", err, testCase.want)
			}
		})
	}
}

// TestRuleServiceNormalizeOrderCreatedAdjustPrice 验证拍下未付款改价规则的合法输入和默认名称。
func TestRuleServiceNormalizeOrderCreatedAdjustPrice(t *testing.T) {
	// service 是绑定测试端口的规则应用服务。
	service := NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{})
	// input、err 分别是规范化结果和校验错误。
	input, err := service.Normalize(context.Background(), 42, RuleDraft{
		CookieID: "account-1", TriggerType: TriggerOrderCreated, Actions: []ActionDraft{
			{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"9.9"}`},
			{ActionType: ActionSendText, MessageTemplate: "已为您改价，请尽快支付"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if input.TriggerType != TriggerOrderCreated || input.Name != "拍下未付款自动改价" || len(input.Actions) != 2 {
		t.Fatalf("拍下改价规则规范化错误: %+v", input)
	}
}

// TestRuleServiceRejectsInvalidOrderCreatedRules 验证拍下未付款规则的动作组合和目标价格校验。
func TestRuleServiceRejectsInvalidOrderCreatedRules(t *testing.T) {
	// cases 保存非法拍下改价规则输入和预期错误。
	cases := []struct {
		// name 是测试分支名称。
		name string
		// draft 是待校验规则。
		draft RuleDraft
		// want 是预期错误片段。
		want string
	}{
		{name: "missing adjust price", draft: RuleDraft{CookieID: "a", TriggerType: TriggerOrderCreated, Actions: []ActionDraft{{ActionType: ActionSendText, MessageTemplate: "x"}}}, want: "至少需要一个已启用的改价动作"},
		{name: "send card forbidden", draft: RuleDraft{CookieID: "a", TriggerType: TriggerOrderCreated, Actions: []ActionDraft{{ActionType: ActionSendCard, CardID: 1}, {ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"1"}`}}}, want: "只能包含改价和文本动作"},
		{name: "confirm shipment forbidden", draft: RuleDraft{CookieID: "a", TriggerType: TriggerOrderCreated, Actions: []ActionDraft{{ActionType: ActionConfirmShipment}, {ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"1"}`}}}, want: "只能包含改价和文本动作"},
		{name: "missing price", draft: RuleDraft{CookieID: "a", TriggerType: TriggerOrderCreated, Actions: []ActionDraft{{ActionType: ActionAdjustPrice, ConfigJSON: `{}`}}}, want: "必须填写目标价格"},
		{name: "bad price format", draft: RuleDraft{CookieID: "a", TriggerType: TriggerOrderCreated, Actions: []ActionDraft{{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"1.234"}`}}}, want: "最多两位小数"},
		{name: "zero price", draft: RuleDraft{CookieID: "a", TriggerType: TriggerOrderCreated, Actions: []ActionDraft{{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"0"}`}}}, want: "0.01 到 1000000"},
		{name: "too high price", draft: RuleDraft{CookieID: "a", TriggerType: TriggerOrderCreated, Actions: []ActionDraft{{ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"1000000.01"}`}}}, want: "0.01 到 1000000"},
		{name: "adjust price on paid trigger", draft: RuleDraft{CookieID: "a", TriggerType: TriggerOrderPaid, Actions: []ActionDraft{{ActionType: ActionSendCard, CardID: 1}, {ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"1"}`}}}, want: "只能用于拍下未付款规则"},
		{name: "adjust price on review trigger", draft: RuleDraft{CookieID: "a", TriggerType: TriggerReviewMissingTimeout, Actions: []ActionDraft{{ActionType: ActionSendText, MessageTemplate: "x"}, {ActionType: ActionAdjustPrice, ConfigJSON: `{"target_price":"1"}`}}}, want: "求评价规则只能发送文本"},
	}
	// testCase 是当前非法拍下改价规则分支及其预期错误的测试样例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// service 是当前分支使用的规则应用服务。
			service := NewRuleService(&ruleRepositoryFake{}, &ruleOwnershipFake{})
			// _, err 保存规则校验错误。
			_, err := service.Normalize(context.Background(), 42, testCase.draft)
			if err == nil || !containsRuleError(err, testCase.want) {
				t.Fatalf("错误=%v，期望包含=%q", err, testCase.want)
			}
		})
	}
}

// TestRuleServiceNormalizesPageLimit 验证分页大小和偏移量被应用层归一化。
func TestRuleServiceNormalizesPageLimit(t *testing.T) {
	// repository 保存分页调用条件。
	repository := &ruleRepositoryFake{}
	// service 是待验证的规则应用服务。
	service /* service 是待验证的规则应用服务。 */ := NewRuleService(repository, &ruleOwnershipFake{})
	// err 表示分页查询归一化后的仓储调用结果。
	if _, _, err := service.ListPageForUser(context.Background(), RuleFilter{UserID: 1, Limit: 0, Offset: -2}); err != nil {
		t.Fatal(err)
	}
	if repository.listedFilter.Limit != 10 || repository.listedFilter.Offset != 0 {
		t.Fatalf("分页归一化错误: %+v", repository.listedFilter)
	}
}

// containsRuleError 判断规则校验错误是否包含指定业务提示。
func containsRuleError(err error, want string) bool {
	return err != nil && want != "" && strings.Contains(err.Error(), want)
}
