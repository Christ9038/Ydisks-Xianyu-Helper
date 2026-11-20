package orders

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/money"
)

var (
	// ErrInvalidPrice 表示请求的改价金额不是大于 0 且最多两位小数的元金额。
	ErrInvalidPrice = errors.New("改价金额必须是大于 0 且最多两位小数的金额")
	// ErrOrderNotAwaitingPayment 表示订单已不处于未付款阶段，不能改价。
	ErrOrderNotAwaitingPayment = errors.New("仅未付款订单可以改价")
	// ErrPriceAdjustUnavailable 表示平台客户端未装配，无法发起改价。
	ErrPriceAdjustUnavailable = errors.New("平台客户端未初始化，无法改价")
	// ErrPriceAdjustRejected 表示平台明确拒绝或调用失败；错误文本携带不含凭证的原因。
	ErrPriceAdjustRejected = errors.New("订单改价失败")
)

// AdjustPriceRequest 描述单个未付款订单改价用例的输入。
type AdjustPriceRequest struct {
	// UserID 是当前登录用户标识，用于订单所属账号的所有权校验。
	UserID int64
	// OrderID 是待改价的闲鱼订单标识。
	OrderID string
	// Price 是改价后的订单总价，单位为元的十进制文本（最多两位小数）；不使用浮点数。
	Price string
}

// AdjustPriceResult 描述改价成功后的订单金额和本地同步状态。
type AdjustPriceResult struct {
	// OrderID 是已改价的订单标识。
	OrderID string
	// Amount 是改价后写入本地订单的金额文本，单位为元，固定两位小数。
	Amount string
	// Status 是结果状态：succeeded 表示远端和本地均完成，local_sync_pending 表示远端已改价而本地金额待同步。
	Status string
	// Message 是面向用户的结果说明。
	Message string
}

// AdjustPriceCallResult 描述平台改价调用结果和账号凭证变化。
type AdjustPriceCallResult struct {
	// Success 表示平台是否确认改价成功。
	Success bool
	// Messages 是平台返回的业务提示列表。
	Messages []string
	// RuntimeCookie 是需要同步到运行时的最新 Cookie；属于敏感数据，不得记录或返回给前端。
	RuntimeCookie string
	// RuntimeCookieChanged 表示 RuntimeCookie 是否有变化。
	RuntimeCookieChanged bool
	// Err 是平台调用或会话处理错误。
	Err error
}

// AdjustPriceRepository 定义订单改价用例所需的最小订单持久化能力。
type AdjustPriceRepository interface {
	// ListOwnedIDs 返回用户拥有的账号标识。
	ListOwnedIDs(ctx context.Context, userID int64) ([]string, error)
	// GetOrder 读取待改价订单。
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	// UpsertOrder 写入平台确认后的本地订单金额。
	UpsertOrder(ctx context.Context, orderID string, options UpsertOptions) error
}

// AdjustPriceRuntime 定义订单改价访问平台和运行时的最小 Port。
type AdjustPriceRuntime interface {
	// MTopAvailable 判断平台客户端是否已装配。
	MTopAvailable() bool
	// AdjustPrice 使用订单所属账号的当前凭证调用平台改价接口，单次用户操作最多对应一次平台改价请求。
	AdjustPrice(ctx context.Context, cookieID, orderID string, userID int64, priceCents int64) AdjustPriceCallResult
	// UpdateRunningCookie 同步运行时账号 Cookie。
	UpdateRunningCookie(ctx context.Context, cookieID, value string)
	// ReportPersistenceFailure 记录本地订单金额持久化失败。
	ReportPersistenceFailure(orderID string, err error)
}

// AdjustPriceService 承载未付款订单改价的所有权、状态门禁、远端动作和本地补偿规则。
type AdjustPriceService struct {
	// repository 保存改价用例所需的订单 Port。
	repository AdjustPriceRepository
	// runtime 保存改价用例所需的平台和运行时 Port。
	runtime AdjustPriceRuntime
}

// NewAdjustPriceService 创建订单改价应用服务；两个依赖在调用时校验，缺失时返回错误而不是 panic。
func NewAdjustPriceService(repository AdjustPriceRepository, runtime AdjustPriceRuntime) *AdjustPriceService {
	return &AdjustPriceService{repository: repository, runtime: runtime}
}

// FormatCents 将整数分格式化为固定两位小数的元金额文本，不经过浮点数。
func FormatCents(cents int64) string {
	return fmt.Sprintf("%d.%02d", cents/100, cents%100)
}

// AdjustPrice 校验订单归属和未付款状态后调用平台改价，并把新金额写回本地订单。
// 本方法最多发起一次平台改价请求；遇到凭证失效等错误直接返回，不在此处重放。
func (s *AdjustPriceService) AdjustPrice(ctx context.Context, request AdjustPriceRequest) (AdjustPriceResult, error) {
	if s == nil || s.repository == nil || s.runtime == nil {
		return AdjustPriceResult{}, errors.New("订单改价依赖未初始化")
	}
	// orderID 是去除空白后的订单标识。
	orderID := strings.TrimSpace(request.OrderID)
	if orderID == "" {
		return AdjustPriceResult{}, ErrNotFound
	}
	// priceCents、parseErr 是严格解析后的整数分和解析错误；零或负数无业务意义。
	priceCents, parseErr := money.ParseYuanToCents(request.Price)
	if parseErr != nil || priceCents <= 0 {
		return AdjustPriceResult{}, ErrInvalidPrice
	}
	// ownedCookieIDs 是当前用户可操作的账号标识集合。
	ownedCookieIDs, err := s.repository.ListOwnedIDs(ctx, request.UserID)
	if err != nil {
		return AdjustPriceResult{}, err
	}
	// order、getErr 是订单读取结果；读取失败与不存在统一视为不存在，避免暴露存储细节。
	order, getErr := s.repository.GetOrder(ctx, orderID)
	if getErr != nil || order == nil {
		return AdjustPriceResult{}, ErrNotFound
	}
	if !containsOwnedCookieID(ownedCookieIDs, order.CookieID) {
		return AdjustPriceResult{}, ErrForbidden
	}
	if NormalizeOrderStatus(strings.TrimSpace(order.OrderStatus)) != "processing" {
		return AdjustPriceResult{}, ErrOrderNotAwaitingPayment
	}
	if !s.runtime.MTopAvailable() {
		return AdjustPriceResult{}, ErrPriceAdjustUnavailable
	}
	// call 是平台改价调用结果。
	call := s.runtime.AdjustPrice(ctx, order.CookieID, orderID, request.UserID, priceCents)
	if call.RuntimeCookieChanged {
		s.runtime.UpdateRunningCookie(ctx, order.CookieID, call.RuntimeCookie)
	}
	if errors.Is(call.Err, ErrForbidden) {
		return AdjustPriceResult{}, ErrForbidden
	}
	if !call.Success {
		return AdjustPriceResult{}, adjustPriceFailure(call)
	}
	return s.persistAdjustedAmount(ctx, order, orderID, priceCents), nil
}

// adjustPriceFailure 把平台失败转换为携带不含凭证原因的 ErrPriceAdjustRejected。
func adjustPriceFailure(call AdjustPriceCallResult) error {
	// reason 是面向用户的失败原因，优先使用调用错误，其次使用平台业务提示。
	reason := ""
	if call.Err != nil {
		reason = call.Err.Error()
	} else if len(call.Messages) > 0 {
		reason = strings.Join(call.Messages, "; ")
	}
	if reason == "" {
		return ErrPriceAdjustRejected
	}
	return fmt.Errorf("%w: %s", ErrPriceAdjustRejected, reason)
}

// persistAdjustedAmount 在平台确认改价后写入本地金额。
// 补偿记录服务只支持确认发货，本地写入失败时不登记补偿，而是告警并提示同步订单：
// 订单同步会从平台读取真实金额，因此不会让本地长期偏离平台，也不会诱导用户重复改价。
func (s *AdjustPriceService) persistAdjustedAmount(ctx context.Context, order *Order, orderID string, priceCents int64) AdjustPriceResult {
	// amount 是改价后的本地金额文本。
	amount := FormatCents(priceCents)
	// upsertErr 是远端成功后本地金额写入错误。
	upsertErr := s.repository.UpsertOrder(ctx, orderID, UpsertOptions{
		CookieID: order.CookieID, OrderStatus: NormalizeOrderStatus(strings.TrimSpace(order.OrderStatus)), ItemID: order.ItemID,
		BuyerID: order.BuyerID, ReceiverName: order.ReceiverName, ReceiverPhone: order.ReceiverPhone,
		ReceiverAddress: order.ReceiverAddress, ReceiverCity: order.ReceiverCity, ChatID: order.ChatID,
		SpecName: order.SpecName, SpecValue: order.SpecValue, Quantity: order.Quantity, Amount: amount,
	})
	if upsertErr == nil {
		return AdjustPriceResult{OrderID: orderID, Amount: amount, Status: "succeeded", Message: "已修改订单价格为 ¥" + amount}
	}
	s.runtime.ReportPersistenceFailure(orderID, upsertErr)
	return AdjustPriceResult{
		OrderID: orderID, Amount: amount, Status: "local_sync_pending",
		Message: "闲鱼已改价为 ¥" + amount + "，但本地订单金额写入失败；请勿重复改价，同步该订单即可校正",
	}
}
