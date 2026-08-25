package server

// automationActionResponse 是自动化规则动作的具名响应 DTO。
type automationActionResponse struct {
	// ID 是动作稳定标识。
	ID int64 `json:"id"`
	// ActionType 是动作类型。
	ActionType string `json:"action_type"`
	// CardID 是动作关联卡券组标识。
	CardID int64 `json:"card_id"`
	// CardName 是动作关联卡券组名称。
	CardName string `json:"card_name"`
	// DeliveryCount 是动作发送数量。
	DeliveryCount int `json:"delivery_count"`
	// MessageTemplate 是动作消息模板。
	MessageTemplate string `json:"message_template"`
	// DelaySeconds 是动作延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
	// ConfigJSON 是动作扩展配置 JSON。
	ConfigJSON string `json:"config_json"`
	// Enabled 表示动作是否启用。
	Enabled bool `json:"enabled"`
	// SortOrder 是动作执行顺序。
	SortOrder int `json:"sort_order"`
	// DeliveryTemplateID 是模板发货动作引用的模板标识。
	DeliveryTemplateID int64 `json:"delivery_template_id"`
	// DeliveryTemplateName 是模板名称。
	DeliveryTemplateName string `json:"delivery_template_name"`
	// TemplateKeys 是模板变量键列表。
	TemplateKeys []string `json:"template_keys"`
	// TemplateBindings 是模板变量到卡密组的绑定列表。
	TemplateBindings []automationTemplateBindingResponse `json:"template_bindings"`
	// CustomVariables 是传给发货模板的自定义字符串键值表。
	CustomVariables map[string]string `json:"custom_variables"`
}
