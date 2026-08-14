package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedPasswordLoginRoutes 挂载已禁用密码登录的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedPasswordLoginRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Post("/api/v1/password-login", s.passwordLoginDisabled)
		r.Get("/api/v1/password-login/check/{session_id}", s.passwordLoginDisabled)
		r.Delete("/api/v1/password-login/cancel/{session_id}", s.passwordLoginDisabled)
	})
}
