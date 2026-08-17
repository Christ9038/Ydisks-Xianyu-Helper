package adapter

import (
	"log/slog"

	"xianyu-go/internal/db"
	"xianyu-go/internal/reconciliation"
)

// SystemDependencies 封装健康检查与订单补偿扫描所需的系统级适配器。
type SystemDependencies struct {
	// store 保存系统适配器共享的数据库入口，仅在 adapter 内部使用。
	store *db.Store
}

// NewSystemDependencies 从数据库 Store 构造系统专用依赖组。
func NewSystemDependencies(store *db.Store) *SystemDependencies {
	if store == nil {
		return nil
	}
	return &SystemDependencies{store: store}
}

// NewDatabaseHealth 创建数据库健康检查适配器。
func (d *SystemDependencies) NewDatabaseHealth() *DatabaseHealth {
	if d == nil {
		return nil
	}
	return NewDatabaseHealth(d.store)
}

// NewReconciliationService 创建外部发货成功后的本地补偿扫描服务。
func (d *SystemDependencies) NewReconciliationService(logger *slog.Logger) *reconciliation.Service {
	if d == nil {
		return nil
	}
	return reconciliation.New(d.store, logger)
}
