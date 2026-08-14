package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedItemBatchRoutes 挂载商品同步、类目推荐和批量发布的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedItemBatchRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)
		r.Post("/api/v1/items/get-all-from-account", s.syncItemsFromAccount)
		r.Post("/api/v1/items/get-by-page", s.syncItemsPageFromAccount)
		r.Post("/api/v1/items/publish-categories/recommend", s.recommendItemPublishCategory)
		r.Post("/api/v1/items/publish-batches/preview", s.previewItemPublishBatch)
		r.Post("/api/v1/items/publish-batches", s.startItemPublishBatch)
		r.Get("/api/v1/items/publish-batches", s.listItemPublishBatches)
		r.Get("/api/v1/items/publish-batches/{batch_id}/result.csv", s.downloadItemPublishBatchResult)
		r.Get("/api/v1/items/publish-batches/{batch_id}", s.getItemPublishBatch)
		r.Delete("/api/v1/items/publish-batches/{batch_id}", s.deleteItemPublishBatch)
		r.Post("/api/v1/items/publish-batches/{batch_id}/cancel", s.cancelItemPublishBatch)
		r.Post("/api/v1/items/publish-batches/{batch_id}/retry-failed", s.retryFailedItemPublishBatch)
	})
}
