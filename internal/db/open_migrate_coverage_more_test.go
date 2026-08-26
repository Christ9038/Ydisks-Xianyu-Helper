package db

import (
	"context"
	"testing"
)

// TestOpenAndMigrateCoverInputAndInfrastructureErrors 验证数据库连接解析、取消和迁移方言错误边界。
func TestOpenAndMigrateCoverInputAndInfrastructureErrors(t *testing.T) {
	// ctx 是数据库打开测试共用的上下文。
	ctx := context.Background()
	// invalidURLs 保存应在连接前被拒绝的数据库 URL。
	invalidURLs := []string{"", "   ", "oracle://example"}
	// invalidURL 表示当前数据库 URL 输入。
	for _, invalidURL := range invalidURLs {
		// openErr 保存数据库 URL 校验错误。
		if _, _, openErr := Open(ctx, invalidURL); openErr == nil {
			t.Fatalf("invalid database URL %q should fail", invalidURL)
		}
	}
	// canceledContext 保存已取消的连接上下文，验证 Ping 错误会关闭连接并返回。
	canceledContext, cancel := context.WithCancel(ctx)
	cancel()
	// canceledOpenErr 保存取消上下文下 SQLite 打开的错误。
	if _, _, canceledOpenErr := Open(canceledContext, t.TempDir()+"/canceled.db"); canceledOpenErr == nil {
		t.Fatal("canceled database open should fail")
	}
	// unknownDialectErr 保存未知数据库方言的迁移错误。
	if unknownDialectErr := Migrate(ctx, nil, Dialect("oracle")); unknownDialectErr == nil {
		t.Fatal("unknown dialect should fail")
	}
	// store、cleanup 保存有效数据库及随后主动关闭的连接。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// closeErr 保存关闭数据库连接的错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// closedMigrationErr 保存关闭连接后的迁移错误。
	if closedMigrationErr := Migrate(ctx, store.DB, DialectSQLite); closedMigrationErr == nil {
		t.Fatal("migration on closed database should fail")
	}
}
