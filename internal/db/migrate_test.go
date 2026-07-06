package db

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"
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
		{"default_replies", "reply_once"},
		{"users", "is_admin"},
		{"sessions", "session_id"},
		{"notification_channels", "user_id"},
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
