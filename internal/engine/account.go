// Package engine 实现单账号运行时：WebSocket 连接生命周期、token 刷新、
// 消息分发主循环（信号量限并发 + 防抖 + 去重）、重连策略。
//
// 业务逻辑（自动发货、回复）在 Phase 3 通过 Handler 接口注入，
// Phase 2 先搭好骨架并跑通"收消息→解密→去重→防抖→回调"。
package engine

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/protocol"
	"xianyu-go/internal/xianyu/ws"
)

// 账号运行时参数。
const (
	MaxConnectionFailures = 5               // 连续失败上限，触发密码登录刷新
	MessageSemaphoreSize  = 100             // 并发消息处理上限
	MessageDebounceDelay  = 1 * time.Second // 防抖延迟：用户停止发送 1s 后回复
	MessageExpireTime     = time.Hour       // 去重有效期
	ProcessedIDsMaxSize   = 10000           // 去重表上限，超限清理
	HeartbeatInterval     = 15 * time.Second
	// token 心跳间隔。主动刷新 token 让服务端持续续期 cookie（_m_h5_tk /
	// sgcookie 等），避免 cookie 自然滑向过期。15 分钟一次，平衡保活与风控。
	// 不宜过短，否则易触发阿里系滑块风控。
	TokenRefreshInterval  = 15 * time.Minute
	TokenRefreshMinGap    = time.Minute
	MessageCooldown       = 5 * time.Minute // 收到消息后 5 分钟内不刷 cookie
	PasswordLoginMinGap   = 30 * time.Minute

	// TokenCacheTTL 缓存的 accessToken 视为有效的时长。tokenRefreshLoop 每 15min
	// 刷新一次会续期该缓存；短重启 / 瞬时 mtop 失败时回退到此缓存不掉线。
	TokenCacheTTL = 90 * time.Minute
	// ShortConnectionThreshold 连接维持不足此时长视为短连接，疑似 token 失效，
	// 清除 token 缓存下轮重新派生，避免反复用失效缓存 token。
	ShortConnectionThreshold = 30 * time.Second
	// AuthExpiredRetryInterval 登录凭证失效且无法自动恢复时的慢重试间隔，
	// 保持 goroutine 存活，等用户重新扫码（DB cookie 更新后 reloadCookieFromDB 自愈）。
	AuthExpiredRetryInterval = 5 * time.Minute
)

// 告警级别（OnAccountAlert 的 level 参数）。
const (
	AlertLevelInfo     = "info"
	AlertLevelWarn     = "warn"
	AlertLevelCritical = "critical"
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
)

// Account 单账号运行时。
type Account struct {
	CookieID  string
	CookieStr string
	UserID    string // unb（myid）

	store    *db.Store
	mtop     mtop.Client
	wsDialer wsDialer
	handler  Handler
	logger   *slog.Logger

	// 运行时状态（受 mu 保护）
	mu                sync.Mutex
	currentToken      string
	deviceID          string
	connFailures      int
	lastMsgReceived   time.Time
	lastTokenRefresh  time.Time
	lastPasswordLogin time.Time
	runtimeState      string
	runtimeMessage    string
	runtimeUpdatedAt  time.Time
	stopFn            context.CancelFunc
	stopped           bool
	conn              WSConn
	connStartedAt     time.Time // 本次 WS 连接建立时间，用于短连接检测
	authExpiredAlerted bool     // 已发过 auth_expired 告警，连接恢复后复位（避免刷屏）

	// 去重
	dedupMu   sync.Mutex
	processed map[string]time.Time

	// 防抖：chat_id → 防抖句柄
	debounceMu     sync.Mutex
	debounceTimers map[string]*debounceEntry

	// 消息处理信号量
	sem chan struct{}

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
	SetAccessToken(token string)
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

// Config 构造 Account 所需依赖。
type Config struct {
	CookieID  string
	CookieStr string
	Store     *db.Store
	Handler   Handler
	Logger    *slog.Logger
	// MTop 可选：注入 mtop 客户端以便测试 mock。 nil 时使用默认 HTTP 实现。
	MTop mtop.Client
}

// New 构造单账号运行时（未启动）。
func New(cfg Config) *Account {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	mtopClient := cfg.MTop
	if mtopClient == nil {
		mtopClient = mtop.NewClient()
	}
	cookies := protocol.TransCookies(cfg.CookieStr)
	myid := cookies["unb"]
	a := &Account{
		CookieID:         cfg.CookieID,
		CookieStr:        cfg.CookieStr,
		UserID:           myid,
		store:            cfg.Store,
		mtop:             mtopClient,
		wsDialer:         defaultDialer{},
		handler:          cfg.Handler,
		logger:           logger.With("account", cfg.CookieID),
		deviceID:         protocol.GenerateDeviceID(myid),
		processed:        make(map[string]time.Time),
		debounceTimers:   make(map[string]*debounceEntry),
		sem:              make(chan struct{}, MessageSemaphoreSize),
		runtimeState:     RuntimeStarting,
		runtimeMessage:   "正在启动账号服务",
		runtimeUpdatedAt: time.Now(),
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
	a.mu.Lock()
	a.stopFn = cancel
	a.mu.Unlock()

	// 复用 DB 缓存的 device_id（跨进程重启稳定），避免每次重启换新设备 ID 触发风控。
	a.ensureDeviceIDCached(ctx)

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// 账号是否被禁用。
		if !a.store.Cookies.GetStatus(ctx, a.CookieID) {
			a.logger.Info("账号已禁用，停止主循环")
			return nil
		}

		// 0) 复读 DB cookie：吸收外部更新（人工重新扫码 / refreshAccountProfile），
		// 让运行中账号无需 Manager.Restart 即可自愈。
		a.reloadCookieFromDB(ctx)

		// 1) 取 token。缓存命中且未过期则跳过 mtop（降低风控 + 瞬时失败不掉线），
		// 否则刷新；刷新成功落库缓存。session 过期直接跳到慢重试兜底。
		token, cookieStr, err := a.acquireToken(ctx)
		if err != nil {
			a.logger.Error("获取 token 失败", "err", err)
			a.mu.Lock()
			a.connFailures++
			failures := a.connFailures
			a.mu.Unlock()
			a.setRuntimeError(ctx, err)
			if mtop.IsSessionExpiredErr(err) {
				a.logger.Warn("session 已失效，清除 token 缓存并进入慢重试", "err", err)
				a.clearTokenCache(ctx)
				a.alert(ctx, AlertLevelWarn, "账号 session 已失效",
					"登录凭证已被闲鱼服务端判定为失效，系统正在尝试通过浏览器密码登录自动恢复。若长时间未恢复，请人工处理。")
				if err := a.handleMaxFailures(ctx); err != nil {
					return err
				}
				continue
			}
			if failures >= MaxConnectionFailures {
				if err := a.handleMaxFailures(ctx); err != nil {
					return err
				}
				continue
			}
			if sleepCtx(ctx, a.retryDelay("token_refresh")) != nil {
				return ctx.Err()
			}
			continue
		}
		a.mu.Lock()
		a.currentToken = token
		a.CookieStr = cookieStr
		a.connFailures = 0
		a.mu.Unlock()
		a.logger.Info("token 刷新成功")
		a.setRuntimeState(RuntimeConnecting, "登录凭证有效，正在连接消息服务")

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
		conn, err := a.wsDialer.Dial(ctx, ws.Config{
			CookieStr:   cookieStr,
			DeviceID:    deviceID,
			AccessToken: token,
			Recorder:    recorder,
		}, a.logger)
		if err != nil {
			a.logger.Error("WS 连接失败", "err", err)
			a.mu.Lock()
			a.connFailures++
			failures := a.connFailures
			a.mu.Unlock()
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
		a.authExpiredAlerted = false // 连接成功，复位 auth_expired 告警标记
		a.mu.Unlock()
		a.setRuntimeState(RuntimeOnline, "消息服务连接正常")

		// 3) 心跳 + 消息接收，并并行运行 token 定时刷新。
		hbCtx, hbCancel := context.WithCancel(ctx)
		var hbErr error
		hbDone := make(chan struct{})
		go func() {
			hbErr = conn.HeartbeatLoop(hbCtx, HeartbeatInterval)
			hbCancel()
			close(hbDone)
		}()

		refreshDone := make(chan struct{})
		go func() {
			a.tokenRefreshLoop(hbCtx, conn)
			close(refreshDone)
		}()

		recvErr := conn.ReceiveLoop(ctx, a.dispatch)
		hbCancel()
		<-refreshDone
		<-hbDone // 确保 hbErr 写入完成后再读取（消除数据竞争）。
		_ = conn.Close()

		// 连接结束：清 token，统计失败，决定重连或密码刷新。
		a.mu.Lock()
		startedAt := a.connStartedAt
		a.currentToken = ""
		a.conn = nil
		a.connFailures++
		failures := a.connFailures
		a.mu.Unlock()
		// 短连接：疑似 token 失效，清缓存下轮重新派生，避免反复用失效缓存 token。
		a.clearCacheIfShortConnection(ctx, startedAt)
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
	defer a.mu.Unlock()
	if a.stopped {
		return
	}
	a.stopped = true
	a.runtimeState = RuntimeStopped
	a.runtimeMessage = "账号服务已停止"
	a.runtimeUpdatedAt = time.Now()
	if a.stopFn != nil {
		a.stopFn()
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
}

// handleMaxFailures 连续失败达上限：尝试密码登录刷新；失败则返回 err 触发上层重启实例。
func (a *Account) handleMaxFailures(ctx context.Context) error {
	a.logger.Warn("连续失败达上限，触发密码登录刷新", "failures", MaxConnectionFailures)
	a.mu.Lock()
	lastMsg := a.lastMsgReceived
	lastLogin := a.lastPasswordLogin
	a.mu.Unlock()
	if !lastMsg.IsZero() && time.Since(lastMsg) < MessageCooldown {
		a.logger.Warn("最近仍收到消息，跳过密码登录刷新", "cooldown", MessageCooldown)
		a.resetFailures()
		return sleepCtx(ctx, a.retryDelay("recent_message_skip_password_login"))
	}
	if !lastLogin.IsZero() && time.Since(lastLogin) < PasswordLoginMinGap {
		remain := PasswordLoginMinGap - time.Since(lastLogin)
		a.logger.Warn("密码登录刷新冷却中，跳过本次刷新", "remain", remain.Round(time.Second))
		a.resetFailures()
		return sleepCtx(ctx, minDuration(remain, time.Minute))
	}

	a.mu.Lock()
	a.lastPasswordLogin = time.Now()
	a.mu.Unlock()
	if a.handler != nil && a.handler.OnPasswordLoginRefresh(ctx, a.CookieID) {
		if d, err := a.store.Cookies.GetDetails(ctx, a.CookieID); err == nil && d != nil && d.Value != "" {
			a.replaceCookieStr(d.Value)
		}
		a.logger.Info("密码登录刷新成功，重置失败计数")
		a.setRuntimeState(RuntimeConnecting, "登录凭证已刷新，正在重新连接")
		a.resetFailures()
		return sleepCtx(ctx, 2*time.Second)
	}
	a.resetFailures()
	a.clearTokenCache(ctx)
	// 不再硬退出：进入慢重试，保持 goroutine 存活。用户重新扫码（DB cookie 更新）
	// 后，Run 顶部的 reloadCookieFromDB 会吸收新 cookie 并自愈回在线。
	a.setRuntimeState(RuntimeAuthExpired, "登录凭证已失效，自动慢重试中，重新扫码后自愈")
	if a.markAuthExpired() {
		a.alert(ctx, AlertLevelCritical, "账号自动恢复失败，需人工处理",
			fmt.Sprintf("账号 %s 连续失败 %d 次，登录凭证暂未自动恢复。账号将每 %v 慢重试一次，不会停止运行；前往后台重新扫码后无需手动重启即可自动恢复在线。", a.CookieID, MaxConnectionFailures, AuthExpiredRetryInterval))
	}
	if err := sleepCtx(ctx, AuthExpiredRetryInterval); err != nil {
		return err
	}
	return nil
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

// alert 触发账号告警通知。handler 未注入或为 nil 时静默跳过。
func (a *Account) alert(ctx context.Context, level, title, body string) {
	if a.handler == nil {
		return
	}
	a.handler.OnAccountAlert(ctx, a.CookieID, level, title, body)
}

func (a *Account) resetFailures() {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.connFailures = 0
}

// retryDelay 移植自 _calculate_retry_delay。
func (a *Account) retryDelay(errMsg string) time.Duration {
	a.mu.Lock()
	f := a.connFailures
	a.mu.Unlock()
	if f < 1 {
		f = 1
	}
	secs := 0
	switch {
	case contains(errMsg, "no close frame received or sent"):
		secs = min(3*f, 15)
	case contains(errMsg, "connection refused") || contains(errMsg, "timeout"):
		secs = min(10*f, 60)
	default:
		secs = min(5*f, 30)
	}
	return time.Duration(secs) * time.Second
}

// refreshToken 调 mtop token API，返回 (accessToken, 更新后的 cookie)。
// 成功时把 token + device_id 缓存到 DB（saveTokenCache），供进程重启复用与
// 稳态重连时跳过 mtop（acquireToken 命中缓存即直接返回）。
func (a *Account) refreshToken(ctx context.Context) (string, string, error) {
	a.mu.Lock()
	cookieStr := a.CookieStr
	last := a.lastTokenRefresh
	if !last.IsZero() && time.Since(last) < TokenRefreshMinGap {
		remain := TokenRefreshMinGap - time.Since(last)
		a.mu.Unlock()
		a.logger.Info("等待 token 刷新冷却", "remain", remain.Round(time.Second))
		if err := sleepCtx(ctx, remain); err != nil {
			return "", "", err
		}
		a.mu.Lock()
		cookieStr = a.CookieStr
	}
	a.lastTokenRefresh = time.Now()
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
	res, err := a.mtop.RefreshTokenWithDeviceIDContext(ctx, cookieStr, deviceID)
	if res != nil && res.UpdatedCookies != "" && res.UpdatedCookies != cookieStr {
		a.replaceCookieStr(res.UpdatedCookies)
		cookieStr = res.UpdatedCookies
		if a.store != nil {
			if d, e := a.store.Cookies.GetDetails(ctx, a.CookieID); e == nil {
				if err := a.store.Cookies.Save(ctx, a.CookieID, res.UpdatedCookies, d.UserID); err != nil {
					a.logger.Error("token 刷新后保存 cookie 失败", "cookie_id", a.CookieID, "err", err)
				}
			}
		}
	}
	if err != nil {
		return "", "", err
	}
	if res == nil {
		return "", "", fmt.Errorf("token API 未返回结果")
	}
	a.saveTokenCache(ctx, deviceID, res.AccessToken)
	return res.AccessToken, cookieStr, nil
}

// acquireToken 取用于 WS /reg 的 accessToken：缓存命中且未过期则跳过 mtop（降低风控
// 与瞬时失败影响），否则调 refreshToken 重新派生并落库缓存。
//
// 设计：稳态重连时复用未过期缓存 token，避免每次重连都打 mtop（降低风控触发）。
// 缓存 token 若已被服务端失效，dial 后会短连接断开 → clearTokenCache → 下轮重新派生，自愈。
func (a *Account) acquireToken(ctx context.Context) (string, string, error) {
	if token, ok := a.cachedTokenIfFresh(ctx); ok {
		a.mu.Lock()
		a.currentToken = token
		cookieStr := a.CookieStr
		a.mu.Unlock()
		a.logger.Info("使用缓存的 accessToken，跳过 mtop 刷新")
		return token, cookieStr, nil
	}
	return a.refreshToken(ctx)
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
	if tk.AccessToken == "" || tk.ExpireAt <= time.Now().Unix() {
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

// ensureDeviceIDCached 优先复用 DB 中的 device_id；无缓存则持久化当前生成的 device_id，
// 供后续进程重启复用。这是 device_id 跨重启稳定的关键。
func (a *Account) ensureDeviceIDCached(ctx context.Context) {
	if a.store == nil || a.store.Tokens == nil {
		return
	}
	if tk, err := a.store.Tokens.Get(ctx, a.CookieID); err == nil && tk.DeviceID != "" {
		a.mu.Lock()
		a.deviceID = tk.DeviceID
		a.mu.Unlock()
		return
	}
	a.mu.Lock()
	did := a.deviceID
	a.mu.Unlock()
	if did == "" {
		return
	}
	if err := a.store.Tokens.Save(ctx, a.CookieID, did, "", 0); err != nil {
		a.logger.Warn("持久化 device_id 失败", "err", err)
	}
}

// saveTokenCache 把 accessToken + device_id 缓存到 DB（expire_at = now + TokenCacheTTL）。
func (a *Account) saveTokenCache(ctx context.Context, deviceID, accessToken string) {
	if a.store == nil || a.store.Tokens == nil || accessToken == "" {
		return
	}
	if err := a.store.Tokens.Save(ctx, a.CookieID, deviceID, accessToken, time.Now().Add(TokenCacheTTL).Unix()); err != nil {
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
func (a *Account) reloadCookieFromDB(ctx context.Context) {
	if a.store == nil || a.store.Cookies == nil {
		return
	}
	d, err := a.store.Cookies.GetDetails(ctx, a.CookieID)
	if err != nil || d == nil || d.Value == "" {
		return
	}
	a.mu.Lock()
	cur := a.CookieStr
	a.mu.Unlock()
	if d.Value == cur {
		return
	}
	a.logger.Info("检测到 DB cookie 已更新，重新加载", "account", a.CookieID)
	a.replaceCookieStr(d.Value)
	a.clearTokenCache(ctx)
}

// clearCacheIfShortConnection 连接维持不足 ShortConnectionThreshold 视为短连接，
// 疑似 token 失效，清除 token 缓存让下轮重新派生。
func (a *Account) clearCacheIfShortConnection(ctx context.Context, startedAt time.Time) {
	if startedAt.IsZero() || time.Since(startedAt) >= ShortConnectionThreshold {
		return
	}
	a.logger.Warn("连接维持过短，疑似 token 失效，清除 token 缓存", "duration", time.Since(startedAt).Round(time.Second))
	a.clearTokenCache(ctx)
}

// tokenRefreshLoop 在 WS 在线期间按 TokenRefreshInterval 定时刷新 token。
// 刷新失败不重连（让心跳/接收循环去判定），仅记录日志；session 过期会触发
// 接收循环断开，主循环再走 handleMaxFailures 兜底。
func (a *Account) tokenRefreshLoop(ctx context.Context, conn WSConn) {
	ticker := time.NewTicker(TokenRefreshInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
		if ctx.Err() != nil {
			return
		}
		token, _, err := a.refreshToken(ctx)
		if err != nil {
			a.logger.Warn("定时刷新 token 失败", "err", err)
			if mtop.IsSessionExpiredErr(err) {
				// session 失效：清缓存，让接收循环尽快断开走兜底，主动关闭连接。
				a.clearTokenCache(ctx)
				_ = conn.Close()
				return
			}
			continue
		}
		a.mu.Lock()
		a.currentToken = token
		a.mu.Unlock()
		conn.SetAccessToken(token)
		a.logger.Info("定时刷新 token 成功")
	}
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
			a.alert(ctx, AlertLevelWarn, "闲鱼要求安全验证",
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
		if a.UserID != "" && a.UserID != unb {
			a.deviceID = protocol.GenerateDeviceID(unb)
		}
		a.UserID = unb
		if a.deviceID == "" {
			a.deviceID = protocol.GenerateDeviceID(unb)
		}
	}
}

// UpdateCookie 用外部刷新得到的新 cookie 更新运行时状态。
func (a *Account) UpdateCookie(cookieStr string) {
	if strings.TrimSpace(cookieStr) == "" {
		return
	}
	a.replaceCookieStr(cookieStr)
}

func minDuration(a, b time.Duration) time.Duration {
	if a < b {
		return a
	}
	return b
}
