// Package auth 实现 HttpOnly Cookie 会话认证，沿用 Fork 版安全基线：
//   - 无默认口令（管理员可经首次启动 Web UI 或 init-admin CLI 初始化）
//   - 会话存 DB（sessions 表），HttpOnly + SameSite=Lax Cookie 传递
//   - bcrypt 哈希，兼容老库无盐 SHA-26（首次登录静默升级）
//
// 中间件基于 net/http，兼容 chi。
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"xianyu-go/internal/db"
)

// CookieName 是会话 Cookie 名。
const CookieName = "session"

// CookieMaxAge 会话有效期（24 小时）。
const CookieMaxAge = 24 * 60 * 60

// ctxKey 上下文键类型，避免冲突。
type ctxKey int

// ctxKeyUser 保存ctxKey用户，供当前处理流程使用
const (
	ctxKeyUser ctxKey = iota
)

// Service 认证服务。
type Service struct {
	Store  *db.Store
	Logger *slog.Logger
	Secure bool // HTTPS 下应为 true，Cookie 加 Secure 标记
}

// LoginResult 登录结果。
type LoginResult struct {
	Success bool
	User    *db.User
	Message string
}

// Login 用户名密码登录。成功时返回会话 ID（由调用方写入 Cookie）。
func (s *Service) Login(ctx context.Context, username, password string) (string, *db.User, error) {
	// user、ok、err 保存user、ok、err，供当前处理流程使用
	user, ok, err := s.Store.Users.VerifyAndUpgrade(ctx, username, password)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, nil // 密码错误或用户不存在/未激活
	}
	// sid、err 保存sid、err，供当前处理流程使用
	sid, err := s.Store.Sessions.Create(ctx, user)
	if err != nil {
		return "", nil, err
	}
	return sid, user, nil
}

// Logout 删除会话并返回清 Cookie 所需操作。
func (s *Service) Logout(ctx context.Context, sessionID string) {
	if sessionID != "" {
		_ = s.Store.Sessions.Delete(ctx, sessionID)
	}
}

// SetSessionCookie 把会话 ID 写入响应 Cookie（HttpOnly + SameSite=Lax）。
func (s *Service) SetSessionCookie(w http.ResponseWriter, sessionID string) {
	// #nosec G124 -- HttpOnly/SameSite 始终设置；Secure 由部署是否启用 HTTPS 决定。
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    sessionID,
		Path:     "/",
		MaxAge:   CookieMaxAge,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.Secure,
	})
}

// ClearSessionCookie 清除会话 Cookie。
func (s *Service) ClearSessionCookie(w http.ResponseWriter) {
	// #nosec G124 -- 清除 Cookie 必须与创建时使用相同的 Secure 配置。
	http.SetCookie(w, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   s.Secure,
	})
}

// SessionFromRequest 从请求 Cookie 取会话。无效返回 nil。
func (s *Service) SessionFromRequest(ctx context.Context, r *http.Request) (*db.Session, error) {
	// c、err 保存c、err，供当前处理流程使用
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	// sess、err 保存sess、err，供当前处理流程使用
	sess, err := s.Store.Sessions.Get(ctx, c.Value)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return sess, nil
}

// Middleware 解析会话并把 *db.Session 放入请求上下文。不强制登录（公开路由也可用）。
func (s *Service) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// sess、err 保存sess、err，供当前处理流程使用
		sess, err := s.SessionFromRequest(r.Context(), r)
		if err != nil {
			s.Logger.Warn("读取会话失败", "err", err)
		}
		// ctx 保存ctx，供当前处理流程使用
		ctx := WithSession(r.Context(), sess)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// RequireAuth 要求已登录，否则 401。
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFromContext(r.Context()) == nil {
			writeAuthError(w, r, http.StatusUnauthorized, "unauthorized", "未授权访问")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin 要求管理员，否则 403（须在 RequireAuth 之后）。
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// sess 保存sess，供当前处理流程使用
		sess := SessionFromContext(r.Context())
		if sess == nil || !sess.IsAdmin {
			writeAuthError(w, r, http.StatusForbidden, "forbidden", "需要管理员权限")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// WithSession / SessionFromContext 上下文存取。
func WithSession(ctx context.Context, sess *db.Session) context.Context {
	return context.WithValue(ctx, ctxKeyUser, sess)
}

// SessionFromContext 负责会话From上下文相关处理。
func SessionFromContext(ctx context.Context) *db.Session {
	// v 保存v，供当前处理流程使用
	v, _ := ctx.Value(ctxKeyUser).(*db.Session)
	return v
}

// SessionIdentity 是 HTTP 应用层读取当前用户身份所需的最小会话视图，不暴露数据库会话字段。
type SessionIdentity struct {
	// UserID 是当前认证用户的稳定数据库标识，仅用于应用层归属和授权判断。
	UserID int64
}

// IdentityFromContext 从请求上下文提取最小用户身份；未认证或上下文没有会话时返回 nil。
func IdentityFromContext(ctx context.Context) *SessionIdentity {
	// session 保存认证中间件注入的完整会话，仅在认证包内部读取并裁剪为最小身份视图。
	session := SessionFromContext(ctx)
	if session == nil {
		return nil
	}
	return &SessionIdentity{UserID: session.UserID}
}

// InitAdmin 创建或重置 admin 管理员账号，是 cmd/server -init-admin 与
// cmd/init-admin 共用的公共入口。返回 created=true 表示新建 admin，
// false 表示重置已存在 admin 的密码。邮箱仅在新建时使用。
// InitAdmin 负责InitAdmin相关处理。
func InitAdmin(ctx context.Context, store *db.Store, email, password string) (created bool, err error) {
	// existing、err 保存existing、err，供当前处理流程使用
	existing, err := store.Users.GetAdmin(ctx)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return false, fmt.Errorf("查询 admin 失败: %w", err)
	}
	if existing != nil {
		// ok、err 保存ok、err，供当前处理流程使用
		ok, err := store.Users.UpdatePassword(ctx, existing.Username, password)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, fmt.Errorf("admin 用户存在但密码未更新")
		}
		return false, store.Users.SetAdmin(ctx, existing.Username)
	}
	// ok、err 保存ok、err，供当前处理流程使用
	ok, err := store.Users.Create(ctx, "admin", email, password)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("创建 admin 用户失败：用户名或邮箱可能已存在")
	}
	return true, store.Users.SetAdmin(ctx, "admin")
}
