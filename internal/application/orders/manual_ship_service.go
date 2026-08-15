package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ManualShipRequest 描述批量手动发货用例的输入。
type ManualShipRequest struct {
	// UserID 是当前登录用户标识。
	UserID int64
	// OrderIDs 是待处理订单标识列表。
	OrderIDs []string
	// ShipMode 是发货模式，支持 status_only 和 full_delivery。
	ShipMode string
}

// ManualShipItemResult 描述单个订单的手动发货结果。
type ManualShipItemResult struct {
	// OrderID 是结果对应的订单标识。
	OrderID string
	// Status 是结果状态，如 succeeded、failed 或 reconciliation_required。
	Status string
	// Success 表示远端动作或完整自动化动作是否成功。
	Success bool
	// Message 是面向用户的结果说明。
	Message string
	// ReconciliationID 是本地补偿记录标识。
	ReconciliationID string
	// ReconciliationWarning 是补偿或本地状态写入警告。
	ReconciliationWarning string
	// ReconciliationFieldsPresent 表示兼容响应是否应包含补偿字段。
	ReconciliationFieldsPresent bool
}

// ManualShipResult 描述批量手动发货统计及逐单结果。
type ManualShipResult struct {
	// SuccessCount 是成功处理数量。
	SuccessCount int
	// FailedCount 是失败处理数量。
	FailedCount int
	// Results 是逐订单结果。
	Results []ManualShipItemResult
}

// ConsignResult 描述远端确认发货接口的结果和凭证变化。
type ConsignResult struct {
	// Success 表示远端是否确认发货成功。
	Success bool
	// Messages 是远端返回的业务提示列表。
	Messages []string
	// RuntimeCookie 是需要同步到运行时的最新 Cookie。
	RuntimeCookie string
	// RuntimeCookieChanged 表示 RuntimeCookie 是否有变化。
	RuntimeCookieChanged bool
	// Err 是远端调用或会话处理错误。
	Err error
}

// ManualShipRepository 定义手动发货用例所需的最小订单持久化能力。
type ManualShipRepository interface {
	// ListOwnedIDs 返回用户拥有的账号标识。
	ListOwnedIDs(ctx context.Context, userID int64) ([]string, error)
	// GetOrder 读取待发货订单。
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	// UpsertOrder 写入远端确认后的本地订单状态。
	UpsertOrder(ctx context.Context, orderID string, options UpsertOptions) error
}

// ManualShipRuntime 定义手动发货访问平台、自动化、运行时和补偿能力的最小 Port。
type ManualShipRuntime interface {
	// MTopAvailable 判断平台客户端是否已装配。
	MTopAvailable() bool
	// AutomationReady 判断完整发货自动化依赖是否已装配。
	AutomationReady() bool
	// AccountRunning 判断订单所属账号运行时是否在线。
	AccountRunning(cookieID string) bool
	// ManualFullDelivery 执行完整自动化发货。
	ManualFullDelivery(ctx context.Context, order *Order) (int, error)
	// ConfirmShipment 调用平台确认发货接口。
	ConfirmShipment(ctx context.Context, cookieID, orderID string, userID int64) ConsignResult
	// UpdateRunningCookie 同步运行时账号 Cookie。
	UpdateRunningCookie(ctx context.Context, cookieID, value string)
	// NotifyDelivery 发送手动发货结果通知。
	NotifyDelivery(cookieID, buyerID, itemID, chatID, message string)
	// RecordReconciliation 创建外部成功、本地状态待补偿的记录。
	RecordReconciliation(ctx context.Context, orderID, cookieID, kind, message string) (string, error)
	// ReportPersistenceFailure 记录本地订单状态持久化失败。
	ReportPersistenceFailure(orderID string, err error)
}

// ManualShipService 承载手动发货的所有权、远端动作和本地补偿规则。
type ManualShipService struct {
	// repository 保存手动发货用例所需的订单 Port。
	repository ManualShipRepository
	// runtime 保存手动发货用例所需的平台和运行时 Port。
	runtime ManualShipRuntime
}

// NewManualShipService 创建手动发货应用服务。
func NewManualShipService(repository ManualShipRepository, runtime ManualShipRuntime) *ManualShipService {
	return &ManualShipService{repository: repository, runtime: runtime}
}

// ManualShip 按订单逐条执行状态确认或完整自动化发货。
func (s *ManualShipService) ManualShip(ctx context.Context, request ManualShipRequest) (ManualShipResult, error) {
	if s == nil || s.repository == nil || s.runtime == nil {
		return ManualShipResult{}, errors.New("手动发货依赖未初始化")
	}
	// ownedCookieIDs 保存当前用户可操作的账号标识。
	ownedCookieIDs, err := s.repository.ListOwnedIDs(ctx, request.UserID)
	if err != nil {
		return ManualShipResult{}, err
	}
	// result 保存批量手动发货统计和逐条结果。
	result := ManualShipResult{Results: make([]ManualShipItemResult, 0, len(request.OrderIDs))}
	// rawOrderID 表示当前遍历中的原始订单标识。
	for _, rawOrderID := range request.OrderIDs {
		// orderID 保存去除空白后的订单标识。
		orderID := strings.TrimSpace(rawOrderID)
		if orderID == "" {
			continue
		}
		// order、getErr 保存订单读取结果及错误。
		order, getErr := s.repository.GetOrder(ctx, orderID)
		if getErr != nil || order == nil {
			s.appendFailure(&result, orderID, "订单不存在")
			continue
		}
		if !containsOwnedCookieID(ownedCookieIDs, order.CookieID) {
			s.appendFailure(&result, orderID, "无权操作此订单")
			continue
		}
		if NormalizeOrderStatus(strings.TrimSpace(order.OrderStatus)) != "pending_ship" {
			s.appendFailure(&result, orderID, "仅待发货订单可以执行手动发货")
			continue
		}
		if request.ShipMode == "full_delivery" {
			s.manualFullDelivery(ctx, order, orderID, &result)
			continue
		}
		s.manualStatusShip(ctx, request.UserID, order, orderID, &result)
	}
	return result, nil
}

// appendFailure 追加一条手动发货失败记录并更新失败计数。
func (s *ManualShipService) appendFailure(result *ManualShipResult, orderID, message string) {
	result.FailedCount++
	result.Results = append(result.Results, ManualShipItemResult{OrderID: orderID, Status: "failed", Message: message})
}

// manualFullDelivery 执行完整自动化发货分支。
func (s *ManualShipService) manualFullDelivery(ctx context.Context, order *Order, orderID string, result *ManualShipResult) {
	if !s.runtime.AutomationReady() {
		s.appendFailure(result, orderID, "自动化中心未初始化")
		return
	}
	if !s.runtime.AccountRunning(order.CookieID) {
		s.appendFailure(result, orderID, "该账号未在线运行，无法执行完整发货")
		return
	}
	// sent、err 保存完整自动化发货结果和错误。
	sent, err := s.runtime.ManualFullDelivery(ctx, order)
	if err != nil {
		s.appendFailure(result, orderID, err.Error())
		s.runtime.NotifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, "手动完整发货失败: "+err.Error())
		return
	}
	result.SuccessCount++
	result.Results = append(result.Results, ManualShipItemResult{OrderID: orderID, Status: "succeeded", Success: true, Message: fmt.Sprintf("完整发货成功，已发送%d条卡券信息给买家", sent)})
	s.runtime.NotifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, fmt.Sprintf("手动完整发货成功（订单 %s，已发送 %d 条）", orderID, sent))
}

// manualStatusShip 调用平台确认发货并把成功状态写入本地订单。
func (s *ManualShipService) manualStatusShip(ctx context.Context, userID int64, order *Order, orderID string, result *ManualShipResult) {
	if !s.runtime.MTopAvailable() {
		s.appendFailure(result, orderID, "mtop 客户端未初始化")
		return
	}
	// consign 保存远端确认发货结果。
	consign := s.runtime.ConfirmShipment(ctx, order.CookieID, orderID, userID)
	if consign.RuntimeCookieChanged {
		s.runtime.UpdateRunningCookie(ctx, order.CookieID, consign.RuntimeCookie)
	}
	if consign.Err != nil && !consign.Success {
		s.appendFailure(result, orderID, "确认发货异常: "+consign.Err.Error())
		s.runtime.NotifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, "手动确认发货异常: "+consign.Err.Error())
		return
	}
	if !consign.Success {
		// message 保存远端确认发货失败说明。
		message := "确认发货失败"
		if len(consign.Messages) > 0 {
			message += ": " + strings.Join(consign.Messages, "; ")
		}
		s.appendFailure(result, orderID, message)
		s.runtime.NotifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, "手动确认发货失败: "+message)
		return
	}
	// systemShipped 标识本地订单已由系统确认发货。
	systemShipped := true
	// upsertErr 保存远端成功后本地订单写入错误。
	upsertErr := s.repository.UpsertOrder(ctx, orderID, UpsertOptions{
		CookieID: order.CookieID, OrderStatus: "shipped", SystemShipped: &systemShipped, ItemID: order.ItemID,
		BuyerID: order.BuyerID, ReceiverName: order.ReceiverName, ReceiverPhone: order.ReceiverPhone,
		ReceiverAddress: order.ReceiverAddress, ReceiverCity: order.ReceiverCity, ChatID: order.ChatID,
		SpecName: order.SpecName, SpecValue: order.SpecValue, Quantity: order.Quantity, Amount: order.Amount,
	})
	if upsertErr != nil {
		s.runtime.ReportPersistenceFailure(orderID, upsertErr)
	}
	result.SuccessCount++
	// status 表示远端动作与本地状态同步的三态结果。
	status := "succeeded"
	// message 保存手动确认发货结果说明。
	message := "已成功修改闲鱼发货状态"
	// reconciliationID 保存待补偿记录标识。
	reconciliationID := ""
	// reconciliationWarning 保存本地状态写入或补偿记录写入错误。
	reconciliationWarning := ""
	if upsertErr != nil {
		status = "reconciliation_required"
		message = "闲鱼已确认发货；本地订单状态待补偿，请勿重复确认发货"
		reconciliationWarning = upsertErr.Error()
		// recordCtx、recordCancel 保存补偿写入使用的短时独立上下文。
		recordCtx, recordCancel := context.WithTimeout(context.Background(), 5*time.Second)
		// recordErr 保存补偿记录写入错误。
		var recordErr error
		reconciliationID, recordErr = s.runtime.RecordReconciliation(recordCtx, orderID, order.CookieID, "manual_status_ship", upsertErr.Error())
		recordCancel()
		if recordErr != nil {
			reconciliationWarning = errors.Join(errors.New(reconciliationWarning), fmt.Errorf("写入补偿记录失败: %w", recordErr)).Error()
		}
	}
	result.Results = append(result.Results, ManualShipItemResult{
		OrderID: orderID, Status: status, Success: true, Message: message,
		ReconciliationID: reconciliationID, ReconciliationWarning: reconciliationWarning, ReconciliationFieldsPresent: true,
	})
	s.runtime.NotifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, fmt.Sprintf("手动确认发货成功（订单 %s）", orderID))
}
