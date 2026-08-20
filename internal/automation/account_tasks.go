package automation

import (
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// TaskAutoRate 用于本次流程后续判断的任务AutoRate
const (
	TaskAutoRate       = "auto_rate"
	TaskAutoPolish     = "auto_polish"
	polishItemPageSize = 20
	polishItemMaxPages = 20
)

// AccountTaskClient 用于本次流程后续判断的账号任务Client
type AccountTaskClient interface {
	FetchPendingRateOrders(ctx context.Context, cookiesStr string, page, pageSize int) (*mtop.PendingRateResult, error)
	RateBuyer(ctx context.Context, cookiesStr, tradeID, feedback string) (*mtop.AccountTaskResult, error)
	FetchAllItems(ctx context.Context, cookiesStr string, pageSize, maxPages int) (*mtop.ItemListResult, error)
	PolishItem(ctx context.Context, cookiesStr, itemID string) (*mtop.AccountTaskResult, error)
}

// AccountTaskSummary 用于本次流程后续判断的账号任务Summary
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
// accountTaskCoordinator 用于本次流程后续判断的账号任务Coordinator
type accountTaskCoordinator struct {
	// repository 提供账号任务所需的最小持久化能力。
	repository AccountTaskRepository
	// client 返回构造期固定的账号任务平台客户端。
	client func() AccountTaskClient
	// senders 用于把任务响应 Cookie 同步到在线账号运行时。
	senders SenderProvider
	// recoverer 返回构造期固定的凭证恢复器。
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
	// paused、err 用于本次流程后续判断的paused、err
	paused, _, err := c.repository.IsPaused(ctx, accountID)
	if err != nil {
		return false, fmt.Errorf("读取账号暂停状态: %w", err)
	}
	// enabled 表示账号是否处于启用状态。
	// statusErr 保存读取账号启用状态的错误。
	// enabled、statusErr 用于本次流程后续判断的enabled、statusErr
	enabled, statusErr := c.repository.Status(ctx, accountID)
	if statusErr != nil {
		return false, fmt.Errorf("读取账号启用状态: %w", statusErr)
	}
	return !paused && enabled, nil
}

// runAccountTask 执行指定账号任务，并在执行前检查账号状态门禁。
func (c *accountTaskCoordinator) runAccountTask(ctx context.Context, accountID, taskType string) (AccountTaskSummary, error) {
	// allowed、err 用于本次流程后续判断的allowed、err
	allowed, err := c.accountAutomationAllowed(ctx, accountID)
	if err != nil {
		return AccountTaskSummary{TaskType: taskType}, err
	}
	if !allowed {
		return AccountTaskSummary{TaskType: taskType}, fmt.Errorf("账号已停用或暂停，无法执行任务")
	}
	// settings、err 用于本次流程后续判断的settings、err
	settings, err := c.repository.Get(ctx, accountID)
	if err != nil {
		return AccountTaskSummary{TaskType: taskType}, err
	}
	return c.runConfiguredAccountTask(ctx, settings, taskType)
}

// runConfiguredAccountTask 封装运行Configured账号任务业务协调。
func (c *accountTaskCoordinator) runConfiguredAccountTask(ctx context.Context, settings db.AccountTaskSettings, taskType string) (AccountTaskSummary, error) {
	if // blocked、err 用于本次流程后续判断的blocked、err
	blocked, err := c.accountTaskSessionBlocked(ctx, settings.CookieID); err != nil {
		return AccountTaskSummary{TaskType: taskType}, err
	} else if blocked {
		return AccountTaskSummary{TaskType: taskType, Skipped: 1, Message: "Session 已失效，等待续期或重新登录"},
			fmt.Errorf("账号 Session 已失效，已停止自动化 API 请求，等待续期或重新登录")
	}
	// summary 用于本次流程后续判断的summary
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

// scanAccountTasks 封装scan账号任务列表业务协调。
func (c *accountTaskCoordinator) scanAccountTasks(ctx context.Context) {
	if c.client() == nil || c.repository == nil {
		return
	}
	// settings、err 用于本次流程后续判断的settings、err
	settings, err := c.repository.Enabled(ctx)
	if err != nil {
		c.logger.Warn("扫描账号任务配置失败", "err", err)
		return
	}
	// now 用于本次流程后续判断的now
	now := beijingNow()
	// setting 表示当前遍历过程中的设置
	for _, setting := range settings {
		// allowed、err 用于本次流程后续判断的allowed、err
		allowed, err := c.accountAutomationAllowed(ctx, setting.CookieID)
		if err != nil || !allowed {
			continue
		}
		if // blocked、blockErr 用于本次流程后续判断的blocked、blockErr
		blocked, blockErr := c.accountTaskSessionBlocked(ctx, setting.CookieID); blockErr != nil || blocked {
			continue
		}
		if setting.AutoRateEnabled {
			if // err 用于本次流程后续判断的err
			_, err := c.runConfiguredAccountTask(ctx, setting, TaskAutoRate); err != nil {
				c.logger.Warn("自动评价扫描失败", "account", setting.CookieID, "err", err)
			}
		}
		if setting.AutoPolishEnabled && polishDue(setting, now) {
			if // blocked 用于本次流程后续判断的blocked
			blocked, _ := c.accountTaskSessionBlocked(ctx, setting.CookieID); blocked {
				continue
			}
			// taskErr 用于本次流程后续判断的任务Err
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

// recoverAccountTaskSession 封装recover账号任务会话业务协调。
func (c *accountTaskCoordinator) recoverAccountTaskSession(ctx context.Context, accountID string, sessionErr error) error {
	// fingerprint、fingerprintErr 用于本次流程后续判断的fingerprint、fingerprintErr
	fingerprint, fingerprintErr := c.accountCredentialFingerprint(ctx, accountID)
	if fingerprintErr == nil {
		c.sessionExpired.Store(accountID, fingerprint)
	}
	c.logger.Warn("自动化 API 检测到 Session 过期，停止后续请求并开始即时续期", "account", accountID, "err", sessionErr)
	if // recoverer 用于本次流程后续判断的recoverer
	recoverer := c.recoverer(); recoverer != nil && recoverer.RecoverExpiredCredential(ctx, accountID) {
		c.sessionExpired.Delete(accountID)
		return fmt.Errorf("%w；Session 续期成功，本次自动化已停止，下一轮将使用新凭证", sessionErr)
	}
	return fmt.Errorf("%w；已停止该账号自动化 API 请求，等待续期或重新登录", sessionErr)
}

// accountTaskSessionBlocked 封装账号任务会话Blocked业务协调。
func (c *accountTaskCoordinator) accountTaskSessionBlocked(ctx context.Context, accountID string) (bool, error) {
	// blockedFingerprint、ok 用于本次流程后续判断的blockedFingerprint、ok
	blockedFingerprint, ok := c.sessionExpired.Load(accountID)
	if !ok {
		return false, nil
	}
	// current、err 用于本次流程后续判断的current、err
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

// accountCredentialFingerprint 封装账号CredentialFingerprint业务协调。
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

// finishAccountTaskRun 保存账号任务的最终状态；首次写入失败时立即隔离运行记录，避免外部动作已经成功却被下一轮重复执行。
// 返回值会同时保留首次写入和隔离写入的错误，调用方可据此告警并阻止自动重放。
func (c *accountTaskCoordinator) finishAccountTaskRun(ctx context.Context, runKey, status string, success, failed int, message string, nextRetryAt int64) error {
	// finishErr 表示尝试写入预期运行状态时的数据库错误。
	finishErr := c.repository.FinishRun(ctx, runKey, status, success, failed, message, nextRetryAt)
	if finishErr == nil {
		return nil
	}
	// quarantineMessage 说明外部动作结果已经产生但本地状态未能正常收口，禁止系统自动重放。
	quarantineMessage := fmt.Sprintf("账号任务外部动作结果可能已执行，但运行状态保存失败，请人工核对，禁止自动重放: %v", finishErr)
	// quarantineCtx、cancel 确保人工核对状态写入不受已取消请求影响，并在函数返回时释放计时器。
	quarantineCtx, cancel := newAccountTaskCompensationContext(ctx)
	defer cancel()
	// quarantineErr 表示补偿上下文下写入 needs_review 隔离状态的数据库错误；该错误必须与首次收口失败一并返回。
	quarantineErr := c.repository.FinishRun(quarantineCtx, runKey, "needs_review", success, failed, quarantineMessage, 0)
	if quarantineErr != nil {
		return errors.Join(
			errAutomationNeedsReview,
			fmt.Errorf("保存账号任务运行结果失败: %w", finishErr),
			fmt.Errorf("保存账号任务人工核对状态失败: %w", quarantineErr),
		)
	}
	return errors.Join(errAutomationNeedsReview, fmt.Errorf("保存账号任务运行结果失败: %w", finishErr))
}

// quarantineAccountTaskRun 将外部动作结果未知的账号任务运行记录隔离到人工核对状态。
// 当隔离写入再次失败时，返回值会保留原始原因和第二次数据库错误。
func (c *accountTaskCoordinator) quarantineAccountTaskRun(ctx context.Context, runKey string, success, failed int, reason error) error {
	// quarantineMessage 将本地持久化故障转换为运维可识别的人工核对原因。
	quarantineMessage := fmt.Sprintf("账号任务外部动作结果未知，请人工核对，禁止自动重放: %v", reason)
	// quarantineCtx、cancel 确保人工核对状态写入不受已取消请求影响，并在函数返回时释放计时器。
	quarantineCtx, cancel := newAccountTaskCompensationContext(ctx)
	defer cancel()
	// quarantineErr 表示补偿上下文下再次写入 needs_review 状态的数据库错误；它决定是否需要同时报告两次持久化失败。
	quarantineErr := c.repository.FinishRun(quarantineCtx, runKey, "needs_review", success, failed, quarantineMessage, 0)
	if quarantineErr != nil {
		return errors.Join(
			errAutomationNeedsReview,
			fmt.Errorf("账号任务本地持久化失败: %w", reason),
			fmt.Errorf("保存账号任务人工核对状态失败: %w", quarantineErr),
		)
	}
	return errors.Join(errAutomationNeedsReview, fmt.Errorf("账号任务本地持久化失败: %w", reason))
}

// newAccountTaskCompensationContext 基于调用方值域创建不受其取消影响的短时补偿上下文；返回的取消函数由调用方负责释放计时器。
func newAccountTaskCompensationContext(parent context.Context) (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(parent), 5*time.Second)
}

// runAutoRate 执行自动评价任务，并在明确的单值 Cookie 查询边界内调用平台 API。
func (c *accountTaskCoordinator) runAutoRate(ctx context.Context, settings db.AccountTaskSettings) (AccountTaskSummary, error) {
	// summary 用于本次流程后续判断的summary
	summary := AccountTaskSummary{TaskType: TaskAutoRate}
	if c.client() == nil {
		return summary, fmt.Errorf("自动评价客户端未初始化")
	}
	// cookies、err 用于本次流程后续判断的cookies、err
	cookies, err := c.repository.GetValue(ctx, settings.CookieID)
	if err != nil {
		return summary, err
	}
	// current 用于本次流程后续判断的current
	current := cookies
	// orders 用于本次流程后续判断的订单列表
	var orders []mtop.PendingRateOrder
	for // page 用于本次流程后续判断的页码
	page := 1; page <= 20; page++ {
		// pending、err 用于本次流程后续判断的pending、err
		pending, err := c.client().FetchPendingRateOrders(ctx, current, page, 50)
		if err != nil {
			return summary, err
		}
		// updatedCookies 保存扫描接口返回的最新 Cookie；cookieErr 表示同步该 Cookie 时的持久化错误。
		updatedCookies, cookieErr := c.persistTaskCookies(ctx, settings.CookieID, current, pending.UpdatedCookies)
		if cookieErr != nil {
			return summary, cookieErr
		}
		current = updatedCookies
		orders = append(orders, pending.Orders...)
		if len(pending.Orders) < 50 {
			break
		}
	}
	summary.Found = len(orders)
	// order 表示当前遍历过程中的订单
	for _, order := range orders {
		// runKey 用于本次流程后续判断的运行Key
		runKey := "rate:" + settings.CookieID + ":" + order.TradeID
		// claimed、err 用于本次流程后续判断的claimed、err
		claimed, err := c.repository.ClaimRun(ctx, db.AccountTaskRun{RunKey: runKey, CookieID: settings.CookieID,
			TaskType: TaskAutoRate, TargetID: order.TradeID}, time.Now().UTC().Unix())
		if err != nil {
			return summary, err
		}
		if !claimed {
			summary.Skipped++
			continue
		}
		// result、rateErr 用于本次流程后续判断的result、rateErr
		result, rateErr := c.client().RateBuyer(ctx, current, order.TradeID, settings.RateContent)
		if rateErr != nil || result == nil || !result.Success {
			// message 用于本次流程后续判断的消息
			message := errorString(rateErr)
			if result != nil && result.Message != "" {
				message = result.Message
			}
			// finishErr 保存失败结果的落库错误；失败时隔离运行记录，避免错误状态不明导致重放。
			finishErr := c.finishAccountTaskRun(ctx, runKey, "failed", 0, 1, message, time.Now().UTC().Add(10*time.Minute).Unix())
			if finishErr != nil {
				return summary, errors.Join(rateErr, finishErr)
			}
			summary.Failed++
			if mtop.IsSessionExpiredErr(rateErr) {
				return summary, rateErr
			}
			continue
		}
		// updatedCookies 保存评价接口返回的最新 Cookie；cookieErr 表示动作成功后同步 Cookie 的落库错误。
		updatedCookies, cookieErr := c.persistTaskCookies(ctx, settings.CookieID, current, result.UpdatedCookies)
		if cookieErr != nil {
			return summary, c.quarantineAccountTaskRun(ctx, runKey, summary.Success+1, summary.Failed, cookieErr)
		}
		current = updatedCookies
		// finishErr 保存评价成功结果的落库错误；失败时会把运行记录隔离为人工核对。
		finishErr := c.finishAccountTaskRun(ctx, runKey, "success", 1, 0, "", 0)
		if finishErr != nil {
			return summary, finishErr
		}
		summary.Success++
	}
	// markErr 表示自动评价扫描时间写入失败；不能静默忽略，否则调度状态会永久滞后。
	markErr := c.repository.MarkRateScan(ctx, settings.CookieID, time.Now().UTC().Unix())
	if markErr != nil {
		return summary, fmt.Errorf("保存自动评价扫描时间: %w", markErr)
	}
	return summary, nil
}

// runAutoPolish 封装运行AutoPolish业务协调。
func (c *accountTaskCoordinator) runAutoPolish(ctx context.Context, settings db.AccountTaskSettings, now time.Time, manual bool) (AccountTaskSummary, error) {
	// summary 用于本次流程后续判断的summary
	summary := AccountTaskSummary{TaskType: TaskAutoPolish}
	if c.client() == nil {
		return summary, fmt.Errorf("擦亮客户端未初始化")
	}
	// date 用于本次流程后续判断的日期
	date := now.Format("2006-01-02")
	// runKey 用于本次流程后续判断的运行Key
	runKey := "polish:" + settings.CookieID + ":" + date
	// run 用于本次流程后续判断的运行
	run := db.AccountTaskRun{RunKey: runKey, CookieID: settings.CookieID, TaskType: TaskAutoPolish, RunDate: date}
	// claimed 用于本次流程后续判断的claimed
	var claimed bool
	// err 用于本次流程后续判断的err
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
	// cookies、err 用于本次流程后续判断的cookies、err
	cookies, err := c.repository.GetValue(ctx, settings.CookieID)
	if err != nil {
		return summary, errors.Join(err, c.finishAccountTaskRun(ctx, runKey, "failed", 0, 1, err.Error(), time.Now().UTC().Add(10*time.Minute).Unix()))
	}
	// items、err 用于本次流程后续判断的items、err
	items, err := c.client().FetchAllItems(ctx, cookies, polishItemPageSize, polishItemMaxPages)
	if err != nil {
		return summary, errors.Join(err, c.finishAccountTaskRun(ctx, runKey, "failed", 0, 1, err.Error(), time.Now().UTC().Add(10*time.Minute).Unix()))
	}
	// current 用于本次流程后续判断的current
	// current 保存擦亮接口返回前可继续使用的最新 Cookie。
	current, cookieErr := c.persistTaskCookies(ctx, settings.CookieID, cookies, items.UpdatedCookies)
	if cookieErr != nil {
		return summary, errors.Join(cookieErr, c.quarantineAccountTaskRun(ctx, runKey, 0, 0, cookieErr))
	}
	summary.Found = len(items.Items)
	if summary.Found == 0 {
		// emptyMessage 说明本次未拉到任何在售商品；按失败记录并保留当日重试机会，避免空跑被记成功后锁定全天。
		const emptyMessage = "商品列表未发现在售商品，未执行擦亮，稍后将自动重试"
		c.logger.Warn("每日擦亮未发现在售商品", "account", settings.CookieID)
		// finishErr 保存空商品运行结果的落库错误。
		finishErr := c.finishAccountTaskRun(ctx, runKey, "failed", 0, 0, emptyMessage, time.Now().UTC().Add(10*time.Minute).Unix())
		if finishErr != nil {
			return summary, finishErr
		}
		summary.Message = emptyMessage
		return summary, nil
	}
	// lastError 用于本次流程后续判断的last错误
	var lastError string
	// item 表示当前遍历过程中的商品
	for _, item := range items.Items {
		// result、polishErr 用于本次流程后续判断的result、polishErr
		result, polishErr := c.client().PolishItem(ctx, current, item.ID)
		if polishErr != nil || result == nil || !result.Success {
			summary.Failed++
			lastError = errorString(polishErr)
			if result != nil && result.Message != "" {
				lastError = result.Message
			}
			if mtop.IsSessionExpiredErr(polishErr) {
				return summary, errors.Join(polishErr, c.finishAccountTaskRun(ctx, runKey, "failed", summary.Success, summary.Failed, lastError, 0))
			}
			continue
		}
		// updatedCookies 保存擦亮接口返回的最新 Cookie；cookieErr 表示外部动作成功后同步 Cookie 的落库错误。
		updatedCookies, cookieErr := c.persistTaskCookies(ctx, settings.CookieID, current, result.UpdatedCookies)
		if cookieErr != nil {
			return summary, c.quarantineAccountTaskRun(ctx, runKey, summary.Success+1, summary.Failed, cookieErr)
		}
		current = updatedCookies
		summary.Success++
	}
	// status、retryAt 用于本次流程后续判断的status、retryAt
	status, retryAt := "success", int64(0)
	if summary.Failed > 0 {
		status, retryAt = "failed", time.Now().UTC().Add(10*time.Minute).Unix()
	} else {
		// markErr 表示所有商品已擦亮但日期索引写入失败；返回错误以便运维补偿而不伪装成成功。
		markErr := c.repository.MarkPolished(ctx, settings.CookieID, date, time.Now().UTC().Unix())
		if markErr != nil {
			// persistenceErr 为日期索引失败补充业务上下文，便于人工核对具体失败边界。
			persistenceErr := fmt.Errorf("保存商品擦亮日期: %w", markErr)
			return summary, errors.Join(persistenceErr, c.quarantineAccountTaskRun(ctx, runKey, summary.Success, summary.Failed, persistenceErr))
		}
	}
	// finishErr 保存擦亮任务汇总结果的落库错误；失败时将运行记录隔离，避免下一次重放已完成商品。
	finishErr := c.finishAccountTaskRun(ctx, runKey, status, summary.Success, summary.Failed, lastError, retryAt)
	if finishErr != nil {
		return summary, finishErr
	}
	if summary.Failed > 0 {
		return summary, fmt.Errorf("%d 个商品擦亮失败: %s", summary.Failed, lastError)
	}
	return summary, nil
}

// persistTaskCookies 封装persist任务Cookies业务协调。
func (c *accountTaskCoordinator) persistTaskCookies(ctx context.Context, accountID, oldValue, newValue string) (string, error) {
	newValue = strings.TrimSpace(newValue)
	if newValue == "" || newValue == oldValue {
		return oldValue, nil
	}
	if // err 用于本次流程后续判断的err
	err := c.repository.UpdateValueExisting(ctx, accountID, newValue); err != nil {
		c.logger.Warn("保存账号任务响应 Cookie 失败", "account", accountID, "err", err)
		return oldValue, fmt.Errorf("保存账号任务响应 Cookie: %w", err)
	}
	if c.senders != nil {
		if // sender、ok 用于本次流程后续判断的sender、ok
		sender, ok := c.senders.Sender(accountID); ok && sender != nil {
			sender.UpdateCookie(newValue)
		}
	}
	return newValue, nil
}

// beijingNow 封装beijingNow业务协调。
func beijingNow() time.Time {
	return time.Now().In(time.FixedZone("Asia/Shanghai", 8*60*60))
}

// polishDue 封装polishDue业务协调。
func polishDue(settings db.AccountTaskSettings, now time.Time) bool {
	if settings.LastPolishDate == now.Format("2006-01-02") {
		return false
	}
	// target、err 用于本次流程后续判断的target、err
	target, err := time.Parse("15:04", settings.PolishTime)
	if err != nil {
		target, _ = time.Parse("15:04", "03:00")
	}
	return now.Hour() > target.Hour() || now.Hour() == target.Hour() && now.Minute() >= target.Minute()
}
