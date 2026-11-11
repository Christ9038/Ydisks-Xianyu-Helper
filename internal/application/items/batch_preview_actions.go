package items

import (
	"context"
	"strings"

	"xianyu-go/internal/orderspec"
)

// BatchTemplateRequirements 是发货模板在预检中需要核对的非敏感摘要，不包含模板正文。
type BatchTemplateRequirements struct {
	// Usable 表示模板属于当前用户且处于启用状态。
	Usable bool
	// CardKeys 是模板 {{cards.<key>}} 引用、必须绑定卡密组的变量键。
	CardKeys []string
	// CustomKeys 是模板 {{custom.<key>}} 引用、必须由规则提供非空取值的变量键。
	CustomKeys []string
}

// BatchPreviewTemplatePort 是预检时核对发货模板可用性的可选端口；
// 未实现该端口的归属实现（如旧测试替身）只能使用发卡动作。
type BatchPreviewTemplatePort interface {
	// DeliveryTemplateRequirements 返回模板归属、启用状态和变量键；模板不存在或属于他人时 Usable 为 false 且无错误。
	DeliveryTemplateRequirements(ctx context.Context, userID, templateID int64) (BatchTemplateRequirements, error)
	// TemplateCardBindable 判断卡密组属于用户、已启用且类型可被发货模板绑定（文本、批量数据或配置有效的 API）。
	TemplateCardBindable(ctx context.Context, userID, cardID int64) (bool, error)
}

// validateDeliveryAction 校验单个发货动作：发卡动作核对卡密组归属，发货模板动作核对模板可用性与绑定卡密组归属，
// 并统一校验份数、延迟和规格绑定格式。
func (service *BatchPreviewService) validateDeliveryAction(ctx context.Context, userID int64, prefix string, action BatchPreviewCardAction) []string {
	// found 保存当前动作的校验提示。
	var found []string
	switch action.ActionType {
	case "", BatchActionSendCard:
		// owned、err 保存卡券归属查询结果。
		owned, err := service.ownership.CardOwned(ctx, userID, action.CardID)
		if err != nil || !owned {
			found = append(found, prefix+"卡密组不存在或不属于当前用户")
		}
	case BatchActionSendTemplate:
		found = append(found, service.validateTemplateAction(ctx, userID, prefix, action)...)
	default:
		found = append(found, prefix+"动作类型不受支持")
	}
	if action.ActionType != BatchActionSendTemplate && action.DeliveryCount <= 0 {
		found = append(found, prefix+"每件份数必须大于0")
	}
	if action.DelaySeconds < 0 || action.DelaySeconds > 3600 {
		found = append(found, prefix+"延迟秒必须在 0 到 3600 之间")
	}
	if strings.TrimSpace(action.SpecName) != "" || strings.TrimSpace(action.SpecValue) != "" {
		// normalized、err 保存规格绑定的规范化结果；预检要求写入的值已经是规范形式。
		normalized, err := orderspec.NormalizeColumns(action.SpecName, action.SpecValue)
		if err != nil || normalized.Dimensions == 0 || normalized.Name != action.SpecName || normalized.Value != action.SpecValue {
			found = append(found, prefix+"规格绑定格式错误")
		}
	}
	return found
}

// validateTemplateAction 校验发货模板动作：模板可用、卡密变量绑定与模板一一对应且可绑定、自定义变量都已填写。
// 这些条件与发布后建规则的校验一致，提前在预检拦截，避免商品已上架而发货规则写入失败。
// 错误提示只包含变量键，不回显变量取值（可能是网盘链接等业务内容）。
func (service *BatchPreviewService) validateTemplateAction(ctx context.Context, userID int64, prefix string, action BatchPreviewCardAction) []string {
	// found 保存模板动作的校验提示。
	var found []string
	// port、ok 保存归属实现是否支持模板核对。
	port, ok := service.ownership.(BatchPreviewTemplatePort)
	if !ok || action.DeliveryTemplateID <= 0 {
		return append(found, prefix+"发货模板不可用")
	}
	// requirements、err 保存模板归属、启用状态与变量键。
	requirements, err := port.DeliveryTemplateRequirements(ctx, userID, action.DeliveryTemplateID)
	// usable 表示模板可用，只有可用时才继续核对变量键是否齐全。
	usable := err == nil && requirements.Usable
	if !usable {
		found = append(found, prefix+"发货模板不存在或已停用")
	}
	// binding 是当前模板变量绑定。
	for _, binding := range action.TemplateBindings {
		// owned、cardErr 保存绑定卡密组的归属结果。
		owned, cardErr := service.ownership.CardOwned(ctx, userID, binding.CardID)
		if cardErr != nil || !owned {
			found = append(found, prefix+"模板变量绑定的卡密组不存在或不属于当前用户")
			continue
		}
		// bindable、bindErr 保存卡密组能否被模板绑定。
		if bindable, bindErr := port.TemplateCardBindable(ctx, userID, binding.CardID); bindErr != nil || !bindable {
			found = append(found, prefix+"模板卡密 "+binding.VariableKey+" 绑定的卡密组已停用或类型不支持")
		}
	}
	if !usable {
		return found
	}
	found = append(found, templateBindingCoverageErrors(prefix, requirements.CardKeys, action.TemplateBindings)...)
	// key 是模板要求填写的自定义变量键。
	for _, key := range requirements.CustomKeys {
		if strings.TrimSpace(action.CustomVariables[key]) == "" {
			found = append(found, prefix+"缺少模板变量 "+key)
		}
	}
	return found
}

// templateBindingCoverageErrors 校验绑定恰好覆盖模板卡密变量键：缺少、重复或多余的绑定都会在发布后建规则时失败。
func templateBindingCoverageErrors(prefix string, keys []string, bindings []BatchPreviewTemplateBinding) []string {
	// found 保存绑定覆盖度的校验提示。
	var found []string
	// counts 记录每个绑定键出现的次数。
	counts := make(map[string]int, len(bindings))
	// binding 是当前统计的绑定。
	for _, binding := range bindings {
		counts[binding.VariableKey]++
	}
	// expected 记录模板要求的卡密变量键。
	expected := make(map[string]bool, len(keys))
	// key 是模板要求绑定的卡密变量键。
	for _, key := range keys {
		expected[key] = true
		if counts[key] == 0 {
			found = append(found, prefix+"缺少模板卡密 "+key)
		} else if counts[key] > 1 {
			found = append(found, prefix+"模板卡密 "+key+" 重复绑定")
		}
	}
	// binding 是当前检查是否多余的绑定。
	for _, binding := range bindings {
		if !expected[binding.VariableKey] {
			found = append(found, prefix+"模板未使用卡密变量 "+binding.VariableKey)
		}
	}
	return found
}
