package server

import (
	"context"
	"database/sql"
	"errors"
)

// withTransaction 在 Server 应用层统一创建、提交或回滚数据库事务。
// 调用方只描述事务内的数据库操作，不再直接管理事务生命周期。
// withTransaction 负责withTransaction相关处理。
func (s *Server) withTransaction(ctx context.Context, work func(*sql.Tx) error) error {
	// repository 是当前 Server 装配的事务执行边界。
	repository := s.transactionRepository
	if repository == nil {
		return errors.New("事务 repository 未初始化")
	}
	return repository.WithTransaction(ctx, work)
}
