package server

import "github.com/go-chi/chi/v5"

// mountHealthAndVersionedRoutes 挂载健康检查及当前阶段的版本化 API 入口。
func (s *Server) mountHealthAndVersionedRoutes(r chi.Router) {
	r.Get("/health", s.health)
	s.mountVersionedSession(r)
	s.mountVersionedAccounts(r)
	s.mountVersionedOrders(r)
	s.mountVersionedItems(r)
	s.mountVersionedItemBatchRoutes(r)
	s.mountVersionedSettingsCardNotificationRoutes(r)
	s.mountVersionedChatTaskRoutes(r)
	s.mountVersionedReplyRoutes(r)
}

// mountVersionedSession 挂载会话 API 的 `/api/v1` 兼容入口，复用现有 handler。
func (s *Server) mountVersionedSession(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Post("/api/v1/session/login", s.login)
		r.Post("/api/v1/session/initialize", s.initialize)
		r.Get("/api/v1/session", s.verify)
		r.Post("/api/v1/session/logout", s.logout)
	})
}
