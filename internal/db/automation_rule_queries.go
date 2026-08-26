package db

import "context"

// ListPageForUser 按用户隔离筛选并分页查询自动化规则和动作。
func (a *AutomationRules) ListPageForUser(ctx context.Context, f AutomationRuleListFilter) ([]AutomationRule, int, error) {
	// whereSQL、args 保存筛选条件及其参数，供统计和列表查询复用。
	whereSQL, args := automationRuleWhere(f)

	// total 保存符合筛选条件的规则总数，不受分页参数影响。
	var total int
	// err 保存规则总数查询的数据库错误。
	if err := a.DB.QueryRowContext(ctx, `
SELECT COUNT(*)
  FROM automation_rules r
	  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id AND i.deleted_at IS NULL
 WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// queryArgs 保存列表查询的参数副本，避免追加分页参数修改统计查询参数。
	queryArgs := append([]any{}, args...)
	// limitSQL 保存可选的分页片段。
	limitSQL := ""
	if f.Limit > 0 {
		limitSQL = " LIMIT ? OFFSET ?"
		queryArgs = append(queryArgs, f.Limit, f.Offset)
	}
	// rows 保存规则基础字段游标；动作在游标关闭后分阶段加载，避免小连接池嵌套查询死锁。
	rows, err := a.DB.QueryContext(ctx, `
SELECT r.id,r.user_id,r.cookie_id,r.item_id,COALESCE(i.item_title,''),r.name,r.trigger_type,r.enabled,
       r.priority,r.config_json,r.created_at,r.updated_at
  FROM automation_rules r
	  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id AND i.deleted_at IS NULL
	WHERE `+whereSQL+`
	ORDER BY r.created_at DESC,r.id DESC`+limitSQL, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	// out 保存游标关闭后再加载动作的规则基础字段。
	out := []AutomationRule{}
	for rows.Next() {
		// r 保存当前游标行对应的规则基础字段。
		var r AutomationRule
		// enabled 保存数据库中的整数启用标记。
		var enabled int
		// err 保存当前规则基础字段扫描错误。
		if err := rows.Scan(&r.ID, &r.UserID, &r.CookieID, &r.ItemID, &r.ItemTitle, &r.Name, &r.TriggerType,
			&enabled, &r.Priority, &r.ConfigJSON, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, err
		}
		r.Enabled = enabled != 0
		out = append(out, r)
	}
	// rowsErr 保存规则基础游标遍历错误。
	rowsErr := rows.Err()
	// closeErr 保存规则基础游标关闭错误。
	closeErr := rows.Close()
	if rowsErr != nil {
		return nil, 0, rowsErr
	}
	if closeErr != nil {
		return nil, 0, closeErr
	}
	for /* index 表示当前待加载动作的规则位置。 */ index := range out {
		// acts 保存当前规则的动作列表。
		acts, err := a.Actions(ctx, out[index].ID)
		if err != nil {
			return nil, 0, err
		}
		out[index].Actions = acts
	}
	return out, total, nil
}
