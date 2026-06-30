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
	TokenRefreshInterval  = 1 * time.Hour
	TokenRefreshMinGap    = time.Minute
	MessageCooldown       = 5 * time.Minute // 收到消息后 5 分钟内不刷 cookie
	PasswordLoginMinGap   = 30 * time.Minute
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
	mtop     *mtop.Client
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
	conn              *ws.Conn

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

// wsDialer 抽象 ws.Dial，便于测试替身。
type wsDialer interface {
	Dial(ctx context.Context, cfg ws.Config, logger *slog.Logger) (*ws.Conn, error)
}

type defaultDialer struct{}

func (defaultDialer) Dial(ctx context.Context, cfg ws.Config, logger *slog.Logger) (*ws.Conn, error) {
	return ws.Dial(ctx, cfg, logger)
}

// Config 构造 Account 所需依赖。
type Config struct {
	CookieID  string
	CookieStr string
	Store     *db.Store
	Handler   Handler
	Logger    *slog.Logger
}

// New 构造单账号运行时（未启动）。
func New(cfg Config) *Account {
	logger := cfg.Logger
	if logger == nil {
		logger = slog.Default()
	}
	cookies := protocol.TransCookies(cfg.CookieStr)
	myid := cookies["unb"]
	a := &Account{
		CookieID:         cfg.CookieID,
		CookieStr:        cfg.CookieStr,
		UserID:           myid,
		store:            cfg.Store,
		mtop:             &mtop.Client{},
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

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// 账号是否被禁用。
		if !a.store.Cookies.GetStatus(ctx, a.CookieID) {
			a.logger.Info("账号已禁用，停止主循环")
			return nil
		}

		// 1) 刷新 token。
		token, cookieStr, err := a.refreshToken(ctx)
		if err != nil {
			a.logger.Error("刷新 token 失败", "err", err)
			a.mu.Lock()
			a.connFailures++
			failures := a.connFailures
			a.mu.Unlock()
			a.setRuntimeError(err)
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
		a.setRuntimeState(RuntimeConnecting, "登录凭证有效，正在连接消息服务")

		// 2) 连接 WS + 注册。
		var recorder func(direction, rawText, parsedJSON, parseStatus, errMsg string)
		if a.store != nil && a.store.WSMessages != nil {
			recorder = func(direction, rawText, parsedJSON, parseStatus, errMsg string) {
				_ = a.store.WSMessages.Add(context.Background(), db.WSMessage{
					CookieID:    a.CookieID,
					Direction:   direction,
					RawText:     rawText,
					ParsedJSON:  parsedJSON,
					MessageKind: "",
					ParseStatus: parseStatus,
					Error:       errMsg,
				})
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
		a.mu.Unlock()
		a.setRuntimeState(RuntimeOnline, "消息服务连接正常")

		// 3) 心跳 + 消息接收。
		hbCtx, hbCancel := context.WithCancel(ctx)
		var hbErr error
		go func() {
			hbErr = conn.HeartbeatLoop(hbCtx, HeartbeatInterval)
			hbCancel()
		}()

		recvErr := conn.ReceiveLoop(ctx, a.dispatch)
		hbCancel()
		_ = conn.Close()

		// 连接结束：清 token，统计失败，决定重连或密码刷新。
		a.mu.Lock()
		a.currentToken = ""
		a.conn = nil
		a.connFailures++
		failures := a.connFailures
		a.mu.Unlock()
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
	a.setRuntimeState(RuntimeAuthExpired, "登录凭证已失效，请重新扫码登录")
	return fmt.Errorf("账号 %s 连续失败 %d 次且密码登录刷新失败，需重启实例", a.CookieID, MaxConnectionFailures)
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
				_ = a.store.Cookies.Save(ctx, a.CookieID, res.UpdatedCookies, d.UserID)
			}
		}
	}
	if err != nil {
		return "", "", err
	}
	if res == nil {
		return "", "", fmt.Errorf("token API 未返回结果")
	}
	// 若 cookie 被刷新，持久化回数据库。
	if res.UpdatedCookies != cookieStr {
		a.mu.Lock()
		a.CookieStr = res.UpdatedCookies
		cookieStr = res.UpdatedCookies
		a.mu.Unlock()
		if a.store != nil {
			// 保留原 user_id：用 GetDetails 取后回写。
			if d, e := a.store.Cookies.GetDetails(ctx, a.CookieID); e == nil {
				_ = a.store.Cookies.Save(ctx, a.CookieID, res.UpdatedCookies, d.UserID)
			}
		}
	}
	return res.AccessToken, cookieStr, nil
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

func (a *Account) setRuntimeError(err error) {
	msg := strings.ToLower(errString(err))
	switch {
	case strings.Contains(msg, "验证"), strings.Contains(msg, "captcha"), strings.Contains(msg, "risk"), strings.Contains(msg, "rgv587"), strings.Contains(msg, "fail_sys_user_validate"):
		a.setRuntimeState(RuntimeVerificationRequired, "闲鱼要求安全验证，请重新扫码并完成验证")
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

func (a *Account) currentSenderState() (*ws.Conn, string, error) {
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
