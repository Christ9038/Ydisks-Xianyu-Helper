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
	"sync"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
	"xianyu-go/internal/xianyu/renew"
	"xianyu-go/internal/xianyu/ws"
)

// 账号运行时参数。
const (
	MaxConnectionFailures       = 5               // 连续失败上限，触发密码登录刷新
	TokenFetchDisableThreshold  = 100             // 参考实现：连续真实 token 故障 100 次后禁用
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
	// OnPasswordLoginRefresh 连续失败时触发浏览器密码登录刷新；返回是否成功。
	OnPasswordLoginRefresh(ctx context.Context, cookieID string) bool
	// OnAccountAlert 账号告警通知（token 失效/自动恢复失败/风控验证等）。
	// level 取 AlertLevel* 常量。实现方应把告警推送到该账号绑定的通知渠道。
	OnAccountAlert(ctx context.Context, cookieID, level, title, body string)
}

type accountEventHandler interface {
	OnAccountEvent(ctx context.Context, cookieID, eventType, level, title, body string)
}

type tokenCaptchaHandler interface {
	OnTokenCaptchaVerification(ctx context.Context, cookieID, cookieStr, verificationURL, deviceID string) (*mtop.RefreshResult, bool)
}

const (
	tokenRefreshStarted            = "started"
	tokenRefreshSuccess            = "success"
	tokenRefreshSuccessFromCache   = "success_from_cache"
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

// RuntimeStatus 是账号引擎的实时连接状态，不写入数据库。
type RuntimeStatus struct {
	State     string    `json:"state"`
	Message   string    `json:"message,omitempty"`
	Connected bool      `json:"connected"`
	Failures  int       `json:"failures"`
	UpdatedAt time.Time `json:"updated_at"`
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
	wsDialer wsDialer
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
}

type debounceEntry struct {
	timer    *time.Timer
	lastMsg  ChatMessage
	deadline time.Time
}

// WSConn 是 Account 对 ws 连接的最小契约。*ws.Conn 实现该接口；
// 测试可注入 fakeWSConn 以隔离真实 WS 握手与网络。
type WSConn interface {
	HeartbeatLoop(ctx context.Context, interval time.Duration) error
	ReceiveLoop(ctx context.Context, onMessage func(map[string]any)) error
	Close() error
	SendText(ctx context.Context, myID, cid, toID, text string) error
	SendImage(ctx context.Context, myID, cid, toID, imageURL string, width, height int) error
}

// wsDialer 抽象 ws.Dial，便于测试替身。
type wsDialer interface {
	Dial(ctx context.Context, cfg ws.Config, logger *slog.Logger) (WSConn, error)
}

type defaultDialer struct{}

func (defaultDialer) Dial(ctx context.Context, cfg ws.Config, logger *slog.Logger) (WSConn, error) {
	return ws.Dial(ctx, cfg, logger)
}

type cookieRenewer interface {
	RenewAPIFirst(ctx context.Context, cookiesStr string) (*renew.Result, error)
}

type loginStatusChecker interface {
	CheckLoginStatusContext(ctx context.Context, cookiesStr string) (*mtop.LoginStatusResult, error)
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
	a := &Account{
		CookieID:         cfg.CookieID,
		CookieStr:        cfg.CookieStr,
		UserID:           myid,
		store:            cfg.Store,
		mtop:             mtopClient,
		renewer:          renewer,
		wsDialer:         defaultDialer{},
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
	}
	if cfg.Store != nil && cfg.Store.Tokens != nil {
		deviceID, err := cfg.Store.Tokens.GetOrCreateDeviceID(context.Background(), cfg.CookieID, a.deviceID)
		if err != nil {
			logger.Error("读取或保存账号 device ID 失败", "account", cfg.CookieID, "err", err)
		} else {
			a.deviceID = deviceID
		}
	}
	if cfg.Store != nil {
		a.reply = NewReplyService(cfg.CookieID, cfg.Store, a, nil, NewAIReplier(cfg.CookieID, cfg.Store, logger), logger)
	}
	return a
}

// Run 阻塞运行账号主循环，直到 ctx 取消或不可恢复错误。
// 调用方应在独立 goroutine 中运行；Stop 可优雅停止。
func (a *Account) Run(parent context.Context) error {
	ctx, cancel := context.WithCancel(parent)
	defer cancel()
	invalidTokenRetried := false
	a.mu.Lock()
	a.stopFn = cancel
	a.mu.Unlock()
	a.taskMu.Lock()
	a.runtimeCtx = ctx
	a.accepting = true
	a.taskMu.Unlock()

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
		token, cookieStr, err := a.acquireRuntimeToken(ctx)
		if err != nil {
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
			if !nonCounted && tokenFailures >= TokenFetchDisableThreshold {
				reason := fmt.Sprintf("连续 %d 次获取 token 失败，最后错误：%s", TokenFetchDisableThreshold, errString(err))
				a.disableForTokenFailures(ctx, reason)
				return nil
			}
			if mtop.IsRiskVerificationErr(err) {
				a.logger.Warn("闲鱼要求安全验证，等待 5 秒后重试", "err", err)
				a.alertEvent(ctx, EventSecurityVerification, AlertLevelWarn, "闲鱼要求安全验证",
					"账号触发闲鱼风控验证（滑块/人脸等），系统会按参考策略继续重试。")
				if sleepCtx(ctx, 5*time.Second) != nil {
					return ctx.Err()
				}
				continue
			}
			if mtop.IsSessionExpiredErr(err) {
				a.logger.Warn("session 已失效，进入一次密码登录恢复链", "err", err)
				a.clearTokenCache(ctx)
				a.notifyOffline(ctx, "token/session 已失效："+errString(err))
				if err := a.handleMaxFailures(ctx); err != nil {
					return err
				}
				continue
			}
			delay := 5 * time.Second
			if status == tokenRefreshSkippedCooldown {
				delay = 5 * time.Minute
			}
			if sleepCtx(ctx, delay) != nil {
				return ctx.Err()
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

		// 2) 连接 WS + 注册。
		var recorder func(direction, rawText, parsedJSON, parseStatus, errMsg string)
		if a.store != nil && a.store.WSMessages != nil {
			recorder = func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
				if err := a.store.WSMessages.Add(context.Background(), db.WSMessage{
					CookieID:    a.CookieID,
					Direction:   direction,
					RawText:     rawText,
					ParsedJSON:  parsedJSON,
					MessageKind: "",
					ParseStatus: parseStatus,
					Error:       errMsg,
				}); err != nil {
					a.logger.Error("记录 WS 报文失败", "cookie_id", a.CookieID, "err", err)
				}
			}
		}
		a.mu.Lock()
		deviceID := a.deviceID
		a.mu.Unlock()
		credentialUnlock := func() {}
		if a.store != nil {
			credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
		}
		if !a.cookieSnapshotMatchesDB(ctx, cookieStr) {
			credentialUnlock()
			a.reloadCookieFromDB(ctx)
			continue
		}
		attemptStartedAt := time.Now()
		conn, err := a.wsDialer.Dial(ctx, ws.Config{
			CookieStr:   cookieStr,
			DeviceID:    deviceID,
			AccessToken: token,
			Recorder:    recorder,
		}, a.logger)
		credentialUnlock()
		if err != nil {
			a.logger.Error("WS 连接失败", "err", err)
			if ws.IsInvalidTokenError(err) && !invalidTokenRetried {
				invalidTokenRetried = true
				a.clearCurrentToken()
				a.clearTokenCache(ctx)
				a.setRuntimeState(RuntimeConnecting, "消息凭证被拒绝，正在重新获取一次新凭证")
				continue
			}
			if ws.IsConnectLimitError(err) {
				a.setRuntimeState(RuntimeReconnecting, "消息服务连接数受限，正在限速重试")
				if sleepCtx(ctx, withRetryJitter(30*time.Second)) != nil {
					return ctx.Err()
				}
				continue
			}
			a.mu.Lock()
			a.connFailures++
			failures := a.connFailures
			a.mu.Unlock()
			if ws.IsInvalidTokenError(err) || ws.IsAuthenticationError(err) {
				a.clearCurrentToken()
				a.clearTokenCache(ctx)
			}
			a.setRuntimeState(RuntimeReconnecting, "消息服务连接失败，稍后自动重试")
			if failures >= MaxConnectionFailures {
				if err := a.handleMaxFailures(ctx); err != nil {
					return err
				}
				continue
			}
			if sleepCtx(ctx, a.retryDelay("ws_dial")) != nil {
				return ctx.Err()
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
		invalidTokenRetried = false
		a.setRuntimeState(RuntimeOnline, "消息服务连接正常")
		if shouldRecovered {
			a.alertEvent(ctx, EventAccountRecovered, AlertLevelInfo, "账号已恢复在线",
				fmt.Sprintf("账号 %s 已重新连接闲鱼消息服务。掉线开始时间：%s。", a.CookieID, formatTimeOrUnknown(offlineSince)))
		}

		// 3) 健康连接只维持心跳和收包；token 只在下一次注册前更新。
		hbCtx, hbCancel := context.WithCancel(ctx)
		var hbErr error
		hbDone := make(chan struct{})
		go func() {
			hbErr = conn.HeartbeatLoop(hbCtx, HeartbeatInterval)
			hbCancel()
			close(hbDone)
		}()

		recvErr := conn.ReceiveLoop(ctx, a.dispatch)
		hbCancel()
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

		// 正常 close 的 async-for 会直接进入下一轮，不计任何失败。
		if recvErr == nil {
			a.setRuntimeState(RuntimeReconnecting, "消息连接已结束，正在重新连接")
			continue
		}

		if isEstablishedNetworkError(recvErr) {
			a.mu.Lock()
			a.networkFailures++
			networkFailures := a.networkFailures
			a.mu.Unlock()
			if a.recordShortDisconnect(connectedDuration) {
				reason := "未知原因频繁断开连接"
				a.disableForFrequentDisconnects(ctx, reason)
				return nil
			}
			a.setRuntimeState(RuntimeReconnecting, "网络连接已断开，稍后自动重连")
			a.logger.Warn("WS 网络连接结束", "err", recvErr, "network_failures", networkFailures, "connected_duration", connectedDuration.Round(time.Second), "heartbeat_err", hbErr)
			if networkFailures >= MaxNetworkFailures {
				a.mu.Lock()
				a.networkFailures = 0
				a.mu.Unlock()
				if sleepCtx(ctx, 120*time.Second) != nil {
					return ctx.Err()
				}
				continue
			}
			if sleepCtx(ctx, a.networkRetryDelay()) != nil {
				return ctx.Err()
			}
			continue
		}

		a.mu.Lock()
		a.currentToken = ""
		a.connFailures++
		failures := a.connFailures
		a.mu.Unlock()
		if time.Since(attemptStartedAt) < 15*time.Second {
			a.clearTokenCache(ctx)
		}
		a.setRuntimeState(RuntimeReconnecting, "消息连接已断开，稍后自动重连")
		a.logger.Warn("WS 连接结束", "err", recvErr, "failures", failures, "heartbeat_err", hbErr)

		if failures >= MaxConnectionFailures {
			if err := a.handleMaxFailures(ctx); err != nil {
				return err
			}
			continue
		}
		if sleepCtx(ctx, a.retryDelay(errString(recvErr))) != nil {
			return ctx.Err()
		}
	}
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

// handleMaxFailures 连续失败达上限：尝试密码登录刷新；失败则返回 err 触发上层重启实例。
func (a *Account) handleMaxFailures(ctx context.Context) error {
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	a.logger.Warn("连续失败达上限，触发密码登录刷新", "failures", MaxConnectionFailures)
	a.notifyOffline(ctx, fmt.Sprintf("消息服务连续认证/连接失败 %d 次，开始自动恢复", MaxConnectionFailures))
	if a.handler != nil && a.handler.OnPasswordLoginRefresh(ctx, a.CookieID) {
		if d, err := a.store.Cookies.GetDetails(ctx, a.CookieID); err == nil && d != nil && d.Value != "" {
			a.replaceCookieStr(d.Value)
			a.clearCurrentToken()
			a.clearTokenCache(ctx)
		}
		a.logger.Info("密码登录刷新成功，重置失败计数")
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
	a.mu.Lock()
	if a.offlineNotified {
		a.mu.Unlock()
		return
	}
	a.offlineNotified = true
	a.offlineSince = time.Now()
	a.lastOfflineReason = reason
	a.mu.Unlock()
	a.alertEvent(ctx, EventAccountOffline, AlertLevelWarn, "账号已掉线，正在自动恢复",
		fmt.Sprintf("账号 %s 出现登录凭证过期或认证掉线。原因：%s。系统会先发送本通知，再继续尝试轻量续期、浏览器续期或密码登录恢复。", a.CookieID, reason))
}

func (a *Account) disableForTokenFailures(ctx context.Context, reason string) {
	a.logger.Error("连续获取 token 失败，自动禁用账号", "account", a.CookieID, "reason", reason)
	if a.store != nil && a.store.Cookies != nil {
		if err := a.store.Cookies.SetStatusWithReason(ctx, a.CookieID, false, reason); err != nil {
			a.logger.Error("自动禁用账号失败", "account", a.CookieID, "err", err)
		}
	}
	a.clearTokenCache(ctx)
	a.setRuntimeState(RuntimeStopped, "连续获取 token 失败，账号已自动禁用")
	a.alertEvent(ctx, EventAccountDisabled, AlertLevelCritical, "账号已自动禁用", reason)
}

func (a *Account) disableForFrequentDisconnects(ctx context.Context, reason string) {
	a.logger.Error("频繁短连接断开，自动禁用账号", "account", a.CookieID, "reason", reason)
	if a.store != nil && a.store.Cookies != nil {
		if err := a.store.Cookies.SetStatusWithReason(ctx, a.CookieID, false, reason); err != nil {
			a.logger.Error("自动禁用账号失败", "account", a.CookieID, "err", err)
		}
	}
	a.setRuntimeState(RuntimeStopped, "频繁断开连接，账号已自动禁用")
	a.alertEvent(ctx, EventAccountDisabled, AlertLevelCritical, "账号已自动禁用", reason)
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
// 这个接口的成本低于完整 token 刷新和浏览器动作，且可能顺手下发新的签名 Cookie；
// 因此在 session 失效后、接口续期前先跑一遍，避免已实现的登录态检查能力闲置。
func (a *Account) tryLoginStatusCheck(ctx context.Context) loginStatusCheckResult {
	checker, ok := a.mtop.(loginStatusChecker)
	if !ok {
		return loginStatusCheckResult{}
	}
	a.mu.Lock()
	cookieStr := a.CookieStr
	a.mu.Unlock()
	res, err := checker.CheckLoginStatusContext(ctx, cookieStr)
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
	if a.adoptRecoveredCookie(ctx, res.UpdatedCookies, "登录态检查") {
		a.logger.Info("登录态检查刷新了 Cookie", "status", res.Status, "message", res.Message)
		return loginStatusCheckResult{recovered: true}
	}
	a.logger.Info("登录态检查未产生可用 Cookie 更新", "status", res.Status, "message", res.Message)
	return loginStatusCheckResult{}
}

// tryAPIRenew 是密码登录前的轻量恢复层。
// 闲鱼很多“登录态滑向过期”的场景会先允许 hasLogin/silentHasLogin/
// setLoginSettings 刷新一批 Cookie；直接上密码登录更重，也更容易触发风控。
// 因此这里先尝试接口续期：只有 setLoginSettings 确认成功才短路后续恢复；
// 如果只拿到部分 Cookie，仍会保存并清 token，但继续降级到浏览器/密码登录。
func (a *Account) tryAPIRenew(ctx context.Context) bool {
	if a.renewer == nil {
		return false
	}
	a.mu.Lock()
	cookieStr := a.CookieStr
	a.mu.Unlock()
	res, err := a.renewer.RenewAPIFirst(ctx, cookieStr)
	if err != nil {
		a.logger.Warn("接口续期失败", "err", err)
		return false
	}
	if res == nil {
		return false
	}
	updated := false
	if res.NewCookies != "" && res.NewCookies != cookieStr {
		updated = a.adoptRecoveredCookie(ctx, res.NewCookies, "接口续期")
	}
	if res.Success {
		if !updated {
			a.setRuntimeState(RuntimeConnecting, "登录凭证已接口续期，正在重新连接")
		}
		a.logger.Info("接口续期成功", "method", res.RenewMethod, "updated", strings.Join(res.UpdatedCookieNames, ","))
		return true
	}
	if updated {
		a.logger.Info("接口续期返回部分 Cookie 更新，继续降级恢复", "updated", strings.Join(res.UpdatedCookieNames, ","))
		return false
	}
	a.logger.Info("接口续期未产生可用恢复", "success", res.Success, "message", res.Message)
	return false
}

// adoptRecoveredCookie 统一接收“轻量检查/接口续期/浏览器恢复”拿到的新 Cookie。
// Cookie 变更意味着旧 accessToken 与 session 绑定关系失效，必须清 token 缓存，
// 让下一轮 acquireToken 使用新 Cookie 重新派生凭证。
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
	a.replaceCookieStr(newCookies)
	a.clearCurrentToken()
	if a.store != nil && a.store.Cookies != nil {
		if err := a.store.Cookies.UpdateValueExisting(ctx, a.CookieID, newCookies); err != nil {
			a.logger.Error(source+"后保存 cookie 失败", "cookie_id", a.CookieID, "err", err)
		}
	}
	a.clearTokenCache(ctx)
	a.setRuntimeState(RuntimeConnecting, source+"已更新登录凭证，正在重新连接")
	return true
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
// 成功时把 token + device_id 缓存到 DB（saveTokenCache），供进程重启复用与
// 稳态重连时跳过 mtop（acquireToken 命中缓存即直接返回）。
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
		res, err := a.mtop.RefreshTokenWithDeviceIDContext(ctx, cookieStr, deviceID)
		// 参考实现无论业务结果为何，都先合并并持久化响应 Set-Cookie。
		cookieStr = a.adoptTokenResponseCookies(ctx, cookieStr, res)
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
		a.saveTokenCache(ctx, deviceID, res.AccessToken, res.AccessTokenExpireAt, cookieStr)
		a.mu.Lock()
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
	a.mu.Unlock()
}

func (a *Account) adoptTokenResponseCookies(ctx context.Context, cookieStr string, res *mtop.RefreshResult) string {
	if res == nil || strings.TrimSpace(res.UpdatedCookies) == "" || res.UpdatedCookies == cookieStr {
		return cookieStr
	}
	a.replaceCookieStr(res.UpdatedCookies)
	if a.store != nil && a.store.Cookies != nil {
		if err := a.store.Cookies.UpdateValueExisting(ctx, a.CookieID, res.UpdatedCookies); err != nil {
			a.logger.Error("token 响应后保存 cookie 失败", "cookie_id", a.CookieID, "err", err)
		}
	}
	return res.UpdatedCookies
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
	a.replaceCookieStr(result.UpdatedCookies)
	if a.store != nil && a.store.Cookies != nil {
		if err := a.store.Cookies.UpdateValueExisting(ctx, a.CookieID, result.UpdatedCookies); err != nil {
			a.logger.Error("滑块验证后保存 cookie 失败", "cookie_id", a.CookieID, "err", err)
			return nil, false
		}
	}
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

// acquireToken 取用于 WS /reg 的 accessToken：缓存命中且未过期则跳过 mtop（降低风控
// 与瞬时失败影响），否则调 refreshToken 重新派生并落库缓存。
//
// 设计：稳态重连时复用未过期缓存 token，避免每次重连都打 mtop（降低风控触发）。
// 缓存 token 若已被服务端失效，dial 后会短连接断开 → clearTokenCache → 下轮重新派生，自愈。
func (a *Account) acquireToken(ctx context.Context) (string, string, error) {
	return a.acquireTokenWithMinGap(ctx, false)
}

// acquireRuntimeToken 对齐参考主循环：网络重连时只要内存 token 仍存在就直接
// 复用；只有认证分支明确清空它后，才重新检查数据库缓存或调用 Token API。
func (a *Account) acquireRuntimeToken(ctx context.Context) (string, string, error) {
	a.mu.Lock()
	token := a.currentToken
	cookieStr := a.CookieStr
	a.mu.Unlock()
	if strings.TrimSpace(token) != "" {
		return token, cookieStr, nil
	}
	return a.acquireToken(ctx)
}

func (a *Account) acquireTokenWithMinGap(ctx context.Context, _ bool) (string, string, error) {
	if token, ok := a.cachedTokenIfFresh(ctx); ok {
		a.mu.Lock()
		a.currentToken = token
		a.lastTokenRefresh = time.Now()
		a.lastTokenStatus = tokenRefreshSuccessFromCache
		cookieStr := a.CookieStr
		a.mu.Unlock()
		a.logger.Info("使用缓存的 accessToken，跳过 mtop 刷新")
		return token, cookieStr, nil
	}
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

// cachedTokenIfFresh 返回未过期的缓存 accessToken。无缓存 / 已过期返回 ok=false。
func (a *Account) cachedTokenIfFresh(ctx context.Context) (string, bool) {
	if a.store == nil || a.store.Tokens == nil {
		return "", false
	}
	tk, err := a.store.Tokens.Get(ctx, a.CookieID)
	if err != nil {
		return "", false
	}
	a.mu.Lock()
	cookieStr := a.CookieStr
	a.mu.Unlock()
	expectedFingerprint := credentialCookieFingerprint(cookieStr)
	if tk.AccessToken == "" || tk.ExpireAt <= time.Now().Unix() ||
		tk.CookieFingerprint == "" || tk.CookieFingerprint != expectedFingerprint {
		// 参考实现发现过期缓存后立即删除整行，再进入标准 token 请求。
		if tk.AccessToken != "" {
			a.clearTokenCache(ctx)
		}
		return "", false
	}
	// 命中缓存时同步复用其 device_id，保证 token 与 /reg.did 一致。
	if tk.DeviceID != "" {
		a.mu.Lock()
		a.deviceID = tk.DeviceID
		a.mu.Unlock()
	}
	return tk.AccessToken, true
}

// saveTokenCache uses the expiry returned by the token API. A response without
// a usable accessTokenExpiredTime remains valid for the current process only;
// it is deliberately not assigned a guessed multi-hour lifetime.
func (a *Account) saveTokenCache(ctx context.Context, deviceID, accessToken string, serverExpireAt int64, cookieStr string) {
	if a.store == nil || a.store.Tokens == nil || accessToken == "" {
		return
	}
	expireAt := effectiveTokenExpireAt(serverExpireAt, time.Now())
	if expireAt == 0 {
		a.logger.Warn("token API 未返回可用过期时间，本次 token 不持久化")
		a.clearTokenCache(ctx)
		return
	}
	if err := a.store.Tokens.SaveBound(ctx, a.CookieID, deviceID, accessToken, expireAt, credentialCookieFingerprint(cookieStr)); err != nil {
		a.logger.Warn("缓存 accessToken 失败", "err", err)
	}
}

// clearTokenCache 清除账号 token 缓存（session 失效 / 短连接可疑 / cookie 被外部更新时调用）。
func (a *Account) clearTokenCache(ctx context.Context) {
	if a.store == nil || a.store.Tokens == nil {
		return
	}
	if err := a.store.Tokens.Clear(ctx, a.CookieID); err != nil {
		a.logger.Warn("清除 token 缓存失败", "err", err)
	}
}

// reloadCookieFromDB 复读 DB cookie：与内存不同则采纳，并清 token 缓存（新 cookie 对应
// 新 session，旧 token 作废）。让运行中账号吸收外部更新（人工重新扫码 / refreshAccountProfile）。
func (a *Account) reloadCookieFromDB(ctx context.Context) bool {
	if a.store == nil || a.store.Cookies == nil {
		return false
	}
	d, err := a.store.Cookies.GetDetails(ctx, a.CookieID)
	if err != nil || d == nil || d.Value == "" {
		return false
	}
	a.mu.Lock()
	cur := a.CookieStr
	a.mu.Unlock()
	if credentialCookieFingerprint(d.Value) == credentialCookieFingerprint(cur) {
		return false
	}
	a.logger.Info("检测到 DB cookie 已更新，重新加载", "account", a.CookieID)
	a.replaceCookieStr(d.Value)
	a.clearCurrentToken()
	a.clearTokenCache(ctx)
	a.mu.Lock()
	a.lastCaptchaFailure = time.Time{}
	a.mu.Unlock()
	return true
}

func (a *Account) cookieSnapshotMatchesDB(ctx context.Context, cookieStr string) bool {
	if a.store == nil || a.store.Cookies == nil {
		return true
	}
	current, err := a.store.Cookies.GetValue(ctx, a.CookieID)
	if err != nil || strings.TrimSpace(current) == "" {
		return true
	}
	return credentialCookieFingerprint(current) == credentialCookieFingerprint(cookieStr)
}

// RuntimeStatus 返回账号当前连接状态的线程安全快照。
func (a *Account) RuntimeStatus() RuntimeStatus {
	a.mu.Lock()
	defer a.mu.Unlock()
	return RuntimeStatus{
		State:     a.runtimeState,
		Message:   a.runtimeMessage,
		Connected: a.conn != nil && a.runtimeState == RuntimeOnline,
		Failures:  a.connFailures,
		UpdatedAt: a.runtimeUpdatedAt,
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
	prev := a.runtimeState
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
	return conn.SendText(sendCtx, myID, chatID, toUserID, text)
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
		return nil, "", fmt.Errorf("账号 %s 当前没有可用 WebSocket 连接", a.CookieID)
	}
	myID := strings.TrimSpace(a.UserID)
	if myID == "" {
		myID = protocol.TransCookies(a.CookieStr)["unb"]
	}
	if myID == "" {
		return nil, "", fmt.Errorf("账号 %s 缺少 unb，无法发送消息", a.CookieID)
	}
	return a.conn, myID, nil
}

func (a *Account) replaceCookieStr(cookieStr string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.CookieStr = cookieStr
	if unb := protocol.TransCookies(cookieStr)["unb"]; unb != "" {
		a.UserID = unb
	}
}

// UpdateCookie 用外部刷新得到的新 cookie 更新运行时状态。
func (a *Account) UpdateCookie(cookieStr string) {
	if strings.TrimSpace(cookieStr) == "" {
		return
	}
	a.refreshMu.Lock()
	defer a.refreshMu.Unlock()
	credentialUnlock := func() {}
	if a.store != nil {
		credentialUnlock = a.store.LockAccountCredentials(a.CookieID)
	}
	defer credentialUnlock()
	a.mu.Lock()
	changed := cookieStr != a.CookieStr
	a.mu.Unlock()
	if !changed {
		return
	}
	if a.store != nil && a.store.Cookies != nil {
		if err := a.store.Cookies.UpdateValueExisting(context.Background(), a.CookieID, cookieStr); err != nil {
			a.logger.Warn("同步外部 Cookie 到数据库失败", "err", err)
		}
	}
	a.replaceCookieStr(cookieStr)
	// Updating credentials does not close a healthy connection. Clearing the
	// in-memory and persisted token makes the next reconnect authenticate from
	// this Cookie snapshot instead of reusing a token issued for the old one.
	a.clearCurrentToken()
	a.clearTokenCache(context.Background())
}
