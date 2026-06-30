package automation

import (
	"context"
	"encoding/json"
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
	orders, err := s.center.store.Automation.DueReviewRequestOrders(ctx, 200)
	if err != nil {
		s.center.logger.Warn("扫描求评价计划任务失败", "err", err)
		return
	}
	for _, order := range orders {
		rules, err := s.center.store.Automation.Match(ctx, order.CookieID, order.ItemID, TriggerReviewMissingTimeout)
		if err != nil || len(rules) == 0 {
			continue
		}
		for _, rule := range rules {
			if !reviewRequestRuleDue(order, rule) {
				continue
			}
			task := Task{
				Source:      "scheduler",
				AccountID:   order.CookieID,
				TriggerType: TriggerReviewMissingTimeout,
				ChatID:      order.ChatID,
				OrderID:     order.OrderID,
				ItemID:      order.ItemID,
				BuyerID:     order.BuyerID,
				Text:        "发货后一段时间未评价",
				Raw: map[string]any{
					"source":   "scheduler",
					"rule_id":  rule.ID,
					"order_id": order.OrderID,
					"attempt":  order.ReviewRequestCount + 1,
				},
			}
			_ = s.center.executeRule(ctx, task, rule)
		}
	}
}

func reviewRequestRuleDue(order db.Order, rule db.AutomationRule) bool {
	cfg := parseReviewRuleConfig(rule.ConfigJSON)
	if cfg.MaxAttempts > 0 && order.ReviewRequestCount >= cfg.MaxAttempts {
		return false
	}
	base := parseDBTime(firstNonEmpty(order.ShippedAt, order.UpdatedAt, order.CreatedAt))
	if base.IsZero() {
		return false
	}
	return time.Since(base) >= time.Duration(cfg.AfterShippedHours)*time.Hour
}

type reviewRuleConfig struct {
	AfterShippedHours int
	MaxAttempts       int
}

func parseReviewRuleConfig(raw string) reviewRuleConfig {
	cfg := reviewRuleConfig{AfterShippedHours: 72, MaxAttempts: 1}
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
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.Local); err == nil {
			return t
		}
	}
	return time.Time{}
}
