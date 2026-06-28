// Package server 实现 HTTP API 服务（chi 路由），移植自 Python reply_server.py 的 ~115 端点。
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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"xianyu-go/internal/account"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
	"xianyu-go/internal/webui"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/qrlogin"
)

// Server 聚合 HTTP 服务依赖。
type Server struct {
	Store   *db.Store
	Auth    *auth.Service
	Manager *account.Manager
	Browser *browser.Manager
	MTop    *mtop.Client
	QRLogin *qrlogin.Manager
	Logger  *slog.Logger
	WebDir  string // 前端静态资源目录（含 index.html）
	Addr    string
}

// New 构造。bm 为浏览器管理器（风控验证后用浏览器提取 cookie；为 nil 则禁用浏览器自动化）。
func New(store *db.Store, manager *account.Manager, bm *browser.Manager, secure bool, webDir, addr string, logger *slog.Logger) *Server {
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(os.Stdout, nil))
	}
	qrMgr := qrlogin.NewManager(logger)
	if bm != nil {
		qrMgr.SetSidecarRefresher(func(ctx context.Context, tmpCookies, verificationURL string, onScreenshot func(string)) (string, string, error) {
			return bm.QRCookieRefresh(ctx, tmpCookies, verificationURL, onScreenshot)
		})
	}
	return &Server{
		Store:   store,
		Auth:    &auth.Service{Store: store, Logger: logger, Secure: secure},
		Manager: manager,
		Browser: bm,
		MTop:    &mtop.Client{},
		QRLogin: qrMgr,
		Logger:  logger,
		WebDir:  webDir,
		Addr:    addr,
	}
}

// Router 构建完整路由树。
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.RealIP)
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
		// 扫码登录
		s.mountQRLoginReal(r)
		// 订单
		s.mountOrdersReal(r)
		// 订单分析（仪表盘）
		s.mountAnalyticsReal(r)
		// 卡密 + 发货规则
		s.mountCardsReal(r)
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
		s.mountSettings(r)
		// AI 设置
		s.mountAIReply(r)
		// 用户
		s.mountUser(r)

		// 管理员专用。
		r.Group(func(r chi.Router) {
			r.Use(auth.RequireAdmin)
			s.mountAdmin(r)
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
// 覆盖前端 vite proxy 列表中的所有后端前缀。
func isAPIPath(path string) bool {
	apiPrefixes := []string{
		"/api/", "/admin/", "/health", "/login", "/logout", "/verify",
		"/change-password", "/change-admin-password", "/account/",
		"/cookies", "/cookie/", "/orders", "/analytics",
		"/cards", "/delivery-rules", "/items", "/keywords", "/default-replies", "/default-reply",
		"/notification-channels", "/message-notifications",
		"/system-settings", "/ai-reply", "/ai-reply-settings",
		"/ai-models",
		"/user-settings", "/logs", "/risk-control",
		"/item-reply", "/itemReplays",
		"/register", "/registration-status", "/login-info-status",
		"/qr-login", "/password-login", "/face-verification",
		"/generate-captcha", "/verify-captcha", "/geetest", "/send-verification-code",
		"/backup", "/upload-image", "/send-message", "/xianyu",
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
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok"})
}

// 占位：各分组 mount 方法在 handlers 文件中实现。
// 为避免大文件，mount* 方法分文件定义但属于 Server。

// Run 启动 HTTP 服务（阻塞）。
func (s *Server) Run(ctx context.Context) error {
	srv := &http.Server{
		Addr:    s.Addr,
		Handler: s.Router(),
	}
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
