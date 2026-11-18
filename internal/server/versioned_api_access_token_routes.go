package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedAPIAccessTokenRoutes 挂载仅允许真实浏览器会话管理的外部 API 令牌入口。
func (s *Server) mountVersionedAPIAccessTokenRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireSession)
		r.Get("/api/v1/access-tokens", s.listAPIAccessTokens)
		r.Post("/api/v1/access-tokens", s.createAPIAccessToken)
		r.Delete("/api/v1/access-tokens/{token_id}", s.revokeAPIAccessToken)
	})
}
