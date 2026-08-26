package db

import (
	"context"
	"database/sql"
	"errors"
)

// OrderWriteUnitOfWork 负责订单与商品基础信息跨仓储写入的事务边界。
// 它只在 db 层持有 *sql.DB，应用层和基础设施适配器只能通过业务写入方法参与事务。
type OrderWriteUnitOfWork struct {
	// database 保存创建订单写入事务的底层连接池，不向 db 包外暴露。
	database *sql.DB
	// orders 保存当前事务可调用的订单仓储。
	orders *Orders
	// items 保存当前事务可调用的商品仓储。
	items *Items
}

// OrderWriteTransaction 表示一次订单写入事务内允许的窄操作集合。
// transaction 只在 UnitOfWork 回调存活期间有效，回调返回后不得继续持有或并发使用。
type OrderWriteTransaction struct {
	// transaction 保存实际 SQL 事务；仅由 db 包中的窄写入方法使用。
	transaction *sql.Tx
	// orders 保存订单写入仓储。
	orders *Orders
	// items 保存商品写入仓储。
	items *Items
}

// newOrderWriteUnitOfWork 使用同一数据库连接池和两个窄仓储构造订单写入事务实现。
// database、orders 或 items 缺失时仍返回对象，由 WithTransaction 返回明确初始化错误。
func newOrderWriteUnitOfWork(database *sql.DB, orders *Orders, items *Items) *OrderWriteUnitOfWork {
	return &OrderWriteUnitOfWork{database: database, orders: orders, items: items}
}

// WithTransaction 在单个数据库事务中执行 work，并根据其返回值提交或回滚。
// ctx 决定建事务和所有 SQL 操作的取消；work 返回错误、提交失败或依赖未初始化时调用方会收到错误。
func (unit *OrderWriteUnitOfWork) WithTransaction(ctx context.Context, work func(*OrderWriteTransaction) error) error {
	if unit == nil || unit.database == nil || unit.orders == nil || unit.items == nil {
		return errors.New("订单写入事务未初始化")
	}
	if work == nil {
		return errors.New("订单写入事务工作函数不能为空")
	}
	// transaction、beginErr 保存开始 SQL 事务的结果；事务对象不会离开本函数的回调作用域。
	transaction, beginErr := unit.database.BeginTx(ctx, nil)
	if beginErr != nil {
		return beginErr
	}
	// committed 表示提交已完成，defer 仅在失败路径回滚事务。
	committed := false
	defer func() {
		if !committed {
			_ = transaction.Rollback()
		}
	}()
	// writeTransaction 保存本次回调可用的窄订单/商品写入能力。
	writeTransaction := &OrderWriteTransaction{transaction: transaction, orders: unit.orders, items: unit.items}
	// workErr 保存应用写入回调的错误；该错误会使此前所有写入原子回滚。
	if workErr := work(writeTransaction); workErr != nil {
		return workErr
	}
	// commitErr 保存提交数据库事务时的错误；提交失败后 defer 会尽力回滚。
	if commitErr := transaction.Commit(); commitErr != nil {
		return commitErr
	}
	committed = true
	return nil
}

// PatchOrder 在当前事务中更新订单的可选字段。
// ctx 用于取消 SQL；orderID 与 patch 的字段校验语义保持与 Orders.Patch 一致。
func (transaction *OrderWriteTransaction) PatchOrder(ctx context.Context, orderID string, patch OrderPatch) error {
	if transaction == nil || transaction.transaction == nil || transaction.orders == nil {
		return errors.New("订单写入事务未初始化")
	}
	return transaction.orders.PatchTx(ctx, transaction.transaction, orderID, patch)
}

// UpsertItemBasic 在当前事务中创建或补全订单关联商品的基础信息。
// ctx 用于取消 SQL；row 由调用方提供，成功后仅在所在事务提交时对外可见。
func (transaction *OrderWriteTransaction) UpsertItemBasic(ctx context.Context, row *ItemInfoRow) error {
	if transaction == nil || transaction.transaction == nil || transaction.items == nil {
		return errors.New("订单写入事务未初始化")
	}
	return transaction.items.UpsertBasicTx(ctx, transaction.transaction, row)
}

// UpsertOrder 在当前事务中创建或更新一条订单。
// ctx 用于取消 SQL；orderID 和 options 的数据校验、乐观锁及方言语义委托给 Orders。
func (transaction *OrderWriteTransaction) UpsertOrder(ctx context.Context, orderID string, options OrderUpsertOpts) error {
	if transaction == nil || transaction.transaction == nil || transaction.orders == nil {
		return errors.New("订单写入事务未初始化")
	}
	return transaction.orders.UpsertTx(ctx, transaction.transaction, orderID, options)
}

// UpsertOrders 在当前事务中以最多两条多值 UPSERT 写入订单详情分片。
// ctx 用于取消 SQL；rows 为空时不产生写入，重复订单和跨账号冲突仍由 Orders 校验。
func (transaction *OrderWriteTransaction) UpsertOrders(ctx context.Context, rows []BatchOrderUpsert) error {
	if transaction == nil || transaction.transaction == nil || transaction.orders == nil {
		return errors.New("订单写入事务未初始化")
	}
	return transaction.orders.UpsertManyTx(ctx, transaction.transaction, rows)
}
