package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"

	"xianyu-go/internal/deliverytemplate"
)

// ErrDeliveryTemplateUnavailable 表示规则写入时引用的发货模板已不存在、被删除或不属于当前用户。
var ErrDeliveryTemplateUnavailable = errors.New("发货模板不存在或已不可用")

// lockAutomationRuleAndTemplateRefsTx 锁定待更新规则并读取其既有模板引用。
func lockAutomationRuleAndTemplateRefsTx(ctx context.Context, tx *sql.Tx, dialect Dialect, userID, ruleID int64) (map[int64]struct{}, error) {
	if dialect == DialectSQLite {
		// lockQuery 通过不改变业务值的更新取得 SQLite 规则写锁。
		lockQuery := `UPDATE automation_rules SET updated_at=updated_at WHERE id=? AND user_id=? AND deleted_at IS NULL`
		// result、err 保存 SQLite 规则锁定结果及数据库错误。
		result, err := tx.ExecContext(ctx, lockQuery, ruleID, userID)
		if err != nil {
			return nil, err
		}
		// affected、affectedErr 保存规则写锁命中行数及读取错误。
		if affected, affectedErr := result.RowsAffected(); affectedErr != nil {
			return nil, affectedErr
		} else if affected != 1 {
			return nil, ErrNotFound
		}
	} else {
		// lockedID 保存行锁确认后的规则主键。
		var lockedID int64
		// query 保存数据库行锁查询语句。
		query := `SELECT id FROM automation_rules WHERE id=? AND user_id=? AND deleted_at IS NULL FOR UPDATE`
		// err 保存行锁查询错误。
		if err := tx.QueryRowContext(ctx, query, ruleID, userID).Scan(&lockedID); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return nil, ErrNotFound
			}
			return nil, err
		}
	}
	// rows 保存当前规则既有模板引用查询游标。
	// rows、err 保存既有模板引用查询游标及查询错误。
	rows, err := tx.QueryContext(ctx, `SELECT DISTINCT delivery_template_id FROM automation_rule_actions WHERE rule_id=? AND action_type='send_template' AND delivery_template_id IS NOT NULL`, ruleID)
	if err != nil {
		return nil, err
	}
	// retained 保存更新前规则已经引用的模板主键集合。
	retained := make(map[int64]struct{})
	for rows.Next() {
		// templateID 保存当前规则动作引用的模板主键。
		var templateID int64
		// err 保存模板引用主键扫描错误。
		if err := rows.Scan(&templateID); err != nil {
			rows.Close()
			return nil, err
		}
		if templateID > 0 {
			retained[templateID] = struct{}{}
		}
	}
	// rowsErr 保存既有模板引用游标遍历错误。
	rowsErr := rows.Err()
	// closeErr 保存既有模板引用游标关闭错误。
	closeErr := rows.Close()
	if rowsErr != nil {
		return nil, rowsErr
	}
	if closeErr != nil {
		return nil, closeErr
	}
	return retained, nil
}

// deliveryTemplateIDsFromActions 提取动作引用的模板 ID，并去重排序以固定跨事务锁顺序。
func deliveryTemplateIDsFromActions(actions []AutomationActionInput) []int64 {
	// seen 保存已经出现的正数模板 ID，避免同一事务重复请求同一行锁。
	seen := make(map[int64]struct{})
	for /* action 表示当前待检查的规则动作。 */ _, action := range actions {
		if action.DeliveryTemplateID > 0 {
			seen[action.DeliveryTemplateID] = struct{}{}
		}
	}
	// templateIDs 保存去重后的模板 ID，并按升序作为所有写事务的统一锁顺序。
	templateIDs := make([]int64, 0, len(seen))
	for /* templateID 表示当前已发现的模板主键。 */ templateID := range seen {
		templateIDs = append(templateIDs, templateID)
	}
	sort.Slice(templateIDs /* left、right 表示待比较的模板 ID 下标。 */, func(left, right int) bool { return templateIDs[left] < templateIDs[right] })
	return templateIDs
}

// lockLiveDeliveryTemplatesTx 在规则写事务内按固定顺序锁定并校验当前用户的有效模板。
// MySQL/PostgreSQL 使用行锁；SQLite 先执行同值更新取得写锁，再复用同一事务校验模板状态。
func lockLiveDeliveryTemplatesTx(ctx context.Context, tx *sql.Tx, dialect Dialect, userID int64, templateIDs []int64) error {
	// sortedIDs 保存去重后的升序模板 ID，保证调用方传入顺序不会影响锁顺序。
	sortedIDs := append([]int64(nil), templateIDs...)
	sort.Slice(sortedIDs /* left、right 表示待比较的模板 ID 下标。 */, func(left, right int) bool { return sortedIDs[left] < sortedIDs[right] })
	// uniqueIDs 保存去重后的正数模板 ID，非法零值不构成数据库引用。
	uniqueIDs := make([]int64, 0, len(sortedIDs))
	for /* templateIndex 表示排序后模板 ID 的位置。 */ templateIndex, templateID := range sortedIDs {
		if templateID <= 0 || (templateIndex > 0 && templateID == sortedIDs[templateIndex-1]) {
			continue
		}
		uniqueIDs = append(uniqueIDs, templateID)
	}
	if len(uniqueIDs) == 0 {
		return nil
	}
	// placeholders 保存本次查询按模板 ID 数量生成的参数占位符。
	placeholders := make([]string, 0, len(uniqueIDs))
	// args 保存用户归属和模板主键查询参数，顺序与占位符严格对应。
	args := make([]any, 0, len(uniqueIDs)+1)
	args = append(args, userID)
	for /* templateID 表示按升序取得锁的模板主键。 */ _, templateID := range uniqueIDs {
		placeholders = append(placeholders, "?")
		args = append(args, templateID)
	}
	// whereIDs 保存只由固定 SQL 占位符组成的模板 ID 条件。
	whereIDs := strings.Join(placeholders, ",")
	if dialect == DialectSQLite {
		// lockQuery 通过不改变业务值的更新取得 SQLite 写锁，避免检查与规则写入之间被软删除插入窗口。
		lockQuery := "UPDATE delivery_templates SET enabled=enabled WHERE user_id=? AND deleted_at IS NULL AND id IN (" + whereIDs + ")"
		// execErr 保存 SQLite 获取模板写锁时的数据库错误。
		if _, execErr := tx.ExecContext(ctx, lockQuery, args...); execErr != nil {
			return execErr
		}
	}
	// selectQuery 在行锁数据库中追加 FOR UPDATE；SQLite 已在同一事务中取得写锁，不能使用该语法。
	selectQuery := "SELECT id FROM delivery_templates WHERE user_id=? AND deleted_at IS NULL AND id IN (" + whereIDs + ") ORDER BY id ASC"
	if dialect == DialectMySQL || dialect == DialectPostgres {
		selectQuery += " FOR UPDATE"
	}
	// rows 保存当前事务锁定并验证后的模板行。
	rows, err := tx.QueryContext(ctx, selectQuery, args...)
	if err != nil {
		return err
	}
	defer rows.Close()
	// lockedIDs 保存数据库实际返回的有效模板 ID，用于检测缺失、越权和已删除模板。
	lockedIDs := make([]int64, 0, len(uniqueIDs))
	for rows.Next() {
		// templateID 保存当前锁定模板的数据库主键。
		var templateID int64
		// scanErr 保存锁定模板主键扫描错误。
		if scanErr := rows.Scan(&templateID); scanErr != nil {
			return scanErr
		}
		lockedIDs = append(lockedIDs, templateID)
	}
	// rowsErr 保存锁定模板结果遍历错误。
	if rowsErr := rows.Err(); rowsErr != nil {
		rows.Close()
		return rowsErr
	}
	// closeErr 保存锁定模板游标关闭错误，避免驱动错误被吞掉。
	if closeErr := rows.Close(); closeErr != nil {
		return closeErr
	}
	if len(lockedIDs) != len(uniqueIDs) {
		return ErrDeliveryTemplateUnavailable
	}
	for /* templateIndex 表示按升序对比模板主键的位置。 */ templateIndex, templateID := range uniqueIDs {
		if lockedIDs[templateIndex] != templateID {
			return ErrDeliveryTemplateUnavailable
		}
	}
	return nil
}

// validateAutomationTemplateContractsTx 在规则写事务内复核模板变量契约，防止应用层读取后模板被并发更新。
func validateAutomationTemplateContractsTx(ctx context.Context, tx *sql.Tx, dialect Dialect, userID int64, actions []AutomationActionInput, allowedDisabledTemplateIDs map[int64]struct{}) error {
	// templateIDs 保存规则动作引用的模板主键，并负责先取得固定顺序的行锁。
	templateIDs := deliveryTemplateIDsFromActions(actions)
	// lockErr 保存模板行锁定及有效性检查错误。
	if lockErr := lockLiveDeliveryTemplatesTx(ctx, tx, dialect, userID, templateIDs); lockErr != nil {
		return lockErr
	}
	for /* templateID 表示当前规则引用的模板主键。 */ _, templateID := range templateIDs {
		// enabled 保存事务锁定后模板当前是否允许新规则引用。
		var enabled int
		// err 保存模板状态读取错误。
		if err := tx.QueryRowContext(ctx, `SELECT enabled FROM delivery_templates WHERE id=? AND user_id=? AND deleted_at IS NULL`, templateID, userID).Scan(&enabled); err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return ErrDeliveryTemplateUnavailable
			}
			return err
		}
		if enabled == 0 {
			// _, retained 表示停用模板是否已经被正在更新的旧规则引用。
			_, retained := allowedDisabledTemplateIDs[templateID]
			if !retained {
				return ErrDeliveryTemplateUnavailable
			}
		}
	}
	for /* action 表示当前待复核模板契约的规则动作。 */ _, action := range actions {
		if action.ActionType != "send_template" || action.DeliveryTemplateID <= 0 {
			continue
		}
		// rows 保存锁定模板的最新消息，供事务内变量解析使用。
		rows, err := tx.QueryContext(ctx, `SELECT content FROM delivery_template_messages WHERE template_id=? ORDER BY sort_order ASC,id ASC`, action.DeliveryTemplateID)
		if err != nil {
			return err
		}
		// messages 保存事务快照中的模板消息正文。
		messages := make([]string, 0)
		for rows.Next() {
			// content 保存当前模板消息正文。
			var content string
			// scanErr 保存模板消息正文扫描错误。
			if scanErr := rows.Scan(&content); scanErr != nil {
				rows.Close()
				return scanErr
			}
			messages = append(messages, content)
		}
		// rowsErr 保存模板消息遍历错误。
		rowsErr := rows.Err()
		// closeErr 保存模板消息游标关闭错误。
		closeErr := rows.Close()
		if rowsErr != nil {
			return rowsErr
		}
		if closeErr != nil {
			return closeErr
		}
		// parsed 保存事务内解析出的模板变量集合。
		parsed, parseErr := deliverytemplate.Parse(messages)
		if parseErr != nil {
			return ErrDeliveryTemplateUnavailable
		}
		if !sameStringSet(parsed.Keys, templateBindingKeys(action.TemplateBindings)) {
			return ErrDeliveryTemplateUnavailable
		}
		// customVariables 保存新旧格式动作配置中的自定义变量值。
		customVariables := action.CustomVariables
		if customVariables == nil {
			customVariables = customVariablesFromActionConfig(action.ConfigJSON)
		}
		for /* key 表示模板要求填写的自定义变量键。 */ _, key := range parsed.CustomKeys {
			if strings.TrimSpace(customVariables[key]) == "" {
				return ErrDeliveryTemplateUnavailable
			}
		}
	}
	return nil
}

// customVariablesFromActionConfig 读取历史数组和当前对象格式的自定义变量。
func customVariablesFromActionConfig(configJSON string) map[string]string {
	// rawConfig 保存动作配置中的字段原文。
	var rawConfig map[string]json.RawMessage
	if json.Unmarshal([]byte(configJSON), &rawConfig) != nil {
		return nil
	}
	// values 保存当前对象格式的自定义变量。
	var values map[string]string
	if json.Unmarshal(rawConfig["custom_variables"], &values) == nil && values != nil {
		return values
	}
	// legacyValues 保存历史数组格式的自定义变量。
	var legacyValues []string
	if json.Unmarshal(rawConfig["custom_variables"], &legacyValues) != nil {
		return nil
	}
	// converted 保存按历史数组下标转换的变量值。
	converted := make(map[string]string, len(legacyValues))
	for /* index 表示历史自定义变量数组下标；value 表示变量值。 */ index, value := range legacyValues {
		converted[strconv.Itoa(index)] = value
	}
	return converted
}

// templateBindingKeys 提取规则动作中的模板绑定变量键，供事务内契约比较使用。
func templateBindingKeys(bindings []DeliveryTemplateBinding) []string {
	// keys 保存模板绑定按请求顺序出现的变量键。
	keys := make([]string, 0, len(bindings))
	for /* binding 表示当前模板变量绑定。 */ _, binding := range bindings {
		keys = append(keys, binding.VariableKey)
	}
	return keys
}
