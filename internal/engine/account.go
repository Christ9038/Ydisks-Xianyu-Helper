// Package engine 实现单账号运行时：WebSocket 连接生命周期、token 刷新、
// 消息分发主循环（信号量限并发 + 防抖 + 去重）、重连策略。
//
// 业务逻辑（自动发货、回复）在 Phase 3 通过 Handler 接口注入，
// Phase 2 先搭好骨架并跑通"收消息→解密→去重→防抖→回调"。
package engine

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
	"xianyu-go/internal/xianyu/renew"
	"xianyu-go/internal/xianyu/ws"
)

// 账号运行时参数。
// PasswordLoginMinGap 保存密码登录MinGap，供当前处理流程使用
const (
	MaxConnectionFailures       = 5               // 仅保留给显式人工恢复入口和兼容测试
	TokenFetchDisableThreshold  = 100             // 兼容常量；官网运行时不会按次数自动禁用账号
	MessageSemaphoreSize        = 100             // 并发消息处理上限
	MessageDebounceDelay        = 1 * time.Second // 防抖延迟：用户停止发送 1s 后回复
	MessageExpireTime           = time.Hour       // 去重有效期
	ProcessedIDsMaxSize         = 10000           // 去重表上限，超限清理
	HeartbeatInterval           = 15 * time.Second
	PasswordLoginMinGap         = 60 * time.Second
	MaxNetworkFailures          = 20
	FrequentDisconnectLimit     = 5
	FrequentDisconnectWindow    = 5 * time.Minute
	TokenCaptchaFailureCooldown = 5 * time.Minute
	WSRecordBatchSize           = 32
	WSRecordFlushInterval       = 250 * time.Millisecond
	WSRecordWriteTimeout        = 5 * time.Second
	WSRecordRetention           = 7 * 24 * time.Hour

	// ShortConnectionThreshold 仅用于统计频繁短连接；已经建立后的网络断线
	// 不会清 Token 缓存。
	ShortConnectionThreshold = 30 * time.Second
)

// 告警级别（OnAccountAlert 的 level 参数）。
// AlertLevelInfo 保存AlertLevelInfo，供当前处理流程使用
const (
	AlertLevelInfo     = "info"
	AlertLevelWarn     = "warn"
	AlertLevelCritical = "critical"
)

// EventAccountOffline 保存Event账号Offline，供当前处理流程使用
const (
	EventAccountOffline       = "account_offline"
	EventAccountRecovered     = "account_recovered"
	EventAccountDisabled      = "account_disabled"
	EventSecurityVerification = "security_verification"
	EventTokenRenewal         = "token_renewal"
	EventSystemError          = "system_error"
)

// Handler 是业务逻辑注入点（Phase 3 实现）。
// 收到一条防抖后的聊天消息时回调；返回错误仅记录日志、不影响主循环。
// 注：生产 handlerAdapter.HandleChatMessage 当前为 no-op，留作未来注入聊天旁路处理
// （如外部消息持久化）。回复链由 ReplyService.Handle 完成，不依赖本回调。
// Handler 保存Handler，供当前处理流程使用
type Handler interface {
	HandleChatMessage(ctx context.Context, m ChatMessage) error
	// HandleSystemEvent 处理平台系统事件。系统卡片永远不进入 AI 回复链，
	// 这里只把事件交给自动化中心，由自动化规则决定是否执行。
	HandleSystemEvent(ctx context.Context, task automation.Task) error
	// OnPasswordLoginRefresh 是历史接口名；连续失败时只触发 Go 协议续期，
	// 不得启动浏览器密码登录。
	OnPasswordLoginRefresh(ctx context.Context, cookieID string) bool
	// OnAccountAlert 账号告警通知（token 失效/自动恢复失败/风控验证等）。
	// level 取 AlertLevel* 常量。实现方应把告警推送到该账号绑定的通知渠道。
	OnAccountAlert(ctx context.Context, cookieID, level, title, body string)
}

// accountEventHandler 保存账号EventHandler，供当前处理流程使用
type accountEventHandler interface {
	OnAccountEvent(ctx context.Context, cookieID, eventType, level, title, body string)
}

// credentialUpdateHandler 保存credentialUpdateHandler，供当前处理流程使用
type credentialUpdateHandler interface {
	OnCredentialUpdated(ctx context.Context, cookieID string)
}

// transportReadyHandler 保存transportReadyHandler，供当前处理流程使用
type transportReadyHandler interface {
	OnTransportReady(ctx context.Context, cookieID string)
}

// tokenCaptchaHandler 保存令牌CaptchaHandler，供当前处理流程使用
type tokenCaptchaHandler interface {
	OnTokenCaptchaVerification(ctx context.Context, cookieID, cookieStr, verificationURL, deviceID string) (*mtop.RefreshResult, bool)
}

// tokenRefreshStarted 保存令牌RefreshStarted，供当前处理流程使用
const (
	tokenRefreshStarted            = "started"
	tokenRefreshSuccess            = "success"
	tokenRefreshFailedCaptcha      = "failed_captcha"
	tokenRefreshFailedCaptchaError = "failed_captcha_exception"
	tokenRefreshFailedTimeout      = "failed_timeout"
	tokenRefreshFailedNetwork      = "failed_network"
	tokenRefreshFailedAPI          = "failed_api"
	tokenRefreshFailedSession      = "failed_session_expired"
	tokenRefreshSkippedCooldown    = "skipped_cooldown"
)

// errTokenCaptchaCooldown 保存err令牌CaptchaCooldown，供当前处理流程使用
var errTokenCaptchaCooldown = errors.New("token 风控验证冷却中")

// ChatMessage 防抖后投递给业务层的一条聊天消息。
type ChatMessage struct {
	AccountID    string // cookie_id
	CookieStr    string
	ChatID       string
	SenderUserID string
	SenderName   string
	Text         string
	ItemID       string
	Raw          map[string]any // 解密后的完整消息
}

// OutgoingChatMessage is emitted after the existing account WebSocket has
// accepted a text message. It is an observation hook only; persistence errors
// never change the delivery result.
// OutgoingChatMessage 保存Outgoing聊天消息，供当前处理流程使用
type OutgoingChatMessage struct {
	AccountID  string
	ChatID     string
	BuyerID    string
	Text       string
	MessageKey string
}

// outgoingChatHandler 保存outgoing聊天Handler，供当前处理流程使用
type outgoingChatHandler interface {
	HandleOutgoingChatMessage(ctx context.Context, message OutgoingChatMessage) error
}

// outgoingMessageKeyContextKey 保存outgoing消息Key上下文Key，供当前处理流程使用
type outgoingMessageKeyContextKey struct{}

// WithOutgoingMessageKey correlates a UI-created pending message with the
// post-send observer so the same text is not inserted twice.
// WithOutgoingMessageKey 负责WithOutgoing消息Key相关处理。
func WithOutgoingMessageKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, outgoingMessageKeyContextKey{}, strings.TrimSpace(key))
}

// RuntimeStatus 是账号引擎的实时连接状态，不写入数据库。
type RuntimeStatus struct {
	State                 string    `json:"state"`
	Message               string    `json:"message,omitempty"`
	Connected             bool      `json:"connected"`
	Failures              int       `json:"failures"`
	UpdatedAt             time.Time `json:"updated_at"`
	TokenAcquiredAt       time.Time `json:"token_acquired_at,omitempty"`
	TokenExpiresAt        time.Time `json:"token_expires_at,omitempty"`
	TokenRefreshAt        time.Time `json:"token_refresh_at,omitempty"`
	TokenRemainingSeconds int64     `json:"token_remaining_seconds,omitempty"`
	TokenRefreshStatus    string    `json:"token_refresh_status,omitempty"`
}

// RuntimeStarting 保存RuntimeStarting，供当前处理流程使用
const (
	RuntimeStarting             = "starting"
	RuntimeConnecting           = "connecting"
	RuntimeOnline               = "online"
	RuntimeReconnecting         = "reconnecting"
	RuntimeAuthExpired          = "auth_expired"
	RuntimeVerificationRequired = "verification_required"
	RuntimeError                = "error"
	RuntimeStopped              = "stopped"
	tokenRiskRecoveryMessage    = "token 风控验证已处理，正在重新获取登录凭证"
)

// Account 单账号运行时。
type Account struct {
	// CookieID 是账号在数据库中的稳定标识。
	CookieID string
	// accountRuntimeComponents 统一拥有该账号的可变运行状态与任务生命周期。
	accountRuntimeComponents
	// accountDependencies 固定该账号运行时使用的基础设施和业务端口。
	accountDependencies
}

// debounceEntry 保存debounceEntry，供当前处理流程使用
type debounceEntry struct {
	timer    *time.Timer
	lastMsg  ChatMessage
	deadline time.Time
}

// WSConn 是 Account 对 ws 连接的最小契约。*ws.Conn 实现该接口；
// 测试可注入 fakeWSConn 以隔离真实 WS 握手与网络。
// WSConn 保存WSConn，供当前处理流程使用
type WSConn interface {
	Register(ctx context.Context, deviceID, accessToken string) error
	HeartbeatLoop(ctx context.Context, interval time.Duration) error
	ReceiveLoop(ctx context.Context, onMessage func(map[string]any)) error
	Close() error
	SendText(ctx context.Context, myID, cid, toID, text string) error
	SendImage(ctx context.Context, myID, cid, toID, imageURL string, width, height int) error
}

// WSDialer 抽象 WebSocket 打开阶段，便于测试隔离真实网络。
type WSDialer interface {
	Dial(ctx context.Context, cfg ws.Config, logger *slog.Logger) (WSConn, error)
}

// defaultDialer 保存defaultDialer，供当前处理流程使用
type defaultDialer struct{}

// Dial 负责Dial相关处理。
func (defaultDialer) Dial(ctx context.Context, cfg ws.Config, logger *slog.Logger) (WSConn, error) {
	return ws.Open(ctx, cfg, logger)
}

// cookieRenewer 保存登录凭证Renewer，供当前处理流程使用
type cookieRenewer interface {
	RenewAPIFirst(ctx context.Context, cookiesStr string, snapshots ...[]cookierefresh.BrowserCookie) (*renew.Result, error)
}

// loginStatusChecker 保存登录状态Checker，供当前处理流程使用
type loginStatusChecker interface {
	CheckLoginStatusContext(ctx context.Context, cookiesStr string) (*mtop.LoginStatusResult, error)
}

// scopedTokenClient 保存scoped令牌Client，供当前处理流程使用
type scopedTokenClient interface {
	RefreshTokenWithCredentialContext(ctx context.Context, cookiesStr, deviceID string, snapshot []cookierefresh.BrowserCookie) (*mtop.RefreshResult, error)
}

// loginStatusCheckResult 保存登录状态Check结果，供当前处理流程使用
type loginStatusCheckResult struct {
	recovered       bool
	riskRequired    bool
	verificationURL string
}

// Config 构造 Account 所需依赖。
type Config struct {
	CookieID  string
	CookieStr string
	Store     *db.Store
	Handler   Handler
	Logger    *slog.Logger
	// MTop 可选：注入 mtop 客户端以便测试 mock。 nil 时使用默认 HTTP 实现。
	MTop mtop.Client
	// Renewer 可选：注入 Cookie 接口续期服务以便测试 mock。nil 时使用默认实现。
	Renewer cookieRenewer
	// WSDialer 可选：用于测试隔离原生 WebSocket 握手。
	WSDialer WSDialer
}

// New 构造单账号运行时（未启动）。
func New(cfg Config) *Account {
	// logger 保存logger，供当前处理流程使用
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	// mtopWasNil 保存mtopWasNil，供当前处理流程使用
	mtopWasNil := cfg.MTop == nil
	// mtopClient 保存mtopClient，供当前处理流程使用
	mtopClient := cfg.MTop
	if mtopClient == nil {
		mtopClient = mtop.NewClient()
	}
	// renewer 保存renewer，供当前处理流程使用
	renewer := cfg.Renewer
	if renewer == nil && mtopWasNil {
		renewer = renew.Service{}
	}
	// cookies 保存cookies，供当前处理流程使用
	cookies := protocol.TransCookies(cfg.CookieStr)
	// myid 保存myid，供当前处理流程使用
	myid := cookies["unb"]
	// wsDialer 保存wsDialer，供当前处理流程使用
	wsDialer := cfg.WSDialer
	if wsDialer == nil {
		wsDialer = defaultDialer{}
	}
	// a 保存已经完成依赖快照和状态组件装配的账号 facade。
	a := &Account{
		CookieID: cfg.CookieID,
		accountRuntimeComponents: accountRuntimeComponents{
			lifecycle: accountLifecycle{accepting: true},
			accountRuntimeState: accountRuntimeState{
				runtimeState:     RuntimeStarting,
				runtimeMessage:   "正在启动账号服务",
				runtimeUpdatedAt: time.Now(),
			},
			credentialState: credentialState{
				refreshGate:  newRefreshGate(),
				CookieStr:    cfg.CookieStr,
				UserID:       myid,
				deviceID:     protocol.GenerateDeviceID(myid),
				credentialFP: credentialStateFingerprint(cfg.CookieStr, ""),
			},
			pendingRenewal: pendingRenewalCoordinator{},
		},
		accountDependencies: newAccountDependencies(cfg.Store, mtopClient, renewer, wsDialer, cfg.Handler, logger.With("account", cfg.CookieID), nil, newWSRecorder(cfg.Store, cfg.CookieID, logger)),
	}
	if cfg.Store != nil {
		a.reply = NewReplyService(cfg.CookieID, cfg.Store, a, nil, NewAIReplier(cfg.CookieID, cfg.Store, logger), logger)
	}
	a.messageDispatcher = newMessageDispatcher(messageDispatcherConfig{
		CookieID:       cfg.CookieID,
		CurrentCookie:  a.currentCookieStr,
		CurrentHandler: func() Handler { return a.handler },
		Reply:          a.reply,
		Logger:         logger,
		BeginTask:      a.lifecycle.beginTask,
		FinishTask:     a.lifecycle.finishTask,
		RecordMessage:  a.recordMessageReceived,
	})
	// connection 保存绑定当前账号 facade 的连接编排组件；它只在构造完成后才可被 Run 调用。
	a.connection = connectionCoordinator{account: a}
	// outgoing 保存绑定当前账号 facade 的出站消息协调器；它只读取连接快照后执行外部 I/O。
	a.outgoing = outgoingMessageCoordinator{account: a}
	// credentials 保存绑定当前账号 facade 的凭证协调器；外部凭证 I/O 均由它控制锁边界。
	a.credentials = credentialCoordinator{account: a}
	return a
}

// Run 阻塞运行账号主循环，直到 ctx 取消或不可恢复错误。
// 调用方应在独立 goroutine 中运行；Stop 可优雅停止。
// Account 只保留稳定 facade；WebSocket 拨号、注册和重连编排由 connectionCoordinator 独立拥有。
func (a *Account) Run(parent context.Context) error {
	return a.connection.run(parent)
}

// wsRecorder 返回供 WebSocket 连接记录报文的非阻塞回调。
func (a *Account) wsRecorder() func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
	return a.recorder.callback()
}

// startWSRecorder 启动账号级 WebSocket 报文记录 worker。
func (a *Account) startWSRecorder(ctx context.Context) {
	a.recorder.start(ctx)
}

// handleWSConnectFailure 负责handleWSConnectFailure相关处理。
func (a *Account) handleWSConnectFailure(ctx context.Context, err error) error {
	a.clearConnectionToken(ctx)
	// reason 保存原因，供当前处理流程使用
	reason := "消息凭证被拒绝，请重新登录"
	if ws.IsConnectLimitError(err) {
		reason = "消息会话已被服务端移除"
	} else if !ws.IsInvalidTokenError(err) && !ws.IsAuthenticationError(err) {
		// 原生握手 CONNECT_FAILED 和 INVALID_TOKEN 一样进入官网 CONN_ERROR=5；
		// /im 页面不会对该状态自动 reConnect，而是展示重新登录入口。
		reason = "消息服务连接失败，请重新登录"
	}
	a.setRuntimeState(RuntimeAuthExpired, reason)
	a.notifyOffline(ctx, reason)
	return err
}

// acquireFreshConnectionToken mirrors the official web message client:
// authTokenCallback obtains a fresh login.token result before every WebSocket
// loginV2/reConnect and the returned accessToken is used only for that /reg.
// acquireFreshConnectionToken 负责acquireFreshConnection令牌相关处理。
func (a *Account) acquireFreshConnectionToken(ctx context.Context) (string, string, error) {
	return a.refreshToken(ctx)
}

// clearConnectionToken ends the lifetime of the token used by the previous
// WebSocket attempt. The page-runtime device ID remains stable until a Cookie
// update maps to an official page reload.
// clearConnectionToken 负责clearConnection令牌相关处理。
func (a *Account) clearConnectionToken(ctx context.Context) {
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
}

// Stop 优雅停止并兼容不需要错误返回值的旧调用方。
func (a *Account) Stop() {
	// stopCtx、stopCancel 为旧 Stop 入口提供有限的 Join 预算，避免不响应取消的外部实现永久阻塞调用方。
	stopCtx, stopCancel := context.WithTimeout(context.Background(), connectionShutdownJoinTimeout)
	defer stopCancel()
	_ = a.StopContext(stopCtx)
}

// StopContext 在 ctx 约束内停止账号，并等待已登记任务完成。
func (a *Account) StopContext(ctx context.Context) error {
	// cancel 是当前 Run 上下文的取消函数；shouldStop 表示当前调用是否负责首次清理。
	cancel, shouldStop, err := a.lifecycle.stopContext(ctx)
	if err != nil {
		return err
	}
	if !shouldStop {
		return nil
	}
	a.setRuntimeState(RuntimeStopped, "账号服务已停止")
	if cancel != nil {
		cancel()
	}
	// 取消所有防抖定时器；回调任务仍由 lifecycle 统一等待。
	a.stop()

	if !a.lifecycle.waitContext(ctx) {
		a.logger.Warn("等待账号业务任务退出超时")
		if ctx == nil || ctx.Err() == nil {
			return context.DeadlineExceeded
		}
		return ctx.Err()
	}
	if a.recorder != nil && !a.recorder.waitContext(ctx) {
		a.logger.Warn("等待账号 WS 记录 worker 退出超时")
		if ctx == nil || ctx.Err() == nil {
			return context.DeadlineExceeded
		}
		return ctx.Err()
	}
	return nil
}

// beginTask 负责begin任务相关处理。
func (a *Account) beginTask() (context.Context, bool) {
	return a.lifecycle.beginTask()
}

// handleMaxFailures 是历史兼容恢复入口；只尝试 Go 协议续期，不执行密码登录。
func (a *Account) handleMaxFailures(ctx context.Context) error {
	// 先执行低成本登录态检查。它可能仅凭 loginuser.get 响应头恢复签名
	// Cookie，也能在进入静默续期前准确识别风控状态。
	// loginStatus 保存登录状态，供当前处理流程使用
	loginStatus := a.tryLoginStatusCheck(ctx)
	if loginStatus.riskRequired {
		return fmt.Errorf("账号 %s 需要完成安全验证", a.CookieID)
	}
	if loginStatus.recovered {
		a.logger.Info("登录态检查已恢复 Cookie，重置失败计数")
		a.setRuntimeState(RuntimeConnecting, "登录凭证已刷新，正在重新连接")
		a.resetFailures()
		return sleepCtx(ctx, 2*time.Second)
	}
	a.logger.Warn("连续失败达上限，触发 Go 协议续期", "failures", MaxConnectionFailures)
	a.notifyRecoveringOffline(ctx, fmt.Sprintf("消息服务连续认证/连接失败 %d 次，开始自动恢复", MaxConnectionFailures))
	// passwordRefreshResult 保存外部凭证恢复调用结果；恢复调用不得持有账号凭证锁。
	passwordRefreshResult := a.handler != nil && a.handler.OnPasswordLoginRefresh(ctx, a.CookieID)
	if passwordRefreshResult {
		if cookieValue, err := a.store.Cookies.GetValue(ctx, a.CookieID); err == nil && cookieValue != "" { // cookieValue 是恢复回调刚写入的 Cookie 明文。
			a.replaceCookieStr(cookieValue)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
		}
		a.logger.Info("Go 协议续期成功，重置失败计数")
		a.setRuntimeState(RuntimeConnecting, "登录凭证已刷新，正在重新连接")
		a.resetFailures()
		return sleepCtx(ctx, 2*time.Second)
	}
	a.clearTokenCache(ctx)
	a.setRuntimeState(RuntimeAuthExpired, "登录凭证已失效，自动恢复失败")
	if a.markAuthExpired() {
		a.alertEvent(ctx, EventAccountOffline, AlertLevelCritical, "账号自动恢复失败，需人工处理",
			fmt.Sprintf("账号 %s 连续失败 %d 次，登录凭证未能自动恢复。", a.CookieID, MaxConnectionFailures))
	}
	return fmt.Errorf("账号 %s 登录凭证自动恢复失败", a.CookieID)
}

// markAuthExpired 标记进入 auth_expired 状态。仅在首次（未告警过）返回 true，
// 连接恢复后由 Run 的 online 分支复位 authExpiredAlerted，避免重复告警刷屏。
// markAuthExpired 负责markAuthExpired相关处理。
func (a *Account) markAuthExpired() bool {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	if a.authExpiredAlerted {
		return false
	}
	a.authExpiredAlerted = true
	return true
}

// notifyOffline 负责notifyOffline相关处理。
func (a *Account) notifyOffline(ctx context.Context, reason string) {
	if !a.markOfflineNotified(reason) {
		return
	}
	a.alertEvent(ctx, EventAccountOffline, AlertLevelWarn, "账号已掉线，需要重新登录",
		fmt.Sprintf("账号 %s 的闲鱼消息连接已进入不可自动重连状态。原因：%s。请更新登录信息或重新登录后再启动账号。", a.CookieID, reason))
}

// notifyRecoveringOffline 负责notifyRecoveringOffline相关处理。
func (a *Account) notifyRecoveringOffline(ctx context.Context, reason string) {
	if !a.markOfflineNotified(reason) {
		return
	}
	a.alertEvent(ctx, EventAccountOffline, AlertLevelWarn, "账号已掉线，正在自动恢复",
		fmt.Sprintf("账号 %s 出现登录凭证过期或认证掉线。原因：%s。系统会先发送本通知，再继续尝试 Go 协议续期；如仍失败则需要重新扫码登录。", a.CookieID, reason))
}

// markOfflineNotified 负责markOfflineNotified相关处理。
func (a *Account) markOfflineNotified(reason string) bool {
	a.runtimeMu.Lock()
	if a.offlineNotified {
		a.runtimeMu.Unlock()
		return false
	}
	a.offlineNotified = true
	a.offlineSince = time.Now()
	a.lastOfflineReason = reason
	a.runtimeMu.Unlock()
	return true
}

// alert 触发账号告警通知。handler 未注入或为 nil 时静默跳过。
func (a *Account) alert(ctx context.Context, level, title, body string) {
	a.alertEvent(ctx, EventTokenRenewal, level, title, body)
}

// alertEvent 负责alertEvent相关处理。
func (a *Account) alertEvent(ctx context.Context, eventType, level, title, body string) {
	if a.handler == nil {
		return
	}
	if // h、ok 保存h、ok，供当前处理流程使用
	h, ok := a.handler.(accountEventHandler); ok {
		h.OnAccountEvent(ctx, a.CookieID, eventType, level, title, body)
		return
	}
	a.handler.OnAccountAlert(ctx, a.CookieID, level, title, body)
}

// resetFailures 负责resetFailures相关处理。
func (a *Account) resetFailures() {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	a.connFailures = 0
}

// formatTimeOrUnknown 负责format时间OrUnknown相关处理。
func formatTimeOrUnknown(t time.Time) string {
	if t.IsZero() {
		return "未知"
	}
	return t.Format("2006-01-02 15:04:05")
}

// retryDelay 按错误类型计算退避，并加入 0-30% 抖动。
// 多账号同时断线时，纯固定退避会让所有账号在同一秒重连，容易形成重连风暴。
// retryDelay 负责重试延迟相关处理。
func (a *Account) retryDelay(errMsg string) time.Duration {
	a.runtimeMu.Lock()
	// f 保存f，供当前处理流程使用
	f := a.connFailures
	a.runtimeMu.Unlock()
	if f < 1 {
		f = 1
	}
	// base 保存base，供当前处理流程使用
	base := exponentialSeconds(f)
	// secs 保存secs，供当前处理流程使用
	secs := 0
	switch {
	case contains(errMsg, "no close frame received or sent"):
		secs = min(base, 30)
	case contains(errMsg, "connection refused") || contains(errMsg, "timeout"):
		secs = min(2*base, 90)
	default:
		secs = min(base, 45)
	}
	return withRetryJitter(time.Duration(secs) * time.Second)
}

// networkRetryDelay 负责network重试延迟相关处理。
func (a *Account) networkRetryDelay() time.Duration {
	a.runtimeMu.Lock()
	// f 保存f，供当前处理流程使用
	f := a.networkFailures
	a.runtimeMu.Unlock()
	if f < 1 {
		f = 1
	}
	return withRetryJitter(time.Duration(min(2+exponentialSeconds(f), 60)) * time.Second)
}

// exponentialSeconds 负责exponential秒数相关处理。
func exponentialSeconds(failures int) int {
	if failures < 1 {
		failures = 1
	}
	if failures > 30 {
		failures = 30
	}
	return 1 << failures
}

// withRetryJitter 负责with重试Jitter相关处理。
func withRetryJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	// maxJitter 保存maxJitter，供当前处理流程使用
	maxJitter := base * 3 / 10
	if maxJitter <= 0 {
		return base
	}
	// n、err 保存n、err，供当前处理流程使用
	n, err := rand.Int(rand.Reader, big.NewInt(int64(maxJitter)))
	if err != nil {
		// 熵源异常时使用时间纳秒兜底；这里只影响退避抖动，不用于安全令牌。
		return base + time.Duration(time.Now().UnixNano()%int64(maxJitter))
	}
	return base + time.Duration(n.Int64())
}

// isEstablishedNetworkError 负责isEstablishedNetwork错误相关处理。
func isEstablishedNetworkError(err error) bool {
	if err == nil {
		return false
	}
	// msg 保存msg，供当前处理流程使用
	msg := strings.ToLower(err.Error())
	// marker 表示当前遍历过程中的marker
	for _, marker := range []string{
		"connectionclosed", "no close frame received or sent", "connection reset",
		"connectionreseterror", "timeouterror", "timeout", "websocket: close",
		"received close frame", "failed to read frame", "unexpected eof", " eof",
	} {
		if strings.Contains(msg, marker) {
			return true
		}
	}
	return false
}

// recordShortDisconnect 负责recordShortDisconnect相关处理。
func (a *Account) recordShortDisconnect(connectedDuration time.Duration) bool {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	if connectedDuration >= ShortConnectionThreshold {
		a.shortDisconnects = nil
		return false
	}
	// now 保存now，供当前处理流程使用
	now := time.Now()
	a.shortDisconnects = append(a.shortDisconnects, now)
	// cutoff 保存cutoff，供当前处理流程使用
	cutoff := now.Add(-FrequentDisconnectWindow)
	// kept 保存kept，供当前处理流程使用
	kept := a.shortDisconnects[:0]
	// disconnectedAt 表示当前遍历过程中的disconnectedAt
	for _, disconnectedAt := range a.shortDisconnects {
		if !disconnectedAt.Before(cutoff) {
			kept = append(kept, disconnectedAt)
		}
	}
	a.shortDisconnects = kept
	return len(a.shortDisconnects) >= FrequentDisconnectLimit
}

// acquireToken is kept for internal callers and tests, but intentionally does
// not reuse the persisted accessToken. Official web reconnects always invoke
// the login.token API before /reg.
// acquireToken 负责acquire令牌相关处理。
func (a *Account) acquireToken(ctx context.Context) (string, string, error) {
	return a.acquireTokenWithMinGap(ctx, false)
}

// acquireRuntimeToken is retained as a compatibility wrapper for focused
// tests and older internal callers. It follows the same fresh-token rule.
// acquireRuntimeToken 负责acquireRuntime令牌相关处理。
func (a *Account) acquireRuntimeToken(ctx context.Context) (string, string, error) {
	return a.acquireFreshConnectionToken(ctx)
}

// acquireTokenWithMinGap 负责acquire令牌WithMinGap相关处理。
func (a *Account) acquireTokenWithMinGap(ctx context.Context, _ bool) (string, string, error) {
	// Invalidate any access token left by an older process/attempt before asking
	// MTOP for the token that will be bound to this connection.
	a.clearTokenCache(ctx)
	return a.refreshToken(ctx)
}

// setLastTokenStatus 负责setLast令牌状态相关处理。
func (c *credentialCoordinator) setLastTokenStatus(status string) {
	// a 是本凭证协调器绑定的账号 facade，持有刷新诊断状态。
	a := c.account
	a.mu.Lock()
	a.lastTokenStatus = status
	a.mu.Unlock()
}

// classifyTokenFailure 负责classify令牌Failure相关处理。
func classifyTokenFailure(err error) string {
	if err == nil {
		return tokenRefreshFailedAPI
	}
	if mtop.IsSessionExpiredErr(err) {
		return tokenRefreshFailedSession
	}
	if errors.Is(err, context.DeadlineExceeded) || strings.Contains(strings.ToLower(err.Error()), "timeout") || strings.Contains(err.Error(), "超时") {
		return tokenRefreshFailedTimeout
	}
	// msg 保存msg，供当前处理流程使用
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "network") || strings.Contains(msg, "connection") || strings.Contains(msg, "请求失败") {
		return tokenRefreshFailedNetwork
	}
	return tokenRefreshFailedAPI
}

// tokenFailureIsNonCounted 负责令牌FailureIsNonCounted相关处理。
func tokenFailureIsNonCounted(status string) bool {
	switch status {
	case tokenRefreshFailedCaptcha, tokenRefreshFailedCaptchaError, tokenRefreshSkippedCooldown:
		return true
	default:
		return false
	}
}

// RuntimeStatus 返回账号当前连接状态的线程安全快照。
func (a *Account) RuntimeStatus() RuntimeStatus {
	a.runtimeMu.Lock()
	// state 是当前运行状态枚举快照。
	state := a.runtimeState
	// message 是当前运行状态说明快照。
	message := a.runtimeMessage
	// connected 是当前连接存在且状态为 online 的快照。
	connected := a.conn != nil && a.runtimeState == RuntimeOnline
	// failures 是连续连接失败次数快照。
	failures := a.connFailures
	// updatedAt 是状态最近更新时间快照。
	updatedAt := a.runtimeUpdatedAt
	a.runtimeMu.Unlock()

	a.mu.Lock()
	// remaining 保存remaining，供当前处理流程使用
	remaining := int64(0)
	if !a.tokenExpiresAt.IsZero() {
		remaining = int64(time.Until(a.tokenExpiresAt).Seconds())
		if remaining < 0 {
			remaining = 0
		}
	}
	// status 保存状态，供当前处理流程使用
	status := RuntimeStatus{
		State:                 state,
		Message:               message,
		Connected:             connected,
		Failures:              failures,
		UpdatedAt:             updatedAt,
		TokenAcquiredAt:       a.tokenAcquiredAt,
		TokenExpiresAt:        a.tokenExpiresAt,
		TokenRefreshAt:        a.tokenRefreshAt,
		TokenRemainingSeconds: remaining,
		TokenRefreshStatus:    a.lastTokenStatus,
	}
	a.mu.Unlock()
	return status
}

// recordMessageReceived 更新最近收到消息时间，供 messageDispatcher 使用。
func (a *Account) recordMessageReceived(receivedAt time.Time) {
	a.runtimeMu.Lock()
	a.lastMsgReceived = receivedAt
	a.runtimeMu.Unlock()
}

// tokenRetryDelay 负责令牌重试延迟相关处理。
func (a *Account) tokenRetryDelay() time.Duration {
	a.mu.Lock()
	// expiresAt 保存expiresAt，供当前处理流程使用
	expiresAt := a.tokenExpiresAt
	// failures 保存failures，供当前处理流程使用
	failures := a.tokenFetchFailures
	a.mu.Unlock()
	// delay 保存延迟，供当前处理流程使用
	delay := time.Minute
	if failures > 1 {
		delay = 2 * time.Minute
	}
	if !expiresAt.IsZero() && time.Until(expiresAt) <= 2*time.Minute {
		delay = 30 * time.Second
	}
	return delay
}

// notifyTransportReady 负责notifyTransportReady相关处理。
func (a *Account) notifyTransportReady(ctx context.Context) {
	if // handler、ok 保存handler、ok，供当前处理流程使用
	handler, ok := a.handler.(transportReadyHandler); ok {
		handler.OnTransportReady(ctx, a.CookieID)
	}
}

// setRuntimeState 负责setRuntime状态相关处理。
func (a *Account) setRuntimeState(state, message string) {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	a.runtimeState = state
	a.runtimeMessage = message
	a.runtimeUpdatedAt = time.Now()
}

// setRuntimeError 负责setRuntime错误相关处理。
func (a *Account) setRuntimeError(ctx context.Context, err error) {
	// msg 保存msg，供当前处理流程使用
	msg := strings.ToLower(errString(err))
	a.runtimeMu.Lock()
	// prev 保存prev，供当前处理流程使用
	prev := a.runtimeState
	a.runtimeMu.Unlock()
	switch {
	case strings.Contains(msg, "验证"), strings.Contains(msg, "captcha"), strings.Contains(msg, "risk"), strings.Contains(msg, "rgv587"), strings.Contains(msg, "fail_sys_user_validate"):
		a.setRuntimeState(RuntimeVerificationRequired, "闲鱼要求安全验证，请重新扫码并完成验证")
		// 仅在从非验证状态转入时告警一次，避免重复刷屏。
		if prev != RuntimeVerificationRequired {
			a.alertEvent(ctx, EventSecurityVerification, AlertLevelWarn, "闲鱼要求安全验证",
				"账号触发闲鱼风控验证（滑块/短信/人脸等）。系统可能无法自动恢复，请前往后台扫码完成验证。")
		}
	case strings.Contains(msg, "登录凭证已失效"), strings.Contains(msg, "fail_sys_token_exoired"), strings.Contains(msg, "fail_sys_token_expired"), strings.Contains(msg, "cookie 缺少 unb"):
		a.setRuntimeState(RuntimeAuthExpired, "登录凭证已失效，请重新扫码登录")
	default:
		a.setRuntimeState(RuntimeReconnecting, "连接异常，系统将在限速后自动重试")
	}
}

// SendText 通过当前 WebSocket 给买家发送文本消息。
func (a *Account) SendText(ctx context.Context, chatID, toUserID, text string) error {
	return a.outgoing.sendText(ctx, chatID, toUserID, text)
}

// SendImage 通过当前 WebSocket 给买家发送图片消息。当前仅支持可直接访问的 CDN/公网 URL。
func (a *Account) SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64) error {
	return a.outgoing.sendImage(ctx, chatID, toUserID, imageURL, cardID)
}

// FetchChatHistory reuses the account's registered IM connection. Keeping this
// optional capability outside WSConn avoids forcing non-chat test transports to
// implement history retrieval.
// FetchChatHistory 负责Fetch聊天History相关处理。
func (a *Account) FetchChatHistory(ctx context.Context, chatID string, cursor int64, limit int) (map[string]any, string, error) {
	return a.outgoing.fetchChatHistory(ctx, chatID, cursor, limit)
}

// FetchChatConversations 负责Fetch聊天Conversations相关处理。
func (a *Account) FetchChatConversations(ctx context.Context, cursor int64, limit int) (map[string]any, string, error) {
	return a.outgoing.fetchChatConversations(ctx, cursor, limit)
}

// AutomationReady 报告自动化消息是否可以立即使用当前 WS 连接发送。
func (a *Account) AutomationReady() bool {
	return a.outgoing.automationReady()
}

// rotatePageDeviceID 对应官网 auto-login 成功后的 location.reload()：
// 新 FishEngine 使用新的 UUID-userID，普通 Set-Cookie 与自然重连不会调用它。
// rotatePageDeviceID 负责rotate页码DeviceID相关处理。
func (a *Account) rotatePageDeviceID() {
	a.mu.Lock()
	// userID 保存用户ID，供当前处理流程使用
	userID := a.UserID
	if userID == "" {
		userID = protocol.TransCookies(a.CookieStr)["unb"]
	}
	a.deviceID = protocol.GenerateDeviceID(userID)
	a.mu.Unlock()
}
