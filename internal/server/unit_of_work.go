package server

import (
	"context"
	"database/sql"
)

// withTransaction 在 Server 应用层统一创建、提交或回滚数据库事务。
// 调用方只描述事务内的数据库操作，不再直接管理事务生命周期。
func (s *Server) withTransaction(ctx context.Context, work func(*sql.Tx) error) error {
	// tx 是本次用例使用的数据库事务。
	tx, err := s.Store.DB.BeginTx(ctx, nil)
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
	if err := work(tx); err != nil {
		return err
	}
	if err := tx.Commit(); err != nil {
		return err
	}
	committed = true
	return nil
}
