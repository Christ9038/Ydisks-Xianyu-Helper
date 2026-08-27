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
// TestMigrate_AppliesCleanSchema 封装TestMigrateAppliesCleanSchema业务协调。
func TestMigrate_AppliesCleanSchema(t *testing.T) {
	// tmp 用于本次流程后续判断的tmp
	tmp := t.TempDir()
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(tmp, "test.db")

	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// db、err 用于本次流程后续判断的db、err
	db, _, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	// checks 用于本次流程后续判断的checks
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
		{"automation_rules", "sku_migration_status"},
		{"default_replies", "reply_once"},
		{"default_reply_records", "status"},
		{"default_reply_records", "text_sent"},
		{"automation_runs", "action_cursor"},
		{"automation_runs", "action_started"},
		{"automation_runs", "delivery_proof"},
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
		{"order_reconciliations", "idempotency_key"},
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
	// db2、err 用于本次流程后续判断的db2、err
	db2, _, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("二次 Open 幂等失败: %v", err)
	}
	db2.Close()
}

// TestMigrate_ExistingAutomationRunsReceiveEmptyDeliveryProof 验证已有自动化运行升级后获得非 NULL 空凭证。
func TestMigrate_ExistingAutomationRunsReceiveEmptyDeliveryProof(t *testing.T) {
	// tmpDir 保存隔离的历史数据库目录，测试结束后由 testing 清理。
	tmpDir := t.TempDir()
	// dbPath 指向模拟已运行至 00038 的 SQLite 文件。
	dbPath := filepath.Join(tmpDir, "automation-v38.db")
	// rawDB、openErr 保存历史数据库连接和打开错误。
	rawDB, openErr := sql.Open("sqlite", sqliteDSN(dbPath))
	if openErr != nil {
		t.Fatalf("open legacy database: %v", openErr)
	}
	defer rawDB.Close()
	// dialectErr 保存 Goose SQLite 方言设置错误。
	if dialectErr := goose.SetDialect("sqlite3"); dialectErr != nil {
		t.Fatal(dialectErr)
	}
	goose.SetBaseFS(migrationsFS)
	// upErr 保存历史数据库推进到 00038 的错误。
	if upErr := goose.UpTo(rawDB, "migrations/sqlite", 38); upErr != nil {
		t.Fatalf("apply v38 migrations: %v", upErr)
	}
	// userResult、userErr 保存历史用户写入结果。
	userResult, userErr := rawDB.Exec(`INSERT INTO users (username,email,password_hash) VALUES ('migration-user','migration@example.com','hash')`)
	if userErr != nil {
		t.Fatal(userErr)
	}
	// userID、idErr 保存历史用户主键。
	userID, idErr := userResult.LastInsertId()
	if idErr != nil {
		t.Fatal(idErr)
	}
	// cookieErr 保存历史账号写入错误。
	if _, cookieErr := rawDB.Exec(`INSERT INTO cookies (id,value,user_id) VALUES ('migration-cookie','cv',?)`, userID); cookieErr != nil {
		t.Fatal(cookieErr)
	}
	// ruleResult、ruleErr 保存历史自动化规则写入结果。
	ruleResult, ruleErr := rawDB.Exec(`INSERT INTO automation_rules (user_id,cookie_id,item_id,name,trigger_type,enabled,priority,config_json) VALUES (?,?,?,?,?,1,100,'{}')`, userID, "migration-cookie", "migration-item", "migration-rule", "paid")
	if ruleErr != nil {
		t.Fatal(ruleErr)
	}
	// ruleID、ruleIDErr 保存历史规则主键。
	ruleID, ruleIDErr := ruleResult.LastInsertId()
	if ruleIDErr != nil {
		t.Fatal(ruleIDErr)
	}
	// runErr 保存没有 delivery_proof 列时写入历史运行的错误。
	if _, runErr := rawDB.Exec(`INSERT INTO automation_runs (rule_id,cookie_id,item_id,order_id,trigger_type,trigger_key,status) VALUES (?,?,?,?,?,?,?)`, ruleID, "migration-cookie", "migration-item", "migration-order", "paid", "migration-key", "running"); runErr != nil {
		t.Fatal(runErr)
	}
	// migrateErr 保存从 00038 升级到最新版本的错误。
	if migrateErr := Migrate(context.Background(), rawDB, DialectSQLite); migrateErr != nil {
		t.Fatalf("upgrade v38 database: %v", migrateErr)
	}
	// varProof、scanErr 保存升级后历史运行的凭证值和扫描错误。
	var varProof string
	// scanErr 保存升级后历史运行凭证读取错误。
	if scanErr := rawDB.QueryRow(`SELECT delivery_proof FROM automation_runs WHERE trigger_key='migration-key'`).Scan(&varProof); scanErr != nil {
		t.Fatal(scanErr)
	}
	if varProof != "" {
		t.Fatalf("历史运行凭证应为空: %q", varProof)
	}
	// finalVersion、versionErr 保存升级后的 Goose 版本和读取错误。
	finalVersion, versionErr := goose.GetDBVersion(rawDB)
	if versionErr != nil || finalVersion != 40 {
		t.Fatalf("final migration version=%d err=%v", finalVersion, versionErr)
	}
}

// TestMigrate_UpgradesDatabaseWithMainChatVersions 验证已发布 main 的 00029/00030
// 聊天迁移可以原样升级到包含快捷回复和买家备注的 00036 最终版本。
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
	// migrateErr 保存从 main 00030 接续 dev 00031 至 00036 时的迁移失败。
	if migrateErr := Migrate(ctx, rawDB, DialectSQLite); migrateErr != nil {
		t.Fatalf("upgrade from main 00030: %v", migrateErr)
	}
	if !tableExists(t, rawDB, "order_reconciliations") {
		t.Fatal("order_reconciliations should be created by the dev schema baseline migration")
	}
	if !columnExists(t, rawDB, "chat_messages", "read_status") || !columnExists(t, rawDB, "chat_messages", "read_at") || !columnExists(t, rawDB, "chat_messages", "media_duration") || !columnExists(t, rawDB, "chat_sessions", "item_image_url") {
		t.Fatal("chat read tracking and media presentation columns should remain after dev schema baseline upgrade")
	}
	if !tableExists(t, rawDB, "chat_quick_replies") || !tableExists(t, rawDB, "chat_buyer_notes") {
		t.Fatal("chat quick reply and buyer note tables should be created by the latest migration")
	}
	if !tableExists(t, rawDB, "delivery_templates") || !tableExists(t, rawDB, "delivery_template_messages") || !tableExists(t, rawDB, "automation_action_template_bindings") {
		t.Fatal("delivery template tables should be created by the latest migration")
	}
	if !columnExists(t, rawDB, "automation_rule_actions", "delivery_template_id") {
		t.Fatal("automation_rule_actions should reference delivery templates")
	}
	// finalVersion 验证迁移账本已推进到包含发货模板的最新 dev schema 版本。
	finalVersion, versionErr := goose.GetDBVersion(rawDB)
	if versionErr != nil {
		t.Fatalf("read final migration version: %v", versionErr)
	}
	if finalVersion != 40 {
		t.Fatalf("final migration version=%d, want 40", finalVersion)
	}
}

// columnExists 封装columnExists业务协调。
func columnExists(t *testing.T, db *sql.DB, table, col string) bool {
	t.Helper()
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		// name 用于本次流程后续判断的名称
		var name string
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == col {
			return true
		}
	}
	return false
}

// TestLatestMigrationsDownUpSQLite 封装TestLatestMigrationsDownUpSQLite业务协调。
func TestLatestMigrationsDownUpSQLite(t *testing.T) {
	// tmp 用于本次流程后续判断的tmp
	tmp := t.TempDir()
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(tmp, "rollback.db")
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// d、err 用于本次流程后续判断的d、err
	d, _, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	if // err 用于本次流程后续判断的err
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
	if columnExists(t, d, "automation_runs", "delivery_proof") {
		t.Fatal("automation_runs.delivery_proof should be removed after migration 39 down")
	}
	// table 表示当前遍历过程中的table
	for _, table := range []string{"account_task_settings", "account_task_runs", "chat_sessions", "chat_messages"} {
		if tableExists(t, d, table) {
			t.Fatalf("table should be removed after migration 24 down: %s", table)
		}
	}

	if // err 用于本次流程后续判断的err
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
		{"chat_sessions", "item_image_url"},
		{"chat_messages", "message_key"},
		{"chat_messages", "read_status"},
		{"chat_messages", "read_at"},
		{"chat_messages", "media_duration"},
		{"notification_outbox", "uncertain_at"},
		{"automation_runs", "delivery_proof"},
	} {
		if !columnExists(t, d, c.table, c.col) {
			t.Fatalf("column missing after re-up: %s.%s", c.table, c.col)
		}
	}
	// val 用于本次流程后续判断的val
	var val string
	if // err 用于本次流程后续判断的err
	err := d.QueryRow(`SELECT value FROM system_settings WHERE key='renewal_log_retention_days'`).Scan(&val); err != nil || val != "10" {
		t.Fatalf("renewal_log_retention_days after re-up: val=%q err=%v", val, err)
	}
}

// tableExists 封装tableExists业务协调。
func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	// name 用于本次流程后续判断的名称
	var name string
	// err 用于本次流程后续判断的err
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if err != nil {
		return false
	}
	return name == table
}
