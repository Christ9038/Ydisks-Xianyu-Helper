package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// TestCookieSQLAndOrderCursorBoundaries 验证跨数据库锁语句、订单游标时间和取消删除路径。
func TestCookieSQLAndOrderCursorBoundaries(t *testing.T) {
	// cookies 保存不同数据库方言下的凭证仓储。
	cookies := &Cookies{}
	// sqliteQuery、mysqlQuery 和 postgresQuery 保存各方言生成的锁定查询。
	sqliteQuery := (&Cookies{Dialect: DialectSQLite}).cookieSelectForUpdate("id")
	// mysqlQuery 保存 MySQL 方言追加行锁的查询。
	mysqlQuery := (&Cookies{Dialect: DialectMySQL}).cookieSelectForUpdate("id")
	// postgresQuery 保存 PostgreSQL 方言追加行锁的查询。
	postgresQuery := (&Cookies{Dialect: DialectPostgres}).cookieSelectForUpdate("id")
	_ = cookies
	if strings.Contains(sqliteQuery, "FOR UPDATE") || !strings.Contains(mysqlQuery, "FOR UPDATE") || !strings.Contains(postgresQuery, "FOR UPDATE") {
		t.Fatalf("凭证锁查询方言错误 sqlite=%q mysql=%q postgres=%q", sqliteQuery, mysqlQuery, postgresQuery)
	}
	// cursorCases 保存标准时间、非标准时间和空游标的归一化预期。
	cursorCases := []struct {
		// raw 是数据库驱动或兼容客户端返回的原始游标时间。
		raw string
		// want 是用于跨方言比较的标准时间文本。
		want string
	}{
		{raw: "2026-08-26T04:05:06.123456789Z", want: "2026-08-26 04:05:06.123456789"},
		{raw: "2026-08-26T04:05:06Z", want: "2026-08-26 04:05:06"},
		{raw: "legacy-valueZ", want: "legacy-value"},
		{raw: "legacyTvalue", want: "legacy value"},
	}
	// cursorCase 表示当前订单游标时间样例。
	for _, cursorCase := range cursorCases {
		// got 保存当前游标时间归一化后的结果。
		if got := normalizeOrderCursorTime(cursorCase.raw); got != cursorCase.want {
			t.Errorf("游标时间=%q got=%q want=%q", cursorCase.raw, got, cursorCase.want)
		}
	}
	// store、cleanup 保存用于验证取消删除错误路径的数据库仓储。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// canceledContext、cancel 保存已取消的删除上下文。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	// deleteErr 保存数据库操作被取消后的底层错误。
	_, deleteErr := store.WSMessages.DeleteBefore(canceledContext, "cid", time.Now())
	if !errors.Is(deleteErr, context.Canceled) {
		t.Fatalf("取消删除错误=%v", deleteErr)
	}
}
