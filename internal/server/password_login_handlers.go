package server

import (
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	accountapp "xianyu-go/internal/application/account"
)

// mountPasswordLogin 保留历史 API 路径，避免旧前端收到 404。Go 客户端的
// 登录凭证只允许由扫码流程产生；这里永远不会调用 Chromium 密码登录。
// mountPasswordLogin 封装mount密码登录业务协调。
func (s *Server) mountPasswordLogin(r chi.Router) {
	r.Post("/password-login", s.passwordLoginDisabled)
	r.Get("/password-login/check/{session_id}", s.passwordLoginDisabled)
	r.Delete("/password-login/cancel/{session_id}", s.passwordLoginDisabled)
}

// passwordLoginApplication 返回当前 Server 绑定的密码登录应用服务。
func (s *Server) passwordLoginApplication() *accountapp.PasswordLoginService {
	return s.applicationServiceSet().passwordLogin
}

// passwordLoginDisabled 通过应用层关闭策略返回统一错误，保留旧路径和版本化路径兼容性。
func (s *Server) passwordLoginDisabled(w http.ResponseWriter, r *http.Request) {
	// service 是当前 Server 装配的密码登录应用服务。
	service := s.passwordLoginApplication()
	if service == nil {
		writeErrCode(w, http.StatusInternalServerError, "internal_error", "密码登录应用服务未初始化", "")
		return
	}
	// userID 保存鉴权中间件注入的本地用户身份；密码和 Cookie 不从请求体读取。
	var userID int64
	// sess 保存当前请求的认证会话；未经过完整路由装配时允许为空。
	sess := authSess(r)
	if sess != nil {
		userID = sess.UserID
	}
	// operationErr 保存应用服务对当前 HTTP 操作返回的关闭策略结果。
	var operationErr error
	switch r.Method {
	case http.MethodPost:
		operationErr = service.Start(r.Context(), accountapp.PasswordLoginStartInput{UserID: userID})
	case http.MethodGet:
		operationErr = service.Check(r.Context(), accountapp.PasswordLoginSessionInput{UserID: userID, SessionID: chi.URLParam(r, "session_id")})
	case http.MethodDelete:
		operationErr = service.Cancel(r.Context(), accountapp.PasswordLoginSessionInput{UserID: userID, SessionID: chi.URLParam(r, "session_id")})
	default:
		writeErrCode(w, http.StatusMethodNotAllowed, "method_not_allowed", "不支持的密码登录操作", "")
		return
	}
	if errors.Is(operationErr, accountapp.ErrPasswordLoginDisabled) {
		writeErrCode(w, http.StatusNotImplemented, "password_login_disabled", "Go 客户端仅支持扫码登录，密码登录已禁用", "")
		return
	}
	if operationErr != nil {
		writeErrCode(w, http.StatusInternalServerError, "internal_error", "密码登录操作失败", "")
	}
}
