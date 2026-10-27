package items

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	automationapp "xianyu-go/internal/application/automation"
)

// ErrBatchLocalPublishUnavailable 表示批量发布成功后的本地收口服务未完成装配。
var ErrBatchLocalPublishUnavailable = errors.New("批量发布本地收口服务未初始化")

// BatchCompletionRepository 定义批量发布本地成功收口所需的最小批次端口。
type BatchCompletionRepository interface {
	// GetBatch 读取批次归属、状态和当前 worker 租约。
	GetBatch(context.Context, int64, string) (BatchInfo, error)
	// MarkClaimedRowSuccess 只允许持有明细租约的 worker 保存成功结果。
	MarkClaimedRowSuccess(context.Context, int64, string, string, string, string) (bool, error)
}

// BatchPublishedItem 是远端商品成功后保存到本地商品目录的非敏感模型。
type BatchPublishedItem struct {
	// CookieID 是商品所属账号标识。
	CookieID string
	// ItemID 是平台商品标识。
	ItemID string
	// ItemTitle 是平台确认后的商品标题。
	ItemTitle string
	// ItemDescription 是批量发布时保存的商品描述。
	ItemDescription string
	// ItemCategory 是平台确认后的类目标识。
	ItemCategory string
	// ItemPrice 是平台确认后的价格文本。
	ItemPrice string
	// ItemDetail 是供商品详情页使用的扩展 JSON。
	ItemDetail string
	// MultiQuantityDelivery 表示库存大于一时是否启用多数量交付。
	MultiQuantityDelivery bool
	// IsMultiSpec 表示商品以多规格 SKU 发布，规则动作需要按规格绑定。
	IsMultiSpec bool
}

// BatchPublishedItemRepository 保存远端发布成功后的本地商品记录。
type BatchPublishedItemRepository interface {
	// UpsertPublishedItem 创建或更新本地商品记录。
	UpsertPublishedItem(context.Context, BatchPublishedItem) error
}

// BatchLocalPublisher 定义远端商品发布成功后的本地收口能力。
// adapter 只依赖该 Port，不应持有具体应用服务实现。
type BatchLocalPublisher interface {
	Complete(context.Context, int64, BatchRow, string, *BatchPublishResult) error
}

// BatchLocalPublishService 编排远端成功后的本地商品、自动化规则和批次成功收口。
type BatchLocalPublishService struct {
	// completionRepository 负责复核批次租约并保存明细成功检查点。
	completionRepository BatchCompletionRepository
	// itemRepository 负责保存非敏感本地商品目录记录。
	itemRepository BatchPublishedItemRepository
	// ruleRepository 负责幂等创建发布后的自动化规则。
	ruleRepository automationapp.PublishRuleRepository
}

// NewBatchLocalPublishService 构造批量发布本地收口服务并校验必需端口。
func NewBatchLocalPublishService(completionRepository BatchCompletionRepository, itemRepository BatchPublishedItemRepository, ruleRepository automationapp.PublishRuleRepository) (*BatchLocalPublishService, error) {
	if completionRepository == nil {
		return nil, errors.New("批量发布批次收口端口不能为空")
	}
	if itemRepository == nil {
		return nil, errors.New("批量发布商品收口端口不能为空")
	}
	if ruleRepository == nil {
		return nil, errors.New("批量发布规则收口端口不能为空")
	}
	return &BatchLocalPublishService{completionRepository: completionRepository, itemRepository: itemRepository, ruleRepository: ruleRepository}, nil
}

// Complete 保存远端成功后的本地状态；失败时返回后置错误以阻止不可逆远端动作重试。
func (service *BatchLocalPublishService) Complete(ctx context.Context, userID int64, row BatchRow, workerToken string, result *BatchPublishResult) error {
	if service == nil || service.completionRepository == nil || service.itemRepository == nil || service.ruleRepository == nil {
		return errors.New("批量发布本地收口服务未初始化")
	}
	if result == nil || result.ItemID == "" {
		return errors.New("发布商品接口未返回结果")
	}
	// batch、batchErr 保存当前批次状态及租约复核错误。
	batch, batchErr := service.completionRepository.GetBatch(ctx, userID, row.BatchID)
	if batchErr != nil || batch.Status == "canceled" || batch.WorkerToken != workerToken {
		return &PostPublishError{Err: context.Canceled}
	}
	// detail 保存商品详情页需要的非敏感扩展字段。
	detail := map[string]any{"item_image": result.ImageURL, "web_url": result.ItemURL, "category_name": result.CategoryName, "quantity": result.Quantity, "publish_raw": result.RawData}
	// detailJSON 保存商品详情扩展字段的 JSON 表示。
	detailJSON, _ := json.Marshal(detail)
	// specs、specsErr 保存行的多规格快照；损坏的快照在远端成功后按后置错误处理，避免重放发布。
	specs, specsErr := DecodeBatchSpecs(row.SpecsJSON)
	if specsErr != nil {
		return &PostPublishError{Err: specsErr}
	}
	// itemErr 保存本地商品目录写入错误。
	itemErr := service.itemRepository.UpsertPublishedItem(ctx, BatchPublishedItem{
		CookieID: row.CookieID, ItemID: result.ItemID, ItemTitle: firstBatchResultTitle(result.Title, row.Title),
		ItemDescription: row.Description, ItemCategory: result.CategoryID, ItemPrice: result.PriceText,
		ItemDetail: string(detailJSON), MultiQuantityDelivery: row.Quantity > 1 || specs.TotalQuantity() > 1, IsMultiSpec: specs.IsMultiSpec(),
	})
	if itemErr != nil {
		return &PostPublishError{Err: fmt.Errorf("保存发布商品信息: %w", itemErr)}
	}
	// ruleErr 保存发布后自动化规则幂等写入错误。
	if ruleErr := service.ensureAutomationRules(ctx, userID, row, result); ruleErr != nil {
		return &PostPublishError{Err: fmt.Errorf("创建发布商品自动化规则: %w", ruleErr)}
	}
	// rawJSON 保存平台原始结果，供批次明细恢复和详情查询使用。
	rawJSON, _ := json.Marshal(result.RawData)
	// marked、markErr 保存当前 worker 的批次成功检查点结果。
	marked, markErr := service.completionRepository.MarkClaimedRowSuccess(ctx, row.ID, workerToken, result.ItemID, result.ItemURL, string(rawJSON))
	if markErr != nil {
		return markErr
	}
	if !marked {
		return ErrBatchLeaseLost
	}
	return nil
}

// EnsureAutomationRules 将发布明细中的自动化配置幂等写入规则端口；兼容测试入口不触碰商品或批次状态。
func (service *BatchLocalPublishService) EnsureAutomationRules(ctx context.Context, userID int64, row BatchRow, result *BatchPublishResult) error {
	if service == nil || service.ruleRepository == nil {
		return errors.New("批量发布规则收口端口未装配")
	}
	if result == nil || result.ItemID == "" {
		return errors.New("发布商品接口未返回结果")
	}
	return service.ensureAutomationRules(ctx, userID, row, result)
}

// batchPublishAutomationConfig 保存批量表格中的发布后自动化配置。
type batchPublishAutomationConfig struct {
	// PaidDelivery 保存付款后自动发货配置。
	PaidDelivery batchPublishCardAutomation `json:"paid_delivery"`
	// ReviewGift 保存评价后赠品配置。
	ReviewGift batchPublishCardAutomation `json:"review_gift"`
	// ReviewRequest 保存超时求评价配置。
	ReviewRequest batchPublishReviewRequest `json:"review_request"`
}

// batchPublishCardAutomation 保存卡密动作开关和动作列表。
type batchPublishCardAutomation struct {
	// Enabled 表示该自动化规则是否启用。
	Enabled bool `json:"enabled"`
	// Actions 保存按顺序发送的卡密动作。
	Actions []batchPublishCardAction `json:"actions"`
}

// batchPublishCardAction 保存单个卡密发送动作的参数。
type batchPublishCardAction struct {
	// CardID 是卡密组标识。
	CardID int64 `json:"card_id"`
	// DeliveryCount 是本动作发送的卡密数量。
	DeliveryCount int `json:"delivery_count"`
	// DelaySeconds 是动作执行前的延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
	// ActionType 是动作类型；空串兼容旧数据，等同发卡。
	ActionType string `json:"action_type"`
	// DeliveryTemplateID 是发货模板动作引用的模板标识。
	DeliveryTemplateID int64 `json:"delivery_template_id"`
	// TemplateBindings 是发货模板变量到卡密组的绑定。
	TemplateBindings []BatchPreviewTemplateBinding `json:"template_bindings"`
	// CustomVariables 是传给发货模板的自定义字符串键值表。
	CustomVariables map[string]string `json:"custom_variables"`
	// SpecName 是动作绑定的规范化规格名称，单规格为空。
	SpecName string `json:"spec_name"`
	// SpecValue 是与 SpecName 同序的规格值，单规格为空。
	SpecValue string `json:"spec_value"`
}

// batchPublishReviewRequest 保存超时求评价规则参数。
type batchPublishReviewRequest struct {
	// Enabled 表示是否启用超时求评价。
	Enabled bool `json:"enabled"`
	// AfterShippedHours 是发货后等待小时数。
	AfterShippedHours int `json:"after_shipped_hours"`
	// Message 是发送给买家的求评价文案。
	Message string `json:"message"`
	// MaxAttempts 是最大重试次数。
	MaxAttempts int `json:"max_attempts"`
	// DelaySeconds 是消息发送前的延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
}

// ensureAutomationRules 将批量配置转换为应用规则并逐条幂等保存。
func (service *BatchLocalPublishService) ensureAutomationRules(ctx context.Context, userID int64, row BatchRow, result *BatchPublishResult) error {
	// config 保存批量明细中的自动化规则配置。
	var config batchPublishAutomationConfig
	// err 表示批量明细自动化配置的 JSON 解析错误。
	if err := json.Unmarshal([]byte(row.AutomationJSON), &config); err != nil {
		return err
	}
	// title 保存规则名称使用的平台标题或导入标题。
	title := firstBatchResultTitle(result.Title, row.Title)
	if config.PaidDelivery.Enabled {
		// actions 保存付款后自动发货规则的动作顺序，发货动作之后固定追加确认发货。
		actions := batchDeliveryActions(config.PaidDelivery.Actions)
		actions = append(actions, automationapp.ActionInput{ActionType: automationapp.ActionConfirmShipment, Enabled: true, SortOrder: len(actions) + 1})
		// err 表示付款后自动发货规则的幂等写入错误。
		if err := service.ruleRepository.EnsurePublishRule(ctx, automationapp.RuleInput{UserID: userID, CookieID: row.CookieID, ItemID: result.ItemID, Name: "付款后自动发货 - " + title, TriggerType: automationapp.TriggerOrderPaid, Enabled: true, Priority: 100, ConfigJSON: "{}", Actions: actions}); err != nil {
			return err
		}
	}
	if config.ReviewGift.Enabled {
		// actions 保存评价后赠品规则的动作顺序。
		actions := batchDeliveryActions(config.ReviewGift.Actions)
		// err 表示评价赠品规则的幂等写入错误。
		if err := service.ruleRepository.EnsurePublishRule(ctx, automationapp.RuleInput{UserID: userID, CookieID: row.CookieID, ItemID: result.ItemID, Name: "评价后发送赠品 - " + title, TriggerType: automationapp.TriggerBuyerReviewed, Enabled: true, Priority: 100, ConfigJSON: "{}", Actions: actions}); err != nil {
			return err
		}
	}
	if config.ReviewRequest.Enabled {
		// configJSON 保存超时求评价规则的小时数与最大尝试次数。
		configJSON, _ := json.Marshal(map[string]any{"after_shipped_hours": config.ReviewRequest.AfterShippedHours, "max_attempts": config.ReviewRequest.MaxAttempts})
		// err 表示超时求评价规则的幂等写入错误。
		if err := service.ruleRepository.EnsurePublishRule(ctx, automationapp.RuleInput{UserID: userID, CookieID: row.CookieID, ItemID: result.ItemID, Name: "超时未评价求评价 - " + title, TriggerType: automationapp.TriggerReviewMissingTimeout, Enabled: true, Priority: 100, ConfigJSON: string(configJSON), Actions: []automationapp.ActionInput{{ActionType: automationapp.ActionSendText, MessageTemplate: config.ReviewRequest.Message, DelaySeconds: config.ReviewRequest.DelaySeconds, Enabled: true, SortOrder: 1}}}); err != nil {
			return err
		}
	}
	return nil
}

// firstBatchResultTitle 选择规则和本地商品使用的首个非空标题。
func firstBatchResultTitle(values ...string) string {
	// value 表示当前候选的商品标题文本。
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}
