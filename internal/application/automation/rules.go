package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// ErrRuleNotFound 表示规则不存在或不属于当前用户。
var ErrRuleNotFound = errors.New("自动化规则不存在")

// ErrRuleActive 表示规则仍有待处理运行，不能直接删除。
var ErrRuleActive = errors.New("规则仍有待处理的自动化运行")

// ErrPricingModeConflict 表示启用的 AI 议价账号不能再启用固定规则改价。
var ErrPricingModeConflict = errors.New("该账号已启用 AI 议价，不能同时启用自动化规则改价")

// TriggerOrderCreated 表示买家拍下未付款触发器。
const TriggerOrderCreated = "order_created"

// TriggerOrderPaid 表示付款后触发器。
const TriggerOrderPaid = "order_paid"

// TriggerBuyerReviewed 表示买家评价后触发器。
const TriggerBuyerReviewed = "buyer_reviewed"

// TriggerReviewMissingTimeout 表示超时未评价触发器。
const TriggerReviewMissingTimeout = "review_missing_timeout"

// ActionConfirmShipment 表示确认发货动作。
const ActionConfirmShipment = "confirm_shipment"

// ActionSendCard 表示发送卡密动作。
const ActionSendCard = "send_card"

// ActionSendTemplate 表示按可复用模板发送多条发货消息。
const ActionSendTemplate = "send_template"

// ActionSendText 表示发送文本动作。
const ActionSendText = "send_text"

// ActionAdjustPrice 表示把待付款订单价格修改为目标价格的动作。
const ActionAdjustPrice = "adjust_price"

// ActionDraft 是 HTTP/应用边界使用的自动化动作输入。
type ActionDraft struct {
	// ActionType 是动作类型。
	ActionType string
	// CardID 是发送卡密动作使用的卡密组标识。
	CardID int64
	// DeliveryCount 是动作发送数量。
	DeliveryCount int
	// MessageTemplate 是发送文本动作的文案。
	MessageTemplate string
	// DelaySeconds 是动作执行前的延迟秒数。
	DelaySeconds int
	// ConfigJSON 是动作扩展配置 JSON 对象。
	ConfigJSON string
	// Enabled 表示动作是否启用。
	Enabled *bool
	// SortOrder 是动作在规则中的顺序。
	SortOrder int
	// DeliveryTemplateID 是模板发货动作引用的模板标识。
	DeliveryTemplateID int64
	// TemplateBindings 是模板变量到卡密组的绑定列表。
	TemplateBindings []TemplateBinding
	// CustomVariables 是规则传给模板的自定义字符串键值表。
	CustomVariables map[string]string
}

// RuleDraft 是创建或更新自动化规则的业务输入。
type RuleDraft struct {
	// CookieID 是规则所属账号标识。
	CookieID string
	// ItemID 是可选的商品标识。
	ItemID string
	// Name 是规则显示名称。
	Name string
	// TriggerType 是规则触发类型。
	TriggerType string
	// Enabled 表示规则是否启用。
	Enabled bool
	// Priority 是规则匹配优先级。
	Priority int
	// ConfigJSON 是规则扩展配置 JSON 对象。
	ConfigJSON string
	// Actions 是规则包含的动作列表。
	Actions []ActionDraft
}

// RuleInput 是通过校验并可交给仓储写入的规则模型。
type RuleInput struct {
	// UserID 是规则所属用户。
	UserID int64
	// CookieID 是规则所属账号标识。
	CookieID string
	// ItemID 是可选的商品标识。
	ItemID string
	// Name 是规则显示名称。
	Name string
	// TriggerType 是规则触发类型。
	TriggerType string
	// Enabled 表示规则是否启用。
	Enabled bool
	// Priority 是规则匹配优先级。
	Priority int
	// ConfigJSON 是规则扩展配置 JSON 对象。
	ConfigJSON string
	// Actions 是经过规范化的动作列表。
	Actions []ActionInput
}

// ActionInput 是经过校验并可持久化的自动化动作。
type ActionInput struct {
	// ActionType 是动作类型。
	ActionType string
	// CardID 是发送卡密动作使用的卡密组标识。
	CardID int64
	// DeliveryCount 是动作发送数量。
	DeliveryCount int
	// MessageTemplate 是发送文本动作的文案。
	MessageTemplate string
	// DelaySeconds 是动作执行前的延迟秒数。
	DelaySeconds int
	// ConfigJSON 是动作扩展配置 JSON 对象。
	ConfigJSON string
	// Enabled 表示动作是否启用。
	Enabled bool
	// SortOrder 是动作在规则中的顺序。
	SortOrder int
	// DeliveryTemplateID 是模板发货动作引用的模板标识。
	DeliveryTemplateID int64
	// TemplateBindings 是已经校验的模板变量绑定列表。
	TemplateBindings []TemplateBinding
	// CustomVariables 是经过校验并持久化的模板自定义字符串键值表。
	CustomVariables map[string]string
}

// Rule 是返回给 HTTP 适配层的非数据库规则模型。
type Rule struct {
	// ID 是规则持久化标识。
	ID int64
	// CookieID 是规则所属账号标识。
	CookieID string
	// ItemID 是可选的商品标识。
	ItemID string
	// ItemTitle 是商品标题摘要。
	ItemTitle string
	// Name 是规则显示名称。
	Name string
	// TriggerType 是规则触发类型。
	TriggerType string
	// Enabled 表示规则是否启用。
	Enabled bool
	// Priority 是规则匹配优先级。
	Priority int
	// ConfigJSON 是规则扩展配置。
	ConfigJSON string
	// Actions 是规则动作列表。
	Actions []Action
	// CreatedAt 是规则创建时间文本。
	CreatedAt string
	// UpdatedAt 是规则更新时间文本。
	UpdatedAt string
}

// Action 是返回给 HTTP 适配层的非数据库动作模型。
type Action struct {
	// ID 是动作持久化标识。
	ID int64
	// ActionType 是动作类型。
	ActionType string
	// CardID 是关联卡密组标识。
	CardID int64
	// CardName 是关联卡密组名称。
	CardName string
	// DeliveryCount 是动作发送数量。
	DeliveryCount int
	// MessageTemplate 是发送文本文案。
	MessageTemplate string
	// DelaySeconds 是动作延迟秒数。
	DelaySeconds int
	// ConfigJSON 是动作扩展配置。
	ConfigJSON string
	// Enabled 表示动作是否启用。
	Enabled bool
	// SortOrder 是动作顺序。
	SortOrder int
	// DeliveryTemplateID 是模板发货动作引用的模板标识。
	DeliveryTemplateID int64
	// DeliveryTemplateName 是模板名称展示字段。
	DeliveryTemplateName string
	// TemplateMessages 是模板动作的有序消息。
	TemplateMessages []string
	// TemplateKeys 是模板动作需要绑定的变量键。
	TemplateKeys []string
	// TemplateBindings 是模板变量绑定展示列表。
	TemplateBindings []TemplateBinding
	// CustomVariables 是规则保存时传入模板的自定义字符串键值表。
	CustomVariables map[string]string
}

// TemplateBinding 是应用层模板变量到卡密组的绑定。
type TemplateBinding struct {
	// VariableKey 是模板变量键。
	VariableKey string
	// CardID 是绑定的卡密组标识。
	CardID int64
	// CardName 是卡密组名称。
	CardName string
	// DeliveryCount 是每件商品准备的卡密份数。
	DeliveryCount int
}

// TemplateInfo 是规则校验所需的模板非敏感摘要。
type TemplateInfo struct {
	// Enabled 表示模板是否可供规则引用。
	Enabled bool
	// Keys 是模板需要绑定的变量键。
	Keys []string
	// CustomKeys 是模板需要规则提供的自定义变量键。
	CustomKeys []string
}

// RuleFilter 是用户范围规则分页查询条件。
type RuleFilter struct {
	// UserID 是查询所属用户。
	UserID int64
	// CookieID 是可选账号过滤条件。
	CookieID string
	// TriggerType 是可选触发类型过滤条件。
	TriggerType string
	// Enabled 是可选启用状态过滤条件。
	Enabled *bool
	// Search 是规则名称或商品搜索词。
	Search string
	// Limit 是分页大小。
	Limit int
	// Offset 是分页偏移量。
	Offset int
}

// CardInfo 是规则校验所需的最小卡密组信息。
type CardInfo struct {
	// Enabled 表示卡密组当前是否允许被发货动作使用。
	Enabled bool
	// Type 是卡密组类型。
	Type string
	// APIReady 表示 API 卡券配置已通过完整校验且允许被规则选择。
	APIReady bool
}

// RuleRepository 定义规则持久化所需的窄接口。
type RuleRepository interface {
	// ListForUser 返回用户全部规则。
	ListForUser(ctx context.Context, userID int64) ([]Rule, error)
	// ListPageForUser 返回用户规则分页和总数。
	ListPageForUser(ctx context.Context, filter RuleFilter) ([]Rule, int, error)
	// CountByTriggerForUser 返回用户规则触发类型统计。
	CountByTriggerForUser(ctx context.Context, filter RuleFilter) (map[string]int, error)
	// Create 创建规则并返回标识。
	Create(ctx context.Context, input RuleInput) (int64, error)
	// Update 更新用户拥有的规则。
	Update(ctx context.Context, userID, ruleID int64, input RuleInput) error
	// Delete 删除用户拥有的规则。
	Delete(ctx context.Context, userID, ruleID int64) error
}

// RuleOwnership 定义规则校验所需的账号、商品和卡密组归属能力。
type RuleOwnership interface {
	// OwnsAccount 判断账号是否属于用户。
	OwnsAccount(ctx context.Context, userID int64, accountID string) (bool, error)
	// OwnsItem 判断商品是否属于用户账号。
	OwnsItem(ctx context.Context, userID int64, accountID, itemID string) (bool, error)
	// GetCard 返回用户拥有的卡密组类型。
	GetCard(ctx context.Context, userID, cardID int64) (CardInfo, error)
	// AIReplyEnabled 判断账号是否启用了 AI 议价模式。
	AIReplyEnabled(ctx context.Context, accountID string) (bool, error)
}

// RuleService 编排自动化规则校验、分页和持久化。
type RuleService struct {
	// repository 提供规则持久化能力。
	repository RuleRepository
	// ownership 提供规则输入的归属与卡密组校验能力。
	ownership RuleOwnership
}

// NewRuleService 构造自动化规则应用服务。
func NewRuleService(repository RuleRepository, ownership RuleOwnership) *RuleService {
	return &RuleService{repository: repository, ownership: ownership}
}

// ListForUser 查询用户全部自动化规则。
func (s *RuleService) ListForUser(ctx context.Context, userID int64) ([]Rule, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repository.ListForUser(ctx, userID)
}

// ListPageForUser 查询用户自动化规则分页并归一化分页参数。
func (s *RuleService) ListPageForUser(ctx context.Context, filter RuleFilter) ([]Rule, int, error) {
	if s == nil || s.repository == nil || filter.UserID <= 0 {
		return nil, 0, ErrInvalidInput
	}
	if filter.Limit <= 0 || filter.Limit > 100 {
		filter.Limit = 10
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	return s.repository.ListPageForUser(ctx, filter)
}

// CountByTriggerForUser 查询用户规则触发类型统计。
func (s *RuleService) CountByTriggerForUser(ctx context.Context, filter RuleFilter) (map[string]int, error) {
	if s == nil || s.repository == nil || filter.UserID <= 0 {
		return nil, ErrInvalidInput
	}
	return s.repository.CountByTriggerForUser(ctx, filter)
}

// Normalize 校验并规范化 HTTP 边界传入的规则草稿。
func (s *RuleService) Normalize(ctx context.Context, userID int64, draft RuleDraft) (RuleInput, error) {
	if s == nil || s.repository == nil || s.ownership == nil || userID <= 0 {
		return RuleInput{}, ErrInvalidInput
	}
	draft.CookieID = strings.TrimSpace(draft.CookieID)
	draft.ItemID = strings.TrimSpace(draft.ItemID)
	draft.Name = strings.TrimSpace(draft.Name)
	draft.TriggerType = strings.TrimSpace(draft.TriggerType)
	if draft.TriggerType != TriggerOrderCreated && draft.TriggerType != TriggerOrderPaid && draft.TriggerType != TriggerBuyerReviewed && draft.TriggerType != TriggerReviewMissingTimeout {
		return RuleInput{}, errors.New("不支持的触发类型")
	}
	// owned 表示账号是否归当前用户所有；err 表示归属查询失败。
	owned, err := s.ownership.OwnsAccount(ctx, userID, draft.CookieID)
	if err != nil {
		return RuleInput{}, err
	}
	if !owned {
		return RuleInput{}, errors.New("账号不存在或不属于当前用户")
	}
	if draft.ItemID != "" {
		owned, err = s.ownership.OwnsItem(ctx, userID, draft.CookieID, draft.ItemID)
		if err != nil {
			return RuleInput{}, err
		}
		if !owned {
			return RuleInput{}, errors.New("商品不属于当前用户")
		}
	}
	if draft.Priority <= 0 {
		draft.Priority = 100
	}
	if draft.ConfigJSON == "" {
		draft.ConfigJSON = "{}"
	}
	if !isJSONObject(draft.ConfigJSON) {
		return RuleInput{}, errors.New("规则配置必须是 JSON 对象")
	}
	if len(draft.Actions) == 0 {
		return RuleInput{}, errors.New("至少需要一个自动化动作")
	}
	if draft.Name == "" {
		draft.Name = defaultRuleName(draft.TriggerType, draft.ItemID)
	}
	// actions 是规范化后的动作列表；flags 汇总启用动作类型，供触发类型组合校验使用。
	actions, flags, actionsErr := s.normalizeDraftActions(ctx, userID, draft.TriggerType, draft.Actions)
	if actionsErr != nil {
		return RuleInput{}, actionsErr
	}
	// combinationErr 表示触发类型与启用动作组合不满足业务约束。
	if combinationErr := validateTriggerActionCombination(draft.TriggerType, flags); combinationErr != nil {
		return RuleInput{}, combinationErr
	}
	if draft.Enabled && flags.hasAdjustPrice {
		// aiEnabled 表示账号是否已经采用 AI 议价；aiErr 是非敏感设置读取错误。
		aiEnabled, aiErr := s.ownership.AIReplyEnabled(ctx, draft.CookieID)
		if aiErr != nil {
			return RuleInput{}, aiErr
		}
		if aiEnabled {
			return RuleInput{}, ErrPricingModeConflict
		}
	}
	return RuleInput{UserID: userID, CookieID: draft.CookieID, ItemID: draft.ItemID, Name: draft.Name,
		TriggerType: draft.TriggerType, Enabled: draft.Enabled, Priority: draft.Priority,
		ConfigJSON: draft.ConfigJSON, Actions: actions}, nil
}

// ruleActionFlags 汇总规则草稿中各类启用动作的存在情况，供触发类型组合校验使用。
type ruleActionFlags struct {
	// hasSendCard 表示是否存在启用的发卡动作。
	hasSendCard bool
	// hasSendTemplate 表示是否存在启用的模板发货动作。
	hasSendTemplate bool
	// hasSendText 表示是否存在启用的文本动作。
	hasSendText bool
	// hasConfirmShipment 表示是否存在启用的确认发货动作。
	hasConfirmShipment bool
	// hasAdjustPrice 表示是否存在启用的订单改价动作。
	hasAdjustPrice bool
}

// normalizeDraftActions 逐个校验并规范化规则草稿中的动作，同时汇总启用动作类型标志。
func (s *RuleService) normalizeDraftActions(ctx context.Context, userID int64, triggerType string, draftActions []ActionDraft) ([]ActionInput, ruleActionFlags, error) {
	// actions 保存规范化后的动作；flags 记录启用动作类型，供规则完整性校验使用。
	actions := make([]ActionInput, 0, len(draftActions))
	// flags 汇总当前草稿中启用的动作类型。
	var flags ruleActionFlags
	// index 是当前动作在草稿中的位置；draftAction 是待校验和规范化的动作。
	for index, draftAction := range draftActions {
		// enabled 表示当前动作是否参与运行；未提供时默认启用。
		enabled := true
		if draftAction.Enabled != nil {
			enabled = *draftAction.Enabled
		}
		draftAction.ActionType = strings.TrimSpace(draftAction.ActionType)
		if draftAction.CustomVariables == nil {
			draftAction.CustomVariables = customVariablesFromConfig(draftAction.ConfigJSON)
		}
		switch draftAction.ActionType {
		case ActionConfirmShipment:
			flags.hasConfirmShipment = flags.hasConfirmShipment || enabled
		case ActionSendCard:
			// cardErr 表示卡密选择缺失、读取失败或类型不支持。
			if cardErr := s.validateSendCardAction(ctx, userID, draftAction); cardErr != nil {
				return nil, flags, cardErr
			}
			flags.hasSendCard = flags.hasSendCard || enabled
		case ActionSendTemplate:
			if triggerType != TriggerOrderPaid && triggerType != TriggerBuyerReviewed {
				return nil, flags, errors.New("发货模板动作仅支持付款发货或评价赠品")
			}
			if draftAction.DeliveryTemplateID <= 0 {
				return nil, flags, errors.New("发货模板动作必须选择发货模板")
			}
			// templateOwnership 保存可选的模板归属能力，兼容只支持旧动作的测试仓储。
			templateOwnership, ok := s.ownership.(interface {
				GetDeliveryTemplate(context.Context, int64, int64) (TemplateInfo, error)
			})
			if !ok {
				return nil, flags, errors.New("发货模板能力未装配")
			}
			// template 保存模板变量摘要和启用状态。
			template, templateErr := templateOwnership.GetDeliveryTemplate(ctx, userID, draftAction.DeliveryTemplateID)
			if templateErr != nil {
				return nil, flags, templateErr
			}
			if !template.Enabled {
				return nil, flags, errors.New("发货模板不存在或已停用")
			}
			// bindingErr 保存模板变量绑定校验失败原因。
			if bindingErr := validateTemplateBindings(ctx, s.ownership, userID, template.Keys, draftAction.TemplateBindings); bindingErr != nil {
				return nil, flags, bindingErr
			}
			// customErr 保存模板自定义变量数量校验失败原因。
			if customErr := validateTemplateCustomVariables(template.CustomKeys, draftAction.CustomVariables); customErr != nil {
				return nil, flags, customErr
			}
			flags.hasSendTemplate = flags.hasSendTemplate || enabled
		case ActionSendText:
			if strings.TrimSpace(draftAction.MessageTemplate) == "" {
				return nil, flags, errors.New("发送文本动作必须填写文案")
			}
			flags.hasSendText = flags.hasSendText || enabled
		case ActionAdjustPrice:
			// priceErr 表示改价动作目标价格缺失或格式非法。
			if priceErr := validateAdjustPriceConfig(draftAction.ConfigJSON); priceErr != nil {
				return nil, flags, priceErr
			}
			flags.hasAdjustPrice = flags.hasAdjustPrice || enabled
		default:
			return nil, flags, errors.New("不支持的动作类型")
		}
		if draftAction.DeliveryCount <= 0 {
			draftAction.DeliveryCount = 1
		}
		if draftAction.DelaySeconds < 0 || draftAction.DelaySeconds > 3600 {
			return nil, flags, errors.New("动作延时必须在 0 到 3600 秒之间")
		}
		if draftAction.ConfigJSON == "" {
			draftAction.ConfigJSON = "{}"
		}
		if !isJSONObject(draftAction.ConfigJSON) {
			return nil, flags, errors.New("动作配置必须是 JSON 对象")
		}
		// configErr 保存把规则自定义变量写入动作配置的错误。
		configErr := error(nil)
		if draftAction.ActionType == ActionSendTemplate {
			draftAction.ConfigJSON, configErr = withCustomVariables(draftAction.ConfigJSON, draftAction.CustomVariables)
			if configErr != nil {
				return nil, flags, configErr
			}
		}
		actions = append(actions, ActionInput{ActionType: draftAction.ActionType, CardID: draftAction.CardID,
			DeliveryCount: draftAction.DeliveryCount, MessageTemplate: draftAction.MessageTemplate,
			DelaySeconds: draftAction.DelaySeconds, ConfigJSON: draftAction.ConfigJSON, Enabled: enabled,
			SortOrder: firstRuleNonZero(draftAction.SortOrder, index+1), DeliveryTemplateID: draftAction.DeliveryTemplateID,
			TemplateBindings: append([]TemplateBinding(nil), draftAction.TemplateBindings...),
			CustomVariables:  copyCustomVariables(draftAction.CustomVariables)})
	}
	return actions, flags, nil
}

// validateTemplateCustomVariables 校验模板使用的自定义变量键都能从规则键值表中取到非空字符串。
func validateTemplateCustomVariables(keys []string, values map[string]string) error {
	for /* key 表示模板引用的自定义变量键。 */ _, key := range keys {
		if strings.TrimSpace(values[key]) == "" {
			return fmt.Errorf("请填写发货模板自定义变量 %q", key)
		}
	}
	return nil
}

// withCustomVariables 把规则页提交的自定义字符串键值表写入动作配置，保证延迟运行仍能使用原始值。
func withCustomVariables(configJSON string, values map[string]string) (string, error) {
	// config 保存动作配置对象，保留已有规格和延迟字段。
	config := make(map[string]any)
	// err 保存动作配置 JSON 解码错误。
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return "", errors.New("动作配置必须是 JSON 对象")
	}
	// normalized 保存去除键和值首尾空白后的自定义变量键值表。
	normalized := make(map[string]string, len(values))
	for /* key 表示自定义变量键；value 表示规则页输入的字符串。 */ key, value := range values {
		normalized[strings.TrimSpace(key)] = strings.TrimSpace(value)
	}
	config["custom_variables"] = normalized
	// encoded 保存包含自定义变量的规范化 JSON 配置。
	encoded, err := json.Marshal(config)
	if err != nil {
		return "", errors.New("保存发货模板自定义变量失败")
	}
	return string(encoded), nil
}

// customVariablesFromConfig 从动作配置 JSON 中读取键值表，并兼容历史字符串数组格式。
func customVariablesFromConfig(configJSON string) map[string]string {
	// config 保存动作配置中与模板相关的非敏感字段原文。
	var config map[string]json.RawMessage
	// err 保存动作配置解析错误。
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil {
		return nil
	}
	// raw 保存自定义变量字段的 JSON 原文，便于兼容对象和历史数组两种格式。
	raw := config["custom_variables"]
	// values 保存新格式的自定义变量键值表。
	var values map[string]string
	if json.Unmarshal(raw, &values) == nil && values != nil {
		return copyCustomVariables(values)
	}
	// legacyValues 保存历史数组格式的自定义变量值。
	var legacyValues []string
	if json.Unmarshal(raw, &legacyValues) != nil {
		return nil
	}
	// converted 保存按数组下标转换得到的兼容键值表。
	converted := make(map[string]string, len(legacyValues))
	for /* index 表示历史数组下标；value 表示历史自定义字符串。 */ index, value := range legacyValues {
		converted[strconv.Itoa(index)] = value
	}
	return converted
}

// copyCustomVariables 复制自定义变量键值表，避免应用模型与调用方共享可变 map。
func copyCustomVariables(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}
	// copied 保存自定义变量键值表的独立副本。
	copied := make(map[string]string, len(values))
	for /* key 表示自定义变量键；value 表示自定义字符串。 */ key, value := range values {
		copied[key] = value
	}
	return copied
}

// validateSendCardAction 校验发卡动作的卡密选择和归属；具体卡券类型由执行器处理。
func (s *RuleService) validateSendCardAction(ctx context.Context, userID int64, draftAction ActionDraft) error {
	if draftAction.CardID <= 0 {
		return errors.New("发送卡密动作必须选择卡密组")
	}
	// card 是归属校验通过的卡密摘要；cardErr 表示卡密读取或归属校验失败。
	card, cardErr := s.ownership.GetCard(ctx, userID, draftAction.CardID)
	if cardErr != nil {
		if !errors.Is(cardErr, ErrRuleNotFound) {
			return cardErr
		}
		return errors.New("卡密组不存在或不属于当前用户")
	}
	if card.Type == "api" && !card.APIReady {
		return errors.New("API 卡券配置无效，请重新保存后再选择")
	}
	return nil
}

// validateTemplateBindings 校验模板变量覆盖完整且每个卡密组属于当前用户。
func validateTemplateBindings(ctx context.Context, ownership RuleOwnership, userID int64, keys []string, bindings []TemplateBinding) error {
	if len(keys) != len(bindings) {
		return errors.New("发货模板的卡密变量绑定不完整")
	}
	// expected 保存模板变量键集合，避免重复绑定或遗漏。
	expected := make(map[string]bool, len(keys))
	for /* key 表示模板要求绑定的变量键。 */ _, key := range keys {
		expected[key] = true
	}
	// seen 保存已经出现的绑定键。
	seen := make(map[string]bool, len(bindings))
	for /* binding 表示当前变量到卡密组的绑定。 */ _, binding := range bindings {
		if !expected[binding.VariableKey] || seen[binding.VariableKey] || binding.CardID <= 0 {
			return errors.New("发货模板的卡密变量绑定无效")
		}
		seen[binding.VariableKey] = true
		// card 保存绑定卡密组的非敏感摘要。
		card, err := ownership.GetCard(ctx, userID, binding.CardID)
		if err != nil {
			return err
		}
		if card.Type != "text" && card.Type != "data" {
			return errors.New("发货模板只能绑定文本或批量数据卡密组")
		}
		if !card.Enabled {
			return errors.New("发货模板不能绑定已停用的卡密组")
		}
	}
	if len(seen) != len(expected) {
		return errors.New("发货模板的卡密变量绑定不完整")
	}
	return nil
}

// validateTriggerActionCombination 校验触发类型允许的动作组合和必需动作。
func validateTriggerActionCombination(triggerType string, flags ruleActionFlags) error {
	switch triggerType {
	case TriggerOrderCreated:
		if flags.hasConfirmShipment || flags.hasSendCard {
			return errors.New("拍下未付款规则只能包含改价和文本动作")
		}
		if !flags.hasAdjustPrice {
			return errors.New("拍下未付款规则至少需要一个已启用的改价动作")
		}
	case TriggerOrderPaid:
		if flags.hasAdjustPrice {
			return errors.New("改价动作只能用于拍下未付款规则")
		}
		if !flags.hasSendCard && !flags.hasSendTemplate {
			return errors.New("付款后自动发货至少需要一个已启用的发送卡密或模板动作")
		}
	case TriggerBuyerReviewed:
		if flags.hasConfirmShipment {
			return errors.New("评价后规则不能包含确认发货动作")
		}
		if flags.hasAdjustPrice {
			return errors.New("改价动作只能用于拍下未付款规则")
		}
		if !flags.hasSendCard && !flags.hasSendTemplate && !flags.hasSendText {
			return errors.New("评价后规则至少需要一个已启用的发送动作")
		}
	case TriggerReviewMissingTimeout:
		if flags.hasConfirmShipment || flags.hasSendCard || flags.hasAdjustPrice {
			return errors.New("求评价规则只能发送文本")
		}
		if !flags.hasSendText {
			return errors.New("求评价规则至少需要一个已启用的文本动作")
		}
	}
	return nil
}

// Create 创建已校验的自动化规则。
func (s *RuleService) Create(ctx context.Context, input RuleInput) (int64, error) {
	if s == nil || s.repository == nil {
		return 0, ErrInvalidInput
	}
	return s.repository.Create(ctx, input)
}

// Update 更新用户拥有的自动化规则。
func (s *RuleService) Update(ctx context.Context, userID, ruleID int64, input RuleInput) error {
	if s == nil || s.repository == nil || userID <= 0 || ruleID <= 0 {
		return ErrInvalidInput
	}
	return s.repository.Update(ctx, userID, ruleID, input)
}

// Delete 删除用户拥有的自动化规则。
func (s *RuleService) Delete(ctx context.Context, userID, ruleID int64) error {
	if s == nil || s.repository == nil || userID <= 0 || ruleID <= 0 {
		return ErrInvalidInput
	}
	return s.repository.Delete(ctx, userID, ruleID)
}

// validateAdjustPriceConfig 校验改价动作配置中的目标价格。
// 金额使用十进制字符串校验：0.01 到 1000000 元、至多两位小数，禁止浮点解析。
func validateAdjustPriceConfig(configJSON string) error {
	// cfg 保存改价动作的目标价格配置；target_price 以元为单位的字符串。
	var cfg struct {
		TargetPrice string `json:"target_price"`
	}
	if json.Unmarshal([]byte(configJSON), &cfg) != nil {
		return errors.New("改价动作配置必须是 JSON 对象")
	}
	// raw 是去空白后的目标价格文本。
	raw := strings.TrimSpace(cfg.TargetPrice)
	if raw == "" {
		return errors.New("改价动作必须填写目标价格")
	}
	// wholeText、fracText 分别是金额的整数部分与小数部分文本。
	wholeText, fracText := raw, ""
	if // dot 是小数点在金额文本中的位置。
	dot := strings.IndexByte(raw, '.'); dot >= 0 {
		wholeText, fracText = raw[:dot], raw[dot+1:]
	}
	if wholeText == "" || len(fracText) > 2 {
		return errors.New("目标价格必须是最多两位小数的金额")
	}
	// whole、wholeErr 分别是整数元部分的数值和解析错误。
	whole, wholeErr := strconv.ParseInt(wholeText, 10, 64)
	if wholeErr != nil || whole < 0 {
		return errors.New("目标价格必须是最多两位小数的金额")
	}
	// frac 是小数部分折算出的分值。
	frac := int64(0)
	if fracText != "" {
		// fracValue、fracErr 分别是小数部分的数值和解析错误。
		fracValue, fracErr := strconv.ParseInt(fracText, 10, 64)
		if fracErr != nil || fracValue < 0 {
			return errors.New("目标价格必须是最多两位小数的金额")
		}
		frac = fracValue
		if len(fracText) == 1 {
			frac *= 10
		}
	}
	// cents 是目标价格的整数分结果。
	cents := whole*100 + frac
	if cents <= 0 || cents > 100000000 {
		return errors.New("目标价格必须在 0.01 到 1000000 元之间")
	}
	return nil
}

// isJSONObject 判断配置是否为 JSON 对象。
func isJSONObject(raw string) bool {
	// value 是 JSON 对象解析结果，仅用于确认配置顶层类型，不保存业务状态。
	var value map[string]any
	return json.Unmarshal([]byte(raw), &value) == nil
}

// defaultRuleName 根据触发类型和商品标识生成默认规则名称。
func defaultRuleName(triggerType, itemID string) string {
	// name 是按触发类型选择的默认显示名称，必要时再附加商品标识。
	name := map[string]string{TriggerOrderCreated: "拍下未付款自动改价", TriggerOrderPaid: "付款后自动发货", TriggerBuyerReviewed: "评价后发送赠品", TriggerReviewMissingTimeout: "超时未评价求评价"}[triggerType]
	if name == "" {
		name = "自动化规则"
	}
	if strings.TrimSpace(itemID) != "" {
		return fmt.Sprintf("%s - %s", name, strings.TrimSpace(itemID))
	}
	return name
}

// firstRuleNonZero 返回动作顺序或其默认下标。
func firstRuleNonZero(value, fallback int) int {
	if value != 0 {
		return value
	}
	return fallback
}
