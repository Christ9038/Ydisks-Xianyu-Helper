package adapter

import (
	"context"
	"database/sql"
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
