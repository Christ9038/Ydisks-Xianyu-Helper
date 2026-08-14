package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedOrders 挂载订单列表、详情和更新的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedOrders(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Get("/api/v1/orders", s.listOrders)
		r.Get("/api/v1/orders/{order_id}", s.getOrder)
		r.Put("/api/v1/orders/{order_id}", s.updateOrder)
		r.Post("/api/v1/orders/refresh", s.refreshOrders)
		r.Post("/api/v1/orders/manual-ship", s.manualShipOrders)
		r.Post("/api/v1/orders/import", s.importOrders)
		r.Post("/api/v1/orders/{order_id}/refresh", s.refreshSingleOrder)
	})
}
