package automation

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

const (
	// auditEventSkipped 表示事件被门禁拦截，没有执行任何外部动作。
	auditEventSkipped = "skipped"
	// auditEventQueued 表示事件或动作已持久化排队，等待延迟到期或账号恢复。
	auditEventQueued = "queued"
	// auditEventRunStarted 表示自动化运行取得唯一执行权并开始执行。
	auditEventRunStarted = "run_started"
	// auditEventActionSucceeded 表示一个动作已确认完成并推进了检查点。
	auditEventActionSucceeded = "action_succeeded"
	// auditEventActionFailed 表示一个动作失败或结果不确定。
	auditEventActionFailed = "action_failed"
	// auditEventRunFinished 表示运行进入终态（成功、失败、人工核对或取消）。
	auditEventRunFinished = "run_finished"
	// auditEventDeadLetter 表示延期任务耗尽自动重试或无法安全收口，已转人工处理。
	auditEventDeadLetter = "dead_letter"
	// auditEventBargainFreeShip 表示二人小刀免拼阶段的执行结果。
	auditEventBargainFreeShip = "bargain_free_ship"

	// auditReasonMaxRunes 限制单条审计原因的字符数，避免异常错误文本撑大审计表。
	auditReasonMaxRunes = 500
	// auditSummaryMaxRunes 限制单条审计摘要的字符数，摘要只应是卡密组名称等简短描述。
	auditSummaryMaxRunes = 200

	// auditRetention 是审计事件的保留时长，超过后由调度器清理。
	auditRetention = 90 * 24 * time.Hour
	// auditPruneInterval 是两次保留期清理之间的最短间隔，清理只访问本地数据库。
	auditPruneInterval = 24 * time.Hour
)

// auditEvent 描述一次待记录的自动化行为；字段均不得包含卡密正文、消息正文或任何凭证。
type auditEvent struct {
	// eventType 取 auditEvent* 常量之一。
	eventType string
	// status 取 db.AuditStatus* 常量之一。
	status string
	// ruleID 与 runID 是关联的规则和运行，未创建运行的跳过事件为零。
	ruleID int64
	runID  int64
	// actionType 是动作类型，运行级事件为空。
	actionType string
	// summary 是不含敏感内容的摘要，例如卡密组名称与数量。
	summary string
	// reason 是跳过或失败原因。
	reason string
}

// auditRecorder 把自动化行为追加到审计表。它只读写自己持有的 store，无后台 goroutine、无可变共享状态，
// 因而可以按值复制并被并发调用；零值（store 为空）是安全的空操作。
// 审计是旁路记录：写入失败只输出 Warn 日志，绝不影响自动化结果，也绝不阻止外部动作。
type auditRecorder struct {
	// store 提供审计事件持久化；为空时不记录。
	store *db.Store
	// logger 输出审计写入失败告警。
	logger *slog.Logger
}

// newAuditRecorder 构造审计记录器；store 或 logger 可以为空。
func newAuditRecorder(store *db.Store, logger *slog.Logger) auditRecorder {
	return auditRecorder{store: store, logger: logger}
}

// record 追加一条审计事件。task 提供账号、订单、商品、触发类型和来源；ctx 被取消后仍使用短时独立上下文写入，
// 保证终态与取消路径同样留痕。
func (r auditRecorder) record(ctx context.Context, task Task, event auditEvent) {
	if r.store == nil || r.store.Automation == nil || task.AccountID == "" {
		return
	}
	// writeCtx 保证原始请求取消后审计仍有独立的短时写入预算。
	writeCtx, cancel := newAutomationRunCompensationContext(ctx)
	defer cancel()
	// source 是事件来源；延迟或恢复重放统一标记为 recovery，便于区分首次 WS 触发。
	source := task.Source
	if isDeferredReplay(task) {
		source = "recovery"
	}
	// appendErr 保存审计写入错误，只记录不上抛。
	appendErr := r.store.Automation.AppendAuditEvent(writeCtx, db.AutomationAuditEvent{
		CookieID: task.AccountID, OrderID: task.OrderID, ItemID: task.ItemID,
		RuleID: event.ruleID, RunID: event.runID, TriggerType: task.TriggerType, Source: source,
		EventType: event.eventType, Status: event.status, ActionType: event.actionType,
		Summary: truncateAuditText(event.summary, auditSummaryMaxRunes),
		Reason:  truncateAuditText(event.reason, auditReasonMaxRunes),
		// CreatedAt 使用 UTC 秒，与其他自动化表保持一致。
		CreatedAt: time.Now().UTC().Unix(),
	})
	if appendErr != nil && r.logger != nil {
		r.logger.WarnContext(ctx, "写入自动化审计事件失败", "account", task.AccountID, "order_id", task.OrderID, "event", event.eventType, "err", appendErr)
	}
}

// skipped 记录一次被门禁拦截、未执行任何外部动作的事件。
func (r auditRecorder) skipped(ctx context.Context, task Task, reason string) {
	r.record(ctx, task, auditEvent{eventType: auditEventSkipped, status: db.AuditStatusSkipped, reason: reason})
}

// queued 记录一次已持久化排队的事件；reason 说明排队原因，例如账号暂停或延迟动作。
func (r auditRecorder) queued(ctx context.Context, task Task, runID, ruleID int64, reason string) {
	r.record(ctx, task, auditEvent{eventType: auditEventQueued, status: db.AuditStatusQueued, runID: runID, ruleID: ruleID, reason: reason})
}

// runFinished 记录运行进入终态；status 取 success、failed、needs_review 或 canceled，sent 是已确认完成的动作数，
// reason 是失败、取消或转人工核对的原因，成功时为空。
func (r auditRecorder) runFinished(ctx context.Context, task Task, ruleID, runID int64, status string, sent int, reason string) {
	r.record(ctx, task, auditEvent{
		eventType: auditEventRunFinished, status: status, ruleID: ruleID, runID: runID,
		summary: fmt.Sprintf("运行结束，已完成 %d 个动作", sent), reason: reason,
	})
}

// truncateAuditText 按字符数截断审计文本并去除首尾空白，超长时以省略号结尾。
func truncateAuditText(text string, maxRunes int) string {
	// trimmed 是去掉首尾空白后的文本。
	trimmed := strings.TrimSpace(text)
	// runes 用于按字符而不是字节截断中文。
	runes := []rune(trimmed)
	if len(runes) <= maxRunes {
		return trimmed
	}
	return string(runes[:maxRunes]) + "…"
}

// summarizeAction 生成动作的非敏感摘要：只使用卡密组名称、数量、模板名称和消息条数，
// 绝不读取已发送的卡密或消息正文。result 是动作执行结果，失败时传零值。
func summarizeAction(action db.AutomationAction, result actionExecutionResult) string {
	switch action.ActionType {
	case ActionSendCard:
		// count 优先取动作实际确认的发送数量，未知时回退到规则配置的发货数量。
		count := result.sent
		if count <= 0 {
			count = action.DeliveryCount
		}
		return fmt.Sprintf("发送卡密「%s」×%d", action.CardName, max(count, 1))
	case ActionSendTemplate:
		return fmt.Sprintf("发送发货模板「%s」（%d 条消息）", action.DeliveryTemplateName, len(action.TemplateMessages))
	case ActionSendText:
		return "发送文本消息"
	case ActionConfirmShipment:
		return "确认发货"
	case ActionAdjustPrice:
		return "修改订单价格"
	default:
		return action.ActionType
	}
}

// pruneAuditEvents 在距上次清理满 auditPruneInterval 后删除超过保留期的审计事件。
// 它只在调度循环 goroutine 内被 scan 调用，不触碰平台接口；仅在确实删除了记录时输出一条 Info 摘要。
func (s *Scheduler) pruneAuditEvents(ctx context.Context) {
	// now 是本次清理判断使用的当前 UTC 时间。
	now := time.Now().UTC()
	// last 是上次清理的 Unix 秒，零值表示尚未清理过。
	last := s.auditPrunedAt.Load()
	if last != 0 && now.Unix()-last < int64(auditPruneInterval/time.Second) {
		return
	}
	s.auditPrunedAt.Store(now.Unix())
	// removed、pruneErr 保存本次删除的记录数及错误。
	removed, pruneErr := s.center.store.Automation.PruneAuditEvents(ctx, now.Add(-auditRetention).Unix())
	if pruneErr != nil {
		s.center.logger.Warn("清理过期自动化审计事件失败", "err", pruneErr)
		return
	}
	if removed > 0 {
		s.center.logger.Info("已清理过期自动化审计事件", "count", removed)
	}
}
