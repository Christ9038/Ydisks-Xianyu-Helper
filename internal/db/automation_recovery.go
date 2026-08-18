package db

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// RecoverDefinitelyUnsentReviewRuns 将已证明未发送的评价任务恢复为可重试状态，并返回恢复数量。
func (a *AutomationRules) RecoverDefinitelyUnsentReviewRuns(ctx context.Context) (int64, error) {
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
	   SET status='failed',action_started=0,lease_expires_at=0,next_retry_at=0,
	       error_message='历史版本在 WebSocket 就绪前执行，已恢复等待安全重试',
	       updated_at=CURRENT_TIMESTAMP
	 WHERE trigger_type='review_missing_timeout' AND status='needs_review' AND sent_count=0
	   AND (error_message LIKE '%当前没有可用 WebSocket 连接%'
	        OR error_message LIKE '%账号未在线，无法发送自动化消息%'
	        OR error_message LIKE '%账号发送器未初始化%')`)
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

// DeferTask 封装Defer任务业务协调。
func (a *AutomationRules) DeferTask(ctx context.Context, task DeferredAutomationTask) error {
	// err 用于本次流程后续判断的err
	_, err := a.DB.ExecContext(ctx, `INSERT INTO automation_pending_tasks
    (task_key,cookie_id,trigger_type,task_json,due_at,status,attempt_count,lease_expires_at,error_message)
VALUES (?,?,?,?,?,'pending',0,0,?)`+dialectUpsert(a.Dialect, []string{"task_key"}, map[string]string{
		"cookie_id":        "excluded.cookie_id",
		"trigger_type":     "excluded.trigger_type",
		"task_json":        "excluded.task_json",
		"due_at":           "excluded.due_at",
		"status":           "'pending'",
		"attempt_count":    "0",
		"lease_expires_at": "0",
		"error_message":    "excluded.error_message",
	}), task.TaskKey, task.CookieID, task.TriggerType, validJSON(task.TaskJSON), task.DueAt, task.ErrorMessage)
	return err
}

// ListIssues 读取问题列表。
func (a *AutomationRules) ListIssues(ctx context.Context, userID int64) ([]AutomationRunIssue, []DeferredAutomationIssue, error) {
	// runRows、err 用于本次流程后续判断的运行Rows、err
	runRows, err := a.DB.QueryContext(ctx, `SELECT ar.id,ar.cookie_id,ar.order_id,ar.trigger_type,ar.error_message,
		ar.action_cursor,ar.sent_count,ar.updated_at,ar.raw_event_json,ar.action_started,COALESCE(r.enabled,0)
		FROM automation_runs ar JOIN cookies c ON c.id=ar.cookie_id
		LEFT JOIN automation_rules r ON r.id=ar.rule_id
		WHERE c.user_id=? AND ar.status='needs_review' AND r.deleted_at IS NULL ORDER BY ar.updated_at DESC,ar.id DESC`, userID)
	if err != nil {
		return nil, nil, err
	}
	// runs 用于本次流程后续判断的运行记录
	runs := []AutomationRunIssue{}
	for runRows.Next() {
		// issue 用于本次流程后续判断的问题
		var issue AutomationRunIssue
		// rawEventJSON 用于本次流程后续判断的原始EventJSON
		var rawEventJSON string
		// actionStarted、ruleEnabled 用于本次流程后续判断的动作Started、rule启用状态
		var actionStarted, ruleEnabled int
		if // err 用于本次流程后续判断的err
		err := runRows.Scan(&issue.ID, &issue.CookieID, &issue.OrderID, &issue.TriggerType, &issue.ErrorMessage,
			&issue.ActionCursor, &issue.SentCount, &issue.UpdatedAt, &rawEventJSON, &actionStarted, &ruleEnabled); err != nil {
			_ = runRows.Close()
			return nil, nil, err
		}
		issue.IssueKind, issue.AllowedResolutions = automationIssuePolicy(
			rawEventJSON, actionStarted != 0, ruleEnabled != 0, issue.SentCount, issue.ErrorMessage,
		)
		runs = append(runs, issue)
	}
	if // err 用于本次流程后续判断的err
	err := runRows.Close(); err != nil {
		return nil, nil, err
	}
	// taskRows、err 用于本次流程后续判断的任务Rows、err
	taskRows, err := a.DB.QueryContext(ctx, `SELECT apt.id,apt.cookie_id,apt.trigger_type,apt.error_message,
		apt.attempt_count,apt.updated_at
		FROM automation_pending_tasks apt JOIN cookies c ON c.id=apt.cookie_id
		WHERE c.user_id=? AND apt.status='dead_letter' ORDER BY apt.updated_at DESC,apt.id DESC`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer taskRows.Close()
	// tasks 用于本次流程后续判断的任务列表
	tasks := []DeferredAutomationIssue{}
	for taskRows.Next() {
		// issue 用于本次流程后续判断的问题
		var issue DeferredAutomationIssue
		if // err 用于本次流程后续判断的err
		err := taskRows.Scan(&issue.ID, &issue.CookieID, &issue.TriggerType, &issue.ErrorMessage,
			&issue.AttemptCount, &issue.UpdatedAt); err != nil {
			return nil, nil, err
		}
		tasks = append(tasks, issue)
	}
	return runs, tasks, taskRows.Err()
}

// ResolveRunIssue 处理运行问题。
func (a *AutomationRules) ResolveRunIssue(ctx context.Context, userID, runID int64, resolution string) error {
	// rawEventJSON、errorMessage 用于本次流程后续判断的原始EventJSON、error消息
	var rawEventJSON, errorMessage string
	// actionStarted、ruleEnabled、sentCount 用于本次流程后续判断的动作Started、ruleEnabled、sent数量
	var actionStarted, ruleEnabled, sentCount int
	// err 用于本次流程后续判断的err
	err := a.DB.QueryRowContext(ctx, `SELECT ar.raw_event_json,ar.action_started,COALESCE(r.enabled,0),ar.sent_count,ar.error_message
		FROM automation_runs ar JOIN cookies c ON c.id=ar.cookie_id
		LEFT JOIN automation_rules r ON r.id=ar.rule_id
		WHERE ar.id=? AND ar.status='needs_review' AND c.user_id=? AND r.deleted_at IS NULL`, runID, userID).
		Scan(&rawEventJSON, &actionStarted, &ruleEnabled, &sentCount, &errorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	// allowed 用于本次流程后续判断的allowed
	_, allowed := automationIssuePolicy(rawEventJSON, actionStarted != 0, ruleEnabled != 0, sentCount, errorMessage)
	if !containsString(allowed, resolution) {
		return fmt.Errorf("当前异常不允许使用 %s 处理", resolution)
	}
	// set 用于本次流程后续判断的set
	var set string
	switch resolution {
	case "continue":
		set = "status='running',action_cursor=action_cursor+1,action_started=0,lease_expires_at=0,next_retry_at=0,error_message=''"
	case "retry":
		set = "status='running',action_started=0,lease_expires_at=0,next_retry_at=0,error_message=''"
	case "cancel":
		set = "status='canceled',action_started=0,lease_expires_at=0,next_retry_at=0"
	default:
		return errors.New("不支持的人工处理方式")
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs SET `+set+`,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='needs_review' AND cookie_id IN (SELECT id FROM cookies WHERE user_id=?)`, runID, userID)
	if err != nil {
		return err
	}
	if // n 用于本次流程后续判断的n
	n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// automationIssuePolicy 封装自动化问题Policy业务协调。
func automationIssuePolicy(rawEventJSON string, actionStarted, ruleEnabled bool, sentCount int, errorMessage string) (string, []string) {
	if actionStarted {
		// 外部接口没有可依赖的幂等键。结果未知时重放当前游标可能重复发卡、
		// 发消息或确认发货，因此后端也必须拒绝 retry，不能只依赖前端警告。
		return "external_result_unknown", []string{"continue", "cancel"}
	}
	// raw 用于本次流程后续判断的原始
	var raw map[string]any
	if json.Unmarshal([]byte(rawEventJSON), &raw) != nil {
		return "invalid_snapshot", []string{"cancel"}
	}
	// accountID 用于本次流程后续判断的账号ID
	accountID, _ := raw["AccountID"].(string)
	if strings.TrimSpace(accountID) == "" {
		accountID, _ = raw["account_id"].(string)
	}
	if strings.TrimSpace(accountID) == "" {
		return "invalid_snapshot", []string{"cancel"}
	}
	if strings.Contains(errorMessage, "规则不存在或已停用") {
		if ruleEnabled {
			return "rule_unavailable", []string{"retry", "cancel"}
		}
		return "rule_unavailable", []string{"cancel"}
	}
	if sentCount > 0 {
		return "partial_failure", []string{"continue", "retry", "cancel"}
	}
	return "execution_failed", []string{"retry", "cancel"}
}

// containsString 封装containsString业务协调。
func containsString(values []string, target string) bool {
	// value 表示当前遍历过程中的值
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

// ResolveDeferredIssue 处理Deferred问题。
func (a *AutomationRules) ResolveDeferredIssue(ctx context.Context, userID, taskID int64, retry bool) error {
	if retry {
		// res、err 用于本次流程后续判断的res、err
		res, err := a.DB.ExecContext(ctx, `UPDATE automation_pending_tasks
			SET status='pending',attempt_count=0,due_at=0,lease_expires_at=0,error_message='',updated_at=CURRENT_TIMESTAMP
			WHERE id=? AND status='dead_letter' AND cookie_id IN (SELECT id FROM cookies WHERE user_id=?)`, taskID, userID)
		if err != nil {
			return err
		}
		if // n 用于本次流程后续判断的n
		n, _ := res.RowsAffected(); n != 1 {
			return ErrNotFound
		}
		return nil
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `DELETE FROM automation_pending_tasks
		WHERE id=? AND status='dead_letter' AND cookie_id IN (SELECT id FROM cookies WHERE user_id=?)`, taskID, userID)
	if err != nil {
		return err
	}
	if // n 用于本次流程后续判断的n
	n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

// ClaimDueDeferredTasks 封装ClaimDueDeferred任务列表业务协调。
func (a *AutomationRules) ClaimDueDeferredTasks(ctx context.Context, limit int) ([]DeferredAutomationTask, error) {
	if limit <= 0 {
		limit = 100
	}
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Unix()
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := a.DB.QueryContext(ctx, `SELECT id,task_key,cookie_id,trigger_type,task_json,due_at,attempt_count
  FROM automation_pending_tasks
	 WHERE ((status='pending' AND due_at<=?) OR (status='running' AND lease_expires_at<?)) AND attempt_count<5
 ORDER BY due_at,id LIMIT ?`, now, now, limit)
	if err != nil {
		return nil, err
	}
	// candidates 用于本次流程后续判断的candidates
	candidates := []DeferredAutomationTask{}
	for rows.Next() {
		// task 用于本次流程后续判断的任务
		var task DeferredAutomationTask
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&task.ID, &task.TaskKey, &task.CookieID, &task.TriggerType, &task.TaskJSON, &task.DueAt, &task.ClaimVersion); err != nil {
			_ = rows.Close()
			return nil, err
		}
		candidates = append(candidates, task)
	}
	if // err 用于本次流程后续判断的err
	err := rows.Close(); err != nil {
		return nil, err
	}
	// claimed 用于本次流程后续判断的claimed
	claimed := make([]DeferredAutomationTask, 0, len(candidates))
	// task 表示当前遍历过程中的任务
	for _, task := range candidates {
		// res、err 用于本次流程后续判断的res、err
		res, err := a.DB.ExecContext(ctx, `UPDATE automation_pending_tasks
   SET status='running',attempt_count=attempt_count+1,lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
	 WHERE id=? AND attempt_count<5 AND ((status='pending' AND due_at<=?) OR (status='running' AND lease_expires_at<?))`, now+300, task.ID, now, now)
		if err != nil {
			return nil, err
		}
		if // n 用于本次流程后续判断的n
		n, _ := res.RowsAffected(); n == 1 {
			task.ClaimVersion++
			claimed = append(claimed, task)
		}
	}
	return claimed, nil
}

// FinishDeferredTask 封装FinishDeferred任务业务协调。
func (a *AutomationRules) FinishDeferredTask(ctx context.Context, id int64, claimVersion int, success bool, errMsg string) error {
	if success {
		// res、err 用于本次流程后续判断的res、err
		res, err := a.DB.ExecContext(ctx, `DELETE FROM automation_pending_tasks
			WHERE id=? AND status='running' AND attempt_count=?`, id, claimVersion)
		return requireDeferredTaskOwner(res, err)
	}
	// retryDelay 用于本次流程后续判断的重试延迟
	retryDelay := deferredRetryDelay(claimVersion)
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_pending_tasks
	   SET status=CASE WHEN attempt_count>=5 THEN 'dead_letter' ELSE 'pending' END,
	       due_at=?,lease_expires_at=0,error_message=?,updated_at=CURRENT_TIMESTAMP
	 WHERE id=? AND status='running' AND attempt_count=?`, time.Now().UTC().Add(retryDelay).Unix(), errMsg, id, claimVersion)
	return requireDeferredTaskOwner(res, err)
}

// deferredRetryDelay 封装deferred重试延迟业务协调。
func deferredRetryDelay(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	// delay 用于本次流程后续判断的延迟
	delay := 5 * time.Minute * time.Duration(1<<(attempt-1))
	if delay > time.Hour {
		return time.Hour
	}
	return delay
}

// WakeCredentialBlocked 让 Cookie 恢复后的账号尽快重新领取明确尚未发送的任务。
func (a *AutomationRules) WakeCredentialBlocked(ctx context.Context, cookieID string) error {
	if strings.TrimSpace(cookieID) == "" {
		return nil
	}
	if // err 用于本次流程后续判断的err
	_, err := a.DB.ExecContext(ctx, `UPDATE automation_pending_tasks
		SET due_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND status='pending'
		  AND (LOWER(error_message) LIKE '%session%' OR error_message LIKE '%登录凭证%'
		       OR error_message LIKE '%Cookie%' OR error_message LIKE '%cookie%'
		       OR LOWER(error_message) LIKE '%websocket%' OR LOWER(error_message) LIKE '%token%'
		       OR error_message LIKE '%消息连接%' OR error_message LIKE '%账号未在线%')`, cookieID); err != nil {
		return err
	}
	// err 用于本次流程后续判断的err
	_, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
		SET next_retry_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE cookie_id=? AND status='failed' AND action_started=0
		  AND (sent_count=0 OR error_message LIKE '[safe_retry]%')
		  AND (LOWER(error_message) LIKE '%session%' OR error_message LIKE '%登录凭证%'
		       OR error_message LIKE '%Cookie%' OR error_message LIKE '%cookie%'
		       OR LOWER(error_message) LIKE '%websocket%' OR LOWER(error_message) LIKE '%token%'
		       OR error_message LIKE '%消息连接%' OR error_message LIKE '%账号未在线%')`, cookieID)
	return err
}

// RenewDeferredTaskLease 封装RenewDeferred任务Lease业务协调。
func (a *AutomationRules) RenewDeferredTaskLease(ctx context.Context, id int64, claimVersion int, leaseExpiresAt int64) error {
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_pending_tasks
		SET lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND attempt_count=?`, leaseExpiresAt, id, claimVersion)
	return requireDeferredTaskOwner(res, err)
}

// requireDeferredTaskOwner 封装requireDeferred任务所有者业务协调。
func requireDeferredTaskOwner(res sql.Result, err error) error {
	if err != nil {
		return err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrDeferredTaskLeaseLost
	}
	return nil
}

// MarkOrderEventTime 更新订单事件时间字段。字段名由调用方控制白名单。
func (a *AutomationRules) MarkOrderEventTime(ctx context.Context, orderID, field string) error {
	switch field {
	case "paid_at", "shipped_at", "completed_at", "buyer_reviewed_at", "last_review_request_at":
	default:
		return fmt.Errorf("不允许更新的订单时间字段: %s", field)
	}
	// 事件时间列是跨方言 TEXT。不能直接写 CURRENT_TIMESTAMP：Postgres 会产生
	// "2006-01-02 15:04:05.999999+00"，而 MySQL 的无时区文本又取决于会话时区，
	// 调度器无法可靠解释。统一由应用写 RFC3339 UTC，已有值仍保留（幂等）。
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// err 用于本次流程后续判断的err
	_, err := a.DB.ExecContext(ctx, "UPDATE orders SET "+field+"=?, updated_at=CURRENT_TIMESTAMP WHERE order_id=? AND COALESCE("+field+",'')=''", now, orderID)
	return err
}

// IncrementReviewRequest 记录一次求评价请求。
func (a *AutomationRules) IncrementReviewRequest(ctx context.Context, orderID string) error {
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Format(time.RFC3339Nano)
	// err 用于本次流程后续判断的err
	_, err := a.DB.ExecContext(ctx, `
UPDATE orders
   SET review_request_count=review_request_count+1,
       last_review_request_at=?,
       updated_at=CURRENT_TIMESTAMP
 WHERE order_id=?`, now, orderID)
	return err
}

// DueReviewRequestOrders 返回到期但尚未评价的订单。调度器会再按规则配置做精确判断。
func (a *AutomationRules) DueReviewRequestOrders(ctx context.Context, limit int) ([]Order, error) {
	return a.DueReviewRequestOrdersAfter(ctx, "", limit)
}

// DueReviewRequestOrdersAfter 用不可变的订单 ID 作为稳定游标分页扫描，避免固定 LIMIT
// 让旧订单饿死后续订单。不能使用 updated_at：执行求评价动作本身会修改该字段。
// DueReviewRequestOrdersAfter 封装DueReview请求订单列表After业务协调。
func (a *AutomationRules) DueReviewRequestOrdersAfter(ctx context.Context, afterOrderID string, limit int) ([]Order, error) {
	if limit <= 0 {
		limit = 200
	}
	// cursorSQL 用于本次流程后续判断的游标SQL
	cursorSQL := ""
	// args 用于本次流程后续判断的args
	args := []any{}
	if afterOrderID != "" {
		cursorSQL = " AND o.order_id>?"
		args = append(args, afterOrderID)
	}
	args = append(args, limit)
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := a.DB.QueryContext(ctx, `
SELECT order_id,item_id,buyer_id,spec_name,spec_value,quantity,amount,order_status,cookie_id,is_bargain,
       receiver_name,receiver_phone,receiver_address,receiver_city,version,chat_id,system_shipped,
       COALESCE(paid_at,''),COALESCE(shipped_at,''),COALESCE(completed_at,''),
       COALESCE(buyer_reviewed_at,''),COALESCE(last_review_request_at,''),review_request_count,
       created_at,updated_at
  FROM orders o
WHERE o.system_shipped=1
   AND o.deleted_at IS NULL
   AND COALESCE(o.buyer_reviewed_at,'')=''
   AND COALESCE(o.chat_id,'')<>''
   AND EXISTS (SELECT 1 FROM automation_rules r
                WHERE r.cookie_id=o.cookie_id AND r.trigger_type='review_missing_timeout'
                  AND r.deleted_at IS NULL AND r.enabled=1
                  AND (r.item_id=o.item_id OR r.item_id=''))`+cursorSQL+`
 ORDER BY o.order_id ASC
	 LIMIT ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 用于本次流程后续判断的out
	out := []Order{}
	for rows.Next() {
		// ord 用于本次流程后续判断的ord
		var ord Order
		// isBargain、version、sysShipped 用于本次流程后续判断的isBargain、version、sysShipped
		var isBargain, version, sysShipped int
		// orders 表的可空 TEXT 列必须用 NullString 扫描，旧库数据可能为 NULL
		// （spec_name/spec_value 等在 init schema 中无 NOT NULL 约束）。
		// itemID、buyerID、specName、specValue、qty、amount、status、cookieID、receiverName、receiverPhone、receiverAddr、receiverCity、chatID 保存商品ID、buyerID、specName、specValue、qty、amount、status、cookieID、receiverName、receiverPhone、receiverAddr、receiverCity、chatID，供当前处理流程使用
		var itemID, buyerID, specName, specValue, qty, amount, status, cookieID,
			receiverName, receiverPhone, receiverAddr, receiverCity, chatID sql.NullString
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&ord.OrderID, &itemID, &buyerID, &specName, &specValue, &qty, &amount,
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

// createAutomationRuleTx 封装create自动化规则Tx业务协调。
func createAutomationRuleTx(ctx context.Context, tx *sql.Tx, dialect Dialect, in AutomationRuleInput) (int64, error) {
	if in.Priority <= 0 {
		in.Priority = 100
	}
	// id、err 用于本次流程后续判断的id、err
	id, err := insertReturningID(ctx, tx, dialect, `
INSERT INTO automation_rules (user_id,cookie_id,item_id,name,trigger_type,enabled,priority,config_json)
VALUES (?,?,?,?,?,?,?,?)`,
		in.UserID, in.CookieID, in.ItemID, in.Name, in.TriggerType, boolToInt(in.Enabled), in.Priority, validJSON(in.ConfigJSON))
	if err != nil {
		return 0, err
	}
	// act 表示当前遍历过程中的act
	for _, act := range in.Actions {
		if // err 用于本次流程后续判断的err
		err := insertAutomationActionTx(ctx, tx, id, act); err != nil {
			return 0, err
		}
	}
	return id, nil
}

// insertAutomationActionTx 封装insert自动化动作Tx业务协调。
func insertAutomationActionTx(ctx context.Context, tx *sql.Tx, ruleID int64, act AutomationActionInput) error {
	if act.DeliveryCount <= 0 {
		act.DeliveryCount = 1
	}
	// err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, `
INSERT INTO automation_rule_actions
    (rule_id,action_type,card_id,delivery_count,message_template,delay_seconds,config_json,enabled,sort_order)
VALUES (?,?,?,?,?,?,?,?,?)`,
		ruleID, act.ActionType, nullInt64(act.CardID), act.DeliveryCount, act.MessageTemplate,
		act.DelaySeconds, validJSON(act.ConfigJSON), boolToInt(act.Enabled), act.SortOrder)
	return err
}

// nullInt64 封装nullInt64业务协调。
func nullInt64(v int64) any {
	if v <= 0 {
		return nil
	}
	return v
}

// validJSON 封装有效JSON业务协调。
func validJSON(s string) string {
	if s == "" {
		return "{}"
	}
	// v 用于本次流程后续判断的v
	var v any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal([]byte(s), &v); err != nil {
		return "{}"
	}
	return s
}
