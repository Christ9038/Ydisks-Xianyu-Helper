package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"
)

// ImportOrder 描述已经由文件/HTTP 适配层解析的订单导入行。
type ImportOrder struct {
	// OrderID 是订单业务标识。
	OrderID string
	// CookieID 是订单所属账号标识。
	CookieID string
	// ItemID 是关联商品标识。
	ItemID string
	// ItemTitle 是商品标题。
	ItemTitle string
	// ItemPrice 是商品价格文本。
	ItemPrice string
	// ItemDetail 是商品详情 JSON 或描述文本。
	ItemDetail string
	// BuyerID 是买家标识。
	BuyerID string
	// OrderStatus 是外部订单状态文本或数字码。
	OrderStatus string
	// SpecName 是规格名称。
	SpecName string
	// SpecValue 是规格值。
	SpecValue string
	// Quantity 是购买数量文本。
	Quantity string
	// Amount 是订单金额文本。
	Amount string
	// ReceiverName 是收货人姓名。
	ReceiverName string
	// ReceiverPhone 是收货人电话。
	ReceiverPhone string
	// ReceiverAddress 是收货地址。
	ReceiverAddress string
	// ReceiverCity 是收货城市。
	ReceiverCity string
	// ChatID 是聊天会话标识。
	ChatID string
}

// ImportItemResult 描述单条订单导入结果。
type ImportItemResult struct {
	// OrderID 是导入结果对应的订单标识。
	OrderID string
	// Success 表示该订单是否导入成功。
	Success bool
	// Message 是成功或失败说明。
	Message string
}

// ImportResult 描述批量订单导入统计及逐单结果。
type ImportResult struct {
	// Total 是本次导入订单总数。
	Total int
	// SuccessCount 是成功导入数量。
	SuccessCount int
	// FailedCount 是失败导入数量。
	FailedCount int
	// Results 是逐订单导入结果。
	Results []ImportItemResult
}

// ImportRepository 定义订单导入用例所需的最小持久化能力。
type ImportRepository interface {
	// ListOwnedIDs 返回用户拥有的账号标识。
	ListOwnedIDs(ctx context.Context, userID int64) ([]string, error)
	// WithTransaction 在单条订单导入中执行原子写入。
	WithTransaction(ctx context.Context, work func(Writer) error) error
}

// ImportService 承载订单导入的归属、字段校验和事务写入规则。
type ImportService struct {
	// repository 保存订单导入用例所需的窄数据访问 Port。
	repository ImportRepository
}

// NewImportService 创建订单导入应用服务。
func NewImportService(repository ImportRepository) *ImportService {
	return &ImportService{repository: repository}
}

// Import 按当前用户账号所有权逐单导入订单，并补全关联商品基础信息。
func (s *ImportService) Import(ctx context.Context, userID int64, inputs []ImportOrder) (ImportResult, error) {
	if s == nil || s.repository == nil {
		return ImportResult{}, errors.New("订单导入 repository 未初始化")
	}
	// ownedCookieIDs 保存当前用户可操作的账号标识。
	ownedCookieIDs, err := s.repository.ListOwnedIDs(ctx, userID)
	if err != nil {
		return ImportResult{}, err
	}
	// defaultCookieID 保存仅有一个账号时使用的默认账号标识。
	defaultCookieID := ""
	if len(ownedCookieIDs) == 1 {
		defaultCookieID = ownedCookieIDs[0]
	}
	// result 保存批量导入统计和逐条结果。
	result := ImportResult{Total: len(inputs), Results: make([]ImportItemResult, 0, len(inputs))}
	// input 保存当前处理的订单导入行。
	for _, input := range inputs {
		// err 保存当前订单导入错误。
		if err := s.importOne(ctx, ownedCookieIDs, defaultCookieID, input); err != nil {
			result.FailedCount++
			result.Results = append(result.Results, ImportItemResult{OrderID: importResultOrderID(input), Message: err.Error()})
			continue
		}
		result.SuccessCount++
		result.Results = append(result.Results, ImportItemResult{OrderID: input.OrderID, Success: true, Message: "订单已导入"})
	}
	return result, nil
}

// importOne 在独立事务中写入一条订单及其商品信息。
func (s *ImportService) importOne(ctx context.Context, ownedCookieIDs []string, defaultCookieID string, input ImportOrder) error {
	// orderID 保存规范化后的订单标识。
	orderID := strings.TrimSpace(input.OrderID)
	if orderID == "" {
		return errors.New("缺少必需字段: order_id")
	}
	// cookieID 保存解析后的订单所属账号标识。
	cookieID := strings.TrimSpace(input.CookieID)
	if cookieID == "" {
		cookieID = defaultCookieID
	}
	if cookieID == "" {
		return errors.New("缺少必需字段: cookie_id")
	}
	if !containsOwnedCookieID(ownedCookieIDs, cookieID) {
		return errors.New("无权操作此账号的订单")
	}
	// status 保存规范化后的订单状态。
	status := strings.TrimSpace(input.OrderStatus)
	if status != "" {
		status = NormalizeOrderStatus(status)
		if !ValidEditableOrderStatus(status) {
			return errors.New("不支持的订单状态")
		}
	}
	// amount 保存规范化后的订单金额。
	amount, ok := NormalizeOrderAmount(input.Amount)
	if !ok {
		return errors.New("订单金额必须是普通格式的非负有限数字")
	}
	// err 保存订单事务执行错误。
	if err := s.repository.WithTransaction(ctx, func(writer Writer) error {
		// err 保存订单主体写入错误。
		if err := writer.UpsertOrder(ctx, orderID, UpsertOptions{
			CookieID: cookieID, ItemID: input.ItemID, BuyerID: input.BuyerID,
			OrderStatus: status, SpecName: input.SpecName, SpecValue: input.SpecValue,
			Quantity: input.Quantity, Amount: amount, ReceiverName: input.ReceiverName,
			ReceiverPhone: input.ReceiverPhone, ReceiverAddress: input.ReceiverAddress,
			ReceiverCity: input.ReceiverCity, ChatID: input.ChatID,
		}); err != nil {
			return err
		}
		if input.ItemID == "" {
			return nil
		}
		// err 保存商品基础信息写入错误。
		if err := writer.UpsertItemBasic(ctx, ItemWrite{
			CookieID: cookieID, ItemID: input.ItemID, ItemTitle: input.ItemTitle,
			ItemPrice: input.ItemPrice, ItemDetail: input.ItemDetail,
		}); err != nil {
			return fmt.Errorf("补全商品信息失败: %w", err)
		}
		return nil
	}); err != nil {
		return errors.New("订单导入事务失败: " + err.Error())
	}
	return nil
}

// containsOwnedCookieID 判断账号标识是否在当前用户的授权集合中。
func containsOwnedCookieID(cookieIDs []string, target string) bool {
	// cookieID 表示当前遍历的账号标识。
	for _, cookieID := range cookieIDs {
		if cookieID == target {
			return true
		}
	}
	return false
}

// importResultOrderID 返回结果行使用的稳定订单标识。
func importResultOrderID(input ImportOrder) string {
	// orderID 保存导入结果使用的订单标识。
	orderID := strings.TrimSpace(input.OrderID)
	if orderID == "" {
		return "unknown"
	}
	return orderID
}
