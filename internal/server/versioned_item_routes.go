package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedItems 挂载商品列表、详情、发布、更新和删除的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedItems(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Get("/api/v1/items", s.listItems)
		r.Get("/api/v1/items/{cookie_id}/{item_id}", s.getItem)
		r.Post("/api/v1/items/publish", s.publishItem)
		r.Put("/api/v1/items/{cookie_id}/{item_id}", s.updateItem)
		r.Delete("/api/v1/items/{cookie_id}/{item_id}", s.deleteItem)
	})
}
