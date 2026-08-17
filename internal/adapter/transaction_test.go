package adapter

import (
	"context"
	"database/sql"
	"errors"
	"testing"

	"xianyu-go/internal/db"
)

// TestTransactionRepositoryRejectsMissingDependencies 验证事务适配器缺少数据库或工作函数时快速失败。
func TestTransactionRepositoryRejectsMissingDependencies(t *testing.T) {
	// repository 保存缺少数据库的事务适配器。
	repository := NewTransactionRepository(nil)
	// err 保存缺少数据库时的错误。
	if err := repository.WithTransaction(context.Background(), func(*sql.Tx) error { return nil }); err == nil {
		t.Fatal("缺少数据库时事务不应伪装成功")
	}
	// err 保存缺少数据库和工作函数时的错误。
	if err := NewTransactionRepository(nil).WithTransaction(context.Background(), nil); err == nil {
		t.Fatal("缺少数据库和工作函数时事务不应伪装成功")
	}
	// database、dialect 和 openErr 保存最小 SQLite 数据库装配结果。
	database, dialect, openErr := db.Open(context.Background(), t.TempDir()+"/transaction.db")
	if openErr != nil {
		t.Fatalf("打开测试数据库失败: %v", openErr)
	}
	defer database.Close()
	// err 保存有效数据库但缺少工作函数时的错误。
	if err := NewTransactionRepository(db.NewStore(database, dialect)).WithTransaction(context.Background(), nil); err == nil {
		t.Fatal("缺少事务工作函数时不应伪装成功")
	}
}

// TestTransactionRepositoryCommitsAndRollsBack 验证事务适配器提交成功操作并回滚失败操作。
func TestTransactionRepositoryCommitsAndRollsBack(t *testing.T) {
	// database、dialect 和 openErr 保存最小 SQLite 数据库装配结果。
	database, dialect, openErr := db.Open(context.Background(), t.TempDir()+"/transaction-commit.db")
	if openErr != nil {
		t.Fatalf("打开测试数据库失败: %v", openErr)
	}
	defer database.Close()
	// repository 是当前测试使用的数据库事务适配器。
	repository := NewTransactionRepository(db.NewStore(database, dialect))
	// ctx 是本测试使用的背景上下文。
	ctx := context.Background()
	// createErr 表示创建事务测试表时的数据库错误。
	if _, createErr := database.ExecContext(ctx, `CREATE TABLE transaction_probe (id INTEGER PRIMARY KEY, value TEXT NOT NULL)`); createErr != nil {
		t.Fatalf("创建事务测试表失败: %v", createErr)
	}
	// commitErr 表示成功事务的执行结果。
	commitErr := repository.WithTransaction(ctx, func(tx *sql.Tx) error {
		// execErr 表示事务内插入成功记录时的数据库错误。
		_, execErr := tx.ExecContext(ctx, `INSERT INTO transaction_probe (id, value) VALUES (1, 'committed')`)
		return execErr
	})
	if commitErr != nil {
		t.Fatalf("事务提交失败: %v", commitErr)
	}
	// rollbackErr 是故意返回的错误，用于验证事务不会提交部分写入。
	rollbackErr := errors.New("故意回滚")
	// returnedErr 表示回滚事务向调用方传播的错误。
	returnedErr := repository.WithTransaction(ctx, func(tx *sql.Tx) error {
		// execErr 表示事务内插入回滚记录时的数据库错误。
		_, execErr := tx.ExecContext(ctx, `INSERT INTO transaction_probe (id, value) VALUES (2, 'rolled back')`)
		if execErr != nil {
			return execErr
		}
		return rollbackErr
	})
	if !errors.Is(returnedErr, rollbackErr) {
		t.Fatalf("事务回滚错误不匹配: %v", returnedErr)
	}
	// count 是事务最终提交后测试表中的记录数。
	var count int
	// scanErr 表示读取事务测试表统计结果时的数据库错误。
	if scanErr := database.QueryRowContext(ctx, `SELECT COUNT(*) FROM transaction_probe`).Scan(&count); scanErr != nil {
		t.Fatalf("查询事务测试结果失败: %v", scanErr)
	}
	if count != 1 {
		t.Fatalf("事务回滚后记录数应为 1，got %d", count)
	}
}
