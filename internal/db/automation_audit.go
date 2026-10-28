package db

import (
	"context"
	"strings"
)

const (
	// AuditStatusQueued 表示事件已持久化排队，等待延迟到期或账号恢复后继续执行。
	AuditStatusQueued = "queued"
	// AuditStatusRunning 表示自动化运行已取得执行权并开始执行动作。
	AuditStatusRunning = "running"
	// AuditStatusSuccess 表示动作或整次运行已确认成功。
	AuditStatusSuccess = "success"
	// AuditStatusFailed 表示动作或整次运行失败，可能由兜底任务重试。
	AuditStatusFailed = "failed"
	// AuditStatusSkipped 表示事件被门禁拦截而未执行任何外部动作。
	AuditStatusSkipped = "skipped"
	// AuditStatusNeedsReview 表示外部结果不确定或自动重放被禁止，需要人工核对。
	AuditStatusNeedsReview = "needs_review"
	// AuditStatusCanceled 表示运行因订单状态变化等原因被取消。
	AuditStatusCanceled = "canceled"
)

// AutomationAuditEvent 是自动化行为审计表中的一条只追加事件，不包含卡密正文、消息正文或任何凭证。
type AutomationAuditEvent struct {
	// ID 是事件主键，列表按它倒序，保证同一秒内的事件顺序稳定。
	ID int64
	// UserID 是账号所有者，写入时由 cookies 表推导，查询必须按它隔离。
	UserID int64
	// CookieID 是触发事件的闲鱼账号标识。
	CookieID string
	// OrderID 与 ItemID 是事件关联的订单和商品，可为空。
	OrderID string
	ItemID  string
	// RuleID 与 RunID 是关联的自动化规则和运行，未创建运行的跳过事件为零。
	RuleID int64
	RunID  int64
	// TriggerType 是触发类型，例如 order_paid。
	TriggerType string
	// Source 是事件来源：ws、scheduler、recovery 或 manual。
	Source string
	// EventType 是行为分类，例如 skipped、queued、action_succeeded、run_finished。
	EventType string
	// Status 取 AuditStatus* 常量之一。
	Status string
	// ActionType 是动作类型，运行级事件为空。
	ActionType string
	// Summary 是不含敏感内容的摘要，例如卡密组名称与数量。
	Summary string
	// Reason 是跳过或失败原因，写入前必须已脱敏。
	Reason string
	// CreatedAt 是事件发生时刻（Unix 秒）。
	CreatedAt int64
}

// AutomationAuditQuery 描述审计事件的分页查询条件。
type AutomationAuditQuery struct {
	// UserID 是必填的所有者隔离条件。
	UserID int64
	// CookieID、Status、TriggerType 为空时不过滤对应维度。
	CookieID    string
	Status      string
	TriggerType string
	// RunID 大于零时只返回某次运行的完整时间线。
	RunID int64
	// Search 按订单号子串匹配，LIKE 通配符会被转义。
	Search string
	// Limit 与 Offset 控制分页，Limit 小于等于零时取 50 且最大 200。
	Limit  int
	Offset int
	// Ascending 为 true 时按时间正序返回，用于运行时间线；默认倒序。
	Ascending bool
}

// AutomationAuditPage 是一页审计事件及符合条件的总数。
type AutomationAuditPage struct {
	// Events 是当前页事件。
	Events []AutomationAuditEvent
	// Total 是符合条件的事件总数。
	Total int64
}

// auditEventColumns 是读取审计事件时统一使用的列顺序，必须与 scanAuditEvent 保持一致。
const auditEventColumns = `id,user_id,cookie_id,order_id,item_id,rule_id,run_id,trigger_type,source,event_type,status,action_type,summary,reason,created_at`

// scanAuditEvent 按 auditEventColumns 的顺序读取一行审计事件；scan 是 Rows.Scan 或 Row.Scan。
func scanAuditEvent(scan func(dest ...any) error) (AutomationAuditEvent, error) {
	// event 保存读取到的审计事件。
	var event AutomationAuditEvent
	// scanErr 保存读取行的数据库错误。
	scanErr := scan(&event.ID, &event.UserID, &event.CookieID, &event.OrderID, &event.ItemID, &event.RuleID, &event.RunID,
		&event.TriggerType, &event.Source, &event.EventType, &event.Status, &event.ActionType, &event.Summary, &event.Reason, &event.CreatedAt)
	return event, scanErr
}

// AppendAuditEvent 追加一条审计事件；所有者由 cookies 表按 event.CookieID 推导，账号不存在时返回 ErrNotFound。
// 审计写入只做一条 INSERT...SELECT，调用方遇到错误只能记录日志，不得影响自动化结果。
func (a *AutomationRules) AppendAuditEvent(ctx context.Context, event AutomationAuditEvent) error {
	// result、execErr 保存插入结果；受影响行数为零说明账号不存在。
	result, execErr := a.DB.ExecContext(ctx,
		`INSERT INTO automation_audit_events (user_id,cookie_id,order_id,item_id,rule_id,run_id,trigger_type,source,event_type,status,action_type,summary,reason,created_at)
		 SELECT user_id,id,?,?,?,?,?,?,?,?,?,?,?,? FROM cookies WHERE id=?`,
		event.OrderID, event.ItemID, event.RuleID, event.RunID, event.TriggerType, event.Source, event.EventType,
		event.Status, event.ActionType, event.Summary, event.Reason, event.CreatedAt, event.CookieID)
	if execErr != nil {
		return execErr
	}
	// affected 是实际写入的行数。
	affected, affectedErr := result.RowsAffected()
	if affectedErr != nil {
		return affectedErr
	}
	if affected == 0 {
		return ErrNotFound
	}
	return nil
}

// ListAuditEvents 按 query 分页读取审计事件，所有查询必须带 UserID。
func (a *AutomationRules) ListAuditEvents(ctx context.Context, query AutomationAuditQuery) (AutomationAuditPage, error) {
	// where、args 是动态拼接的过滤条件及其参数，值一律走占位符。
	where, args := auditEventFilter(query)
	// page 保存读取结果。
	var page AutomationAuditPage
	// scanErr 保存统计符合条件事件总数的数据库错误。
	if scanErr := a.DB.QueryRowContext(ctx, `SELECT COUNT(*) FROM automation_audit_events WHERE `+where, args...).Scan(&page.Total); scanErr != nil {
		return AutomationAuditPage{}, scanErr
	}
	// limit 把分页大小限制在 1 到 200，默认 50。
	limit := query.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}
	// offset 是分页偏移，负数按 0 处理。
	offset := max(query.Offset, 0)
	// order 是排序方向，时间线按正序，列表按倒序。
	order := "DESC"
	if query.Ascending {
		order = "ASC"
	}
	// rows、queryErr 保存当前页查询结果。
	rows, queryErr := a.DB.QueryContext(ctx,
		`SELECT `+auditEventColumns+` FROM automation_audit_events WHERE `+where+` ORDER BY id `+order+` LIMIT ? OFFSET ?`,
		append(args, limit, offset)...)
	if queryErr != nil {
		return AutomationAuditPage{}, queryErr
	}
	defer rows.Close()
	for rows.Next() {
		// event、scanErr 保存当前行解析出的事件及解析错误。
		event, scanErr := scanAuditEvent(rows.Scan)
		if scanErr != nil {
			return AutomationAuditPage{}, scanErr
		}
		page.Events = append(page.Events, event)
	}
	return page, rows.Err()
}

// auditEventFilter 把查询条件转换为 WHERE 片段和参数；搜索词中的 LIKE 通配符会被转义。
func auditEventFilter(query AutomationAuditQuery) (string, []any) {
	// clauses 保存过滤片段，首项固定为所有者隔离。
	clauses := []string{"user_id=?"}
	// args 保存与 clauses 一一对应的占位符参数。
	args := []any{query.UserID}
	if query.CookieID != "" {
		clauses = append(clauses, "cookie_id=?")
		args = append(args, query.CookieID)
	}
	if query.Status != "" {
		clauses = append(clauses, "status=?")
		args = append(args, query.Status)
	}
	if query.TriggerType != "" {
		clauses = append(clauses, "trigger_type=?")
		args = append(args, query.TriggerType)
	}
	if query.RunID > 0 {
		clauses = append(clauses, "run_id=?")
		args = append(args, query.RunID)
	}
	// search 是去掉首尾空白后的订单号搜索词，为空则不加条件。
	if search := strings.TrimSpace(query.Search); search != "" {
		// pattern 是转义后的 LIKE 子串模式。
		pattern := "%" + strings.NewReplacer("!", "!!", "%", "!%", "_", "!_").Replace(search) + "%"
		clauses = append(clauses, `order_id LIKE ? ESCAPE '!'`)
		args = append(args, pattern)
	}
	return strings.Join(clauses, " AND "), args
}

// PruneAuditEvents 删除创建时间早于 before（Unix 秒）的审计事件并返回删除行数，用于保留期清理。
func (a *AutomationRules) PruneAuditEvents(ctx context.Context, before int64) (int64, error) {
	// result、execErr 保存删除结果。
	result, execErr := a.DB.ExecContext(ctx, `DELETE FROM automation_audit_events WHERE created_at<?`, before)
	if execErr != nil {
		return 0, execErr
	}
	return result.RowsAffected()
}
