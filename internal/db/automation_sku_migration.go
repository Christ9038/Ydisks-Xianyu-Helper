package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"

	"xianyu-go/internal/orderspec"
)

// errInvalidLegacyAutomationSKUConfig 表示旧动作配置不是可迁移的 JSON 对象。
var errInvalidLegacyAutomationSKUConfig = errors.New("旧自动化动作配置不是 JSON 对象")

// pendingAutomationSKURule 保存待迁移规则及迁移时需要的商品事实。
type pendingAutomationSKURule struct {
	// id 是待迁移规则主键。
	id int64
	// itemID 是规则绑定商品标识。
	itemID string
	// itemExists 表示绑定商品是否仍存在。
	itemExists bool
	// multi 表示商品是否为多规格商品。
	multi bool
	// enabled 表示迁移前规则是否已启用。
	enabled bool
}

// pendingAutomationSKUAction 保存已从游标读取的动作快照，避免游标未关闭时执行更新。
type pendingAutomationSKUAction struct {
	// id 是动作主键。
	id int64
	// actionType 是动作类型。
	actionType string
	// rawConfig 是动作配置原文。
	rawConfig string
	// enabled 表示动作迁移前是否参与执行。
	enabled bool
}

// automationSKUMigrationState 保存单条规则迁移期间逐动作累积的规格契约状态。
type automationSKUMigrationState struct {
	// status 是规则迁移后的状态；needs_reconfiguration 表示不能安全自动执行。
	status string
	// expectedName 是已确认的规格名称顺序。
	expectedName string
	// expectedDimensions 是已确认的规格维度数量。
	expectedDimensions int
}

// automationSKUMigrationResult 保存单条规则迁移完成后的状态，供提交事务后的日志使用。
type automationSKUMigrationResult struct {
	// ruleID 是已迁移规则主键。
	ruleID int64
	// status 是迁移后的规则状态。
	status string
}

// migratePendingAutomationSKURules 将历史自动化规则迁移到完整规格契约。
func migratePendingAutomationSKURules(ctx context.Context, database *sql.DB) error {
	// tx、err 保存迁移事务及事务开启错误。
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// rules、err 保存待迁移规则快照及读取错误。
	rules, err := loadPendingAutomationSKURules(ctx, tx)
	if err != nil {
		return err
	}
	// logger 记录迁移结果；日志只包含规则主键和数量，不包含账号凭证。
	logger := slog.Default()
	// results 保存待事务提交后输出的规则迁移结果。
	results := make([]automationSKUMigrationResult, 0, len(rules))
	for /* rule 表示当前待迁移规则。 */ _, rule := range rules {
		// status、err 保存单条规则迁移状态及错误。
		status, err := migratePendingAutomationSKURule(ctx, tx, rule)
		if err != nil {
			return err
		}
		results = append(results, automationSKUMigrationResult{ruleID: rule.id, status: status})
	}
	// err 保存迁移事务提交错误。
	if err := tx.Commit(); err != nil {
		return err
	}
	if len(results) == 0 {
		return nil
	}
	// readyCount、reconfigureCount 保存两类迁移结果的数量。
	readyCount, reconfigureCount := 0, 0
	for /* result 表示当前已提交的规则迁移结果。 */ _, result := range results {
		if result.status == "needs_reconfiguration" {
			reconfigureCount++
			logger.Warn("自动化多 SKU 规则需要重新配置，已停用规则", "rule_id", result.ruleID)
			continue
		}
		readyCount++
	}
	logger.Info("自动化多 SKU 规则迁移完成", "total", len(results), "ready", readyCount, "needs_reconfiguration", reconfigureCount)
	return nil
}

// loadPendingAutomationSKURules 读取待迁移规则和商品的非敏感规格事实。
func loadPendingAutomationSKURules(ctx context.Context, tx *sql.Tx) ([]pendingAutomationSKURule, error) {
	// rows、err 保存待迁移规则查询游标及查询错误。
	rows, err := tx.QueryContext(ctx, `
SELECT r.id,r.item_id,COALESCE(i.id,0),COALESCE(i.is_multi_spec,0),COALESCE(r.enabled,0)
  FROM automation_rules r
  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id AND i.deleted_at IS NULL
 WHERE r.sku_migration_status='pending' AND r.deleted_at IS NULL`)
	if err != nil {
		return nil, err
	}
	// rules 保存关闭游标后供迁移阶段使用的规则快照。
	rules := make([]pendingAutomationSKURule, 0)
	for rows.Next() {
		// rule 保存当前规则迁移事实。
		var rule pendingAutomationSKURule
		// itemID 保存数据库返回的商品标识。
		var itemID string
		// itemRowID、multi、enabled 保存商品存在性、多规格标志和原启用状态。
		var itemRowID, multi, enabled int
		// err 保存规则字段扫描错误。
		if err := rows.Scan(&rule.id, &itemID, &itemRowID, &multi, &enabled); err != nil {
			_ = rows.Close()
			return nil, err
		}
		rule.itemID, rule.itemExists, rule.multi, rule.enabled = itemID, itemRowID > 0, multi != 0, enabled != 0
		rules = append(rules, rule)
	}
	// rowsErr 保存规则游标遍历错误。
	rowsErr := rows.Err()
	// closeErr 保存规则游标关闭错误。
	closeErr := rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return rules, nil
}

// loadPendingAutomationSKUActions 读取规则动作快照，并保留动作启用状态。
func loadPendingAutomationSKUActions(ctx context.Context, tx *sql.Tx, ruleID int64) ([]pendingAutomationSKUAction, error) {
	// rows、err 保存当前规则动作游标及查询错误。
	rows, err := tx.QueryContext(ctx, `SELECT id,action_type,config_json,COALESCE(enabled,0) FROM automation_rule_actions WHERE rule_id=? ORDER BY sort_order,id`, ruleID)
	if err != nil {
		return nil, err
	}
	// actions 保存关闭游标后供迁移阶段使用的动作快照。
	actions := make([]pendingAutomationSKUAction, 0)
	for rows.Next() {
		// actionID 保存当前动作主键。
		var actionID int64
		// actionType、rawConfig、actionEnabled 保存动作类型、配置原文和启用标志。
		var actionType, rawConfig string
		// actionEnabled 保存动作迁移前是否启用。
		var actionEnabled int
		// err 保存动作字段扫描错误。
		if err := rows.Scan(&actionID, &actionType, &rawConfig, &actionEnabled); err != nil {
			_ = rows.Close()
			return nil, err
		}
		actions = append(actions, pendingAutomationSKUAction{id: actionID, actionType: actionType, rawConfig: rawConfig, enabled: actionEnabled != 0})
	}
	// rowsErr 保存动作游标遍历错误。
	rowsErr := rows.Err()
	// closeErr 保存动作游标关闭错误。
	closeErr := rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return actions, nil
}

// migratePendingAutomationSKURule 迁移一条规则的启用发货动作，并写回规则状态。
func migratePendingAutomationSKURule(ctx context.Context, tx *sql.Tx, rule pendingAutomationSKURule) (string, error) {
	// actions、err 保存当前规则动作快照及查询错误。
	actions, err := loadPendingAutomationSKUActions(ctx, tx, rule.id)
	if err != nil {
		return "", err
	}
	// state 保存当前规则逐动作累积的规格契约状态。
	state := automationSKUMigrationState{status: "ready"}
	for /* action 表示当前规则的动作快照。 */ _, action := range actions {
		if !action.enabled || (action.actionType != "send_card" && action.actionType != "send_template") {
			continue
		}
		// config、name、value 保存动作配置及其规格字段；actionErr 表示配置校验错误。
		config, name, value, actionErr := legacyAutomationSKUConfig(action.rawConfig)
		if actionErr != nil {
			state.status = "needs_reconfiguration"
			continue
		}
		if !rule.itemExists && rule.itemID != "" {
			state.status = "needs_reconfiguration"
			continue
		}
		if !rule.multi {
			if strings.TrimSpace(name) != "" || strings.TrimSpace(value) != "" {
				config["spec_name"], config["spec_value"] = "", ""
				// err 保存单规格动作配置清理错误。
				if err := updateAutomationSKUActionConfig(ctx, tx, action.id, config); err != nil {
					return "", err
				}
			}
			continue
		}
		// normalized、normalizeErr 保存完整规格规范化结果及校验错误。
		normalized, normalizeErr := orderspec.NormalizeColumns(name, value)
		if normalizeErr != nil || normalized.Dimensions == 0 {
			state.status = "needs_reconfiguration"
			continue
		}
		if state.expectedName != "" && (state.expectedName != normalized.Name || state.expectedDimensions != normalized.Dimensions) {
			state.status = "needs_reconfiguration"
			continue
		}
		if state.expectedName == "" {
			state.expectedName, state.expectedDimensions = normalized.Name, normalized.Dimensions
		}
		config["spec_name"], config["spec_value"] = normalized.Name, normalized.Value
		// err 保存多规格动作配置规范化写回错误。
		if err := updateAutomationSKUActionConfig(ctx, tx, action.id, config); err != nil {
			return "", err
		}
	}
	// enabled 保存迁移后规则启用状态；无效规则必须停用，有效规则保留原状态。
	enabled := boolToInt(rule.enabled)
	if state.status == "needs_reconfiguration" {
		enabled = 0
	}
	// err 保存规则状态写回错误。
	if _, err := tx.ExecContext(ctx, `UPDATE automation_rules SET sku_migration_status=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, state.status, enabled, rule.id); err != nil {
		return "", err
	}
	return state.status, nil
}

// legacyAutomationSKUConfig 解析旧动作配置，并拒绝 null、数组等非对象 JSON。
func legacyAutomationSKUConfig(rawConfig string) (map[string]any, string, string, error) {
	// config 保存动作配置对象。
	var config map[string]any
	// err 保存动作配置 JSON 解析错误。
	if err := json.Unmarshal([]byte(rawConfig), &config); err != nil || config == nil {
		return nil, "", "", errInvalidLegacyAutomationSKUConfig
	}
	// name、value 保存旧动作中的规格名称和值。
	name, _ := config["spec_name"].(string)
	// value 保存旧动作中的规格值。
	value, _ := config["spec_value"].(string)
	return config, name, value, nil
}

// updateAutomationSKUActionConfig 将迁移后的动作配置以 JSON 对象写回动作表。
func updateAutomationSKUActionConfig(ctx context.Context, tx *sql.Tx, actionID int64, config map[string]any) error {
	// encoded、err 保存动作配置编码结果及编码错误。
	encoded, err := json.Marshal(config)
	if err != nil {
		return err
	}
	// err 保存动作配置写回错误。
	if _, err := tx.ExecContext(ctx, `UPDATE automation_rule_actions SET config_json=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, string(encoded), actionID); err != nil {
		return err
	}
	return nil
}
