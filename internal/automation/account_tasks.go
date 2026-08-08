package automation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

const (
	TaskAutoRate       = "auto_rate"
	TaskAutoPolish     = "auto_polish"
	polishItemPageSize = 20
	polishItemMaxPages = 20
)

type AccountTaskClient interface {
	FetchPendingRateOrders(ctx context.Context, cookiesStr string, page, pageSize int) (*mtop.PendingRateResult, error)
	RateBuyer(ctx context.Context, cookiesStr, tradeID, feedback string) (*mtop.AccountTaskResult, error)
	FetchAllItems(ctx context.Context, cookiesStr string, pageSize, maxPages int) (*mtop.ItemListResult, error)
	PolishItem(ctx context.Context, cookiesStr, itemID string) (*mtop.AccountTaskResult, error)
}

type AccountTaskSummary struct {
	TaskType string `json:"task_type"`
	Found    int    `json:"found"`
	Success  int    `json:"success"`
	Failed   int    `json:"failed"`
	Skipped  int    `json:"skipped"`
	Message  string `json:"message,omitempty"`
}

func (c *Center) RunAccountTask(ctx context.Context, accountID, taskType string) (AccountTaskSummary, error) {
	allowed, err := c.accountAutomationAllowed(ctx, accountID)
	if err != nil {
		return AccountTaskSummary{TaskType: taskType}, err
	}
	if !allowed {
		return AccountTaskSummary{TaskType: taskType}, fmt.Errorf("账号已停用或暂停，无法执行任务")
	}
	settings, err := c.store.AccountTasks.Get(ctx, accountID)
	if err != nil {
		return AccountTaskSummary{TaskType: taskType}, err
	}
	switch taskType {
	case TaskAutoRate:
		return c.runAutoRate(ctx, settings)
	case TaskAutoPolish:
		return c.runAutoPolish(ctx, settings, beijingNow(), true)
	default:
		return AccountTaskSummary{TaskType: taskType}, fmt.Errorf("不支持的账号任务: %s", taskType)
	}
}

func (c *Center) scanAccountTasks(ctx context.Context) {
	if c.accountTasks == nil || c.store.AccountTasks == nil {
		return
	}
	settings, err := c.store.AccountTasks.Enabled(ctx)
	if err != nil {
		c.logger.Warn("扫描账号任务配置失败", "err", err)
		return
	}
	now := beijingNow()
	for _, setting := range settings {
		allowed, err := c.accountAutomationAllowed(ctx, setting.CookieID)
		if err != nil || !allowed {
			continue
		}
		if setting.AutoRateEnabled {
			if _, err := c.runAutoRate(ctx, setting); err != nil {
				c.logger.Warn("自动评价扫描失败", "account", setting.CookieID, "err", err)
			}
		}
		if setting.AutoPolishEnabled && polishDue(setting, now) {
			if _, err := c.runAutoPolish(ctx, setting, now, false); err != nil {
				c.logger.Warn("每日擦亮失败", "account", setting.CookieID, "err", err)
			}
		}
	}
}

func (c *Center) runAutoRate(ctx context.Context, settings db.AccountTaskSettings) (AccountTaskSummary, error) {
	summary := AccountTaskSummary{TaskType: TaskAutoRate}
	if c.accountTasks == nil {
		return summary, fmt.Errorf("自动评价客户端未初始化")
	}
	cookies, err := c.store.Cookies.GetValue(ctx, settings.CookieID)
	if err != nil {
		return summary, err
	}
	current := cookies
	var orders []mtop.PendingRateOrder
	for page := 1; page <= 20; page++ {
		pending, err := c.accountTasks.FetchPendingRateOrders(ctx, current, page, 50)
		if err != nil {
			return summary, err
		}
		current = c.persistTaskCookies(ctx, settings.CookieID, current, pending.UpdatedCookies)
		orders = append(orders, pending.Orders...)
		if len(pending.Orders) < 50 {
			break
		}
	}
	summary.Found = len(orders)
	for _, order := range orders {
		runKey := "rate:" + settings.CookieID + ":" + order.TradeID
		claimed, err := c.store.AccountTasks.ClaimRun(ctx, db.AccountTaskRun{RunKey: runKey, CookieID: settings.CookieID,
			TaskType: TaskAutoRate, TargetID: order.TradeID}, time.Now().UTC().Unix())
		if err != nil {
			return summary, err
		}
		if !claimed {
			summary.Skipped++
			continue
		}
		result, rateErr := c.accountTasks.RateBuyer(ctx, current, order.TradeID, settings.RateContent)
		if rateErr != nil || result == nil || !result.Success {
			message := errorString(rateErr)
			if result != nil && result.Message != "" {
				message = result.Message
			}
			_ = c.store.AccountTasks.FinishRun(ctx, runKey, "failed", 0, 1, message, time.Now().UTC().Add(10*time.Minute).Unix())
			summary.Failed++
			continue
		}
		current = c.persistTaskCookies(ctx, settings.CookieID, current, result.UpdatedCookies)
		_ = c.store.AccountTasks.FinishRun(ctx, runKey, "success", 1, 0, "", 0)
		summary.Success++
	}
	_ = c.store.AccountTasks.MarkRateScan(ctx, settings.CookieID, time.Now().UTC().Unix())
	return summary, nil
}

func (c *Center) runAutoPolish(ctx context.Context, settings db.AccountTaskSettings, now time.Time, manual bool) (AccountTaskSummary, error) {
	summary := AccountTaskSummary{TaskType: TaskAutoPolish}
	if c.accountTasks == nil {
		return summary, fmt.Errorf("擦亮客户端未初始化")
	}
	date := now.Format("2006-01-02")
	runKey := "polish:" + settings.CookieID + ":" + date
	run := db.AccountTaskRun{RunKey: runKey, CookieID: settings.CookieID, TaskType: TaskAutoPolish, RunDate: date}
	var claimed bool
	var err error
	if manual {
		claimed, err = c.store.AccountTasks.ClaimRunImmediately(ctx, run, time.Now().UTC().Unix())
	} else {
		claimed, err = c.store.AccountTasks.ClaimRun(ctx, run, time.Now().UTC().Unix())
	}
	if err != nil || !claimed {
		if !claimed && err == nil {
			summary.Skipped = 1
			summary.Message = "今天已经执行过擦亮"
		}
		return summary, err
	}
	cookies, err := c.store.Cookies.GetValue(ctx, settings.CookieID)
	if err != nil {
		_ = c.store.AccountTasks.FinishRun(ctx, runKey, "failed", 0, 1, err.Error(), time.Now().UTC().Add(10*time.Minute).Unix())
		return summary, err
	}
	items, err := c.accountTasks.FetchAllItems(ctx, cookies, polishItemPageSize, polishItemMaxPages)
	if err != nil {
		_ = c.store.AccountTasks.FinishRun(ctx, runKey, "failed", 0, 1, err.Error(), time.Now().UTC().Add(10*time.Minute).Unix())
		return summary, err
	}
	current := c.persistTaskCookies(ctx, settings.CookieID, cookies, items.UpdatedCookies)
	summary.Found = len(items.Items)
	var lastError string
	for _, item := range items.Items {
		result, polishErr := c.accountTasks.PolishItem(ctx, current, item.ID)
		if polishErr != nil || result == nil || !result.Success {
			summary.Failed++
			lastError = errorString(polishErr)
			if result != nil && result.Message != "" {
				lastError = result.Message
			}
			continue
		}
		current = c.persistTaskCookies(ctx, settings.CookieID, current, result.UpdatedCookies)
		summary.Success++
	}
	status, retryAt := "success", int64(0)
	if summary.Failed > 0 {
		status, retryAt = "failed", time.Now().UTC().Add(10*time.Minute).Unix()
	} else {
		_ = c.store.AccountTasks.MarkPolished(ctx, settings.CookieID, date, time.Now().UTC().Unix())
	}
	_ = c.store.AccountTasks.FinishRun(ctx, runKey, status, summary.Success, summary.Failed, lastError, retryAt)
	if summary.Failed > 0 {
		return summary, fmt.Errorf("%d 个商品擦亮失败: %s", summary.Failed, lastError)
	}
	return summary, nil
}

func (c *Center) persistTaskCookies(ctx context.Context, accountID, oldValue, newValue string) string {
	newValue = strings.TrimSpace(newValue)
	if newValue == "" || newValue == oldValue {
		return oldValue
	}
	if err := c.store.Cookies.UpdateValueExisting(ctx, accountID, newValue); err != nil {
		c.logger.Warn("保存账号任务响应 Cookie 失败", "account", accountID, "err", err)
		return oldValue
	}
	if c.senders != nil {
		if sender, ok := c.senders.Sender(accountID); ok && sender != nil {
			sender.UpdateCookie(newValue)
		}
	}
	return newValue
}

func beijingNow() time.Time {
	return time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
}

func polishDue(settings db.AccountTaskSettings, now time.Time) bool {
	if settings.LastPolishDate == now.Format("2006-01-02") {
		return false
	}
	target, err := time.Parse("15:04", settings.PolishTime)
	if err != nil {
		target, _ = time.Parse("15:04", "03:00")
	}
	return now.Hour() > target.Hour() || now.Hour() == target.Hour() && now.Minute() >= target.Minute()
}
