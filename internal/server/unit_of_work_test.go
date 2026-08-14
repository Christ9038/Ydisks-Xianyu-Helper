package server

import (
	"context"
	"database/sql"
	"errors"
	"testing"
)

// TestWithTransactionCommitAndRollback 验证统一事务入口能够提交成功操作并回滚失败操作。
func TestWithTransactionCommitAndRollback(t *testing.T) {
	// srv 是使用测试数据库的 Server。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是本测试使用的可取消上下文。
	ctx := context.Background()
	// createErr 是创建测试表失败时的错误。
	if _, createErr := srv.Store.DB.ExecContext(ctx, `CREATE TABLE unit_of_work_probe (id INTEGER PRIMARY KEY, value TEXT NOT NULL)`); createErr != nil {
		t.Fatalf("创建事务测试表失败: %v", createErr)
	}
	// commitErr 是提交事务执行结果。
	commitErr := srv.withTransaction(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO unit_of_work_probe (id, value) VALUES (1, 'committed')`)
		return err
	})
	if commitErr != nil {
		t.Fatalf("事务提交失败: %v", commitErr)
	}
	// rollbackErr 是故意触发回滚事务的错误。
	rollbackErr := errors.New("故意回滚")
	if err := srv.withTransaction(ctx, func(tx *sql.Tx) error {
		_, err := tx.ExecContext(ctx, `INSERT INTO unit_of_work_probe (id, value) VALUES (2, 'rolled back')`)
		if err != nil {
			return err
		}
		return rollbackErr
	}); !errors.Is(err, rollbackErr) {
		t.Fatalf("事务回滚错误不匹配: %v", err)
	}
	// count 是事务提交后测试表中的记录数。
	var count int
	if err := srv.Store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM unit_of_work_probe`).Scan(&count); err != nil {
		t.Fatalf("查询事务测试结果失败: %v", err)
	}
	if count != 1 {
		t.Fatalf("事务回滚后记录数应为 1，got %d", count)
	}
}
