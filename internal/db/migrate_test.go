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
func TestMigrate_AppliesCleanSchema(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "test.db")

	ctx := context.Background()
	db, _, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer db.Close()

	checks := []struct {
		table string
		col   string
	}{
		{"orders", "system_shipped"},
		{"orders", "receiver_city"},
		{"orders", "version"},
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
	}
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
	db2, _, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("二次 Open 幂等失败: %v", err)
	}
	db2.Close()
}

func columnExists(t *testing.T, db *sql.DB, table, col string) bool {
	t.Helper()
	rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
	if err != nil {
		t.Fatalf("pragma_table_info(%s): %v", table, err)
	}
	defer rows.Close()
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			t.Fatalf("scan: %v", err)
		}
		if name == col {
			return true
		}
	}
	return false
}

func TestLatestMigrationsDownUpSQLite(t *testing.T) {
	tmp := t.TempDir()
	dbPath := filepath.Join(tmp, "rollback.db")
	ctx := context.Background()
	d, _, err := Open(ctx, dbPath)
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer d.Close()

	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatalf("goose dialect: %v", err)
	}
	goose.SetBaseFS(migrationsFS)
	// 依次回滚最新版本到 13，再整体升级。
	for i := 0; i < 13; i++ {
		if err := goose.Down(d, "migrations/sqlite"); err != nil {
			t.Fatalf("down migration #%d: %v", i+1, err)
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
	for _, table := range []string{"account_task_settings", "account_task_runs", "chat_sessions", "chat_messages"} {
		if tableExists(t, d, table) {
			t.Fatalf("table should be removed after migration 24 down: %s", table)
		}
	}

	if err := goose.Up(d, "migrations/sqlite"); err != nil {
		t.Fatalf("up after down: %v", err)
	}
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
		{"account_task_settings", "auto_rate_enabled"},
		{"account_task_runs", "run_key"},
		{"chat_sessions", "unread_count"},
		{"chat_messages", "message_key"},
	} {
		if !columnExists(t, d, c.table, c.col) {
			t.Fatalf("column missing after re-up: %s.%s", c.table, c.col)
		}
	}
	var val string
	if err := d.QueryRow(`SELECT value FROM system_settings WHERE key='renewal_log_retention_days'`).Scan(&val); err != nil || val != "10" {
		t.Fatalf("renewal_log_retention_days after re-up: val=%q err=%v", val, err)
	}
}

func tableExists(t *testing.T, db *sql.DB, table string) bool {
	t.Helper()
	var name string
	err := db.QueryRow(`SELECT name FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&name)
	if err != nil {
		return false
	}
	return name == table
}
