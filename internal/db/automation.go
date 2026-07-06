package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
)

// AutomationRules 管理自动化规则、动作和执行记录。
//
// 自动化中心不区分触发来源：WS 系统事件、计划任务、后台手动触发都通过
// trigger_type + action 编排表达；真正的防重由 automation_runs.trigger_key 保证。
type AutomationRules struct {
	DB      *sql.DB
	Dialect Dialect
}

// AutomationRule 是一条自动化规则。规则只描述“什么时候、对哪个商品生效”，
// 具体做什么放在 AutomationAction 中，便于组合付款发货、评价赠品、求评价等流程。
type AutomationRule struct {
	ID          int64
	UserID      int64
	CookieID    string
	ItemID      string
	ItemTitle   string
	Name        string
	TriggerType string
	Enabled     bool
	Priority    int
	ConfigJSON  string
	CreatedAt   string
	UpdatedAt   string
	Actions     []AutomationAction
}

// AutomationAction 是规则下的一步动作。
type AutomationAction struct {
	ID              int64
	RuleID          int64
	ActionType      string
	CardID          int64
	CardName        string
	DeliveryCount   int
	MessageTemplate string
	DelaySeconds    int
	ConfigJSON      string
	Enabled         bool
	SortOrder       int
}

// AutomationRun 是一次自动化执行记录。trigger_key 是持久化防重键。
type AutomationRun struct {
	ID           int64
	RuleID       int64
	CookieID     string
	ItemID       string
	OrderID      string
	BuyerID      string
	ChatID       string
	TriggerType  string
	TriggerKey   string
	Status       string
	SentCount    int
	ErrorMessage string
	RawEventJSON string
	CreatedAt    string
	UpdatedAt    string
}

// AutomationRuleInput 是创建/更新规则的输入。
type AutomationRuleInput struct {
	UserID      int64
	CookieID    string
	ItemID      string
	Name        string
	TriggerType string
	Enabled     bool
	Priority    int
	ConfigJSON  string
	Actions     []AutomationActionInput
}

// AutomationActionInput 是创建动作的输入。
type AutomationActionInput struct {
	ActionType      string
	CardID          int64
	DeliveryCount   int
	MessageTemplate string
	DelaySeconds    int
	ConfigJSON      string
	Enabled         bool
	SortOrder       int
}

// ListForUser 返回用户下全部自动化规则和动作。
func (a *AutomationRules) ListForUser(ctx context.Context, userID int64) ([]AutomationRule, error) {
	rows, err := a.DB.QueryContext(ctx, `
SELECT r.id,r.user_id,r.cookie_id,r.item_id,COALESCE(i.item_title,''),r.name,r.trigger_type,r.enabled,
       r.priority,r.config_json,COALESCE(r.created_at,''),COALESCE(r.updated_at,'')
  FROM automation_rules r
  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id
 WHERE r.user_id=?
 ORDER BY r.created_at DESC,r.id DESC`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutomationRule{}
	for rows.Next() {
		var r AutomationRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.UserID, &r.CookieID, &r.ItemID, &r.ItemTitle, &r.Name, &r.TriggerType,
			&enabled, &r.Priority, &r.ConfigJSON, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		acts, err := a.Actions(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		r.Actions = acts
		out = append(out, r)
	}
	return out, rows.Err()
}

// Match 查询某事件可触发的规则。商品精确规则优先，其次允许 item_id 为空的账号级规则。
func (a *AutomationRules) Match(ctx context.Context, cookieID, itemID, triggerType string) ([]AutomationRule, error) {
	rows, err := a.DB.QueryContext(ctx, `
SELECT r.id,r.user_id,r.cookie_id,r.item_id,COALESCE(i.item_title,''),r.name,r.trigger_type,r.enabled,
       r.priority,r.config_json,COALESCE(r.created_at,''),COALESCE(r.updated_at,'')
  FROM automation_rules r
  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id
 WHERE r.enabled=1
   AND r.cookie_id=?
   AND r.trigger_type=?
   AND (r.item_id=? OR r.item_id='')
 ORDER BY CASE WHEN r.item_id=? THEN 0 ELSE 1 END, r.priority ASC, r.id ASC`,
		cookieID, triggerType, itemID, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutomationRule{}
	for rows.Next() {
		var r AutomationRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.UserID, &r.CookieID, &r.ItemID, &r.ItemTitle, &r.Name, &r.TriggerType,
			&enabled, &r.Priority, &r.ConfigJSON, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		acts, err := a.Actions(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		r.Actions = acts
		out = append(out, r)
	}
	return out, rows.Err()
}

// Create 创建规则和动作。
func (a *AutomationRules) Create(ctx context.Context, in AutomationRuleInput) (int64, error) {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	id, err := createAutomationRuleTx(ctx, tx, in)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// Update 替换规则和动作。动作采用删除重建，避免前端携带展示字段造成局部更新不一致。
func (a *AutomationRules) Update(ctx context.Context, userID, ruleID int64, in AutomationRuleInput) error {
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `
UPDATE automation_rules
   SET cookie_id=?,item_id=?,name=?,trigger_type=?,enabled=?,priority=?,config_json=?,updated_at=CURRENT_TIMESTAMP
 WHERE id=? AND user_id=?`,
		in.CookieID, in.ItemID, in.Name, in.TriggerType, boolToInt(in.Enabled), in.Priority, validJSON(in.ConfigJSON), ruleID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM automation_rule_actions WHERE rule_id=?`, ruleID); err != nil {
		return err
	}
	for _, act := range in.Actions {
		if err := insertAutomationActionTx(ctx, tx, ruleID, act); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Delete 删除规则。
func (a *AutomationRules) Delete(ctx context.Context, userID, ruleID int64) error {
	res, err := a.DB.ExecContext(ctx, `DELETE FROM automation_rules WHERE id=? AND user_id=?`, ruleID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// Actions 返回规则动作。
func (a *AutomationRules) Actions(ctx context.Context, ruleID int64) ([]AutomationAction, error) {
	rows, err := a.DB.QueryContext(ctx, `
SELECT a.id,a.rule_id,a.action_type,COALESCE(a.card_id,0),COALESCE(c.name,''),a.delivery_count,
       a.message_template,a.delay_seconds,a.config_json,a.enabled,a.sort_order
  FROM automation_rule_actions a
  LEFT JOIN cards c ON c.id=a.card_id
 WHERE a.rule_id=?
 ORDER BY a.sort_order ASC,a.id ASC`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []AutomationAction{}
	for rows.Next() {
		var act AutomationAction
		var enabled int
		if err := rows.Scan(&act.ID, &act.RuleID, &act.ActionType, &act.CardID, &act.CardName,
			&act.DeliveryCount, &act.MessageTemplate, &act.DelaySeconds, &act.ConfigJSON, &enabled,
			&act.SortOrder); err != nil {
			return nil, err
		}
		act.Enabled = enabled != 0
		out = append(out, act)
	}
	return out, rows.Err()
}

// TryStartRun 以 UNIQUE(rule_id, trigger_key) 作为持久化防重。
// 返回 started=false 表示该规则对该触发已执行或正在执行，调用方应直接跳过。
func (a *AutomationRules) TryStartRun(ctx context.Context, run AutomationRun) (int64, bool, error) {
	res, err := a.DB.ExecContext(ctx,
		dialectInsertIgnorePrefix(a.Dialect)+` INTO automation_runs
    (rule_id,cookie_id,item_id,order_id,buyer_id,chat_id,trigger_type,trigger_key,status,raw_event_json)
VALUES (?,?,?,?,?,?,?,?,?,?)`+dialectInsertIgnore(a.Dialect, []string{"rule_id", "trigger_key"}),
		run.RuleID, run.CookieID, run.ItemID, run.OrderID, run.BuyerID, run.ChatID, run.TriggerType,
		run.TriggerKey, "running", validJSON(run.RawEventJSON))
	if err != nil {
		return 0, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return 0, false, nil
	}
	id, _ := res.LastInsertId()
	return id, true, nil
}

// FinishRun 标记执行完成或失败。
func (a *AutomationRules) FinishRun(ctx context.Context, id int64, status string, sentCount int, errMsg string) error {
	_, err := a.DB.ExecContext(ctx, `
UPDATE automation_runs
   SET status=?,sent_count=?,error_message=?,updated_at=CURRENT_TIMESTAMP
 WHERE id=?`, status, sentCount, errMsg, id)
	return err
}

// MarkOrderEventTime 更新订单事件时间字段。字段名由调用方控制白名单。
func (a *AutomationRules) MarkOrderEventTime(ctx context.Context, orderID, field string) error {
	switch field {
	case "paid_at", "shipped_at", "completed_at", "buyer_reviewed_at", "last_review_request_at":
	default:
		return fmt.Errorf("不允许更新的订单时间字段: %s", field)
	}
	_, err := a.DB.ExecContext(ctx, "UPDATE orders SET "+field+"=CURRENT_TIMESTAMP, updated_at=CURRENT_TIMESTAMP WHERE order_id=?", orderID)
	return err
}

// IncrementReviewRequest 记录一次求评价请求。
func (a *AutomationRules) IncrementReviewRequest(ctx context.Context, orderID string) error {
	_, err := a.DB.ExecContext(ctx, `
UPDATE orders
   SET review_request_count=review_request_count+1,
       last_review_request_at=CURRENT_TIMESTAMP,
       updated_at=CURRENT_TIMESTAMP
 WHERE order_id=?`, orderID)
	return err
}

// DueReviewRequestOrders 返回到期但尚未评价的订单。调度器会再按规则配置做精确判断。
func (a *AutomationRules) DueReviewRequestOrders(ctx context.Context, limit int) ([]Order, error) {
	if limit <= 0 {
		limit = 200
	}
	rows, err := a.DB.QueryContext(ctx, `
SELECT order_id,item_id,buyer_id,spec_name,spec_value,quantity,amount,order_status,cookie_id,is_bargain,
       receiver_name,receiver_phone,receiver_address,receiver_city,version,chat_id,system_shipped,
       COALESCE(paid_at,''),COALESCE(shipped_at,''),COALESCE(completed_at,''),
       COALESCE(buyer_reviewed_at,''),COALESCE(last_review_request_at,''),review_request_count,
       COALESCE(created_at,''),COALESCE(updated_at,'')
  FROM orders
 WHERE system_shipped=1
   AND COALESCE(buyer_reviewed_at,'')=''
   AND COALESCE(chat_id,'')<>''
 ORDER BY updated_at ASC
 LIMIT ?`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Order{}
	for rows.Next() {
		var ord Order
		var isBargain, version, sysShipped int
		// orders 表的可空 TEXT 列必须用 NullString 扫描，旧库数据可能为 NULL
		// （spec_name/spec_value 等在 init schema 中无 NOT NULL 约束）。
		var itemID, buyerID, specName, specValue, qty, amount, status, cookieID,
			receiverName, receiverPhone, receiverAddr, receiverCity, chatID sql.NullString
		if err := rows.Scan(&ord.OrderID, &itemID, &buyerID, &specName, &specValue, &qty, &amount,
			&status, &cookieID, &isBargain, &receiverName, &receiverPhone, &receiverAddr,
			&receiverCity, &version, &chatID, &sysShipped, &ord.PaidAt,
			&ord.ShippedAt, &ord.CompletedAt, &ord.BuyerReviewedAt, &ord.LastReviewRequestAt,
			&ord.ReviewRequestCount, &ord.CreatedAt, &ord.UpdatedAt); err != nil {
			return nil, err
		}
		ord.ItemID = itemID.String
		ord.BuyerID = buyerID.String
		ord.SpecName = specName.String
		ord.SpecValue = specValue.String
		ord.Quantity = qty.String
		ord.Amount = amount.String
		ord.OrderStatus = status.String
		ord.CookieID = cookieID.String
		ord.ReceiverName = receiverName.String
		ord.ReceiverPhone = receiverPhone.String
		ord.ReceiverAddr = receiverAddr.String
		ord.ReceiverCity = receiverCity.String
		ord.ChatID = chatID.String
		ord.IsBargain = isBargain
		ord.Version = version
		ord.SystemShipped = sysShipped != 0
		out = append(out, ord)
	}
	return out, rows.Err()
}

func createAutomationRuleTx(ctx context.Context, tx *sql.Tx, in AutomationRuleInput) (int64, error) {
	if in.Priority <= 0 {
		in.Priority = 100
	}
	res, err := tx.ExecContext(ctx, `
INSERT INTO automation_rules (user_id,cookie_id,item_id,name,trigger_type,enabled,priority,config_json)
VALUES (?,?,?,?,?,?,?,?)`,
		in.UserID, in.CookieID, in.ItemID, in.Name, in.TriggerType, boolToInt(in.Enabled), in.Priority, validJSON(in.ConfigJSON))
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	for _, act := range in.Actions {
		if err := insertAutomationActionTx(ctx, tx, id, act); err != nil {
			return 0, err
		}
	}
	return id, nil
}

func insertAutomationActionTx(ctx context.Context, tx *sql.Tx, ruleID int64, act AutomationActionInput) error {
	if act.DeliveryCount <= 0 {
		act.DeliveryCount = 1
	}
	_, err := tx.ExecContext(ctx, `
INSERT INTO automation_rule_actions
    (rule_id,action_type,card_id,delivery_count,message_template,delay_seconds,config_json,enabled,sort_order)
VALUES (?,?,?,?,?,?,?,?,?)`,
		ruleID, act.ActionType, nullInt64(act.CardID), act.DeliveryCount, act.MessageTemplate,
		act.DelaySeconds, validJSON(act.ConfigJSON), boolToInt(act.Enabled), act.SortOrder)
	return err
}

func nullInt64(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}

func validJSON(s string) string {
	if s == "" {
		return "{}"
	}
	var v any
	if err := json.Unmarshal([]byte(s), &v); err != nil {
		return "{}"
	}
	return s
}
