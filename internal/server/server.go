// Package server 实现 HTTP API 服务（chi 路由）。
// 复用 internal/auth 中间件、internal/db.Store、internal/account.Manager。
// 端点按分组组织在同一 package 的多个 handler 文件中。
package server

import (
	"bytes"
	"context"
	"fmt"
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
	"xianyu-go/internal/logging"
	"xianyu-go/internal/notify"
	"xianyu-go/internal/reconciliation"
	appversion "xianyu-go/internal/version"
	"xianyu-go/internal/webui"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/qrlogin"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// qrLoginService 保存qr登录Service，供当前处理流程使用
type qrLoginService interface {
	GenerateQRCode(ctx context.Context) (sessionID string, qrCodeURL string, err error)
	GetSessionStatus(sessionID string) map[string]any
	CompleteVerification(ctx context.Context, sessionID string) (cookies string, unb string, err error)
}

// qrLoginPersistence 保存qr登录Persistence，供当前处理流程使用
type qrLoginPersistence struct {
	AccountID string
	IsNew     bool
	UserID    int64
	CreatedAt time.Time
}

// qrLoginOwner 保存qr登录所有者，供当前处理流程使用
type qrLoginOwner struct {
	UserID    int64
	CreatedAt time.Time
}

// publishBatchWorker 保存发布批次工作器，供当前处理流程使用
type publishBatchWorker struct {
	token  string
	cancel context.CancelFunc
}

// ServerOption 是 Server 构造阶段应用可选依赖的配置函数。
type ServerOption func(*Server)

// Server 聚合 HTTP 服务依赖。Automation 与 Notifier 由构造函数注入，
// 不再允许外部直接改字段，避免运行时被替换成 nil。
// Server 保存Server，供当前处理流程使用
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
	// applications 保存统一装配的应用服务实例。
	applications *applicationServices
	// reconciliation 负责恢复外部发货成功但本地订单状态未完成的记录。
	reconciliation *reconciliation.Service
	// transactionRepository 提供统一事务执行所需的最小持久化能力。
	transactionRepository transactionRepository
	// itemSpecCacheMu 保护商品多规格探测缓存。
	itemSpecCacheMu sync.Mutex
	// itemSpecCache 保存短期商品多规格探测结果，减少重复远端请求。
	itemSpecCache map[string]itemSpecCacheEntry

	publishMu           sync.Mutex
	publishCancels      map[string]publishBatchWorker
	orderRefreshMu      sync.Mutex
	orderRefreshCancels map[string]orderRefreshWorker
	workerMu            sync.Mutex
	workerCount         int
	workersDone         chan struct{}
	backgroundWG        sync.WaitGroup
	// taskRegistryMu 保护后台任务注册表的惰性初始化，避免测试构造的零值 Server 产生数据竞争。
	taskRegistryMu sync.Mutex
	// taskRegistry 保存 Server 自有后台任务的生命周期状态，不持久化业务数据或敏感凭证。
	taskRegistry    *taskRegistry
	lifecycleMu     sync.RWMutex
	lifecycleCtx    context.Context
	lifecycleCancel context.CancelFunc
	httpServer      *http.Server
	httpDone        chan struct{}
	httpErr         error
	started         bool
	stopped         bool

	qrMu        sync.Mutex
	qrPersisted map[string]qrLoginPersistence
	qrOwners    map[string]qrLoginOwner
	// qrPersistLocks 按扫码会话串行化持久化，避免持有全局 qrMu 执行数据库、
	// 资料刷新和账号重启等慢操作。
	qrPersistLocks   sync.Map
	loginLimiter     *loginFailureLimiter
	initializationMu sync.Mutex
}

// WithChatService 注入聊天持久化与实时事件中心。
func WithChatService(service *chat.Service) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中待注入聊天服务的 HTTP 服务实例。
		server.chat = service
	}
}

// New 构造并校验 HTTP 服务所需依赖。autoCenter/notifier 由调用方完成创建后注入
// （创建顺序：adapter → manager → automation → notifier → server）。
// New 负责New相关处理。
func New(store *db.Store, manager *account.Manager, secure bool, webDir, addr string, logger *slog.Logger, autoCenter *automation.Center, notifier *notify.Notifier, options ...ServerOption) (*Server, error) {
	if store == nil {
		return nil, fmt.Errorf("server 依赖 db.Store 不能为空")
	}
	if manager == nil {
		return nil, fmt.Errorf("server 依赖 account.Manager 不能为空")
	}
	if logger == nil {
		logger = logging.NewLogger(os.Stdout, "text")
	}
	// qrMgr 是二维码登录流程使用的默认会话管理器。
	qrMgr := qrlogin.NewManager(logger)
	// server 是完成依赖装配、等待应用服务初始化后的 HTTP 服务实例。
	server := &Server{
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

		publishCancels:      make(map[string]publishBatchWorker),
		orderRefreshCancels: make(map[string]orderRefreshWorker),
		workersDone:         closedSignal(),
		lifecycleCtx:        context.Background(),
		qrPersisted:         make(map[string]qrLoginPersistence),
		qrOwners:            make(map[string]qrLoginOwner),
		loginLimiter:        newLoginFailureLimiter(),
		itemSpecCache:       make(map[string]itemSpecCacheEntry),
		taskRegistry:        newTaskRegistry(),
	}
	// option 表示当前遍历过程中的option
	for _, option := range options {
		// option 是当前构造调用提供的可选依赖配置。
		if option != nil {
			option(server)
		}
	}
	server.applications = newApplicationServices(server)
	server.transactionRepository = newStoreTransactionRepository(store)
	server.reconciliation = reconciliation.New(store, logger)
	return server, nil
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
// recoverExpiredMTOPSession 负责recoverExpiredMTOP会话相关处理。
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
	// r 保存r，供当前处理流程使用
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	// 请求日志（精简）。
	r.Use(s.requestLogger)

	// 健康检查（无需认证）。
	s.mountHealthAndVersionedRoutes(r)

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
		// start 保存开始，供当前处理流程使用
		start := time.Now()
		// ww 保存ww，供当前处理流程使用
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		// status 保存状态，供当前处理流程使用
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		// level 保存level，供当前处理流程使用
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
// mountSPA 负责mountSPA相关处理。
func (s *Server) mountSPA(r chi.Router) {
	if s.WebDir != "" {
		s.mountDirSPA(r)
		return
	}
	// embedded、err 保存embedded、err，供当前处理流程使用
	embedded, err := webui.Static()
	if err != nil {
		return
	}
	s.mountFSSPA(r, embedded)
}

// mountDirSPA 负责mountDirSPA相关处理。
func (s *Server) mountDirSPA(r chi.Router) {
	// indexFile 保存index文件，供当前处理流程使用
	indexFile := filepath.Join(s.WebDir, "index.html")
	// /static/* 直接作为静态文件服务（assets/、favicon.svg 等）。
	// StripPrefix("/static/") 后，URL /static/assets/x.js → WebDir/assets/x.js。
	// staticFiles 保存static文件列表，供当前处理流程使用
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
			writeErrRequest(w, r, http.StatusNotFound, "接口不存在")
			return
		}
		if // err 保存err，供当前处理流程使用
		_, err := os.Stat(indexFile); err != nil {
			writeErrRequest(w, r, http.StatusNotFound, "前端未构建")
			return
		}
		setNoStore(w)
		http.ServeFile(w, r, indexFile)
	})
}

// mountFSSPA 负责mountFSSPA相关处理。
func (s *Server) mountFSSPA(r chi.Router, staticFS fs.FS) {
	// staticFiles 保存static文件列表，供当前处理流程使用
	staticFiles := http.StripPrefix("/static/", http.FileServer(http.FS(staticFS)))
	r.Handle("/static/*", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/static/" || r.URL.Path == "/static/index.html" {
			setNoStore(w)
		}
		staticFiles.ServeHTTP(w, r)
	}))

	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			writeErrRequest(w, r, http.StatusNotFound, "接口不存在")
			return
		}
		// index、err 保存index、err，供当前处理流程使用
		index, err := fs.ReadFile(staticFS, "index.html")
		if err != nil {
			writeErrRequest(w, r, http.StatusNotFound, "前端未构建")
			return
		}
		setNoStore(w)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		http.ServeContent(w, r, "index.html", time.Time{}, bytes.NewReader(index))
	})
}

// setNoStore 负责setNoStore相关处理。
func setNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// isAPIPath 判断是否为 API 路径（不应被 SPA 拦截）。
// 仅保留实际挂载的路由前缀，与 Router() 中的 mount* 一一对应。
// isAPIPath 负责isAPI路径相关处理。
func isAPIPath(path string) bool {
	// apiPrefixes 保存apiPrefixes，供当前处理流程使用
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
	// p 表示当前遍历过程中的p
	for _, p := range apiPrefixes {
		if strings.HasPrefix(path, p) {
			return true
		}
	}
	return false
}

// health 健康检查。
func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.Store == nil || s.Store.DB == nil || s.Store.DB.PingContext(ctx) != nil {
		writeJSON(w, http.StatusServiceUnavailable, healthResponse{Status: "degraded", Database: "unavailable"})
		return
	}
	writeJSON(w, http.StatusOK, healthResponse{
		Status:    "ok",
		Database:  "ok",
		Version:   appversion.Version,
		Commit:    appversion.ShortCommit(),
		BuildTime: appversion.BuildTime,
	})
}

// 各分组 mount*Real 方法在 handlers 文件中实现；为避免单文件过大，按业务域分文件。

// Start 启动 HTTP 服务及其生命周期监听。重复调用不会重复监听端口。
func (s *Server) Start(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("server 不能为空")
	}
	// ctx 是控制 HTTP 服务及其后台任务的进程级生命周期上下文。
	if ctx == nil {
		ctx = context.Background()
	}
	s.lifecycleMu.Lock()
	if s.started {
		s.lifecycleMu.Unlock()
		return nil
	}
	// httpServer 是本次启动创建的标准库 HTTP 监听器。
	httpServer := &http.Server{
		Addr:              s.Addr,
		Handler:           s.Router(),
		ReadHeaderTimeout: 10 * time.Second,
		// 批量发布允许上传约 200 MiB；请求头仍由 10 秒限制防慢连接，正文给足上传时间。
		ReadTimeout:  10 * time.Minute,
		WriteTimeout: 2 * time.Minute,
		IdleTimeout:  2 * time.Minute,
	}
	// lifecycleCtx 是 Server 内部可取消的生命周期上下文。
	// lifecycleCancel 是触发 Server 生命周期收束的取消函数。
	// lifecycleCtx、lifecycleCancel 保存lifecycleCtx、lifecycle取消，供当前处理流程使用
	lifecycleCtx, lifecycleCancel := context.WithCancel(ctx)
	s.lifecycleCtx = lifecycleCtx
	s.lifecycleCancel = lifecycleCancel
	s.httpServer = httpServer
	s.httpDone = make(chan struct{})
	s.httpErr = nil
	s.started = true
	s.stopped = false
	// httpDone 是 HTTP 监听 goroutine 结束时关闭的完成信号。
	httpDone := s.httpDone
	s.lifecycleMu.Unlock()

	// 生命周期上下文取消时触发统一 Stop；该 goroutine 不登记到后台任务组，
	// 避免 Stop 等待自身造成死锁。
	go func() {
		<-lifecycleCtx.Done()
		// stopCtx 是自动关闭 HTTP 服务时使用的有限时长上下文。
		// cancel 释放 stopCtx 的定时器资源。
		// stopCtx、cancel 保存stopCtx、cancel，供当前处理流程使用
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if // err 保存err，供当前处理流程使用
		err := s.Stop(stopCtx); err != nil && s.Logger != nil {
			s.Logger.Warn("HTTP 服务关闭异常", "err", err)
		}
	}()
	go func() {
		s.Logger.Info("HTTP 服务启动", "addr", s.Addr)
		// err 是 HTTP 监听器退出时返回的原始错误。
		err := httpServer.ListenAndServe()
		if err == http.ErrServerClosed {
			err = nil
		}
		s.lifecycleMu.Lock()
		s.httpErr = err
		s.lifecycleMu.Unlock()
		close(httpDone)
	}()
	return nil
}

// Wait 等待 HTTP 服务结束，并返回监听或关闭错误。
func (s *Server) Wait() error {
	if s == nil {
		return nil
	}
	s.lifecycleMu.RLock()
	// httpDone 是 HTTP 监听 goroutine 的完成信号。
	httpDone := s.httpDone
	s.lifecycleMu.RUnlock()
	if httpDone == nil {
		return fmt.Errorf("server 尚未启动")
	}
	<-httpDone
	s.lifecycleMu.RLock()
	// err 是监听 goroutine 记录的退出错误。
	err := s.httpErr
	s.lifecycleMu.RUnlock()
	return err
}

// Stop 幂等关闭 HTTP 服务，并等待 Server 自有后台任务和批量 worker 退出。
func (s *Server) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	// ctx 是限制 HTTP 优雅关闭等待时间的上下文。
	if ctx == nil {
		ctx = context.Background()
	}
	s.lifecycleMu.Lock()
	if !s.started {
		s.lifecycleMu.Unlock()
		return nil
	}
	if s.stopped {
		// httpDone 是已进入停止流程的 HTTP 监听完成信号。
		httpDone := s.httpDone
		s.lifecycleMu.Unlock()
		if httpDone != nil && !waitForSignal(ctx, httpDone) {
			return ctx.Err()
		}
		if !waitForWaitGroup(ctx, &s.backgroundWG) || !s.waitForWorkersContext(ctx) {
			return ctx.Err()
		}
		return nil
	}
	s.stopped = true
	// httpServer 是需要执行优雅关闭的标准库 HTTP 服务。
	// httpDone 是监听 goroutine 退出的完成信号。
	// lifecycleCancel 是取消 Server 内部生命周期上下文的函数。
	// httpServer 保存httpServer，供当前处理流程使用
	httpServer := s.httpServer
	// httpDone 保存httpDone，供当前处理流程使用
	httpDone := s.httpDone
	// lifecycleCancel 保存lifecycle取消，供当前处理流程使用
	lifecycleCancel := s.lifecycleCancel
	s.lifecycleMu.Unlock()

	if lifecycleCancel != nil {
		lifecycleCancel()
	}
	// shutdownErr 是 HTTP 优雅关闭返回的错误；后台等待错误由 worker 自身记录。
	var shutdownErr error
	if httpServer != nil {
		shutdownErr = httpServer.Shutdown(ctx)
	}
	if httpDone != nil && !waitForSignal(ctx, httpDone) {
		return ctx.Err()
	}
	if !waitForWaitGroup(ctx, &s.backgroundWG) || !s.waitForWorkersContext(ctx) {
		return ctx.Err()
	}
	return shutdownErr
}

// Run 启动并阻塞等待 HTTP 服务结束，兼容旧的进程入口调用方式。
func (s *Server) Run(ctx context.Context) error {
	// err 是显式启动阶段返回的构造或监听准备错误。
	if err := s.Start(ctx); err != nil {
		return err
	}
	return s.Wait()
}

// WaitForBackground 等待恢复扫描器先退出，再等待其已经登记的批量 worker。
func (s *Server) WaitForBackground() {
	if s == nil {
		return
	}
	s.backgroundWG.Wait()
	s.waitForWorkers(10 * time.Second)
}

// closedSignal 负责closedSignal相关处理。
func closedSignal() chan struct{} {
	// done 保存done，供当前处理流程使用
	done := make(chan struct{})
	close(done)
	return done
}

// lifecycleContext 负责lifecycle上下文相关处理。
func (s *Server) lifecycleContext() context.Context {
	s.lifecycleMu.RLock()
	defer s.lifecycleMu.RUnlock()
	return s.lifecycleCtx
}

// startBackgroundTask 登记并启动一个受 Server 生命周期管理的后台任务。
// 调用方负责在任务函数内部响应上下文取消；WaitForBackground 会等待任务退出。
// startBackgroundTask 负责开始Background任务相关处理。
func (s *Server) startBackgroundTask(name string, task func()) string {
	return s.startBackgroundTaskContext(name, s.lifecycleContext(), task)
}

// startBackgroundTaskContext 登记并启动带显式 Context 的 Server 后台任务。
// 返回值是可供管理端查询的任务 ID；任务完成、取消或超时后会保留有限历史。
func (s *Server) startBackgroundTaskContext(name string, ctx context.Context, task func()) string {
	// taskID、complete 记录任务状态并提供一次性收束回调。
	taskID, complete := s.taskRegistryForServer().start(name, ctx)
	s.backgroundWG.Add(1)
	// #nosec G118 -- 任务由调用方提供的 Server 生命周期控制。
	go func() {
		defer s.backgroundWG.Done()
		defer complete(nil)
		if task == nil {
			if s.Logger != nil {
				s.Logger.Warn("跳过空后台任务", "task", name)
			}
			return
		}
		task()
	}()
	return taskID
}

// StartOrderReconciliationRecovery 启动受 Server 生命周期管理的订单补偿扫描器。
func (s *Server) StartOrderReconciliationRecovery(ctx context.Context) string {
	if s == nil || s.reconciliation == nil {
		return ""
	}
	return s.startBackgroundTaskContext("订单状态补偿扫描器", ctx, func() {
		s.reconciliation.Run(ctx)
	})
}

// taskRegistryForServer 返回 Server 的后台任务注册表；零值 Server 也会安全惰性初始化。
func (s *Server) taskRegistryForServer() *taskRegistry {
	if s == nil {
		return newTaskRegistry()
	}
	s.taskRegistryMu.Lock()
	defer s.taskRegistryMu.Unlock()
	if s.taskRegistry == nil {
		s.taskRegistry = newTaskRegistry()
	}
	return s.taskRegistry
}

// beginWorker 负责begin工作器相关处理。
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

// waitForWorkers 负责waitForWorkers相关处理。
func (s *Server) waitForWorkers(timeout time.Duration) {
	if timeout <= 0 {
		_ = s.waitForWorkersContext(context.Background())
		return
	}
	// ctx、cancel 分别表示有限等待上下文和释放定时器的函数。
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = s.waitForWorkersContext(ctx)
}

// waitForWorkersContext 等待批量 worker，并将等待限制在调用方的关闭上下文内。
func (s *Server) waitForWorkersContext(ctx context.Context) bool {
	if ctx == nil {
		ctx = context.Background()
	}
	s.workerMu.Lock()
	// done 保存done，供当前处理流程使用
	done := s.workersDone
	s.workerMu.Unlock()
	if done == nil {
		// 零值 Server 尚未登记批量 worker，可直接视为已完成。
		return true
	}
	select {
	case <-done:
		return true
	case <-ctx.Done():
		s.Logger.Warn("等待后台 worker 退出超时")
		return false
	}
}

// waitForSignal 在关闭上下文取消或目标信号到达时返回，避免无界阻塞。
func waitForSignal(ctx context.Context, signal <-chan struct{}) bool {
	select {
	case <-signal:
		return true
	case <-ctx.Done():
		return false
	}
}

// waitForWaitGroup 在关闭上下文取消或 WaitGroup 清空时返回，避免无界阻塞。
func waitForWaitGroup(ctx context.Context, group *sync.WaitGroup) bool {
	// done 是 WaitGroup 清空后的完成信号。
	done := make(chan struct{})
	go func() {
		group.Wait()
		close(done)
	}()
	return waitForSignal(ctx, done)
}
