// Package adapter 是账号运行时与外部能力（风控验证、通知、自动化中心）的装配层。
//
// 它实现 engine.Handler 与 automation.OrderDetailFetcher，把系统事件转发到自动化中心、
// 把订单详情抓取/凭证续期接到 Go 协议客户端、把账号告警推到通知器。业务逻辑集中在此，
// cmd/server 只负责构造与接线。
package adapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/renewal"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// browserManager 只暴露风控验证能力。普通 Token、Cookie 续期、订单和
// WebSocket 流程不得通过 Chromium 实现。
type browserManager interface {
	TokenCaptchaRecover(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider browser.TokenCaptchaURLProvider) (string, error)
}

type browserTokenCaptchaRecoverer interface {
	TokenCaptchaRecover(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider browser.TokenCaptchaURLProvider) (string, error)
}

type browserTokenCaptchaEngineRecoverer interface {
	TokenCaptchaRecoverWithEngine(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider browser.TokenCaptchaURLProvider) (cookies, engine string, err error)
}

type browserTokenCaptchaSnapshotReader interface {
	TokenCaptchaCookieSnapshot(ctx context.Context, cookieID string, headless bool) (cookies string, snapshot []cookierefresh.BrowserCookie, err error)
}

type tokenCaptchaRequester interface {
	RequestFreshCaptchaURLContext(ctx context.Context, cookiesStr, deviceID string) (*mtop.FreshCaptchaResult, error)
}

type orderDetailClient interface {
	FetchOrderDetail(ctx context.Context, cookiesStr, orderID string) (*mtop.OrderDetailResult, error)
}

// Adapter 实现 engine.Handler 与 automation.OrderDetailFetcher，
// 把系统消息、订单详情抓取和协议级凭证续期接到 Go 客户端与自动化中心。
//
// 自动发货只走 automation.Center；用户聊天消息由 Account 内部 ReplyService 处理，
// 故 HandleChatMessage 为空实现。
type Adapter struct {
	store      *db.Store
	browser    browserManager
	logger     *slog.Logger
	automation *automation.Center
	notifier   notifyNotifier
	renewSvc   xrenew.Service
	cooldown   *renewal.CooldownManager
	captchaReq tokenCaptchaRequester
	orderMTop  orderDetailClient
	chat       *chat.Service

	orderFetchMu   sync.Mutex
	lastOrderFetch time.Time

	passwordMu         sync.Mutex
	passwordProcessing map[string]struct{}
}

// notifyNotifier 是 *notify.Notifier 的最小接口，避免 adapter 直接依赖 notify 包
// （notify 包未来若反向引用 adapter 也不会形成循环）。
type notifyNotifier interface {
	NotifyAccountAlert(cookieID, level, title, body string)
}

type notifyEventNotifier interface {
	NotifyAccountEvent(cookieID, eventType, level, title, body string)
}

// New 构造 Adapter。automation 与 notifier 通过 Set* 后期注入（因创建顺序存在循环：
// mgr 依赖 adapter，automation 依赖 mgr，adapter 又依赖 automation）。
func New(store *db.Store, bm *browser.Manager, logger *slog.Logger) *Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Adapter{
		store:              store,
		browser:            browserManagerOrNil(bm),
		logger:             logger,
		cooldown:           renewal.GlobalCooldown,
		captchaReq:         mtop.NewClient(),
		orderMTop:          mtop.NewClient(),
		passwordProcessing: make(map[string]struct{}),
	}
}

// browserManagerOrNil 把 *browser.Manager 转为接口；nil 时返回 nil 接口。
func browserManagerOrNil(bm *browser.Manager) browserManager {
	if bm == nil {
		return nil
	}
	return bm
}

// SetAutomation 注入自动化中心（系统事件转发目标）。
func (a *Adapter) SetAutomation(c *automation.Center) { a.automation = c }

// SetNotifier 注入通知器（账号告警推送目标）。
func (a *Adapter) SetNotifier(n notifyNotifier) { a.notifier = n }

// SetBrowser 覆盖浏览器实现，便于测试注入桩。
func (a *Adapter) SetBrowser(b browserManager) { a.browser = b }

// SetRenewService 覆盖轻量续期服务，便于测试注入本地 HTTP 服务。
func (a *Adapter) SetRenewService(s xrenew.Service) { a.renewSvc = s }

// SetTokenCaptchaRequester 覆盖 token 风控验证链接刷新器，便于测试隔离网络。
func (a *Adapter) SetTokenCaptchaRequester(r tokenCaptchaRequester) { a.captchaReq = r }

// SetOrderDetailClient 覆盖纯 Go 订单详情客户端，便于测试隔离网络。
func (a *Adapter) SetOrderDetailClient(c orderDetailClient) { a.orderMTop = c }

// SetChatService installs the user-facing chat side channel. It persists and
// broadcasts messages without changing the automatic reply path.
func (a *Adapter) SetChatService(service *chat.Service) { a.chat = service }

// HandleChatMessage 用户聊天消息由 Account 内部 ReplyService 处理，此处空实现满足接口。
func (a *Adapter) HandleChatMessage(ctx context.Context, message engine.ChatMessage) error {
	if a.chat == nil {
		return nil
	}
	// Xianyu echoes messages sent by this account back over the same WS. Those
	// sends are already captured by HandleOutgoingChatMessage; recording the
	// echo as incoming would put our own bubble on the buyer side and duplicate it.
	if strings.TrimSuffix(message.SenderUserID, "@goofish") == strings.TrimSuffix(message.AccountID, "@goofish") {
		return nil
	}
	_, _, err := a.chat.RecordIncoming(ctx, chat.Incoming{
		AccountID: message.AccountID, ChatID: message.ChatID, BuyerID: message.SenderUserID,
		BuyerName: message.SenderName, Text: message.Text, ItemID: message.ItemID, Raw: message.Raw,
	})
	return err
}

// HandleOutgoingChatMessage records successful manual/automatic text sends as
// a side channel; it never participates in platform delivery.
func (a *Adapter) HandleOutgoingChatMessage(ctx context.Context, message engine.OutgoingChatMessage) error {
	if a.chat == nil {
		return nil
	}
	_, err := a.chat.RecordOutgoingSent(ctx, db.ChatSession{CookieID: message.AccountID, ChatID: message.ChatID,
		BuyerID: message.BuyerID}, message.MessageKey, message.Text)
	return err
}

// OnAccountAlert 把账号告警（token 失效/自动恢复失败/风控验证等）转发给通知器，
// 推送到该账号已绑定的通知渠道。
func (a *Adapter) OnAccountAlert(ctx context.Context, cookieID, level, title, body string) {
	a.OnAccountEvent(ctx, cookieID, classifyAccountAlertEvent(title, body), level, title, body)
}

// OnAccountEvent 把带类型的账号事件转发给通知器。
func (a *Adapter) OnAccountEvent(_ context.Context, cookieID, eventType, level, title, body string) {
	if a.notifier == nil {
		a.logger.Warn("账号事件通知未发送：通知器未注入", "account", cookieID, "event_type", eventType, "level", level, "title", title)
		return
	}
	if n, ok := a.notifier.(notifyEventNotifier); ok {
		n.NotifyAccountEvent(cookieID, eventType, level, title, body)
		return
	}
	a.notifier.NotifyAccountAlert(cookieID, level, title, body)
}

func classifyAccountAlertEvent(title, body string) string {
	msg := strings.ToLower(title + " " + body)
	switch {
	case strings.Contains(msg, "风控"), strings.Contains(msg, "验证"),
		strings.Contains(msg, "滑块"), strings.Contains(msg, "captcha"),
		strings.Contains(msg, "risk"), strings.Contains(msg, "x5sec"):
		return engine.EventSecurityVerification
	case strings.Contains(msg, "禁用"), strings.Contains(msg, "disabled"):
		return engine.EventAccountDisabled
	case strings.Contains(msg, "掉线"), strings.Contains(msg, "离线"),
		strings.Contains(msg, "offline"), strings.Contains(msg, "session"),
		strings.Contains(msg, "登录凭证已失效"):
		return engine.EventAccountOffline
	case strings.Contains(msg, "token"), strings.Contains(msg, "续期"), strings.Contains(msg, "renew"):
		return engine.EventTokenRenewal
	default:
		return engine.EventSystemError
	}
}

// OnTokenCaptchaVerification 处理 token 刷新触发的闲鱼滑块风控。
func (a *Adapter) OnTokenCaptchaVerification(ctx context.Context, cookieID, cookieStr, verificationURL, deviceID string) (*mtop.RefreshResult, bool) {
	start := time.Now()
	var logID int64
	if a.store != nil && a.store.RiskLogs != nil {
		if id, err := a.store.RiskLogs.Add(ctx, db.RiskControlLog{
			CookieID:         cookieID,
			EventType:        "slider_captcha",
			EventDescription: "触发场景: Token刷新, URL: " + verificationURL,
			ProcessingStatus: "processing",
		}); err == nil {
			logID = id
		} else {
			a.logger.Warn("记录风控日志失败", "account", cookieID, "err", err)
		}
	}

	showBrowser := false
	metadataJSON := ""
	if a.store == nil || a.store.Cookies == nil {
		a.OnAccountEvent(ctx, cookieID, engine.EventSecurityVerification, engine.AlertLevelWarn,
			"token 风控验证无法保存", "账号存储未初始化，无法保存验证后的 Cookie。")
		return nil, false
	}

	if d, err := a.store.Cookies.GetDetails(ctx, cookieID); err == nil && d != nil {
		showBrowser = d.ShowBrowser
		metadataJSON = d.MetadataJSON
	}

	provider := func(runCtx context.Context, currentCookies string) (string, bool, string, error) {
		if a.captchaReq == nil {
			return "", false, "", nil
		}
		res, err := a.captchaReq.RequestFreshCaptchaURLContext(runCtx, currentCookies, deviceID)
		if err != nil || res == nil {
			return "", false, "", err
		}
		return res.VerificationURL, res.TokenOK, res.UpdatedCookies, nil
	}

	newCookies := ""
	captchaEngine := "playwright"
	remoteHandled := false
	captchaHeadless := browser.ResolveHeadless(showBrowser)
	var err error
	if remoteConfig := a.loadRemoteCaptchaConfig(ctx); remoteConfig != nil {
		newCookies, remoteHandled, err = solveRemoteCaptcha(
			ctx, newRemoteCaptchaHTTPClient(), *remoteConfig,
			cookieID, verificationURL, cookieStr, deviceID, provider,
		)
		if remoteHandled {
			captchaEngine = "remote"
		} else if err != nil {
			a.logger.Warn("远程过滑块不可用，回退本机逻辑", "account", cookieID, "err", err)
			err = nil
		}
	}
	if !remoteHandled {
		br, ok := a.browser.(browserTokenCaptchaRecoverer)
		if a.browser == nil || !ok {
			a.OnAccountEvent(ctx, cookieID, engine.EventSecurityVerification, engine.AlertLevelWarn,
				"token 风控验证无法自动处理", "远程服务不可用且浏览器自动化未启用，无法自动完成 token 滑块验证。")
			return nil, false
		}
		if withEngine, ok := a.browser.(browserTokenCaptchaEngineRecoverer); ok {
			newCookies, captchaEngine, err = withEngine.TokenCaptchaRecoverWithEngine(
				ctx, cookieID, cookieStr, verificationURL, captchaHeadless, provider,
			)
		} else {
			newCookies, err = br.TokenCaptchaRecover(
				ctx, cookieID, cookieStr, verificationURL, captchaHeadless, provider,
			)
		}
	}
	if err != nil {
		a.logger.Warn("token 风控滑块处理失败", "account", cookieID, "err", err)
		if a.store != nil && a.store.RiskLogs != nil {
			_ = a.store.RiskLogs.Update(ctx, logID, db.RiskControlLog{
				ProcessingStatus: "failed",
				ProcessingResult: fmt.Sprintf("token 风控滑块处理失败，耗时: %.2f秒", time.Since(start).Seconds()),
				CaptchaEngine:    captchaEngine,
				ErrorMessage:     err.Error(),
				DurationMS:       time.Since(start).Milliseconds(),
			})
		}
		a.OnAccountEvent(ctx, cookieID, engine.EventSecurityVerification, engine.AlertLevelWarn,
			"token 风控验证失败", err.Error())
		return nil, false
	}
	if strings.TrimSpace(newCookies) == "" {
		return nil, false
	}
	var cookieSnapshot []cookierefresh.BrowserCookie
	snapshotComplete := false
	if !remoteHandled {
		if reader, ok := a.browser.(browserTokenCaptchaSnapshotReader); ok {
			profileCookies, profileSnapshot, readErr := reader.TokenCaptchaCookieSnapshot(ctx, cookieID, captchaHeadless)
			if readErr != nil {
				a.logger.Warn("读取滑块验证后完整 Cookie Jar 失败，回退 Go 快照合并", "account", cookieID, "err", readErr)
			} else {
				cookieSnapshot = cookierefresh.NormalizeSnapshot(profileSnapshot)
				if cookieSnapshot == nil {
					cookieSnapshot = []cookierefresh.BrowserCookie{}
				}
				snapshotComplete = true
				newCookies = profileCookies
			}
		}
	}
	if !snapshotComplete {
		if existing, complete := cookierefresh.SnapshotFromMetadataOK(metadataJSON); complete {
			cookieSnapshot = cookierefresh.ReconcileSnapshotWithCookieString(existing, newCookies)
			snapshotComplete = true
		}
	}
	updatedMetadata := cookierefresh.MetadataWithoutSnapshot(metadataJSON)
	if snapshotComplete {
		updatedMetadata = cookierefresh.MetadataWithSnapshot(metadataJSON, cookieSnapshot)
	}
	if err := a.store.Cookies.UpdateRenewalCookie(ctx, cookieID, newCookies, updatedMetadata, time.Now().Unix()); err != nil {
		a.logger.Warn("保存 token 风控恢复 Cookie 失败", "account", cookieID, "err", err)
		if a.store != nil && a.store.RiskLogs != nil {
			_ = a.store.RiskLogs.Update(ctx, logID, db.RiskControlLog{
				ProcessingStatus: "error",
				ProcessingResult: "滑块完成但保存 Cookie 失败",
				CaptchaEngine:    captchaEngine,
				ErrorMessage:     err.Error(),
				DurationMS:       time.Since(start).Milliseconds(),
			})
		}
		return nil, false
	}
	if a.store.Tokens != nil {
		_ = a.store.Tokens.Clear(ctx, cookieID)
	}
	if a.store != nil && a.store.RiskLogs != nil {
		_ = a.store.RiskLogs.Update(ctx, logID, db.RiskControlLog{
			ProcessingStatus: "success",
			ProcessingResult: fmt.Sprintf("token 风控滑块验证成功（%s），已更新登录凭证，耗时: %.2f秒", captchaEngine, time.Since(start).Seconds()),
			CaptchaEngine:    captchaEngine,
			DurationMS:       time.Since(start).Milliseconds(),
		})
	}
	a.OnAccountEvent(ctx, cookieID, engine.EventSecurityVerification, engine.AlertLevelInfo,
		"token 风控验证已自动恢复", "系统已完成验证并更新登录凭证。")
	return &mtop.RefreshResult{
		UpdatedCookies:         newCookies,
		CookieSnapshot:         cookieSnapshot,
		CookieSnapshotComplete: snapshotComplete,
		CookieStateChanged:     newCookies != cookieStr || snapshotComplete,
	}, true
}

// HandleSystemEvent 把系统卡片事件转发到自动化中心，由自动化规则决定是否执行。
func (a *Adapter) HandleSystemEvent(ctx context.Context, task automation.Task) error {
	if a.automation == nil {
		return nil
	}
	a.logger.Info("系统自动化事件", "account", task.AccountID, "trigger", task.TriggerType, "order_id", task.OrderID)
	return a.automation.HandleTask(ctx, task)
}

// FetchOrderDetail 实现 automation.OrderDetailFetcher。只在本地订单缺少关键字段时
// 调用纯 Go MTOP 客户端，并将详情请求串行化、至少间隔 3 秒，避免短时间高频访问闲鱼。
func (a *Adapter) FetchOrderDetail(ctx context.Context, cookieID, orderID, itemID, buyerID, _ string) (*automation.OrderDetail, error) {
	if detail, ok := a.localOrderDetail(ctx, orderID); ok {
		return detail, nil
	}
	if a.orderMTop == nil {
		return nil, fmt.Errorf("订单详情 MTOP 客户端未配置")
	}
	detail, err := a.fetchOrderDetailAttempt(ctx, cookieID, orderID)
	if err == nil || !mtop.IsSessionExpiredErr(err) {
		return detail, err
	}
	a.logger.Warn("订单详情检测到 Session 过期，开始即时续期", "account", cookieID, "order_id", orderID)
	if !a.OnPasswordLoginRefresh(ctx, cookieID) {
		return nil, fmt.Errorf("订单详情 Session 过期且即时续期失败: %w", err)
	}
	a.logger.Info("Cookie 即时续期成功，重新请求订单详情", "account", cookieID, "order_id", orderID)
	return a.fetchOrderDetailAttempt(ctx, cookieID, orderID)
}

func (a *Adapter) fetchOrderDetailAttempt(ctx context.Context, cookieID, orderID string) (*automation.OrderDetail, error) {

	a.orderFetchMu.Lock()
	defer a.orderFetchMu.Unlock()
	// 等锁期间其他流程可能已经补齐订单，再检查一次。
	if detail, ok := a.localOrderDetail(ctx, orderID); ok {
		return detail, nil
	}
	if remain := 3*time.Second - time.Since(a.lastOrderFetch); !a.lastOrderFetch.IsZero() && remain > 0 {
		timer := time.NewTimer(remain)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	a.lastOrderFetch = time.Now()
	credentialUnlock := a.store.LockAccountCredentials(cookieID)
	defer credentialUnlock()
	account, err := a.store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		return nil, fmt.Errorf("读取订单账号最新 Cookie: %w", err)
	}
	if account == nil || (strings.TrimSpace(account.Value) == "" && !hasCompleteCookieSnapshot(account.MetadataJSON)) {
		return nil, fmt.Errorf("订单账号 %s Cookie 为空", cookieID)
	}
	cookieStr := account.Value
	var requestCtx context.Context
	var cookieSession *mtop.CookieSession
	if snapshot, complete := cookierefresh.SnapshotFromMetadataOK(account.MetadataJSON); complete {
		requestCtx, cookieSession = mtop.WithCookieSnapshot(ctx, snapshot)
	} else {
		requestCtx, cookieSession = mtop.WithFlatCookieSession(ctx, cookieStr)
	}
	detail, fetchErr := a.orderMTop.FetchOrderDetail(requestCtx, cookieStr, orderID)
	authoritativeCookies, authoritativeSnapshot, sessionChanged := cookieSession.State()
	if sessionChanged {
		metadata := cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)
		if authoritativeSnapshot != nil {
			metadata = cookierefresh.MetadataWithSnapshot(account.MetadataJSON, authoritativeSnapshot)
		}
		if persistErr := a.store.Cookies.UpdateRenewalCookie(ctx, cookieID, authoritativeCookies, metadata, time.Now().Unix()); persistErr != nil {
			fetchErr = errors.Join(fetchErr, fmt.Errorf("保存订单详情响应 Cookie: %w", persistErr))
		} else {
			cookieStr = authoritativeCookies
			a.wakeCredentialBlockedAutomation(ctx, cookieID)
		}
	}
	if fetchErr != nil {
		return nil, fetchErr
	}
	if detail == nil {
		return nil, errors.New("订单详情 MTOP 接口返回空结果")
	}
	if !sessionChanged && authoritativeSnapshot == nil && detail.UpdatedCookies != "" && detail.UpdatedCookies != cookieStr {
		metadata := cookierefresh.MetadataWithoutSnapshot(account.MetadataJSON)
		if err := a.store.Cookies.UpdateRenewalCookie(ctx, cookieID, detail.UpdatedCookies, metadata, time.Now().Unix()); err != nil {
			return nil, fmt.Errorf("保存订单详情响应 Cookie: %w", err)
		}
		a.wakeCredentialBlockedAutomation(ctx, cookieID)
	}
	return &automation.OrderDetail{
		Quantity: detail.Quantity, SpecName: detail.SpecName, SpecValue: detail.SpecValue,
		Amount: detail.Amount, OrderStatus: detail.OrderStatus,
	}, nil
}

func (a *Adapter) wakeCredentialBlockedAutomation(ctx context.Context, cookieID string) {
	if a.store == nil || a.store.Automation == nil {
		return
	}
	if err := a.store.Automation.WakeCredentialBlocked(ctx, cookieID); err != nil {
		a.logger.Warn("Cookie 更新后唤醒自动化任务失败", "account", cookieID, "err", err)
	}
}

func hasCompleteCookieSnapshot(metadata string) bool {
	_, ok := cookierefresh.SnapshotFromMetadataOK(metadata)
	return ok
}

// localOrderDetail 命中本地完整订单时直接返回，避免不必要的 MTOP 请求。
func (a *Adapter) localOrderDetail(ctx context.Context, orderID string) (*automation.OrderDetail, bool) {
	order, err := a.store.Orders.Get(ctx, orderID)
	if err != nil || order == nil {
		return nil, false
	}
	if order.Amount == "" || order.Quantity == "" || order.SpecName == "" || order.SpecValue == "" {
		return nil, false
	}
	return &automation.OrderDetail{
		Quantity: order.Quantity, SpecName: order.SpecName, SpecValue: order.SpecValue,
		Amount: order.Amount, OrderStatus: order.OrderStatus,
	}, true
}

// OnPasswordLoginRefresh 是 engine 的历史回调名。Go 客户端只执行协议级
// auto-login 续期；失败后要求重新扫码，不得启动 Chromium 密码登录或页面校验。
func (a *Adapter) OnPasswordLoginRefresh(ctx context.Context, cookieID string) bool {
	cooldown := a.cooldown
	if cooldown == nil {
		cooldown = renewal.GlobalCooldown
	}
	if ok, remain, reason := cooldown.PasswordLoginAllowed(cookieID, engine.PasswordLoginMinGap); !ok {
		a.logger.Warn("协议续期冷却中", "account", cookieID, "remain", remain.Round(time.Second))
		a.recordPasswordLogin(ctx, cookieID, 0, "skipped_cooldown", reason, fmt.Sprintf("协议续期冷却中，还需等待 %s", remain.Round(time.Second)))
		return false
	}
	if !a.beginPasswordLogin(cookieID) {
		a.logger.Warn("协议续期已在处理中", "account", cookieID)
		return false
	}
	defer a.finishPasswordLogin(cookieID)

	d, err := a.store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		a.logger.Warn("协议续期失败：读取账号详情失败", "account", cookieID, "err", err)
		a.recordPasswordLogin(ctx, cookieID, 0, "failed", "account_lookup_failed", err.Error())
		return false
	}

	renewed, renewErr := a.tryProtocolCredentialRenew(ctx, d)
	if renewed {
		a.wakeCredentialBlockedAutomation(ctx, cookieID)
		a.recordPasswordLogin(ctx, cookieID, d.UserID, "success", "", "Go 协议续期成功")
		return true
	}
	message := "Go 协议续期未恢复登录凭证，请重新扫码登录"
	if renewErr != nil {
		message += "：" + renewErr.Error()
	}
	a.logger.Warn("协议续期未恢复账号", "account", cookieID, "err", renewErr)
	a.OnAccountEvent(ctx, cookieID, engine.EventAccountOffline, engine.AlertLevelWarn, "账号需要重新扫码", message)
	a.recordPasswordLogin(ctx, cookieID, d.UserID, "failed", "qr_login_required", message)
	return false
}

// RecoverExpiredCredential 供自动化外部动作在平台明确拒绝旧 Session 后恢复凭证。
func (a *Adapter) RecoverExpiredCredential(ctx context.Context, cookieID string) bool {
	return a.OnPasswordLoginRefresh(ctx, cookieID)
}

// OnCredentialUpdated 接收账号运行时保存的新凭证并唤醒失败任务。
func (a *Adapter) OnCredentialUpdated(ctx context.Context, cookieID string) {
	a.wakeCredentialBlockedAutomation(ctx, cookieID)
}

// OnTransportReady 在 WS 注册完成后立即唤醒发送前明确未执行的任务。
func (a *Adapter) OnTransportReady(ctx context.Context, cookieID string) {
	a.wakeCredentialBlockedAutomation(ctx, cookieID)
}

func (a *Adapter) beginPasswordLogin(cookieID string) bool {
	a.passwordMu.Lock()
	defer a.passwordMu.Unlock()
	if a.passwordProcessing == nil {
		a.passwordProcessing = make(map[string]struct{})
	}
	if _, ok := a.passwordProcessing[cookieID]; ok {
		return false
	}
	a.passwordProcessing[cookieID] = struct{}{}
	return true
}

func (a *Adapter) finishPasswordLogin(cookieID string) {
	a.passwordMu.Lock()
	defer a.passwordMu.Unlock()
	delete(a.passwordProcessing, cookieID)
}

func (a *Adapter) recordPasswordLogin(ctx context.Context, cookieID string, userID int64, status, failureReason, message string) {
	if a.store == nil || a.store.LoginLogs == nil {
		return
	}
	if err := a.store.LoginLogs.Add(ctx, db.AccountLoginLog{
		CookieID:          cookieID,
		UserID:            userID,
		Method:            "protocol",
		Status:            status,
		Message:           truncateMessage(message, 500),
		TriggerReason:     "令牌/Session过期",
		FailureReason:     failureReason,
		ErrorMessage:      truncateMessage(message, 500),
		AccountIdentifier: cookieID,
		DurationMS:        0,
		CreatedAt:         time.Now().Unix(),
	}); err != nil {
		a.logger.Warn("记录协议续期日志失败", "account", cookieID, "err", err)
	}
}

func truncateMessage(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

func (a *Adapter) tryProtocolCredentialRenew(ctx context.Context, d *db.CookieDetail) (bool, error) {
	if d == nil {
		return false, nil
	}
	current := d.Value
	api := a.renewSvc
	save := func(cookieStr string, setCookies []string, completeSnapshot []cookierefresh.BrowserCookie) error {
		if cookieStr == current && len(setCookies) == 0 && completeSnapshot == nil {
			return nil
		}
		metadata := cookierefresh.MetadataWithoutSnapshot(d.MetadataJSON)
		if completeSnapshot != nil {
			// API 在完整 Jar 基础上得到的快照是权威结果，包含
			// 服务端删除和新的 Domain/Path/expiry 属性。
			metadata = cookierefresh.MetadataWithSnapshot(d.MetadataJSON, completeSnapshot)
		}
		if err := a.store.Cookies.UpdateRenewalCookie(ctx, d.ID, cookieStr, metadata, time.Now().Unix()); err != nil {
			a.logger.Warn("轻量续期保存 Cookie 失败", "account", d.ID, "err", err)
			return err
		}
		valueChanged := cookieStr != current
		current = cookieStr
		d.Value = cookieStr
		d.MetadataJSON = metadata
		if valueChanged && a.store.Tokens != nil {
			if err := a.store.Tokens.Clear(ctx, d.ID); err != nil {
				// Token 仅是运行期缓存；Cookie 已原子提交后不能再把整次
				// 续期报告成失败，否则调用方可能用旧凭证重试并覆盖新 Jar。
				a.logger.Warn("轻量续期清理旧 Token 缓存失败", "account", d.ID, "err", err)
			}
		}
		return nil
	}
	// 官网始终先由 auto-login plugin 按 havana_lgc_exp/cookie3_bak_exp
	// 决定是否调用 silentHasLogin。Go 客户端复刻该 HTTP 协议，不加载页面。
	runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	res, err := api.RenewAfterSessionExpired(runCtx, current, cookierefresh.SnapshotFromMetadata(d.MetadataJSON))
	if res != nil {
		var completeSnapshot []cookierefresh.BrowserCookie
		if res.CookieSnapshotComplete {
			completeSnapshot = res.CookieSnapshot
			if completeSnapshot == nil {
				completeSnapshot = []cookierefresh.BrowserCookie{}
			}
		}
		if saveErr := save(res.NewCookies, res.SetCookies, completeSnapshot); saveErr != nil {
			return false, saveErr
		}
		if res.HasPending() {
			// 恢复路径不能把“底层请求仍在进行”提前记为成功，否则上层会
			// 重置失败计数并继续使用未确认的旧凭证。这里等待最终响应；定时
			// 调度仍使用异步 watcher，不阻塞健康账号。
			waitCtx, waitCancel := context.WithTimeout(ctx, 35*time.Second)
			late, waitErr := res.AwaitPending(waitCtx)
			waitCancel()
			if late == nil {
				if waitErr != nil {
					return false, waitErr
				}
				return false, errors.New("协议续期底层响应未返回结果")
			}
			var lateSnapshot []cookierefresh.BrowserCookie
			if late.CookieSnapshotComplete {
				lateSnapshot = late.CookieSnapshot
				if lateSnapshot == nil {
					lateSnapshot = []cookierefresh.BrowserCookie{}
				}
			}
			if saveErr := save(late.NewCookies, late.SetCookies, lateSnapshot); saveErr != nil {
				return false, saveErr
			}
			if waitErr != nil {
				return false, waitErr
			}
			if late.Success {
				a.logger.Info("Go 协议续期迟到响应成功", "account", d.ID)
				return true, nil
			}
			message := strings.TrimSpace(late.Message)
			if message == "" {
				message = "协议续期未通过"
			}
			return false, errors.New(message)
		}
		if err == nil && res.Success {
			a.logger.Info("Go 协议续期成功", "account", d.ID)
			return true, nil
		}
	}
	return false, err
}

// 编译期保证 *Adapter 同时实现 engine.Handler 与 automation.OrderDetailFetcher。
var (
	_ engine.Handler                = (*Adapter)(nil)
	_ automation.OrderDetailFetcher = (*Adapter)(nil)
)
