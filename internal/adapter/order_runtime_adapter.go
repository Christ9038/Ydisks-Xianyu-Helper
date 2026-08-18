package adapter

import (
	"context"
	"errors"
	"log/slog"

	"xianyu-go/internal/account"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/notify"
)

// OrderRuntimeAdapter 将账号、自动化、通知和平台调用组合为订单应用 Port，不依赖 HTTP Server。
type OrderRuntimeAdapter struct {
	// runtime 保存已完成基础设施装配的订单运行时；nil 仅用于隔离测试的缺失依赖分支。
	runtime *OrderRuntime
}

// OrderRuntimeFactory 定义订单运行时装配所需的最小基础设施工厂，由组合根提供具体实现。
type OrderRuntimeFactory interface {
	// NewOrderRuntime 使用已装配 hooks 与补偿记录端口创建订单运行时。
	NewOrderRuntime(OrderRuntimeHooks, orderapp.ReconciliationRecorder, *slog.Logger) *OrderRuntime
}

// NewOrderRuntimeAdapter 构造订单应用运行时 Port；所有外部能力仅通过组合期回调和窄接口进入。
func NewOrderRuntimeAdapter(dependencies OrderRuntimeFactory, manager *account.Manager, autoCenter *automation.Center, notifier *notify.Notifier, mtopClient func() MTOPClient, updateRunningCookie func(context.Context, string, string), sessionRecovery SessionRecoveryHandler, logger *slog.Logger, reconciliation orderapp.ReconciliationRecorder) OrderRuntimeAdapter {
	// automationPort 保持 nil 接口语义，避免 nil 指针伪装成已装配自动化能力。
	var automationPort OrderAutomation
	if autoCenter != nil {
		automationPort = autoCenter
	}
	// notifierPort 保持 nil 接口语义，避免 nil 指针伪装成已装配通知能力。
	var notifierPort OrderNotifier
	if notifier != nil {
		notifierPort = notifier
	}
	// hooks 收口账号、自动化、通知、平台和凭证恢复能力，订单应用不接触 HTTP transport。
	hooks := NewOrderRuntimeHooks(mtopClient, manager, automationPort, notifierPort, updateRunningCookie, sessionRecovery)
	// runtime 保存由 adapter 创建的订单运行时；无数据库依赖时仅支持确定性错误分支。
	var runtime *OrderRuntime
	if dependencies == nil {
		runtime = NewOrderRuntime(nil, hooks, reconciliation, logger)
	} else {
		runtime = dependencies.NewOrderRuntime(hooks, reconciliation, logger)
	}
	return OrderRuntimeAdapter{runtime: runtime}
}

// AccountRunning 判断指定账号是否在线运行。
func (adapter OrderRuntimeAdapter) AccountRunning(cookieID string) bool {
	return adapter.runtime != nil && adapter.runtime.AccountRunning(cookieID)
}

// AutomationReady 判断完整发货自动化依赖是否已装配。
func (adapter OrderRuntimeAdapter) AutomationReady() bool {
	return adapter.runtime != nil && adapter.runtime.AutomationReady()
}

// ManualFullDelivery 执行完整自动化发货。
func (adapter OrderRuntimeAdapter) ManualFullDelivery(ctx context.Context, order *orderapp.Order) (int, error) {
	if adapter.runtime == nil {
		return 0, errors.New("订单运行时未初始化")
	}
	return adapter.runtime.ManualFullDelivery(ctx, order)
}

// MTopAvailable 判断平台客户端是否已注入。
func (adapter OrderRuntimeAdapter) MTopAvailable() bool {
	return adapter.runtime != nil && adapter.runtime.MTopAvailable()
}

// ConfirmShipment 执行平台确认发货并返回应用层结果。
func (adapter OrderRuntimeAdapter) ConfirmShipment(ctx context.Context, cookieID, orderID string, userID int64) orderapp.ConsignResult {
	if adapter.runtime == nil {
		return orderapp.ConsignResult{Err: errors.New("订单运行时未初始化")}
	}
	return adapter.runtime.ConfirmShipment(ctx, cookieID, orderID, userID)
}

// UpdateRunningCookie 同步运行时账号 Cookie。
func (adapter OrderRuntimeAdapter) UpdateRunningCookie(ctx context.Context, cookieID, value string) {
	if adapter.runtime != nil {
		adapter.runtime.UpdateRunningCookie(ctx, cookieID, value)
	}
}

// NotifyDelivery 发送发货结果通知。
func (adapter OrderRuntimeAdapter) NotifyDelivery(cookieID, buyerID, itemID, chatID, message string) {
	if adapter.runtime != nil {
		adapter.runtime.NotifyDelivery(cookieID, buyerID, itemID, chatID, message)
	}
}

// RecordOrderReconciliation 写入外部动作成功后的本地异常补偿记录。
func (adapter OrderRuntimeAdapter) RecordOrderReconciliation(ctx context.Context, orderID, cookieID, kind, message string) (string, error) {
	if adapter.runtime == nil {
		return "", errors.New("订单运行时未初始化")
	}
	return adapter.runtime.RecordOrderReconciliation(ctx, orderID, cookieID, kind, message)
}

// RecordReconciliation 满足手动发货应用 Port 的补偿记录接口。
func (adapter OrderRuntimeAdapter) RecordReconciliation(ctx context.Context, orderID, cookieID, kind, message string) (string, error) {
	return adapter.RecordOrderReconciliation(ctx, orderID, cookieID, kind, message)
}

// ReportPersistenceFailure 记录手动发货本地状态持久化失败。
func (adapter OrderRuntimeAdapter) ReportPersistenceFailure(orderID string, err error) {
	if adapter.runtime != nil {
		adapter.runtime.ReportPersistenceFailure(orderID, err)
	}
}

// RecoverExpiredSession 将订单刷新发现的会话失效交给组合期恢复回调。
func (adapter OrderRuntimeAdapter) RecoverExpiredSession(ctx context.Context, cookieID string, err error) bool {
	return adapter.runtime != nil && adapter.runtime.RecoverExpiredSession(ctx, cookieID, err)
}

// DetailAvailable 判断订单详情接口是否可用。
func (adapter OrderRuntimeAdapter) DetailAvailable() bool {
	return adapter.runtime != nil && adapter.runtime.DetailAvailable()
}

// SoldAvailable 判断已售订单列表接口是否可用。
func (adapter OrderRuntimeAdapter) SoldAvailable() bool {
	return adapter.runtime != nil && adapter.runtime.SoldAvailable()
}

// CredentialAvailable 判断平台请求视图是否包含可用 Cookie。
func (adapter OrderRuntimeAdapter) CredentialAvailable(detail *orderapp.PlatformRuntimeData) bool {
	return adapter.runtime != nil && adapter.runtime.CredentialAvailable(detail)
}

// FetchOrderDetail 调用平台详情接口并收集 Cookie 会话变化。
func (adapter OrderRuntimeAdapter) FetchOrderDetail(ctx context.Context, detail *orderapp.PlatformRuntimeData, orderID string) (orderapp.RefreshDetailFetchResult, error) {
	if adapter.runtime == nil {
		return orderapp.RefreshDetailFetchResult{}, errors.New("订单运行时未初始化")
	}
	return adapter.runtime.FetchOrderDetail(ctx, detail, orderID)
}

// FetchSoldOrders 调用平台已售订单接口并收集 Cookie 会话变化。
func (adapter OrderRuntimeAdapter) FetchSoldOrders(ctx context.Context, detail *orderapp.PlatformRuntimeData) (orderapp.RefreshSoldFetchResult, error) {
	if adapter.runtime == nil {
		return orderapp.RefreshSoldFetchResult{}, errors.New("订单运行时未初始化")
	}
	return adapter.runtime.FetchSoldOrders(ctx, detail)
}

// PersistCookieSession 在凭证锁内保存应用层 Cookie 更新。
func (adapter OrderRuntimeAdapter) PersistCookieSession(ctx context.Context, detail *orderapp.PlatformRuntimeData, update orderapp.RefreshCookieUpdate) (string, bool, bool, error) {
	if adapter.runtime == nil {
		if detail == nil {
			return update.Value, false, update.Handled, errors.New("订单运行时未初始化")
		}
		return update.Value, update.Value != detail.Value, update.Handled, errors.New("订单运行时未初始化")
	}
	return adapter.runtime.PersistCookieSession(ctx, detail, update)
}

// IsSessionExpired 判断平台错误是否为会话过期。
func (adapter OrderRuntimeAdapter) IsSessionExpired(err error) bool {
	return adapter.runtime != nil && adapter.runtime.IsSessionExpired(err)
}
