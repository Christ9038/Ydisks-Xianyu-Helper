package automation

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

const defaultReviewRequestScanInterval = 10 * time.Minute

// Scheduler 执行计划任务类自动化。
// 计划任务只负责“发现应该触发的任务”，具体动作仍交给 Center，避免形成第二套执行链。
type Scheduler struct {
	center   *Center
	interval time.Duration
}

// NewScheduler 构造计划任务调度器。
func NewScheduler(center *Center) *Scheduler {
	return &Scheduler{center: center, interval: defaultReviewRequestScanInterval}
}

// Run 周期扫描计划任务。调用方应在 goroutine 中启动，并用 ctx 控制生命周期。
func (s *Scheduler) Run(ctx context.Context) {
	if s == nil || s.center == nil || s.center.store == nil {
		return
	}
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
}

func (s *Scheduler) scan(ctx context.Context) {
	s.runDeferredTasks(ctx)
	s.runRecoveryTasks(ctx)
	// 逐页执行，避免把所有到期订单一次性装入内存。稳定 ID 游标确保本轮有界。
	afterOrderID := ""
	for {
		orders, err := s.center.store.Automation.DueReviewRequestOrdersAfter(ctx, afterOrderID, 200)
		if err != nil {
			s.center.logger.Warn("扫描求评价计划任务失败", "err", err)
			return
		}
		for _, order := range orders {
			allowed, allowErr := s.center.accountAutomationAllowed(ctx, order.CookieID)
			if allowErr != nil {
				s.center.logger.Warn("检查求评价账号状态失败", "account", order.CookieID, "err", allowErr)
				continue
			}
			if !allowed {
				continue
			}
			rules, err := s.center.store.Automation.Match(ctx, order.CookieID, order.ItemID, TriggerReviewMissingTimeout)
			if err != nil || len(rules) == 0 {
				continue
			}
			for _, rule := range rules {
				if !reviewRequestRuleDue(order, rule) {
					continue
				}
				task := Task{Source: "scheduler", AccountID: order.CookieID, TriggerType: TriggerReviewMissingTimeout,
					ChatID: order.ChatID, OrderID: order.OrderID, ItemID: order.ItemID, BuyerID: order.BuyerID,
					Text: "发货后一段时间未评价", Raw: map[string]any{"source": "scheduler", "rule_id": rule.ID,
						"order_id": order.OrderID, "attempt": order.ReviewRequestCount + 1}}
				if err := s.center.executeRule(ctx, task, rule); err != nil {
					s.center.logger.Warn("求评价计划任务执行失败", "account", order.CookieID, "order_id", order.OrderID, "rule_id", rule.ID, "err", err)
				}
			}
		}
		if len(orders) < 200 {
			break
		}
		afterOrderID = orders[len(orders)-1].OrderID
	}
}

func (s *Scheduler) runRecoveryTasks(ctx context.Context) {
	runs, err := s.center.store.Automation.DueRecoveryRuns(ctx, 100)
	if err != nil {
		s.center.logger.Warn("扫描失败自动化运行失败", "err", err)
		return
	}
	for _, run := range runs {
		if run.ActionStarted {
			reason := "进程在外部动作执行期间中断，发送结果未知，已禁止自动重放"
			_ = s.center.store.Automation.QuarantineRun(ctx, run.ID, run.AttemptCount, reason)
			s.center.notifyRunNeedsReview(run, reason)
			continue
		}
		var task Task
		if err := json.Unmarshal([]byte(run.RawEventJSON), &task); err != nil || task.AccountID == "" {
			reason := "历史运行数据无法安全解析，已移入人工检查"
			_ = s.center.store.Automation.QuarantineRun(ctx, run.ID, run.AttemptCount, reason)
			s.center.notifyRunNeedsReview(run, reason)
			continue
		}
		allowed, err := s.center.accountAutomationAllowed(ctx, task.AccountID)
		if err != nil || !allowed {
			_ = s.center.store.Automation.PostponeRecoveryRun(ctx, run.ID, time.Now().UTC().Add(10*time.Minute).Unix())
			continue
		}
		rule, err := s.center.store.Automation.Get(ctx, run.RuleID)
		if err != nil || !rule.Enabled {
			reason := "自动化规则不存在或已停用，无法恢复"
			_ = s.center.store.Automation.QuarantineRun(ctx, run.ID, run.AttemptCount, reason)
			s.center.notifyRunNeedsReview(run, reason)
			continue
		}
		claimed, claimErr := s.center.store.Automation.ClaimRecoveryRun(ctx, run.ID, time.Now().UTC().Add(5*time.Minute).Unix())
		if claimErr != nil || !claimed {
			continue
		}
		if task.Raw == nil {
			task.Raw = map[string]any{}
		}
		task.Raw["automation_run_id"] = run.ID
		task.Raw["automation_rule_id"] = run.RuleID
		if err := s.center.executeRule(ctx, task, *rule); err != nil && !errors.Is(err, errAutomationDeferred) {
			s.center.logger.Warn("重试自动化运行失败", "run_id", run.ID, "err", err)
		}
	}
}

func (s *Scheduler) runDeferredTasks(ctx context.Context) {
	tasks, err := s.center.store.Automation.ClaimDueDeferredTasks(ctx, 100)
	if err != nil {
		s.center.logger.Warn("扫描暂停期间自动化事件失败", "err", err)
		return
	}
	for _, pending := range tasks {
		var task Task
		if err := json.Unmarshal([]byte(pending.TaskJSON), &task); err != nil {
			_ = s.center.store.Automation.FinishDeferredTask(ctx, pending.ID, pending.ClaimVersion, false, "解析任务失败: "+err.Error())
			continue
		}
		deferredAgain, runErr := s.center.handleTask(ctx, task)
		if deferredAgain {
			// handleTask 已按新的 paused_until 重置同一任务；当前 claim 不再删除。
			continue
		}
		if err := s.center.store.Automation.FinishDeferredTask(ctx, pending.ID, pending.ClaimVersion, runErr == nil, errorString(runErr)); err != nil {
			s.center.logger.Warn("保存暂停事件重放结果失败", "task_id", pending.ID, "err", err)
		}
	}
}

func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func reviewRequestRuleDue(order db.Order, rule db.AutomationRule) bool {
	cfg := parseReviewRuleConfig(rule.ConfigJSON)
	if cfg.MaxAttempts > 0 && order.ReviewRequestCount >= cfg.MaxAttempts {
		return false
	}
	baseRaw := firstNonEmpty(order.ShippedAt, order.UpdatedAt, order.CreatedAt)
	waitHours := cfg.AfterShippedHours
	if order.ReviewRequestCount > 0 && strings.TrimSpace(order.LastReviewRequestAt) != "" {
		baseRaw = order.LastReviewRequestAt
		waitHours = cfg.RepeatIntervalHours
	}
	base := parseDBTime(baseRaw)
	if base.IsZero() {
		return false
	}
	return time.Since(base) >= time.Duration(waitHours)*time.Hour
}

type reviewRuleConfig struct {
	AfterShippedHours   int
	RepeatIntervalHours int
	MaxAttempts         int
}

func parseReviewRuleConfig(raw string) reviewRuleConfig {
	cfg := reviewRuleConfig{AfterShippedHours: 72, RepeatIntervalHours: 24, MaxAttempts: 1}
	if strings.TrimSpace(raw) == "" {
		return cfg
	}
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return cfg
	}
	if v := intFromAny(m["after_shipped_hours"]); v > 0 {
		cfg.AfterShippedHours = v
	}
	if v := intFromAny(m["first_delay_hours"]); v > 0 {
		cfg.AfterShippedHours = v
	}
	if v := intFromAny(m["repeat_interval_hours"]); v > 0 {
		cfg.RepeatIntervalHours = v
	}
	if v := intFromAny(m["max_attempts"]); v > 0 {
		cfg.MaxAttempts = v
	}
	return cfg
}

func intFromAny(v any) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

func parseDBTime(s string) time.Time {
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00", // Postgres TEXT(CURRENT_TIMESTAMP)
		"2006-01-02 15:04:05.999999999Z07",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05Z07",
		"2006-01-02 15:04:05", // SQLite/MySQL 历史值；按既有 UTC 约定解释
	} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}
