package adapter

import (
	"errors"
	"log/slog"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// OrderDependencies 只封装订单用例所需的持久化与运行时适配器构造，避免订单装配依赖万能设施容器。
type OrderDependencies struct {
	// store 是订单适配器共享的数据库入口，仅在 adapter 内部用于创建窄接口实现。
	store *db.Store
}

// NewOrderDependencies 构造订单专用依赖并拒绝缺失数据库入口。
func NewOrderDependencies(store *db.Store) (*OrderDependencies, error) {
	if store == nil {
		return nil, errors.New("订单依赖 Store 不能为空")
	}
	return &OrderDependencies{store: store}, nil
}

// NewOrderRepository 创建订单读写仓储，供订单应用服务共享同一适配器实例。
func (d *OrderDependencies) NewOrderRepository() *OrderRepository {
	if d == nil {
		return nil
	}
	return NewOrderRepository(d.store)
}

// NewOrderReconciliationRepository 创建订单补偿记录仓储，供跨外部动作的异常收口使用。
func (d *OrderDependencies) NewOrderReconciliationRepository() *OrderReconciliationRepository {
	if d == nil {
		return nil
	}
	return NewOrderReconciliationRepository(d.store)
}

// NewOrderRuntime 创建订单平台运行时，调用方通过 hooks 注入账号、自动化和通知能力。
func (d *OrderDependencies) NewOrderRuntime(hooks OrderRuntimeHooks, reconciliation orderapp.ReconciliationRecorder, logger *slog.Logger) *OrderRuntime {
	if d == nil {
		return nil
	}
	return NewOrderRuntime(d.store, hooks, reconciliation, logger)
}

// NewOrderRefreshJobRepository 创建订单刷新任务仓储，隐藏任务租约与状态的持久化模型。
func (d *OrderDependencies) NewOrderRefreshJobRepository() orderapp.RefreshJobRepository {
	if d == nil {
		return nil
	}
	return NewOrderRefreshJobRepository(d.store)
}
