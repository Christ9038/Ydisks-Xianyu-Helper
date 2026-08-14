package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// mountPasswordLogin 保留历史 API 路径，避免旧前端收到 404。Go 客户端的
// 登录凭证只允许由扫码流程产生；这里永远不会调用 Chromium 密码登录。
func (s *Server) mountPasswordLogin(r chi.Router) {
	r.Post("/password-login", s.passwordLoginDisabled)
	r.Get("/password-login/check/{session_id}", s.passwordLoginDisabled)
	r.Delete("/password-login/cancel/{session_id}", s.passwordLoginDisabled)
}

// passwordLoginDisabled 返回密码登录已禁用的统一错误响应，保留旧路径兼容性。
func (s *Server) passwordLoginDisabled(w http.ResponseWriter, _ *http.Request) {
	writeErrCode(
		w,
		http.StatusNotImplemented,
		"password_login_disabled",
		"Go 客户端仅支持扫码登录，密码登录已禁用", "")
}
