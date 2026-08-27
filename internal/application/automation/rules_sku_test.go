package automation

import (
	"context"
	"testing"
)

// ruleOwnershipWithItemProfileFake 为 SKU 规则测试提供权威的商品规格摘要。
type ruleOwnershipWithItemProfileFake struct {
	// ruleOwnershipFake 保存通用账号、商品和卡密归属测试行为。
	*ruleOwnershipFake
	// profile 保存测试商品的多规格标志。
	profile ItemDeliveryProfile
}

// GetItemDeliveryProfile 返回测试商品的非敏感规格摘要。
func (f *ruleOwnershipWithItemProfileFake) GetItemDeliveryProfile(context.Context, int64, string, string) (ItemDeliveryProfile, error) {
	return f.profile, nil
}

// TestNormalizeRuleSKUConfigsHonorsItemProfile 验证商品事实优先于动作文本推断，并支持单维多 SKU。
func TestNormalizeRuleSKUConfigsHonorsItemProfile(t *testing.T) {
	// singleOwnership 表示商品明确不是多规格时的规则归属能力。
	singleOwnership := &ruleOwnershipWithItemProfileFake{ruleOwnershipFake: &ruleOwnershipFake{}, profile: ItemDeliveryProfile{IsMultiSpec: false}}
	// singleActions 保存单规格商品中带有历史分隔符文本的动作。
	singleActions := []ActionInput{{ActionType: ActionSendCard, Enabled: true, ConfigJSON: `{"spec_name":"颜色；版本","spec_value":"红；专业"}`}}
	// err 保存单规格商品动作规范化错误。
	if err := normalizeRuleSKUConfigs(context.Background(), singleOwnership, 1, "account", "item", singleActions); err != nil {
		t.Fatalf("单规格商品规范化失败: %v", err)
	}
	if singleActions[0].ConfigJSON != `{"spec_name":"","spec_value":""}` {
		t.Fatalf("商品多规格事实不应被动作文本覆盖: %s", singleActions[0].ConfigJSON)
	}

	// multiOwnership 表示存在单维 SKU 的多规格商品。
	multiOwnership := &ruleOwnershipWithItemProfileFake{ruleOwnershipFake: &ruleOwnershipFake{}, profile: ItemDeliveryProfile{IsMultiSpec: true}}
	// multiActions 保存单维多 SKU 的完整规格动作。
	multiActions := []ActionInput{{ActionType: ActionSendCard, Enabled: true, ConfigJSON: `{"spec_name":"套餐","spec_value":"标准"}`}}
	// err 保存单维多 SKU 动作规范化错误。
	if err := normalizeRuleSKUConfigs(context.Background(), multiOwnership, 1, "account", "item", multiActions); err != nil {
		t.Fatalf("单维多 SKU 规范化失败: %v", err)
	}
	if multiActions[0].ConfigJSON != `{"spec_name":"套餐","spec_value":"标准"}` {
		t.Fatalf("单维多 SKU 规格被错误清除: %s", multiActions[0].ConfigJSON)
	}
}
