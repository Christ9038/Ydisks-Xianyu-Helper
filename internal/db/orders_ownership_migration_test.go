package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/pressly/goose/v3"
)

// assertOwnershipLookupIndexes 为 t 检查 database 的恢复查询索引；dialect 决定元数据查询，present 表示 Up 或 Down 后的预期。
// 检查真实数据库中的索引列顺序，防止同名索引存在却无法按 order_id 定位。
func assertOwnershipLookupIndexes(t *testing.T, database *sql.DB, dialect Dialect, present bool) {
	t.Helper()
	// expected 描述本次迁移必须创建的索引、所属表及有序列集合。
	for _, expected := range []struct {
		// name 是索引名，用于区分原有账号优先索引。
		name string
		// table 是索引目标表，避免在其他表上误匹配同名索引。
		table string
		// columns 是按索引键顺序排列的列名。
		columns string
	}{
		{name: "idx_automation_runs_order", table: "automation_runs", columns: "order_id"},
		{name: "idx_order_reconciliations_order_status", table: "order_reconciliations", columns: "order_id,status"},
	} {
		// query、args 指向对应方言的索引元数据，仅读 schema，不读取业务载荷。
		query, args := `SELECT name FROM pragma_index_info(?) ORDER BY seqno`, []any{expected.name}
		switch dialect {
		case DialectMySQL:
			query = `SELECT COLUMN_NAME FROM information_schema.STATISTICS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND INDEX_NAME=? ORDER BY SEQ_IN_INDEX`
			args = []any{expected.table, expected.name}
		case DialectPostgres:
			query = `SELECT pg_get_indexdef(i.indexrelid, positions.n, true)
				FROM pg_index i JOIN pg_class idx ON idx.oid=i.indexrelid
				JOIN pg_class tbl ON tbl.oid=i.indrelid
				CROSS JOIN LATERAL generate_series(1, i.indnkeyatts) AS positions(n)
				WHERE idx.relname=? AND tbl.relname=? AND tbl.relnamespace=current_schema()::regnamespace
				ORDER BY positions.n`
			args = []any{expected.name, expected.table}
		}
		// rows、err 保存索引键列表查询结果；数据库错误不能被当成索引不存在。
		rows, err := database.Query(query, args...)
		if err != nil {
			t.Fatal(err)
		}
		// columns 收集真实索引键的顺序，用于检查 order_id 是首列。
		var columns []string
		for rows.Next() {
			// column 是当前索引键名，不包含业务值。
			var column string
			// err 保存索引键扫描错误，提前失败时仍关闭结果集。
			if err := rows.Scan(&column); err != nil {
				_ = rows.Close()
				t.Fatal(err)
			}
			columns = append(columns, column)
		}
		// readErr 保存元数据迭代错误，关闭游标后统一处理。
		readErr := rows.Err()
		_ = rows.Close()
		if readErr != nil {
			t.Fatal(readErr)
		}
		if present && strings.Join(columns, ",") != expected.columns {
			t.Fatalf("索引 %s 列顺序=%v，期望 %s", expected.name, columns, expected.columns)
		}
		if !present && len(columns) != 0 {
			t.Fatalf("回滚后索引 %s 仍存在", expected.name)
		}
	}
}

// TestMultiDB_OrderOwnershipLookupIndexes 验证 t 中各可用方言的 42 迁移创建索引、回滚删除索引及再次升级重建索引。
func TestMultiDB_OrderOwnershipLookupIndexes(t *testing.T) {
	// target 提供当前方言的独立数据库，子测试负责释放，不操作真实账号数据库。
	for _, target := range allTestTargets(t) {
		// t 管理当前方言的迁移生命周期及断言，不并行修改 Goose 全局方言配置。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			assertOwnershipLookupIndexes(t, target.store.DB, target.dialect, true)
			// subdir、gooseDialect 为当前方言选择已有嵌入迁移目录和 Goose 配置。
			subdir, gooseDialect := migrationTestSubdir(t, target.dialect)
			// err 保存迁移方言设置错误。
			if err := goose.SetDialect(gooseDialect); err != nil {
				t.Fatal(err)
			}
			goose.SetBaseFS(migrationsFS)
			// err 保存回滚至 41 的结果；42 的审计表和查询索引必须同时退出。
			if err := goose.DownTo(target.store.DB, "migrations/"+subdir, 41); err != nil {
				t.Fatal(err)
			}
			assertOwnershipLookupIndexes(t, target.store.DB, target.dialect, false)
			if tableExistsForDialect(t, target.store.DB, target.dialect, "order_ownership_repairs") {
				t.Fatal("回滚 42 后仍残留归属审计表")
			}
			// err 保存重新升级至 42 的结果，用于发现 Down 遗留的重名索引。
			if err := goose.UpTo(target.store.DB, "migrations/"+subdir, 42); err != nil {
				t.Fatal(err)
			}
			assertOwnershipLookupIndexes(t, target.store.DB, target.dialect, true)
			if !tableExistsForDialect(t, target.store.DB, target.dialect, "order_ownership_repairs") {
				t.Fatal("重新升级 42 后未恢复归属审计表")
			}
		})
	}
}

// TestMultiDB_OrderAutomationGuardsBackfill 验证 t 中 42 回填所有状态的去重执行痕迹，规则级联删除后仍阻止恢复。
// 守卫无外键且无秘密；回滚 42 必须删除守卫表，重新升级必须恢复 schema。
func TestMultiDB_OrderAutomationGuardsBackfill(t *testing.T) {
	// target 是当前可用的独立测试数据库，不接触用户业务数据。
	for _, target := range allTestTargets(t) {
		// t 管理当前方言的迁移和数据断言，不并行使用 Goose 全局配置。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx、store 提供同步数据库操作及仓储。
			ctx, store := context.Background(), target.store
			// subdir、gooseDialect 指向当前方言的嵌入迁移。
			subdir, gooseDialect := migrationTestSubdir(t, target.dialect)
			// err 保存方言配置失败。
			if err := goose.SetDialect(gooseDialect); err != nil {
				t.Fatal(err)
			}
			goose.SetBaseFS(migrationsFS)
			// err 保存回退到没有守卫表的 41 的失败，以复现真实升级入口。
			if err := goose.DownTo(store.DB, "migrations/"+subdir, 41); err != nil {
				t.Fatal(err)
			}
			// userID、expected、options 提供买家错绑旧单及合法恢复输入。
			userID, expected, options := seedOwnershipRepair(t, store)
			// ruleID、err 创建承载历史执行记录的规则，未配置任何外部动作。
			ruleID, err := store.Automation.Create(ctx, AutomationRuleInput{UserID: userID, CookieID: expected.CookieID, Name: "guard-fixture", TriggerType: "paid"})
			if err != nil {
				t.Fatal(err)
			}
			// index、record 构造同订单账号的重复运行、另一账号及缺失身份的旧运行，覆盖 DISTINCT 和非空过滤。
			for index, record := range []struct {
				// orderID 是旧运行记录的订单身份，空值不应产生伪守卫。
				orderID string
				// cookieID 是执行账号身份，空值不应产生伪守卫。
				cookieID string
				// status 表示历史动作阶段，任何状态均必须保留痕迹。
				status string
			}{
				{expected.OrderID, expected.CookieID, "success"},
				{expected.OrderID, expected.CookieID, "failed"},
				{expected.OrderID, options.CookieID, "running"},
				{"", expected.CookieID, "success"},
				{expected.OrderID, "", "success"},
			} {
				// err 保存历史运行插入错误；只用虚构载荷，不打印原文或凭据。
				if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_runs
					(rule_id,cookie_id,order_id,trigger_type,trigger_key,status,error_message,raw_event_json,delivery_proof)
					VALUES(?,?,?,'paid',?,?,'','{}','')`, ruleID, record.cookieID, record.orderID, fmt.Sprintf("guard-%d", index), record.status); err != nil {
					t.Fatal(err)
				}
			}
			// err 保存从历史运行回填守卫的升级错误。
			if err := goose.UpTo(store.DB, "migrations/"+subdir, 42); err != nil {
				t.Fatal(err)
			}
			// count 检查回填按订单/账号去重并过滤缺失身份，默认时间必须落库。
			var count int
			// err 保存非秘密守卫行数检查结果。
			if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_automation_guards WHERE order_id=? AND cookie_id IN (?,?) AND created_at IS NOT NULL`, expected.OrderID, expected.CookieID, options.CookieID).Scan(&count); err != nil || count != 2 {
				t.Fatalf("守卫回填未保留两组有效身份: count=%d err=%v", count, err)
			}
			// err 验证缺失身份没有留下额外守卫。
			if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM order_automation_guards`).Scan(&count); err != nil || count != 2 {
				t.Fatalf("守卫回填未过滤无效身份: count=%d err=%v", count, err)
			}
			// err 验证复合主键拒绝同一订单和账号的重复守卫。
			if _, err := store.DB.ExecContext(ctx, `INSERT INTO order_automation_guards(order_id,cookie_id) VALUES(?,?)`, expected.OrderID, expected.CookieID); err == nil {
				t.Fatal("守卫缺少订单和账号复合唯一性")
			}
			// err 验证守卫不依赖订单或账号外键，即使这些业务对象不存在仍可保留历史痕迹。
			if _, err := store.DB.ExecContext(ctx, `INSERT INTO order_automation_guards(order_id,cookie_id) VALUES('orphan-guard-order','orphan-guard-account')`); err != nil {
				t.Fatalf("独立守卫不应要求订单或账号外键: %v", err)
			}
			// err 模拟原有物理删除规则的级联语义，守卫不得随运行一起消失。
			if _, err := store.DB.ExecContext(ctx, `DELETE FROM automation_rules WHERE id=?`, ruleID); err != nil {
				t.Fatal(err)
			}
			// err 确认原运行已被规则外键级联删除，后续阻断只能来自持久守卫。
			if err := store.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_runs WHERE rule_id=?`, ruleID).Scan(&count); err != nil || count != 0 {
				t.Fatalf("规则删除未级联清理运行: count=%d err=%v", count, err)
			}
			// err 必须拒绝已失去运行记录但仍有持久守卫的归属修复。
			if err := store.Orders.RecoverSoldOwnership(ctx, userID, options.CookieID, expected, options); !errors.Is(err, ErrOrderRecoveryUnsafe) {
				t.Fatalf("删除运行后历史动作保护被绕过: %v", err)
			}
			assertOwnershipRepairRolledBack(t, store, userID, expected)
			// err 保存第二次回退错误，检查守卫表的 Down 完整性。
			if err := goose.DownTo(store.DB, "migrations/"+subdir, 41); err != nil {
				t.Fatal(err)
			}
			if tableExistsForDialect(t, store.DB, target.dialect, "order_automation_guards") {
				t.Fatal("回滚 42 后守卫表仍存在")
			}
			// err 保存再次升级结果，空历史运行也必须能够建立守卫表。
			if err := goose.UpTo(store.DB, "migrations/"+subdir, 42); err != nil {
				t.Fatal(err)
			}
			if !tableExistsForDialect(t, store.DB, target.dialect, "order_automation_guards") {
				t.Fatal("重新升级未建立守卫表")
			}
		})
	}
}
