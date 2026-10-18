package db

import (
	"context"
	"database/sql"
	"testing"
)

// templateMessageQueryFixture 用真实 SQLite 游标注入读取故障，不修改生产查询或数据库结构。
type templateMessageQueryFixture struct {
	// database 提供与生产相同的 database/sql 游标生命周期。
	database *sql.DB
	// query 是测试指定的固定 SQL，仅用于构造扫描或迭代错误。
	query string
}

// QueryContext 用 ctx 执行固定夹具查询，忽略生产 SQL 参数以精确控制失败位置。
func (f templateMessageQueryFixture) QueryContext(ctx context.Context, _ string, _ ...any) (*sql.Rows, error) {
	return f.database.QueryContext(ctx, f.query)
}

// TestReadDeliveryTemplateMessagesRejectsIncompleteRows 验证查询、字段扫描和后续行迭代失败不能返回部分模板。
func TestReadDeliveryTemplateMessagesRejectsIncompleteRows(t *testing.T) {
	// store、cleanup 为真实行流错误测试提供独立 SQLite 连接。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// scenarios 每条 SQL 都返回或尝试返回七列，但在不同读取阶段失败。
	scenarios := []struct {
		// name 标识失败阶段，query 构造对应故障。
		name string
		// query 不包含真实业务数据，迭代场景在第二行才产生整数溢出。
		query string
	}{
		{name: "query", query: "SELECT * FROM missing_delivery_message_fixture"},
		{name: "scan", query: "SELECT 'invalid-id', 1, 0, 'text', 'text', '', ''"},
		{name: "iteration", query: "SELECT CASE x WHEN 1 THEN 1 ELSE abs(-9223372036854775808) END, 1, 0, 'text', 'text', '', '' FROM (SELECT 1 AS x UNION ALL SELECT 2)"},
	}
	for /* scenario 是当前游标失败场景。 */ _, scenario := range scenarios {
		t.Run(scenario.name, func(t *testing.T) {
			// messages、err 必须丢弃可能已经读到的第一条消息，不能把不完整模板用于发货。
			messages, err := readDeliveryTemplateMessages(context.Background(), templateMessageQueryFixture{database: store.DB, query: scenario.query}, 1)
			if err == nil || messages != nil {
				t.Fatalf("读取失败返回了部分模板: count=%d err=%v", len(messages), err)
			}
			if pingErr := store.DB.PingContext(context.Background()); pingErr != nil { // pingErr 检查错误返回后没有遗留不可用连接。
				t.Fatal(pingErr)
			}
		})
	}
}
