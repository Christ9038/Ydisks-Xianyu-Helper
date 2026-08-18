// Package server 实现 HTTP API 服务（chi 路由）。
// 复用 internal/auth 中间件、adapter 装配依赖和 internal/account.Manager。
// 端点按分组组织在同一 package 的多个 handler 文件中。
package server

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/logging"
	appversion "xianyu-go/internal/version"
	"xianyu-go/internal/webui"
)

// Dependencies 是 HTTP Server 的不可变组合依赖；应用服务和生命周期组件必须在进入 Server 前完成装配。
type Dependencies struct {
	// Auth 是负责会话解析和认证中间件的应用认证服务。
	Auth *auth.Service
	// WebDir 是嵌入或外置前端静态资源目录。
	WebDir string
	// Addr 是 HTTP 监听地址，默认由 cmd/server 提供 :59188。
	Addr string
	// Logger 是 Server 使用的结构化日志器。
	Logger *slog.Logger
	// DatabaseHealth 是数据库健康检查应用 Port。
	DatabaseHealth DatabaseHealthPort
	// Applications 是完整的 transport 应用 Port 快照；缺失时构造必须失败。
	Applications *ApplicationPorts
}

// DatabaseHealthPort 定义健康检查需要的最小数据库连通性能力。
type DatabaseHealthPort interface {
	// Ping 在调用方 Context 内探测数据库连接是否可用。
	Ping(context.Context) error
}

// Server 聚合 HTTP transport 依赖，不持有账号运行时或业务 worker 实现。
type Server struct {
	// Auth 是 HTTP 会话认证中间件依赖。
	Auth   *auth.Service
	Logger *slog.Logger
	WebDir string // 前端静态资源目录（含 index.html）
	Addr   string
	// applications 保存构造期注入的 transport 应用 Port 快照。
	applications *ApplicationPorts
	// databaseHealth 提供健康检查所需的数据库探测能力，避免 handler 直接触碰 SQL 连接。
	databaseHealth DatabaseHealthPort
	// backgroundMu 保护 Server 后台任务计数与完成信号，避免关闭等待创建不可取消的等待 goroutine。
	backgroundMu    sync.Mutex
	backgroundCount int
	backgroundDone  chan struct{}
	// taskRegistryMu 保护后台任务注册表的惰性初始化，避免测试构造的零值 Server 产生数据竞争。
	taskRegistryMu sync.Mutex
	// taskRegistry 保存 Server 自有后台任务的生命周期状态，不持久化业务数据或敏感凭证。
	taskRegistry *taskRegistry
	lifecycleMu  sync.RWMutex
	httpServer   *http.Server
	// listener 保存 Bind 成功后由 Server 独占的 TCP 监听器；只有 Stop 或 Serve 退出可以关闭它。
	listener net.Listener
	httpDone chan struct{}
	httpErr  error
	started  bool
	stopped  bool

	loginLimiter     *loginFailureLimiter
	initializationMu sync.Mutex
}

// New 构造纯 HTTP transport Server；所有应用服务、平台客户端和生命周期组件必须已由组合根创建。
func New(dependencies Dependencies) (*Server, error) {
	if dependencies.Auth == nil {
		return nil, fmt.Errorf("server 依赖认证服务不能为空")
	}
	if dependencies.DatabaseHealth == nil {
		return nil, fmt.Errorf("server 基础设施应用端口不能为空")
	}
	if dependencies.Applications == nil {
		return nil, fmt.Errorf("server 应用服务集合不能为空")
	}
	// applicationPortsErr 表示组合根提供的 Port 容器仍有未装配的 HTTP 路由能力。
	if applicationPortsErr := dependencies.Applications.validate(); applicationPortsErr != nil {
		return nil, fmt.Errorf("server 应用 Port 未完成装配: %w", applicationPortsErr)
	}
	// copiedApplications 冻结应用服务集合容器，调用方不得在 Server 启动后替换服务引用。
	copiedApplications := *dependencies.Applications
	// logger 是缺省时用于 transport 诊断的标准文本日志器。
	logger := dependencies.Logger
	if logger == nil {
		logger = logging.NewLogger(os.Stdout, "text")
	}
	return &Server{
		Auth:           dependencies.Auth,
		Logger:         logger,
		WebDir:         dependencies.WebDir,
		Addr:           dependencies.Addr,
		applications:   &copiedApplications,
		databaseHealth: dependencies.DatabaseHealth,
		loginLimiter:   newLoginFailureLimiter(),
		taskRegistry:   newTaskRegistry(),
		backgroundDone: closedSignal(),
	}, nil
}

// recoverExpiredSession 触发已注入的平台会话恢复回调，供 HTTP transport 处理应用层平台错误。
// 该方法不读取凭证、不调用 Manager，也不在凭证锁内执行外部 I/O。
func (s *Server) recoverExpiredSession(ctx context.Context, cookieID string, err error) bool {
	if s == nil || s.applications == nil || s.applications.sessionRecovery == nil {
		return false
	}
	return s.applications.sessionRecovery.Recover(ctx, cookieID, err)
}

// Router 构建完整路由树。
func (s *Server) Router() http.Handler {
	// r 用于本次流程后续判断的r
	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.Recoverer)
	// legacyAPITelemetry 为保留的历史 API 写入弃用元数据并提供结构化观测，不参与版本化入口或 SPA 路由。
	r.Use(s.legacyAPITelemetry)
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
		// start 用于本次流程后续判断的开始
		start := time.Now()
		// ww 用于本次流程后续判断的ww
		ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		next.ServeHTTP(ww, r)
		// status 用于本次流程后续判断的状态
		status := ww.Status()
		if status == 0 {
			status = http.StatusOK
		}

		// level 用于本次流程后续判断的level
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
			slog.Bool("legacy_api", isLegacyAPIRequest(r.Method, r.URL.Path)),
		)
	})
}

const (
	// legacyAPISunsetDate 是历史 API 的标准 HTTP-date 退场时间；实际删除仍须满足兼容矩阵中的观测窗口。
	legacyAPISunsetDate = "Thu, 31 Dec 2026 23:59:59 GMT"
	// legacyAPISuccessorLink 是带计划版本标识的版本化后继 API 链接。
	legacyAPISuccessorLink = "</api/v1>; rel=\"successor-version\"; title=\"v2.0\""
)

// legacyAPITelemetry 为历史 API 请求写入标准弃用响应头，供客户端、日志和发布观测统一识别。
// next 保持原有 handler、鉴权及错误映射；只有 isLegacyAPIPath 判定为真时才附加元数据。
func (s *Server) legacyAPITelemetry(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// legacyPath 表示当前请求是否命中仍受兼容矩阵保护的旧 API，而非 /api/v1 或公开页面。
		legacyPath := isLegacyAPIRequest(r.Method, r.URL.Path)
		if legacyPath {
			w.Header().Set("Deprecation", "true")
			w.Header().Set("Sunset", legacyAPISunsetDate)
			w.Header().Set("Link", legacyAPISuccessorLink)
		}
		next.ServeHTTP(w, r)
	})
}

// isLegacyAPIRequest 判断请求是否为需要遥测并保留到 Sunset 条件满足的历史 API。
// method 用于区分 SPA 的 GET /login 与同路径的历史认证 POST；/api/v1、健康检查、版本信息和静态页面不属于旧 API。
func isLegacyAPIRequest(method, path string) bool {
	if isLegacyAPIPath(path) {
		return true
	}
	// normalizedMethod 是标准化后的请求方法，避免空白输入意外匹配历史认证入口。
	normalizedMethod := strings.ToUpper(strings.TrimSpace(method))
	// normalizedPath 是标准化后的请求路径，用于与有限的历史认证入口精确比较。
	normalizedPath := strings.TrimSpace(path)
	switch normalizedPath {
	case "/login", "/logout", "/initialize", "/change-admin-password", "/change-password":
		return normalizedMethod == http.MethodPost
	case "/verify":
		return normalizedMethod == http.MethodGet
	default:
		return false
	}
}

// isLegacyAPIPath 判断不与 SPA 客户端路由重名的历史业务路径是否需要遥测。
// 调用方还必须通过 isLegacyAPIRequest 处理认证入口的 HTTP 方法语义。
func isLegacyAPIPath(path string) bool {
	// normalizedPath 保存去除首尾空白后的请求路径，路由路径本身不允许以空白构成兼容入口。
	normalizedPath := strings.TrimSpace(path)
	if strings.HasPrefix(normalizedPath, "/api/v1/") || normalizedPath == "/health" || normalizedPath == "/version" {
		return false
	}
	return strings.HasPrefix(normalizedPath, "/api/") ||
		strings.HasPrefix(normalizedPath, "/cookies") || strings.HasPrefix(normalizedPath, "/cookie/") ||
		strings.HasPrefix(normalizedPath, "/items") || strings.HasPrefix(normalizedPath, "/orders") ||
		strings.HasPrefix(normalizedPath, "/automation-") || strings.HasPrefix(normalizedPath, "/keywords") ||
		strings.HasPrefix(normalizedPath, "/default-repl") || strings.HasPrefix(normalizedPath, "/item-reply") ||
		strings.HasPrefix(normalizedPath, "/notification-") || strings.HasPrefix(normalizedPath, "/message-notifications") ||
		strings.HasPrefix(normalizedPath, "/system-settings") || strings.HasPrefix(normalizedPath, "/user-settings") ||
		strings.HasPrefix(normalizedPath, "/ai-") || strings.HasPrefix(normalizedPath, "/cards") ||
		strings.HasPrefix(normalizedPath, "/admin/") || strings.HasPrefix(normalizedPath, "/qr-login") ||
		strings.HasPrefix(normalizedPath, "/password-login") || strings.HasPrefix(normalizedPath, "/account/") ||
		strings.HasPrefix(normalizedPath, "/dashboard/") || strings.HasPrefix(normalizedPath, "/analytics/") ||
		strings.HasPrefix(normalizedPath, "/itemReplays")
}

// mountSPA 挂载前端静态资源与 SPA catch-all。
//
// 前端 vite base 为 /static/，构建后 index.html 引用 /static/assets/...、/static/favicon.svg。
// 故静态资源统一从 /static/ 前缀提供；非 API 的 GET 请求（/、/login 等客户端路由）
// 返回 /static/index.html，交给 React Router 接管。
// mountSPA 封装mountSPA业务协调。
func (s *Server) mountSPA(r chi.Router) {
	if s.WebDir != "" {
		s.mountDirSPA(r)
		return
	}
	// embedded、err 用于本次流程后续判断的embedded、err
	embedded, err := webui.Static()
	if err != nil {
		return
	}
	s.mountFSSPA(r, embedded)
}

// mountDirSPA 封装mountDirSPA业务协调。
func (s *Server) mountDirSPA(r chi.Router) {
	// indexFile 用于本次流程后续判断的index文件
	indexFile := filepath.Join(s.WebDir, "index.html")
	// /static/* 直接作为静态文件服务（assets/、favicon.svg 等）。
	// StripPrefix("/static/") 后，URL /static/assets/x.js → WebDir/assets/x.js。
	// staticFiles 用于本次流程后续判断的static文件列表
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
		if // err 用于本次流程后续判断的err
		_, err := os.Stat(indexFile); err != nil {
			writeErrRequest(w, r, http.StatusNotFound, "前端未构建")
			return
		}
		setNoStore(w)
		http.ServeFile(w, r, indexFile)
	})
}

// mountFSSPA 封装mountFSSPA业务协调。
func (s *Server) mountFSSPA(r chi.Router, staticFS fs.FS) {
	// staticFiles 用于本次流程后续判断的static文件列表
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
		// index、err 用于本次流程后续判断的index、err
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

// setNoStore 封装setNoStore业务协调。
func setNoStore(w http.ResponseWriter) {
	w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	w.Header().Set("Pragma", "no-cache")
	w.Header().Set("Expires", "0")
}

// isAPIPath 判断是否为 API 路径（不应被 SPA 拦截）。
// 仅保留实际挂载的路由前缀，与 Router() 中的 mount* 一一对应。
// isAPIPath 封装isAPI路径业务协调。
func isAPIPath(path string) bool {
	// apiPrefixes 用于本次流程后续判断的apiPrefixes
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
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if s.databaseHealth == nil || s.databaseHealth.Ping(ctx) != nil {
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

// Bind 同步占用 HTTP 监听地址，但不接受请求。
// 进程组合根必须先调用它，再启动可能产生外部副作用的应用 worker，避免端口冲突在 worker 启动后才暴露。
func (s *Server) Bind() error {
	if s == nil {
		return fmt.Errorf("server 不能为空")
	}
	s.lifecycleMu.Lock()
	if s.listener != nil {
		s.lifecycleMu.Unlock()
		return nil
	}
	if s.stopped {
		s.lifecycleMu.Unlock()
		return fmt.Errorf("HTTP 服务已经停止，不能重新绑定")
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
	// listener 是在启动应用 worker 前同步绑定的 TCP 端口；失败时 Server 保持未启动状态。
	listener, listenErr := net.Listen("tcp", s.Addr)
	if listenErr != nil {
		s.lifecycleMu.Unlock()
		return fmt.Errorf("监听 HTTP 地址 %q 失败: %w", s.Addr, listenErr)
	}
	s.httpServer = httpServer
	s.listener = listener
	s.httpDone = make(chan struct{})
	s.httpErr = nil
	s.stopped = false
	s.lifecycleMu.Unlock()
	return nil
}

// Start 启动已经绑定的 HTTP 服务及其生命周期监听。重复调用不会重复接受连接。
func (s *Server) Start(ctx context.Context) error {
	if s == nil {
		return fmt.Errorf("server 不能为空")
	}
	// ctx 是控制 HTTP 服务及其后台任务的进程级生命周期上下文。
	if ctx == nil {
		ctx = context.Background()
	}
	// bindErr 是同步占用监听地址失败时的启动前置错误。
	if bindErr := s.Bind(); bindErr != nil {
		return bindErr
	}
	s.lifecycleMu.Lock()
	if s.started {
		s.lifecycleMu.Unlock()
		return nil
	}
	if s.stopped || s.httpServer == nil || s.listener == nil || s.httpDone == nil {
		s.lifecycleMu.Unlock()
		return fmt.Errorf("HTTP 服务未处于可启动的已绑定状态")
	}
	// httpServer 是本次 Serve goroutine 独占的 HTTP transport。
	httpServer := s.httpServer
	// listener 是已成功绑定并交由本次 Serve 独占接收连接的监听器。
	listener := s.listener
	// httpDone 在本次 Serve 退出后关闭，供 Wait 和 Stop 观察最终退出。
	httpDone := s.httpDone
	s.started = true
	s.stopped = false
	s.lifecycleMu.Unlock()

	// 进程生命周期 Context 取消时触发 HTTP Stop；应用组件关闭由 cmd 统一协调。
	go func() {
		<-ctx.Done()
		// stopCtx 是自动关闭 HTTP 服务时使用的有限时长上下文。
		// cancel 释放 stopCtx 的定时器资源。
		// stopCtx、cancel 用于本次流程后续判断的stopCtx、cancel
		stopCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if // err 用于本次流程后续判断的err
		err := s.Stop(stopCtx); err != nil && s.Logger != nil {
			s.Logger.Warn("HTTP 服务关闭异常", "err", err)
		}
	}()
	go func() {
		s.Logger.Info("HTTP 服务启动", "addr", s.Addr)
		// err 是 HTTP 监听器退出时返回的原始错误。
		err := httpServer.Serve(listener)
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

// Stop 幂等关闭 HTTP 服务，并等待 Server 自有后台任务退出。
// 应用 worker 由进程生命周期协调器在独立 Context 中取消和 Join，避免 HTTP 超时耗尽其关闭预算。
func (s *Server) Stop(ctx context.Context) error {
	if s == nil {
		return nil
	}
	// ctx 是限制 HTTP 优雅关闭等待时间的上下文。
	if ctx == nil {
		ctx = context.Background()
	}
	s.lifecycleMu.Lock()
	if !s.started && s.listener == nil {
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
		if !s.waitForBackgroundContext(ctx) {
			return ctx.Err()
		}
		return nil
	}
	s.stopped = true
	// httpServer 是需要执行优雅关闭的标准库 HTTP 服务。
	httpServer := s.httpServer
	// listener 是已绑定但可能尚未进入 Serve 的监听器；绑定失败回滚由当前调用关闭它。
	listener := s.listener
	// httpDone 是监听 goroutine 退出或绑定回滚完成时关闭的完成信号。
	httpDone := s.httpDone
	// started 区分已进入 Serve 的服务和仅完成 Bind 的启动回滚路径。
	started := s.started
	s.lifecycleMu.Unlock()
	// shutdownErr 是 HTTP 优雅关闭返回的错误；后台等待错误由 worker 自身记录。
	var shutdownErr error
	if started && httpServer != nil {
		shutdownErr = httpServer.Shutdown(ctx)
	} else if listener != nil {
		shutdownErr = listener.Close()
		if shutdownErr != nil && !errors.Is(shutdownErr, net.ErrClosed) {
			return shutdownErr
		}
		s.lifecycleMu.Lock()
		if s.listener == listener {
			s.listener = nil
		}
		if httpDone != nil {
			close(httpDone)
		}
		s.lifecycleMu.Unlock()
	}
	if httpDone != nil && !waitForSignal(ctx, httpDone) {
		return ctx.Err()
	}
	if !s.waitForBackgroundContext(ctx) {
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

// WaitForBackground 等待 Server 自有的 HTTP 后台任务退出；应用 worker 由生命周期协调器负责。
func (s *Server) WaitForBackground() {
	if s == nil {
		return
	}
	_ = s.waitForBackgroundContext(context.Background())
}

// closedSignal 封装closedSignal业务协调。
func closedSignal() chan struct{} {
	// done 用于本次流程后续判断的done
	done := make(chan struct{})
	close(done)
	return done
}

// startBackgroundTaskContext 登记并启动带显式 Context 的 Server 后台任务。
// 返回值是可供管理端查询的任务 ID；任务完成、取消或超时后会保留有限历史。
func (s *Server) startBackgroundTaskContext(name string, ctx context.Context, task func()) string {
	return s.startBackgroundTaskResult(name, ctx, func() error {
		if task != nil {
			task()
		}
		return nil
	})
}

// startBackgroundTaskResult 登记并启动可返回错误的 Server 后台任务。
// 任务错误会进入任务注册表；调用方仍需自行处理敏感错误日志和取消语义。
func (s *Server) startBackgroundTaskResult(name string, ctx context.Context, task func() error) string {
	// taskID、complete 记录任务状态并提供一次性收束回调。
	taskID, complete := s.taskRegistryForServer().start(name, ctx)
	s.beginBackgroundTask()
	// #nosec G118 -- 任务由调用方提供的 Server 生命周期控制。
	go func() {
		defer s.finishBackgroundTask()
		// taskErr 保存后台任务函数返回的可观测错误。
		var taskErr error
		defer func() { complete(taskErr) }()
		if task == nil {
			if s.Logger != nil {
				s.Logger.Warn("跳过空后台任务", "task", name)
			}
			return
		}
		taskErr = task()
	}()
	return taskID
}

// beginBackgroundTask 登记一个由 Server 负责等待的后台任务，并刷新零到一任务转换信号。
func (s *Server) beginBackgroundTask() {
	if s == nil {
		return
	}
	s.backgroundMu.Lock()
	defer s.backgroundMu.Unlock()
	if s.backgroundCount == 0 {
		s.backgroundDone = make(chan struct{})
	}
	s.backgroundCount++
}

// finishBackgroundTask 标记一个后台任务退出，并在计数归零时关闭完成信号。
func (s *Server) finishBackgroundTask() {
	if s == nil {
		return
	}
	s.backgroundMu.Lock()
	defer s.backgroundMu.Unlock()
	if s.backgroundCount <= 0 {
		return
	}
	s.backgroundCount--
	if s.backgroundCount == 0 && s.backgroundDone != nil {
		close(s.backgroundDone)
	}
}

// beginWorker 为仍需由 Server 等待的通用后台任务提供兼容测试入口；业务 worker 生命周期由应用协调器拥有。
func (s *Server) beginWorker() func() {
	if s == nil {
		return func() {}
	}
	s.beginBackgroundTask()
	return func() {
		s.finishBackgroundTask()
	}
}

// waitForBackgroundContext 等待已登记后台任务退出；超时只结束当前等待，不创建游离等待 goroutine。
func (s *Server) waitForBackgroundContext(ctx context.Context) bool {
	if s == nil {
		return true
	}
	if ctx == nil {
		ctx = context.Background()
	}
	s.backgroundMu.Lock()
	// done 是当前后台任务批次归零时关闭的完成信号。
	done := s.backgroundDone
	if done == nil {
		done = closedSignal()
		s.backgroundDone = done
	}
	s.backgroundMu.Unlock()
	return waitForSignal(ctx, done)
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

// waitForSignal 在关闭上下文取消或目标信号到达时返回，避免无界阻塞。
func waitForSignal(ctx context.Context, signal <-chan struct{}) bool {
	select {
	case <-signal:
		return true
	case <-ctx.Done():
		return false
	}
}
