package adapter

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// TestDatabaseHealthRejectsMissingDependency 验证健康检查缺少数据库时不会伪装成功。
func TestDatabaseHealthRejectsMissingDependency(t *testing.T) {
	// health 保存缺少 Store 的数据库健康检查适配器。
	health := NewDatabaseHealth(nil)
	// err 保存缺少数据库依赖时的探测错误。
	if err := health.Ping(context.Background()); err == nil {
		t.Fatal("缺少数据库时健康检查不应成功")
	}
}

// TestDatabaseHealthPingsDatabase 验证健康检查适配器能够在 Context 内探测 SQLite 连接。
func TestDatabaseHealthPingsDatabase(t *testing.T) {
	// database、dialect 和 openErr 保存临时 SQLite 连接及其方言。
	database, dialect, openErr := db.Open(context.Background(), t.TempDir()+"/health.db")
	if openErr != nil {
		t.Fatalf("打开测试数据库失败: %v", openErr)
	}
	defer database.Close()
	// health 保存绑定 SQLite 连接的健康检查适配器。
	health := NewDatabaseHealth(db.NewStore(database, dialect))
	// err 保存数据库连接仍可用时的探测错误。
	if err := health.Ping(context.Background()); err != nil {
		t.Fatalf("数据库健康检查失败: %v", err)
	}
	// closeErr 保存关闭数据库后的探测错误，验证故障状态会被上报。
	if closeErr := database.Close(); closeErr != nil {
		t.Fatalf("关闭测试数据库失败: %v", closeErr)
	}
	// err 保存数据库已关闭后的探测错误。
	if err := health.Ping(context.Background()); err == nil {
		t.Fatal("关闭数据库后健康检查不应成功")
	}
}
