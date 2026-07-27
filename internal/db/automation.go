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

var ErrAutomationRunActive = errors.New("规则仍有待处理的自动化运行")

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
	ID             int64
	RuleID         int64
	CookieID       string
	ItemID         string
	OrderID        string
	BuyerID        string
	ChatID         string
	TriggerType    string
	TriggerKey     string
	Status         string
	SentCount      int
	ErrorMessage   string
	RawEventJSON   string
	CreatedAt      string
	UpdatedAt      string
	LeaseExpiresAt int64
	AttemptCount   int
	NextRetryAt    int64
	ActionCursor   int
	ActionStarted  bool
}

// ErrAutomationRunLeaseLost 表示自动化运行已被更高 attempt_count 的 worker 接管。
var ErrAutomationRunLeaseLost = errors.New("自动化运行租约已失效")

type DeferredAutomationTask struct {
	ID           int64
	TaskKey      string
	CookieID     string
	TriggerType  string
	TaskJSON     string
	DueAt        int64
	ClaimVersion int
}

var ErrDeferredTaskLeaseLost = errors.New("延迟自动化任务租约已失效")

type AutomationRunIssue struct {
	ID                 int64    `json:"id"`
	CookieID           string   `json:"cookie_id"`
	OrderID            string   `json:"order_id"`
	TriggerType        string   `json:"trigger_type"`
	ErrorMessage       string   `json:"error_message"`
	IssueKind          string   `json:"issue_kind"`
	AllowedResolutions []string `json:"allowed_resolutions"`
	ActionCursor       int      `json:"action_cursor"`
	SentCount          int      `json:"sent_count"`
	UpdatedAt          string   `json:"updated_at"`
}

type DeferredAutomationIssue struct {
	ID           int64  `json:"id"`
	CookieID     string `json:"cookie_id"`
	TriggerType  string `json:"trigger_type"`
	ErrorMessage string `json:"error_message"`
	AttemptCount int    `json:"attempt_count"`
	UpdatedAt    string `json:"updated_at"`
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

// AutomationRuleListFilter 是自动化规则列表的筛选和分页条件。
type AutomationRuleListFilter struct {
	UserID      int64
	CookieID    string
	TriggerType string
	Enabled     *bool
	Search      string
	Limit       int
	Offset      int
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
	rules, _, err := a.ListPageForUser(ctx, AutomationRuleListFilter{UserID: userID})
	return rules, err
}

// ListPageForUser 按用户隔离筛选并分页查询自动化规则和动作。
func (a *AutomationRules) ListPageForUser(ctx context.Context, f AutomationRuleListFilter) ([]AutomationRule, int, error) {
	where := []string{"r.user_id=?"}
	args := []any{f.UserID}
	if f.CookieID != "" {
		where = append(where, "r.cookie_id=?")
		args = append(args, f.CookieID)
	}
	if f.TriggerType != "" {
		where = append(where, "r.trigger_type=?")
		args = append(args, f.TriggerType)
	}
	if f.Enabled != nil {
		where = append(where, "r.enabled=?")
		args = append(args, boolToInt(*f.Enabled))
	}
	if search := strings.ToLower(strings.TrimSpace(f.Search)); search != "" {
		pattern := "%" + search + "%"
		where = append(where, `(LOWER(COALESCE(r.name,'')) LIKE ?
			OR LOWER(COALESCE(r.item_id,'')) LIKE ?
			OR LOWER(COALESCE(i.item_title,'')) LIKE ?)`)
		args = append(args, pattern, pattern, pattern)
	}
	whereSQL := strings.Join(where, " AND ")

	var total int
	if err := a.DB.QueryRowContext(ctx, `
SELECT COUNT(*)
  FROM automation_rules r
  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id
 WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	queryArgs := append([]any{}, args...)
	limitSQL := ""
	if f.Limit > 0 {
		limitSQL = " LIMIT ? OFFSET ?"
		queryArgs = append(queryArgs, f.Limit, f.Offset)
	}
	rows, err := a.DB.QueryContext(ctx, `
SELECT r.id,r.user_id,r.cookie_id,r.item_id,COALESCE(i.item_title,''),r.name,r.trigger_type,r.enabled,
       r.priority,r.config_json,r.created_at,r.updated_at
  FROM automation_rules r
  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id
	WHERE `+whereSQL+`
	ORDER BY r.created_at DESC,r.id DESC`+limitSQL, queryArgs...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	out := []AutomationRule{}
	for rows.Next() {
		var r AutomationRule
		var enabled int
		if err := rows.Scan(&r.ID, &r.UserID, &r.CookieID, &r.ItemID, &r.ItemTitle, &r.Name, &r.TriggerType,
			&enabled, &r.Priority, &r.ConfigJSON, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, err
		}
		r.Enabled = enabled != 0
		acts, err := a.Actions(ctx, r.ID)
		if err != nil {
			return nil, 0, err
		}
		r.Actions = acts
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// Match 查询某事件可触发的规则。商品级规则存在时只返回商品级规则；
// 没有商品级规则时才回退到账号级规则，避免两层规则叠加导致重复发货。
func (a *AutomationRules) Match(ctx context.Context, cookieID, itemID, triggerType string) ([]AutomationRule, error) {
	out, err := a.matchScope(ctx, cookieID, itemID, triggerType)
	if err != nil || len(out) > 0 || itemID == "" {
		return highestPriorityRule(out), err
	}
	out, err = a.matchScope(ctx, cookieID, "", triggerType)
	return highestPriorityRule(out), err
}

func highestPriorityRule(rules []AutomationRule) []AutomationRule {
	if len(rules) <= 1 {
		return rules
	}
	return rules[:1]
}

// Get 返回指定规则及其动作。
func (a *AutomationRules) Get(ctx context.Context, ruleID int64) (*AutomationRule, error) {
	var rule AutomationRule
	var enabled int
	err := a.DB.QueryRowContext(ctx, `
SELECT r.id,r.user_id,r.cookie_id,r.item_id,COALESCE(i.item_title,''),r.name,r.trigger_type,r.enabled,
       r.priority,r.config_json,r.created_at,r.updated_at
  FROM automation_rules r
  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id
 WHERE r.id=?`, ruleID).Scan(&rule.ID, &rule.UserID, &rule.CookieID, &rule.ItemID, &rule.ItemTitle,
		&rule.Name, &rule.TriggerType, &enabled, &rule.Priority, &rule.ConfigJSON, &rule.CreatedAt, &rule.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rule.Enabled = enabled != 0
	rule.Actions, err = a.Actions(ctx, rule.ID)
	return &rule, err
}

func (a *AutomationRules) matchScope(ctx context.Context, cookieID, itemID, triggerType string) ([]AutomationRule, error) {
	rows, err := a.DB.QueryContext(ctx, `
SELECT r.id,r.user_id,r.cookie_id,r.item_id,COALESCE(i.item_title,''),r.name,r.trigger_type,r.enabled,
       r.priority,r.config_json,r.created_at,r.updated_at
  FROM automation_rules r
  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id
 WHERE r.enabled=1
   AND r.cookie_id=?
   AND r.trigger_type=?
	AND r.item_id=?
 ORDER BY r.priority ASC, r.id ASC`, cookieID, triggerType, itemID)
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
	id, err := createAutomationRuleTx(ctx, tx, a.Dialect, in)
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
	res, err := a.DB.ExecContext(ctx, `DELETE FROM automation_rules
		WHERE id=? AND user_id=? AND NOT EXISTS (
			SELECT 1 FROM automation_runs ar WHERE ar.rule_id=automation_rules.id
			  AND (ar.status IN ('running','needs_review') OR (ar.status='failed' AND ar.sent_count=0 AND ar.attempt_count<3)))`, ruleID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		var exists int
		if err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rules WHERE id=? AND user_id=?`, ruleID, userID).Scan(&exists); err != nil {
			return err
		}
		if exists > 0 {
			return ErrAutomationRunActive
		}
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
	now := time.Now().UTC().Unix()
	leaseExpiresAt := run.LeaseExpiresAt
	if leaseExpiresAt <= now {
		leaseExpiresAt = now + int64((5*time.Minute)/time.Second)
	}
	query := dialectInsertIgnorePrefix(a.Dialect) + ` INTO automation_runs
	    (rule_id,cookie_id,item_id,order_id,buyer_id,chat_id,trigger_type,trigger_key,status,raw_event_json,lease_expires_at,attempt_count,next_retry_at)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(a.Dialect, []string{"rule_id", "trigger_key"})
	args := []any{run.RuleID, run.CookieID, run.ItemID, run.OrderID, run.BuyerID, run.ChatID,
		run.TriggerType, run.TriggerKey, "running", validJSON(run.RawEventJSON), leaseExpiresAt, 1, 0}

	if a.Dialect == DialectPostgres {
		// pgx 不支持 LastInsertId；用 RETURNING id。ON CONFLICT DO NOTHING 冲突时无行返回 → 未启动。
		var id int64
		err := a.DB.QueryRowContext(ctx, query+" RETURNING id", args...).Scan(&id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return a.reclaimRun(ctx, run.RuleID, run.TriggerKey, leaseExpiresAt, now)
			}
			return 0, false, err
		}
		return id, true, nil
	}

	res, err := a.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return a.reclaimRun(ctx, run.RuleID, run.TriggerKey, leaseExpiresAt, now)
	}
	id, _ := res.LastInsertId()
	return id, true, nil
}

func (a *AutomationRules) reclaimRun(ctx context.Context, ruleID int64, triggerKey string, leaseExpiresAt, now int64) (int64, bool, error) {
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
	   SET status='running',error_message='',lease_expires_at=?,next_retry_at=0,
	       attempt_count=attempt_count+1,updated_at=CURRENT_TIMESTAMP
	 WHERE rule_id=? AND trigger_key=?
	   AND ((status='running' AND action_started=0 AND (lease_expires_at=0 OR lease_expires_at<?))
	        OR (status='failed' AND sent_count=0 AND attempt_count<3 AND next_retry_at<=?))`,
		leaseExpiresAt, ruleID, triggerKey, now, now)
	if err != nil {
		return 0, false, err
	}
	if n, rowsErr := res.RowsAffected(); rowsErr != nil || n != 1 {
		return 0, false, rowsErr
	}
	var id int64
	if err := a.DB.QueryRowContext(ctx,
		`SELECT id FROM automation_runs WHERE rule_id=? AND trigger_key=?`, ruleID, triggerKey).Scan(&id); err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// GetRun 返回自动化运行及动作检查点。
func (a *AutomationRules) GetRun(ctx context.Context, id int64) (*AutomationRun, error) {
	var run AutomationRun
	var actionStarted int
	err := a.DB.QueryRowContext(ctx, `SELECT id,rule_id,cookie_id,item_id,order_id,buyer_id,chat_id,trigger_type,trigger_key,
		status,sent_count,error_message,raw_event_json,lease_expires_at,attempt_count,next_retry_at,action_cursor,action_started
		FROM automation_runs WHERE id=?`, id).Scan(&run.ID, &run.RuleID, &run.CookieID, &run.ItemID, &run.OrderID,
		&run.BuyerID, &run.ChatID, &run.TriggerType, &run.TriggerKey, &run.Status, &run.SentCount,
		&run.ErrorMessage, &run.RawEventJSON, &run.LeaseExpiresAt, &run.AttemptCount, &run.NextRetryAt,
		&run.ActionCursor, &actionStarted)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	run.ActionStarted = actionStarted != 0
	return &run, err
}

// StartRunAction 在外部副作用前持久化 started；崩溃恢复看到 started 时不会盲目重放。
func (a *AutomationRules) StartRunAction(ctx context.Context, runID int64, attempt, cursor int, leaseExpiresAt int64) (bool, error) {
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs SET action_started=1,lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status='running' AND action_cursor=? AND action_started=0`, leaseExpiresAt, runID, attempt, cursor)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// AdvanceRunAction 在动作明确成功后推进游标并累计已发送数量。
func (a *AutomationRules) AdvanceRunAction(ctx context.Context, runID int64, attempt, cursor, sentDelta int) error {
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
		SET action_cursor=?,action_started=0,sent_count=sent_count+?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status='running' AND action_cursor=? AND action_started=1`, cursor+1, sentDelta, runID, attempt, cursor)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

func (a *AutomationRules) AbortRunAction(ctx context.Context, runID int64, attempt, cursor int) error {
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs SET action_started=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status='running' AND action_cursor=?`, runID, attempt, cursor)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

func (a *AutomationRules) RenewRunLease(ctx context.Context, runID int64, attempt int, leaseExpiresAt int64) error {
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status='running'`, leaseExpiresAt, runID, attempt)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

func (a *AutomationRules) QuarantineRun(ctx context.Context, runID int64, attempt int, reason string) error {
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
		SET status='needs_review',error_message=?,lease_expires_at=0,next_retry_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status IN ('running','failed')`, reason, runID, attempt)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

func (a *AutomationRules) QuarantineRunResult(ctx context.Context, runID int64, attempt, sentCount int, reason string) error {
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
		SET status='needs_review',sent_count=?,error_message=?,lease_expires_at=0,next_retry_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status IN ('running','failed')`, sentCount, reason, runID, attempt)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

// PostponeRecoveryRun 把暂时不能执行的账号移到恢复队列尾部，避免固定的前 100 条饿死后续任务。
func (a *AutomationRules) PostponeRecoveryRun(ctx context.Context, runID, retryAt int64) error {
	_, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
		SET lease_expires_at=CASE WHEN status='running' THEN ? ELSE lease_expires_at END,
		    next_retry_at=CASE WHEN status='failed' THEN ? ELSE next_retry_at END,
		    updated_at=CURRENT_TIMESTAMP WHERE id=? AND status IN ('running','failed')`, retryAt, retryAt, runID)
	return err
}

func (a *AutomationRules) ClaimRecoveryRun(ctx context.Context, runID, leaseExpiresAt int64) (bool, error) {
	now := time.Now().UTC().Unix()
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
		SET status='running',error_message='',lease_expires_at=?,next_retry_at=0,attempt_count=attempt_count+1,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND action_started=0 AND ((status='running' AND (lease_expires_at=0 OR lease_expires_at<?))
		 OR (status='failed' AND sent_count=0 AND attempt_count<3 AND next_retry_at<=?))`, leaseExpiresAt, runID, now, now)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// FinishRun 标记执行完成或失败。
func (a *AutomationRules) FinishRun(ctx context.Context, id int64, attempt int, status string, sentCount int, errMsg string) error {
	nextRetryAt := int64(0)
	if status == "failed" && sentCount == 0 {
		nextRetryAt = time.Now().UTC().Add(time.Minute).Unix()
	}
	res, err := a.DB.ExecContext(ctx, `
UPDATE automation_runs
	   SET status=?,sent_count=?,error_message=?,lease_expires_at=0,next_retry_at=?,updated_at=CURRENT_TIMESTAMP
	 WHERE id=? AND attempt_count=? AND status='running'`, status, sentCount, errMsg, nextRetryAt, id, attempt)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

func requireAutomationRunOwner(res sql.Result) error {
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n != 1 {
		return ErrAutomationRunLeaseLost
	}
	return nil
}

// DueRecoveryRuns 返回需要主动恢复的失败运行和租约已过期的运行。
// 真正的领取仍由 TryStartRun 完成，多个 scheduler 并发扫描也不会重复执行。
func (a *AutomationRules) DueRecoveryRuns(ctx context.Context, limit int) ([]AutomationRun, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now().UTC().Unix()
	rows, err := a.DB.QueryContext(ctx, `
SELECT id,rule_id,cookie_id,item_id,order_id,buyer_id,chat_id,trigger_type,trigger_key,
	       status,sent_count,error_message,raw_event_json,lease_expires_at,attempt_count,next_retry_at,action_cursor,action_started
  FROM automation_runs
 WHERE (status='running' AND (lease_expires_at=0 OR lease_expires_at<?))
    OR (status='failed' AND sent_count=0 AND attempt_count<3 AND next_retry_at<=?)
 ORDER BY updated_at,id LIMIT ?`, now, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AutomationRun
	for rows.Next() {
		var run AutomationRun
		var actionStarted int
		if err := rows.Scan(&run.ID, &run.RuleID, &run.CookieID, &run.ItemID, &run.OrderID,
			&run.BuyerID, &run.ChatID, &run.TriggerType, &run.TriggerKey, &run.Status,
			&run.SentCount, &run.ErrorMessage, &run.RawEventJSON, &run.LeaseExpiresAt,
			&run.AttemptCount, &run.NextRetryAt, &run.ActionCursor, &actionStarted); err != nil {
			return nil, err
		}
		run.ActionStarted = actionStarted != 0
		out = append(out, run)
	}
	return out, rows.Err()
}

// RecoverDefinitelyUnsentReviewRuns 恢复旧版本把“发送前没有 WS 连接”误判成
// 结果不确定的求评价运行。这些记录 sent_count=0，且错误明确发生在调用发送
// 接口之前，可以安全清除 action_started 并进入现有失败重试流程。
func (a *AutomationRules) RecoverDefinitelyUnsentReviewRuns(ctx context.Context) (int64, error) {
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

func (a *AutomationRules) DeferTask(ctx context.Context, task DeferredAutomationTask) error {
	_, err := a.DB.ExecContext(ctx, `INSERT INTO automation_pending_tasks
    (task_key,cookie_id,trigger_type,task_json,due_at,status,attempt_count,lease_expires_at,error_message)
VALUES (?,?,?,?,?,'pending',0,0,'')`+dialectUpsert(a.Dialect, []string{"task_key"}, map[string]string{
		"cookie_id":        "excluded.cookie_id",
		"trigger_type":     "excluded.trigger_type",
		"task_json":        "excluded.task_json",
		"due_at":           "excluded.due_at",
		"status":           "'pending'",
		"attempt_count":    "0",
		"lease_expires_at": "0",
		"error_message":    "''",
	}), task.TaskKey, task.CookieID, task.TriggerType, validJSON(task.TaskJSON), task.DueAt)
	return err
}

func (a *AutomationRules) ListIssues(ctx context.Context, userID int64) ([]AutomationRunIssue, []DeferredAutomationIssue, error) {
	runRows, err := a.DB.QueryContext(ctx, `SELECT ar.id,ar.cookie_id,ar.order_id,ar.trigger_type,ar.error_message,
		ar.action_cursor,ar.sent_count,ar.updated_at,ar.raw_event_json,ar.action_started,COALESCE(r.enabled,0)
		FROM automation_runs ar JOIN cookies c ON c.id=ar.cookie_id
		LEFT JOIN automation_rules r ON r.id=ar.rule_id
		WHERE c.user_id=? AND ar.status='needs_review' ORDER BY ar.updated_at DESC,ar.id DESC`, userID)
	if err != nil {
		return nil, nil, err
	}
	runs := []AutomationRunIssue{}
	for runRows.Next() {
		var issue AutomationRunIssue
		var rawEventJSON string
		var actionStarted, ruleEnabled int
		if err := runRows.Scan(&issue.ID, &issue.CookieID, &issue.OrderID, &issue.TriggerType, &issue.ErrorMessage,
			&issue.ActionCursor, &issue.SentCount, &issue.UpdatedAt, &rawEventJSON, &actionStarted, &ruleEnabled); err != nil {
			_ = runRows.Close()
			return nil, nil, err
		}
		issue.IssueKind, issue.AllowedResolutions = automationIssuePolicy(
			rawEventJSON, actionStarted != 0, ruleEnabled != 0, issue.SentCount, issue.ErrorMessage,
		)
		runs = append(runs, issue)
	}
	if err := runRows.Close(); err != nil {
		return nil, nil, err
	}
	taskRows, err := a.DB.QueryContext(ctx, `SELECT apt.id,apt.cookie_id,apt.trigger_type,apt.error_message,
		apt.attempt_count,apt.updated_at
		FROM automation_pending_tasks apt JOIN cookies c ON c.id=apt.cookie_id
		WHERE c.user_id=? AND apt.status='dead_letter' ORDER BY apt.updated_at DESC,apt.id DESC`, userID)
	if err != nil {
		return nil, nil, err
	}
	defer taskRows.Close()
	tasks := []DeferredAutomationIssue{}
	for taskRows.Next() {
		var issue DeferredAutomationIssue
		if err := taskRows.Scan(&issue.ID, &issue.CookieID, &issue.TriggerType, &issue.ErrorMessage,
			&issue.AttemptCount, &issue.UpdatedAt); err != nil {
			return nil, nil, err
		}
		tasks = append(tasks, issue)
	}
	return runs, tasks, taskRows.Err()
}

func (a *AutomationRules) ResolveRunIssue(ctx context.Context, userID, runID int64, resolution string) error {
	var rawEventJSON, errorMessage string
	var actionStarted, ruleEnabled, sentCount int
	err := a.DB.QueryRowContext(ctx, `SELECT ar.raw_event_json,ar.action_started,COALESCE(r.enabled,0),ar.sent_count,ar.error_message
		FROM automation_runs ar JOIN cookies c ON c.id=ar.cookie_id
		LEFT JOIN automation_rules r ON r.id=ar.rule_id
		WHERE ar.id=? AND ar.status='needs_review' AND c.user_id=?`, runID, userID).
		Scan(&rawEventJSON, &actionStarted, &ruleEnabled, &sentCount, &errorMessage)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	_, allowed := automationIssuePolicy(rawEventJSON, actionStarted != 0, ruleEnabled != 0, sentCount, errorMessage)
	if !containsString(allowed, resolution) {
		return fmt.Errorf("当前异常不允许使用 %s 处理", resolution)
	}
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
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs SET `+set+`,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='needs_review' AND cookie_id IN (SELECT id FROM cookies WHERE user_id=?)`, runID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

func automationIssuePolicy(rawEventJSON string, actionStarted, ruleEnabled bool, sentCount int, errorMessage string) (string, []string) {
	if actionStarted {
		// 外部接口没有可依赖的幂等键。结果未知时重放当前游标可能重复发卡、
		// 发消息或确认发货，因此后端也必须拒绝 retry，不能只依赖前端警告。
		return "external_result_unknown", []string{"continue", "cancel"}
	}
	var raw map[string]any
	if json.Unmarshal([]byte(rawEventJSON), &raw) != nil {
		return "invalid_snapshot", []string{"cancel"}
	}
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func (a *AutomationRules) ResolveDeferredIssue(ctx context.Context, userID, taskID int64, retry bool) error {
	if retry {
		res, err := a.DB.ExecContext(ctx, `UPDATE automation_pending_tasks
			SET status='pending',attempt_count=0,due_at=0,lease_expires_at=0,error_message='',updated_at=CURRENT_TIMESTAMP
			WHERE id=? AND status='dead_letter' AND cookie_id IN (SELECT id FROM cookies WHERE user_id=?)`, taskID, userID)
		if err != nil {
			return err
		}
		if n, _ := res.RowsAffected(); n != 1 {
			return ErrNotFound
		}
		return nil
	}
	res, err := a.DB.ExecContext(ctx, `DELETE FROM automation_pending_tasks
		WHERE id=? AND status='dead_letter' AND cookie_id IN (SELECT id FROM cookies WHERE user_id=?)`, taskID, userID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n != 1 {
		return ErrNotFound
	}
	return nil
}

func (a *AutomationRules) ClaimDueDeferredTasks(ctx context.Context, limit int) ([]DeferredAutomationTask, error) {
	if limit <= 0 {
		limit = 100
	}
	now := time.Now().UTC().Unix()
	rows, err := a.DB.QueryContext(ctx, `SELECT id,task_key,cookie_id,trigger_type,task_json,due_at,attempt_count
  FROM automation_pending_tasks
	 WHERE ((status='pending' AND due_at<=?) OR (status='running' AND lease_expires_at<?)) AND attempt_count<5
 ORDER BY due_at,id LIMIT ?`, now, now, limit)
	if err != nil {
		return nil, err
	}
	candidates := []DeferredAutomationTask{}
	for rows.Next() {
		var task DeferredAutomationTask
		if err := rows.Scan(&task.ID, &task.TaskKey, &task.CookieID, &task.TriggerType, &task.TaskJSON, &task.DueAt, &task.ClaimVersion); err != nil {
			_ = rows.Close()
			return nil, err
		}
		candidates = append(candidates, task)
	}
	if err := rows.Close(); err != nil {
		return nil, err
	}
	claimed := make([]DeferredAutomationTask, 0, len(candidates))
	for _, task := range candidates {
		res, err := a.DB.ExecContext(ctx, `UPDATE automation_pending_tasks
   SET status='running',attempt_count=attempt_count+1,lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
	 WHERE id=? AND attempt_count<5 AND ((status='pending' AND due_at<=?) OR (status='running' AND lease_expires_at<?))`, now+300, task.ID, now, now)
		if err != nil {
			return nil, err
		}
		if n, _ := res.RowsAffected(); n == 1 {
			task.ClaimVersion++
			claimed = append(claimed, task)
		}
	}
	return claimed, nil
}

func (a *AutomationRules) FinishDeferredTask(ctx context.Context, id int64, claimVersion int, success bool, errMsg string) error {
	if success {
		res, err := a.DB.ExecContext(ctx, `DELETE FROM automation_pending_tasks
			WHERE id=? AND status='running' AND attempt_count=?`, id, claimVersion)
		return requireDeferredTaskOwner(res, err)
	}
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_pending_tasks
	   SET status=CASE WHEN attempt_count>=5 THEN 'dead_letter' ELSE 'pending' END,
	       due_at=?,lease_expires_at=0,error_message=?,updated_at=CURRENT_TIMESTAMP
	 WHERE id=? AND status='running' AND attempt_count=?`, time.Now().UTC().Add(time.Minute).Unix(), errMsg, id, claimVersion)
	return requireDeferredTaskOwner(res, err)
}

func (a *AutomationRules) RenewDeferredTaskLease(ctx context.Context, id int64, claimVersion int, leaseExpiresAt int64) error {
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_pending_tasks
		SET lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND status='running' AND attempt_count=?`, leaseExpiresAt, id, claimVersion)
	return requireDeferredTaskOwner(res, err)
}

func requireDeferredTaskOwner(res sql.Result, err error) error {
	if err != nil {
		return err
	}
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
	now := time.Now().UTC().Format(time.RFC3339Nano)
	_, err := a.DB.ExecContext(ctx, "UPDATE orders SET "+field+"=?, updated_at=CURRENT_TIMESTAMP WHERE order_id=? AND COALESCE("+field+",'')=''", now, orderID)
	return err
}

// IncrementReviewRequest 记录一次求评价请求。
func (a *AutomationRules) IncrementReviewRequest(ctx context.Context, orderID string) error {
	now := time.Now().UTC().Format(time.RFC3339Nano)
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
func (a *AutomationRules) DueReviewRequestOrdersAfter(ctx context.Context, afterOrderID string, limit int) ([]Order, error) {
	if limit <= 0 {
		limit = 200
	}
	cursorSQL := ""
	args := []any{}
	if afterOrderID != "" {
		cursorSQL = " AND o.order_id>?"
		args = append(args, afterOrderID)
	}
	args = append(args, limit)
	rows, err := a.DB.QueryContext(ctx, `
SELECT order_id,item_id,buyer_id,spec_name,spec_value,quantity,amount,order_status,cookie_id,is_bargain,
       receiver_name,receiver_phone,receiver_address,receiver_city,version,chat_id,system_shipped,
       COALESCE(paid_at,''),COALESCE(shipped_at,''),COALESCE(completed_at,''),
       COALESCE(buyer_reviewed_at,''),COALESCE(last_review_request_at,''),review_request_count,
       created_at,updated_at
  FROM orders o
 WHERE o.system_shipped=1
   AND COALESCE(o.buyer_reviewed_at,'')=''
   AND COALESCE(o.chat_id,'')<>''
   AND EXISTS (SELECT 1 FROM automation_rules r
                WHERE r.cookie_id=o.cookie_id AND r.trigger_type='review_missing_timeout'
                  AND r.enabled=1 AND (r.item_id=o.item_id OR r.item_id=''))`+cursorSQL+`
 ORDER BY o.order_id ASC
	 LIMIT ?`, args...)
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

func createAutomationRuleTx(ctx context.Context, tx *sql.Tx, dialect Dialect, in AutomationRuleInput) (int64, error) {
	if in.Priority <= 0 {
		in.Priority = 100
	}
	id, err := insertReturningID(ctx, tx, dialect, `
INSERT INTO automation_rules (user_id,cookie_id,item_id,name,trigger_type,enabled,priority,config_json)
VALUES (?,?,?,?,?,?,?,?)`,
		in.UserID, in.CookieID, in.ItemID, in.Name, in.TriggerType, boolToInt(in.Enabled), in.Priority, validJSON(in.ConfigJSON))
	if err != nil {
		return 0, err
	}
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
