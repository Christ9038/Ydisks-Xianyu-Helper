package items

import (
	"context"
	"strings"

	"xianyu-go/internal/orderspec"
)

// BatchPreviewTemplatePort 是预检时核对发货模板可用性的可选端口；
// 未实现该端口的归属实现（如旧测试替身）只能使用发卡动作。
type BatchPreviewTemplatePort interface {
	// DeliveryTemplateUsable 判断发货模板是否属于用户且处于启用状态。
	DeliveryTemplateUsable(ctx context.Context, userID, templateID int64) (bool, error)
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

// validateTemplateAction 校验发货模板动作引用的模板和变量绑定卡密组。
func (service *BatchPreviewService) validateTemplateAction(ctx context.Context, userID int64, prefix string, action BatchPreviewCardAction) []string {
	// found 保存模板动作的校验提示。
	var found []string
	// port、ok 保存归属实现是否支持模板核对。
	port, ok := service.ownership.(BatchPreviewTemplatePort)
	if !ok || action.DeliveryTemplateID <= 0 {
		return append(found, prefix+"发货模板不可用")
	}
	// usable、err 保存模板归属与启用状态。
	usable, err := port.DeliveryTemplateUsable(ctx, userID, action.DeliveryTemplateID)
	if err != nil || !usable {
		found = append(found, prefix+"发货模板不存在或已停用")
	}
	// binding 是当前模板变量绑定。
	for _, binding := range action.TemplateBindings {
		// owned、cardErr 保存绑定卡密组的归属结果。
		owned, cardErr := service.ownership.CardOwned(ctx, userID, binding.CardID)
		if cardErr != nil || !owned {
			found = append(found, prefix+"模板变量绑定的卡密组不存在或不属于当前用户")
		}
	}
	return found
}
