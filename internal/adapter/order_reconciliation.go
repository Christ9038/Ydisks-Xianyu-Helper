package adapter

import (
	"context"
	"errors"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// OrderReconciliationRepository 将订单应用层补偿 Port 适配为数据库写入。
// 数据库聚合仅在本适配器内部可见，订单应用服务和 Server 运行时不直接访问它。
type OrderReconciliationRepository struct {
	// store 保存补偿记录使用的数据库聚合入口。
	store *db.Store
}

// NewOrderReconciliationRepository 创建订单补偿记录数据库适配器。
// 缺少 Store 或补偿仓储时返回 nil，由调用方在运行时边界报告装配错误。
func NewOrderReconciliationRepository(store *db.Store) *OrderReconciliationRepository {
	if store == nil || store.Reconciliations == nil {
		return nil
	}
	return &OrderReconciliationRepository{store: store}
}

// RecordReconciliation 创建外部动作成功、本地状态待补偿的记录。
func (r *OrderReconciliationRepository) RecordReconciliation(ctx context.Context, orderID, cookieID, kind, message string) (string, error) {
	if r == nil || r.store == nil || r.store.Reconciliations == nil {
		return "", errors.New("订单补偿存储未初始化")
	}
	return r.store.Reconciliations.CreatePending(ctx, orderID, cookieID, kind, message)
}

// 确保数据库适配器覆盖订单应用层声明的补偿 Port。
var _ orderapp.ReconciliationRecorder = (*OrderReconciliationRepository)(nil)
