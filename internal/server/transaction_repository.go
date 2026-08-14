package server

import (
	"context"
	"database/sql"
	"errors"

	"xianyu-go/internal/db"
)

// transactionRepository 定义 Server 统一事务入口所需的最小持久化能力。
type transactionRepository interface {
	// WithTransaction 在一个事务中执行工作并负责提交或回滚。
	WithTransaction(ctx context.Context, work func(*sql.Tx) error) error
}

// storeTransactionRepository 将完整 Store 适配为事务执行窄 repository。
type storeTransactionRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// WithTransaction 创建、提交或回滚数据库事务。
func (r storeTransactionRepository) WithTransaction(ctx context.Context, work func(*sql.Tx) error) error {
	if r.store == nil || r.store.DB == nil {
		return errors.New("事务 repository 未初始化")
	}
	// tx 是本次用例使用的数据库事务。
	// err 表示事务创建错误。
	// tx、err 保存tx、err，供当前处理流程使用
	tx, err := r.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	// committed 标识事务是否已经成功提交，避免提交失败后重复处理回滚错误。
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	if // err 保存err，供当前处理流程使用
	err := work(tx); err != nil {
		return err
	}
	// err 表示事务提交错误。
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}

// newStoreTransactionRepository 从完整 Store 构造事务执行窄 repository。
func newStoreTransactionRepository(store *db.Store) transactionRepository {
	if store == nil || store.DB == nil {
		return nil
	}
	return storeTransactionRepository{store: store}
}

// 确保 Store 适配器始终覆盖事务执行边界。
var _ transactionRepository = storeTransactionRepository{}
