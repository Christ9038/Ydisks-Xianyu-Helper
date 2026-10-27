package items

import (
	"encoding/json"

	automationapp "xianyu-go/internal/application/automation"
)

// batchDeliveryActions 把批量配置中的发货动作转换为规则动作，保持配置顺序；
// 发卡与发货模板动作共用一条动作链，多规格动作把规范化规格写入动作配置。
func batchDeliveryActions(actions []batchPublishCardAction) []automationapp.ActionInput {
	// result 保存转换后的规则动作，顺序与批量配置一致。
	result := make([]automationapp.ActionInput, 0, len(actions)+1)
	// index、action 是当前动作在配置中的顺序和内容。
	for index, action := range actions {
		// config 保存动作延迟策略及可选的规格绑定。
		config := map[string]any{"delay_override": true}
		if action.SpecName != "" || action.SpecValue != "" {
			config["spec_name"], config["spec_value"] = action.SpecName, action.SpecValue
		}
		// configJSON 是动作配置的序列化文本；map 仅含字符串和布尔值，序列化不会失败。
		configJSON, _ := json.Marshal(config)
		// input 保存当前动作的公共字段。
		input := automationapp.ActionInput{DeliveryCount: action.DeliveryCount, DelaySeconds: action.DelaySeconds,
			ConfigJSON: string(configJSON), Enabled: true, SortOrder: index + 1}
		if action.ActionType == BatchActionSendTemplate {
			input.ActionType = automationapp.ActionSendTemplate
			input.DeliveryTemplateID = action.DeliveryTemplateID
			input.CustomVariables = action.CustomVariables
			// binding 是当前模板变量绑定。
			for _, binding := range action.TemplateBindings {
				input.TemplateBindings = append(input.TemplateBindings, automationapp.TemplateBinding{VariableKey: binding.VariableKey, CardID: binding.CardID, DeliveryCount: binding.DeliveryCount})
			}
		} else {
			input.ActionType = automationapp.ActionSendCard
			input.CardID = action.CardID
		}
		result = append(result, input)
	}
	return result
}
