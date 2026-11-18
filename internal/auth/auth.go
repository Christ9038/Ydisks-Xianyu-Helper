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
	"strings"
	"time"

	"xianyu-go/internal/application/apikey"

	"xianyu-go/internal/db"
	"xianyu-go/internal/logging"
)

// CookieName 是会话 Cookie 名。
const CookieName = "session"

// CookieMaxAge 会话有效期（24 小时）。
const CookieMaxAge = 24 * 60 * 60

// ctxKey 上下文键类型，避免冲突。
type ctxKey int

// ctxKeyUser 用于本次流程后续判断的ctxKey用户
const (
	ctxKeyUser ctxKey = iota
	ctxKeyAPIKey
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
	// user、ok、err 用于本次流程后续判断的user、ok、err
	user, ok, err := s.Store.Users.VerifyAndUpgrade(ctx, username, password)
	if err != nil {
		return "", nil, err
	}
	if !ok {
		return "", nil, nil // 密码错误或用户不存在/未激活
	}
	// sid、err 用于本次流程后续判断的sid、err
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
	// c、err 用于本次流程后续判断的c、err
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return nil, nil
	}
	// sess、err 用于本次流程后续判断的sess、err
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
		// authorization 保存调用方显式提供的认证头；只支持标准 Bearer 形式，不读取 URL 参数令牌。
		authorization := strings.TrimSpace(r.Header.Get("Authorization"))
		if authorization != "" {
			// token、validBearer 保存解析出的明文令牌和认证头格式是否有效；令牌只留在本次请求栈内。
			token, validBearer := parseBearerToken(authorization)
			if validBearer && s.Store != nil && s.Store.APIAccessTokens != nil {
				// now 是本次令牌认证和最近使用时间的 Unix 秒数。
				now := time.Now().Unix()
				// principal、lookupErr 保存按摘要查询的有效用户身份及数据库错误；查询不读取任何闲鱼凭证。
				principal, lookupErr := s.Store.APIAccessTokens.Authenticate(r.Context(), apikey.HashToken(token), now)
				if lookupErr == nil {
					// 外部令牌响应统一禁止缓存，避免账号、聊天和订单数据进入共享缓存。
					w.Header().Set("Cache-Control", "no-store")
					// identity 是放入请求上下文的非秘密令牌身份与权限快照。
					identity := APIKeyIdentity{ID: principal.ID, Prefix: principal.Prefix, Scopes: append([]string(nil), principal.Scopes...)}
					// session 仅为复用既有 handler 的 user_id 归属判断构造，不代表浏览器会话或管理员权限。
					session := &db.Session{UserID: principal.UserID, Username: principal.Username, IsAdmin: false}
					// ctx 绑定令牌身份和兼容既有 handler 的最小用户会话。
					ctx := WithAPIKey(WithSession(r.Context(), session), identity)
					ctx = logging.With(ctx, slog.Int64("user_id", session.UserID))
					// touchErr 仅影响最近使用时间展示，不影响已经完成的令牌认证结果。
					if touchErr := s.Store.APIAccessTokens.TouchLastUsed(r.Context(), principal.ID, now); touchErr != nil && s.Logger != nil {
						s.Logger.Debug("更新 API 访问令牌最近使用时间失败", "err", touchErr)
					}
					next.ServeHTTP(w, r.WithContext(ctx))
					return
				}
				if !errors.Is(lookupErr, db.ErrNotFound) && s.Logger != nil {
					s.Logger.Warn("校验 API 访问令牌失败", "err", lookupErr)
				}
			}
			// 显式 Bearer 认证失败时不回退到 Cookie，避免无效令牌意外借用浏览器登录态。
			next.ServeHTTP(w, r.WithContext(WithSession(r.Context(), nil)))
			return
		}
		// sess、err 保存传统 HttpOnly Cookie 会话及其读取错误。
		sess, err := s.SessionFromRequest(r.Context(), r)
		if err != nil && s.Logger != nil {
			s.Logger.Warn("读取会话失败", "err", err)
		}
		// ctx 保存传统浏览器会话；Cookie 认证行为与历史版本保持一致。
		ctx := WithSession(r.Context(), sess)
		if sess != nil {
			ctx = logging.With(ctx, slog.Int64("user_id", sess.UserID))
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// parseBearerToken 只接受 Authorization: Bearer <token>，不接受查询参数或 Cookie 中的 API 令牌。
func parseBearerToken(authorization string) (string, bool) {
	// parts 是按空白分割后的认证方案与令牌字段。
	parts := strings.Fields(authorization)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") || !strings.HasPrefix(parts[1], apikey.TokenPrefix) {
		return "", false
	}
	return parts[1], true
}

// RequireAuth 要求已登录，否则 401。
func RequireAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFromContext(r.Context()) == nil {
			writeAuthError(w, r, http.StatusUnauthorized, "unauthorized", "未授权访问")
			return
		}
		// identity 是已认证 Bearer 令牌的非秘密权限快照；浏览器 Cookie 会话时为空。
		if identity := APIKeyFromContext(r.Context()); identity != nil {
			// requiredScope 是当前 API 方法和路径唯一允许的令牌权限；未登记路径默认拒绝。
			requiredScope, allowed := requiredAPIKeyScope(r.Method, r.URL.Path)
			if !allowed || !hasAPIKeyScope(identity.Scopes, requiredScope) {
				writeAuthError(w, r, http.StatusForbidden, "insufficient_scope", "API 访问令牌未授予此操作权限")
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

// RejectAPIKey 禁止外部 API 令牌访问公开的登录、初始化、会话校验和退出入口。
// 这些路由不使用 RequireAuth，因此必须显式拒绝已认证的 Bearer 身份，避免令牌绕过 scope 白名单。
func RejectAPIKey(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if APIKeyFromContext(r.Context()) != nil {
			writeAuthError(w, r, http.StatusForbidden, "session_required", "外部 API 访问令牌不能调用会话入口")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireSession 要求真实浏览器 Cookie 会话；外部 API 令牌不能管理令牌、修改登录凭据或进入管理员页面。
func RequireSession(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if SessionFromContext(r.Context()) == nil || APIKeyFromContext(r.Context()) != nil {
			writeAuthError(w, r, http.StatusUnauthorized, "session_required", "此操作需要浏览器登录会话")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// RequireAdmin 要求管理员，否则 403（须在 RequireAuth 之后）。
func RequireAdmin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// sess 用于本次流程后续判断的sess
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

// SessionFromContext 封装会话From上下文业务协调。
func SessionFromContext(ctx context.Context) *db.Session {
	// v 用于本次流程后续判断的v
	v, _ := ctx.Value(ctxKeyUser).(*db.Session)
	return v
}

// APIKeyIdentity 是外部访问令牌的非秘密上下文视图。
type APIKeyIdentity struct {
	// ID 是已认证令牌的本地记录标识。
	ID string
	// Prefix 是令牌管理界面使用的非秘密前缀。
	Prefix string
	// Scopes 是该令牌的权限快照，仅用于当前请求授权。
	Scopes []string
}

// WithAPIKey 把已通过数据库认证的令牌身份绑定到请求上下文。
func WithAPIKey(ctx context.Context, identity APIKeyIdentity) context.Context {
	return context.WithValue(ctx, ctxKeyAPIKey, identity)
}

// APIKeyFromContext 读取当前请求的外部令牌身份；浏览器 Cookie 会话返回 nil。
func APIKeyFromContext(ctx context.Context) *APIKeyIdentity {
	// identity 是认证中间件注入的令牌权限快照。
	identity, ok := ctx.Value(ctxKeyAPIKey).(APIKeyIdentity)
	if !ok {
		return nil
	}
	return &identity
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
// InitAdmin 封装InitAdmin业务协调。
func InitAdmin(ctx context.Context, store *db.Store, email, password string) (created bool, err error) {
	// existing、err 用于本次流程后续判断的existing、err
	existing, err := store.Users.GetAdmin(ctx)
	if err != nil && !errors.Is(err, db.ErrNotFound) {
		return false, fmt.Errorf("查询 admin 失败: %w", err)
	}
	if existing != nil {
		// ok、err 用于本次流程后续判断的ok、err
		ok, err := store.Users.UpdatePassword(ctx, existing.Username, password)
		if err != nil {
			return false, err
		}
		if !ok {
			return false, fmt.Errorf("admin 用户存在但密码未更新")
		}
		return false, store.Users.SetAdmin(ctx, existing.Username)
	}
	// ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "admin", email, password)
	if err != nil {
		return false, err
	}
	if !ok {
		return false, fmt.Errorf("创建 admin 用户失败：用户名或邮箱可能已存在")
	}
	return true, store.Users.SetAdmin(ctx, "admin")
}
