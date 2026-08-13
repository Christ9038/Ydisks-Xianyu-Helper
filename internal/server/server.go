// Package server 实现 HTTP API 服务（chi 路由）。
// 复用 internal/auth 中间件、internal/db.Store、internal/account.Manager。
// 端点按分组组织在同一 package 的多个 handler 文件中。
package server

import (
	"bytes"
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"xianyu-go/internal/account"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/notify"
	appversion "xianyu-go/internal/version"
	"xianyu-go/internal/webui"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/qrlogin"
	xrenew "xianyu-go/internal/xianyu/renew"
)

type qrLoginService interface {
	GenerateQRCode(ctx context.Context) (sessionID string, qrCodeURL string, err error)
	GetSessionStatus(sessionID string) map[string]any
	CompleteVerification(ctx context.Context, sessionID string) (cookies string, unb string, err error)
}

type qrLoginPersistence struct {
	AccountID string
	IsNew     bool
	UserID    int64
	CreatedAt time.Time
}

type qrLoginOwner struct {
	UserID    int64
	CreatedAt time.Time
}

type publishBatchWorker struct {
	token  string
	cancel context.CancelFunc
}

// Server 聚合 HTTP 服务依赖。Automation 与 Notifier 由构造函数注入，
// 不再允许外部直接改字段，避免运行时被替换成 nil。
type Server struct {
	Store       *db.Store
	Auth        *auth.Service
	Manager     *account.Manager
	automation  *automation.Center
	notifier    *notify.Notifier
	chat        *chat.Service
	MTop        mtop.Client
	CookieRenew xrenew.Service
	QRLogin     qrLoginService
	Logger      *slog.Logger
	WebDir      string // 前端静态资源目录（含 index.html）
	Addr        string

	publishMu      sync.Mutex
	publishCancels map[string]publishBatchWorker
	workerMu       sync.Mutex
	workerCount    int
	workersDone    chan struct{}
	recoveryWG     sync.WaitGroup
	lifecycleMu    sync.RWMutex
	lifecycleCtx   context.Context

	qrMu        sync.Mutex
	qrPersisted map[string]qrLoginPersistence
	qrOwners    map[string]qrLoginOwner
	// qrPersistLocks 按扫码会话串行化持久化，避免持有全局 qrMu 执行数据库、
	// 资料刷新和账号重启等慢操作。
	qrPersistLocks   sync.Map
	loginLimiter     *loginFailureLimiter
	initializationMu sync.Mutex
}

// SetChatService installs the shared chat persistence and live event hub.
func (s *Server) SetChatService(service *chat.Service) { s.chat = service }

// New 构造。autoCenter/notifier 由调用方完成创建后注入（创建顺序：
// adapter → manager → automation → notifier → server）。
func New(store *db.Store, manager *account.Manager, secure bool, webDir, addr string, logger *slog.Logger, autoCenter *automation.Center, notifier *notify.Notifier) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	qrMgr := qrlogin.NewManager(logger)
	return &Server{
		Store:       store,
		Auth:        &auth.Service{Store: store, Logger: logger, Secure: secure},
		Manager:     manager,
		automation:  autoCenter,
		notifier:    notifier,
		MTop:        mtop.NewClient(),
		CookieRenew: xrenew.Service{},
		QRLogin:     qrMgr,
		Logger:      logger,
		WebDir:      webDir,
		Addr:        addr,

		publishCancels: make(map[string]publishBatchWorker),
		workersDone:    closedSignal(),
		lifecycleCtx:   context.Background(),
		qrPersisted:    make(map[string]qrLoginPersistence),
		qrOwners:       make(map[string]qrLoginOwner),
		loginLimiter:   newLoginFailureLimiter(),
	}
}

// mtopClient 返回注入的 mtop 客户端；未注入时退回默认 HTTP 实现（保证零值可用）。
func (s *Server) mtopClient() mtop.Client {
	if s.MTop != nil {
		return s.MTop
	}
	return mtop.NewClient()
}

// recoverExpiredMTOPSession 是 HTTP/API 入口的统一 Session 失效出口。
// 各调用点必须先保存响应 Cookie 并释放账号凭证锁，再进入续期，避免锁反转。
func (s *Server) recoverExpiredMTOPSession(ctx context.Context, cookieID string, err error) bool {
	if !mtop.IsSessionExpiredErr(err) {
		return false
	}
	if s.Logger != nil {
		s.Logger.Warn("MTOP API 检测到 Session 过期，停止业务请求并开始即时续期", "account", cookieID, "err", err)
	}
	if s.Manager == nil {
		return false
	}
	return s.Manager.RecoverExpiredCredential(ctx, cookieID)
}

// Router 构建完整路由树。
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	// 请求日志（精简）。
	r.Use(s.requestLogger)

	// 健康检查（无需认证）。
	r.Get("/health", s.health)

	// 认证组（无需登录的端点，但解析会话以判断登录态）。
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware) // 解析 session，不强制登录
		r.Post("/login", s.login)
		r.Post("/initialize", s.initialize)
		r.Get("/verify", s.verify)
		r.Post("/logout", s.logout)
	})
	r.Post("/change-admin-password", s.authMiddleware(http.HandlerFunc(s.changeAdminPassword)).ServeHTTP)
	r.Post("/change-password", s.authMiddleware(http.HandlerFunc(s.changePassword)).ServeHTTP)

	// 认证后的 API。
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Put("/account/credentials", s.updateCredentials)

		// 账号 cookie
		s.mountCookies(r)
		s.mountAccountTasks(r)
		// 在线聊天（历史 REST + 应用层 WebSocket）
		s.mountChat(r)
		// 扫码登录
		s.mountQRLoginReal(r)
		// 密码登录
		s.mountPasswordLogin(r)
		// 订单
		s.mountOrdersReal(r)
		// 订单分析（仪表盘）
		s.mountAnalyticsReal(r)
		// 卡密 + 发货规则
		s.mountCardsReal(r)
		// 自动化规则
		s.mountAutomation(r)
		// 商品
		s.mountItemsReal(r)
		// 关键字 + 指定商品回复
		s.mountKeywordsReal(r)
		s.mountItemRepliesReal(r)
		// 默认回复
		s.mountDefaultRepliesReal(r)
		// 通知
		s.mountNotificationsReal(r)
		// 系统设置（已认证）
		s.mountSettingsReal(r)
		// AI 设置
		s.mountAIReplyReal(r)
		// 用户
		s.mountUserReal(r)

		// 管理员专用。
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAdmin)
			s.mountAdminReal(r)
		})
	})

	// 公开系统设置（无需登录，前端登录页读取主题等）。
	s.mountPublicSettings(r)

	// SPA 静态资源 catch-all（最后挂载）。
	s.mountSPA(r)
	return r
}

// mountPublicSettings 公开系统设置（无需登录）。
func (s *Server) mountPublicSettings(r chi.Router) {
	r.Get("/system-settings/public", s.publicSettings)
}

// authMiddleware 仅对单个 handler 应用会话解析 + RequireAuth。
func (s *Server) authMiddleware(h http.Handler) http.Handler {
	return s.Auth.Middleware(auth.RequireAuth(h))
}

// requestLogger 记录请求完成状态、耗时和 chi request_id。
func (s *Server) requestLogger(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		level := slog.LevelDebug
		if status >= 500 {
			level = slog.LevelError
		} else if status >= 400 {
			level = slog.LevelWarn
		}
		s.Logger.LogAttrs(r.Context(), level, "HTTP 请求完成",
			slog.String("method", r.Method),
			slog.String("path", r.URL.Path),
			slog.Int("status", status),
			slog.Int("bytes", ww.BytesWritten()),
			slog.Duration("duration", time.Since(start)),
			slog.String("request_id", middleware.GetReqID(r.Context())),
			slog.String("remote", r.RemoteAddr),
		)
	})
}

// mountSPA 挂载前端静态资源与 SPA catch-all。
//
// 前端 vite base 为 /static/，构建后 index.html 引用 /static/assets/...、/static/favicon.svg。
// 故静态资源统一从 /static/ 前缀提供；非 API 的 GET 请求（/、/login 等客户端路由）
// 返回 /static/index.html，交给 React Router 接管。
func (s *Server) mountSPA(r chi.Router) {
	if s.WebDir != "" {
		s.mountDirSPA(r)
		return
	}
	embedded, err := webui.Static()
	if err != nil {
		return
	}
	s.mountFSSPA(r, embedded)
}

func (s *Server) mountDirSPA(r chi.Router) {
	indexFile := filepath.Join(s.WebDir, "index.html")
	// /static/* 直接作为静态文件服务（assets/、favicon.svg 等）。
	// StripPrefix("/static/") 后，URL /static/assets/x.js → WebDir/assets/x.js。
	staticFiles := http.StripPrefix("/static/", http.FileServer(http.Dir(s.WebDir)))
	r.Handle("/static/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/static/" || r.URL.Path == "/static/index.html" {
			setNoStore(w)
		}
		staticFiles.ServeHTTP(w, r)
	}))

	// SPA catch-all：非 API 的 GET 请求返回 index.html。
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			writeJSON(w, http.StatusNotFound, errDetail("接口不存在"))
			return
		}
		if _, err := os.Stat(indexFile); err != nil {
			writeJSON(w, http.StatusNotFound, errDetail("前端未构建"))
			return
		}
		setNoStore(w)
		http.ServeFile(w, r, indexFile)
	})
}

func (s *Server) mountFSSPA(r chi.Router, staticFS fs.FS) {
	staticFiles := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
	r.Handle("/static/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/static/" || r.URL.Path == "/static/index.html" {
			setNoStore(w)
		}
		staticFiles.ServeHTTP(w, r)
	}))

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			writeJSON(w, http.StatusNotFound, errDetail("接口不存在"))
			return
		}
		index, err := fs.ReadFile(staticFS, "index.html")
		if err != nil {
			writeJSON(w, http.StatusNotFound, errDetail("前端未构建"))
			return
		}
		setNoStore(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}

func setNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// isAPIPath 判断是否为 API 路径（不应被 SPA 拦截）。
// 仅保留实际挂载的路由前缀，与 Router() 中的 mount* 一一对应。
func isAPIPath(path string) bool {
	apiPrefixes := []string{
		"/api/", "/admin/", "/health", "/login", "/initialize", "/logout", "/verify",
		"/change-password", "/change-admin-password", "/account/",
		"/cookies", "/cookie/", "/orders", "/analytics",
		"/cards", "/automation-rules", "/items", "/keywords", "/default-replies", "/default-reply",
		"/notification-channels", "/message-notifications",
		"/system-settings", "/ai-reply", "/ai-models",
		"/user-settings",
		"/item-reply", "/itemReplays",
		"/qr-login", "/password-login",
		"/static/", // 静态资源（由 /static/* handler 处理，不进 catch-all）
	}
	for _, p := range apiPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// health 健康检查。
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.Store == nil || s.Store.DB == nil || s.Store.DB.PingContext(ctx) != nil {
		writeJSON(w, http.StatusServiceUnavailable, map[string]any{"status": "degraded", "database": "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":     "ok",
		"database":   "ok",
		"version":    appversion.Version,
		"commit":     appversion.ShortCommit(),
		"build_time": appversion.BuildTime,
	})
}

// 各分组 mount*Real 方法在 handlers 文件中实现；为避免单文件过大，按业务域分文件。

// Run 启动 HTTP 服务（阻塞）。
func (s *Server) Run(ctx context.Context) error {
	s.lifecycleMu.Lock()
	s.lifecycleCtx = ctx
	s.lifecycleMu.Unlock()
	srv := &http.Server{
		Addr:              s.Addr,
		Handler:           s.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		// 批量发布允许上传约 200 MiB；请求头仍由 10 秒限制防慢连接，正文给足上传时间。
		ReadTimeout:  10 * time.Minute,
		WriteTimeout: 2 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}
	// #nosec G118 -- 关闭协程由传入的服务生命周期上下文控制。
	go func() {
		<-ctx.Done()
		shCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if err := srv.Shutdown(shCtx); err != nil {
			s.Logger.Warn("HTTP 服务关闭异常", "err", err)
		}
	}()
	s.Logger.Info("HTTP 服务启动", "addr", s.Addr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

// WaitForBackground 等待恢复扫描器先退出，再等待其已经登记的批量 worker。
func (s *Server) WaitForBackground() {
	if s == nil {
		return
	}
	s.recoveryWG.Wait()
	s.waitForWorkers(10 * time.Second)
}

func closedSignal() chan struct{} {
	done := make(chan struct{})
	close(done)
	return done
}

func (s *Server) lifecycleContext() context.Context {
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	return s.lifecycleCtx
}

func (s *Server) beginWorker() func() {
	s.workerMu.Lock()
	if s.workerCount == 0 {
		s.workersDone = make(chan struct{})
	}
	s.workerCount++
	s.workerMu.Unlock()
	return func() {
		s.workerMu.Lock()
		s.workerCount--
		if s.workerCount == 0 {
			close(s.workersDone)
		}
		s.workerMu.Unlock()
	}
}

func (s *Server) waitForWorkers(timeout time.Duration) {
	s.workerMu.Lock()
	done := s.workersDone
	s.workerMu.Unlock()
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-done:
	case <-timer.C:
		s.Logger.Warn("等待后台 worker 退出超时")
	}
}
