package server

import (
	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountVersionedReplyRoutes 挂载关键词回复、指定商品回复和默认回复的 `/api/v1` 兼容入口。
func (s *Server) mountVersionedReplyRoutes(r chi.Router) {
	r.Group(func(r chi.Router) {
		r.Use(s.Auth.Middleware)
		r.Use(auth.RequireAuth)

		// 指定商品回复使用独立资源前缀，避免与账号级关键词规则混淆。
		r.Get("/api/v1/reply-rules/items", s.listItemReplies)
		r.Get("/api/v1/reply-rules/items/{cookie_id}/{item_id}", s.getItemReply)
		r.Put("/api/v1/reply-rules/items/{cookie_id}/{item_id}", s.setItemReply)
		r.Delete("/api/v1/reply-rules/items/{cookie_id}/{item_id}", s.deleteItemReply)

		// 关键词规则保留基础、商品和类型三种旧响应语义。
		r.Get("/api/v1/reply-rules/{cid}", s.listKeywords)
		r.Post("/api/v1/reply-rules/{cid}", s.addKeyword)
		r.Get("/api/v1/reply-rules/{cid}/items", s.listKeywordsWithItemID)
		r.Post("/api/v1/reply-rules/{cid}/items", s.addKeywordWithItemID)
		r.Get("/api/v1/reply-rules/{cid}/typed", s.listKeywordsWithType)
		r.Put("/api/v1/reply-rules/{cid}/typed/{id}", s.updateKeywordByID)
		r.Delete("/api/v1/reply-rules/{cid}/typed/{id}", s.deleteKeywordByID)
		r.Delete("/api/v1/reply-rules/{cid}/index/{index}", s.deleteKeyword)

		// 默认回复的列表、单账号配置和记录清理统一使用默认回复资源前缀。
		r.Get("/api/v1/default-replies", s.listDefaultRepliesMap)
		r.Get("/api/v1/default-replies/list", s.listDefaultReplies)
		r.Get("/api/v1/default-replies/{cid}", s.getDefaultReply)
		r.Put("/api/v1/default-replies/{cid}", s.setDefaultReply)
		r.Delete("/api/v1/default-replies/{cid}", s.deleteDefaultReply)
		r.Post("/api/v1/default-replies/{cid}/clear-records", s.clearDefaultReplyRecords)
	})
}
