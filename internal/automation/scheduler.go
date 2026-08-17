package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
)

// defaultReviewRequestScanInterval 保存defaultReview请求ScanInterval，供当前处理流程使用
const defaultReviewRequestScanInterval = time.Minute

// Scheduler 执行计划任务类自动化。
// 计划任务只负责“发现应该触发的任务”，具体动作仍交给 Center，避免形成第二套执行链。
// Scheduler 保存Scheduler，供当前处理流程使用
type Scheduler struct {
	center   *Center
	interval time.Duration
	runOnce  sync.Once
	done     chan struct{}
}

// NewScheduler 构造计划任务调度器。
func NewScheduler(center *Center) *Scheduler {
	return &Scheduler{center: center, interval: defaultReviewRequestScanInterval, done: make(chan struct{})}
}

// Run 周期扫描计划任务。调用方应在 goroutine 中启动，并用 ctx 控制生命周期。
func (s *Scheduler) Run(ctx context.Context) {
	if s == nil || s.center == nil || s.center.store == nil {
		return
	}
	s.runOnce.Do(func() {
		defer close(s.done)
		if ctx.Err() != nil {
			return
		}
		// ticker 保存ticker，供当前处理流程使用
		ticker := time.NewTicker(s.interval)
		defer ticker.Stop()
		s.scan(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				s.scan(ctx)
			}
		}
	})
}

// Wait 等待调度器完成，并兼容不需要超时的旧调用方。
func (s *Scheduler) Wait() {
	_ = s.WaitContext(context.Background())
}

// WaitContext 在 ctx 约束内等待调度器完成，避免关闭流程无限阻塞。
func (s *Scheduler) WaitContext(ctx context.Context) error {
	if s != nil && s.done != nil {
		if ctx == nil {
			ctx = context.Background()
		}
		select {
		case <-s.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// scan 负责scan相关处理。
func (s *Scheduler) scan(ctx context.Context) {
	// deferredErr 汇总延迟任务状态收口失败，供本轮扫描结束时统一告警。
	deferredErr := s.runDeferredTasks(ctx)
	s.center.scanAccountTasks(ctx)
	if // recovered、err 保存recovered、err，供当前处理流程使用
	recovered, err := s.center.store.Automation.RecoverDefinitelyUnsentReviewRuns(ctx); err != nil {
		s.center.logger.Warn("恢复历史求评价未发送任务失败", "err", err)
	} else if recovered > 0 {
		s.center.logger.Info("已恢复历史求评价未发送任务，等待安全重试", "count", recovered)
	}
	// recoveryErr 汇总恢复运行状态收口失败，避免数据库写错误只记录日志后丢失。
	recoveryErr := s.runRecoveryTasks(ctx)
	if // scanErr 汇总两个自动化扫描阶段的状态收口错误，供统一告警使用。
	scanErr := errors.Join(deferredErr, recoveryErr); scanErr != nil {
		s.center.logger.Error("自动化计划任务状态收口失败", "err", scanErr)
	}
	// 逐页执行，避免把所有到期订单一次性装入内存。稳定 ID 游标确保本轮有界。
	afterOrderID := ""
	// waitingForWS 保存waitingForWS，供当前处理流程使用
	waitingForWS := map[string]int{}
	for {
		// orders、err 保存orders、err，供当前处理流程使用
		orders, err := s.center.store.Automation.DueReviewRequestOrdersAfter(ctx, afterOrderID, 200)
		if err != nil {
			s.center.logger.Warn("扫描求评价计划任务失败", "err", err)
			return
		}
		// order 表示当前遍历过程中的订单
		for _, order := range orders {
			// allowed、allowErr 保存allowed、allowErr，供当前处理流程使用
			allowed, allowErr := s.center.accountAutomationAllowed(ctx, order.CookieID)
			if allowErr != nil {
				s.center.logger.Warn("检查求评价账号状态失败", "account", order.CookieID, "err", allowErr)
				continue
			}
			if !allowed {
				continue
			}
			if !s.center.accountSenderReady(order.CookieID) {
				waitingForWS[order.CookieID]++
				continue
			}
			// rules、err 保存rules、err，供当前处理流程使用
			rules, err := s.center.rules.match(ctx, Task{AccountID: order.CookieID, ItemID: order.ItemID, TriggerType: TriggerReviewMissingTimeout})
			if err != nil {
				s.center.logger.Warn("查询求评价自动化规则失败", "account", order.CookieID, "order_id", order.OrderID, "item_id", order.ItemID, "err", err)
				continue
			}
			if len(rules) == 0 {
				continue
			}
			// rule 表示当前遍历过程中的规则
			for _, rule := range rules {
				if !reviewRequestRuleDue(order, rule) {
					continue
				}
				// task 保存任务，供当前处理流程使用
				task := Task{Source: "scheduler", AccountID: order.CookieID, TriggerType: TriggerReviewMissingTimeout,
					ChatID: order.ChatID, OrderID: order.OrderID, ItemID: order.ItemID, BuyerID: order.BuyerID,
					Text: "发货后一段时间未评价", Raw: map[string]any{"source": "scheduler", "rule_id": rule.ID,
						"order_id": order.OrderID, "attempt": order.ReviewRequestCount + 1}}
				if // err 保存err，供当前处理流程使用
				err := s.center.executeRule(ctx, task, rule); err != nil {
					s.center.logger.Warn("求评价计划任务执行失败", "account", order.CookieID, "order_id", order.OrderID, "rule_id", rule.ID, "err", err)
				}
			}
		}
		if len(orders) < 200 {
			break
		}
		afterOrderID = orders[len(orders)-1].OrderID
	}
	// accountID、count 表示当前遍历过程中的账号ID、count
	for accountID, count := range waitingForWS {
		s.center.logger.Info("账号 WebSocket 尚未就绪，求评价任务等待下次扫描", "account", accountID, "orders", count)
	}
}

// runRecoveryTasks 负责运行Recovery任务列表相关处理。
func (s *Scheduler) runRecoveryTasks(ctx context.Context) error {
	// resultErr 汇总本轮恢复任务的持久化错误，调用方可据此触发统一告警。
	var resultErr error
	// runs、err 保存runs、err，供当前处理流程使用
	runs, err := s.center.store.Automation.DueRecoveryRuns(ctx, 100)
	if err != nil {
		s.center.logger.Warn("扫描失败自动化运行失败", "err", err)
		return err
	}
	// run 表示当前遍历过程中的运行
	for _, run := range runs {
		if run.ActionStarted {
			// reason 保存原因，供当前处理流程使用
			reason := "进程在外部动作执行期间中断，发送结果未知，已禁止自动重放"
			// quarantineErr 表示把外部动作结果未知的运行转为人工核对状态时的错误。
			quarantineErr := s.quarantineRunForReview(ctx, run, reason)
			resultErr = errors.Join(resultErr, quarantineErr)
			continue
		}
		// task 保存任务，供当前处理流程使用
		var task Task
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal([]byte(run.RawEventJSON), &task); err != nil || task.AccountID == "" {
			// reason 保存原因，供当前处理流程使用
			reason := "历史运行数据无法安全解析，已移入人工检查"
			// quarantineErr 表示历史任务无法解析时写入人工核对状态的错误。
			quarantineErr := s.quarantineRunForReview(ctx, run, reason)
			resultErr = errors.Join(resultErr, quarantineErr)
			continue
		}
		// allowed、err 保存allowed、err，供当前处理流程使用
		allowed, err := s.center.accountAutomationAllowed(ctx, task.AccountID)
		if err != nil || !allowed {
			if // postponeErr 保存postponeErr，供当前处理流程使用
			postponeErr := s.center.store.Automation.PostponeRecoveryRun(ctx, run.ID, run.AttemptCount, time.Now().UTC().Add(10*time.Minute).Unix()); postponeErr != nil {
				s.center.logger.Warn("延期自动化恢复任务失败", "run_id", run.ID, "err", postponeErr)
				resultErr = errors.Join(resultErr, fmt.Errorf("延期自动化恢复任务失败: %w", postponeErr))
			}
			continue
		}
		// rule、err 保存rule、err，供当前处理流程使用
		rule, err := s.center.store.Automation.Get(ctx, run.RuleID)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			if ctx.Err() != nil {
				return errors.Join(resultErr, ctx.Err())
			}
			s.center.logger.Warn("读取自动化恢复规则失败，保留任务等待重试", "run_id", run.ID, "rule_id", run.RuleID, "err", err)
			continue
		}
		if errors.Is(err, db.ErrNotFound) || rule == nil || !rule.Enabled {
			// reason 保存原因，供当前处理流程使用
			reason := "自动化规则不存在或已停用，无法恢复"
			// quarantineErr 表示规则不可恢复时写入人工核对状态的错误。
			quarantineErr := s.quarantineRunForReview(ctx, run, reason)
			resultErr = errors.Join(resultErr, quarantineErr)
			continue
		}
		if recoveryNeedsSender(task, *rule, run.ActionCursor) && !s.center.accountSenderReady(task.AccountID) {
			if // postponeErr 保存postponeErr，供当前处理流程使用
			postponeErr := s.center.store.Automation.PostponeRecoveryRun(ctx, run.ID, run.AttemptCount, time.Now().UTC().Add(defaultReviewRequestScanInterval).Unix()); postponeErr != nil {
				s.center.logger.Warn("等待 WebSocket 时延期自动化任务失败", "run_id", run.ID, "err", postponeErr)
				resultErr = errors.Join(resultErr, fmt.Errorf("等待 WebSocket 时延期自动化任务失败: %w", postponeErr))
			}
			continue
		}
		// claimed、claimErr 保存claimed、claimErr，供当前处理流程使用
		claimed, claimErr := s.center.store.Automation.ClaimRecoveryRun(ctx, run.ID, time.Now().UTC().Add(5*time.Minute).Unix())
		if claimErr != nil {
			// claimFailure 表示领取恢复运行的状态写入失败，必须返回而不能被当作并发未领取。
			claimFailure := fmt.Errorf("领取自动化恢复任务失败: %w", claimErr)
			resultErr = errors.Join(resultErr, claimFailure)
		}
		if claimErr != nil || !claimed {
			continue
		}
		if task.Raw == nil {
			task.Raw = map[string]any{}
		}
		task.Raw["automation_run_id"] = run.ID
		task.Raw["automation_rule_id"] = run.RuleID
		if // err 保存err，供当前处理流程使用
		err := s.center.executeRule(ctx, task, *rule); err != nil && !errors.Is(err, errAutomationDeferred) {
			s.center.logger.Warn("重试自动化运行失败", "run_id", run.ID, "err", err)
			resultErr = errors.Join(resultErr, err)
		}
	}
	return resultErr
}

// quarantineRunForReview 将恢复运行置为人工核对并发送运维通知；写入失败时返回统一 needs_review 错误，禁止调用方误认为状态已收口。
func (s *Scheduler) quarantineRunForReview(ctx context.Context, run db.AutomationRun, reason string) error {
	// quarantineErr 表示人工核对状态写入失败；失败时数据库中的原状态仍可能允许下一轮恢复。
	quarantineErr := s.center.store.Automation.QuarantineRun(ctx, run.ID, run.AttemptCount, reason)
	s.center.notifyRunNeedsReview(ctx, run, reason)
	if quarantineErr == nil {
		return nil
	}
	s.center.logger.Error("保存自动化恢复运行人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
	return errors.Join(
		errAutomationNeedsReview,
		errAutomationQuarantine,
		fmt.Errorf("保存自动化恢复运行人工核对状态失败: %w", quarantineErr),
	)
}

// recoveryNeedsSender 负责recoveryNeedsSender相关处理。
func recoveryNeedsSender(task Task, rule db.AutomationRule, cursor int) bool {
	// actions 保存动作列表，供当前处理流程使用
	actions := task.ActionPlan
	if len(actions) == 0 {
		actions = (actionPlanner{}).plan(task, rule.Actions)
	}
	if cursor < 0 || cursor >= len(actions) {
		return false
	}
	switch actions[cursor].ActionType {
	case ActionSendText, ActionSendCard:
		return true
	default:
		return false
	}
}

// runDeferredTasks 负责运行Deferred任务列表相关处理。
func (s *Scheduler) runDeferredTasks(ctx context.Context) error {
	// resultErr 汇总延迟任务最终状态写入失败，避免领取成功后状态异常被静默吞掉。
	var resultErr error
	// tasks、err 保存tasks、err，供当前处理流程使用
	tasks, err := s.center.store.Automation.ClaimDueDeferredTasks(ctx, 100)
	if err != nil {
		s.center.logger.Warn("扫描暂停期间自动化事件失败", "err", err)
		return err
	}
	// pending 表示当前遍历过程中的pending
	for _, pending := range tasks {
		// task 保存任务，供当前处理流程使用
		var task Task
		if // err 保存err，供当前处理流程使用
		err := json.Unmarshal([]byte(pending.TaskJSON), &task); err != nil {
			// finishErr 表示解析失败后写入延迟任务重试或死信状态时的错误。
			finishErr := s.center.store.Automation.FinishDeferredTask(ctx, pending.ID, pending.ClaimVersion, false, "解析任务失败: "+err.Error())
			if finishErr != nil {
				s.center.logger.Error("保存解析失败的暂停事件状态失败", "task_id", pending.ID, "err", finishErr)
				resultErr = errors.Join(
					resultErr,
					errAutomationNeedsReview,
					fmt.Errorf("保存解析失败的暂停事件状态失败: %w", finishErr),
				)
			}
			continue
		}
		if task.Raw == nil {
			task.Raw = map[string]any{}
		}
		task.Raw["automation_deferred_replay"] = true
		// deferredAgain、runErr 保存deferredAgain、runErr，供当前处理流程使用
		deferredAgain, runErr := s.center.handleTask(ctx, task)
		if deferredAgain {
			// handleTask 已按新的 paused_until 重置同一任务；当前 claim 不再删除。
			continue
		}
		if // err 保存err，供当前处理流程使用
		err := s.center.store.Automation.FinishDeferredTask(ctx, pending.ID, pending.ClaimVersion, runErr == nil, errorString(runErr)); err != nil {
			s.center.logger.Warn("保存暂停事件重放结果失败", "task_id", pending.ID, "err", err)
			resultErr = errors.Join(resultErr, errAutomationNeedsReview, runErr, fmt.Errorf("保存暂停事件重放结果失败: %w", err))
		}
	}
	return resultErr
}

// errorString 负责错误String相关处理。
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// reviewRequestRuleDue 负责review请求规则Due相关处理。
func reviewRequestRuleDue(order db.Order, rule db.AutomationRule) bool {
	// cfg 保存cfg，供当前处理流程使用
	cfg := parseReviewRuleConfig(rule.ConfigJSON)
	if cfg.MaxAttempts > 0 && order.ReviewRequestCount >= cfg.MaxAttempts {
		return false
	}
	// baseRaw 保存base原始，供当前处理流程使用
	baseRaw := firstNonEmpty(order.ShippedAt, order.UpdatedAt, order.CreatedAt)
	// waitHours 保存waitHours，供当前处理流程使用
	waitHours := cfg.AfterShippedHours
	if order.ReviewRequestCount > 0 && strings.TrimSpace(order.LastReviewRequestAt) != "" {
		baseRaw = order.LastReviewRequestAt
		waitHours = cfg.RepeatIntervalHours
	}
	// base 保存base，供当前处理流程使用
	base := parseDBTime(baseRaw)
	if base.IsZero() {
		return false
	}
	return time.Since(base) >= time.Duration(waitHours)*time.Hour
}

// reviewRuleConfig 保存review规则配置，供当前处理流程使用
type reviewRuleConfig struct {
	AfterShippedHours   int
	RepeatIntervalHours int
	MaxAttempts         int
}

// parseReviewRuleConfig 负责parseReview规则配置相关处理。
func parseReviewRuleConfig(raw string) reviewRuleConfig {
	// cfg 保存cfg，供当前处理流程使用
	cfg := reviewRuleConfig{AfterShippedHours: 72, RepeatIntervalHours: 24, MaxAttempts: 1}
	if strings.TrimSpace(raw) == "" {
		return cfg
	}
	// m 保存m，供当前处理流程使用
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return cfg
	}
	if // v 保存v，供当前处理流程使用
	v := intFromAny(m["after_shipped_hours"]); v > 0 {
		cfg.AfterShippedHours = v
	}
	if // v 保存v，供当前处理流程使用
	v := intFromAny(m["first_delay_hours"]); v > 0 {
		cfg.AfterShippedHours = v
	}
	if // v 保存v，供当前处理流程使用
	v := intFromAny(m["repeat_interval_hours"]); v > 0 {
		cfg.RepeatIntervalHours = v
	}
	if // v 保存v，供当前处理流程使用
	v := intFromAny(m["max_attempts"]); v > 0 {
		cfg.MaxAttempts = v
	}
	return cfg
}

// intFromAny 负责intFromAny相关处理。
func intFromAny(v any) int {
	switch // x 保存x，供当前处理流程使用
	x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		// n 保存n，供当前处理流程使用
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

// parseDBTime 负责parseDB时间相关处理。
func parseDBTime(s string) time.Time {
	// layout 表示当前遍历过程中的layout
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00", // Postgres TEXT(CURRENT_TIMESTAMP)
		"2006-01-02 15:04:05.999999999Z07",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05Z07",
		"2006-01-02 15:04:05", // SQLite/MySQL 历史值；按既有 UTC 约定解释
	} {
		if // t、err 保存t、err，供当前处理流程使用
		t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}
