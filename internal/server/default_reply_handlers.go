package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// btoi bool→int（SQLite 无原生 bool）。
func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// nullIfEmpty 空串存为 NULL。
func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

// mountDefaultRepliesReal 默认回复端点。
func (s *Server) mountDefaultRepliesReal(r chi.Router) {
	r.Get("/default-replies/{cid}", s.getDefaultReply)
	r.Put("/default-replies/{cid}", s.setDefaultReply)
	r.Get("/default-replies", s.listDefaultReplies)
	r.Delete("/default-replies/{cid}", s.deleteDefaultReply)
	r.Get("/api/default-replies", s.listDefaultRepliesMap)
	r.Get("/api/default-reply/{cid}", s.getDefaultReply)
	r.Put("/api/default-reply/{cid}", s.setDefaultReply)
	r.Delete("/api/default-reply/{cid}", s.deleteDefaultReply)
	r.Post("/api/default-reply/{cid}/clear-records", s.clearDefaultReplyRecords)
}

func (s *Server) getDefaultReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	dr, err := s.Store.DefaultReps.Get(r.Context(), cid)
	if err != nil {
		writeJSON(w, http.StatusOK, defaultReplyResponse{Enabled: false, ReplyContent: "", ReplyOnce: false})
		return
	}
	// 已保存默认回复通过具名 DTO 返回，单账号查询不填充 cookie_id。
	writeJSON(w, http.StatusOK, newDefaultReplyResponse("", *dr))
}

// setDefaultReply 保存指定账号的默认回复配置。
func (s *Server) setDefaultReply(w http.ResponseWriter, r *http.Request) {
	// cid 是当前操作的账号标识。
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// req 是默认回复配置请求体。
	var req struct {
		Enabled       bool   `json:"enabled"`
		ReplyContent  string `json:"reply_content"`
		ReplyImageURL string `json:"reply_image_url"`
		ReplyOnce     bool   `json:"reply_once"`
	}
	// err 是请求体解码错误。
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// err 是默认回复写入错误。
	err := s.Store.DefaultReps.Upsert(r.Context(), cid, db.DefaultReply{
		Enabled: req.Enabled, ReplyContent: req.ReplyContent,
		ReplyImageURL: req.ReplyImageURL, ReplyOnce: req.ReplyOnce,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// listDefaultReplies 查询当前用户的默认回复列表。
func (s *Server) listDefaultReplies(w http.ResponseWriter, r *http.Request) {
	// sess 是当前登录用户会话。
	sess := auth.SessionFromContext(r.Context())
	// rows 和 err 是当前用户的默认回复列表及查询错误。
	rows, err := s.Store.DefaultReps.ListForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 是默认回复列表响应。
	var out []defaultReplyResponse
	for _, row := range rows {
		out = append(out, defaultReplyResponse{
			CookieID: row.CookieID, Enabled: row.Enabled, ReplyContent: row.ReplyContent, ReplyOnce: row.ReplyOnce,
			ReplyImageURL: row.ReplyImageURL,
			// 列表 DTO 保留账号标识，前端可直接建立按账号索引。
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// listDefaultRepliesMap 查询按账号索引的默认回复映射。
func (s *Server) listDefaultRepliesMap(w http.ResponseWriter, r *http.Request) {
	// sess 是当前登录用户会话。
	sess := auth.SessionFromContext(r.Context())
	// rows 和 err 是当前用户的默认回复列表及查询错误。
	rows, err := s.Store.DefaultReps.ListForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 是按账号标识索引的默认回复映射。
	out := make(map[string]defaultReplyResponse)
	for _, row := range rows {
		out[row.CookieID] = defaultReplyResponse{
			CookieID:      row.CookieID,
			Enabled:       row.Enabled,
			ReplyContent:  row.ReplyContent,
			ReplyOnce:     row.ReplyOnce,
			ReplyImageURL: row.ReplyImageURL,
			// map 键与 cookie_id 同时保留，兼容旧前端索引方式。
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// deleteDefaultReply 删除指定账号的默认回复配置。
func (s *Server) deleteDefaultReply(w http.ResponseWriter, r *http.Request) {
	// cid 是当前操作的账号标识。
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// err 是默认回复删除错误。
	err := s.Store.DefaultReps.Delete(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// clearDefaultReplyRecords 清空指定账号的默认回复发送记录。
func (s *Server) clearDefaultReplyRecords(w http.ResponseWriter, r *http.Request) {
	// cid 是当前操作的账号标识。
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// err 是默认回复记录清理错误。
	if err := s.Store.DefaultReps.ClearRecords(r.Context(), cid); err != nil {
		writeErr(w, http.StatusInternalServerError, "清空失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
