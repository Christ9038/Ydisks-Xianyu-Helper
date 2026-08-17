// Package server 实现 HTTP API 服务（chi 路由）。
// 复用 internal/auth 中间件、adapter 装配依赖和 internal/account.Manager。
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
	"xianyu-go/internal/adapter"
	accountapp "xianyu-go/internal/application/account"
	lifecycleapp "xianyu-go/internal/application/lifecycle"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/logging"
	"xianyu-go/internal/notify"
	appversion "xianyu-go/internal/version"
	"xianyu-go/internal/webui"
)

// qrLoginService 保存qr登录Service，供当前处理流程使用
type qrLoginService = adapter.QRLoginService

// qrLoginPersistence 保存qr登录Persistence，供当前处理流程使用
type qrLoginPersistence struct {
	AccountID string
	IsNew     bool
	UserID    int64
	CreatedAt time.Time
}

// ServerOption 是 Server 构造阶段应用可选依赖的配置函数。
type ServerOption func(*Server)

// orderDependencyFactory 是订单装配所需的最小能力集合；Server 不持有通用 Store 工厂。
type orderDependencyFactory interface {
	// NewOrderRepository 创建订单读写仓储。
	NewOrderRepository() *adapter.OrderRepository
	// NewOrderReconciliationRepository 创建订单补偿记录仓储。
	NewOrderReconciliationRepository() *adapter.OrderReconciliationRepository
	// NewOrderRuntime 创建订单平台运行时适配器。
	NewOrderRuntime(adapter.OrderRuntimeHooks, orderapp.ReconciliationRecorder, *slog.Logger) *adapter.OrderRuntime
	// NewOrderRefreshJobRepository 创建订单刷新任务仓储。
	NewOrderRefreshJobRepository() orderapp.RefreshJobRepository
}

// accountDependencyFactory 是账号应用装配所需的最小能力集合；Server 不持有通用 Store 工厂。
type accountDependencyFactory interface {
	// NewAccountLoginRepository 创建账号凭证与资料仓储。
	NewAccountLoginRepository() *adapter.AccountLoginRepository
	// NewAccountSettingsRepository 创建账号设置仓储。
	NewAccountSettingsRepository() *adapter.AccountSettingsRepository
	// NewAccountSummaryRepository 创建账号摘要仓储。
	NewAccountSummaryRepository() *adapter.AccountSummaryRepository
	// NewQRLoginRepository 创建扫码登录凭证端口。
	NewQRLoginRepository() accountapp.QRLoginRepository
	// NewAuthenticationRepository 创建用户认证仓储。
	NewAuthenticationRepository() *adapter.AuthenticationRepository
	// NewAccountLoginAuditRepository 创建账号登录审计仓储。
	NewAccountLoginAuditRepository() *adapter.AccountLoginAuditRepository
}

// itemDependencyFactory 是商品应用装配所需的最小能力集合；Server 不持有通用 Store 工厂。
type itemDependencyFactory interface {
	// NewItemBatchRepository 创建批量发布状态仓储。
	NewItemBatchRepository() *adapter.ItemBatchRepository
	// NewItemBatchPreviewPort 创建批量预检端口。
	NewItemBatchPreviewPort() *adapter.ItemBatchPreviewPort
	// NewItemBatchPublishPort 创建批量远端发布端口。
	NewItemBatchPublishPort(func() adapter.MTOPClient, *slog.Logger, func(context.Context, string, string), func(context.Context, string, error), adapter.ReadPublishImageFile, adapter.DownloadPublishImageURL) *adapter.ItemBatchPublishPort
	// NewItemPublishPort 创建单商品发布端口。
	NewItemPublishPort(func() adapter.MTOPClient, *slog.Logger, func(context.Context, string, string), func(context.Context, string, error)) *adapter.ItemPublishPort
	// NewItemPublishRepository 创建单商品发布结果仓储。
	NewItemPublishRepository() *adapter.ItemPublishRepository
	// NewItemCatalogRepository 创建商品目录仓储。
	NewItemCatalogRepository() *adapter.ItemCatalogRepository
	// NewItemSyncRepository 创建商品同步端口。
	NewItemSyncRepository(func() adapter.MTOPClient, *slog.Logger, func(context.Context, string, string), func(context.Context, string, error)) *adapter.ItemSyncRepository
}

// databaseHealth 定义健康检查需要的最小数据库连通性能力。
type databaseHealth interface {
	// Ping 在调用方 Context 内探测数据库连接是否可用。
	Ping(context.Context) error
}

// Server 聚合 HTTP 服务依赖。Automation 与 Notifier 由构造函数注入，
// 不再允许外部直接改字段，避免运行时被替换成 nil。
// Server 保存Server，供当前处理流程使用
type Server struct {
	// orderDependencies 保存订单应用服务专用的显式装配能力，不允许从通用设施容器回退获取。
	orderDependencies orderDependencyFactory
	// accountDependencies 保存账号应用服务专用的显式装配能力，不允许从通用设施容器回退获取。
	accountDependencies accountDependencyFactory
	// itemDependencies 保存商品应用服务专用的显式装配能力，不允许从通用设施容器回退获取。
	itemDependencies itemDependencyFactory
	// chatDependencies 保存聊天应用服务专用的显式装配能力，不允许从通用设施容器回退获取。
	chatDependencies *adapter.ChatDependencies
	// systemDependencies 保存健康检查与补偿扫描的显式装配能力。
	systemDependencies *adapter.SystemDependencies
	// automationDependencies 保存自动化、默认回复和关键词的显式装配能力。
	automationDependencies *adapter.AutomationDependencies
	// miscDependencies 保存通知、分析和卡券的显式装配能力。
	miscDependencies *adapter.MiscDependencies
	// adminSettingsDependencies 保存管理员与系统设置的显式装配能力。
	adminSettingsDependencies *adapter.AdminSettingsDependencies
	Auth                      *auth.Service
	Manager                   *account.Manager
	automation                *automation.Center
	notifier                  *notify.Notifier
	chat                      *chat.Service
	// platformDependencies 保存构造阶段校验通过的平台能力；生产调用必须通过下方访问器读取。
	platformDependencies *adapter.PlatformDependencies
	// MTop 是阶段性测试兼容别名；所有生产调用迁移完成后删除，删除前须清点字段写入测试并改用 WithPlatformDependencies。
	MTop adapter.MTOPClient
	// CookieRenew 是阶段性测试兼容别名；所有生产调用迁移完成后删除，删除前须清点字段写入测试并改用 WithPlatformDependencies。
	CookieRenew adapter.LongLoginClient
	// QRLogin 是阶段性测试兼容别名；所有生产调用迁移完成后删除，删除前须清点字段写入测试并改用 WithPlatformDependencies。
	QRLogin qrLoginService
	Logger  *slog.Logger
	WebDir  string // 前端静态资源目录（含 index.html）
	Addr    string
	// applications 保存统一装配的应用服务实例。
	applications *applicationServices
	// applicationLifecycle 保存由 cmd 拥有的应用生命周期协调器；Server 只读取其共享 Context。
	applicationLifecycle *lifecycleapp.Coordinator
	// databaseHealth 提供健康检查所需的数据库探测能力，避免 handler 直接触碰 SQL 连接。
	databaseHealth databaseHealth
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
	httpDone     chan struct{}
	httpErr      error
	started      bool
	stopped      bool

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

// WithOrderDependencies 注入订单应用服务所需的专用适配器工厂。
func WithOrderDependencies(dependencies *adapter.OrderDependencies) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中待注入订单专用依赖的 HTTP 服务实例。
		if dependencies == nil {
			server.orderDependencies = nil
			return
		}
		server.orderDependencies = dependencies
	}
}

// WithAccountDependencies 注入账号应用服务所需的专用适配器工厂。
func WithAccountDependencies(dependencies *adapter.AccountDependencies) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中待注入账号专用依赖的 HTTP 服务实例。
		if dependencies == nil {
			server.accountDependencies = nil
			return
		}
		server.accountDependencies = dependencies
	}
}

// WithItemDependencies 注入商品应用服务所需的专用适配器工厂。
func WithItemDependencies(dependencies *adapter.ItemDependencies) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中待注入商品专用依赖的 HTTP 服务实例。
		if dependencies == nil {
			server.itemDependencies = nil
			return
		}
		server.itemDependencies = dependencies
	}
}

// WithChatDependencies 注入聊天应用服务所需的专用适配器工厂。
func WithChatDependencies(dependencies *adapter.ChatDependencies) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中待注入聊天专用依赖的 HTTP 服务实例。
		server.chatDependencies = dependencies
	}
}

// WithSystemDependencies 注入健康检查与订单补偿扫描所需的专用适配器工厂。
func WithSystemDependencies(dependencies *adapter.SystemDependencies) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中待注入系统专用依赖的 HTTP 服务实例。
		server.systemDependencies = dependencies
	}
}

// WithAutomationDependencies 注入自动化领域所需的专用适配器工厂。
func WithAutomationDependencies(dependencies *adapter.AutomationDependencies) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中待注入自动化专用依赖的 HTTP 服务实例。
		server.automationDependencies = dependencies
	}
}

// WithMiscDependencies 注入通知、分析和卡券领域所需的专用适配器工厂。
func WithMiscDependencies(dependencies *adapter.MiscDependencies) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中待注入杂项领域依赖的 HTTP 服务实例。
		server.miscDependencies = dependencies
	}
}

// WithAdminSettingsDependencies 注入管理员与系统设置所需的专用适配器工厂。
func WithAdminSettingsDependencies(dependencies *adapter.AdminSettingsDependencies) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中待注入管理员设置依赖的 HTTP 服务实例。
		server.adminSettingsDependencies = dependencies
	}
}

// WithPlatformDependencies 注入 MTOP、长登录和二维码服务组成的平台能力边界。
func WithPlatformDependencies(dependencies *adapter.PlatformDependencies) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中待注入平台依赖的 HTTP 服务实例。
		server.platformDependencies = dependencies
	}
}

// WithApplicationLifecycle 注入由进程装配层拥有的应用生命周期协调器。
func WithApplicationLifecycle(coordinator *lifecycleapp.Coordinator) ServerOption {
	return func(server *Server) {
		// server 是当前构造流程中需要读取应用生命周期 Context 的 HTTP 服务实例。
		server.applicationLifecycle = coordinator
	}
}

// New 构造并校验 HTTP 服务所需依赖；订单、账号、商品、系统和平台能力必须由显式依赖边界注入。
// autoCenter/notifier 由调用方完成创建后注入，构造顺序为 adapter → manager → automation → notifier → server。
func New(authentication *auth.Service, manager *account.Manager, webDir, addr string, logger *slog.Logger, autoCenter *automation.Center, notifier *notify.Notifier, options ...ServerOption) (*Server, error) {
	if authentication == nil {
		return nil, fmt.Errorf("server 依赖认证服务不能为空")
	}
	if manager == nil {
		return nil, fmt.Errorf("server 依赖 account.Manager 不能为空")
	}
	if logger == nil {
		logger = logging.NewLogger(os.Stdout, "text")
	}
	// server 是完成依赖装配、等待应用服务初始化后的 HTTP 服务实例。
	server := &Server{
		Auth:           authentication,
		Manager:        manager,
		automation:     autoCenter,
		notifier:       notifier,
		Logger:         logger,
		WebDir:         webDir,
		Addr:           addr,
		loginLimiter:   newLoginFailureLimiter(),
		taskRegistry:   newTaskRegistry(),
		backgroundDone: closedSignal(),
	}
	// option 表示当前遍历过程中的option
	for _, option := range options {
		// option 是当前构造调用提供的可选依赖配置。
		if option != nil {
			option(server)
		}
	}
	if server.orderDependencies == nil {
		return nil, fmt.Errorf("server 订单专用依赖不能为空")
	}
	if server.accountDependencies == nil {
		return nil, fmt.Errorf("server 账号专用依赖不能为空")
	}
	if server.itemDependencies == nil {
		return nil, fmt.Errorf("server 商品专用依赖不能为空")
	}
	if server.chatDependencies == nil {
		return nil, fmt.Errorf("server 聊天专用依赖不能为空")
	}
	if server.systemDependencies == nil {
		return nil, fmt.Errorf("server 系统专用依赖不能为空")
	}
	if server.platformDependencies == nil {
		return nil, fmt.Errorf("server 平台依赖不能为空")
	}
	if server.automationDependencies == nil {
		return nil, fmt.Errorf("server 自动化依赖不能为空")
	}
	if server.miscDependencies == nil {
		return nil, fmt.Errorf("server 通知、分析和卡券依赖不能为空")
	}
	if server.adminSettingsDependencies == nil {
		return nil, fmt.Errorf("server 管理员与设置依赖不能为空")
	}
	server.applications = newApplicationServices(server)
	server.databaseHealth = server.systemDependencies.NewDatabaseHealth()
	return server, nil
}

// mtopClient 返回构造阶段校验的平台 MTOP 客户端；零值 Server 返回 nil，避免隐式创建依赖。
func (s *Server) mtopClient() adapter.MTOPClient {
	if s.MTop != nil {
		return s.MTop
	}
	if s.platformDependencies != nil && s.platformDependencies.MTOPClient() != nil {
		return s.platformDependencies.MTOPClient()
	}
	return nil
}

// longLoginClient 返回构造阶段注入的长登录客户端；旧公开字段仅用于兼容现有测试替身。
func (s *Server) longLoginClient() adapter.LongLoginClient {
	if s.CookieRenew != nil {
		return s.CookieRenew
	}
	if s.platformDependencies != nil && s.platformDependencies.LongLoginClient() != nil {
		return s.platformDependencies.LongLoginClient()
	}
	return nil
}

// qrLoginService 返回构造阶段注入的二维码服务；旧公开字段仅用于兼容现有测试替身。
func (s *Server) qrLoginService() qrLoginService {
	if s.QRLogin != nil {
		return s.QRLogin
	}
	if s.platformDependencies != nil && s.platformDependencies.QRLoginService() != nil {
		return s.platformDependencies.QRLoginService()
	}
	return nil
}

// sessionRecoveryCallback 返回平台会话失效的统一适配回调。
// 适配器负责错误分类和日志，账号运行时应用端口负责恢复；调用方不得跨凭证锁执行外部 I/O。
func (s *Server) sessionRecoveryCallback() adapter.SessionRecoveryHandler {
	if s == nil {
		return nil
	}
	return adapter.NewSessionRecoveryHandler(s.Logger, func(ctx context.Context, cookieID string) bool {
		// runtime 保存账号运行时应用服务；恢复由应用端口编排而非 HTTP 层直接调用 Manager。
		runtime := s.accountRuntimeApplication()
		return runtime != nil && runtime.RecoverExpiredCredential(ctx, cookieID)
	})
}

// recoverExpiredMTOPSession 保留旧测试入口并委托统一 Session 恢复适配回调。
// 新生产调用应直接注入 sessionRecoveryCallback，避免把 Server 方法传入基础设施适配器。
func (s *Server) recoverExpiredMTOPSession(ctx context.Context, cookieID string, err error) bool {
	// recovery 保存统一的错误分类、日志和运行时恢复回调。
	recovery := s.sessionRecoveryCallback()
	return recovery != nil && recovery(ctx, cookieID, err)
}

// recoverExpiredSession 触发已注入的平台会话恢复回调，供 HTTP transport 处理应用层平台错误。
// 该方法不读取凭证、不调用 Manager，也不在凭证锁内执行外部 I/O。
func (s *Server) recoverExpiredSession(ctx context.Context, cookieID string, err error) bool {
	// recovery 保存统一的错误分类、日志和运行时恢复回调。
	recovery := s.sessionRecoveryCallback()
	return recovery != nil && recovery(ctx, cookieID, err)
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
	s.httpServer = httpServer
	s.httpDone = make(chan struct{})
	s.httpErr = nil
	s.started = true
	s.stopped = false
	// httpDone 是 HTTP 监听 goroutine 结束时关闭的完成信号。
	httpDone := s.httpDone
	s.lifecycleMu.Unlock()

	// 进程生命周期 Context 取消时触发 HTTP Stop；应用组件关闭由 cmd 统一协调。
	go func() {
		<-ctx.Done()
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
		if !s.waitForBackgroundContext(ctx) {
			return ctx.Err()
		}
		return nil
	}
	s.stopped = true
	// httpServer 是需要执行优雅关闭的标准库 HTTP 服务。
	// httpDone 是监听 goroutine 退出的完成信号。
	// httpServer 保存httpServer，供当前处理流程使用
	httpServer := s.httpServer
	// httpDone 保存httpDone，供当前处理流程使用
	httpDone := s.httpDone
	s.lifecycleMu.Unlock()
	// shutdownErr 是 HTTP 优雅关闭返回的错误；后台等待错误由 worker 自身记录。
	var shutdownErr error
	if httpServer != nil {
		shutdownErr = httpServer.Shutdown(ctx)
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

// closedSignal 负责closedSignal相关处理。
func closedSignal() chan struct{} {
	// done 保存done，供当前处理流程使用
	done := make(chan struct{})
	close(done)
	return done
}

// lifecycleContext 负责lifecycle上下文相关处理。
func (s *Server) lifecycleContext() context.Context {
	if s == nil || s.applicationLifecycle == nil {
		return context.Background()
	}
	return s.applicationLifecycle.Context()
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
