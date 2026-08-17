package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
)

// TestMigrate_AppliesCleanSchema 在临时库上跑迁移，验证全量 schema 干净落地、
// 关键不一致列（orders.system_shipped 等）存在、默认设置就位。
// TestMigrate_AppliesCleanSchema 负责TestMigrateAppliesCleanSchema相关处理。
func TestMigrate_AppliesCleanSchema(t *testing.T) {
	// tmp 保存tmp，供当前处理流程使用
	tmp := t.TempDir()
	// dbPath 保存db路径，供当前处理流程使用
	dbPath := filepath.Join(tmp, "test.db")

	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// db、err 保存db、err，供当前处理流程使用
	db, _, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// checks 保存checks，供当前处理流程使用
	checks := []struct {
		table string
		col   string
	}{
		{"orders", "system_shipped"},
		{"orders", "receiver_city"},
		{"orders", "version"},
		{"orders", "deleted_at"},
		{"cards", "image_url"},
		{"cards", "delay_seconds"},
		{"keywords", "item_id"},
		{"item_info", "multi_quantity_delivery"},
		{"item_info", "deleted_at"},
		{"automation_rules", "deleted_at"},
		{"default_replies", "reply_once"},
		{"default_reply_records", "status"},
		{"default_reply_records", "text_sent"},
		{"automation_runs", "action_cursor"},
		{"automation_runs", "action_started"},
		{"default_reply_records", "image_sent"},
		{"users", "is_admin"},
		{"sessions", "session_id"},
		{"notification_channels", "user_id"},
		{"notification_channels", "event_types"},
		{"message_notifications", "event_types"},
		{"scheduled_cookies_refresh_log", "step_details"},
		{"scheduled_cookies_refresh_log", "renew_method"},
		{"scheduled_cookies_refresh_log", "duration_ms"},
		{"scheduled_cookies_refresh_log", "request_count"},
		{"scheduled_login_renew_log", "step_details"},
		{"scheduled_login_renew_log", "updated_cookie_count"},
		{"scheduled_api_cookie_renew_log", "step_details"},
		{"scheduled_api_cookie_renew_log", "request_count"},
		{"risk_control_logs", "processing_status"},
		{"risk_control_logs", "duration_ms"},
		{"notification_outbox", "worker_token"},
		{"security_audit_logs", "keys_json"},
		{"security_audit_logs", "outcome"},
	}
	// c 表示当前遍历过程中的c
	for _, c := range checks {
		if !columnExists(t, db, c.table, c.col) {
			t.Errorf("列缺失: %s.%s（应为收敛后的最终 schema）", c.table, c.col)
		}
	}

	// 默认系统设置应就位（qq_reply_secret_key 应为空，遵循无默认口令安全基线）。
	var val string
	err = db.QueryRow(`SELECT value FROM system_settings WHERE key='theme_color'`).Scan(&val)
	if err != nil || val != "blue" {
		t.Errorf("默认设置 theme_color 异常: val=%q err=%v", val, err)
	}
	err = db.QueryRow(`SELECT value FROM system_settings WHERE key='qq_reply_secret_key'`).Scan(&val)
	if err != nil || val != "" {
		t.Errorf("qq_reply_secret_key 应为空（无默认值安全基线）: val=%q err=%v", val, err)
	}
	err = db.QueryRow(`SELECT value FROM system_settings WHERE key='log_level'`).Scan(&val)
	if err != nil || val != "info" {
		t.Errorf("log_level 默认设置异常: val=%q err=%v", val, err)
	}
	err = db.QueryRow(`SELECT value FROM system_settings WHERE key='renewal_log_retention_days'`).Scan(&val)
	if err != nil || val != "10" {
		t.Errorf("renewal_log_retention_days 默认设置异常: val=%q err=%v", val, err)
	}

	// 二次 Open 应幂等（迁移不重复执行、不报错）。
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	// db2、err 保存db2、err，供当前处理流程使用
	db2, _, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("二次 Open 幂等失败: %v", err)
	}
	db2.Close()
}

// TestMigrate_UpgradesDatabaseWithMainChatVersions 验证已发布 main 的 00029/00030
// 聊天迁移可以原样升级到合并后的 dev 最终版本。
func TestMigrate_UpgradesDatabaseWithMainChatVersions(t *testing.T) {
	// tmpDir 保存隔离的已发布 main 数据库目录，测试结束后由 testing 清理。
	tmpDir := t.TempDir()
	// dbPath 指向模拟已运行至 main 00030 的 SQLite 文件。
	dbPath := filepath.Join(tmpDir, "main-chat-v30.db")
	// rawDB 在调用 Open 前控制 Goose 只执行已发布的 main 迁移。
	rawDB, openErr := sql.Open("sqlite", sqliteDSN(dbPath))
	if openErr != nil {
		t.Fatalf("open legacy database: %v", openErr)
	}
	defer rawDB.Close()

	// dialectErr 保存 Goose SQLite 方言设置失败，失败时不能构造已发布迁移基线。
	if dialectErr := goose.SetDialect("sqlite3"); dialectErr != nil {
		t.Fatalf("set goose dialect: %v", dialectErr)
	}
	goose.SetBaseFS(migrationsFS)
	// upErr 将数据库推进到已发布 main 的 00030，验证之后的 dev 迁移连续接续。
	upErr := goose.UpTo(rawDB, "migrations/sqlite", 30)
	if upErr != nil {
		t.Fatalf("apply released main migrations: %v", upErr)
	}

	// ctx 提供迁移 API 所需的调用上下文；升级本身不依赖请求生命周期。
	ctx := context.Background()
	// migrateErr 保存从 main 00030 接续 dev 00031 时的迁移失败。
	if migrateErr := Migrate(ctx, rawDB, DialectSQLite); migrateErr != nil {
		t.Fatalf("upgrade from main 00030: %v", migrateErr)
	}
	if !tableExists(t, rawDB, "order_reconciliations") {
		t.Fatal("order_reconciliations should be created by the dev schema baseline migration")
	}
	if !columnExists(t, rawDB, "chat_messages", "read_status") || !columnExists(t, rawDB, "chat_messages", "read_at") {
		t.Fatal("chat read tracking columns should remain after dev schema baseline upgrade")
	}
	// finalVersion 验证迁移账本已推进到合并后的单一 dev schema 基线版本。
	finalVersion, versionErr := goose.GetDBVersion(rawDB)
	if versionErr != nil {
		t.Fatalf("read final migration version: %v", versionErr)
	}
	if finalVersion != 31 {
		t.Fatalf("final migration version=%d, want 31", finalVersion)
	}
}

// columnExists 负责columnExists相关处理。
func columnExists(t *testing.T, db *sql.DB, table, col string) bool {
	t.Helper()
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		// name 保存名称，供当前处理流程使用
		var name string
		if // err 保存err，供当前处理流程使用
		err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == col {
			return true
		}
	}
	return false
}

// TestLatestMigrationsDownUpSQLite 负责TestLatestMigrationsDownUpSQLite相关处理。
func TestLatestMigrationsDownUpSQLite(t *testing.T) {
	// tmp 保存tmp，供当前处理流程使用
	tmp := t.TempDir()
	// dbPath 保存db路径，供当前处理流程使用
	dbPath := filepath.Join(tmp, "rollback.db")
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// d、err 保存d、err，供当前处理流程使用
	d, _, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	if // err 保存err，供当前处理流程使用
	err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	goose.SetBaseFS(migrationsFS)
	// 读取当前最新迁移版本，动态回滚到 13，避免新增迁移后固定次数失效。
	// version、err 保存当前迁移版本及读取错误。
	version, err := goose.GetDBVersion(d)
	if err != nil {
		t.Fatalf("get migration version: %v", err)
	}
	// i 表示本次回滚操作序号。
	for i := 0; version >= 14; i++ {
		if // err 保存当前迁移回滚错误，任一方言步骤失败即终止验证。
		err := goose.Down(d, "migrations/sqlite"); err != nil {
			t.Fatalf("down migration #%d: %v", i+1, err)
		}
		version, err = goose.GetDBVersion(d)
		if err != nil {
			t.Fatalf("get migration version after down: %v", err)
		}
	}
	if columnExists(t, d, "notification_channels", "event_types") {
		t.Fatal("event_types should be removed after migration 14 down")
	}
	if tableExists(t, d, "risk_control_logs") {
		t.Fatal("risk_control_logs should be removed after migration 14 down")
	}
	if columnExists(t, d, "default_reply_records", "status") {
		t.Fatal("default_reply_records.status should be removed after migration 16 down")
	}
	if columnExists(t, d, "account_tokens", "cookie_fingerprint") {
		t.Fatal("account_tokens.cookie_fingerprint should be removed after migration 22 down")
	}
	if columnExists(t, d, "item_publish_batch_rows", "category_json") {
		t.Fatal("item_publish_batch_rows.category_json should be removed after migration 23 down")
	}
	if columnExists(t, d, "item_info", "deleted_at") || columnExists(t, d, "automation_rules", "deleted_at") {
		t.Fatal("soft-delete columns should be removed after migration 26 down")
	}
	if columnExists(t, d, "orders", "deleted_at") {
		t.Fatal("orders.deleted_at should be removed after migration 27 down")
	}
	if columnExists(t, d, "item_publish_batches", "location_json") {
		t.Fatal("item_publish_batches.location_json should be removed after migration 28 down")
	}
	// table 表示当前遍历过程中的table
	for _, table := range []string{"account_task_settings", "account_task_runs", "chat_sessions", "chat_messages"} {
		if tableExists(t, d, table) {
			t.Fatalf("table should be removed after migration 24 down: %s", table)
		}
	}

	if // err 保存err，供当前处理流程使用
	err := goose.Up(d, "migrations/sqlite"); err != nil {
		t.Fatalf("up after down: %v", err)
	}
	// c 表示当前遍历过程中的c
	for _, c := range []struct {
		table string
		col   string
	}{
		{"notification_channels", "event_types"},
		{"message_notifications", "event_types"},
		{"scheduled_cookies_refresh_log", "step_details"},
		{"scheduled_login_renew_log", "updated_cookie_count"},
		{"scheduled_api_cookie_renew_log", "request_count"},
		{"risk_control_logs", "processing_status"},
		{"default_reply_records", "status"},
		{"default_reply_records", "text_sent"},
		{"account_tokens", "cookie_fingerprint"},
		{"item_publish_batch_rows", "category_json"},
		{"item_publish_batches", "location_json"},
		{"account_task_settings", "auto_rate_enabled"},
		{"account_task_runs", "run_key"},
		{"chat_sessions", "unread_count"},
		{"chat_messages", "message_key"},
		{"chat_messages", "read_status"},
		{"chat_messages", "read_at"},
		{"notification_outbox", "uncertain_at"},
	} {
		if !columnExists(t, d, c.table, c.col) {
			t.Fatalf("column missing after re-up: %s.%s", c.table, c.col)
		}
	}
	// val 保存val，供当前处理流程使用
	var val string
	if // err 保存err，供当前处理流程使用
	err := d.QueryRow(`SELECT value FROM system_settings WHERE key='renewal_log_retention_days'`).Scan(&val); err != nil || val != "10" {
		t.Fatalf("renewal_log_retention_days after re-up: val=%q err=%v", val, err)
	}
}

// tableExists 负责tableExists相关处理。
func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	// name 保存名称，供当前处理流程使用
	var name string
	// err 保存err，供当前处理流程使用
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if err != nil {
		return false
	}
	return name == table
}
