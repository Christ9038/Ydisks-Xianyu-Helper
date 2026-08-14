package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedChatTaskRoutes 挂载聊天和账号任务的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedChatTaskRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)

		r.Get("/api/v1/chat/sessions", s.listChatSessions)
		r.Get("/api/v1/chat/messages", s.listChatMessages)
		r.Post("/api/v1/chat/messages", s.sendChatMessage)
		r.Post("/api/v1/chat/images", s.sendChatImage)
		r.Post("/api/v1/chat/read", s.markChatRead)
		r.Get("/api/v1/chat/ws", s.chatWebSocket)

		r.Get("/api/v1/account-tasks/{cid}", s.getAccountTaskSettings)
		r.Put("/api/v1/account-tasks/{cid}", s.updateAccountTaskSettings)
		r.Get("/api/v1/account-tasks/{cid}/runs", s.listAccountTaskRuns)
		r.Post("/api/v1/account-tasks/{cid}/run", s.runAccountTask)
	})
}
