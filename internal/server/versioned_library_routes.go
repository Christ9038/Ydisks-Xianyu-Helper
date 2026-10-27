package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedLibraryRoutes 挂载本地资源库（图片库与商品模板）的 `/api/v1/library` 入口。
func (s *Server) mountVersionedLibraryRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)

		r.Get("/api/v1/library/images", s.listLibraryImages)
		r.Post("/api/v1/library/images", s.uploadLibraryImages)
		r.Get("/api/v1/library/images/{image_id}/content", s.getLibraryImageContent)
		r.Delete("/api/v1/library/images/{image_id}", s.deleteLibraryImage)

		r.Get("/api/v1/library/product-templates", s.listLibraryTemplates)
		r.Post("/api/v1/library/product-templates", s.createLibraryTemplate)
		r.Post("/api/v1/library/product-templates/publish-preview", s.previewLibraryTemplatePublish)
		r.Get("/api/v1/library/product-templates/{template_id}", s.getLibraryTemplate)
		r.Put("/api/v1/library/product-templates/{template_id}", s.updateLibraryTemplate)
		r.Delete("/api/v1/library/product-templates/{template_id}", s.deleteLibraryTemplate)
	})
}
