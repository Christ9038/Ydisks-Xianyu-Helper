// Package engine 实现单账号运行时：WebSocket 连接生命周期、token 刷新、
// 消息分发主循环（信号量限并发 + 防抖 + 去重）、重连策略。
//
// 业务逻辑（自动发货、回复）在 Phase 3 通过 Handler 接口注入，
// Phase 2 先搭好骨架并跑通"收消息→解密→去重→防抖→回调"。
package engine

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"log/slog"
	"math/big"
	"strings"
	"sync"
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
	// credentialState 独立保护 Cookie、Token、设备指纹和刷新状态。
	credentialState

	// accountRuntimeState 独立保护连接状态、失败计数和离线告警状态。
	accountRuntimeState
	// lifecycle 独立保护业务任务接入、取消和等待语义。
	lifecycle accountLifecycle
	// messageDispatcher 独立保护消息去重、防抖和并发投递状态。
	messageDispatcher

	store    *db.Store
	mtop     mtop.Client
	renewer  cookieRenewer
	wsDialer WSDialer
	handler  Handler
	logger   *slog.Logger

	reply *ReplyService
	// recorder 管理 WebSocket 报文记录 worker 的队列和等待。
	recorder *wsRecorder

	pendingRenewWG sync.WaitGroup
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
	// a 保存a，供当前处理流程使用
	a := &Account{
		CookieID:  cfg.CookieID,
		store:     cfg.Store,
		mtop:      mtopClient,
		renewer:   renewer,
		wsDialer:  wsDialer,
		handler:   cfg.Handler,
		logger:    logger.With("account", cfg.CookieID),
		lifecycle: accountLifecycle{accepting: true},
		accountRuntimeState: accountRuntimeState{
			runtimeState:     RuntimeStarting,
			runtimeMessage:   "正在启动账号服务",
			runtimeUpdatedAt: time.Now(),
		},
		recorder: newWSRecorder(cfg.Store, cfg.CookieID, logger),
		credentialState: credentialState{
			CookieStr:    cfg.CookieStr,
			UserID:       myid,
			deviceID:     protocol.GenerateDeviceID(myid),
			credentialFP: credentialStateFingerprint(cfg.CookieStr, ""),
		},
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
	return a
}

// Run 阻塞运行账号主循环，直到 ctx 取消或不可恢复错误。
// 调用方应在独立 goroutine 中运行；Stop 可优雅停止。
// Run 执行当前值。
func (a *Account) Run(parent context.Context) error {
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(parent)
	defer func() {
		cancel()
		if a.recorder != nil {
			a.recorder.wait()
		}
		a.pendingRenewWG.Wait()
	}()
	a.lifecycle.start(ctx, cancel)
	if a.store != nil && a.store.Cookies != nil && !a.store.Cookies.GetStatus(ctx, a.CookieID) {
		a.logger.Info("账号在启动续期前已禁用")
		return nil
	}
	a.startWSRecorder(ctx)
	// 官网 /im 启动时执行 auto-login plugin；成功后 location.reload() 会重建
	// FishEngine 和页面级 device ID。Go 客户端用 HTTP 复刻续期，并在成功时
	// 只重建这一本地运行时身份。续期失败不能用网页 DOM 阻断 token + WS；
	// Chromium 仅用于读取本机指纹和处理 token 滑块。
	if a.renewer != nil {
		if a.tryAPIRenew(ctx) {
			a.rotatePageDeviceID()
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
	}
	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// 账号是否被禁用。
		if !a.store.Cookies.GetStatus(ctx, a.CookieID) {
			a.logger.Info("账号已禁用，停止主循环")
			return nil
		}

		// 每次新建 IM 连接前吸收数据库中的最新 Cookie。健康连接不会被续期任务
		// 主动打断；Cookie 变化只会使本次重连放弃旧 token 并重新派生。
		a.reloadCookieFromDB(ctx)

		// 官网先完成原生 WebSocket 握手，再从 authTokenCallback 获取本次
		// 连接专用 token，最后发送 /reg。
		// conn、err 保存conn、err，供当前处理流程使用
		conn, err := a.wsDialer.Dial(ctx, ws.Config{Recorder: a.wsRecorder()}, a.logger)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.logger.Error("WS 握手失败", "err", err)
			if // retryErr 保存重试Err，供当前处理流程使用
			retryErr := a.handleWSConnectFailure(ctx, err); retryErr != nil {
				return retryErr
			}
			continue
		}
		// The official web client calls
		// mtop.taobao.idlemessage.pc.login.token from authTokenCallback for every
		// loginV2/reConnect attempt. Do the same here: an access token belongs to
		// one connection attempt and must never be reused for a later /reg.
		// token、cookieStr、err 保存token、cookieStr、err，供当前处理流程使用
		token, cookieStr, err := a.acquireFreshConnectionToken(ctx)
		if err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.logger.Error("获取 token 失败", "err", err)
			a.mu.Lock()
			// status 保存状态，供当前处理流程使用
			status := a.lastTokenStatus
			// nonCounted 保存nonCounted，供当前处理流程使用
			nonCounted := tokenFailureIsNonCounted(status)
			if !nonCounted {
				a.tokenFetchFailures++
			}
			// tokenFailures 保存令牌Failures，供当前处理流程使用
			tokenFailures := a.tokenFetchFailures
			a.mu.Unlock()
			a.setRuntimeError(ctx, err)
			_ = tokenFailures // 仅用于诊断；官网不会按次数永久禁用账号。
			if mtop.IsRiskVerificationErr(err) {
				a.logger.Warn("闲鱼要求安全验证，停止本次消息登录", "err", err)
				a.alertEvent(ctx, EventSecurityVerification, AlertLevelWarn, "闲鱼要求安全验证",
					"账号触发闲鱼风控验证（滑块/人脸等），需要重新登录或完成人工验证。")
				return err
			}
			if mtop.IsSessionExpiredErr(err) {
				// reason 保存原因，供当前处理流程使用
				reason := "登录凭证已失效，正在立即续期"
				a.logger.Warn("token API 检测到 Session 过期，停止重试并开始即时续期", "err", err)
				a.clearTokenCache(ctx)
				a.setRuntimeState(RuntimeReconnecting, reason)
				a.notifyOffline(ctx, reason+"："+errString(err))
				if a.handler != nil && a.handler.OnPasswordLoginRefresh(ctx, a.CookieID) {
					a.reloadCookieFromDB(ctx)
					a.clearCurrentToken()
					a.resetFailures()
					a.setRuntimeState(RuntimeConnecting, "Session 续期成功，正在重新连接")
					continue
				}
				reason = "登录凭证已失效，自动续期失败，请重新扫码登录"
				a.setRuntimeState(RuntimeAuthExpired, reason)
				a.notifyOffline(ctx, reason+"："+errString(err))
				return err
			}
			// 网络或服务端瞬时错误不能让账号运行时永久退出。只要登录 Session
			// 没有明确失效，就持续重试获取连接级 Token。
			a.setRuntimeState(RuntimeReconnecting, "获取消息凭证失败，正在重试")
			a.notifyOffline(ctx, "获取消息凭证失败，正在自动重试："+errString(err))
			if // sleepErr 保存sleepErr，供当前处理流程使用
			sleepErr := sleepCtx(ctx, a.tokenRetryDelay()); sleepErr != nil {
				return sleepErr
			}
			continue
		}
		a.mu.Lock()
		a.currentToken = token
		a.CookieStr = cookieStr
		a.tokenFetchFailures = 0
		a.mu.Unlock()
		a.setRuntimeState(RuntimeConnecting, "登录凭证有效，正在连接消息服务")
		a.logger.Info("token 刷新成功")

		// 2) 使用刚获得的 token 注册已经打开的 WS。
		a.mu.Lock()
		// deviceID 保存deviceID，供当前处理流程使用
		deviceID := a.deviceID
		// tokenCredentialFP 保存令牌CredentialFP，供当前处理流程使用
		tokenCredentialFP := a.tokenCredentialFP
		a.mu.Unlock()
		// registerResult 是凭证快照复核与 WebSocket 注册的窄边界结果。
		registerResult := a.registerConnection(ctx, conn, deviceID, token, tokenCredentialFP)
		if !registerResult.Registered {
			_ = conn.Close()
			a.reloadCookieFromDB(ctx)
			continue
		}
		err = registerResult.Err
		if err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.logger.Error("WS 注册失败", "err", err)
			if // retryErr 保存重试Err，供当前处理流程使用
			retryErr := a.handleWSConnectFailure(ctx, err); retryErr != nil {
				return retryErr
			}
			continue
		}
		a.runtimeMu.Lock()
		a.conn = conn
		a.connStartedAt = time.Now()
		a.connFailures = 0
		a.networkFailures = 0
		a.authExpiredAlerted = false // 连接成功，复位 auth_expired 告警标记
		shouldRecovered := a.offlineNotified
		// offlineSince 保存offlineSince，供当前处理流程使用
		offlineSince := a.offlineSince
		a.offlineNotified = false
		a.offlineSince = time.Time{}
		a.lastOfflineReason = ""
		a.runtimeMu.Unlock()
		a.setRuntimeState(RuntimeOnline, "消息服务连接正常")
		a.notifyTransportReady(ctx)
		if shouldRecovered {
			a.alertEvent(ctx, EventAccountRecovered, AlertLevelInfo, "账号已恢复在线",
				fmt.Sprintf("账号 %s 已重新连接闲鱼消息服务。掉线开始时间：%s。", a.CookieID, formatTimeOrUnknown(offlineSince)))
		}

		// 3) 健康连接维持心跳和收包，并在服务端 Token 过期前主动关闭，
		// 进入下一轮连接以重新调用 Token API 和 /reg。
		// refreshAt 和 expiresAt 是本次连接 Token 的轮换时间与服务端过期时间快照。
		a.mu.Lock()
		// refreshAt 保存refreshAt，供当前处理流程使用
		refreshAt := a.tokenRefreshAt
		// expiresAt 保存expiresAt，供当前处理流程使用
		expiresAt := a.tokenExpiresAt
		a.mu.Unlock()
		// session 是本次已注册连接的心跳、接收和 Token 轮换收束结果。
		session := a.runConnectionSession(ctx, conn, refreshAt)
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 连接结束：只有认证类失败才清 token。已经建立后的网络断线继续
		// 使用内存 token 与数据库缓存，避免无意义调用 Token API。
		if session.Rotated {
			a.logger.Info("WS Token 到达提前轮换时间，正在重新获取 Token", "expires_at", expiresAt, "remaining", time.Until(expiresAt).Round(time.Second))
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "WS Token 即将到期，正在主动轮换")
			continue
		}
		if ws.IsConnectLimitError(session.ReceiveErr) {
			a.clearConnectionToken(ctx)
			// reason 保存原因，供当前处理流程使用
			reason := "消息会话已被服务端移除"
			a.setRuntimeState(RuntimeAuthExpired, reason)
			a.notifyOffline(ctx, reason)
			return nil
		}
		if ws.IsAuthenticationError(session.ReceiveErr) {
			// 官网把 /push/kickout 转成 UNCONNECTED，页面监听器随后立即
			// reConnect，并由 authTokenCallback 获取新的连接凭证。
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "消息会话被服务端踢下线，正在重新连接")
			continue
		}
		// 心跳失败会先关闭连接，ReceiveLoop 往往只观察到 context canceled。
		// 官网以心跳 Promise 的 reject 为真实断线原因并立即 reConnect。
		// receiveErr 是后续错误分类使用的本地副本；心跳错误优先于 context canceled。
		// receiveErr 保存receiveErr，供当前处理流程使用
		receiveErr := session.ReceiveErr
		if session.HeartbeatErr != nil && !errors.Is(session.HeartbeatErr, context.Canceled) &&
			(receiveErr == nil || errors.Is(receiveErr, context.Canceled)) {
			receiveErr = session.HeartbeatErr
		}

		// 正常 close 的 async-for 会直接进入下一轮，不计任何失败。
		if receiveErr == nil {
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "消息连接已结束，正在重新连接")
			continue
		}

		if isEstablishedNetworkError(receiveErr) || errors.Is(receiveErr, context.Canceled) {
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "网络连接已断开，正在重新连接")
			a.logger.Warn("WS 网络连接结束", "err", receiveErr, "connected_duration", session.ConnectedDuration.Round(time.Second), "heartbeat_err", session.HeartbeatErr)
			// 官网当前页面在 CONN_UNCONNECTED 事件后立即调用 reConnect。
			continue
		}

		// 其他已经建立后的非认证错误同样进入 UNCONNECTED；不升级为
		// 密码登录、指数退避或永久禁用。
		a.clearConnectionToken(ctx)
		a.setRuntimeState(RuntimeReconnecting, "消息连接已断开，正在重新连接")
		a.logger.Warn("WS 连接结束", "err", receiveErr, "heartbeat_err", session.HeartbeatErr)
		continue
	}
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

// Stop 优雅停止。
func (a *Account) Stop() {
	// cancel 是当前 Run 上下文的取消函数；shouldStop 表示当前调用是否负责首次清理。
	cancel, shouldStop := a.lifecycle.stop()
	if !shouldStop {
		return
	}
	defer a.lifecycle.finishStop()
	a.setRuntimeState(RuntimeStopped, "账号服务已停止")
	if cancel != nil {
		cancel()
	}
	// 取消所有防抖定时器；回调任务仍由 lifecycle 统一等待。
	a.stop()

	if !a.lifecycle.wait(10 * time.Second) {
		a.logger.Warn("等待账号业务任务退出超时")
	}
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
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	a.logger.Warn("连续失败达上限，触发 Go 协议续期", "failures", MaxConnectionFailures)
	a.notifyRecoveringOffline(ctx, fmt.Sprintf("消息服务连续认证/连接失败 %d 次，开始自动恢复", MaxConnectionFailures))
	if a.handler != nil && a.handler.OnPasswordLoginRefresh(ctx, a.CookieID) {
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

// tryLoginStatusCheck 调用 mtop.taobao.idlemessage.pc.loginuser.get 做轻量登录态确认。
// 这个接口的成本低于完整 token 刷新，且可能顺手下发新的签名 Cookie；
// 因此在 session 失效后、接口续期前先跑一遍，避免已实现的登录态检查能力闲置。
// tryLoginStatusCheck 负责try登录状态Check相关处理。
func (a *Account) tryLoginStatusCheck(ctx context.Context) loginStatusCheckResult {
	// checker、ok 保存checker、ok，供当前处理流程使用
	checker, ok := a.mtop.(loginStatusChecker)
	if !ok {
		return loginStatusCheckResult{}
	}
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := func() {}
	// credentialLocked 标识当前调用是否持有账号凭证锁。
	credentialLocked := false
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
		credentialLocked = true
	}
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
	}()
	a.mu.Lock()
	// cookieStr 保存登录凭证Str，供当前处理流程使用
	cookieStr := a.CookieStr
	a.mu.Unlock()
	// requestCtx 保存请求Ctx，供当前处理流程使用
	requestCtx := ctx
	// cookieSession 保存登录凭证会话，供当前处理流程使用
	var cookieSession *mtop.CookieSession
	// metadataJSON 保存metadataJSON，供当前处理流程使用
	metadataJSON := ""
	if a.store != nil && a.store.Cookies != nil {
		runtimeData, detailErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含登录态检查所需的 Cookie 与 metadata。
		if detailErr != nil {
			a.logger.Warn("登录态检查前读取最新 Cookie 失败", "err", detailErr)
			return loginStatusCheckResult{}
		}
		cookieStr = runtimeData.Value
		metadataJSON = runtimeData.MetadataJSON
		// runtimeData 已在凭证锁内读取，下面只负责根据 metadata 建立 Cookie 会话。
		// metadataJSON 保留完整 Jar 信息，不能退化为仅使用扁平 Cookie。
		// snapshot 分支继续沿用登录态检查原有的请求作用域和持久化顺序。
		if // snapshot、complete 保存snapshot、complete，供当前处理流程使用
		snapshot, complete := cookierefresh.SnapshotFromMetadataOK(metadataJSON); complete {
			requestCtx, cookieSession = mtop.WithCookieSnapshot(ctx, snapshot)
		} else {
			requestCtx, cookieSession = mtop.WithFlatCookieSession(ctx, cookieStr)
		}
	}
	// 登录态检查只使用当前凭证快照；慢速外部调用不得持有共享账号锁。
	if credentialLocked {
		credentialUnlock()
		credentialLocked = false
	}
	// res、err 保存res、err，供当前处理流程使用
	res, err := checker.CheckLoginStatusContext(requestCtx, cookieStr)
	if a.store != nil && a.store.Cookies != nil {
		// credentialUnlock 保存外部检查完成后重新进入提交临界区的释放函数。
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
		credentialLocked = true
		// latestRuntimeData 和 reloadErr 保存外部检查完成后的最新凭证视图及重读错误。
		latestRuntimeData, reloadErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID)
		if reloadErr != nil {
			a.logger.Warn("登录态检查完成后读取最新 Cookie 失败", "err", reloadErr)
			return loginStatusCheckResult{}
		}
		// credentialSnapshotChanged 表示外部检查期间已有其他流程更新 Cookie 或 metadata。
		credentialSnapshotChanged := latestRuntimeData.Value != cookieStr || latestRuntimeData.MetadataJSON != metadataJSON
		cookieStr = latestRuntimeData.Value
		metadataJSON = latestRuntimeData.MetadataJSON
		if credentialSnapshotChanged {
			// 外部响应基于旧快照，当前切片不具备可安全重放的 Cookie 集合，因此丢弃旧响应状态。
			cookieSession = nil
			if res != nil {
				res.UpdatedCookies = cookieStr
			}
		}
	}
	if cookieSession != nil {
		// value、snapshot、changed 保存value、snapshot、changed，供当前处理流程使用
		value, snapshot, changed := cookieSession.State()
		if changed {
			// metadata 保存metadata，供当前处理流程使用
			metadata := cookierefresh.MetadataWithoutSnapshot(metadataJSON)
			if snapshot != nil {
				metadata = cookierefresh.MetadataWithSnapshot(metadataJSON, snapshot)
			}
			if // persistErr 保存persistErr，供当前处理流程使用
			persistErr := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, value, metadata, time.Now().Unix()); persistErr != nil {
				a.logger.Warn("登录态检查保存响应 Cookie Jar 失败", "err", persistErr)
				return loginStatusCheckResult{}
			}
			a.replaceCredentialState(value, credentialStateFingerprint(value, metadata))
			a.clearTokenCache(ctx)
			a.notifyCredentialUpdated(ctx)
			if err != nil {
				a.logger.Warn("登录态检查失败，已保存响应 Cookie", "err", err)
			}
			return loginStatusCheckResult{recovered: res != nil && res.Status == mtop.LoginStatusTokenRefreshed}
		}
	}
	if err != nil {
		a.logger.Warn("登录态检查失败", "err", err)
		return loginStatusCheckResult{}
	}
	if res == nil {
		return loginStatusCheckResult{}
	}
	if res.Status == mtop.LoginStatusRiskRequired {
		a.setRuntimeState(RuntimeVerificationRequired, "闲鱼要求安全验证")
		a.logger.Warn("登录态检查命中风控验证", "ret", strings.Join(res.Ret, ","), "verification_url", res.VerificationURL)
		return loginStatusCheckResult{riskRequired: true, verificationURL: res.VerificationURL}
	}
	if res.Status == mtop.LoginStatusTokenRefreshed && len(cookierefresh.ChangedCookieNames(cookieStr, res.UpdatedCookies)) > 0 && a.adoptRecoveredCookie(ctx, res.UpdatedCookies, "登录态检查") {
		a.logger.Info("登录态检查刷新了 Cookie", "status", res.Status, "message", res.Message)
		return loginStatusCheckResult{recovered: true}
	}
	a.logger.Info("登录态检查未产生可用 Cookie 更新", "status", res.Status, "message", res.Message)
	return loginStatusCheckResult{}
}

// tryAPIRenew 是密码登录前的轻量恢复层，只执行官网 auto-login plugin 的
// 单次 silentHasLogin 流程。如果只拿到部分 Cookie，仍先保存并清 token，
// 但继续按 Go 协议执行后续恢复；仍失败时由上层要求重新扫码登录。
// tryAPIRenew 负责tryAPIRenew相关处理。
func (a *Account) tryAPIRenew(ctx context.Context) bool {
	if a.renewer == nil {
		return false
	}
	// renewed 保存renewed，供当前处理流程使用
	renewed, _ := a.tryAPIRenewUsing(ctx, func(runCtx context.Context, cookieStr string, snapshot []cookierefresh.BrowserCookie) (*renew.Result, error) {
		return a.renewer.RenewAPIFirst(runCtx, cookieStr, snapshot)
	})
	return renewed
}

// tryAPIRenewUsing 负责tryAPIRenewUsing相关处理。
func (a *Account) tryAPIRenewUsing(ctx context.Context, call func(context.Context, string, []cookierefresh.BrowserCookie) (*renew.Result, error)) (bool, error) {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	if a.store != nil && a.store.Cookies != nil && !a.store.Cookies.GetStatus(ctx, a.CookieID) {
		return false, nil
	}
	a.mu.Lock()
	// cookieStr 保存登录凭证Str，供当前处理流程使用
	cookieStr := a.CookieStr
	a.mu.Unlock()
	// snapshot 保存snapshot，供当前处理流程使用
	var snapshot []cookierefresh.BrowserCookie
	if a.store != nil && a.store.Cookies != nil {
		runtimeData, detailErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含接口续期所需的 Cookie 与 metadata。
		if detailErr != nil {
			a.logger.Warn("接口续期前读取最新 Cookie 失败", "err", detailErr)
			return false, detailErr
		}
		if runtimeData.Value != cookieStr {
			cookieStr = runtimeData.Value
			a.replaceCookieStr(cookieStr)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
		}
		snapshot = cookierefresh.SnapshotFromMetadata(runtimeData.MetadataJSON)
		// runtimeData 的 Cookie 用于续期请求，metadata 用于恢复浏览器 Cookie 快照。
		// 读取窄模型不会触碰登录用户名、密码等无关凭证字段。
		// API 续期仍沿用原有锁、Cookie 比较和 token 清理顺序。
	}
	// res、err 保存res、err，供当前处理流程使用
	res, err := call(ctx, cookieStr, snapshot)
	if res == nil {
		if err != nil {
			a.logger.Warn("接口续期失败", "err", err)
		}
		return false, err
	}
	if res.HasPending() {
		a.watchPendingAPIRenew(ctx, res)
	}
	// updated 保存updated，供当前处理流程使用
	updated := false
	// persisted 保存persisted，供当前处理流程使用
	persisted := false
	if res.CookieSnapshotComplete && a.store != nil && a.store.Cookies != nil {
		runtimeData, detailErr := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含续期快照持久化所需字段。
		// 快照持久化只依赖 metadata，Cookie 明文由续期响应直接提供。
		// 保留统一运行时查询模型，避免为相同凭证路径恢复完整账号详情。
		// 下面的更新操作和错误处理保持原有续期语义不变。
		if detailErr != nil {
			a.logger.Warn("保存续期 Cookie 快照失败", "err", detailErr)
			return false, detailErr
		}
		// metadata 保存metadata，供当前处理流程使用
		metadata := cookierefresh.MetadataWithSnapshot(runtimeData.MetadataJSON, res.CookieSnapshot)
		if // err 保存err，供当前处理流程使用
		err := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, res.NewCookies, metadata, time.Now().Unix()); err != nil {
			a.logger.Warn("保存续期 Cookie 快照失败", "err", err)
			return false, err
		}
		persisted = true
	} else if len(res.SetCookies) > 0 && a.store != nil && a.store.Cookies != nil {
		if // err 保存err，供当前处理流程使用
		err := a.persistRenewFlatCookie(ctx, res.NewCookies); err != nil {
			a.logger.Warn("保存接口续期扁平 Cookie 失败", "err", err)
			return false, err
		}
		persisted = true
	}
	// credentialChanged 保存credentialChanged，供当前处理流程使用
	credentialChanged := res.NewCookies != cookieStr && (res.CookieSnapshotComplete || len(res.SetCookies) > 0 || res.NewCookies != "")
	if credentialChanged {
		if persisted || a.store == nil || a.store.Cookies == nil {
			a.replaceCookieStr(res.NewCookies)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
			a.setRuntimeState(RuntimeConnecting, "接口续期已更新登录凭证，正在重新连接")
			updated = true
		} else {
			updated = a.adoptRecoveredCookie(ctx, res.NewCookies, "接口续期")
		}
		if updated && persisted {
			a.notifyCredentialUpdated(ctx)
		}
	}
	if err != nil {
		a.logger.Warn("接口续期失败，已保存响应 Cookie", "err", err)
		return false, err
	}
	if res.Success {
		if !updated {
			a.setRuntimeState(RuntimeConnecting, "登录凭证已接口续期，正在重新连接")
		}
		a.logger.Info("接口续期成功", "method", res.RenewMethod, "updated", strings.Join(res.UpdatedCookieNames, ","))
		return true, nil
	}
	if updated {
		a.logger.Info("接口续期返回部分 Cookie 更新，继续降级恢复", "updated", strings.Join(res.UpdatedCookieNames, ","))
		return false, nil
	}
	a.logger.Info("接口续期未产生可用恢复", "success", res.Success, "message", res.Message)
	return false, nil
}

// persistRenewFlatCookie 负责persistRenewFlat登录凭证相关处理。
func (a *Account) persistRenewFlatCookie(ctx context.Context, newCookies string) error {
	if a.store == nil || a.store.Cookies == nil {
		return nil
	}
	metadata, err := a.store.Cookies.GetCookieMetadata(ctx, a.CookieID) // metadata 只包含扁平 Cookie 写回所需的快照信息。
	if err != nil {
		return err
	}
	// 该流程不需要读取现有 Cookie 明文或登录秘密。
	// metadata 已在 repository 层按账号作用域解密，下面只清理旧快照。
	// 更新操作继续由 UpdateRenewalCookie 负责加密和账号存在性校验。
	// 没有权威 Jar 时，接口 Set-Cookie 只能更新兼容扁平值。不能把
	// Domain/Path/HttpOnly/PartitionKey 均未知的 Cookie 伪造成完整快照。
	metadata = cookierefresh.MetadataWithoutSnapshot(metadata)
	return a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, newCookies, metadata, time.Now().Unix())
}

// watchPendingAPIRenew 负责watchPendingAPIRenew相关处理。
func (a *Account) watchPendingAPIRenew(parent context.Context, result *renew.Result) {
	if result == nil || !result.HasPending() {
		return
	}
	a.pendingRenewWG.Add(1)
	go func() {
		defer a.pendingRenewWG.Done()
		// ctx、cancel 保存ctx、cancel，供当前处理流程使用
		ctx, cancel := context.WithTimeout(parent, 35*time.Second)
		defer cancel()
		// late、waitErr 保存late、waitErr，供当前处理流程使用
		late, waitErr := result.AwaitPending(ctx)
		if late == nil {
			if waitErr != nil {
				a.logger.Warn("等待静默续期底层响应失败", "err", waitErr)
			}
			return
		}
		if // persistErr 保存persistErr，供当前处理流程使用
		persistErr := a.persistPendingRenewCookies(ctx, late); persistErr != nil {
			a.logger.Warn("保存静默续期迟到 Cookie 失败", "err", persistErr)
			return
		}
		if waitErr != nil {
			a.logger.Warn("静默续期底层响应失败，已保存响应 Cookie", "err", waitErr)
		}
	}()
}

// persistPendingRenewCookies 负责persistPendingRenewCookies相关处理。
func (a *Account) persistPendingRenewCookies(ctx context.Context, result *renew.Result) error {
	if result == nil || len(result.SetCookies) == 0 || a.store == nil || a.store.Cookies == nil {
		return nil
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := a.store.LockAccountCredentials(a.CookieID)
	defer credentialUnlock()
	runtimeData, err := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含迟到续期合并所需的 Cookie 与 metadata。
	if err != nil {
		return err
	}
	// runtimeData 已在凭证锁内读取，避免迟到响应覆盖并发写入的最新凭证状态。
	// RebaseResponseCookies 继续根据当前 Cookie 与 metadata 重放 Set-Cookie。
	// 下面的 UpdateRenewalCookie、运行时状态和通知顺序保持原有行为。
	// newCookies、metadata、changed 保存newCookies、metadata、changed，供当前处理流程使用
	newCookies, metadata, changed := renew.RebaseResponseCookies(runtimeData.Value, runtimeData.MetadataJSON, result)
	if !changed {
		return nil
	}
	if // err 保存err，供当前处理流程使用
	err := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, newCookies, metadata, time.Now().Unix()); err != nil {
		return err
	}
	a.replaceCredentialState(newCookies, credentialStateFingerprint(newCookies, metadata))
	a.clearTokenCache(ctx)
	a.notifyCredentialUpdated(ctx)
	a.logger.Info("已异步接收官网静默续期迟到 Cookie", "updated", strings.Join(result.UpdatedCookieNames, ","))
	return nil
}

// adoptRecoveredCookie 统一接收“轻量检查/接口续期”拿到的新 Cookie。
// 官网页面在普通 Set-Cookie 更新后保持当前 FishEngine/device ID 与健康 WS；
// 下一次重连才使用新 Cookie 获取新的连接级 accessToken。
// adoptRecoveredCookie 负责adoptRecovered登录凭证相关处理。
func (a *Account) adoptRecoveredCookie(ctx context.Context, newCookies, source string) bool {
	if strings.TrimSpace(newCookies) == "" {
		return false
	}
	a.mu.Lock()
	// oldCookies 保存oldCookies，供当前处理流程使用
	oldCookies := a.CookieStr
	a.mu.Unlock()
	if newCookies == oldCookies {
		return false
	}
	if a.store != nil && a.store.Cookies != nil {
		if // err 保存err，供当前处理流程使用
		err := a.store.Cookies.UpdateValueExisting(ctx, a.CookieID, newCookies); err != nil {
			a.logger.Error(source+"后保存 cookie 失败", "cookie_id", a.CookieID, "err", err)
			return false
		}
	}
	a.replaceCookieStr(newCookies)
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
	a.setRuntimeState(RuntimeConnecting, source+"已更新登录凭证，正在重新连接")
	a.notifyCredentialUpdated(ctx)
	return true
}

// notifyCredentialUpdated 负责notifyCredentialUpdated相关处理。
func (a *Account) notifyCredentialUpdated(ctx context.Context) {
	if // handler、ok 保存handler、ok，供当前处理流程使用
	handler, ok := a.handler.(credentialUpdateHandler); ok {
		handler.OnCredentialUpdated(ctx, a.CookieID)
	}
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

// refreshToken 调 mtop token API，返回 (accessToken, 更新后的 cookie)。
// 成功时记录 token 的服务端过期时间并保持 device_id 持久化，但连接流程
// 不会复用该 token；下一次 loginV2/reConnect 仍会重新调用本方法。
// refreshToken 负责refresh令牌相关处理。
func (a *Account) refreshToken(ctx context.Context) (string, string, error) {
	return a.refreshTokenWithMinGap(ctx, false)
}

// refreshTokenWithMinGap 保留旧签名以避免影响调用方；参考实现没有额外的一分钟
// Token 防抖，因此 enforceMinGap 不参与行为。
// refreshTokenWithMinGap 负责refresh令牌WithMinGap相关处理。
func (a *Account) refreshTokenWithMinGap(ctx context.Context, _ bool) (string, string, error) {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()

	// refreshMu serializes the complete token/Cookie update transaction for an
	// account. A failed automatic verification also suppresses repeated token API
	// calls until the caller-side cooldown expires.
	if // remaining 保存remaining，供当前处理流程使用
	remaining := a.tokenCaptchaCooldownRemaining(); remaining > 0 {
		a.setLastTokenStatus(tokenRefreshSkippedCooldown)
		return "", "", fmt.Errorf("%w，剩余 %s", errTokenCaptchaCooldown, remaining.Round(time.Second))
	}

	a.reloadCookieFromDB(ctx)

	a.mu.Lock()
	// cookieStr 保存登录凭证Str，供当前处理流程使用
	cookieStr := a.CookieStr
	a.lastTokenRefresh = time.Now()
	a.lastTokenStatus = tokenRefreshStarted
	a.mu.Unlock()

	// deviceID 保存deviceID，供当前处理流程使用
	deviceID := strings.TrimSpace(a.deviceID)
	if deviceID == "" {
		if // unb 保存unb，供当前处理流程使用
		unb := protocol.TransCookies(cookieStr)["unb"]; unb != "" {
			deviceID = protocol.GenerateDeviceID(unb)
			a.mu.Lock()
			a.deviceID = deviceID
			a.mu.Unlock()
		}
	}
	for // captchaRetry 保存captcha重试，供当前处理流程使用
	captchaRetry := 0; captchaRetry < 3; captchaRetry++ {
		// res 保存响应，供当前处理流程使用
		var res *mtop.RefreshResult
		// err 保存err，供当前处理流程使用
		var err error
		if // scoped、ok 保存scoped、ok，供当前处理流程使用
		scoped, ok := a.mtop.(scopedTokenClient); ok {
			// snapshot 保存snapshot，供当前处理流程使用
			var snapshot []cookierefresh.BrowserCookie
			if a.store != nil && a.store.Cookies != nil {
				if metadata, metadataErr := a.store.Cookies.GetCookieMetadata(ctx, a.CookieID); metadataErr == nil { // metadata 是 token 请求所需的 Cookie 快照信息。
					snapshot = cookierefresh.SnapshotFromMetadata(metadata)
				}
			}
			res, err = scoped.RefreshTokenWithCredentialContext(ctx, cookieStr, deviceID, snapshot)
		} else {
			res, err = a.mtop.RefreshTokenWithDeviceIDContext(ctx, cookieStr, deviceID)
		}
		// 参考实现无论业务结果为何，都先合并响应 Set-Cookie。本地还必须先把
		// 完整 Jar 持久化成功，避免当前 /reg 成功而下次重连回滚到旧凭证。
		// persistErr 保存persistErr，供当前处理流程使用
		var persistErr error
		cookieStr, persistErr = a.adoptTokenResponseCookies(ctx, cookieStr, res)
		if persistErr != nil {
			a.setLastTokenStatus(tokenRefreshFailedAPI)
			a.clearCurrentToken()
			return "", "", fmt.Errorf("保存 token 响应 Cookie: %w", persistErr)
		}
		if err != nil && mtop.IsRiskVerificationErr(err) {
			if // recovered、ok 保存recovered、ok，供当前处理流程使用
			recovered, ok := a.tryTokenCaptchaRecovery(ctx, cookieStr, deviceID, err); ok {
				cookieStr = recovered.UpdatedCookies
				// 重取地址时即使拿到了 accessToken，参考实现也不会直接采用；
				// 它会清缓存后重新走一次标准 token 请求。
				continue
			}
			a.markTokenCaptchaFailure()
			a.setLastTokenStatus(tokenRefreshFailedCaptcha)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
			return "", "", err
		}
		if err != nil {
			// status 保存状态，供当前处理流程使用
			status := classifyTokenFailure(err)
			a.setLastTokenStatus(status)
			a.clearCurrentToken()
			if status != tokenRefreshFailedNetwork && status != tokenRefreshFailedTimeout {
				a.clearTokenCache(ctx)
			}
			return "", "", err
		}
		if res == nil || strings.TrimSpace(res.AccessToken) == "" {
			a.setLastTokenStatus(tokenRefreshFailedAPI)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
			return "", "", fmt.Errorf("token API 未返回结果")
		}
		// credentialFP、fingerprintErr 保存credentialFP、fingerprintErr，供当前处理流程使用
		credentialFP, fingerprintErr := a.databaseCredentialFingerprint(ctx, cookieStr)
		if fingerprintErr != nil {
			a.setLastTokenStatus(tokenRefreshFailedAPI)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
			return "", "", fmt.Errorf("绑定 token 凭证状态: %w", fingerprintErr)
		}
		a.saveTokenCache(ctx, deviceID, res.AccessToken, res.AccessTokenExpireAt, credentialFP)
		a.mu.Lock()
		a.credentialFP = credentialFP
		a.tokenCredentialFP = credentialFP
		a.lastCaptchaFailure = time.Time{}
		a.tokenFetchFailures = 0
		a.lastTokenStatus = tokenRefreshSuccess
		a.mu.Unlock()
		a.runtimeMu.Lock()
		a.lastMsgReceived = time.Time{}
		a.runtimeMu.Unlock()
		return res.AccessToken, cookieStr, nil
	}

	a.setLastTokenStatus(tokenRefreshFailedCaptcha)
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
	return "", "", fmt.Errorf("滑块验证重试次数已达上限")
}

// clearCurrentToken 负责clearCurrent令牌相关处理。
func (a *Account) clearCurrentToken() {
	a.mu.Lock()
	a.currentToken = ""
	a.tokenCredentialFP = ""
	a.mu.Unlock()
}

// adoptTokenResponseCookies 负责adopt令牌响应Cookies相关处理。
func (a *Account) adoptTokenResponseCookies(ctx context.Context, cookieStr string, res *mtop.RefreshResult) (string, error) {
	if res == nil {
		return cookieStr, nil
	}
	if !res.CookieSnapshotComplete && !res.CookieStateChanged && strings.TrimSpace(res.UpdatedCookies) == "" {
		return cookieStr, nil
	}
	if !res.CookieSnapshotComplete && !res.CookieStateChanged && res.UpdatedCookies == cookieStr && len(res.CookieSnapshot) == 0 {
		return cookieStr, nil
	}
	if a.store != nil && a.store.Cookies != nil {
		metadata, detailErr := a.store.Cookies.GetCookieMetadata(ctx, a.CookieID) // metadata 只包含 token 响应 Cookie 合并所需的快照信息。
		if detailErr != nil {
			return cookieStr, detailErr
		}
		// metadata 已在 repository 层按账号作用域解密，不读取旧 Cookie 或登录秘密。
		// 下面继续根据响应类型合并已有快照，并由 UpdateRenewalCookie 统一持久化。
		// 错误返回和运行时状态更新顺序保持原有 token 响应语义。
		// 只有 token 响应本身发生变化时才进入后续快照合并逻辑。
		if res.CookieSnapshotComplete {
			// snapshot 保存snapshot，供当前处理流程使用
			snapshot := cookierefresh.NormalizeSnapshot(res.CookieSnapshot)
			if snapshot == nil {
				snapshot = []cookierefresh.BrowserCookie{}
			}
			metadata = cookierefresh.MetadataWithSnapshot(metadata, snapshot)
		} else if // snapshot、snapshotOK 保存snapshot、snapshotOK，供当前处理流程使用
		snapshot, snapshotOK := cookierefresh.SnapshotFromMetadataOK(metadata); snapshotOK {
			// 扁平结果不能凭空证明 Jar 完整；仅在已有权威 Jar 时按已知
			// Domain/Path 身份对值做兼容合并。
			snapshot = cookierefresh.ReconcileSnapshotWithCookieString(snapshot, res.UpdatedCookies)
			metadata = cookierefresh.MetadataWithSnapshot(metadata, snapshot)
		} else {
			metadata = cookierefresh.MetadataWithoutSnapshot(metadata)
		}
		if // err 保存err，供当前处理流程使用
		err := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, res.UpdatedCookies, metadata, time.Now().Unix()); err != nil {
			return cookieStr, err
		}
		a.replaceCredentialState(res.UpdatedCookies, credentialStateFingerprint(res.UpdatedCookies, metadata))
		a.notifyCredentialUpdated(ctx)
		return res.UpdatedCookies, nil
	}
	if res.UpdatedCookies != cookieStr {
		a.replaceCookieStr(res.UpdatedCookies)
	}
	return res.UpdatedCookies, nil
}

// tryTokenCaptchaRecovery 负责try令牌CaptchaRecovery相关处理。
func (a *Account) tryTokenCaptchaRecovery(ctx context.Context, cookieStr, deviceID string, err error) (*mtop.RefreshResult, bool) {
	// h、ok 保存h、ok，供当前处理流程使用
	h, ok := a.handler.(tokenCaptchaHandler)
	if !ok {
		return nil, false
	}
	// riskErr 保存riskErr，供当前处理流程使用
	var riskErr *mtop.RiskVerificationError
	if !errors.As(err, &riskErr) || strings.TrimSpace(riskErr.VerificationURL) == "" {
		return nil, false
	}
	a.alertEvent(ctx, EventSecurityVerification, AlertLevelWarn, "闲鱼要求滑块验证",
		"token 刷新触发闲鱼风控验证，系统将尝试自动完成滑块并合并 x5sec。")
	// result、ok 保存result、ok，供当前处理流程使用
	result, ok := h.OnTokenCaptchaVerification(ctx, a.CookieID, cookieStr, riskErr.VerificationURL, deviceID)
	if !ok || result == nil || strings.TrimSpace(result.UpdatedCookies) == "" {
		return nil, false
	}
	// updatedCookies、persistErr 保存updatedCookies、persistErr，供当前处理流程使用
	updatedCookies, persistErr := a.adoptTokenResponseCookies(ctx, cookieStr, result)
	if persistErr != nil {
		a.logger.Error("滑块验证后保存 cookie 失败", "cookie_id", a.CookieID, "err", persistErr)
		return nil, false
	}
	result.UpdatedCookies = updatedCookies
	a.replaceCookieStr(updatedCookies)
	a.clearTokenCache(ctx)
	a.setRuntimeState(RuntimeConnecting, tokenRiskRecoveryMessage)
	return result, true
}

// markTokenCaptchaFailure 负责mark令牌CaptchaFailure相关处理。
func (a *Account) markTokenCaptchaFailure() {
	a.mu.Lock()
	a.lastCaptchaFailure = time.Now()
	a.mu.Unlock()
}

// tokenCaptchaCooldownRemaining 负责令牌CaptchaCooldownRemaining相关处理。
func (a *Account) tokenCaptchaCooldownRemaining() time.Duration {
	a.mu.Lock()
	// lastFailure 保存lastFailure，供当前处理流程使用
	lastFailure := a.lastCaptchaFailure
	a.mu.Unlock()
	if lastFailure.IsZero() {
		return 0
	}
	// remaining 保存remaining，供当前处理流程使用
	remaining := TokenCaptchaFailureCooldown - time.Since(lastFailure)
	if remaining < 0 {
		return 0
	}
	return remaining
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
func (a *Account) setLastTokenStatus(status string) {
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

// saveTokenCache records the server expiry and current page-runtime identity.
// It is diagnostic state only: acquireToken never reads the accessToken back
// for a later WebSocket registration.
// saveTokenCache 负责save令牌Cache相关处理。
func (a *Account) saveTokenCache(ctx context.Context, deviceID, accessToken string, serverExpireAt int64, credentialFP string) {
	if accessToken == "" {
		return
	}
	// now 保存now，供当前处理流程使用
	now := time.Now()
	// expiresAt、refreshAt 保存expiresAt、refreshAt，供当前处理流程使用
	expiresAt, refreshAt := tokenRotationSchedule(serverExpireAt, now)
	// tokenFP 保存令牌FP，供当前处理流程使用
	tokenFP := tokenFingerprint(accessToken)
	a.mu.Lock()
	// previousTokenFP 保存previous令牌FP，供当前处理流程使用
	previousTokenFP := a.tokenFingerprint
	a.tokenFingerprint = tokenFP
	a.tokenAcquiredAt = now
	a.tokenExpiresAt = expiresAt
	a.tokenRefreshAt = refreshAt
	a.mu.Unlock()
	a.logger.Info("WS Token 获取成功", "expires_at", expiresAt, "refresh_at", refreshAt, "ttl", time.Until(expiresAt).Round(time.Second), "token_fp", tokenFP, "previous_token_fp", previousTokenFP, "token_changed", previousTokenFP == "" || previousTokenFP != tokenFP)
	if a.store == nil || a.store.Tokens == nil {
		return
	}
	// expireAt 保存expireAt，供当前处理流程使用
	expireAt := effectiveTokenExpireAt(serverExpireAt, now)
	if expireAt == 0 {
		// 服务端未给有效期时仍使用保守运行时轮换时间，但不把推测期限
		// 伪装成服务端缓存期限。
		a.logger.Warn("token API 未返回可用过期时间，使用保守轮换时间", "refresh_at", refreshAt)
		a.clearTokenCache(ctx)
		return
	}
	if // err 保存err，供当前处理流程使用
	err := a.store.Tokens.SaveBound(ctx, a.CookieID, deviceID, accessToken, expireAt, credentialFP); err != nil {
		a.logger.Warn("缓存 accessToken 失败", "err", err)
	}
}

// tokenFingerprint 用不可逆摘要标识 Token，便于判断服务端是否轮换了 Token，
// 同时避免日志泄露可用于 WS 注册的凭证原文。
// tokenFingerprint 负责令牌Fingerprint相关处理。
func tokenFingerprint(token string) string {
	// sum 保存sum，供当前处理流程使用
	sum := sha256.Sum256([]byte(token))
	return fmt.Sprintf("%x", sum[:6])
}

// clearTokenCache 清除账号 token 缓存（session 失效 / 短连接可疑 / cookie 被外部更新时调用）。
func (a *Account) clearTokenCache(ctx context.Context) {
	a.mu.Lock()
	a.tokenFingerprint = ""
	a.mu.Unlock()
	if a.store == nil || a.store.Tokens == nil {
		return
	}
	if // err 保存err，供当前处理流程使用
	err := a.store.Tokens.Clear(ctx, a.CookieID); err != nil {
		a.logger.Warn("清除 token 缓存失败", "err", err)
	}
}

// databaseCredentialFingerprint returns the complete DB credential state that
// produced cookieStr. It must be called while the account credential lock is
// held when a Store is present.
// databaseCredentialFingerprint 负责databaseCredentialFingerprint相关处理。
func (a *Account) databaseCredentialFingerprint(ctx context.Context, cookieStr string) (string, error) {
	if a.store == nil || a.store.Cookies == nil {
		return credentialStateFingerprint(cookieStr, ""), nil
	}
	runtimeData, err := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含 token 凭证一致性校验所需的 Cookie 与 metadata。
	if err != nil {
		return "", err
	}
	// runtimeData 已在调用方凭证锁内读取，避免校验期间混入另一笔 Cookie 更新。
	// Cookie 与 metadata 均由 repository 按账号作用域解密，登录密码不会进入此流程。
	// 后续空值判断、指纹比较和错误文案保持原有 token 绑定语义。
	// snapshotComplete 保存snapshotComplete，供当前处理流程使用
	_, snapshotComplete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON)
	if strings.TrimSpace(runtimeData.Value) == "" && !snapshotComplete {
		return "", fmt.Errorf("数据库 Cookie 为空且没有权威 Jar")
	}
	if credentialCookieFingerprint(runtimeData.Value) != credentialCookieFingerprint(cookieStr) {
		return "", fmt.Errorf("token 请求期间数据库 Cookie 已变化")
	}
	return credentialStateFingerprint(runtimeData.Value, runtimeData.MetadataJSON), nil
}

// reloadCookieFromDB 复读 DB cookie：与内存不同则采纳，并清 token 缓存。普通 Cookie
// 更新不轮换页面生命周期内的 device ID；显式登录由 Manager 重建 Account。
// reloadCookieFromDB 负责reload登录凭证FromDB相关处理。
func (a *Account) reloadCookieFromDB(ctx context.Context) bool {
	if a.store == nil || a.store.Cookies == nil {
		return false
	}
	runtimeData, err := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含检测外部凭证更新所需的 Cookie 与 metadata。
	if err != nil {
		return false
	}
	if strings.TrimSpace(runtimeData.Value) == "" {
		if // complete 保存complete，供当前处理流程使用
		_, complete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON); !complete {
			return false
		}
	}
	// databaseFP 保存databaseFP，供当前处理流程使用
	databaseFP := credentialStateFingerprint(runtimeData.Value, runtimeData.MetadataJSON)
	a.mu.Lock()
	// currentFP 保存currentFP，供当前处理流程使用
	currentFP := a.credentialFP
	if currentFP == "" {
		currentFP = credentialStateFingerprint(a.CookieStr, "")
	}
	a.mu.Unlock()
	if databaseFP == currentFP {
		return false
	}
	a.logger.Info("检测到 DB cookie 已更新，重新加载", "account", a.CookieID)
	a.replaceCredentialState(runtimeData.Value, databaseFP)
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
	a.mu.Lock()
	a.lastCaptchaFailure = time.Time{}
	a.mu.Unlock()
	return true
}

// cookieSnapshotMatchesDB 负责登录凭证SnapshotMatchesDB相关处理。
func (a *Account) cookieSnapshotMatchesDB(ctx context.Context, expectedFP string) bool {
	if a.store == nil || a.store.Cookies == nil {
		return true
	}
	runtimeData, err := a.store.Cookies.GetCookieRuntimeData(ctx, a.CookieID) // runtimeData 只包含 WS 注册前凭证一致性校验所需的 Cookie 与 metadata。
	if err != nil {
		a.logger.Warn("WS 注册前读取最新 Cookie 失败，放弃本次连接", "err", err)
		return false
	}
	// snapshotComplete 保存snapshotComplete，供当前处理流程使用
	_, snapshotComplete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON)
	if strings.TrimSpace(runtimeData.Value) == "" && !snapshotComplete {
		a.logger.Warn("WS 注册前最新 Cookie 为空且没有权威 Jar，放弃本次连接")
		return false
	}
	if expectedFP == "" {
		a.logger.Warn("WS 注册 token 缺少绑定的凭证状态，放弃本次连接")
		return false
	}
	return credentialStateFingerprint(runtimeData.Value, runtimeData.MetadataJSON) == expectedFP
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
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	// conn、myID、err 保存conn、myID、err，供当前处理流程使用
	conn, myID, err := a.currentSenderState()
	if err != nil {
		return err
	}
	// sendCtx、cancel 保存sendCtx、cancel，供当前处理流程使用
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if // err 保存err，供当前处理流程使用
	err := conn.SendText(sendCtx, myID, chatID, toUserID, text); err != nil {
		return err
	}
	if // observer、ok 保存observer、ok，供当前处理流程使用
	observer, ok := a.handler.(outgoingChatHandler); ok {
		// key 保存key，供当前处理流程使用
		key, _ := ctx.Value(outgoingMessageKeyContextKey{}).(string)
		if // err 保存err，供当前处理流程使用
		err := observer.HandleOutgoingChatMessage(ctx, OutgoingChatMessage{
			AccountID: a.CookieID, ChatID: chatID, BuyerID: toUserID, Text: text, MessageKey: key,
		}); err != nil {
			a.logger.Warn("保存出站聊天旁路失败", "account", a.CookieID, "chat_id", chatID, "err", err)
		}
	}
	return nil
}

// SendImage 通过当前 WebSocket 给买家发送图片消息。当前仅支持可直接访问的 CDN/公网 URL。
func (a *Account) SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64) error {
	imageURL = strings.TrimSpace(imageURL)
	if imageURL == "" {
		return nil
	}
	if strings.HasPrefix(imageURL, "/static/") || strings.HasPrefix(imageURL, "static/") {
		return fmt.Errorf("当前运行时暂不支持本地图片自动上传到闲鱼 CDN: %s", imageURL)
	}
	// conn、myID、err 保存conn、myID、err，供当前处理流程使用
	conn, myID, err := a.currentSenderState()
	if err != nil {
		return err
	}
	// sendCtx、cancel 保存sendCtx、cancel，供当前处理流程使用
	sendCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return conn.SendImage(sendCtx, myID, chatID, toUserID, imageURL, 800, 600)
}

// currentSenderState 负责currentSender状态相关处理。
func (a *Account) currentSenderState() (WSConn, string, error) {
	a.runtimeMu.Lock()
	// conn 是当前连接快照；后续读取账号身份字段使用 Account 自身的凭证锁。
	conn := a.conn
	a.runtimeMu.Unlock()
	if conn == nil {
		return nil, "", fmt.Errorf("%w: 账号 %s 当前没有可用 WebSocket 连接", automation.ErrMessageNotSent, a.CookieID)
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	// myID 保存myID，供当前处理流程使用
	myID := strings.TrimSpace(a.UserID)
	if myID == "" {
		myID = protocol.TransCookies(a.CookieStr)["unb"]
	}
	if myID == "" {
		return nil, "", fmt.Errorf("%w: 账号 %s 缺少 unb，无法发送消息", automation.ErrMessageNotSent, a.CookieID)
	}
	return conn, myID, nil
}

// FetchChatHistory reuses the account's registered IM connection. Keeping this
// optional capability outside WSConn avoids forcing non-chat test transports to
// implement history retrieval.
// FetchChatHistory 负责Fetch聊天History相关处理。
func (a *Account) FetchChatHistory(ctx context.Context, chatID string, cursor int64, limit int) (map[string]any, string, error) {
	// conn、myID、err 保存conn、myID、err，供当前处理流程使用
	conn, myID, err := a.currentSenderState()
	if err != nil {
		return nil, "", err
	}
	// history、ok 保存history、ok，供当前处理流程使用
	history, ok := conn.(interface {
		ListUserMessages(context.Context, string, int64, int) (map[string]any, error)
	})
	if !ok {
		return nil, "", errors.New("当前 WebSocket 连接不支持聊天历史")
	}
	// body、err 保存body、err，供当前处理流程使用
	body, err := history.ListUserMessages(ctx, chatID, cursor, limit)
	return body, myID, err
}

// FetchChatConversations 负责Fetch聊天Conversations相关处理。
func (a *Account) FetchChatConversations(ctx context.Context, cursor int64, limit int) (map[string]any, string, error) {
	// conn、myID、err 保存conn、myID、err，供当前处理流程使用
	conn, myID, err := a.currentSenderState()
	if err != nil {
		return nil, "", err
	}
	// fetcher、ok 保存fetcher、ok，供当前处理流程使用
	fetcher, ok := conn.(interface {
		ListConversations(context.Context, int64, int) (map[string]any, error)
	})
	if !ok {
		return nil, "", errors.New("当前 WebSocket 连接不支持历史会话")
	}
	// body、err 保存body、err，供当前处理流程使用
	body, err := fetcher.ListConversations(ctx, cursor, limit)
	return body, myID, err
}

// AutomationReady 报告自动化消息是否可以立即使用当前 WS 连接发送。
func (a *Account) AutomationReady() bool {
	a.runtimeMu.Lock()
	defer a.runtimeMu.Unlock()
	return a.conn != nil && a.runtimeState == RuntimeOnline
}

// replaceCookieStr 负责replace登录凭证Str相关处理。
func (a *Account) replaceCookieStr(cookieStr string) {
	a.replaceCredentialState(cookieStr, credentialStateFingerprint(cookieStr, ""))
}

// replaceCredentialState 负责replaceCredential状态相关处理。
func (a *Account) replaceCredentialState(cookieStr, credentialFP string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.CookieStr = cookieStr
	a.credentialFP = credentialFP
	if // unb 保存unb，供当前处理流程使用
	unb := protocol.TransCookies(cookieStr)["unb"]; unb != "" {
		a.UserID = unb
	}
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

// UpdateCookie 用外部刷新得到的新 cookie 更新运行时状态。
func (a *Account) UpdateCookie(cookieStr string) {
	if strings.TrimSpace(cookieStr) == "" && (a.store == nil || a.store.Cookies == nil) {
		return
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	// 外部调用通常发生在一次网络请求写回之后。调用排队期间可能已有更新的
	// Cookie 落库，因此参数只作为无 Store 场景的兼容值；有 Store 时始终
	// 复读权威数据库，绝不把较旧的请求结果重新写回运行时。
	// metadataJSON 保存metadataJSON，供当前处理流程使用
	metadataJSON := ""
	if a.store != nil && a.store.Cookies != nil {
		runtimeData, err := a.store.Cookies.GetCookieRuntimeData(context.Background(), a.CookieID) // runtimeData 只包含同步运行时 Cookie 所需的 Cookie 与 metadata。
		if err != nil {
			a.logger.Warn("同步运行时 Cookie 前读取数据库失败", "err", err)
			return
		}
		if strings.TrimSpace(runtimeData.Value) == "" {
			if // complete 保存complete，供当前处理流程使用
			_, complete := cookierefresh.SnapshotFromMetadataOK(runtimeData.MetadataJSON); !complete {
				a.logger.Warn("同步运行时 Cookie 时数据库值为空且无权威 Jar")
				return
			}
		}
		cookieStr = runtimeData.Value
		metadataJSON = runtimeData.MetadataJSON
	}
	// credentialFP 保存credentialFP，供当前处理流程使用
	credentialFP := credentialStateFingerprint(cookieStr, metadataJSON)
	a.mu.Lock()
	// changed 保存changed，供当前处理流程使用
	changed := credentialFP != a.credentialFP
	a.mu.Unlock()
	if !changed {
		return
	}
	a.replaceCredentialState(cookieStr, credentialFP)
	// Cookie Jar 的普通更新不会打断已经认证的 IMPaaS 连接。新 Cookie
	// 会在下一次自然重连前被重新读取并用于获取新的 accessToken。
	a.clearTokenCache(context.Background())
}
