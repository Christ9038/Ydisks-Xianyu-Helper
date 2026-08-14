package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedQRLoginRoutes 挂载二维码登录的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedQRLoginRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Post("/api/v1/qr-login/generate", s.generateQRLogin)
		r.Get("/api/v1/qr-login/check/{session_id}", s.checkQRLoginStatus)
		r.Get("/api/v1/qr-login/status/{session_id}", s.checkQRLoginStatusAndPersist)
		r.Post("/api/v1/qr-login/complete-verification/{session_id}", s.completeQRVerification)
	})
}
