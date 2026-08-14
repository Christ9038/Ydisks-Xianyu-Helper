package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedAutomationRoutes 挂载自动化规则和异常处理的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedAutomationRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Get("/api/v1/automation-rules", s.listAutomationRules)
		r.Post("/api/v1/automation-rules", s.createAutomationRule)
		r.Put("/api/v1/automation-rules/{rule_id}", s.updateAutomationRule)
		r.Delete("/api/v1/automation-rules/{rule_id}", s.deleteAutomationRule)
		r.Get("/api/v1/automation-issues", s.listAutomationIssues)
		r.Post("/api/v1/automation-runs/{run_id}/resolve", s.resolveAutomationRun)
		r.Post("/api/v1/automation-pending-tasks/{task_id}/resolve", s.resolveDeferredAutomationTask)
	})
}
