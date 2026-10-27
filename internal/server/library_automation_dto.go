package server

import itemapp "xianyu-go/internal/application/items"

// toApplication 把自动化 DTO 转换为批量预检使用的应用层配置。
func (automation libraryTemplateAutomation) toApplication() itemapp.BatchPreviewAutomation {
	return itemapp.BatchPreviewAutomation{
		PaidDelivery:  automation.PaidDelivery.toApplication(),
		ReviewGift:    automation.ReviewGift.toApplication(),
		ReviewRequest: itemapp.BatchPreviewReviewRequest(automation.ReviewRequest),
	}
}

// toApplication 把一组发货动作 DTO 转换为应用层配置。
func (group libraryTemplateCardAutomation) toApplication() itemapp.BatchPreviewCardAutomation {
	// result 保存转换后的发货动作组。
	result := itemapp.BatchPreviewCardAutomation{Enabled: group.Enabled}
	// action 是当前发货动作 DTO。
	for _, action := range group.Actions {
		// converted 保存当前动作的应用层表示。
		converted := itemapp.BatchPreviewCardAction{
			CardID: action.CardID, DeliveryCount: action.DeliveryCount, DelaySeconds: action.DelaySeconds,
			ActionType: action.ActionType, DeliveryTemplateID: action.DeliveryTemplateID,
			CustomVariables: action.CustomVariables, SpecName: action.SpecName, SpecValue: action.SpecValue,
		}
		// binding 是当前模板变量绑定 DTO。
		for _, binding := range action.TemplateBindings {
			converted.TemplateBindings = append(converted.TemplateBindings, itemapp.BatchPreviewTemplateBinding(binding))
		}
		result.Actions = append(result.Actions, converted)
	}
	return result
}

// newLibraryTemplateAutomation 把应用层自动化配置转换为响应 DTO，动作集合保证非 null。
func newLibraryTemplateAutomation(automation itemapp.BatchPreviewAutomation) libraryTemplateAutomation {
	return libraryTemplateAutomation{
		PaidDelivery:  newLibraryTemplateCardAutomation(automation.PaidDelivery),
		ReviewGift:    newLibraryTemplateCardAutomation(automation.ReviewGift),
		ReviewRequest: libraryTemplateReviewRequest(automation.ReviewRequest),
	}
}

// newLibraryTemplateCardAutomation 转换一组发货动作，空动作与空绑定输出为空数组或空表。
func newLibraryTemplateCardAutomation(group itemapp.BatchPreviewCardAutomation) libraryTemplateCardAutomation {
	// result 保存响应动作组。
	result := libraryTemplateCardAutomation{Enabled: group.Enabled, Actions: make([]libraryTemplateAction, 0, len(group.Actions))}
	// action 是当前应用层发货动作。
	for _, action := range group.Actions {
		// actionType 把旧数据的空类型规范为发卡。
		actionType := action.ActionType
		if actionType == "" {
			actionType = itemapp.BatchActionSendCard
		}
		// bindings 保存模板变量绑定 DTO，保证非 null。
		bindings := make([]libraryTemplateBinding, 0, len(action.TemplateBindings))
		// binding 是当前应用层绑定。
		for _, binding := range action.TemplateBindings {
			bindings = append(bindings, libraryTemplateBinding(binding))
		}
		// variables 保证自定义变量非 null。
		variables := action.CustomVariables
		if variables == nil {
			variables = map[string]string{}
		}
		result.Actions = append(result.Actions, libraryTemplateAction{
			ActionType: actionType, CardID: action.CardID, DeliveryCount: action.DeliveryCount, DelaySeconds: action.DelaySeconds,
			DeliveryTemplateID: action.DeliveryTemplateID, TemplateBindings: bindings, CustomVariables: variables,
			SpecName: action.SpecName, SpecValue: action.SpecValue,
		})
	}
	return result
}
