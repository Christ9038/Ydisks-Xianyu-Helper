package server

import (
	"net/http"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountKeywordsReal 关键字端点。
func (s *Server) mountKeywordsReal(r chi.Router) {
	r.Get("/keywords/{cid}", s.listKeywords)
	r.Post("/keywords/{cid}", s.addKeyword)
	r.Get("/keywords-with-item-id/{cid}", s.listKeywordsWithItemID)
	r.Post("/keywords-with-item-id/{cid}", s.addKeywordWithItemID)
	r.Get("/keywords-with-type/{cid}", s.listKeywordsWithType)
	r.Delete("/keywords/{cid}/{index}", s.deleteKeyword)
}

func (s *Server) listKeywords(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, k := range rows {
		out = append(out, map[string]any{"keyword": k.Keyword, "reply": k.Reply})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listKeywordsWithItemID(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, k := range rows {
		out = append(out, map[string]any{"keyword": k.Keyword, "reply": k.Reply, "item_id": k.ItemID})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listKeywordsWithType(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]map[string]any, 0, len(rows))
	for _, k := range rows {
		out = append(out, map[string]any{
			"keyword": k.Keyword, "reply": k.Reply, "item_id": k.ItemID,
			"type": k.Type, "image_url": k.ImageURL,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) addKeyword(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	var req struct {
		Keyword string `json:"keyword"`
		Reply   string `json:"reply"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Keyword == "" {
		writeErr(w, http.StatusBadRequest, "keyword 必填")
		return
	}
	if _, err := s.Store.Keywords.Add(r.Context(), cid, req.Keyword, req.Reply, "", "text", ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "添加失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) addKeywordWithItemID(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	var req struct {
		Keyword string `json:"keyword"`
		Reply   string `json:"reply"`
		ItemID  string `json:"item_id"`
	}
	if err := decodeJSON(r, &req); err != nil || req.Keyword == "" {
		writeErr(w, http.StatusBadRequest, "keyword 必填")
		return
	}
	if _, err := s.Store.Keywords.Add(r.Context(), cid, req.Keyword, req.Reply, req.ItemID, "text", ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "添加失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteKeyword(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	index := atoiDefault(chi.URLParam(r, "index"), -1)
	if err := s.Store.Keywords.DeleteByIndex(r.Context(), cid, index); err != nil {
		writeErr(w, http.StatusNotFound, "关键字不存在")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// ---- 指定商品回复 ----
func (s *Server) mountItemRepliesReal(r chi.Router) {
	r.Get("/itemReplays", s.listItemReplies)
	r.Get("/item-reply/{cookie_id}/{item_id}", s.getItemReply)
	r.Put("/item-reply/{cookie_id}/{item_id}", s.setItemReply)
	r.Delete("/item-reply/{cookie_id}/{item_id}", s.deleteItemReply)
}

func (s *Server) listItemReplies(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	all, _ := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	var result []map[string]any
	for cid := range all {
		rows, _ := s.Store.ItemReps.AllForUser(r.Context(), cid)
		for _, ir := range rows {
			result = append(result, map[string]any{
				"item_id": ir.ItemID, "cookie_id": ir.CookieID, "reply_content": ir.ReplyContent,
			})
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) getItemReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	itemID := chi.URLParam(r, "item_id")
	ir, err := s.Store.ItemReps.Get(r.Context(), cid, itemID)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"reply_content": ""})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"item_id": ir.ItemID, "cookie_id": ir.CookieID, "reply_content": ir.ReplyContent,
	})
}

func (s *Server) setItemReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	itemID := chi.URLParam(r, "item_id")
	var req struct {
		ReplyContent string `json:"reply_content"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if err := s.Store.ItemReps.Set(r.Context(), cid, itemID, req.ReplyContent); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) deleteItemReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if _, ok := s.requireCookieOwner(w, r, cid); !ok {
		return
	}
	itemID := chi.URLParam(r, "item_id")
	if err := s.Store.ItemReps.Delete(r.Context(), cid, itemID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}
