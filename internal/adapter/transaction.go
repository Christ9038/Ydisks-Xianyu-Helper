package adapter

import (
	"context"
	"database/sql"
	"errors"

	"xianyu-go/internal/db"
)

// TransactionRepository 将数据库事务生命周期适配为 Server 的最小执行端口。
type TransactionRepository struct {
	// store 保存数据库聚合入口，仅在事务适配器内部使用。
	store *db.Store
}

// NewTransactionRepository 创建数据库事务适配器。
func NewTransactionRepository(store *db.Store) *TransactionRepository {
	return &TransactionRepository{store: store}
}

// WithTransaction 创建、提交或回滚一个数据库事务。
func (repository *TransactionRepository) WithTransaction(ctx context.Context, work func(*sql.Tx) error) error {
	if repository == nil || repository.store == nil || repository.store.DB == nil {
		return errors.New("事务 repository 未初始化")
	}
	if work == nil {
		return errors.New("事务工作函数不能为空")
	}
	// tx 和 beginErr 保存事务创建结果及错误。
	tx, beginErr := repository.store.DB.BeginTx(ctx, nil)
	if beginErr != nil {
		return beginErr
	}
	// committed 标识事务是否已经提交，避免提交失败后重复处理回滚。
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback()
		}
	}()
	// workErr 保存事务工作函数返回的业务错误。
	if workErr := work(tx); workErr != nil {
		return workErr
	}
	// commitErr 保存事务提交错误。
	if commitErr := tx.Commit(); commitErr != nil {
		return commitErr
	}
	committed = true
	return nil
}
