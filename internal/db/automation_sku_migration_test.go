package db

import (
	"context"
	"encoding/json"
	"testing"
)

// TestMigratePendingAutomationSKURules 验证历史规则规格规范化、无效规则停用和重复迁移幂等。
func TestMigratePendingAutomationSKURules(t *testing.T) {
	// store、cleanup 保存迁移测试数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存迁移测试共用上下文。
	ctx := context.Background()
	// admin、err 保存测试用户创建结果。
	if _, err := store.Users.Create(ctx, "admin", "admin@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// admin、err 保存测试用户及读取错误。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// err 保存测试账号写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO cookies (id,value,user_id) VALUES ('sku-migration-cookie','cookie',?)`, admin.ID); err != nil {
		t.Fatal(err)
	}
	// err 保存测试商品写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('sku-migration-cookie','multi','多规格',1),('sku-migration-cookie','single','单规格',0)`); err != nil {
		t.Fatal(err)
	}
	// validRuleID、err 保存有效多规格规则写入结果。
	validRuleID, err := insertPendingSKURule(store, admin.ID, "multi", 1)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存有效规则动作写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_rule_actions (rule_id,action_type,config_json,enabled,sort_order) VALUES (?,?,?,?,?)`, validRuleID, "send_card", `{"spec_name":" 颜色；版本 ","spec_value":" 红；专业 "}`, 1, 1); err != nil {
		t.Fatal(err)
	}
	// invalidRuleID、err 保存规格不完整规则写入结果。
	invalidRuleID, err := insertPendingSKURule(store, admin.ID, "multi", 1)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存无效规则动作写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_rule_actions (rule_id,action_type,config_json,enabled,sort_order) VALUES (?,?,?,?,?)`, invalidRuleID, "send_card", `{"spec_name":"颜色；版本","spec_value":"红"}`, 1, 1); err != nil {
		t.Fatal(err)
	}
	// singleRuleID、err 保存单规格历史规则写入结果。
	singleRuleID, err := insertPendingSKURule(store, admin.ID, "single", 1)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存单规格规则动作写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_rule_actions (rule_id,action_type,config_json,enabled,sort_order) VALUES (?,?,?,?,?)`, singleRuleID, "send_card", `{"spec_name":"颜色","spec_value":"红"}`, 1, 1); err != nil {
		t.Fatal(err)
	}
	// err 保存首次迁移错误。
	if err := migratePendingAutomationSKURules(ctx, store.DB); err != nil {
		t.Fatal(err)
	}
	// status、enabled 保存有效规则迁移后的状态。
	var status string
	// enabled 保存迁移后规则启用标志。
	var enabled int
	// err 保存有效规则状态读取错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT sku_migration_status,enabled FROM automation_rules WHERE id=?`, validRuleID).Scan(&status, &enabled); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || enabled != 1 {
		t.Fatalf("有效规则状态错误 status=%q enabled=%d", status, enabled)
	}
	// configJSON 保存规范化后的规格配置。
	var configJSON string
	// err 保存有效动作配置读取错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT config_json FROM automation_rule_actions WHERE rule_id=?`, validRuleID).Scan(&configJSON); err != nil {
		t.Fatal(err)
	}
	// config 保存规范化后的动作配置对象。
	var config map[string]any
	// err 保存规范化配置解析错误。
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil || config["spec_name"] != "颜色；版本" || config["spec_value"] != "红；专业" {
		t.Fatalf("规格未规范化 config=%s err=%v", configJSON, err)
	}
	// err 保存无效规则状态读取错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT sku_migration_status,enabled FROM automation_rules WHERE id=?`, invalidRuleID).Scan(&status, &enabled); err != nil {
		t.Fatal(err)
	}
	if status != "needs_reconfiguration" || enabled != 0 {
		t.Fatalf("无效规则未隔离 status=%q enabled=%d", status, enabled)
	}
	// err 保存单规格动作配置读取错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT config_json FROM automation_rule_actions WHERE rule_id=?`, singleRuleID).Scan(&configJSON); err != nil {
		t.Fatal(err)
	}
	// err 保存单规格配置解析错误。
	if err := json.Unmarshal([]byte(configJSON), &config); err != nil || config["spec_name"] != "" || config["spec_value"] != "" {
		t.Fatalf("单规格规则未清除规格 config=%s err=%v", configJSON, err)
	}
	// err 保存重复迁移错误。
	if err := migratePendingAutomationSKURules(ctx, store.DB); err != nil {
		t.Fatal(err)
	}
	// err 保存重复迁移状态读取错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT sku_migration_status FROM automation_rules WHERE id=?`, validRuleID).Scan(&status); err != nil || status != "ready" {
		t.Fatalf("重复迁移破坏状态 status=%q err=%v", status, err)
	}
}

// insertPendingSKURule 写入待迁移规则并返回其主键。
func insertPendingSKURule(store *Store, userID int64, itemID string, enabled int) (int64, error) {
	// result、err 保存规则插入结果及数据库错误。
	result, err := store.DB.Exec(`INSERT INTO automation_rules (user_id,cookie_id,item_id,name,trigger_type,enabled,priority,config_json,sku_migration_status) VALUES (?,?,?,?,?,?,?,?,?)`, userID, "sku-migration-cookie", itemID, "迁移规则", "order_paid", enabled, 100, "{}", "pending")
	if err != nil {
		return 0, err
	}
	return result.LastInsertId()
}
