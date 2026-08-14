package automation

import (
	"context"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync"
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

// accountTaskCoordinator 负责账号自动评价、商品擦亮和凭证阻断状态。
// 它拥有账号任务专用的 Session 指纹状态，Center 只保留兼容调用入口和依赖装配。
type accountTaskCoordinator struct {
	// repository 提供账号任务所需的最小持久化能力。
	repository AccountTaskRepository
	// client 返回当前生效的账号任务平台客户端。
	client func() AccountTaskClient
	// senders 用于把任务响应 Cookie 同步到在线账号运行时。
	senders SenderProvider
	// recoverer 返回当前生效的凭证恢复器。
	recoverer func() CredentialRecoverer
	// logger 记录账号任务扫描、凭证恢复和 Cookie 持久化异常。
	logger interface {
		Warn(string, ...any)
	}
	// sessionExpired 保存 Session 失效时的凭证指纹，阻止同一凭证继续调用平台 API。
	sessionExpired sync.Map
}

// accountAutomationAllowed 判断账号是否未暂停且仍处于启用状态。
func (c *accountTaskCoordinator) accountAutomationAllowed(ctx context.Context, accountID string) (bool, error) {
	if c == nil || c.repository == nil {
		return false, fmt.Errorf("账号任务存储未初始化")
	}
	// paused 表示账号是否被用户临时暂停。
	// err 保存读取账号暂停状态的错误。
	paused, _, err := c.repository.IsPaused(ctx, accountID)
	if err != nil {
		return false, fmt.Errorf("读取账号暂停状态: %w", err)
	}
	// enabled 表示账号是否处于启用状态。
	// statusErr 保存读取账号启用状态的错误。
	enabled, statusErr := c.repository.Status(ctx, accountID)
	if statusErr != nil {
		return false, fmt.Errorf("读取账号启用状态: %w", statusErr)
	}
	return !paused && enabled, nil
}

// runAccountTask 执行指定账号任务，并在执行前检查账号状态门禁。
func (c *accountTaskCoordinator) runAccountTask(ctx context.Context, accountID, taskType string) (AccountTaskSummary, error) {
	allowed, err := c.accountAutomationAllowed(ctx, accountID)
	if err != nil {
		return AccountTaskSummary{TaskType: taskType}, err
	}
	if !allowed {
		return AccountTaskSummary{TaskType: taskType}, fmt.Errorf("账号已停用或暂停，无法执行任务")
	}
	settings, err := c.repository.Get(ctx, accountID)
	if err != nil {
		return AccountTaskSummary{TaskType: taskType}, err
	}
	return c.runConfiguredAccountTask(ctx, settings, taskType)
}

func (c *accountTaskCoordinator) runConfiguredAccountTask(ctx context.Context, settings db.AccountTaskSettings, taskType string) (AccountTaskSummary, error) {
	if blocked, err := c.accountTaskSessionBlocked(ctx, settings.CookieID); err != nil {
		return AccountTaskSummary{TaskType: taskType}, err
	} else if blocked {
		return AccountTaskSummary{TaskType: taskType, Skipped: 1, Message: "Session 已失效，等待续期或重新登录"},
			fmt.Errorf("账号 Session 已失效，已停止自动化 API 请求，等待续期或重新登录")
	}
	var (
		summary AccountTaskSummary
		err     error
	)
	switch taskType {
	case TaskAutoRate:
		summary, err = c.runAutoRate(ctx, settings)
	case TaskAutoPolish:
		summary, err = c.runAutoPolish(ctx, settings, beijingNow(), true)
	default:
		return AccountTaskSummary{TaskType: taskType}, fmt.Errorf("不支持的账号任务: %s", taskType)
	}
	if err != nil && mtop.IsSessionExpiredErr(err) {
		err = c.recoverAccountTaskSession(ctx, settings.CookieID, err)
	}
	return summary, err
}

func (c *accountTaskCoordinator) scanAccountTasks(ctx context.Context) {
	if c.client() == nil || c.repository == nil {
		return
	}
	settings, err := c.repository.Enabled(ctx)
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
		if blocked, blockErr := c.accountTaskSessionBlocked(ctx, setting.CookieID); blockErr != nil || blocked {
			continue
		}
		if setting.AutoRateEnabled {
			if _, err := c.runConfiguredAccountTask(ctx, setting, TaskAutoRate); err != nil {
				c.logger.Warn("自动评价扫描失败", "account", setting.CookieID, "err", err)
			}
		}
		if setting.AutoPolishEnabled && polishDue(setting, now) {
			if blocked, _ := c.accountTaskSessionBlocked(ctx, setting.CookieID); blocked {
				continue
			}
			_, taskErr := c.runAutoPolish(ctx, setting, now, false)
			if taskErr != nil && mtop.IsSessionExpiredErr(taskErr) {
				taskErr = c.recoverAccountTaskSession(ctx, setting.CookieID, taskErr)
			}
			if taskErr != nil {
				c.logger.Warn("每日擦亮失败", "account", setting.CookieID, "err", taskErr)
			}
		}
	}
}

func (c *accountTaskCoordinator) recoverAccountTaskSession(ctx context.Context, accountID string, sessionErr error) error {
	fingerprint, fingerprintErr := c.accountCredentialFingerprint(ctx, accountID)
	if fingerprintErr == nil {
		c.sessionExpired.Store(accountID, fingerprint)
	}
	c.logger.Warn("自动化 API 检测到 Session 过期，停止后续请求并开始即时续期", "account", accountID, "err", sessionErr)
	if recoverer := c.recoverer(); recoverer != nil && recoverer.RecoverExpiredCredential(ctx, accountID) {
		c.sessionExpired.Delete(accountID)
		return fmt.Errorf("%w；Session 续期成功，本次自动化已停止，下一轮将使用新凭证", sessionErr)
	}
	return fmt.Errorf("%w；已停止该账号自动化 API 请求，等待续期或重新登录", sessionErr)
}

func (c *accountTaskCoordinator) accountTaskSessionBlocked(ctx context.Context, accountID string) (bool, error) {
	blockedFingerprint, ok := c.sessionExpired.Load(accountID)
	if !ok {
		return false, nil
	}
	current, err := c.accountCredentialFingerprint(ctx, accountID)
	if err != nil {
		return true, err
	}
	if current != blockedFingerprint.(string) {
		c.sessionExpired.Delete(accountID)
		return false, nil
	}
	return true, nil
}

func (c *accountTaskCoordinator) accountCredentialFingerprint(ctx context.Context, accountID string) (string, error) {
	// data 是生成自动化 Session 阻断指纹所需的最小 Cookie 与 metadata 输入。
	data, err := c.repository.GetCookieRuntimeData(ctx, accountID)
	if err != nil {
		return "", err
	}
	// sum 是不暴露凭证内容的稳定摘要，用于判断续期是否更新了账号凭证。
	sum := sha256.Sum256([]byte(data.Value + "\x00" + data.MetadataJSON))
	return fmt.Sprintf("%x", sum[:]), nil
}

// runAutoRate 执行自动评价任务，并在明确的单值 Cookie 查询边界内调用平台 API。
func (c *accountTaskCoordinator) runAutoRate(ctx context.Context, settings db.AccountTaskSettings) (AccountTaskSummary, error) {
	summary := AccountTaskSummary{TaskType: TaskAutoRate}
	if c.client() == nil {
		return summary, fmt.Errorf("自动评价客户端未初始化")
	}
	cookies, err := c.repository.GetValue(ctx, settings.CookieID)
	if err != nil {
		return summary, err
	}
	current := cookies
	var orders []mtop.PendingRateOrder
	for page := 1; page <= 20; page++ {
		pending, err := c.client().FetchPendingRateOrders(ctx, current, page, 50)
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
		claimed, err := c.repository.ClaimRun(ctx, db.AccountTaskRun{RunKey: runKey, CookieID: settings.CookieID,
			TaskType: TaskAutoRate, TargetID: order.TradeID}, time.Now().UTC().Unix())
		if err != nil {
			return summary, err
		}
		if !claimed {
			summary.Skipped++
			continue
		}
		result, rateErr := c.client().RateBuyer(ctx, current, order.TradeID, settings.RateContent)
		if rateErr != nil || result == nil || !result.Success {
			message := errorString(rateErr)
			if result != nil && result.Message != "" {
				message = result.Message
			}
			_ = c.repository.FinishRun(ctx, runKey, "failed", 0, 1, message, time.Now().UTC().Add(10*time.Minute).Unix())
			summary.Failed++
			if mtop.IsSessionExpiredErr(rateErr) {
				return summary, rateErr
			}
			continue
		}
		current = c.persistTaskCookies(ctx, settings.CookieID, current, result.UpdatedCookies)
		_ = c.repository.FinishRun(ctx, runKey, "success", 1, 0, "", 0)
		summary.Success++
	}
	_ = c.repository.MarkRateScan(ctx, settings.CookieID, time.Now().UTC().Unix())
	return summary, nil
}

func (c *accountTaskCoordinator) runAutoPolish(ctx context.Context, settings db.AccountTaskSettings, now time.Time, manual bool) (AccountTaskSummary, error) {
	summary := AccountTaskSummary{TaskType: TaskAutoPolish}
	if c.client() == nil {
		return summary, fmt.Errorf("擦亮客户端未初始化")
	}
	date := now.Format("2006-01-02")
	runKey := "polish:" + settings.CookieID + ":" + date
	run := db.AccountTaskRun{RunKey: runKey, CookieID: settings.CookieID, TaskType: TaskAutoPolish, RunDate: date}
	var claimed bool
	var err error
	if manual {
		claimed, err = c.repository.ClaimRunImmediately(ctx, run, time.Now().UTC().Unix())
	} else {
		claimed, err = c.repository.ClaimRun(ctx, run, time.Now().UTC().Unix())
	}
	if err != nil || !claimed {
		if !claimed && err == nil {
			summary.Skipped = 1
			summary.Message = "今天已经执行过擦亮"
		}
		return summary, err
	}
	cookies, err := c.repository.GetValue(ctx, settings.CookieID)
	if err != nil {
		_ = c.repository.FinishRun(ctx, runKey, "failed", 0, 1, err.Error(), time.Now().UTC().Add(10*time.Minute).Unix())
		return summary, err
	}
	items, err := c.client().FetchAllItems(ctx, cookies, polishItemPageSize, polishItemMaxPages)
	if err != nil {
		_ = c.repository.FinishRun(ctx, runKey, "failed", 0, 1, err.Error(), time.Now().UTC().Add(10*time.Minute).Unix())
		return summary, err
	}
	current := c.persistTaskCookies(ctx, settings.CookieID, cookies, items.UpdatedCookies)
	summary.Found = len(items.Items)
	var lastError string
	for _, item := range items.Items {
		result, polishErr := c.client().PolishItem(ctx, current, item.ID)
		if polishErr != nil || result == nil || !result.Success {
			summary.Failed++
			lastError = errorString(polishErr)
			if result != nil && result.Message != "" {
				lastError = result.Message
			}
			if mtop.IsSessionExpiredErr(polishErr) {
				_ = c.repository.FinishRun(ctx, runKey, "failed", summary.Success, summary.Failed, lastError, 0)
				return summary, polishErr
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
		_ = c.repository.MarkPolished(ctx, settings.CookieID, date, time.Now().UTC().Unix())
	}
	_ = c.repository.FinishRun(ctx, runKey, status, summary.Success, summary.Failed, lastError, retryAt)
	if summary.Failed > 0 {
		return summary, fmt.Errorf("%d 个商品擦亮失败: %s", summary.Failed, lastError)
	}
	return summary, nil
}

func (c *accountTaskCoordinator) persistTaskCookies(ctx context.Context, accountID, oldValue, newValue string) string {
	newValue = strings.TrimSpace(newValue)
	if newValue == "" || newValue == oldValue {
		return oldValue
	}
	if err := c.repository.UpdateValueExisting(ctx, accountID, newValue); err != nil {
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
