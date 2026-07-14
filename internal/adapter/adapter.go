// Package adapter 是账号运行时与外部能力（浏览器自动化、通知、自动化中心）的装配层。
//
// 它实现 engine.Handler 与 automation.OrderDetailFetcher，把系统事件转发到自动化中心、
// 把订单详情抓取/密码登录刷新接到浏览器、把账号告警推到通知器。业务逻辑集中在此，
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
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/renewal"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// browserManager 是 adapter 所需的浏览器能力最小契约。*browser.Manager 实现该接口；
// 测试可注入桩实现，避免依赖 Chromium。
type browserManager interface {
	FetchOrderDetail(ctx context.Context, orderID, cookieID, cookieValue string, requireSpec ...bool) (*browser.OrderDetail, error)
	CookieRenew(ctx context.Context, cookieID, cookieStr string, headless bool) (map[string]string, error)
	PasswordLogin(ctx context.Context, account, password, cookieID, userDataDir string, headless bool) (map[string]string, error)
}

type browserQuickRenewer interface {
	BrowserQuickRenew(ctx context.Context, cookieID, cookieStr string, headless bool) (string, error)
}

type browserTokenCaptchaRecoverer interface {
	TokenCaptchaRecover(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider browser.TokenCaptchaURLProvider) (string, error)
}

type browserTokenCaptchaEngineRecoverer interface {
	TokenCaptchaRecoverWithEngine(ctx context.Context, cookieID, cookieStr, verificationURL string, headless bool, provider browser.TokenCaptchaURLProvider) (cookies, engine string, err error)
}

type tokenCaptchaRequester interface {
	RequestFreshCaptchaURLContext(ctx context.Context, cookiesStr, deviceID string) (*mtop.FreshCaptchaResult, error)
}

// Adapter 实现 engine.Handler 与 automation.OrderDetailFetcher，
// 把系统消息、订单详情抓取和密码登录刷新接到浏览器与自动化中心。
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

// HandleChatMessage 用户聊天消息由 Account 内部 ReplyService 处理，此处空实现满足接口。
func (a *Adapter) HandleChatMessage(_ context.Context, _ engine.ChatMessage) error {
	return nil
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
				ctx, cookieID, cookieStr, verificationURL, browser.ResolveHeadless(showBrowser), provider,
			)
		} else {
			newCookies, err = br.TokenCaptchaRecover(
				ctx, cookieID, cookieStr, verificationURL, browser.ResolveHeadless(showBrowser), provider,
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
	if err := a.store.Cookies.UpdateRenewalCookie(ctx, cookieID, newCookies, cookierefresh.MetadataWithoutSnapshot(metadataJSON), time.Now().Unix()); err != nil {
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
	return &mtop.RefreshResult{UpdatedCookies: newCookies}, true
}

// HandleSystemEvent 把系统卡片事件转发到自动化中心，由自动化规则决定是否执行。
func (a *Adapter) HandleSystemEvent(ctx context.Context, task automation.Task) error {
	if a.automation == nil {
		return nil
	}
	a.logger.Info("系统自动化事件", "account", task.AccountID, "trigger", task.TriggerType, "order_id", task.OrderID)
	return a.automation.HandleTask(ctx, task)
}

// FetchOrderDetail 实现 automation.OrderDetailFetcher。只在本地订单缺少关键字段时启动浏览器，
// 并将所有账号的详情请求串行化、至少间隔 3 秒，避免短时间高频访问闲鱼。
func (a *Adapter) FetchOrderDetail(ctx context.Context, cookieID, orderID, itemID, buyerID, cookieStr string) (*automation.OrderDetail, error) {
	if detail, ok := a.localOrderDetail(ctx, orderID); ok {
		return detail, nil
	}
	if a.browser == nil {
		return nil, fmt.Errorf("订单缺少规格/数量信息且浏览器自动化未启用")
	}

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
	detail, err := a.browser.FetchOrderDetail(ctx, orderID, cookieID, cookieStr, true)
	if err != nil {
		return nil, err
	}
	if detail.UpdatedCookies != "" && detail.UpdatedCookies != cookieStr {
		_ = a.store.Cookies.UpdateValueExisting(ctx, cookieID, detail.UpdatedCookies)
	}
	return &automation.OrderDetail{
		Quantity: detail.Quantity, SpecName: detail.SpecName, SpecValue: detail.SpecValue,
		Amount: detail.Amount, OrderStatus: detail.OrderStatus,
	}, nil
}

// localOrderDetail 命中本地完整订单时直接返回，避免不必要的浏览器抓取。
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

// OnPasswordLoginRefresh 连续失败时恢复 Cookie。
//
// 恢复顺序是“轻量续期 -> 浏览器快速续期 -> 密码登录”。轻量续期/快速续期复用旧 Cookie
// 刷新登录态；这比账号密码登录更轻，风控压力也更小。
// 同一账号冷却 engine.PasswordLoginMinGap，避免短时间反复触发浏览器恢复。
func (a *Adapter) OnPasswordLoginRefresh(ctx context.Context, cookieID string) bool {
	cooldown := a.cooldown
	if cooldown == nil {
		cooldown = renewal.GlobalCooldown
	}
	if ok, remain, reason := cooldown.PasswordLoginAllowed(cookieID, engine.PasswordLoginMinGap); !ok {
		a.logger.Warn("密码登录刷新冷却中", "account", cookieID, "remain", remain.Round(time.Second))
		a.recordPasswordLogin(ctx, cookieID, 0, "skipped_cooldown", reason, fmt.Sprintf("密码登录刷新冷却中，还需等待 %s", remain.Round(time.Second)))
		return false
	}
	if !a.beginPasswordLogin(cookieID) {
		a.logger.Warn("密码登录刷新已在处理中", "account", cookieID)
		return false
	}
	defer a.finishPasswordLogin(cookieID)

	d, err := a.store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		a.logger.Warn("密码登录刷新失败：读取账号详情失败", "account", cookieID, "err", err)
		a.recordPasswordLogin(ctx, cookieID, 0, "failed", "account_lookup_failed", err.Error())
		return false
	}

	lightRenewed, lightRenewErr := a.tryLightRenewBeforePassword(ctx, d)
	if lightRenewed {
		a.recordPasswordLogin(ctx, cookieID, d.UserID, "success", "", "密码登录前轻量续期成功")
		return true
	}
	if errors.Is(lightRenewErr, browser.ErrSecurityVerification) {
		cooldown.MarkPasswordError(cookieID)
		message := "浏览器续期遇到闲鱼安全验证，已停止自动密码登录，请人工完成验证或重新扫码"
		a.OnAccountEvent(ctx, cookieID, engine.EventSecurityVerification, engine.AlertLevelWarn, "闲鱼要求安全验证", message)
		a.recordPasswordLogin(ctx, cookieID, d.UserID, "failed", "verification_required", message)
		return false
	}

	if latest, err := a.store.Cookies.GetDetails(ctx, cookieID); err == nil && latest != nil && latest.Value != "" && latest.Value != d.Value {
		a.logger.Info("密码登录前检测到 DB Cookie 已被外部更新，跳过密码登录", "account", cookieID)
		if a.store.Tokens != nil {
			_ = a.store.Tokens.Clear(ctx, cookieID)
		}
		a.recordPasswordLogin(ctx, cookieID, d.UserID, "success", "cookie_already_updated_externally", "数据库中的 Cookie 已被外部更新，跳过密码登录")
		return true
	}

	if a.browser == nil {
		a.logger.Warn("密码登录刷新失败：浏览器自动化已禁用，接口轻量续期也未恢复", "account", cookieID)
		a.recordPasswordLogin(ctx, cookieID, d.UserID, "failed", "browser_disabled", "浏览器自动化已禁用，接口轻量续期未恢复")
		return false
	}

	if strings.TrimSpace(d.Username) == "" || strings.TrimSpace(d.Password) == "" {
		msg := "账号已掉线且未配置账号密码"
		a.logger.Warn("密码登录刷新失败：账号未配置登录用户名或密码", "account", cookieID)
		if err := a.store.Cookies.SetStatusWithReason(ctx, cookieID, false, msg); err != nil {
			a.logger.Warn("未配置账号密码时停用账号失败", "account", cookieID, "err", err)
		}
		a.OnAccountEvent(ctx, cookieID, engine.EventAccountDisabled, engine.AlertLevelCritical, "账号已自动禁用", msg)
		a.recordPasswordLogin(ctx, cookieID, d.UserID, "no_credentials", "no_credentials", msg)
		return false
	}
	cookies, err := a.browser.PasswordLogin(ctx, d.Username, d.Password, cookieID, "", browser.ResolveHeadless(d.ShowBrowser))
	if err != nil {
		a.handlePasswordLoginError(ctx, cookieID, err)
		a.recordPasswordLogin(ctx, cookieID, d.UserID, "failed", passwordLoginFailureReason(err), err.Error())
		return false
	}
	cookieStr := browser.MarshalCookies(cookies)
	if strings.TrimSpace(cookieStr) == "" {
		a.logger.Warn("密码登录刷新失败：浏览器未返回 cookie", "account", cookieID)
		a.recordPasswordLogin(ctx, cookieID, d.UserID, "failed", "empty_cookies", "浏览器未返回 cookie")
		return false
	}
	// 参考实现只在真实密码登录拿到 Cookie 后启动 60 秒冷却。
	cooldown.MarkPasswordLogin(cookieID)
	if err := a.store.Cookies.UpdateValueExisting(ctx, cookieID, cookieStr); err != nil {
		a.logger.Warn("密码登录刷新失败：保存 cookie 失败", "account", cookieID, "err", err)
		a.recordPasswordLogin(ctx, cookieID, d.UserID, "failed", "cookie_update_failed", err.Error())
		return false
	}
	if a.store.Tokens != nil {
		_ = a.store.Tokens.Clear(ctx, cookieID)
	}
	_ = a.store.Cookies.MarkLogin(ctx, cookieID, "password", time.Now().Unix())
	a.recordPasswordLogin(ctx, cookieID, d.UserID, "success", "", "账号密码登录刷新成功")
	a.logger.Info("密码登录刷新 cookie 成功", "account", cookieID)
	return true
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
		Method:            "password",
		Status:            status,
		Message:           truncateMessage(message, 500),
		TriggerReason:     "令牌/Session过期",
		FailureReason:     failureReason,
		ErrorMessage:      truncateMessage(message, 500),
		AccountIdentifier: cookieID,
		DurationMS:        0,
		CreatedAt:         time.Now().Unix(),
	}); err != nil {
		a.logger.Warn("记录密码登录日志失败", "account", cookieID, "err", err)
	}
}

func passwordLoginFailureReason(err error) string {
	if err == nil {
		return ""
	}
	msg := err.Error()
	if isPasswordLoginBadCredentials(msg) {
		return "bad_credentials"
	}
	event := browser.PasswordLoginEventFromError(err)
	if event.Reason != "" {
		return event.Reason
	}
	if event.Status == browser.PasswordLoginStatusVerificationRequired {
		return "verification_required"
	}
	return "other"
}

func truncateMessage(s string, n int) string {
	if n <= 0 || len(s) <= n {
		return s
	}
	return s[:n]
}

func (a *Adapter) tryLightRenewBeforePassword(ctx context.Context, d *db.CookieDetail) (bool, error) {
	if d == nil {
		return false, nil
	}
	current := d.Value
	api := a.renewSvc
	save := func(cookieStr string) {
		if strings.TrimSpace(cookieStr) == "" || cookieStr == current {
			return
		}
		if err := a.store.Cookies.UpdateRenewalCookie(ctx, d.ID, cookieStr, cookierefresh.MetadataWithoutSnapshot(d.MetadataJSON), time.Now().Unix()); err != nil {
			a.logger.Warn("轻量续期保存 Cookie 失败", "account", d.ID, "err", err)
			return
		}
		current = cookieStr
		d.Value = cookieStr
		if a.store.Tokens != nil {
			_ = a.store.Tokens.Clear(ctx, d.ID)
		}
	}
	apiRenew := func(cookieStr string) (*xrenew.Result, error) {
		runCtx, cancel := context.WithTimeout(ctx, 90*time.Second)
		defer cancel()
		return api.RenewAPIFirst(runCtx, cookieStr)
	}
	browserRenew := func(cookieStr string) (string, error) {
		if a.browser == nil {
			return "", fmt.Errorf("浏览器自动化未启用")
		}
		if br, ok := a.browser.(browserQuickRenewer); ok {
			runCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
			defer cancel()
			return br.BrowserQuickRenew(runCtx, d.ID, cookieStr, browser.ResolveHeadless(d.ShowBrowser))
		}
		m, err := a.browser.CookieRenew(ctx, d.ID, cookieStr, browser.ResolveHeadless(d.ShowBrowser))
		if err != nil {
			return "", err
		}
		return browser.MarshalCookies(m), nil
	}

	hasLongLogin := strings.TrimSpace(protocol.TransCookies(current)["havana_lgc2_77"]) != ""
	if hasLongLogin {
		if renewed, err := browserRenew(current); err == nil && renewed != "" {
			save(renewed)
			a.logger.Info("密码登录前浏览器续期成功，跳过密码登录", "account", d.ID)
			return true, nil
		} else if err != nil {
			if errors.Is(err, browser.ErrSecurityVerification) {
				return false, err
			}
			a.logger.Warn("密码登录前浏览器续期失败，继续尝试接口续期", "account", d.ID, "err", err)
		}
		if res, err := apiRenew(current); err == nil && res != nil {
			save(res.NewCookies)
			if res.Success {
				a.logger.Info("密码登录前静默续期成功，跳过密码登录", "account", d.ID)
				return true, nil
			}
		}
		return false, nil
	}

	res, err := apiRenew(current)
	if err == nil && res != nil {
		save(res.NewCookies)
		if res.Success {
			a.logger.Info("密码登录前接口续期成功，跳过密码登录", "account", d.ID)
			return true, nil
		}
	}
	browserInput := current
	if res != nil && strings.TrimSpace(res.NewCookies) != "" {
		browserInput = res.NewCookies
	}
	browserCookies, err := browserRenew(browserInput)
	if err != nil || browserCookies == "" {
		if errors.Is(err, browser.ErrSecurityVerification) {
			return false, err
		}
		return false, nil
	}
	save(browserCookies)
	a.logger.Info("密码登录前浏览器续期成功，跳过密码登录", "account", d.ID)
	return true, nil
}

func (a *Adapter) handlePasswordLoginError(ctx context.Context, cookieID string, err error) {
	msg := err.Error()
	a.logger.Warn("密码登录刷新失败", "account", cookieID, "err", err)
	if isPasswordLoginDisableError(msg) {
		_ = a.store.Cookies.SetStatusWithReason(ctx, cookieID, false, msg)
		a.OnAccountEvent(ctx, cookieID, engine.EventAccountDisabled, engine.AlertLevelCritical, "账号已自动禁用", msg)
		if isPasswordLoginBadCredentials(msg) {
			a.markPasswordError(cookieID)
		}
		return
	}
	if isPasswordLoginRetryableVerification(msg) {
		a.markPasswordError(cookieID)
	}
}

func isPasswordLoginBadCredentials(msg string) bool {
	return strings.Contains(msg, "账密错误") ||
		strings.Contains(msg, "账号密码错误") ||
		strings.Contains(msg, "用户名或密码错误") ||
		strings.Contains(msg, "账号或密码错误") ||
		strings.Contains(msg, "密码错误")
}

func isPasswordLoginDisableError(msg string) bool {
	return isPasswordLoginBadCredentials(msg) ||
		strings.Contains(msg, "账号不存在") ||
		strings.Contains(msg, "账号已被冻结") ||
		strings.Contains(msg, "账号被冻结") ||
		strings.Contains(msg, "账户被冻结") ||
		strings.Contains(msg, "账号已锁定") ||
		strings.Contains(msg, "账户已锁定") ||
		strings.Contains(msg, "操作过于频繁") ||
		strings.Contains(msg, "登录过于频繁") ||
		strings.Contains(msg, "暂时无法登录")
}

func isPasswordLoginRetryableVerification(msg string) bool {
	event := browser.PasswordLoginEventFromMessage(msg)
	return event.Reason == "baxia_punish_captcha" ||
		event.Status == browser.PasswordLoginStatusVerificationRequired ||
		strings.Contains(msg, "人工验证") ||
		strings.Contains(msg, "人脸验证") ||
		strings.Contains(msg, "身份验证")
}

func (a *Adapter) markPasswordError(cookieID string) {
	cooldown := a.cooldown
	if cooldown == nil {
		cooldown = renewal.GlobalCooldown
	}
	cooldown.MarkPasswordError(cookieID)
}

// 编译期保证 *Adapter 同时实现 engine.Handler 与 automation.OrderDetailFetcher。
var (
	_ engine.Handler                = (*Adapter)(nil)
	_ automation.OrderDetailFetcher = (*Adapter)(nil)
)
