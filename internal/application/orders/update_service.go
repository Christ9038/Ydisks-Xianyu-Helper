package orders

import (
	"context"
	"errors"
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"
)

// UpdateRequest 描述订单更新用例允许修改的字段。
type UpdateRequest struct {
	// OrderStatus 是订单状态补丁。
	OrderStatus *string
	// ItemID 是商品标识补丁。
	ItemID *string
	// BuyerID 是买家标识补丁。
	BuyerID *string
	// SpecName 是规格名称补丁。
	SpecName *string
	// SpecValue 是规格值补丁。
	SpecValue *string
	// Quantity 是购买数量补丁。
	Quantity *string
	// Amount 是订单金额补丁。
	Amount *string
	// ReceiverName 是收货人补丁。
	ReceiverName *string
	// ReceiverPhone 是收货电话补丁。
	ReceiverPhone *string
	// ReceiverAddress 是收货地址补丁。
	ReceiverAddress *string
	// ReceiverCity 是收货城市补丁。
	ReceiverCity *string
	// ChatID 是聊天会话补丁。
	ChatID *string
	// SystemShipped 表示是否由系统完成发货。
	SystemShipped *bool
	// ItemTitle 是关联商品标题补丁。
	ItemTitle *string
}

// UpdateRepository 定义订单更新用例所需的最小持久化能力。
type UpdateRepository interface {
	// ExistsOwned 判断订单绑定的账号是否归属于当前用户。
	ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error)
	// GetOrder 读取待更新的订单实体。
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	// WithTransaction 在单个事务中提交订单及商品更新。
	WithTransaction(ctx context.Context, work func(Writer) error) error
}

// ValidationError 表示请求字段不满足订单业务约束。
type ValidationError struct {
	// Message 是返回给上层适配器的稳定错误信息。
	Message string
}

// Error 返回字段校验错误文本。
func (e *ValidationError) Error() string {
	if e == nil {
		return "订单字段校验失败"
	}
	return e.Message
}

// NewValidationError 创建订单字段校验错误。
func NewValidationError(message string) error {
	return &ValidationError{Message: message}
}

// UpdateService 承载订单更新的所有权、字段校验和事务规则。
type UpdateService struct {
	// repository 保存订单更新用例所需的窄数据访问 Port。
	repository UpdateRepository
}

// NewUpdateService 创建订单更新应用服务。
func NewUpdateService(repository UpdateRepository) *UpdateService {
	return &UpdateService{repository: repository}
}

// Update 校验订单归属并在单事务内更新订单及可选商品标题。
func (s *UpdateService) Update(ctx context.Context, userID int64, orderID string, request UpdateRequest) error {
	if s == nil || s.repository == nil {
		return errors.New("订单更新 repository 未初始化")
	}
	// order 保存待更新订单主体。
	order, err := s.repository.GetOrder(ctx, orderID)
	if err != nil {
		return err
	}
	if order == nil {
		return ErrNotFound
	}
	if strings.TrimSpace(order.CookieID) == "" {
		return ErrForbidden
	}
	// owned 保存订单账号归属校验结果。
	owned, err := s.repository.ExistsOwned(ctx, userID, order.CookieID)
	if err != nil {
		return err
	}
	if !owned {
		return ErrForbidden
	}
	// err 保存更新请求字段校验错误。
	if err := normalizeUpdateRequest(&request); err != nil {
		return err
	}
	// finalItemID 保存更新后订单关联的商品标识。
	finalItemID := strings.TrimSpace(order.ItemID)
	if request.ItemID != nil {
		finalItemID = strings.TrimSpace(*request.ItemID)
		request.ItemID = &finalItemID
	}
	// itemTitle 保存规范化后的商品标题。
	itemTitle := ""
	if request.ItemTitle != nil {
		itemTitle = strings.TrimSpace(*request.ItemTitle)
		if itemTitle == "" || finalItemID == "" {
			return NewValidationError("商品标题不能为空且订单必须关联商品")
		}
		request.ItemTitle = &itemTitle
	}
	return s.repository.WithTransaction(ctx, func(writer Writer) error {
		// patch 保存订单字段更新集合。
		patch := OrderPatch{
			OrderStatus: request.OrderStatus, ItemID: request.ItemID, BuyerID: request.BuyerID,
			SpecName: request.SpecName, SpecValue: request.SpecValue, Quantity: request.Quantity,
			Amount: request.Amount, ReceiverName: request.ReceiverName, ReceiverPhone: request.ReceiverPhone,
			ReceiverAddress: request.ReceiverAddress, ReceiverCity: request.ReceiverCity, ChatID: request.ChatID,
			SystemShipped: request.SystemShipped,
		}
		// err 保存订单补丁写入错误。
		if err := writer.PatchOrder(ctx, orderID, patch); err != nil {
			return err
		}
		if request.ItemTitle == nil {
			return nil
		}
		// err 保存商品标题写入错误。
		if err := writer.UpsertItemBasic(ctx, ItemWrite{CookieID: order.CookieID, ItemID: finalItemID, ItemTitle: itemTitle}); err != nil {
			return fmt.Errorf("更新商品标题失败: %w", err)
		}
		return nil
	})
}

// normalizeUpdateRequest 规范化状态和金额，并拒绝不合法的更新字段。
func normalizeUpdateRequest(request *UpdateRequest) error {
	if request == nil {
		return NewValidationError("订单更新请求不能为空")
	}
	if request.OrderStatus != nil {
		// normalized 保存规范化后的订单状态。
		normalized := NormalizeOrderStatus(strings.TrimSpace(*request.OrderStatus))
		if !ValidEditableOrderStatus(normalized) {
			return NewValidationError("不支持的订单状态")
		}
		request.OrderStatus = &normalized
	}
	if request.Amount != nil {
		// normalized 保存规范化后的订单金额。
		normalized, ok := NormalizeOrderAmount(*request.Amount)
		if !ok {
			return NewValidationError("订单金额必须是普通格式的非负有限数字")
		}
		request.Amount = &normalized
	}
	return nil
}

// orderAmountPattern 匹配不带千位分隔符的非负金额。
var orderAmountPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+)?$`)

// groupedOrderAmountPattern 匹配带千位分隔符的非负金额。
var groupedOrderAmountPattern = regexp.MustCompile(`^[0-9]{1,3}(?:,[0-9]{3})+(?:\.[0-9]+)?$`)

// NormalizeOrderAmount 把货币符号和千位分隔符规范为十进制金额文本。
func NormalizeOrderAmount(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if strings.HasPrefix(raw, "¥") {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "¥"))
	} else if strings.HasPrefix(raw, "￥") {
		raw = strings.TrimSpace(strings.TrimPrefix(raw, "￥"))
	}
	if raw == "" {
		return "", true
	}
	if strings.Contains(raw, ",") {
		if !groupedOrderAmountPattern.MatchString(raw) {
			return "", false
		}
		raw = strings.ReplaceAll(raw, ",", "")
	} else if !orderAmountPattern.MatchString(raw) {
		return "", false
	}
	// value 保存解析后的金额数值，用于拒绝 NaN 和无穷大。
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
		return "", false
	}
	return raw, true
}

// NormalizeOrderStatus 将平台数字状态码归一为订单文本状态。
func NormalizeOrderStatus(status string) string {
	switch status {
	case "paid":
		return "pending_ship"
	case "1":
		return "processing"
	case "2":
		return "pending_ship"
	case "3":
		return "shipped"
	case "4", "11":
		return "completed"
	case "5", "7", "9":
		return "refunding"
	case "6", "8", "10", "12":
		return "cancelled"
	case "":
		return "unknown"
	default:
		return status
	}
}

// ValidEditableOrderStatus 判断订单状态是否允许通过更新接口修改。
func ValidEditableOrderStatus(status string) bool {
	switch status {
	case "processing", "pending_ship", "shipped", "completed", "cancelled", "refunding":
		return true
	default:
		return false
	}
}
