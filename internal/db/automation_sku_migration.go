package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"xianyu-go/internal/orderspec"
)

// migratePendingAutomationSKURules 将历史自动化规则迁移到完整规格契约。
func migratePendingAutomationSKURules(ctx context.Context, database *sql.DB) error {
	// tx、err 保存迁移事务及事务开启错误。
	tx, err := database.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// rows、err 保存待迁移规则查询游标及查询错误。
	rows, err := tx.QueryContext(ctx, `
SELECT r.id,r.item_id,COALESCE(i.id,0),COALESCE(i.is_multi_spec,0)
  FROM automation_rules r
  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id AND i.deleted_at IS NULL
 WHERE r.sku_migration_status='pending' AND r.deleted_at IS NULL`)
	if err != nil {
		return err
	}
	// pendingRule 描述待迁移规则及其商品事实。
	type pendingRule struct {
		// id 是待迁移规则主键。
		id int64
		// itemID 是规则绑定商品标识。
		itemID string
		// itemExists 表示绑定商品是否仍存在。
		itemExists bool
		// multi 表示商品是否为多规格商品。
		multi bool
	}
	// rules 保存待迁移规则快照。
	var rules []pendingRule
	for rows.Next() {
		// rule 保存当前规则迁移状态。
		var rule pendingRule
		// itemID 保存数据库返回的商品标识。
		var itemID string
		// itemRowID、multi 保存商品存在性和多规格标志。
		var itemRowID, multi int
		// err 保存规则字段扫描错误。
		if err := rows.Scan(&rule.id, &itemID, &itemRowID, &multi); err != nil {
			rows.Close()
			return err
		}
		rule.itemID, rule.itemExists, rule.multi = itemID, itemRowID > 0, multi != 0
		rules = append(rules, rule)
	}
	// err 保存规则游标遍历错误。
	if err := rows.Err(); err != nil {
		rows.Close()
		return err
	}
	// err 保存规则游标关闭错误。
	// err 保存规则游标关闭错误。
	if err := rows.Close(); err != nil {
		return err
	}
	for /* rule 表示当前待迁移规则。 */ _, rule := range rules {
		// status 保存规则迁移后的契约状态。
		status := "ready"
		// expectedName、expectedDimensions 保存同一规则要求一致的规格列定义。
		var expectedName string
		// expectedDimensions 保存规格维度数量。
		var expectedDimensions int
		// actions、err 保存当前规则动作游标及查询错误。
		actions, err := tx.QueryContext(ctx, `SELECT id,action_type,config_json FROM automation_rule_actions WHERE rule_id=? ORDER BY sort_order,id`, rule.id)
		if err != nil {
			return err
		}
		// pendingAction 保存已从游标读取的动作，避免游标未关闭时执行同一事务的 UPDATE。
		type pendingAction struct {
			// id 是动作主键。
			id int64
			// actionType 是动作类型。
			actionType string
			// rawConfig 是动作配置原文。
			rawConfig string
		}
		// actionRows 保存当前规则动作快照。
		actionRows := make([]pendingAction, 0)
		for actions.Next() {
			// actionID 保存当前动作主键。
			var actionID int64
			// actionType、rawConfig 保存动作类型和配置原文。
			var actionType, rawConfig string
			// err 保存动作字段扫描错误。
			if err := actions.Scan(&actionID, &actionType, &rawConfig); err != nil {
				_ = actions.Close()
				return err
			}
			actionRows = append(actionRows, pendingAction{id: actionID, actionType: actionType, rawConfig: rawConfig})
		}
		// err 保存动作游标遍历错误。
		if err := actions.Err(); err != nil {
			_ = actions.Close()
			return err
		}
		// err 保存动作游标关闭错误。
		if err := actions.Close(); err != nil {
			return err
		}
		for /* action 表示当前规则的动作快照。 */ _, action := range actionRows {
			actionID, actionType, rawConfig := action.id, action.actionType, action.rawConfig
			if actionType != "send_card" && actionType != "send_template" {
				continue
			}
			// config 保存动作配置对象。
			var config map[string]any
			if json.Unmarshal([]byte(rawConfig), &config) != nil {
				status = "needs_reconfiguration"
				continue
			}
			// name、value 保存动作规格列原文。
			name, _ := config["spec_name"].(string)
			// value 保存动作规格值列。
			value, _ := config["spec_value"].(string)
			if !rule.itemExists && rule.itemID != "" {
				status = "needs_reconfiguration"
				continue
			}
			if !rule.multi {
				if strings.TrimSpace(name) != "" || strings.TrimSpace(value) != "" {
					config["spec_name"], config["spec_value"] = "", ""
					// encoded、marshalErr 保存清除规格后的配置 JSON 及编码错误。
					encoded, marshalErr := json.Marshal(config)
					if marshalErr != nil {
						actions.Close()
						return marshalErr
					}
					// err 保存清除规格写回错误。
					if _, err := tx.ExecContext(ctx, `UPDATE automation_rule_actions SET config_json=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, string(encoded), actionID); err != nil {
						actions.Close()
						return err
					}
				}
				continue
			}
			// normalized、normalizeErr 保存规范化规格及校验错误。
			normalized, normalizeErr := orderspec.NormalizeColumns(name, value)
			if normalizeErr != nil || normalized.Dimensions == 0 || (expectedName != "" && expectedName != normalized.Name) {
				status = "needs_reconfiguration"
				continue
			}
			if expectedName == "" {
				expectedName, expectedDimensions = normalized.Name, normalized.Dimensions
			} else if expectedDimensions != normalized.Dimensions {
				status = "needs_reconfiguration"
				continue
			}
			config["spec_name"], config["spec_value"] = normalized.Name, normalized.Value
			// encoded、marshalErr 保存规范化后的配置 JSON 及编码错误。
			encoded, marshalErr := json.Marshal(config)
			if marshalErr != nil {
				actions.Close()
				return marshalErr
			}
			// err 保存规范化规格写回错误。
			if _, err := tx.ExecContext(ctx, `UPDATE automation_rule_actions SET config_json=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, string(encoded), actionID); err != nil {
				actions.Close()
				return err
			}
		}
		// err 保存动作游标遍历错误。
		// enabled 保存迁移后规则启用标志，无效规则必须停用。
		enabled := 1
		if status == "needs_reconfiguration" {
			enabled = 0
		}
		// err 保存规则状态写回错误。
		if _, err := tx.ExecContext(ctx, `UPDATE automation_rules SET sku_migration_status=?,enabled=?,updated_at=CURRENT_TIMESTAMP WHERE id=?`, status, enabled, rule.id); err != nil {
			return err
		}
	}
	return tx.Commit()
}
