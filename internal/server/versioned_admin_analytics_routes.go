package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedAdminAnalyticsRoutes 挂载管理员和统计分析的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedAdminAnalyticsRoutes(r chi.Router) {
	// 普通用户只能访问自己的仪表盘和订单分析数据。
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Get("/api/v1/analytics/dashboard", s.dashboardStats)
		r.Get("/api/v1/analytics/orders", s.orderAnalytics)
		r.Get("/api/v1/analytics/orders/valid", s.validOrders)
	})

	// 管理员资源保持旧入口的管理员权限边界。
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Use(auth.RequireAdmin)
		r.Get("/api/v1/admin/users", s.adminListUsers)
		r.Delete("/api/v1/admin/users/{user_id}", s.adminDeleteUser)
		r.Get("/api/v1/admin/cookies", s.adminListCookies)
		r.Get("/api/v1/admin/stats", s.adminStats)
	})
}
