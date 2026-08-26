package db

import (
	"context"
	"database/sql"
	"errors"
	"strings"
	"time"
)

// ErrAutomationRunActive 用于本次流程后续判断的Err自动化运行Active
var ErrAutomationRunActive = errors.New("规则仍有待处理的自动化运行")

// SafeRetryErrorPrefix 标记当前动作明确没有产生外部副作用，可以从动作游标安全恢复。
const SafeRetryErrorPrefix = "[safe_retry]"

// NoRetryErrorPrefix 标记当前动作明确不应进入自动化恢复队列。
const NoRetryErrorPrefix = "[no_retry]"

// AutomationRules 管理自动化规则、动作和执行记录。
//
// 自动化中心不区分触发来源：WS 系统事件、计划任务、后台手动触发都通过
// trigger_type + action 编排表达；真正的防重由 automation_runs.trigger_key 保证。
// AutomationRules 用于本次流程后续判断的自动化规则列表
type AutomationRules struct {
	DB      *sql.DB
	Dialect Dialect
	// codec 负责自动化运行中卡密凭证的静态加密，避免凭证以明文落库。
	codec *secretCodec
}

// AutomationDeliveryProof 保存确认发货需要的已投递文本和图片凭证。
type AutomationDeliveryProof struct {
	// TradeText 是已发送给买家的文本凭证，多个动作按顺序合并。
	TradeText string `json:"trade_text"`
	// PicList 是已发送给买家的图片地址，顺序与消息发送顺序一致。
	PicList []string `json:"pic_list"`
}

// AutomationRunActionAdvance 描述动作成功后的原子检查点更新。
type AutomationRunActionAdvance struct {
	// RunID 是待推进的自动化运行标识。
	RunID int64
	// Attempt 是当前运行租约代次，防止旧 worker 覆盖新 worker。
	Attempt int
	// Cursor 是动作执行前的游标位置。
	Cursor int
	// SentDelta 是本动作明确成功的外部结果数量。
	SentDelta int
	// DeliveryProof 是动作完成后应保留的完整凭证；为空指针表示保持已有值。
	DeliveryProof *AutomationDeliveryProof
	// ClearDeliveryProof 表示动作成功后应立即清除凭证。
	ClearDeliveryProof bool
}

// HasEnabledAdjustPriceRule 判断账号是否存在会实际执行改价动作的启用规则。
func (a *AutomationRules) HasEnabledAdjustPriceRule(ctx context.Context, cookieID string) (bool, error) {
	// exists 表示启用规则下是否至少存在一个启用的订单改价动作。
	var exists bool
	// err 是互斥模式查询失败原因。
	err := a.DB.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM automation_rules r
		JOIN automation_rule_actions action ON action.rule_id=r.id
		WHERE r.cookie_id=? AND r.enabled=1 AND r.deleted_at IS NULL
		  AND action.enabled=1 AND action.action_type='adjust_price'
	)`, cookieID).Scan(&exists)
	return exists, err
}

// ExistsPublishRule 判断指定发布自动化规则是否已经存在，避免重复创建同一规则。
func (a *AutomationRules) ExistsPublishRule(ctx context.Context, input AutomationRuleInput) (bool, error) {
	// exists 用于本次流程后续判断的exists
	var exists bool
	// err 用于本次流程后续判断的err
	err := a.DB.QueryRowContext(ctx, `SELECT EXISTS(
		SELECT 1 FROM automation_rules
		 WHERE user_id=? AND cookie_id=? AND item_id=? AND trigger_type=? AND name=? AND deleted_at IS NULL
	)`, input.UserID, input.CookieID, input.ItemID, input.TriggerType, input.Name).Scan(&exists)
	return exists, err
}

// AutomationRule 是一条自动化规则。规则只描述“什么时候、对哪个商品生效”，
// 具体做什么放在 AutomationAction 中，便于组合付款发货、评价赠品、求评价等流程。
// AutomationRule 用于本次流程后续判断的自动化规则
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
	// DeliveryProof 是恢复确认发货所需的短期敏感凭证，仅在数据库仓储和自动化执行器之间流转。
	DeliveryProof AutomationDeliveryProof
}

// ErrAutomationRunLeaseLost 表示自动化运行已被更高 attempt_count 的 worker 接管。
var ErrAutomationRunLeaseLost = errors.New("自动化运行租约已失效")

// DeferredAutomationTask 用于本次流程后续判断的Deferred自动化任务
type DeferredAutomationTask struct {
	ID           int64
	TaskKey      string
	CookieID     string
	TriggerType  string
	TaskJSON     string
	DueAt        int64
	ClaimVersion int
	ErrorMessage string
}

// ErrDeferredTaskLeaseLost 用于本次流程后续判断的ErrDeferred任务LeaseLost
var ErrDeferredTaskLeaseLost = errors.New("延迟自动化任务租约已失效")

// AutomationRunIssue 用于本次流程后续判断的自动化运行问题
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

// DeferredAutomationIssue 用于本次流程后续判断的Deferred自动化问题
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

// automationRuleWhere 封装自动化规则Where业务协调。
func automationRuleWhere(f AutomationRuleListFilter) (string, []any) {
	// where 用于本次流程后续判断的where
	where := []string{"r.user_id=?", "r.deleted_at IS NULL"}
	// args 用于本次流程后续判断的args
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
	if // search 用于本次流程后续判断的搜索
	search := strings.ToLower(strings.TrimSpace(f.Search)); search != "" {
		// pattern 用于本次流程后续判断的pattern
		pattern := "%" + search + "%"
		where = append(where, `(LOWER(COALESCE(r.name,'')) LIKE ?
			OR LOWER(COALESCE(r.item_id,'')) LIKE ?
			OR LOWER(COALESCE(i.item_title,'')) LIKE ?)`)
		args = append(args, pattern, pattern, pattern)
	}
	return strings.Join(where, " AND "), args
}

// ListForUser 返回用户下全部自动化规则和动作。
func (a *AutomationRules) ListForUser(ctx context.Context, userID int64) ([]AutomationRule, error) {
	// rules、err 用于本次流程后续判断的rules、err
	rules, _, err := a.ListPageForUser(ctx, AutomationRuleListFilter{UserID: userID})
	return rules, err
}

// ListPageForUser 按用户隔离筛选并分页查询自动化规则和动作。
func (a *AutomationRules) ListPageForUser(ctx context.Context, f AutomationRuleListFilter) ([]AutomationRule, int, error) {
	// whereSQL、args 用于本次流程后续判断的whereSQL、args
	whereSQL, args := automationRuleWhere(f)

	// total 用于本次流程后续判断的总数
	var total int
	if // err 用于本次流程后续判断的err
	err := a.DB.QueryRowContext(ctx, `
SELECT COUNT(*)
  FROM automation_rules r
	  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id AND i.deleted_at IS NULL
 WHERE `+whereSQL, args...).Scan(&total); err != nil {
		return nil, 0, err
	}

	// queryArgs 用于本次流程后续判断的查询Args
	queryArgs := append([]any{}, args...)
	// limitSQL 用于本次流程后续判断的上限SQL
	limitSQL := ""
	if f.Limit > 0 {
		limitSQL = " LIMIT ? OFFSET ?"
		queryArgs = append(queryArgs, f.Limit, f.Offset)
	}
	// rows、err 用于本次流程后续判断的rows、err
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
	defer rows.Close()
	// out 用于本次流程后续判断的out
	out := []AutomationRule{}
	for rows.Next() {
		// r 用于本次流程后续判断的r
		var r AutomationRule
		// enabled 用于本次流程后续判断的启用状态
		var enabled int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&r.ID, &r.UserID, &r.CookieID, &r.ItemID, &r.ItemTitle, &r.Name, &r.TriggerType,
			&enabled, &r.Priority, &r.ConfigJSON, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, 0, err
		}
		r.Enabled = enabled != 0
		// acts、err 用于本次流程后续判断的acts、err
		acts, err := a.Actions(ctx, r.ID)
		if err != nil {
			return nil, 0, err
		}
		r.Actions = acts
		out = append(out, r)
	}
	return out, total, rows.Err()
}

// CountByTriggerForUser 返回同一筛选条件下各触发类型的规则数量。
// 该统计不受分页影响，确保页面汇总与 total 使用同一数据集。
// CountByTriggerForUser 封装数量ByTriggerFor用户业务协调。
func (a *AutomationRules) CountByTriggerForUser(ctx context.Context, f AutomationRuleListFilter) (map[string]int, error) {
	// whereSQL、args 用于本次流程后续判断的whereSQL、args
	whereSQL, args := automationRuleWhere(f)
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := a.DB.QueryContext(ctx, `
SELECT r.trigger_type, COUNT(*)
  FROM automation_rules r
  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id AND i.deleted_at IS NULL
 WHERE `+whereSQL+`
 GROUP BY r.trigger_type`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// counts 用于本次流程后续判断的counts
	counts := map[string]int{}
	for rows.Next() {
		// triggerType 用于本次流程后续判断的trigger类型
		var triggerType string
		// count 用于本次流程后续判断的数量
		var count int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&triggerType, &count); err != nil {
			return nil, err
		}
		counts[triggerType] = count
	}
	return counts, rows.Err()
}

// Match 查询某事件可触发的规则。商品级规则存在时只返回商品级规则；
// 没有商品级规则时才回退到账号级规则，避免两层规则叠加导致重复发货。
// Match 封装Match业务协调。
func (a *AutomationRules) Match(ctx context.Context, cookieID, itemID, triggerType string) ([]AutomationRule, error) {
	// out、err 用于本次流程后续判断的out、err
	out, err := a.matchScope(ctx, cookieID, itemID, triggerType)
	if err != nil || len(out) > 0 || itemID == "" {
		return highestPriorityRule(out), err
	}
	out, err = a.matchScope(ctx, cookieID, "", triggerType)
	return highestPriorityRule(out), err
}

// highestPriorityRule 封装highest优先级规则业务协调。
func highestPriorityRule(rules []AutomationRule) []AutomationRule {
	if len(rules) <= 1 {
		return rules
	}
	return rules[:1]
}

// Get 返回指定规则及其动作。
func (a *AutomationRules) Get(ctx context.Context, ruleID int64) (*AutomationRule, error) {
	// rule 用于本次流程后续判断的规则
	var rule AutomationRule
	// enabled 用于本次流程后续判断的启用状态
	var enabled int
	// err 用于本次流程后续判断的err
	err := a.DB.QueryRowContext(ctx, `
SELECT r.id,r.user_id,r.cookie_id,r.item_id,COALESCE(i.item_title,''),r.name,r.trigger_type,r.enabled,
       r.priority,r.config_json,r.created_at,r.updated_at
  FROM automation_rules r
	  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id AND i.deleted_at IS NULL
	 WHERE r.id=? AND r.deleted_at IS NULL`, ruleID).Scan(&rule.ID, &rule.UserID, &rule.CookieID, &rule.ItemID, &rule.ItemTitle,
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

// matchScope 封装matchScope业务协调。
func (a *AutomationRules) matchScope(ctx context.Context, cookieID, itemID, triggerType string) ([]AutomationRule, error) {
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := a.DB.QueryContext(ctx, `
SELECT r.id,r.user_id,r.cookie_id,r.item_id,COALESCE(i.item_title,''),r.name,r.trigger_type,r.enabled,
       r.priority,r.config_json,r.created_at,r.updated_at
  FROM automation_rules r
  LEFT JOIN item_info i ON i.cookie_id=r.cookie_id AND i.item_id=r.item_id AND i.deleted_at IS NULL
 WHERE r.deleted_at IS NULL
   AND r.enabled=1
	AND r.cookie_id=?
	AND r.trigger_type=?
	AND r.item_id=?
	ORDER BY r.priority ASC, r.id ASC`, cookieID, triggerType, itemID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 用于本次流程后续判断的out
	out := []AutomationRule{}
	for rows.Next() {
		// r 用于本次流程后续判断的r
		var r AutomationRule
		// enabled 用于本次流程后续判断的启用状态
		var enabled int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&r.ID, &r.UserID, &r.CookieID, &r.ItemID, &r.ItemTitle, &r.Name, &r.TriggerType,
			&enabled, &r.Priority, &r.ConfigJSON, &r.CreatedAt, &r.UpdatedAt); err != nil {
			return nil, err
		}
		r.Enabled = enabled != 0
		// acts、err 用于本次流程后续判断的acts、err
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
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	// err 保存模板锁定及变量契约复核错误，规则写入必须与该校验处于同一事务。
	if err := validateAutomationTemplateContractsTx(ctx, tx, a.Dialect, in.UserID, in.Actions); err != nil {
		return 0, err
	}
	// id、err 用于本次流程后续判断的id、err
	id, err := createAutomationRuleTx(ctx, tx, a.Dialect, in)
	if err != nil {
		return 0, err
	}
	if // err 用于本次流程后续判断的err
	err := tx.Commit(); err != nil {
		return 0, err
	}
	return id, nil
}

// Update 替换规则和动作。动作采用删除重建，避免前端携带展示字段造成局部更新不一致。
func (a *AutomationRules) Update(ctx context.Context, userID, ruleID int64, in AutomationRuleInput) error {
	// tx、err 用于本次流程后续判断的tx、err
	tx, err := a.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	// err 保存新动作模板锁定及变量契约复核错误，必须先于旧动作删除执行。
	if err := validateAutomationTemplateContractsTx(ctx, tx, a.Dialect, userID, in.Actions); err != nil {
		return err
	}
	// res、err 用于本次流程后续判断的res、err
	res, err := tx.ExecContext(ctx, `
UPDATE automation_rules
   SET cookie_id=?,item_id=?,name=?,trigger_type=?,enabled=?,priority=?,config_json=?,updated_at=CURRENT_TIMESTAMP
	 WHERE id=? AND user_id=? AND deleted_at IS NULL`,
		in.CookieID, in.ItemID, in.Name, in.TriggerType, boolToInt(in.Enabled), in.Priority, validJSON(in.ConfigJSON), ruleID, userID)
	if err != nil {
		return err
	}
	if // n 用于本次流程后续判断的n
	n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if // err 用于本次流程后续判断的err
	_, err := tx.ExecContext(ctx, `DELETE FROM automation_rule_actions WHERE rule_id=?`, ruleID); err != nil {
		return err
	}
	// act 表示当前遍历过程中的act
	for _, act := range in.Actions {
		if // err 用于本次流程后续判断的err
		_, err := insertAutomationActionTx(ctx, tx, a.Dialect, ruleID, act); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// Delete 逻辑删除规则，保留动作和执行记录以便审计；逻辑删除后不再出现在规则列表和匹配结果中。
func (a *AutomationRules) Delete(ctx context.Context, userID, ruleID int64) error {
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_rules
		SET deleted_at=CURRENT_TIMESTAMP, enabled=0, updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND user_id=? AND deleted_at IS NULL AND NOT EXISTS (
			SELECT 1 FROM automation_runs ar WHERE ar.rule_id=automation_rules.id
			  AND (ar.status IN ('running','needs_review') OR (ar.status='failed' AND ar.sent_count=0 AND ar.attempt_count<3 AND ar.error_message NOT LIKE '[no_retry]%')))`, ruleID, userID)
	if err != nil {
		return err
	}
	if // n 用于本次流程后续判断的n
	n, _ := res.RowsAffected(); n == 0 {
		// exists 用于本次流程后续判断的exists
		var exists int
		if // err 用于本次流程后续判断的err
		err := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_rules WHERE id=? AND user_id=? AND deleted_at IS NULL`, ruleID, userID).Scan(&exists); err != nil {
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
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := a.DB.QueryContext(ctx, `
SELECT a.id,a.rule_id,a.action_type,COALESCE(a.card_id,0),COALESCE(c.name,''),a.delivery_count,
       a.message_template,a.delay_seconds,a.config_json,a.enabled,a.sort_order,
       COALESCE(a.delivery_template_id,0),COALESCE(t.name,'')
  FROM automation_rule_actions a
  LEFT JOIN cards c ON c.id=a.card_id
  LEFT JOIN delivery_templates t ON t.id=a.delivery_template_id
 WHERE a.rule_id=?
 ORDER BY a.sort_order ASC,a.id ASC`, ruleID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 用于本次流程后续判断的out
	out := []AutomationAction{}
	for rows.Next() {
		// act 用于本次流程后续判断的act
		var act AutomationAction
		// enabled 用于本次流程后续判断的启用状态
		var enabled int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&act.ID, &act.RuleID, &act.ActionType, &act.CardID, &act.CardName,
			&act.DeliveryCount, &act.MessageTemplate, &act.DelaySeconds, &act.ConfigJSON, &enabled,
			&act.SortOrder, &act.DeliveryTemplateID, &act.DeliveryTemplateName); err != nil {
			return nil, err
		}
		act.Enabled = enabled != 0
		if act.DeliveryTemplateID > 0 {
			// err 保存模板动作详情加载错误。
			if err := a.loadTemplateAction(ctx, &act); err != nil {
				return nil, err
			}
		}
		out = append(out, act)
	}
	return out, rows.Err()
}

// TryStartRun 以 UNIQUE(rule_id, trigger_key) 作为持久化防重。
// 返回 started=false 表示该规则对该触发已执行或正在执行，调用方应直接跳过。
// TryStartRun 封装Try开始运行业务协调。
func (a *AutomationRules) TryStartRun(ctx context.Context, run AutomationRun) (int64, bool, error) {
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Unix()
	// leaseExpiresAt 用于本次流程后续判断的leaseExpiresAt
	leaseExpiresAt := run.LeaseExpiresAt
	if leaseExpiresAt <= now {
		leaseExpiresAt = now + int64((5*time.Minute)/time.Second)
	}
	// query 用于本次流程后续判断的查询
	query := dialectInsertIgnorePrefix(a.Dialect) + ` INTO automation_runs
	    (rule_id,cookie_id,item_id,order_id,buyer_id,chat_id,trigger_type,trigger_key,status,raw_event_json,delivery_proof,lease_expires_at,attempt_count,next_retry_at)
	VALUES (?,?,?,?,?,?,?,?,?,?,?,?,?,?)` + dialectInsertIgnore(a.Dialect, []string{"rule_id", "trigger_key"})
	// args 用于本次流程后续判断的args
	args := []any{run.RuleID, run.CookieID, run.ItemID, run.OrderID, run.BuyerID, run.ChatID,
		run.TriggerType, run.TriggerKey, "running", validJSON(run.RawEventJSON), "", leaseExpiresAt, 1, 0}

	if a.Dialect == DialectPostgres {
		// pgx 不支持 LastInsertId；用 RETURNING id。ON CONFLICT DO NOTHING 冲突时无行返回 → 未启动。
		var id int64
		// err 用于本次流程后续判断的err
		err := a.DB.QueryRowContext(ctx, query+" RETURNING id", args...).Scan(&id)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				return a.reclaimRun(ctx, run.RuleID, run.TriggerKey, leaseExpiresAt, now)
			}
			return 0, false, err
		}
		return id, true, nil
	}

	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, query, args...)
	if err != nil {
		return 0, false, err
	}
	if // n 用于本次流程后续判断的n
	n, _ := res.RowsAffected(); n == 0 {
		return a.reclaimRun(ctx, run.RuleID, run.TriggerKey, leaseExpiresAt, now)
	}
	// id 用于本次流程后续判断的标识
	id, _ := res.LastInsertId()
	return id, true, nil
}

// reclaimRun 封装reclaim运行业务协调。
func (a *AutomationRules) reclaimRun(ctx context.Context, ruleID int64, triggerKey string, leaseExpiresAt, now int64) (int64, bool, error) {
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
	   SET status='running',error_message='',lease_expires_at=?,next_retry_at=0,
	       attempt_count=attempt_count+1,updated_at=CURRENT_TIMESTAMP
	 WHERE rule_id=? AND trigger_key=?
	   AND ((status='running' AND action_started=0 AND (lease_expires_at=0 OR lease_expires_at<?))
	        OR (status='failed' AND action_started=0 AND attempt_count<3 AND next_retry_at<=?
	            AND ((sent_count=0 AND error_message NOT LIKE '[no_retry]%') OR error_message LIKE '[safe_retry]%')))`,
		leaseExpiresAt, ruleID, triggerKey, now, now)
	if err != nil {
		return 0, false, err
	}
	if // n、rowsErr 用于本次流程后续判断的n、rowsErr
	n, rowsErr := res.RowsAffected(); rowsErr != nil || n != 1 {
		return 0, false, rowsErr
	}
	// id 用于本次流程后续判断的标识
	var id int64
	if // err 用于本次流程后续判断的err
	err := a.DB.QueryRowContext(ctx,
		`SELECT id FROM automation_runs WHERE rule_id=? AND trigger_key=?`, ruleID, triggerKey).Scan(&id); err != nil {
		return 0, false, err
	}
	return id, true, nil
}

// StartRunAction 在外部副作用前持久化 started；崩溃恢复看到 started 时不会盲目重放。
func (a *AutomationRules) StartRunAction(ctx context.Context, runID int64, attempt, cursor int, leaseExpiresAt int64) (bool, error) {
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs SET action_started=1,lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status='running' AND action_cursor=? AND action_started=0`, leaseExpiresAt, runID, attempt, cursor)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// AbortRunAction 封装Abort运行动作业务协调。
func (a *AutomationRules) AbortRunAction(ctx context.Context, runID int64, attempt, cursor int) error {
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs SET action_started=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status='running' AND action_cursor=?`, runID, attempt, cursor)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

// RenewRunLease 封装Renew运行Lease业务协调。
func (a *AutomationRules) RenewRunLease(ctx context.Context, runID int64, attempt int, leaseExpiresAt int64) error {
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=?,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status='running'`, leaseExpiresAt, runID, attempt)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

// QuarantineRun 封装Quarantine运行业务协调。
func (a *AutomationRules) QuarantineRun(ctx context.Context, runID int64, attempt int, reason string) error {
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
		SET status='needs_review',error_message=?,lease_expires_at=0,next_retry_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status IN ('running','failed')`, reason, runID, attempt)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

// QuarantineRunResult 封装Quarantine运行结果业务协调。
func (a *AutomationRules) QuarantineRunResult(ctx context.Context, runID int64, attempt, sentCount int, reason string) error {
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
		SET status='needs_review',sent_count=?,error_message=?,lease_expires_at=0,next_retry_at=0,updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND status IN ('running','failed')`, sentCount, reason, runID, attempt)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

// PostponeRecoveryRun 把暂时不能执行的账号移到恢复队列尾部，避免固定的前 100 条饿死后续任务。
func (a *AutomationRules) PostponeRecoveryRun(ctx context.Context, runID int64, attempt int, retryAt int64) error {
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
		SET lease_expires_at=CASE WHEN status='running' THEN ? ELSE lease_expires_at END,
		    next_retry_at=CASE WHEN status='failed' THEN ? ELSE next_retry_at END,
		    updated_at=CURRENT_TIMESTAMP
		WHERE id=? AND attempt_count=? AND action_started=0 AND status IN ('running','failed')`, retryAt, retryAt, runID, attempt)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

// ClaimRecoveryRun 封装ClaimRecovery运行业务协调。
func (a *AutomationRules) ClaimRecoveryRun(ctx context.Context, runID, leaseExpiresAt int64) (bool, error) {
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Unix()
	// res、err 用于本次流程后续判断的res、err
	res, err := a.DB.ExecContext(ctx, `UPDATE automation_runs
		SET status='running',error_message='',lease_expires_at=?,next_retry_at=0,attempt_count=attempt_count+1,updated_at=CURRENT_TIMESTAMP
		 WHERE id=? AND action_started=0 AND ((status='running' AND (lease_expires_at=0 OR lease_expires_at<?))
		 OR (status='failed' AND attempt_count<3 AND next_retry_at<=?
		     AND ((sent_count=0 AND error_message NOT LIKE '[no_retry]%') OR error_message LIKE '[safe_retry]%')))`, leaseExpiresAt, runID, now, now)
	if err != nil {
		return false, err
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := res.RowsAffected()
	return err == nil && n == 1, err
}

// FinishRun 标记执行完成或失败。
func (a *AutomationRules) FinishRun(ctx context.Context, id int64, attempt int, status string, sentCount int, errMsg string) error {
	// nextRetryAt 用于本次流程后续判断的next重试At
	nextRetryAt := int64(0)
	if status == "failed" && (strings.HasPrefix(errMsg, SafeRetryErrorPrefix) || sentCount == 0 && !strings.HasPrefix(errMsg, NoRetryErrorPrefix)) {
		nextRetryAt = time.Now().UTC().Add(time.Minute).Unix()
	}
	// clearProof 表示本次终态不会再自动恢复，因此应立即清理敏感发货凭证。
	clearProof := nextRetryAt == 0
	// res、err 用于本次流程后续判断的res、err
	query := `
UPDATE automation_runs
	   SET status=?,sent_count=?,error_message=?,lease_expires_at=0,next_retry_at=?,delivery_proof=CASE WHEN ? THEN '' ELSE delivery_proof END,updated_at=CURRENT_TIMESTAMP
	 WHERE id=? AND attempt_count=? AND status='running'`
	// res、err 保存运行终态更新结果及数据库错误。
	res, err := a.DB.ExecContext(ctx, query, status, sentCount, errMsg, nextRetryAt, clearProof, id, attempt)
	if err != nil {
		return err
	}
	return requireAutomationRunOwner(res)
}

// requireAutomationRunOwner 封装require自动化运行所有者业务协调。
func requireAutomationRunOwner(res sql.Result) error {
	// n、err 用于本次流程后续判断的n、err
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
// DueRecoveryRuns 封装DueRecovery运行记录业务协调。
func (a *AutomationRules) DueRecoveryRuns(ctx context.Context, limit int) ([]AutomationRun, error) {
	if limit <= 0 {
		limit = 100
	}
	// now 用于本次流程后续判断的now
	now := time.Now().UTC().Unix()
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := a.DB.QueryContext(ctx, `
SELECT id,rule_id,cookie_id,item_id,order_id,buyer_id,chat_id,trigger_type,trigger_key,
	       status,sent_count,error_message,raw_event_json,lease_expires_at,attempt_count,next_retry_at,action_cursor,action_started
  FROM automation_runs
 WHERE (status='running' AND (lease_expires_at=0 OR lease_expires_at<?))
	    OR (status='failed' AND action_started=0 AND attempt_count<3 AND next_retry_at<=?
	        AND ((sent_count=0 AND error_message NOT LIKE '[no_retry]%') OR error_message LIKE '[safe_retry]%'))
 ORDER BY updated_at,id LIMIT ?`, now, now, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 用于本次流程后续判断的out
	var out []AutomationRun
	for rows.Next() {
		// run 用于本次流程后续判断的运行
		var run AutomationRun
		// actionStarted 用于本次流程后续判断的动作Started
		var actionStarted int
		if // err 用于本次流程后续判断的err
		err := rows.Scan(&run.ID, &run.RuleID, &run.CookieID, &run.ItemID, &run.OrderID,
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
// RecoverDefinitelyUnsentReviewRuns 封装RecoverDefinitelyUnsentReview运行记录业务协调。
