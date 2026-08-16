package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

// ErrRuleNotFound 表示规则不存在或不属于当前用户。
var ErrRuleNotFound = errors.New("自动化规则不存在")

// ErrRuleActive 表示规则仍有待处理运行，不能直接删除。
var ErrRuleActive = errors.New("规则仍有待处理的自动化运行")

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

// ActionSendText 表示发送文本动作。
const ActionSendText = "send_text"

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
	// Type 是卡密组类型。
	Type string
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
	if draft.TriggerType != TriggerOrderPaid && draft.TriggerType != TriggerBuyerReviewed && draft.TriggerType != TriggerReviewMissingTimeout {
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
	// actions 保存规范化后的动作；三个布尔值记录启用动作类型，供规则完整性校验使用。
	actions := make([]ActionInput, 0, len(draft.Actions))
	// hasSendCard 表示是否存在启用的发卡动作。
	hasSendCard := false
	// hasSendText 表示是否存在启用的文本动作。
	hasSendText := false
	// hasConfirmShipment 表示是否存在启用的确认发货动作。
	hasConfirmShipment := false
	// index 是当前动作在草稿中的位置；draftAction 是待校验和规范化的动作。
	for index, draftAction := range draft.Actions {
		// enabled 表示当前动作是否参与运行；未提供时默认启用。
		enabled := true
		if draftAction.Enabled != nil {
			enabled = *draftAction.Enabled
		}
		draftAction.ActionType = strings.TrimSpace(draftAction.ActionType)
		switch draftAction.ActionType {
		case ActionConfirmShipment:
			hasConfirmShipment = hasConfirmShipment || enabled
		case ActionSendCard:
			if draftAction.CardID <= 0 {
				return RuleInput{}, errors.New("发送卡密动作必须选择卡密组")
			}
			// card 是归属校验通过的卡密摘要；cardErr 表示卡密读取或归属校验失败。
			card, cardErr := s.ownership.GetCard(ctx, userID, draftAction.CardID)
			if cardErr != nil {
				if !errors.Is(cardErr, ErrRuleNotFound) {
					return RuleInput{}, cardErr
				}
				return RuleInput{}, errors.New("卡密组不存在或不属于当前用户")
			}
			if card.Type == "api" {
				return RuleInput{}, errors.New("API 卡密暂不支持自动发货，请选择文本、批量数据或图片卡密")
			}
			hasSendCard = hasSendCard || enabled
		case ActionSendText:
			if strings.TrimSpace(draftAction.MessageTemplate) == "" {
				return RuleInput{}, errors.New("发送文本动作必须填写文案")
			}
			hasSendText = hasSendText || enabled
		default:
			return RuleInput{}, errors.New("不支持的动作类型")
		}
		if draftAction.DeliveryCount <= 0 {
			draftAction.DeliveryCount = 1
		}
		if draftAction.DelaySeconds < 0 || draftAction.DelaySeconds > 3600 {
			return RuleInput{}, errors.New("动作延时必须在 0 到 3600 秒之间")
		}
		if draftAction.ConfigJSON == "" {
			draftAction.ConfigJSON = "{}"
		}
		if !isJSONObject(draftAction.ConfigJSON) {
			return RuleInput{}, errors.New("动作配置必须是 JSON 对象")
		}
		actions = append(actions, ActionInput{ActionType: draftAction.ActionType, CardID: draftAction.CardID,
			DeliveryCount: draftAction.DeliveryCount, MessageTemplate: draftAction.MessageTemplate,
			DelaySeconds: draftAction.DelaySeconds, ConfigJSON: draftAction.ConfigJSON, Enabled: enabled,
			SortOrder: firstRuleNonZero(draftAction.SortOrder, index+1)})
	}
	switch draft.TriggerType {
	case TriggerOrderPaid:
		if !hasSendCard {
			return RuleInput{}, errors.New("付款后自动发货至少需要一个已启用的发送卡密动作")
		}
	case TriggerBuyerReviewed:
		if hasConfirmShipment {
			return RuleInput{}, errors.New("评价后规则不能包含确认发货动作")
		}
		if !hasSendCard && !hasSendText {
			return RuleInput{}, errors.New("评价后规则至少需要一个已启用的发送动作")
		}
	case TriggerReviewMissingTimeout:
		if hasConfirmShipment || hasSendCard {
			return RuleInput{}, errors.New("求评价规则只能发送文本")
		}
		if !hasSendText {
			return RuleInput{}, errors.New("求评价规则至少需要一个已启用的文本动作")
		}
	}
	return RuleInput{UserID: userID, CookieID: draft.CookieID, ItemID: draft.ItemID, Name: draft.Name,
		TriggerType: draft.TriggerType, Enabled: draft.Enabled, Priority: draft.Priority,
		ConfigJSON: draft.ConfigJSON, Actions: actions}, nil
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

// isJSONObject 判断配置是否为 JSON 对象。
func isJSONObject(raw string) bool {
	// value 是 JSON 对象解析结果，仅用于确认配置顶层类型，不保存业务状态。
	var value map[string]any
	return json.Unmarshal([]byte(raw), &value) == nil
}

// defaultRuleName 根据触发类型和商品标识生成默认规则名称。
func defaultRuleName(triggerType, itemID string) string {
	// name 是按触发类型选择的默认显示名称，必要时再附加商品标识。
	name := map[string]string{TriggerOrderPaid: "付款后自动发货", TriggerBuyerReviewed: "评价后发送赠品", TriggerReviewMissingTimeout: "超时未评价求评价"}[triggerType]
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
