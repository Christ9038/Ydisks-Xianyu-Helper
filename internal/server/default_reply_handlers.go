package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"
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
	dr, err := s.Store.DefaultReps.Get(r.Context(), cid)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"enabled": false, "reply_content": "", "reply_once": false})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"enabled": dr.Enabled, "reply_content": dr.ReplyContent,
		"reply_image_url": dr.ReplyImageURL, "reply_once": dr.ReplyOnce,
	})
}

func (s *Server) setDefaultReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	var req struct {
		Enabled       bool   `json:"enabled"`
		ReplyContent  string `json:"reply_content"`
		ReplyImageURL string `json:"reply_image_url"`
		ReplyOnce     bool   `json:"reply_once"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	_, err := s.Store.DB.ExecContext(r.Context(),
		`INSERT OR REPLACE INTO default_replies (cookie_id, enabled, reply_content, reply_image_url, reply_once, updated_at)
		 VALUES (?,?,?,?,?,CURRENT_TIMESTAMP)`,
		cid, btoi(req.Enabled), req.ReplyContent, nullIfEmpty(req.ReplyImageURL), btoi(req.ReplyOnce))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) listDefaultReplies(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT cookie_id, enabled, reply_content, reply_once, reply_image_url FROM default_replies`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var cid, content, imageURL string
		var enabled, replyOnce int
		if err := rows.Scan(&cid, &enabled, &content, &replyOnce, &imageURL); err != nil {
			continue
		}
		out = append(out, map[string]any{
			"cookie_id": cid, "enabled": enabled != 0, "reply_content": content,
			"reply_once": replyOnce != 0, "reply_image_url": imageURL,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listDefaultRepliesMap(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT cookie_id, enabled, COALESCE(reply_content, ''), reply_once, COALESCE(reply_image_url, '') FROM default_replies`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	out := make(map[string]any)
	for rows.Next() {
		var cid, content, imageURL string
		var enabled, replyOnce int
		if err := rows.Scan(&cid, &enabled, &content, &replyOnce, &imageURL); err != nil {
			continue
		}
		out[cid] = map[string]any{
			"cookie_id":       cid,
			"enabled":         enabled != 0,
			"reply_content":   content,
			"reply_once":      replyOnce != 0,
			"reply_image_url": imageURL,
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) deleteDefaultReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	_, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM default_replies WHERE cookie_id=?`, cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) clearDefaultReplyRecords(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM default_reply_records WHERE cookie_id=?`, cid); err != nil {
		writeErr(w, http.StatusInternalServerError, "清空失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
