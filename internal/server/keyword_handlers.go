package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// mountKeywordsReal 关键字端点。
func (s *Server) mountKeywordsReal(r chi.Router) {
	r.Get("/keywords/{cid}", s.listKeywords)
	r.Post("/keywords/{cid}", s.addKeyword)
	r.Get("/keywords-with-item-id/{cid}", s.listKeywordsWithItemID)
	r.Post("/keywords-with-item-id/{cid}", s.addKeywordWithItemID)
	r.Get("/keywords-with-type/{cid}", s.listKeywordsWithType)
	r.Put("/keywords-with-type/{cid}/{id}", s.updateKeywordByID)
	r.Delete("/keywords-with-type/{cid}/{id}", s.deleteKeywordByID)
	r.Delete("/keywords/{cid}/{index}", s.deleteKeyword)
}

func (s *Server) listKeywords(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]keywordBasicResponse, 0, len(rows))
	for _, k := range rows {
		out = append(out, keywordBasicResponse{Keyword: k.Keyword, Reply: k.Reply})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listKeywordsWithItemID(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]keywordItemResponse, 0, len(rows))
	for _, k := range rows {
		out = append(out, keywordItemResponse{Keyword: k.Keyword, Reply: k.Reply, ItemID: k.ItemID})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) listKeywordsWithType(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	out := make([]keywordTypedResponse, 0, len(rows))
	for _, k := range rows {
		out = append(out, keywordTypedResponse{
			ID: k.ID, Keyword: k.Keyword, Reply: k.Reply, ItemID: k.ItemID,
			Type: k.Type, ImageURL: k.ImageURL,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) addKeyword(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	var req struct {
		Keyword string `json:"keyword"`
		Reply   string `json:"reply"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Keyword) == "" {
		writeErr(w, http.StatusBadRequest, "keyword 必填")
		return
	}
	if strings.TrimSpace(req.Reply) == "" {
		writeErr(w, http.StatusBadRequest, "文字回复内容不能为空")
		return
	}
	if _, err := s.Store.Keywords.Add(r.Context(), cid, req.Keyword, req.Reply, "", "text", ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "添加失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func (s *Server) addKeywordWithItemID(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	var req struct {
		Keyword  string `json:"keyword"`
		Reply    string `json:"reply"`
		ItemID   string `json:"item_id"`
		Type     string `json:"type"`
		ImageURL string `json:"image_url"`
		Keywords *[]struct {
			Keyword  string `json:"keyword"`
			Reply    string `json:"reply"`
			ItemID   string `json:"item_id"`
			Type     string `json:"type"`
			ImageURL string `json:"image_url"`
		} `json:"keywords"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Keywords != nil {
		rows := make([]db.KeywordRow, 0, len(*req.Keywords))
		for _, item := range *req.Keywords {
			if strings.TrimSpace(item.Keyword) == "" {
				writeErr(w, http.StatusBadRequest, "keyword 必填")
				return
			}
			item.Type = strings.ToLower(strings.TrimSpace(item.Type))
			if item.Type == "" {
				item.Type = "text"
			}
			switch item.Type {
			case "text":
				if strings.TrimSpace(item.Reply) == "" {
					writeErr(w, http.StatusBadRequest, "文字回复内容不能为空")
					return
				}
				item.ImageURL = ""
			case "image":
				if strings.TrimSpace(item.ImageURL) == "" {
					writeErr(w, http.StatusBadRequest, "图片回复 URL 不能为空")
					return
				}
				item.Reply = ""
			default:
				writeErr(w, http.StatusBadRequest, "回复类型必须是 text 或 image")
				return
			}
			rows = append(rows, db.KeywordRow{
				CookieID: cid,
				Keyword:  item.Keyword,
				Reply:    item.Reply,
				ItemID:   item.ItemID,
				Type:     item.Type,
				ImageURL: item.ImageURL,
			})
		}
		if err := s.Store.Keywords.ReplaceForCookie(r.Context(), cid, rows); err != nil {
			writeErr(w, http.StatusInternalServerError, "保存失败")
			return
		}
		writeJSON(w, http.StatusOK, operationResponse{Success: true})
		return
	}
	if strings.TrimSpace(req.Keyword) == "" {
		writeErr(w, http.StatusBadRequest, "keyword 必填")
		return
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if req.Type == "" {
		req.Type = "text"
	}
	if req.Type == "text" && strings.TrimSpace(req.Reply) == "" {
		writeErr(w, http.StatusBadRequest, "文字回复内容不能为空")
		return
	}
	if req.Type == "image" && strings.TrimSpace(req.ImageURL) == "" {
		writeErr(w, http.StatusBadRequest, "图片回复 URL 不能为空")
		return
	}
	if req.Type != "text" && req.Type != "image" {
		writeErr(w, http.StatusBadRequest, "回复类型必须是 text 或 image")
		return
	}
	if req.Type == "image" {
		req.Reply = ""
	} else {
		req.ImageURL = ""
	}
	id, err := s.Store.Keywords.Add(r.Context(), cid, req.Keyword, req.Reply, req.ItemID, req.Type, req.ImageURL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "添加失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

func (s *Server) updateKeywordByID(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效关键词ID")
		return
	}
	var req struct {
		Keyword  string `json:"keyword"`
		Reply    string `json:"reply"`
		ItemID   string `json:"item_id"`
		Type     string `json:"type"`
		ImageURL string `json:"image_url"`
	}
	if err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Keyword) == "" {
		writeErr(w, http.StatusBadRequest, "keyword 必填")
		return
	}
	req.Type = strings.ToLower(strings.TrimSpace(req.Type))
	if req.Type == "" {
		req.Type = "text"
	}
	switch req.Type {
	case "text":
		if strings.TrimSpace(req.Reply) == "" {
			writeErr(w, http.StatusBadRequest, "文字回复内容不能为空")
			return
		}
		req.ImageURL = ""
	case "image":
		if strings.TrimSpace(req.ImageURL) == "" {
			writeErr(w, http.StatusBadRequest, "图片回复 URL 不能为空")
			return
		}
		req.Reply = ""
	default:
		writeErr(w, http.StatusBadRequest, "回复类型必须是 text 或 image")
		return
	}
	err = s.Store.Keywords.UpdateByID(r.Context(), db.KeywordRow{
		ID: id, CookieID: cid, Keyword: req.Keyword, Reply: req.Reply,
		ItemID: req.ItemID, Type: req.Type, ImageURL: req.ImageURL,
	})
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "关键字不存在")
			return
		}
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func (s *Server) deleteKeywordByID(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效关键词ID")
		return
	}
	if err := s.Store.Keywords.DeleteByID(r.Context(), cid, id); err != nil {
		writeErr(w, http.StatusNotFound, "关键字不存在")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func (s *Server) deleteKeyword(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	index := atoiDefault(chi.URLParam(r, "index"), -1)
	if err := s.Store.Keywords.DeleteByIndex(r.Context(), cid, index); err != nil {
		writeErr(w, http.StatusNotFound, "关键字不存在")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
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
	cookieIDs, _ := s.Store.Cookies.ListOwnedIDs(r.Context(), sess.UserID) // cookieIDs 是当前用户拥有的账号 ID。
	var result []itemReplyResponse
	for _, cid := range cookieIDs {
		rows, _ := s.Store.ItemReps.AllForUser(r.Context(), cid)
		for _, ir := range rows {
			result = append(result, itemReplyResponse{
				ItemID: ir.ItemID, CookieID: ir.CookieID, ReplyContent: ir.ReplyContent,
			})
		}
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) getItemReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	itemID := chi.URLParam(r, "item_id")
	ir, err := s.Store.ItemReps.Get(r.Context(), cid, itemID)
	if err != nil {
		writeJSON(w, http.StatusOK, itemReplyResponse{ReplyContent: ""})
		return
	}
	writeJSON(w, http.StatusOK, itemReplyResponse{
		ItemID: ir.ItemID, CookieID: ir.CookieID, ReplyContent: ir.ReplyContent,
	})
}

func (s *Server) setItemReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
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
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func (s *Server) deleteItemReply(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	itemID := chi.URLParam(r, "item_id")
	if err := s.Store.ItemReps.Delete(r.Context(), cid, itemID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
