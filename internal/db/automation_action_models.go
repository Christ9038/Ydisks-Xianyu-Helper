package db

// AutomationAction 是规则下的一步动作。
type AutomationAction struct {
	ID              int64
	RuleID          int64
	ActionType      string
	CardID          int64
	CardName        string
	DeliveryCount   int
	MessageTemplate string
	DelaySeconds    int
	ConfigJSON      string
	Enabled         bool
	SortOrder       int
	// DeliveryTemplateID 是模板发货动作引用的模板标识。
	DeliveryTemplateID int64
	// DeliveryTemplateName 是模板名称展示字段。
	DeliveryTemplateName string
	// TemplateMessages 是模板动作执行时使用的有序消息内容。
	TemplateMessages []string
	// TemplateKeys 是模板动作需要绑定的变量键。
	TemplateKeys []string
	// TemplateBindings 是变量键到卡密组的绑定列表。
	TemplateBindings []DeliveryTemplateBinding
	// CustomVariables 是规则传给发货模板的自定义字符串键值表。
	CustomVariables map[string]string
}

// AutomationActionInput 是创建动作的输入。
type AutomationActionInput struct {
	ActionType      string
	CardID          int64
	DeliveryCount   int
	MessageTemplate string
	DelaySeconds    int
	ConfigJSON      string
	Enabled         bool
	SortOrder       int
	// DeliveryTemplateID 是模板发货动作引用的模板标识。
	DeliveryTemplateID int64
	// TemplateBindings 是模板变量到卡密组的写入绑定。
	TemplateBindings []DeliveryTemplateBinding
	// CustomVariables 是规则传给发货模板的自定义字符串键值表。
	CustomVariables map[string]string
}
