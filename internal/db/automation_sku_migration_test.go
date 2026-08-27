package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/pressly/goose/v3"
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
	// disabledRuleID、err 保存迁移前已停用的有效规则，验证迁移不会擅自启用它。
	disabledRuleID, err := insertPendingSKURule(store, admin.ID, "multi", 0)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存停用有效规则动作写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_rule_actions (rule_id,action_type,config_json,enabled,sort_order) VALUES (?,?,?,?,?)`, disabledRuleID, "send_card", `{"spec_name":"套餐","spec_value":"标准"}`, 1, 1); err != nil {
		t.Fatal(err)
	}
	// nullConfigRuleID、err 保存配置为 JSON null 的旧规则，验证迁移只隔离而不触发 panic。
	nullConfigRuleID, err := insertPendingSKURule(store, admin.ID, "multi", 1)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存 null 配置动作写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_rule_actions (rule_id,action_type,config_json,enabled,sort_order) VALUES (?,?,?,?,?)`, nullConfigRuleID, "send_card", `null`, 1, 1); err != nil {
		t.Fatal(err)
	}
	// disabledActionRuleID、err 保存含停用坏动作和启用有效动作的规则，验证停用动作不阻断迁移。
	disabledActionRuleID, err := insertPendingSKURule(store, admin.ID, "multi", 1)
	if err != nil {
		t.Fatal(err)
	}
	// err 保存停用坏动作写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_rule_actions (rule_id,action_type,config_json,enabled,sort_order) VALUES (?,?,?,?,?)`, disabledActionRuleID, "send_card", `null`, 0, 1); err != nil {
		t.Fatal(err)
	}
	// err 保存启用有效动作写入错误。
	if _, err := store.DB.ExecContext(ctx, `INSERT INTO automation_rule_actions (rule_id,action_type,config_json,enabled,sort_order) VALUES (?,?,?,?,?)`, disabledActionRuleID, "send_card", `{"spec_name":"套餐","spec_value":"标准"}`, 1, 2); err != nil {
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
	// err 保存迁移前停用规则的状态读取错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT sku_migration_status,enabled FROM automation_rules WHERE id=?`, disabledRuleID).Scan(&status, &enabled); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || enabled != 0 {
		t.Fatalf("迁移不应启用原本停用的规则 status=%q enabled=%d", status, enabled)
	}
	// err 保存 null 配置规则的状态读取错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT sku_migration_status,enabled FROM automation_rules WHERE id=?`, nullConfigRuleID).Scan(&status, &enabled); err != nil {
		t.Fatal(err)
	}
	if status != "needs_reconfiguration" || enabled != 0 {
		t.Fatalf("null 配置规则未被安全隔离 status=%q enabled=%d", status, enabled)
	}
	// err 保存含停用坏动作规则的状态读取错误。
	if err := store.DB.QueryRowContext(ctx, `SELECT sku_migration_status,enabled FROM automation_rules WHERE id=?`, disabledActionRuleID).Scan(&status, &enabled); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || enabled != 1 {
		t.Fatalf("停用坏动作不应阻断有效规则 status=%q enabled=%d", status, enabled)
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

// TestMigrateLegacyAutomationSKUSchema 验证从 00039 旧库升级时会先落地状态列，再迁移旧规则。
func TestMigrateLegacyAutomationSKUSchema(t *testing.T) {
	// database 保存只迁移到 00039 的旧版 SQLite 数据库连接。
	database, openErr := sql.Open("sqlite", sqliteDSN(filepath.Join(t.TempDir(), "legacy-sku.db")))
	if openErr != nil {
		t.Fatalf("打开旧数据库失败: %v", openErr)
	}
	defer database.Close()
	// ctx 保存旧库升级测试共用上下文。
	ctx := context.Background()
	// err 保存 Goose 方言设置错误。
	if err := goose.SetDialect("sqlite3"); err != nil {
		t.Fatal(err)
	}
	goose.SetBaseFS(migrationsFS)
	// err 保存旧版 00001 至 00039 schema 的落地错误。
	if err := goose.UpTo(database, "migrations/sqlite", 39); err != nil {
		t.Fatalf("落地旧版 schema 失败: %v", err)
	}
	// err 保存旧规则写入错误；动作配置模拟旧版本已经保存的多 SKU 组合。
	if _, err := database.ExecContext(ctx, `INSERT INTO users (id,username,email,password_hash) VALUES (1,'legacy-admin','legacy@example.com','hash')`); err != nil {
		t.Fatal(err)
	}
	// err 保存旧 Cookie 写入错误。
	if _, err := database.ExecContext(ctx, `INSERT INTO cookies (id,value,user_id) VALUES ('legacy-cookie','legacy-cookie-value',1)`); err != nil {
		t.Fatal(err)
	}
	// err 保存旧商品规格事实写入错误。
	if _, err := database.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES ('legacy-cookie','legacy-item','旧多规格商品',1)`); err != nil {
		t.Fatal(err)
	}
	// result、err 保存旧规则插入结果及错误。
	result, err := database.ExecContext(ctx, `INSERT INTO automation_rules (user_id,cookie_id,item_id,name,trigger_type,enabled,priority,config_json) VALUES (1,'legacy-cookie','legacy-item','旧规则','order_paid',0,100,'{}')`)
	if err != nil {
		t.Fatal(err)
	}
	// ruleID 保存旧规则在升级前的主键。
	ruleID, err := result.LastInsertId()
	if err != nil {
		t.Fatal(err)
	}
	// err 保存旧规则动作写入错误。
	if _, err := database.ExecContext(ctx, `INSERT INTO automation_rule_actions (rule_id,action_type,config_json,enabled,sort_order) VALUES (?,?,?,?,?)`, ruleID, "send_card", `{"spec_name":" 套餐 ","spec_value":" 标准 "}`, 1, 1); err != nil {
		t.Fatal(err)
	}
	// err 保存执行完整升级（含 00040 和数据迁移）的错误。
	if err := Migrate(ctx, database, DialectSQLite); err != nil {
		t.Fatalf("升级旧库失败: %v", err)
	}
	// status、enabled 保存升级后的状态和原规则启用标志。
	var status string
	// enabled 保存升级后规则的启用标志。
	var enabled int
	// err 保存升级后规则状态读取错误。
	if err := database.QueryRowContext(ctx, `SELECT sku_migration_status,enabled FROM automation_rules WHERE id=?`, ruleID).Scan(&status, &enabled); err != nil {
		t.Fatal(err)
	}
	if status != "ready" || enabled != 0 {
		t.Fatalf("升级不应重新启用旧规则: status=%q enabled=%d", status, enabled)
	}
}

// TestAutomationMatchOnlyReturnsReadySKURules 验证待迁移和待重配规则不会进入自动发货匹配结果。
func TestAutomationMatchOnlyReturnsReadySKURules(t *testing.T) {
	// store、cleanup 保存隔离的自动化规则测试数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存规则匹配测试共用上下文。
	ctx := context.Background()
	// user、err 保存测试用户及创建错误。
	user, err := store.Users.Create(ctx, "sku-match-admin", "sku-match@example.com", "password")
	if err != nil || !user {
		t.Fatalf("创建测试用户失败: created=%v err=%v", user, err)
	}
	// account、err 保存测试账号及读取错误。
	account, err := store.Users.GetByUsername(ctx, "sku-match-admin")
	if err != nil {
		t.Fatal(err)
	}
	// err 保存测试 Cookie 写入错误。
	if err := store.Cookies.Save(ctx, "sku-match-cookie", "cookie-value", account.ID); err != nil {
		t.Fatal(err)
	}
	// readyID、pendingID、reconfigureID 保存三种 SKU 迁移状态的规则主键。
	readyID, err := store.Automation.Create(ctx, AutomationRuleInput{UserID: account.ID, CookieID: "sku-match-cookie", ItemID: "sku-match-item", Name: "ready", TriggerType: "order_paid", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// err 保存待迁移规则创建错误。
	pendingID, err := store.Automation.Create(ctx, AutomationRuleInput{UserID: account.ID, CookieID: "sku-match-cookie", ItemID: "sku-match-item", Name: "pending", TriggerType: "order_paid", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// err 保存待重配规则创建错误。
	reconfigureID, err := store.Automation.Create(ctx, AutomationRuleInput{UserID: account.ID, CookieID: "sku-match-cookie", ItemID: "sku-match-item", Name: "reconfigure", TriggerType: "order_paid", Enabled: true})
	if err != nil {
		t.Fatal(err)
	}
	// err 保存待迁移状态写入错误。
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_rules SET sku_migration_status='pending' WHERE id=?`, pendingID); err != nil {
		t.Fatal(err)
	}
	// err 保存待重配状态写入错误。
	if _, err := store.DB.ExecContext(ctx, `UPDATE automation_rules SET sku_migration_status='needs_reconfiguration' WHERE id=?`, reconfigureID); err != nil {
		t.Fatal(err)
	}
	// rules、err 保存按商品和付款触发器匹配出的规则。
	rules, err := store.Automation.Match(ctx, "sku-match-cookie", "sku-match-item", "order_paid")
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].ID != readyID {
		t.Fatalf("只有 ready 规则可匹配: rules=%+v", rules)
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
