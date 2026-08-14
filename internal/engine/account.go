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
const (
	AlertLevelInfo     = "info"
	AlertLevelWarn     = "warn"
	AlertLevelCritical = "critical"
)

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

type accountEventHandler interface {
	OnAccountEvent(ctx context.Context, cookieID, eventType, level, title, body string)
}

type credentialUpdateHandler interface {
	OnCredentialUpdated(ctx context.Context, cookieID string)
}

type transportReadyHandler interface {
	OnTransportReady(ctx context.Context, cookieID string)
}

type tokenCaptchaHandler interface {
	OnTokenCaptchaVerification(ctx context.Context, cookieID, cookieStr, verificationURL, deviceID string) (*mtop.RefreshResult, bool)
}

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
type OutgoingChatMessage struct {
	AccountID  string
	ChatID     string
	BuyerID    string
	Text       string
	MessageKey string
}

type outgoingChatHandler interface {
	HandleOutgoingChatMessage(ctx context.Context, message OutgoingChatMessage) error
}

type outgoingMessageKeyContextKey struct{}

// WithOutgoingMessageKey correlates a UI-created pending message with the
// post-send observer so the same text is not inserted twice.
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
	CookieID  string
	CookieStr string
	UserID    string // unb（myid）

	store    *db.Store
	mtop     mtop.Client
	renewer  cookieRenewer
	wsDialer WSDialer
	handler  Handler
	logger   *slog.Logger

	// 运行时状态（受 mu 保护）
	mu                 sync.Mutex
	refreshMu          sync.Mutex
	currentToken       string
	deviceID           string
	connFailures       int
	networkFailures    int
	shortDisconnects   []time.Time
	lastMsgReceived    time.Time
	lastTokenRefresh   time.Time
	lastCaptchaFailure time.Time
	lastTokenStatus    string
	runtimeState       string
	runtimeMessage     string
	runtimeUpdatedAt   time.Time
	stopFn             context.CancelFunc
	stopped            bool
	conn               WSConn
	connStartedAt      time.Time // 本次 WS 连接建立时间，用于短连接检测
	authExpiredAlerted bool      // 已发过 auth_expired 告警，连接恢复后复位（避免刷屏）
	offlineNotified    bool
	offlineSince       time.Time
	lastOfflineReason  string
	tokenFetchFailures int
	credentialFP       string // 当前内存扁平 Cookie + 权威 Jar 的完整状态指纹
	tokenCredentialFP  string // currentToken 获取完成时绑定的完整凭证状态
	tokenAcquiredAt    time.Time
	tokenExpiresAt     time.Time
	tokenRefreshAt     time.Time
	tokenFingerprint   string // 仅用于诊断，不保存原始 Token

	// 去重
	dedupMu   sync.Mutex
	processed map[string]time.Time

	// 防抖：chat_id → 防抖句柄
	debounceMu     sync.Mutex
	debounceTimers map[string]*debounceEntry

	// 消息处理信号量
	sem chan struct{}

	// 业务任务生命周期。Stop 先禁止新增任务并取消 runtimeCtx，再等待已进入
	// 自动化/回复链的任务退出。
	taskMu     sync.Mutex
	taskWG     sync.WaitGroup
	runtimeCtx context.Context
	accepting  bool

	reply *ReplyService

	wsRecordOnce   sync.Once
	wsRecordWG     sync.WaitGroup
	wsRecordQueue  chan db.WSMessage
	pendingRenewWG sync.WaitGroup
}

type debounceEntry struct {
	timer    *time.Timer
	lastMsg  ChatMessage
	deadline time.Time
}

// WSConn 是 Account 对 ws 连接的最小契约。*ws.Conn 实现该接口；
// 测试可注入 fakeWSConn 以隔离真实 WS 握手与网络。
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

type defaultDialer struct{}

func (defaultDialer) Dial(ctx context.Context, cfg ws.Config, logger *slog.Logger) (WSConn, error) {
	return ws.Open(ctx, cfg, logger)
}

type cookieRenewer interface {
	RenewAPIFirst(ctx context.Context, cookiesStr string, snapshots ...[]cookierefresh.BrowserCookie) (*renew.Result, error)
}

type loginStatusChecker interface {
	CheckLoginStatusContext(ctx context.Context, cookiesStr string) (*mtop.LoginStatusResult, error)
}

type scopedTokenClient interface {
	RefreshTokenWithCredentialContext(ctx context.Context, cookiesStr, deviceID string, snapshot []cookierefresh.BrowserCookie) (*mtop.RefreshResult, error)
}

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
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	mtopWasNil := cfg.MTop == nil
	mtopClient := cfg.MTop
	if mtopClient == nil {
		mtopClient = mtop.NewClient()
	}
	renewer := cfg.Renewer
	if renewer == nil && mtopWasNil {
		renewer = renew.Service{}
	}
	cookies := protocol.TransCookies(cfg.CookieStr)
	myid := cookies["unb"]
	wsDialer := cfg.WSDialer
	if wsDialer == nil {
		wsDialer = defaultDialer{}
	}
	a := &Account{
		CookieID:         cfg.CookieID,
		CookieStr:        cfg.CookieStr,
		UserID:           myid,
		store:            cfg.Store,
		mtop:             mtopClient,
		renewer:          renewer,
		wsDialer:         wsDialer,
		handler:          cfg.Handler,
		logger:           logger.With("account", cfg.CookieID),
		deviceID:         protocol.GenerateDeviceID(myid),
		processed:        make(map[string]time.Time),
		debounceTimers:   make(map[string]*debounceEntry),
		sem:              make(chan struct{}, MessageSemaphoreSize),
		accepting:        true,
		runtimeState:     RuntimeStarting,
		runtimeMessage:   "正在启动账号服务",
		runtimeUpdatedAt: time.Now(),
		credentialFP:     credentialStateFingerprint(cfg.CookieStr, ""),
	}
	if cfg.Store != nil {
		a.reply = NewReplyService(cfg.CookieID, cfg.Store, a, nil, NewAIReplier(cfg.CookieID, cfg.Store, logger), logger)
		if cfg.Store.WSMessages != nil {
			a.wsRecordQueue = make(chan db.WSMessage, 256)
		}
	}
	return a
}

// Run 阻塞运行账号主循环，直到 ctx 取消或不可恢复错误。
// 调用方应在独立 goroutine 中运行；Stop 可优雅停止。
func (a *Account) Run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer func() {
		cancel()
		a.wsRecordWG.Wait()
		a.pendingRenewWG.Wait()
	}()
	a.mu.Lock()
	a.stopFn = cancel
	a.mu.Unlock()
	a.taskMu.Lock()
	a.runtimeCtx = ctx
	a.accepting = true
	a.taskMu.Unlock()
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
		conn, err := a.wsDialer.Dial(ctx, ws.Config{Recorder: a.wsRecorder()}, a.logger)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.logger.Error("WS 握手失败", "err", err)
			if retryErr := a.handleWSConnectFailure(ctx, err); retryErr != nil {
				return retryErr
			}
			continue
		}
		// The official web client calls
		// mtop.taobao.idlemessage.pc.login.token from authTokenCallback for every
		// loginV2/reConnect attempt. Do the same here: an access token belongs to
		// one connection attempt and must never be reused for a later /reg.
		token, cookieStr, err := a.acquireFreshConnectionToken(ctx)
		if err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.logger.Error("获取 token 失败", "err", err)
			a.mu.Lock()
			status := a.lastTokenStatus
			nonCounted := tokenFailureIsNonCounted(status)
			if !nonCounted {
				a.tokenFetchFailures++
			}
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
			if sleepErr := sleepCtx(ctx, a.tokenRetryDelay()); sleepErr != nil {
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
		deviceID := a.deviceID
		tokenCredentialFP := a.tokenCredentialFP
		a.mu.Unlock()
		credentialUnlock := func() {}
		if a.store != nil {
			credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
		}
		if !a.cookieSnapshotMatchesDB(ctx, tokenCredentialFP) {
			credentialUnlock()
			_ = conn.Close()
			a.reloadCookieFromDB(ctx)
			continue
		}
		err = conn.Register(ctx, deviceID, token)
		credentialUnlock()
		if err != nil {
			_ = conn.Close()
			if ctx.Err() != nil {
				return ctx.Err()
			}
			a.logger.Error("WS 注册失败", "err", err)
			if retryErr := a.handleWSConnectFailure(ctx, err); retryErr != nil {
				return retryErr
			}
			continue
		}
		a.mu.Lock()
		a.conn = conn
		a.connStartedAt = time.Now()
		a.connFailures = 0
		a.networkFailures = 0
		a.authExpiredAlerted = false // 连接成功，复位 auth_expired 告警标记
		shouldRecovered := a.offlineNotified
		offlineSince := a.offlineSince
		a.offlineNotified = false
		a.offlineSince = time.Time{}
		a.lastOfflineReason = ""
		a.mu.Unlock()
		a.setRuntimeState(RuntimeOnline, "消息服务连接正常")
		a.notifyTransportReady(ctx)
		if shouldRecovered {
			a.alertEvent(ctx, EventAccountRecovered, AlertLevelInfo, "账号已恢复在线",
				fmt.Sprintf("账号 %s 已重新连接闲鱼消息服务。掉线开始时间：%s。", a.CookieID, formatTimeOrUnknown(offlineSince)))
		}

		// 3) 健康连接维持心跳和收包，并在服务端 Token 过期前主动关闭，
		// 进入下一轮连接以重新调用 Token API 和 /reg。
		hbCtx, hbCancel := context.WithCancel(ctx)
		var hbErr error
		hbDone := make(chan struct{})
		go func() {
			hbErr = conn.HeartbeatLoop(hbCtx, HeartbeatInterval)
			_ = conn.Close()
			hbCancel()
			close(hbDone)
		}()
		rotateCh := make(chan struct{}, 1)
		a.mu.Lock()
		refreshAt := a.tokenRefreshAt
		expiresAt := a.tokenExpiresAt
		a.mu.Unlock()
		if refreshAt.IsZero() || !refreshAt.After(time.Now()) {
			refreshAt = time.Now()
		}
		rotateTimer := time.NewTimer(time.Until(refreshAt))
		rotateDone := make(chan struct{})
		go func() {
			defer close(rotateDone)
			select {
			case <-hbCtx.Done():
			case <-rotateTimer.C:
				select {
				case rotateCh <- struct{}{}:
				default:
				}
				_ = conn.Close()
			}
		}()

		recvErr := conn.ReceiveLoop(ctx, a.dispatch)
		if !rotateTimer.Stop() {
			select {
			case <-rotateTimer.C:
			default:
			}
		}
		hbCancel()
		<-rotateDone
		<-hbDone // 确保 hbErr 写入完成后再读取（消除数据竞争）。
		_ = conn.Close()
		if ctx.Err() != nil {
			return ctx.Err()
		}

		// 连接结束：只有认证类失败才清 token。已经建立后的网络断线继续
		// 使用内存 token 与数据库缓存，避免无意义调用 Token API。
		a.mu.Lock()
		startedAt := a.connStartedAt
		a.conn = nil
		a.mu.Unlock()
		connectedDuration := time.Since(startedAt)
		select {
		case <-rotateCh:
			a.logger.Info("WS Token 到达提前轮换时间，正在重新获取 Token", "expires_at", expiresAt, "remaining", time.Until(expiresAt).Round(time.Second))
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "WS Token 即将到期，正在主动轮换")
			continue
		default:
		}
		if ws.IsConnectLimitError(recvErr) {
			a.clearConnectionToken(ctx)
			reason := "消息会话已被服务端移除"
			a.setRuntimeState(RuntimeAuthExpired, reason)
			a.notifyOffline(ctx, reason)
			return nil
		}
		if ws.IsAuthenticationError(recvErr) {
			// 官网把 /push/kickout 转成 UNCONNECTED，页面监听器随后立即
			// reConnect，并由 authTokenCallback 获取新的连接凭证。
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "消息会话被服务端踢下线，正在重新连接")
			continue
		}
		// 心跳失败会先关闭连接，ReceiveLoop 往往只观察到 context canceled。
		// 官网以心跳 Promise 的 reject 为真实断线原因并立即 reConnect。
		if hbErr != nil && !errors.Is(hbErr, context.Canceled) &&
			(recvErr == nil || errors.Is(recvErr, context.Canceled)) {
			recvErr = hbErr
		}

		// 正常 close 的 async-for 会直接进入下一轮，不计任何失败。
		if recvErr == nil {
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "消息连接已结束，正在重新连接")
			continue
		}

		if isEstablishedNetworkError(recvErr) || errors.Is(recvErr, context.Canceled) {
			a.clearConnectionToken(ctx)
			a.setRuntimeState(RuntimeReconnecting, "网络连接已断开，正在重新连接")
			a.logger.Warn("WS 网络连接结束", "err", recvErr, "connected_duration", connectedDuration.Round(time.Second), "heartbeat_err", hbErr)
			// 官网当前页面在 CONN_UNCONNECTED 事件后立即调用 reConnect。
			continue
		}

		// 其他已经建立后的非认证错误同样进入 UNCONNECTED；不升级为
		// 密码登录、指数退避或永久禁用。
		a.clearConnectionToken(ctx)
		a.setRuntimeState(RuntimeReconnecting, "消息连接已断开，正在重新连接")
		a.logger.Warn("WS 连接结束", "err", recvErr, "heartbeat_err", hbErr)
		continue
	}
}

func (a *Account) wsRecorder() func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
	if a.store == nil || a.store.WSMessages == nil || a.wsRecordQueue == nil {
		return nil
	}
	return func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
		message := db.WSMessage{
			CookieID:    a.CookieID,
			Direction:   direction,
			RawText:     rawText,
			ParsedJSON:  parsedJSON,
			MessageKind: "",
			ParseStatus: parseStatus,
			Error:       errMsg,
		}
		select {
		case a.wsRecordQueue <- message:
		default:
			a.logger.Warn("WS 报文记录队列已满，丢弃诊断记录", "cookie_id", a.CookieID, "direction", direction)
		}
	}
}

func (a *Account) startWSRecorder(ctx context.Context) {
	if a.store == nil || a.store.WSMessages == nil || a.wsRecordQueue == nil {
		return
	}
	a.wsRecordOnce.Do(func() {
		a.wsRecordWG.Add(1)
		go func() {
			defer a.wsRecordWG.Done()
			cleanupCtx, cleanupCancel := context.WithTimeout(ctx, WSRecordWriteTimeout)
			deleted, cleanupErr := a.store.WSMessages.DeleteBefore(cleanupCtx, a.CookieID, time.Now().Add(-WSRecordRetention))
			cleanupCancel()
			if cleanupErr != nil && ctx.Err() == nil {
				a.logger.Warn("清理过期 WS 报文失败", "cookie_id", a.CookieID, "err", cleanupErr)
			} else if deleted > 0 {
				a.logger.Info("已清理过期 WS 报文", "cookie_id", a.CookieID, "deleted", deleted)
			}

			ticker := time.NewTicker(WSRecordFlushInterval)
			defer ticker.Stop()
			batch := make([]db.WSMessage, 0, WSRecordBatchSize)
			flush := func() {
				if len(batch) == 0 {
					return
				}
				writeCtx, cancel := context.WithTimeout(ctx, WSRecordWriteTimeout)
				err := a.store.WSMessages.AddBatch(writeCtx, batch)
				cancel()
				if err != nil && ctx.Err() == nil {
					a.logger.Warn("记录 WS 报文失败", "cookie_id", a.CookieID, "count", len(batch), "err", err)
				}
				batch = batch[:0]
			}
			for {
				select {
				case <-ctx.Done():
					return
				case message := <-a.wsRecordQueue:
					batch = append(batch, message)
					if len(batch) >= WSRecordBatchSize {
						flush()
					}
				case <-ticker.C:
					flush()
				}
			}
		}()
	})
}

func (a *Account) handleWSConnectFailure(ctx context.Context, err error) error {
	a.clearConnectionToken(ctx)
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
func (a *Account) acquireFreshConnectionToken(ctx context.Context) (string, string, error) {
	return a.refreshToken(ctx)
}

// clearConnectionToken ends the lifetime of the token used by the previous
// WebSocket attempt. The page-runtime device ID remains stable until a Cookie
// update maps to an official page reload.
func (a *Account) clearConnectionToken(ctx context.Context) {
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
}

// Stop 优雅停止。
func (a *Account) Stop() {
	a.mu.Lock()
	if a.stopped {
		a.mu.Unlock()
		return
	}
	a.stopped = true
	a.runtimeState = RuntimeStopped
	a.runtimeMessage = "账号服务已停止"
	a.runtimeUpdatedAt = time.Now()
	cancel := a.stopFn
	a.mu.Unlock()

	a.taskMu.Lock()
	a.accepting = false
	a.taskMu.Unlock()
	if cancel != nil {
		cancel()
	}
	// 取消所有防抖定时器。
	a.debounceMu.Lock()
	for _, e := range a.debounceTimers {
		if e.timer != nil {
			e.timer.Stop()
		}
	}
	a.debounceTimers = make(map[string]*debounceEntry)
	a.debounceMu.Unlock()

	done := make(chan struct{})
	go func() {
		a.taskWG.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		a.logger.Warn("等待账号业务任务退出超时")
	}
}

func (a *Account) beginTask() (context.Context, bool) {
	a.taskMu.Lock()
	defer a.taskMu.Unlock()
	if !a.accepting {
		return nil, false
	}
	ctx := a.runtimeCtx
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return nil, false
	}
	a.taskWG.Add(1)
	return ctx, true
}

// handleMaxFailures 是历史兼容恢复入口；只尝试 Go 协议续期，不执行密码登录。
func (a *Account) handleMaxFailures(ctx context.Context) error {
	// 先执行低成本登录态检查。它可能仅凭 loginuser.get 响应头恢复签名
	// Cookie，也能在进入静默续期前准确识别风控状态。
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
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	a.logger.Warn("连续失败达上限，触发 Go 协议续期", "failures", MaxConnectionFailures)
	a.notifyRecoveringOffline(ctx, fmt.Sprintf("消息服务连续认证/连接失败 %d 次，开始自动恢复", MaxConnectionFailures))
	if a.handler != nil && a.handler.OnPasswordLoginRefresh(ctx, a.CookieID) {
		if d, err := a.store.Cookies.GetDetails(ctx, a.CookieID); err == nil && d != nil && d.Value != "" {
			a.replaceCookieStr(d.Value)
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
func (a *Account) markAuthExpired() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.authExpiredAlerted {
		return false
	}
	a.authExpiredAlerted = true
	return true
}

func (a *Account) notifyOffline(ctx context.Context, reason string) {
	if !a.markOfflineNotified(reason) {
		return
	}
	a.alertEvent(ctx, EventAccountOffline, AlertLevelWarn, "账号已掉线，需要重新登录",
		fmt.Sprintf("账号 %s 的闲鱼消息连接已进入不可自动重连状态。原因：%s。请更新登录信息或重新登录后再启动账号。", a.CookieID, reason))
}

func (a *Account) notifyRecoveringOffline(ctx context.Context, reason string) {
	if !a.markOfflineNotified(reason) {
		return
	}
	a.alertEvent(ctx, EventAccountOffline, AlertLevelWarn, "账号已掉线，正在自动恢复",
		fmt.Sprintf("账号 %s 出现登录凭证过期或认证掉线。原因：%s。系统会先发送本通知，再继续尝试 Go 协议续期；如仍失败则需要重新扫码登录。", a.CookieID, reason))
}

func (a *Account) markOfflineNotified(reason string) bool {
	a.mu.Lock()
	if a.offlineNotified {
		a.mu.Unlock()
		return false
	}
	a.offlineNotified = true
	a.offlineSince = time.Now()
	a.lastOfflineReason = reason
	a.mu.Unlock()
	return true
}

// alert 触发账号告警通知。handler 未注入或为 nil 时静默跳过。
func (a *Account) alert(ctx context.Context, level, title, body string) {
	a.alertEvent(ctx, EventTokenRenewal, level, title, body)
}

func (a *Account) alertEvent(ctx context.Context, eventType, level, title, body string) {
	if a.handler == nil {
		return
	}
	if h, ok := a.handler.(accountEventHandler); ok {
		h.OnAccountEvent(ctx, a.CookieID, eventType, level, title, body)
		return
	}
	a.handler.OnAccountAlert(ctx, a.CookieID, level, title, body)
}

func (a *Account) resetFailures() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connFailures = 0
}

func formatTimeOrUnknown(t time.Time) string {
	if t.IsZero() {
		return "未知"
	}
	return t.Format("2006-01-02 15:04:05")
}

// tryLoginStatusCheck 调用 mtop.taobao.idlemessage.pc.loginuser.get 做轻量登录态确认。
// 这个接口的成本低于完整 token 刷新，且可能顺手下发新的签名 Cookie；
// 因此在 session 失效后、接口续期前先跑一遍，避免已实现的登录态检查能力闲置。
func (a *Account) tryLoginStatusCheck(ctx context.Context) loginStatusCheckResult {
	checker, ok := a.mtop.(loginStatusChecker)
	if !ok {
		return loginStatusCheckResult{}
	}
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	a.mu.Lock()
	cookieStr := a.CookieStr
	a.mu.Unlock()
	requestCtx := ctx
	var cookieSession *mtop.CookieSession
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
		if snapshot, complete := cookierefresh.SnapshotFromMetadataOK(metadataJSON); complete {
			requestCtx, cookieSession = mtop.WithCookieSnapshot(ctx, snapshot)
		} else {
			requestCtx, cookieSession = mtop.WithFlatCookieSession(ctx, cookieStr)
		}
	}
	res, err := checker.CheckLoginStatusContext(requestCtx, cookieStr)
	if cookieSession != nil {
		value, snapshot, changed := cookieSession.State()
		if changed {
			metadata := cookierefresh.MetadataWithoutSnapshot(metadataJSON)
			if snapshot != nil {
				metadata = cookierefresh.MetadataWithSnapshot(metadataJSON, snapshot)
			}
			if persistErr := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, value, metadata, time.Now().Unix()); persistErr != nil {
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
func (a *Account) tryAPIRenew(ctx context.Context) bool {
	if a.renewer == nil {
		return false
	}
	renewed, _ := a.tryAPIRenewUsing(ctx, func(runCtx context.Context, cookieStr string, snapshot []cookierefresh.BrowserCookie) (*renew.Result, error) {
		return a.renewer.RenewAPIFirst(runCtx, cookieStr, snapshot)
	})
	return renewed
}

func (a *Account) tryAPIRenewUsing(ctx context.Context, call func(context.Context, string, []cookierefresh.BrowserCookie) (*renew.Result, error)) (bool, error) {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	if a.store != nil && a.store.Cookies != nil && !a.store.Cookies.GetStatus(ctx, a.CookieID) {
		return false, nil
	}
	a.mu.Lock()
	cookieStr := a.CookieStr
	a.mu.Unlock()
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
	updated := false
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
		metadata := cookierefresh.MetadataWithSnapshot(runtimeData.MetadataJSON, res.CookieSnapshot)
		if err := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, res.NewCookies, metadata, time.Now().Unix()); err != nil {
			a.logger.Warn("保存续期 Cookie 快照失败", "err", err)
			return false, err
		}
		persisted = true
	} else if len(res.SetCookies) > 0 && a.store != nil && a.store.Cookies != nil {
		if err := a.persistRenewFlatCookie(ctx, res.NewCookies); err != nil {
			a.logger.Warn("保存接口续期扁平 Cookie 失败", "err", err)
			return false, err
		}
		persisted = true
	}
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

func (a *Account) persistRenewFlatCookie(ctx context.Context, newCookies string) error {
	if a.store == nil || a.store.Cookies == nil {
		return nil
	}
	detail, err := a.store.Cookies.GetDetails(ctx, a.CookieID)
	if err != nil || detail == nil {
		if err != nil {
			return err
		}
		return db.ErrNotFound
	}
	// 没有权威 Jar 时，接口 Set-Cookie 只能更新兼容扁平值。不能把
	// Domain/Path/HttpOnly/PartitionKey 均未知的 Cookie 伪造成完整快照。
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	return a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, newCookies, metadata, time.Now().Unix())
}

func (a *Account) watchPendingAPIRenew(parent context.Context, result *renew.Result) {
	if result == nil || !result.HasPending() {
		return
	}
	a.pendingRenewWG.Add(1)
	go func() {
		defer a.pendingRenewWG.Done()
		ctx, cancel := context.WithTimeout(parent, 35*time.Second)
		defer cancel()
		late, waitErr := result.AwaitPending(ctx)
		if late == nil {
			if waitErr != nil {
				a.logger.Warn("等待静默续期底层响应失败", "err", waitErr)
			}
			return
		}
		if persistErr := a.persistPendingRenewCookies(ctx, late); persistErr != nil {
			a.logger.Warn("保存静默续期迟到 Cookie 失败", "err", persistErr)
			return
		}
		if waitErr != nil {
			a.logger.Warn("静默续期底层响应失败，已保存响应 Cookie", "err", waitErr)
		}
	}()
}

func (a *Account) persistPendingRenewCookies(ctx context.Context, result *renew.Result) error {
	if result == nil || len(result.SetCookies) == 0 || a.store == nil || a.store.Cookies == nil {
		return nil
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	credentialUnlock := a.store.LockAccountCredentials(a.CookieID)
	defer credentialUnlock()
	detail, err := a.store.Cookies.GetDetails(ctx, a.CookieID)
	if err != nil || detail == nil {
		if err == nil {
			err = db.ErrNotFound
		}
		return err
	}
	newCookies, metadata, changed := renew.RebaseResponseCookies(detail.Value, detail.MetadataJSON, result)
	if !changed {
		return nil
	}
	if err := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, newCookies, metadata, time.Now().Unix()); err != nil {
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
func (a *Account) adoptRecoveredCookie(ctx context.Context, newCookies, source string) bool {
	if strings.TrimSpace(newCookies) == "" {
		return false
	}
	a.mu.Lock()
	oldCookies := a.CookieStr
	a.mu.Unlock()
	if newCookies == oldCookies {
		return false
	}
	if a.store != nil && a.store.Cookies != nil {
		if err := a.store.Cookies.UpdateValueExisting(ctx, a.CookieID, newCookies); err != nil {
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

func (a *Account) notifyCredentialUpdated(ctx context.Context) {
	if handler, ok := a.handler.(credentialUpdateHandler); ok {
		handler.OnCredentialUpdated(ctx, a.CookieID)
	}
}

// retryDelay 按错误类型计算退避，并加入 0-30% 抖动。
// 多账号同时断线时，纯固定退避会让所有账号在同一秒重连，容易形成重连风暴。
func (a *Account) retryDelay(errMsg string) time.Duration {
	a.mu.Lock()
	f := a.connFailures
	a.mu.Unlock()
	if f < 1 {
		f = 1
	}
	base := exponentialSeconds(f)
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

func (a *Account) networkRetryDelay() time.Duration {
	a.mu.Lock()
	f := a.networkFailures
	a.mu.Unlock()
	if f < 1 {
		f = 1
	}
	return withRetryJitter(time.Duration(min(2+exponentialSeconds(f), 60)) * time.Second)
}

func exponentialSeconds(failures int) int {
	if failures < 1 {
		failures = 1
	}
	if failures > 30 {
		failures = 30
	}
	return 1 << failures
}

func withRetryJitter(base time.Duration) time.Duration {
	if base <= 0 {
		return 0
	}
	maxJitter := base * 3 / 10
	if maxJitter <= 0 {
		return base
	}
	n, err := rand.Int(rand.Reader, big.NewInt(int64(maxJitter)))
	if err != nil {
		// 熵源异常时使用时间纳秒兜底；这里只影响退避抖动，不用于安全令牌。
		return base + time.Duration(time.Now().UnixNano()%int64(maxJitter))
	}
	return base + time.Duration(n.Int64())
}

func isEstablishedNetworkError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
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

func (a *Account) recordShortDisconnect(connectedDuration time.Duration) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	if connectedDuration >= ShortConnectionThreshold {
		a.shortDisconnects = nil
		return false
	}
	now := time.Now()
	a.shortDisconnects = append(a.shortDisconnects, now)
	cutoff := now.Add(-FrequentDisconnectWindow)
	kept := a.shortDisconnects[:0]
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
func (a *Account) refreshToken(ctx context.Context) (string, string, error) {
	return a.refreshTokenWithMinGap(ctx, false)
}

// refreshTokenWithMinGap 保留旧签名以避免影响调用方；参考实现没有额外的一分钟
// Token 防抖，因此 enforceMinGap 不参与行为。
func (a *Account) refreshTokenWithMinGap(ctx context.Context, _ bool) (string, string, error) {
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()

	// refreshMu serializes the complete token/Cookie update transaction for an
	// account. A failed automatic verification also suppresses repeated token API
	// calls until the caller-side cooldown expires.
	if remaining := a.tokenCaptchaCooldownRemaining(); remaining > 0 {
		a.setLastTokenStatus(tokenRefreshSkippedCooldown)
		return "", "", fmt.Errorf("%w，剩余 %s", errTokenCaptchaCooldown, remaining.Round(time.Second))
	}

	a.reloadCookieFromDB(ctx)

	a.mu.Lock()
	cookieStr := a.CookieStr
	a.lastTokenRefresh = time.Now()
	a.lastTokenStatus = tokenRefreshStarted
	a.mu.Unlock()

	deviceID := strings.TrimSpace(a.deviceID)
	if deviceID == "" {
		if unb := protocol.TransCookies(cookieStr)["unb"]; unb != "" {
			deviceID = protocol.GenerateDeviceID(unb)
			a.mu.Lock()
			a.deviceID = deviceID
			a.mu.Unlock()
		}
	}
	for captchaRetry := 0; captchaRetry < 3; captchaRetry++ {
		var res *mtop.RefreshResult
		var err error
		if scoped, ok := a.mtop.(scopedTokenClient); ok {
			var snapshot []cookierefresh.BrowserCookie
			if a.store != nil && a.store.Cookies != nil {
				if detail, detailErr := a.store.Cookies.GetDetails(ctx, a.CookieID); detailErr == nil && detail != nil {
					snapshot = cookierefresh.SnapshotFromMetadata(detail.MetadataJSON)
				}
			}
			res, err = scoped.RefreshTokenWithCredentialContext(ctx, cookieStr, deviceID, snapshot)
		} else {
			res, err = a.mtop.RefreshTokenWithDeviceIDContext(ctx, cookieStr, deviceID)
		}
		// 参考实现无论业务结果为何，都先合并响应 Set-Cookie。本地还必须先把
		// 完整 Jar 持久化成功，避免当前 /reg 成功而下次重连回滚到旧凭证。
		var persistErr error
		cookieStr, persistErr = a.adoptTokenResponseCookies(ctx, cookieStr, res)
		if persistErr != nil {
			a.setLastTokenStatus(tokenRefreshFailedAPI)
			a.clearCurrentToken()
			return "", "", fmt.Errorf("保存 token 响应 Cookie: %w", persistErr)
		}
		if err != nil && mtop.IsRiskVerificationErr(err) {
			if recovered, ok := a.tryTokenCaptchaRecovery(ctx, cookieStr, deviceID, err); ok {
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
		a.lastMsgReceived = time.Time{}
		a.lastCaptchaFailure = time.Time{}
		a.tokenFetchFailures = 0
		a.lastTokenStatus = tokenRefreshSuccess
		a.mu.Unlock()
		return res.AccessToken, cookieStr, nil
	}

	a.setLastTokenStatus(tokenRefreshFailedCaptcha)
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
	return "", "", fmt.Errorf("滑块验证重试次数已达上限")
}

func (a *Account) clearCurrentToken() {
	a.mu.Lock()
	a.currentToken = ""
	a.tokenCredentialFP = ""
	a.mu.Unlock()
}

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
		detail, detailErr := a.store.Cookies.GetDetails(ctx, a.CookieID)
		if detailErr != nil {
			return cookieStr, detailErr
		}
		if detail == nil {
			return cookieStr, db.ErrNotFound
		}
		metadata := detail.MetadataJSON
		if res.CookieSnapshotComplete {
			snapshot := cookierefresh.NormalizeSnapshot(res.CookieSnapshot)
			if snapshot == nil {
				snapshot = []cookierefresh.BrowserCookie{}
			}
			metadata = cookierefresh.MetadataWithSnapshot(metadata, snapshot)
		} else if snapshot, snapshotOK := cookierefresh.SnapshotFromMetadataOK(metadata); snapshotOK {
			// 扁平结果不能凭空证明 Jar 完整；仅在已有权威 Jar 时按已知
			// Domain/Path 身份对值做兼容合并。
			snapshot = cookierefresh.ReconcileSnapshotWithCookieString(snapshot, res.UpdatedCookies)
			metadata = cookierefresh.MetadataWithSnapshot(metadata, snapshot)
		} else {
			metadata = cookierefresh.MetadataWithoutSnapshot(metadata)
		}
		if err := a.store.Cookies.UpdateRenewalCookie(ctx, a.CookieID, res.UpdatedCookies, metadata, time.Now().Unix()); err != nil {
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

func (a *Account) tryTokenCaptchaRecovery(ctx context.Context, cookieStr, deviceID string, err error) (*mtop.RefreshResult, bool) {
	h, ok := a.handler.(tokenCaptchaHandler)
	if !ok {
		return nil, false
	}
	var riskErr *mtop.RiskVerificationError
	if !errors.As(err, &riskErr) || strings.TrimSpace(riskErr.VerificationURL) == "" {
		return nil, false
	}
	a.alertEvent(ctx, EventSecurityVerification, AlertLevelWarn, "闲鱼要求滑块验证",
		"token 刷新触发闲鱼风控验证，系统将尝试自动完成滑块并合并 x5sec。")
	result, ok := h.OnTokenCaptchaVerification(ctx, a.CookieID, cookieStr, riskErr.VerificationURL, deviceID)
	if !ok || result == nil || strings.TrimSpace(result.UpdatedCookies) == "" {
		return nil, false
	}
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

func (a *Account) markTokenCaptchaFailure() {
	a.mu.Lock()
	a.lastCaptchaFailure = time.Now()
	a.mu.Unlock()
}

func (a *Account) tokenCaptchaCooldownRemaining() time.Duration {
	a.mu.Lock()
	lastFailure := a.lastCaptchaFailure
	a.mu.Unlock()
	if lastFailure.IsZero() {
		return 0
	}
	remaining := TokenCaptchaFailureCooldown - time.Since(lastFailure)
	if remaining < 0 {
		return 0
	}
	return remaining
}

// acquireToken is kept for internal callers and tests, but intentionally does
// not reuse the persisted accessToken. Official web reconnects always invoke
// the login.token API before /reg.
func (a *Account) acquireToken(ctx context.Context) (string, string, error) {
	return a.acquireTokenWithMinGap(ctx, false)
}

// acquireRuntimeToken is retained as a compatibility wrapper for focused
// tests and older internal callers. It follows the same fresh-token rule.
func (a *Account) acquireRuntimeToken(ctx context.Context) (string, string, error) {
	return a.acquireFreshConnectionToken(ctx)
}

func (a *Account) acquireTokenWithMinGap(ctx context.Context, _ bool) (string, string, error) {
	// Invalidate any access token left by an older process/attempt before asking
	// MTOP for the token that will be bound to this connection.
	a.clearTokenCache(ctx)
	return a.refreshToken(ctx)
}

func (a *Account) setLastTokenStatus(status string) {
	a.mu.Lock()
	a.lastTokenStatus = status
	a.mu.Unlock()
}

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
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "network") || strings.Contains(msg, "connection") || strings.Contains(msg, "请求失败") {
		return tokenRefreshFailedNetwork
	}
	return tokenRefreshFailedAPI
}

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
func (a *Account) saveTokenCache(ctx context.Context, deviceID, accessToken string, serverExpireAt int64, credentialFP string) {
	if accessToken == "" {
		return
	}
	now := time.Now()
	expiresAt, refreshAt := tokenRotationSchedule(serverExpireAt, now)
	tokenFP := tokenFingerprint(accessToken)
	a.mu.Lock()
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
	expireAt := effectiveTokenExpireAt(serverExpireAt, now)
	if expireAt == 0 {
		// 服务端未给有效期时仍使用保守运行时轮换时间，但不把推测期限
		// 伪装成服务端缓存期限。
		a.logger.Warn("token API 未返回可用过期时间，使用保守轮换时间", "refresh_at", refreshAt)
		a.clearTokenCache(ctx)
		return
	}
	if err := a.store.Tokens.SaveBound(ctx, a.CookieID, deviceID, accessToken, expireAt, credentialFP); err != nil {
		a.logger.Warn("缓存 accessToken 失败", "err", err)
	}
}

// tokenFingerprint 用不可逆摘要标识 Token，便于判断服务端是否轮换了 Token，
// 同时避免日志泄露可用于 WS 注册的凭证原文。
func tokenFingerprint(token string) string {
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
	if err := a.store.Tokens.Clear(ctx, a.CookieID); err != nil {
		a.logger.Warn("清除 token 缓存失败", "err", err)
	}
}

// databaseCredentialFingerprint returns the complete DB credential state that
// produced cookieStr. It must be called while the account credential lock is
// held when a Store is present.
func (a *Account) databaseCredentialFingerprint(ctx context.Context, cookieStr string) (string, error) {
	if a.store == nil || a.store.Cookies == nil {
		return credentialStateFingerprint(cookieStr, ""), nil
	}
	detail, err := a.store.Cookies.GetDetails(ctx, a.CookieID)
	if err != nil {
		return "", err
	}
	if detail == nil {
		return "", db.ErrNotFound
	}
	_, snapshotComplete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if strings.TrimSpace(detail.Value) == "" && !snapshotComplete {
		return "", fmt.Errorf("数据库 Cookie 为空且没有权威 Jar")
	}
	if credentialCookieFingerprint(detail.Value) != credentialCookieFingerprint(cookieStr) {
		return "", fmt.Errorf("token 请求期间数据库 Cookie 已变化")
	}
	return credentialStateFingerprint(detail.Value, detail.MetadataJSON), nil
}

// reloadCookieFromDB 复读 DB cookie：与内存不同则采纳，并清 token 缓存。普通 Cookie
// 更新不轮换页面生命周期内的 device ID；显式登录由 Manager 重建 Account。
func (a *Account) reloadCookieFromDB(ctx context.Context) bool {
	if a.store == nil || a.store.Cookies == nil {
		return false
	}
	d, err := a.store.Cookies.GetDetails(ctx, a.CookieID)
	if err != nil || d == nil {
		return false
	}
	if strings.TrimSpace(d.Value) == "" {
		if _, complete := cookierefresh.SnapshotFromMetadataOK(d.MetadataJSON); !complete {
			return false
		}
	}
	databaseFP := credentialStateFingerprint(d.Value, d.MetadataJSON)
	a.mu.Lock()
	currentFP := a.credentialFP
	if currentFP == "" {
		currentFP = credentialStateFingerprint(a.CookieStr, "")
	}
	a.mu.Unlock()
	if databaseFP == currentFP {
		return false
	}
	a.logger.Info("检测到 DB cookie 已更新，重新加载", "account", a.CookieID)
	a.replaceCredentialState(d.Value, databaseFP)
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
	a.mu.Lock()
	a.lastCaptchaFailure = time.Time{}
	a.mu.Unlock()
	return true
}

func (a *Account) cookieSnapshotMatchesDB(ctx context.Context, expectedFP string) bool {
	if a.store == nil || a.store.Cookies == nil {
		return true
	}
	detail, err := a.store.Cookies.GetDetails(ctx, a.CookieID)
	if err != nil || detail == nil {
		a.logger.Warn("WS 注册前读取最新 Cookie 失败，放弃本次连接", "err", err)
		return false
	}
	_, snapshotComplete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if strings.TrimSpace(detail.Value) == "" && !snapshotComplete {
		a.logger.Warn("WS 注册前最新 Cookie 为空且没有权威 Jar，放弃本次连接")
		return false
	}
	if expectedFP == "" {
		a.logger.Warn("WS 注册 token 缺少绑定的凭证状态，放弃本次连接")
		return false
	}
	return credentialStateFingerprint(detail.Value, detail.MetadataJSON) == expectedFP
}

// RuntimeStatus 返回账号当前连接状态的线程安全快照。
func (a *Account) RuntimeStatus() RuntimeStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	remaining := int64(0)
	if !a.tokenExpiresAt.IsZero() {
		remaining = int64(time.Until(a.tokenExpiresAt).Seconds())
		if remaining < 0 {
			remaining = 0
		}
	}
	return RuntimeStatus{
		State:                 a.runtimeState,
		Message:               a.runtimeMessage,
		Connected:             a.conn != nil && a.runtimeState == RuntimeOnline,
		Failures:              a.connFailures,
		UpdatedAt:             a.runtimeUpdatedAt,
		TokenAcquiredAt:       a.tokenAcquiredAt,
		TokenExpiresAt:        a.tokenExpiresAt,
		TokenRefreshAt:        a.tokenRefreshAt,
		TokenRemainingSeconds: remaining,
		TokenRefreshStatus:    a.lastTokenStatus,
	}
}

func (a *Account) tokenRetryDelay() time.Duration {
	a.mu.Lock()
	expiresAt := a.tokenExpiresAt
	failures := a.tokenFetchFailures
	a.mu.Unlock()
	delay := time.Minute
	if failures > 1 {
		delay = 2 * time.Minute
	}
	if !expiresAt.IsZero() && time.Until(expiresAt) <= 2*time.Minute {
		delay = 30 * time.Second
	}
	return delay
}

func (a *Account) notifyTransportReady(ctx context.Context) {
	if handler, ok := a.handler.(transportReadyHandler); ok {
		handler.OnTransportReady(ctx, a.CookieID)
	}
}

func (a *Account) setRuntimeState(state, message string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.runtimeState = state
	a.runtimeMessage = message
	a.runtimeUpdatedAt = time.Now()
}

func (a *Account) setRuntimeError(ctx context.Context, err error) {
	msg := strings.ToLower(errString(err))
	a.mu.Lock()
	prev := a.runtimeState
	a.mu.Unlock()
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
	conn, myID, err := a.currentSenderState()
	if err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := conn.SendText(sendCtx, myID, chatID, toUserID, text); err != nil {
		return err
	}
	if observer, ok := a.handler.(outgoingChatHandler); ok {
		key, _ := ctx.Value(outgoingMessageKeyContextKey{}).(string)
		if err := observer.HandleOutgoingChatMessage(ctx, OutgoingChatMessage{
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
	conn, myID, err := a.currentSenderState()
	if err != nil {
		return err
	}
	sendCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
	defer cancel()
	return conn.SendImage(sendCtx, myID, chatID, toUserID, imageURL, 800, 600)
}

func (a *Account) currentSenderState() (WSConn, string, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.conn == nil {
		return nil, "", fmt.Errorf("%w: 账号 %s 当前没有可用 WebSocket 连接", automation.ErrMessageNotSent, a.CookieID)
	}
	myID := strings.TrimSpace(a.UserID)
	if myID == "" {
		myID = protocol.TransCookies(a.CookieStr)["unb"]
	}
	if myID == "" {
		return nil, "", fmt.Errorf("%w: 账号 %s 缺少 unb，无法发送消息", automation.ErrMessageNotSent, a.CookieID)
	}
	return a.conn, myID, nil
}

// FetchChatHistory reuses the account's registered IM connection. Keeping this
// optional capability outside WSConn avoids forcing non-chat test transports to
// implement history retrieval.
func (a *Account) FetchChatHistory(ctx context.Context, chatID string, cursor int64, limit int) (map[string]any, string, error) {
	conn, myID, err := a.currentSenderState()
	if err != nil {
		return nil, "", err
	}
	history, ok := conn.(interface {
		ListUserMessages(context.Context, string, int64, int) (map[string]any, error)
	})
	if !ok {
		return nil, "", errors.New("当前 WebSocket 连接不支持聊天历史")
	}
	body, err := history.ListUserMessages(ctx, chatID, cursor, limit)
	return body, myID, err
}

func (a *Account) FetchChatConversations(ctx context.Context, cursor int64, limit int) (map[string]any, string, error) {
	conn, myID, err := a.currentSenderState()
	if err != nil {
		return nil, "", err
	}
	fetcher, ok := conn.(interface {
		ListConversations(context.Context, int64, int) (map[string]any, error)
	})
	if !ok {
		return nil, "", errors.New("当前 WebSocket 连接不支持历史会话")
	}
	body, err := fetcher.ListConversations(ctx, cursor, limit)
	return body, myID, err
}

// AutomationReady 报告自动化消息是否可以立即使用当前 WS 连接发送。
func (a *Account) AutomationReady() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.conn != nil && a.runtimeState == RuntimeOnline
}

func (a *Account) replaceCookieStr(cookieStr string) {
	a.replaceCredentialState(cookieStr, credentialStateFingerprint(cookieStr, ""))
}

func (a *Account) replaceCredentialState(cookieStr, credentialFP string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.CookieStr = cookieStr
	a.credentialFP = credentialFP
	if unb := protocol.TransCookies(cookieStr)["unb"]; unb != "" {
		a.UserID = unb
	}
}

// rotatePageDeviceID 对应官网 auto-login 成功后的 location.reload()：
// 新 FishEngine 使用新的 UUID-userID，普通 Set-Cookie 与自然重连不会调用它。
func (a *Account) rotatePageDeviceID() {
	a.mu.Lock()
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
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	// 外部调用通常发生在一次网络请求写回之后。调用排队期间可能已有更新的
	// Cookie 落库，因此参数只作为无 Store 场景的兼容值；有 Store 时始终
	// 复读权威数据库，绝不把较旧的请求结果重新写回运行时。
	metadataJSON := ""
	if a.store != nil && a.store.Cookies != nil {
		detail, err := a.store.Cookies.GetDetails(context.Background(), a.CookieID)
		if err != nil || detail == nil {
			a.logger.Warn("同步运行时 Cookie 前读取数据库失败", "err", err)
			return
		}
		if strings.TrimSpace(detail.Value) == "" {
			if _, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); !complete {
				a.logger.Warn("同步运行时 Cookie 时数据库值为空且无权威 Jar")
				return
			}
		}
		cookieStr = detail.Value
		metadataJSON = detail.MetadataJSON
	}
	credentialFP := credentialStateFingerprint(cookieStr, metadataJSON)
	a.mu.Lock()
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
