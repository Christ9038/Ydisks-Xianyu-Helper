package automation

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// automationActionExecutor 负责把动作计划转换为确认发货、卡密发送或消息发送，并集中维护凭证快照、卡券库存和外部发送结果的边界。
type automationActionExecutor struct {
	// store 提供订单、卡券、账号 Cookie 和自动化事实的持久化能力。
	store *db.Store
	// senders 提供账号当前在线的消息发送器。
	senders SenderProvider
	// mtop 返回构造期固定的 MTOP 客户端，外部调用期间不允许替换。
	mtop func() mtop.Client
	// recoverer 返回构造期固定的凭证恢复器。
	recoverer func() CredentialRecoverer
	// logger 记录确认发货和本地状态持久化异常。
	logger *slog.Logger
	// cookieSource 读取构造期注入或仓储提供的账号 Cookie。
	cookieSource func(context.Context, string) (string, error)
	// wakeCredentialBlocked 在 Cookie 更新后唤醒凭证阻塞的自动化任务。
	wakeCredentialBlocked func(context.Context, string)
	// apiFetcher 为 API 卡发货提供外部请求能力；业务执行器不直接依赖 net/http。
	apiFetcher func() APICardFetcher
	// cardLocks 串行化同一卡密组的数据库存取和消费。
	cardLocks sync.Map
}

// shipmentConsignSession 保存一次交易类 MTOP 请求（确认发货、订单改价共用）开始前固定的凭证视图和响应 Cookie 会话；Cookie 明文只在本次 MTOP 调用及条件写回期间存在，禁止记录到日志或任务快照。
type shipmentConsignSession struct {
	// requestContext 是携带完整 Cookie Jar 或扁平 Cookie 的 MTOP 请求上下文。
	requestContext context.Context
	// cookieSession 汇集 MTOP 响应的 Set-Cookie，供请求完成后条件写回。
	cookieSession *mtop.CookieSession
	// cookieStr 是本次 MTOP 调用使用的扁平 Cookie，禁止向任务持久化或日志传播。
	cookieStr string
	// credentialFingerprint 用于拒绝覆盖请求期间被并发更新的账号凭证。
	credentialFingerprint string
}

// shipmentConsignResult 保存交易类 MTOP 调用（Consign、订单改价共用）的业务结果、响应 Cookie 和传输错误，供后续会话状态与订单事实分别收口。
type shipmentConsignResult struct {
	// succeeded 表示平台明确确认订单已发货。
	succeeded bool
	// returns 保存平台返回的业务错误文本。
	returns []string
	// updatedCookie 是无完整 Cookie Jar 时平台返回的扁平 Cookie 更新值。
	updatedCookie string
	// callErr 表示 MTOP 调用或响应解码失败，发生时不能假定远端未执行动作。
	callErr error
}

// shipmentCookiePersistence 保存响应 Cookie 的条件写回结果；errors 中的失败需要与远端动作结果一并向调用方报告。
type shipmentCookiePersistence struct {
	// errors 收集读取、冲突检测或写回账号凭证时的本地持久化错误。
	errors []error
	// runtimeCookie 是已成功持久化、需要同步给在线发送器的最新扁平 Cookie。
	runtimeCookie string
	// changed 表示 runtimeCookie 已变化且可安全通知在线运行时。
	changed bool
}

// executeAction 执行一个动作，并把消息发送错误分类为可安全重试或结果未知。
func (e *automationActionExecutor) executeAction(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	switch action.ActionType {
	case ActionConfirmShipment:
		return 0, e.confirmShipment(ctx, task)
	case ActionAdjustPrice:
		return e.adjustOrderPrice(ctx, task, action)
	case ActionSendCard:
		return e.sendCard(ctx, task, action)
	case ActionSendTemplate:
		return e.sendTemplate(ctx, task, action)
	case ActionSendText:
		// text 是渲染后的文字动作内容。
		text := renderTemplate(action.MessageTemplate, task)
		if strings.TrimSpace(text) == "" {
			return 0, nil
		}
		// sendErr 保存文字消息发送错误。
		if sendErr := e.sendText(ctx, task, text); sendErr != nil {
			if errors.Is(sendErr, ErrMessageNotSent) {
				return 0, sendErr
			}
			return 0, uncertainAction(sendErr)
		}
		return 1, nil
	default:
		return 0, fmt.Errorf("未知自动化动作: %s", action.ActionType)
	}
}

// confirmShipment 根据账号设置和任务强制标记决定是否确认订单发货。
func (e *automationActionExecutor) confirmShipment(ctx context.Context, task Task) error {
	if task.OrderID == "" {
		return fmt.Errorf("确认发货缺少订单ID")
	}
	// enabled 表示账号是否打开自动确认发货设置；readErr 表示读取该账号设置时的数据库错误。
	enabled, readErr := e.store.Cookies.GetAutoConfirm(ctx, task.AccountID)
	if readErr != nil {
		return fmt.Errorf("读取自动确认发货设置: %w", readErr)
	}
	if !enabled && !task.ForceConfirmShipment {
		return nil
	}
	return e.confirmShipmentAttempt(ctx, task, true)
}

// confirmShipmentAttempt 使用凭证快照调用 Consign，并以指纹条件写回 Cookie；远端成功但订单事实落库失败时创建可重试补偿记录。
func (e *automationActionExecutor) confirmShipmentAttempt(ctx context.Context, task Task, allowCredentialRecovery bool) error {
	// session 固定本次 MTOP 请求的最小凭证视图，外部调用期间不持有账号凭证锁。
	session, err := e.openShipmentConsignSession(ctx, task.AccountID)
	if err != nil {
		return err
	}
	// succeeded、returns、updatedCookie、callErr 分别保存 MTOP 的业务成功标记、业务返回、扁平 Cookie 更新和调用错误。
	succeeded, returns, updatedCookie, callErr := e.mtop().ConsignContext(session.requestContext, session.cookieStr, task.OrderID)
	// result 归并 MTOP 远端结果，Cookie 写回与订单状态写入随后独立处理。
	result := shipmentConsignResult{succeeded: succeeded, returns: returns, updatedCookie: updatedCookie, callErr: callErr}
	// cookiePersistence 收集响应 Cookie 的条件写回结果，并在锁外同步在线运行时。
	cookiePersistence := e.persistShipmentConsignCookies(ctx, task.AccountID, session, result)
	// persistenceErrs 收集所有本地写入失败；远端动作成功时会用于决定是否进入人工核对。
	persistenceErrs := cookiePersistence.errors
	// sessionErr 将请求层错误和远端业务失败统一为凭证状态判断输入。
	sessionErr := result.callErr
	if sessionErr == nil && !result.succeeded {
		sessionErr = errors.New(strings.Join(result.returns, "; "))
	}
	if mtop.IsSessionExpiredErr(sessionErr) {
		if len(persistenceErrs) > 0 {
			return errors.Join(fmt.Errorf("确认发货 Session 已失效: %w", sessionErr), errors.Join(persistenceErrs...))
		}
		// recoverer 是当前生效的凭证恢复器快照，避免一次判断期间被替换两次。
		recoverer := e.recoverer()
		if allowCredentialRecovery && recoverer != nil && recoverer.RecoverExpiredCredential(ctx, task.AccountID) {
			e.logger.Info("确认发货凭证恢复成功，重新执行确认发货", "account", task.AccountID, "order_id", task.OrderID)
			return e.confirmShipmentAttempt(ctx, task, false)
		}
		if !allowCredentialRecovery {
			return fmt.Errorf("%w: 确认发货在凭证恢复后仍返回 Session 失效: %v", errActionNotPerformed, sessionErr)
		}
		return fmt.Errorf("%w: 确认发货 Session 已失效且凭证恢复失败: %v", errActionNotPerformed, sessionErr)
	}
	if result.callErr != nil {
		if len(persistenceErrs) > 0 {
			result.callErr = errors.Join(result.callErr, errors.Join(persistenceErrs...))
		}
		return uncertainAction(result.callErr)
	}
	if !result.succeeded {
		if consignAlreadyDelivered(result.returns) {
			e.logger.Info("订单已处于发货状态，确认发货按幂等成功处理", "account", task.AccountID, "order_id", task.OrderID)
		} else {
			// failure 是远端拒绝确认发货的业务错误。
			failure := fmt.Errorf("确认发货失败: %s", strings.Join(result.returns, "; "))
			if len(persistenceErrs) > 0 {
				return errors.Join(failure, errors.Join(persistenceErrs...))
			}
			return failure
		}
	}
	// orderPersistence 保存远端成功后本地订单状态和补偿记录的写入结果。
	orderPersistence := e.persistConfirmedShipment(ctx, task)
	persistenceErrs = append(persistenceErrs, orderPersistence...)
	if len(persistenceErrs) > 0 {
		return uncertainAction(fmt.Errorf("闲鱼已确认发货，但本地状态保存失败: %w", errors.Join(persistenceErrs...)))
	}
	return nil
}

// adjustPriceTransientRetryLimit 是平台明确提示暂时无法改价时允许的最大请求次数，避免短暂订单状态同步延迟直接导致自动化失败。
const adjustPriceTransientRetryLimit = 5

// adjustPriceTransientRetryGap 是相邻两次暂时性改价失败之间的等待时间；测试可临时缩短它以验证重试分支。
var adjustPriceTransientRetryGap = 3 * time.Second

// adjustOrderPrice 把买家已拍下未付款订单的价格修改为动作配置的目标价格。
// 成功返回 1 作为外部结果数量，供运行统计与结果通知使用。
func (e *automationActionExecutor) adjustOrderPrice(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	if task.OrderID == "" {
		return 0, fmt.Errorf("%w: 订单改价缺少订单ID", errActionNotPerformed)
	}
	// priceCents 是目标价格的整数分表示；parseErr 表示动作配置缺失或金额格式非法。
	priceCents, parseErr := adjustPriceCentsFromConfig(action.ConfigJSON)
	if parseErr != nil {
		return 0, fmt.Errorf("%w: %v", errActionNotPerformed, parseErr)
	}
	// adjustErr 表示包含暂时性平台繁忙重试后的最终改价结果。
	if adjustErr := e.adjustOrderPriceWithRetry(ctx, task, priceCents); adjustErr != nil {
		return 0, adjustErr
	}
	return 1, nil
}

// adjustOrderPriceWithRetry 为规则改价和 AI 报价共用平台暂忙重试；传输结果未知和明确业务拒绝均不得自动重放。
func (e *automationActionExecutor) adjustOrderPriceWithRetry(ctx context.Context, task Task, priceCents int64) error {
	// attempt 是当前已发送的改价请求次数，最多执行预设次数以避免无限占用自动化工作线程。
	for attempt := 1; attempt <= adjustPriceTransientRetryLimit; attempt++ {
		// attemptErr 是本次请求、凭证恢复和 Cookie 条件写回后的最终结果。
		attemptErr := e.adjustOrderPriceAttempt(ctx, task, priceCents, true)
		if attemptErr == nil {
			return nil
		}
		// uncertain 表示请求可能已被平台执行，重复提交会造成价格状态无法判定，必须交由人工核对。
		var uncertain *uncertainActionError
		if errors.As(attemptErr, &uncertain) || !isAdjustPriceTransientBusy(attemptErr) || attempt == adjustPriceTransientRetryLimit {
			return attemptErr
		}
		e.logger.Info("订单改价暂时不可用，稍后重试", "account", task.AccountID, "order_id", task.OrderID, "attempt", attempt, "limit", adjustPriceTransientRetryLimit)
		// retryTimer 控制下一次平台请求的最小间隔，并在上下文取消时立即释放本次自动化任务。
		retryTimer := time.NewTimer(adjustPriceTransientRetryGap)
		select {
		case <-ctx.Done():
			if !retryTimer.Stop() {
				select {
				case <-retryTimer.C:
				default:
				}
			}
			return fmt.Errorf("%w: 订单改价重试等待被取消: %v", errActionNotPerformed, ctx.Err())
		case <-retryTimer.C:
		}
	}
	return fmt.Errorf("%w: 订单改价重试次数耗尽", errActionNotPerformed)
}

// isAdjustPriceTransientBusy 判断平台是否明确返回订单状态尚未同步完成的暂时性改价失败。
func isAdjustPriceTransientBusy(err error) bool {
	if err == nil {
		return false
	}
	// message 是不含凭证明文的远端业务错误文本，只匹配已验证的暂时性返回而不重试订单关闭等终态错误。
	message := err.Error()
	return strings.Contains(message, "CANNOT_MODIFY_FEE") || strings.Contains(message, "稍后重试") || strings.Contains(message, "稍后再试")
}

// adjustOrderPriceAttempt 使用凭证快照调用订单改价，并以指纹条件写回响应 Cookie；Session 失效时最多执行一次凭证恢复后重试。
func (e *automationActionExecutor) adjustOrderPriceAttempt(ctx context.Context, task Task, priceCents int64, allowCredentialRecovery bool) error {
	// session 固定本次 MTOP 请求的最小凭证视图，外部调用期间不持有账号凭证锁。
	session, err := e.openShipmentConsignSession(ctx, task.AccountID)
	if err != nil {
		return err
	}
	// succeeded、returns、updatedCookie、callErr 分别是改价的业务成功标记、业务返回、扁平 Cookie 更新和调用错误。
	succeeded, returns, updatedCookie, callErr := e.mtop().AdjustOrderPriceContext(session.requestContext, session.cookieStr, task.OrderID, priceCents)
	// result 归并 MTOP 远端结果，Cookie 写回随后独立处理。
	result := shipmentConsignResult{succeeded: succeeded, returns: returns, updatedCookie: updatedCookie, callErr: callErr}
	// cookiePersistence 收集响应 Cookie 的条件写回结果，并在锁外同步在线运行时。
	cookiePersistence := e.persistShipmentConsignCookies(ctx, task.AccountID, session, result)
	// persistenceErrs 收集本地 Cookie 写回失败；改价结果本身不依赖本地事实写入。
	persistenceErrs := cookiePersistence.errors
	// sessionErr 将请求层错误和远端业务失败统一为凭证状态判断输入。
	sessionErr := result.callErr
	if sessionErr == nil && !result.succeeded {
		sessionErr = errors.New(strings.Join(result.returns, "; "))
	}
	if mtop.IsSessionExpiredErr(sessionErr) {
		if len(persistenceErrs) > 0 {
			return errors.Join(fmt.Errorf("订单改价 Session 已失效: %w", sessionErr), errors.Join(persistenceErrs...))
		}
		// recoverer 是当前生效的凭证恢复器快照，避免一次判断期间被替换两次。
		recoverer := e.recoverer()
		if allowCredentialRecovery && recoverer != nil && recoverer.RecoverExpiredCredential(ctx, task.AccountID) {
			e.logger.Info("订单改价凭证恢复成功，重新执行改价", "account", task.AccountID, "order_id", task.OrderID)
			return e.adjustOrderPriceAttempt(ctx, task, priceCents, false)
		}
		if !allowCredentialRecovery {
			return fmt.Errorf("%w: 订单改价在凭证恢复后仍返回 Session 失效: %v", errActionNotPerformed, sessionErr)
		}
		return fmt.Errorf("%w: 订单改价 Session 已失效且凭证恢复失败: %v", errActionNotPerformed, sessionErr)
	}
	if result.callErr != nil {
		if len(persistenceErrs) > 0 {
			result.callErr = errors.Join(result.callErr, errors.Join(persistenceErrs...))
		}
		return uncertainAction(result.callErr)
	}
	if !result.succeeded {
		// failure 是远端拒绝改价的业务错误，例如订单已付款或已关闭。
		failure := fmt.Errorf("订单改价失败: %s", strings.Join(result.returns, "; "))
		if len(persistenceErrs) > 0 {
			return errors.Join(failure, errors.Join(persistenceErrs...))
		}
		return failure
	}
	if len(persistenceErrs) > 0 {
		return uncertainAction(fmt.Errorf("闲鱼已完成订单改价，但响应凭证保存失败: %w", errors.Join(persistenceErrs...)))
	}
	return nil
}

// adjustPriceCentsFromConfig 从动作配置解析目标价格并转换为整数分。
// 金额使用十进制字符串解析，最多两位小数，禁止浮点运算引入误差。
func adjustPriceCentsFromConfig(configJSON string) (int64, error) {
	// cfg 保存改价动作的目标价格配置；target_price 以元为单位的字符串。
	var cfg struct {
		TargetPrice string `json:"target_price"`
	}
	if // err 是动作配置 JSON 的解析错误。
	err := json.Unmarshal([]byte(configJSON), &cfg); err != nil {
		return 0, fmt.Errorf("解析改价动作配置: %w", err)
	}
	return parseYuanToCents(cfg.TargetPrice)
}

// parseYuanToCents 把以元为单位的十进制金额文本转换为整数分。
// 允许 0.01 到 1000000 元、至多两位小数；非法格式返回错误。
func parseYuanToCents(raw string) (int64, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, errors.New("改价动作缺少目标价格")
	}
	// wholeText、fracText 分别是金额的整数部分与小数部分文本。
	wholeText, fracText := raw, ""
	if // dot 是小数点在金额文本中的位置。
	dot := strings.IndexByte(raw, '.'); dot >= 0 {
		wholeText, fracText = raw[:dot], raw[dot+1:]
	}
	if wholeText == "" || len(fracText) > 2 {
		return 0, fmt.Errorf("目标价格格式非法: %q", raw)
	}
	// whole、wholeErr 分别是整数元部分的数值和解析错误。
	whole, wholeErr := strconv.ParseInt(wholeText, 10, 64)
	if wholeErr != nil || whole < 0 {
		return 0, fmt.Errorf("目标价格格式非法: %q", raw)
	}
	// frac 是小数部分折算出的分值。
	frac := int64(0)
	if fracText != "" {
		// fracValue、fracErr 分别是小数部分的数值和解析错误。
		fracValue, fracErr := strconv.ParseInt(fracText, 10, 64)
		if fracErr != nil || fracValue < 0 {
			return 0, fmt.Errorf("目标价格格式非法: %q", raw)
		}
		frac = fracValue
		if len(fracText) == 1 {
			frac *= 10
		}
	}
	// cents 是目标价格的整数分结果。
	cents := whole*100 + frac
	if cents <= 0 || cents > 100000000 {
		return 0, fmt.Errorf("目标价格必须在 0.01 到 1000000 元之间: %q", raw)
	}
	return cents, nil
}

// openShipmentConsignSession 在账号凭证锁内读取最小运行时视图，再在锁外构造 MTOP 会话；返回的 Cookie 只供当前确认发货请求使用。
func (e *automationActionExecutor) openShipmentConsignSession(ctx context.Context, accountID string) (shipmentConsignSession, error) {
	// unlock 释放账号凭证锁；锁仅保护运行时 Cookie 和 metadata 的读取。
	unlock := e.store.LockAccountCredentials(accountID)
	// runtimeData 是确认发货所需的最小 Cookie 与 metadata 运行视图，不包含登录密码和账号资料。
	runtimeData, readErr := e.store.Cookies.GetCookieRuntimeData(ctx, accountID)
	unlock()
	if readErr != nil {
		return shipmentConsignSession{}, readErr
	}
	// snapshot 与 snapshotOK 分别表示 metadata 中的完整 Cookie Jar 及其可用性。
	snapshot, snapshotOK := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON)
	if strings.TrimSpace(runtimeData.Value) == "" && !snapshotOK {
		return shipmentConsignSession{}, fmt.Errorf("账号 %s Cookie 为空", accountID)
	}
	// session 固定请求上下文和响应 Cookie 会话，后续持久化通过指纹检测并发更新。
	session := shipmentConsignSession{
		cookieStr:             runtimeData.Value,
		credentialFingerprint: credentialRuntimeFingerprint(runtimeData),
	}
	if snapshotOK {
		session.requestContext, session.cookieSession = mtop.WithCookieSnapshot(ctx, snapshot)
	} else {
		session.requestContext, session.cookieSession = mtop.WithFlatCookieSession(ctx, session.cookieStr)
	}
	return session, nil
}

// persistShipmentConsignCookies 条件写入确认发货响应产生的 Cookie，并在账号凭证锁外同步在线发送器和等待中的自动化任务。
func (e *automationActionExecutor) persistShipmentConsignCookies(ctx context.Context, accountID string, session shipmentConsignSession, result shipmentConsignResult) shipmentCookiePersistence {
	// value、snapshot、changed 分别是响应会话合并后的扁平 Cookie、完整 Jar 和变更标记。
	value, snapshot, changed := session.cookieSession.State()
	// sessionHandled 表示当前请求由完整 Cookie Jar 处理，避免历史扁平值覆盖权威快照。
	sessionHandled := snapshot != nil
	// responseChanged 表示需要检查并持久化来自 MTOP 响应的 Cookie 状态。
	responseChanged := changed || (!sessionHandled && result.callErr == nil && result.updatedCookie != "" && result.updatedCookie != session.cookieStr)
	if !responseChanged {
		return shipmentCookiePersistence{}
	}
	// unlock 保护重新读取凭证和条件写回；外部 MTOP I/O 已经结束。
	unlock := e.store.LockAccountCredentials(accountID)
	// currentData 是写回前重新读取的最新最小凭证视图。
	currentData, currentErr := e.store.Cookies.GetCookieRuntimeData(ctx, accountID)
	// persistence 保存锁内检查和写回的结果，锁释放后再同步运行时。
	persistence := shipmentCookiePersistence{}
	if currentErr != nil {
		persistence.errors = append(persistence.errors, fmt.Errorf("读取确认发货最新 Cookie: %w", currentErr))
	} else if credentialRuntimeFingerprint(currentData) != session.credentialFingerprint {
		persistence.errors = append(persistence.errors, errors.New("确认发货响应 Cookie 与并发更新冲突，已跳过旧响应写回"))
	} else if changed {
		// metadata 保留非快照 metadata，仅在响应有权威 Jar 时写入新快照。
		metadata := cookierefresh.MetadataWithoutSnapshot(currentData.MetadataJSON)
		if snapshot != nil {
			metadata = cookierefresh.MetadataWithSnapshot(currentData.MetadataJSON, snapshot)
		}
		// saveErr 表示权威 Cookie Jar 写入失败。
		if saveErr := e.store.Cookies.UpdateRenewalCookie(ctx, accountID, value, metadata, time.Now().Unix()); saveErr != nil {
			persistence.errors = append(persistence.errors, fmt.Errorf("保存确认发货响应 Cookie Jar: %w", saveErr))
		} else if value != currentData.Value {
			persistence.runtimeCookie, persistence.changed = value, true
		}
	} else if result.callErr == nil && result.updatedCookie != "" && result.updatedCookie != currentData.Value {
		// 历史账号没有权威 Jar 时兼容扁平 Cookie 写回，并清除可能过期的旧 Jar。
		metadata := cookierefresh.MetadataWithoutSnapshot(currentData.MetadataJSON)
		// saveErr 表示扁平 Cookie 写入失败。
		if saveErr := e.store.Cookies.UpdateRenewalCookie(ctx, accountID, result.updatedCookie, metadata, time.Now().Unix()); saveErr != nil {
			persistence.errors = append(persistence.errors, fmt.Errorf("保存刷新后的 Cookie: %w", saveErr))
		} else {
			persistence.runtimeCookie, persistence.changed = result.updatedCookie, true
		}
	}
	unlock()
	if !persistence.changed {
		return persistence
	}
	if e.senders != nil {
		// sender 与 running 分别是当前账号在线发送器和其运行状态；同步发生在凭证锁外。
		if sender, running := e.senders.Sender(accountID); running {
			sender.UpdateCookie(persistence.runtimeCookie)
		}
	}
	e.wakeCredentialBlocked(ctx, accountID)
	return persistence
}

// persistConfirmedShipment 写入远端已确认发货的订单事实；任一事实写入失败时创建幂等补偿记录并把所有错误返回调用方。
func (e *automationActionExecutor) persistConfirmedShipment(ctx context.Context, task Task) []error {
	// persistenceErrs 收集订单状态、发货时间和补偿记录的本地写入错误。
	var persistenceErrs []error
	// sysShip 表示本地订单已由系统确认发货。
	sysShip := true
	// upsertErr 表示本地订单发货状态写入失败。
	if upsertErr := e.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID:      task.AccountID,
		ItemID:        task.ItemID,
		BuyerID:       task.BuyerID,
		ChatID:        task.ChatID,
		OrderStatus:   "shipped",
		SystemShipped: &sysShip,
	}); upsertErr != nil {
		persistenceErrs = append(persistenceErrs, fmt.Errorf("保存本地订单发货状态: %w", upsertErr))
	}
	// eventErr 表示订单发货事实时间写入失败。
	if eventErr := e.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "shipped_at"); eventErr != nil {
		persistenceErrs = append(persistenceErrs, fmt.Errorf("保存订单发货时间: %w", eventErr))
	}
	if len(persistenceErrs) == 0 {
		return nil
	}
	if e.store == nil || e.store.Reconciliations == nil {
		return append(persistenceErrs, errors.New("创建订单发货补偿记录: 补偿存储未初始化"))
	}
	// reconciliationID 是幂等补偿记录的追踪标识；reconciliationErr 表示创建记录时的数据库错误。
	reconciliationID, reconciliationErr := e.store.Reconciliations.CreatePending(
		ctx,
		task.OrderID,
		task.AccountID,
		"manual_status_ship",
		"闲鱼已确认发货，但本地订单状态写入失败: "+errors.Join(persistenceErrs...).Error(),
	)
	if reconciliationErr != nil {
		return append(persistenceErrs, fmt.Errorf("创建订单发货补偿记录: %w", reconciliationErr))
	}
	return append(persistenceErrs, fmt.Errorf("订单发货补偿记录 %s 已创建", reconciliationID))
}

// consignAlreadyDelivered 判断平台是否明确表示订单已发货；重复确认应按幂等成功处理并补写本地发货事实。
func consignAlreadyDelivered(returns []string) bool {
	// returned 是当前平台业务返回文本，只匹配稳定错误码，避免宽泛中文文案误判为已发货。
	for _, returned := range returns {
		if strings.Contains(returned, "ORDER_ALREADY_DELIVERY") {
			return true
		}
	}
	return false
}

// sendCard 按规格和购买数量分配文本、图片或数据卡密。
func (e *automationActionExecutor) sendCard(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	if !actionMatchesOrderSpec(task, action) {
		return 0, nil
	}
	if action.CardID <= 0 {
		return 0, fmt.Errorf("发送卡密动作缺少卡密组ID")
	}
	// count 是当前订单需要发送的卡密数量。
	count := deliverySendCount(task, action)
	// card 是待发送的卡密组完整配置。
	card, err := e.store.Cards.GetForDelivery(ctx, action.CardID)
	if err != nil {
		return 0, err
	}
	if !card.Enabled {
		return 0, fmt.Errorf("卡密组 %d 已停用", card.ID)
	}
	if card.Type == "data" {
		return e.sendDataCard(ctx, task, card, count)
	}
	if card.Type == "api" {
		return e.sendAPICard(ctx, task, action, card, count)
	}
	// sent 是已经成功发送的卡密数量。
	sent := 0
	// i 表示当前卡密发送序号。
	for i := 0; i < count; i++ {
		// content、imageURL、readErr 分别是当前卡密组可发送的文本、图片地址和读取配置失败原因。
		content, imageURL, readErr := e.cardContent(ctx, card)
		if readErr != nil {
			return sent, readErr
		}
		if imageURL != "" {
			// sendErr 保存图片消息发送错误。
			if sendErr := e.sendImage(ctx, task, imageURL, card.ID); sendErr != nil {
				return sent, classifyMessageSendError(sendErr)
			}
		}
		if strings.TrimSpace(content) != "" {
			// sendErr 保存文字消息发送错误。
			if sendErr := e.sendText(ctx, task, renderTemplate(content, task)); sendErr != nil {
				return sent, classifyMessageSendError(sendErr)
			}
		}
		if strings.TrimSpace(content) == "" && strings.TrimSpace(imageURL) == "" {
			return sent, fmt.Errorf("卡密组 %d 没有可发送内容", card.ID)
		}
		sent++
	}
	return sent, nil
}

// sendAPICard 按发货单位逐次调用普通 API，并在消息发送失败时保留结果未知状态。
func (e *automationActionExecutor) sendAPICard(ctx context.Context, task Task, action db.AutomationAction, card *db.CardFull, count int) (int, error) {
	// fetcher 是构造期固定的 API 卡发货客户端。
	var fetcher APICardFetcher
	if e.apiFetcher != nil {
		fetcher = e.apiFetcher()
	}
	if fetcher == nil {
		return 0, errors.New("API 卡发货客户端未初始化")
	}
	// sent 是已经完成 API 获取和买家消息发送的单位数量。
	sent := 0
	// unitIndex 表示从 1 开始的当前 API 发货单位序号。
	for unitIndex := 1; unitIndex <= count; unitIndex++ {
		// result、fetchErr 保存当前单位的 API 响应与请求错误。
		result, fetchErr := fetcher.Fetch(ctx, APICardRequest{
			Config: card.APIConfig, TriggerKey: buildTriggerKey(task), ActionID: action.ID, CardID: card.ID,
			UnitIndex: unitIndex, TotalUnits: count, AccountID: task.AccountID, OrderID: task.OrderID,
			ItemID: task.ItemID, BuyerID: task.BuyerID, ChatID: task.ChatID, SpecName: task.SpecName,
			SpecValue: task.SpecValue, Quantity: task.Quantity, Amount: task.Amount, TriggerType: task.TriggerType,
		})
		if fetchErr != nil {
			if result.Dispatched {
				return sent, uncertainAction(fetchErr)
			}
			return sent, noRetryAction(fetchErr)
		}
		if strings.TrimSpace(result.Content) == "" {
			return sent, uncertainAction(errors.New("API 卡发货响应没有可发送内容"))
		}
		// sendErr 保存已取得卡密后向买家发送消息的结果；此时失败不能安全重放 API 请求。
		if sendErr := e.sendText(ctx, task, result.Content); sendErr != nil {
			return sent, uncertainAction(sendErr)
		}
		sent++
	}
	return sent, nil
}

// sendDataCard 只在卡券锁内完成库存预留与恢复，把外部消息发送放到锁外。
func (e *automationActionExecutor) sendDataCard(ctx context.Context, task Task, card *db.CardFull, count int) (int, error) {
	// sent 是已经成功发送的数据卡密数量。
	sent := 0
	// i 表示当前数据卡密消费序号。
	for i := 0; i < count; i++ {
		// unlock 释放当前卡密组的并发消费锁；锁只覆盖本地库存操作。
		unlock := e.lockCard(card.ID)
		// content 是从库存中原子消费出的数据卡密。
		content, err := e.store.Cards.ConsumeBatchData(ctx, card.ID)
		unlock()
		if err != nil {
			return sent, err
		}
		if strings.TrimSpace(content) != "" {
			// sendErr 保存数据卡密消息发送错误。
			if sendErr := e.sendText(ctx, task, renderTemplate(content, task)); sendErr != nil {
				if errors.Is(sendErr, ErrMessageNotSent) {
					// restoreUnlock 释放恢复库存所需的卡密组锁。
					restoreUnlock := e.lockCard(card.ID)
					// restoreErr 保存确定未发送时恢复库存的错误。
					restoreErr := e.store.Cards.RestoreBatchData(ctx, card.ID, content)
					restoreUnlock()
					if restoreErr != nil {
						return sent, uncertainAction(errors.Join(sendErr, fmt.Errorf("恢复未发送卡密库存: %w", restoreErr)))
					}
					return sent, sendErr
				}
				// 请求已交给传输层后无法判断远端是否收到，保留消费状态并人工核对。
				return sent, uncertainAction(sendErr)
			}
		}
		sent++
	}
	return sent, nil
}

// sendText 向账号在线发送器发送文字消息，并保留确定未发送的错误标记。
func (e *automationActionExecutor) sendText(ctx context.Context, task Task, text string) error {
	if task.ChatID == "" || task.BuyerID == "" {
		return fmt.Errorf("%w: 发送消息缺少 chat_id 或 buyer_id", ErrMessageNotSent)
	}
	if e.senders == nil {
		return fmt.Errorf("%w: 账号发送器未初始化", ErrMessageNotSent)
	}
	// sender、senderOK 分别是当前账号的在线消息发送器及其可用标记，账号离线时必须返回确定未发送错误。
	sender, senderOK := e.senders.Sender(task.AccountID)
	if !senderOK {
		return fmt.Errorf("%w: 账号未在线，无法发送自动化消息", ErrMessageNotSent)
	}
	return sender.SendText(ctx, task.ChatID, task.BuyerID, text)
}

// sendImage 向账号在线发送器发送图片消息，并标记关联卡密组。
func (e *automationActionExecutor) sendImage(ctx context.Context, task Task, imageURL string, cardID int64) error {
	if task.ChatID == "" || task.BuyerID == "" {
		return fmt.Errorf("%w: 发送图片缺少 chat_id 或 buyer_id", ErrMessageNotSent)
	}
	if e.senders == nil {
		return fmt.Errorf("%w: 账号发送器未初始化", ErrMessageNotSent)
	}
	// sender、senderOK 分别是当前账号的在线图片发送器及其可用标记，账号离线时必须返回确定未发送错误。
	sender, senderOK := e.senders.Sender(task.AccountID)
	if !senderOK {
		return fmt.Errorf("%w: 账号未在线，无法发送自动化图片", ErrMessageNotSent)
	}
	return sender.SendImage(ctx, task.ChatID, task.BuyerID, imageURL, cardID, 0, 0)
}

// cardContent 读取卡密组内容并拒绝不支持的卡密类型。
func (e *automationActionExecutor) cardContent(_ context.Context, card *db.CardFull) (text, imageURL string, err error) {
	switch card.Type {
	case "text":
		if strings.TrimSpace(card.TextContent) == "" {
			return "", "", fmt.Errorf("文本卡密组缺少内容")
		}
		return card.TextContent, "", nil
	case "data":
		return "", "", fmt.Errorf("data 卡密必须通过 sendDataCard 发送")
	case "image":
		if strings.TrimSpace(card.ImageURL) == "" {
			return "", "", fmt.Errorf("图片卡密组缺少图片 URL")
		}
		return "", card.ImageURL, nil
	case "api":
		return "", "", fmt.Errorf("API 卡密必须通过 sendAPICard 发送")
	default:
		return "", "", fmt.Errorf("未知卡密类型: %s", card.Type)
	}
}

// cookieValue 读取账号 Cookie，优先使用测试或运行时覆盖的数据源。
func (e *automationActionExecutor) cookieValue(ctx context.Context, cookieID string) (string, error) {
	return e.cookieSource(ctx, cookieID)
}

// lockCard 获取指定卡密组的进程内互斥锁。
func (e *automationActionExecutor) lockCard(cardID int64) func() {
	// raw 是按卡密组 ID 保存的锁实例。
	raw, _ := e.cardLocks.LoadOrStore(cardID, &sync.Mutex{})
	// mu 是当前卡密组的互斥锁。
	mu := raw.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

// credentialRuntimeFingerprint 生成不暴露凭证内容的运行时指纹，用于条件写回冲突检测。
func credentialRuntimeFingerprint(data db.CookieRuntimeData) string {
	// sum 是 Cookie 与 metadata 拼接后的稳定摘要，不会被写入日志或响应。
	sum := sha256.Sum256([]byte(data.Value + "\x00" + data.MetadataJSON))
	return fmt.Sprintf("%x", sum[:])
}

// actionMatchesOrderSpec 判断动作配置的规格是否匹配订单。
func actionMatchesOrderSpec(task Task, action db.AutomationAction) bool {
	// cfg 保存卡密动作的规格过滤配置。
	var cfg struct {
		SpecName  string `json:"spec_name"`
		SpecValue string `json:"spec_value"`
	}
	if json.Unmarshal([]byte(action.ConfigJSON), &cfg) != nil {
		return false
	}
	if strings.TrimSpace(cfg.SpecName) == "" && strings.TrimSpace(cfg.SpecValue) == "" {
		return true
	}
	return strings.TrimSpace(task.SpecName) == strings.TrimSpace(cfg.SpecName) &&
		strings.TrimSpace(task.SpecValue) == strings.TrimSpace(cfg.SpecValue)
}

// deliverySendCount 根据动作单份数量和订单购买数量计算实际发送数量。
func deliverySendCount(task Task, action db.AutomationAction) int {
	// perUnit 是每个购买单位对应的卡密数量。
	perUnit := action.DeliveryCount
	if perUnit <= 0 {
		perUnit = 1
	}
	// qty 是订单购买数量。
	qty := parsePositiveInt(task.Quantity)
	if qty <= 0 {
		qty = 1
	}
	return perUnit * qty
}

// parsePositiveInt 将数量文本解析为非负整数，非法值返回零。
func parsePositiveInt(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	// n 是解析后的数量值；parseErr 表示原始数量文本不符合十进制整数格式。
	n, parseErr := strconv.Atoi(raw)
	if parseErr != nil || n < 0 {
		return 0
	}
	return n
}

// classifyMessageSendError 将消息发送错误统一分类为确定未发送或结果未知。
func classifyMessageSendError(err error) error {
	if err == nil || errors.Is(err, ErrMessageNotSent) {
		return err
	}
	return uncertainAction(err)
}

// renderTemplate 替换自动化消息模板中的订单字段。
func renderTemplate(tpl string, task Task) string {
	// out 是经过字段替换后的模板文本。
	out := tpl
	// repl 保存模板占位符与订单字段的映射。
	repl := map[string]string{
		"{order_id}":     task.OrderID,
		"{item_id}":      task.ItemID,
		"{buyer_id}":     task.BuyerID,
		"{chat_id}":      task.ChatID,
		"{trigger_type}": task.TriggerType,
		"{spec_name}":    task.SpecName,
		"{spec_value}":   task.SpecValue,
		"{quantity}":     task.Quantity,
		"{amount}":       task.Amount,
	}
	// key 和 value 分别表示当前占位符及其替换值。
	for key, value := range repl {
		out = strings.ReplaceAll(out, key, value)
	}
	return out
}
