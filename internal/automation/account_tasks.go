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
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// TaskAutoRate 用于本次流程后续判断的任务AutoRate
const (
	TaskAutoRate       = "auto_rate"
	TaskAutoPolish     = "auto_polish"
	polishItemPageSize = 20
	polishItemMaxPages = 20
)

// errAccountTaskCredentialRenewed 表示本轮任务因凭证已续期而提前结束，等待后续扫描使用新凭证重试。
var errAccountTaskCredentialRenewed = errors.New("账号任务凭证已续期，等待下一轮重试")

// AccountTaskClient 用于本次流程后续判断的账号任务Client
type AccountTaskClient interface {
	RateBuyer(ctx context.Context, cookiesStr, tradeID, feedback string) (*mtop.AccountTaskResult, error)
	FetchAllItems(ctx context.Context, cookiesStr string, pageSize, maxPages int) (*mtop.ItemListResult, error)
	PolishItem(ctx context.Context, cookiesStr, itemID string) (*mtop.AccountTaskResult, error)
}

// accountTaskCookieWriter 表示能够保留 Cookie 快照 metadata 的账号任务写回能力。
type accountTaskCookieWriter interface {
	// UpdateRenewalCookie 保存平台响应产生的 Cookie 与完整 metadata。
	UpdateRenewalCookie(ctx context.Context, cookieID, cookieValue, metadataJSON string, lastRefreshAt int64) error
}

// accountTaskCredentialSession 保存一次账号任务使用的请求上下文和 Cookie 会话。
// 外部平台请求结束后必须通过该会话收口响应 Cookie，避免失败响应丢失凭证轮换。
type accountTaskCredentialSession struct {
	// requestContext 携带权威 Cookie 快照或历史扁平 Cookie 会话。
	requestContext context.Context
	// cookieSession 吸收本次任务所有 MTOP 响应的 Set-Cookie。
	cookieSession *mtop.CookieSession
	// cookieValue 保存任务开始时读取到的扁平 Cookie，供无快照兼容路径使用。
	cookieValue string
	// persistedValue 和 persistedMetadata 固定最近一次成功读写的凭证版本，仅用于锁内冲突校验。
	persistedValue    string
	persistedMetadata string
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
	// logger 记录账号任务扫描、凭证恢复和 Cookie 持久化异常；空商品列表是成功态，使用 Info 记录。
	logger interface {
		Debug(string, ...any)
		Info(string, ...any)
		Warn(string, ...any)
	}
	// sessionExpired 保存 Session 失效时的凭证指纹，阻止同一凭证继续调用平台 API；仅 Token 失效不进入该阻断表。
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
		err = c.recoverAccountTaskCredential(ctx, settings.CookieID, err)
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
		if err != nil {
			c.logger.Warn("检查账号任务账号状态失败", "account", setting.CookieID, "err", err)
			continue
		}
		if !allowed {
			continue
		}
		if // blocked、blockErr 用于本次流程后续判断的blocked、blockErr
		blocked, blockErr := c.accountTaskSessionBlocked(ctx, setting.CookieID); blockErr != nil || blocked {
			if blockErr != nil {
				c.logger.Warn("检查账号任务 Session 状态失败", "account", setting.CookieID, "err", blockErr)
			} else if blocked {
				c.logger.Info("账号任务因 Session 已失效跳过，等待凭证续期", "account", setting.CookieID)
			}
			continue
		}
		if setting.AutoRateEnabled {
			// summary、err 保存本轮自动评价扫描结果及其错误；成功结果必须落一条可检索日志。
			summary, err := c.runConfiguredAccountTask(ctx, setting, TaskAutoRate)
			if err != nil {
				if errors.Is(err, errAccountTaskCredentialRenewed) {
					c.logger.Info("自动评价扫描因凭证续期暂停，下一轮自动重试", "account", setting.CookieID, "err", err)
				} else {
					c.logger.Warn("自动评价扫描失败", "account", setting.CookieID, "err", err)
				}
			} else {
				c.logger.Debug("自动评价扫描完成", "account", setting.CookieID,
					"found", summary.Found, "success", summary.Success, "failed", summary.Failed, "skipped", summary.Skipped)
			}
		}
		if setting.AutoPolishEnabled && polishDue(setting, now) {
			if // blocked 用于本次流程后续判断的blocked
			blocked, _ := c.accountTaskSessionBlocked(ctx, setting.CookieID); blocked {
				continue
			}
			// polishSummary、taskErr 保存本轮擦亮结果及其错误；成功结果必须落一条可检索日志。
			polishSummary, taskErr := c.runAutoPolish(ctx, setting, now, false)
			if taskErr != nil && mtop.IsSessionExpiredErr(taskErr) {
				taskErr = c.recoverAccountTaskCredential(ctx, setting.CookieID, taskErr)
			}
			if taskErr != nil {
				if errors.Is(taskErr, errAccountTaskCredentialRenewed) {
					c.logger.Info("每日擦亮因凭证续期暂停，下一轮自动重试", "account", setting.CookieID, "err", taskErr)
				} else {
					c.logger.Warn("每日擦亮失败", "account", setting.CookieID, "err", taskErr)
				}
			} else {
				c.logger.Info("每日擦亮完成", "account", setting.CookieID,
					"found", polishSummary.Found, "success", polishSummary.Success,
					"failed", polishSummary.Failed, "skipped", polishSummary.Skipped)
			}
		}
	}
}

// recoverAccountTaskCredential 仅为 accountID 的明确 Session 失效请求账号恢复；ctx 控制存储和续期生命周期。
// c 维护同凭证的自动化阻断；credentialErr 为原始平台错误，返回值保留其分类并说明恢复结果。
func (c *accountTaskCoordinator) recoverAccountTaskCredential(ctx context.Context, accountID string, credentialErr error) error {
	if !mtop.IsSessionExpiredErr(credentialErr) {
		return credentialErr
	}
	// fingerprint、fingerprintErr 保存当前凭证指纹及读取失败；用于阻止同一失效会话继续发送业务请求。
	fingerprint, fingerprintErr := c.accountCredentialFingerprint(ctx, accountID)
	if fingerprintErr == nil {
		c.sessionExpired.Store(accountID, fingerprint)
	}
	c.logger.Warn("自动化 API 检测到 Session 失效，停止后续请求并开始即时续期", "account", accountID, "err", credentialErr)
	// recoverer 是固定装配的账号恢复端口，只接收明确的 Session 失效。
	if recoverer := c.recoverer(); recoverer != nil && recoverer.RecoverExpiredCredential(ctx, accountID) {
		c.sessionExpired.Delete(accountID)
		return fmt.Errorf("%w；%w；Session 续期成功，本次自动化已停止，下一轮将使用新凭证", errAccountTaskCredentialRenewed, credentialErr)
	}
	return fmt.Errorf("%w；已停止该账号自动化 API 请求，等待续期或重新登录", credentialErr)
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

// openAccountTaskCredentialSession 读取账号任务使用的最小凭证视图，并构造可吸收响应 Cookie 的请求上下文。
func (c *accountTaskCoordinator) openAccountTaskCredentialSession(ctx context.Context, accountID string) (accountTaskCredentialSession, error) {
	// data 保存账号任务所需的 Cookie 和 metadata；不读取登录密码或账号资料。
	data, err := c.repository.GetCookieRuntimeData(ctx, accountID)
	if err != nil {
		return accountTaskCredentialSession{}, err
	}
	// cookieValue 保存数据库中的扁平 Cookie；旧测试仓储没有运行时视图时回退到兼容读取接口。
	cookieValue := data.Value
	if strings.TrimSpace(cookieValue) == "" && strings.TrimSpace(data.MetadataJSON) == "" {
		cookieValue, err = c.repository.GetValue(ctx, accountID)
		if err != nil {
			return accountTaskCredentialSession{}, err
		}
	}
	// requestContext 保存携带本轮 Cookie 会话的外部请求上下文。
	var requestContext context.Context
	// session 保存本轮平台响应的 Cookie 更新；完整快照优先于扁平 Cookie。
	var session *mtop.CookieSession
	// snapshot、complete 保存 metadata 中的完整 Cookie 快照及其有效性。
	if snapshot, complete := cookierefresh.SnapshotFromMetadataOK(data.MetadataJSON); complete {
		requestContext, session = mtop.WithCookieSnapshot(mtop.WithBackgroundPacing(ctx), snapshot)
	} else {
		requestContext, session = mtop.WithFlatCookieSession(mtop.WithBackgroundPacing(ctx), cookieValue)
	}
	return accountTaskCredentialSession{requestContext: requestContext, cookieSession: session, cookieValue: cookieValue, persistedValue: data.Value, persistedMetadata: data.MetadataJSON}, nil
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

// runAutoRate 只消费 WebSocket 已确认收货的本地完成订单；它不会以远端 MTOP 列表扫描发现候选订单。
func (c *accountTaskCoordinator) runAutoRate(ctx context.Context, settings db.AccountTaskSettings) (AccountTaskSummary, error) {
	// summary 用于本次流程后续判断的summary
	summary := AccountTaskSummary{TaskType: TaskAutoRate}
	if c.repository == nil {
		return summary, fmt.Errorf("账号任务存储未初始化")
	}
	// orderIDs、err 保存本地买家已确认收货候选订单及读取错误；该读取不触达闲鱼平台。
	orderIDs, err := c.repository.DueAutoRateOrderIDs(ctx, settings.CookieID, 200)
	if err != nil {
		return summary, fmt.Errorf("读取本地已评价订单: %w", err)
	}
	summary.Found = len(orderIDs)
	if len(orderIDs) == 0 {
		// err 保存本地候选为空时写入扫描时间的错误。
		if err := c.repository.MarkRateScan(ctx, settings.CookieID, time.Now().UTC().Unix()); err != nil {
			return summary, fmt.Errorf("保存自动评价本地扫描时间: %w", err)
		}
		return summary, nil
	}
	if c.client() == nil {
		return summary, fmt.Errorf("自动评价客户端未初始化")
	}
	// credential、err 保存仅在存在本地候选订单时才读取的 Cookie 会话及其读取错误。
	credential, err := c.openAccountTaskCredentialSession(ctx, settings.CookieID)
	if err != nil {
		return summary, err
	}
	// current 保存本轮任务当前可继续使用的扁平 Cookie。
	current := credential.cookieValue
	// orderID 表示当前由买家确认收货 WebSocket 确认、可执行评价动作的订单。
	for _, orderID := range orderIDs {
		// runKey 用于本次流程后续判断的运行Key
		runKey := "rate:" + settings.CookieID + ":" + orderID
		// claimed、err 用于本次流程后续判断的claimed、err
		claimed, err := c.repository.ClaimRun(ctx, db.AccountTaskRun{RunKey: runKey, CookieID: settings.CookieID,
			TaskType: TaskAutoRate, TargetID: orderID}, time.Now().UTC().Unix())
		if err != nil {
			return summary, err
		}
		if !claimed {
			summary.Skipped++
			continue
		}
		// result、rateErr 用于本次流程后续判断的result、rateErr
		result, rateErr := c.client().RateBuyer(credential.requestContext, current, orderID, settings.RateContent)
		// alreadyRated 表示平台明确回答订单已评价；评价目标已达成，按成功收口，不再重试也不计为失败。
		alreadyRated := rateErr != nil && mtop.IsRateAlreadyRatedErr(rateErr)
		if !alreadyRated && (rateErr != nil || result == nil || !result.Success) {
			// persistErr 保存动作失败前已收到的响应 Cookie，避免平台已轮换凭证却被错误路径丢弃。
			_, persistErr := c.persistTaskCookieSession(ctx, settings.CookieID, current, "", &credential)
			// message 用于本次流程后续判断的消息
			message := errorString(rateErr)
			if result != nil && result.Message != "" {
				message = result.Message
			}
			// retryAt 保存失败运行记录的下一次自动执行时间；超过评价期限的订单属于永久失败，必须保持为零并排除后续抢占。
			retryAt := time.Now().UTC().Add(10 * time.Minute).Unix()
			if mtop.IsRateOrderExpiredErr(rateErr) || mtop.IsRateOrderNotRateableErr(rateErr) {
				message = db.NoRetryErrorPrefix + ": " + message
				retryAt = 0
			}
			// finishErr 保存失败结果的落库错误；失败时隔离运行记录，避免错误状态不明导致重放。
			finishErr := c.finishAccountTaskRun(ctx, runKey, "failed", 0, 1, message, retryAt)
			if finishErr != nil {
				return summary, errors.Join(rateErr, persistErr, finishErr)
			}
			summary.Failed++
			if persistErr != nil {
				return summary, errors.Join(rateErr, persistErr)
			}
			// Token 内部刷新耗尽后停止当前批次，避免继续用失效签名请求其它订单；不触发账号续期。
			if mtop.IsSessionExpiredErr(rateErr) || mtop.IsMTopTokenExpiredErr(rateErr) || mtop.IsRiskVerificationErr(rateErr) {
				return summary, rateErr
			}
			continue
		}
		// updatedCookies 保存评价接口返回的最新 Cookie；cookieErr 表示动作成功后同步 Cookie 的落库错误。
		// responseCookies 是评价响应携带的最新 Cookie；已评价错误可能没有结果对象，此时为空。
		responseCookies := ""
		if result != nil {
			responseCookies = result.UpdatedCookies
		}
		// updatedCookies、cookieErr 分别保存合并后的最新 Cookie 及其落库错误。
		updatedCookies, cookieErr := c.persistTaskCookieSession(ctx, settings.CookieID, current, responseCookies, &credential)
		if cookieErr != nil {
			return summary, c.quarantineAccountTaskRun(ctx, runKey, summary.Success+1, summary.Failed, cookieErr)
		}
		current = updatedCookies
		// successMessage 仅在平台提示已评价时记录原因，便于运行记录区分真实评价与历史已评价。
		successMessage := ""
		if alreadyRated {
			successMessage = "平台显示订单已评价，无需重复评价"
		}
		// finishErr 保存评价成功结果的落库错误；失败时会把运行记录隔离为人工核对。
		finishErr := c.finishAccountTaskRun(ctx, runKey, "success", 1, 0, successMessage, 0)
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
	// credential、err 保存本轮擦亮使用的 Cookie 会话及其读取错误。
	credential, err := c.openAccountTaskCredentialSession(ctx, settings.CookieID)
	if err != nil {
		return summary, errors.Join(err, c.finishAccountTaskRun(ctx, runKey, "failed", 0, 1, err.Error(), time.Now().UTC().Add(10*time.Minute).Unix()))
	}
	// items、err 保存每日擦亮使用的商品全集和平台查询错误；通用商品查询已统一识别成功但无 cardList 的空商品响应。
	items, err := c.client().FetchAllItems(credential.requestContext, credential.cookieValue, polishItemPageSize, polishItemMaxPages)
	if err != nil {
		// persistErr 保存商品列表请求失败前已吸收的响应 Cookie 写回错误。
		_, persistErr := c.persistTaskCookieSession(ctx, settings.CookieID, credential.cookieValue, "", &credential)
		return summary, errors.Join(err, persistErr, c.finishAccountTaskRun(ctx, runKey, "failed", 0, 1, err.Error(), time.Now().UTC().Add(10*time.Minute).Unix()))
	}
	// current 用于本次流程后续判断的current
	// current 保存擦亮接口返回前可继续使用的最新 Cookie。
	current, cookieErr := c.persistTaskCookieSession(ctx, settings.CookieID, credential.cookieValue, items.UpdatedCookies, &credential)
	if cookieErr != nil {
		return summary, errors.Join(cookieErr, c.quarantineAccountTaskRun(ctx, runKey, 0, 0, cookieErr))
	}
	summary.Found = len(items.Items)
	if summary.Found == 0 {
		// emptyMessage 说明商品列表查询成功但当前没有在售商品；这属于当日任务成功，不应重复请求平台。
		const emptyMessage = "商品列表查询成功，未发现在售商品，今日擦亮任务已完成"
		// completedAt 保存本次空列表成功结果对应的 UTC Unix 时间，供每日去重判断使用。
		completedAt := time.Now().UTC().Unix()
		// markErr 表示空列表成功态的当日完成标记写入失败；没有完成持久化时需要人工核对数据库状态。
		markErr := c.repository.MarkPolished(ctx, settings.CookieID, date, completedAt)
		if markErr != nil {
			// persistenceErr 为日期索引失败补充业务上下文，避免把内部落库失败伪装成平台空列表成功。
			persistenceErr := fmt.Errorf("保存商品擦亮日期: %w", markErr)
			return summary, errors.Join(persistenceErr, c.quarantineAccountTaskRun(ctx, runKey, 0, 0, persistenceErr))
		}
		c.logger.Info("每日擦亮完成，未发现在售商品", "account", settings.CookieID)
		// finishErr 是保存空列表成功运行状态时产生的数据库错误。
		finishErr := c.finishAccountTaskRun(ctx, runKey, "success", 0, 0, emptyMessage, 0)
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
		result, polishErr := c.client().PolishItem(credential.requestContext, current, item.ID)
		if polishErr != nil || result == nil || !result.Success {
			// persistErr 保存擦亮失败前已收到的响应 Cookie，避免错误路径丢弃平台轮换的凭证。
			_, persistErr := c.persistTaskCookieSession(ctx, settings.CookieID, current, "", &credential)
			summary.Failed++
			lastError = errorString(polishErr)
			if result != nil && result.Message != "" {
				lastError = result.Message
			}
			// Token 内部刷新耗尽或平台要求风控验证后立即停止当前批次，不再向平台发送后续请求。
			if mtop.IsSessionExpiredErr(polishErr) || mtop.IsMTopTokenExpiredErr(polishErr) || mtop.IsRiskVerificationErr(polishErr) {
				return summary, errors.Join(polishErr, persistErr, c.finishAccountTaskRun(ctx, runKey, "failed", summary.Success, summary.Failed, lastError, 0))
			}
			if persistErr != nil {
				// isolateErr 将平台失败与凭证落库失败后的运行记录收口到人工核对，避免运行永久停留在 running。
				isolateErr := c.quarantineAccountTaskRun(ctx, runKey, summary.Success, summary.Failed, persistErr)
				return summary, errors.Join(polishErr, persistErr, isolateErr)
			}
			continue
		}
		// updatedCookies 保存擦亮接口返回的最新 Cookie；cookieErr 表示外部动作成功后同步 Cookie 的落库错误。
		updatedCookies, cookieErr := c.persistTaskCookieSession(ctx, settings.CookieID, current, result.UpdatedCookies, &credential)
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

// persistTaskCookieSession 保存 credential 收到的响应；ctx 控制存储，账号和旧新值用于兼容客户端。
// 先在账号锁内校验凭证版本并写回，再在锁外通知运行时；冲突返回错误且不覆盖并发更新。
func (c *accountTaskCoordinator) persistTaskCookieSession(ctx context.Context, accountID, oldValue, newValue string, credential *accountTaskCredentialSession) (string, error) {
	// previousMetadata 记录写回前的作用域版本，只有属性变化时也必须通知运行时。
	previousMetadata := credential.persistedMetadata
	// value、err 保存锁内写回结果；运行时同步必须在锁已经释放后执行。
	value, err := c.persistTaskCredentialLocked(ctx, accountID, oldValue, newValue, credential)
	if err != nil {
		return oldValue, err
	}
	if (value != oldValue || credential.persistedMetadata != previousMetadata) && c.senders != nil {
		// sender、ok 保存当前在线运行时，回调可能重新申请同一账号凭证锁。
		if sender, ok := c.senders.Sender(accountID); ok && sender != nil {
			sender.UpdateCookie(value)
		}
	}
	return value, nil
}

// persistTaskCredentialLocked 以 credential 的最近持久化版本拒绝旧请求覆盖新登录；返回已保存值或不含凭证的错误。
// ctx 控制数据库调用，accountID 定位账号，oldValue/newValue 兼容没有主动更新会话的客户端。
func (c *accountTaskCoordinator) persistTaskCredentialLocked(ctx context.Context, accountID, oldValue, newValue string, credential *accountTaskCredentialSession) (string, error) {
	// value、snapshot、changed 保存本次请求会话内的响应更新，均不得写入日志。
	value, snapshot, changed := credential.cookieSession.State()
	if snapshot == nil && strings.TrimSpace(newValue) != "" && newValue != oldValue {
		value = strings.TrimSpace(newValue)
	}
	if !changed && (strings.TrimSpace(newValue) == "" || value == oldValue) {
		return oldValue, nil
	}
	// locker、ok 提供只覆盖本地校验与写回的账号凭证锁。
	if locker, ok := c.repository.(accountTaskCredentialLocker); ok {
		// unlock 必须在返回运行时通知层之前执行，禁止锁内调用发送器。
		unlock := locker.LockAccountCredentials(accountID)
		defer unlock()
	}
	// data、err 保存最新持久化凭证，用于比较请求开始或上次写回时的版本。
	data, err := c.repository.GetCookieRuntimeData(ctx, accountID)
	if err != nil {
		return oldValue, fmt.Errorf("读取账号任务 Cookie metadata: %w", err)
	}
	if data.Value != credential.persistedValue || data.MetadataJSON != credential.persistedMetadata {
		return oldValue, errors.New("账号凭证已被其他流程更新，已拒绝旧任务 Cookie 写回，请使用最新凭证重试")
	}
	// metadata 保留本次响应的完整作用域；扁平会话不伪造完整快照。
	metadata := cookierefresh.MetadataWithoutSnapshot(data.MetadataJSON)
	if snapshot != nil {
		metadata = cookierefresh.MetadataWithSnapshot(data.MetadataJSON, snapshot)
	}
	if value == data.Value && metadata == data.MetadataJSON {
		return data.Value, nil
	}
	// writer、ok 区分生产完整凭证仓储与旧测试仓储，二者都在相同版本校验后写回。
	if writer, ok := c.repository.(accountTaskCookieWriter); ok {
		err = writer.UpdateRenewalCookie(ctx, accountID, value, metadata, time.Now().Unix())
	} else {
		err = c.repository.UpdateValueExisting(ctx, accountID, value)
	}
	if err != nil {
		return oldValue, fmt.Errorf("保存账号任务 Cookie: %w", err)
	}
	credential.persistedValue, credential.persistedMetadata = value, metadata
	return value, nil
}

// persistTaskCookies 保存历史扁平 Cookie；仅在账号没有完整快照或测试替身不支持快照写回时使用。
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
