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

// listKeywords 负责listKeywords相关处理。
func (s *Server) listKeywords(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 保存out，供当前处理流程使用
	out := make([]keywordBasicResponse, 0, len(rows))
	// k 表示当前遍历过程中的k
	for _, k := range rows {
		out = append(out, keywordBasicResponse{Keyword: k.Keyword, Reply: k.Reply})
	}
	writeJSON(w, http.StatusOK, out)
}

// listKeywordsWithItemID 负责listKeywordsWith商品ID相关处理。
func (s *Server) listKeywordsWithItemID(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 保存out，供当前处理流程使用
	out := make([]keywordItemResponse, 0, len(rows))
	// k 表示当前遍历过程中的k
	for _, k := range rows {
		out = append(out, keywordItemResponse{Keyword: k.Keyword, Reply: k.Reply, ItemID: k.ItemID})
	}
	writeJSON(w, http.StatusOK, out)
}

// listKeywordsWithType 负责listKeywordsWith类型相关处理。
func (s *Server) listKeywordsWithType(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// rows、err 保存rows、err，供当前处理流程使用
	rows, err := s.Store.Keywords.AllRows(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 保存out，供当前处理流程使用
	out := make([]keywordTypedResponse, 0, len(rows))
	// k 表示当前遍历过程中的k
	for _, k := range rows {
		out = append(out, keywordTypedResponse{
			ID: k.ID, Keyword: k.Keyword, Reply: k.Reply, ItemID: k.ItemID,
			Type: k.Type, ImageURL: k.ImageURL,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// addKeyword 负责add关键词相关处理。
func (s *Server) addKeyword(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// req 保存req，供当前处理流程使用
	var req struct {
		Keyword string `json:"keyword"`
		Reply   string `json:"reply"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Keyword) == "" {
		writeErr(w, http.StatusBadRequest, "keyword 必填")
		return
	}
	if strings.TrimSpace(req.Reply) == "" {
		writeErr(w, http.StatusBadRequest, "文字回复内容不能为空")
		return
	}
	if // err 保存err，供当前处理流程使用
	_, err := s.Store.Keywords.Add(r.Context(), cid, req.Keyword, req.Reply, "", "text", ""); err != nil {
		writeErr(w, http.StatusInternalServerError, "添加失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// addKeywordWithItemID 负责add关键词With商品ID相关处理。
func (s *Server) addKeywordWithItemID(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// req 保存req，供当前处理流程使用
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
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if req.Keywords != nil {
		// rows 保存rows，供当前处理流程使用
		rows := make([]db.KeywordRow, 0, len(*req.Keywords))
		// item 表示当前遍历过程中的商品
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
		if // err 保存err，供当前处理流程使用
		err := s.Store.Keywords.ReplaceForCookie(r.Context(), cid, rows); err != nil {
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
	// id、err 保存id、err，供当前处理流程使用
	id, err := s.Store.Keywords.Add(r.Context(), cid, req.Keyword, req.Reply, req.ItemID, req.Type, req.ImageURL)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "添加失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

// updateKeywordByID 负责update关键词ByID相关处理。
func (s *Server) updateKeywordByID(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// id、err 保存id、err，供当前处理流程使用
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效关键词ID")
		return
	}
	// req 保存req，供当前处理流程使用
	var req struct {
		Keyword  string `json:"keyword"`
		Reply    string `json:"reply"`
		ItemID   string `json:"item_id"`
		Type     string `json:"type"`
		ImageURL string `json:"image_url"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil || strings.TrimSpace(req.Keyword) == "" {
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

// deleteKeywordByID 负责delete关键词ByID相关处理。
func (s *Server) deleteKeywordByID(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// id、err 保存id、err，供当前处理流程使用
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效关键词ID")
		return
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.Keywords.DeleteByID(r.Context(), cid, id); err != nil {
		writeErr(w, http.StatusNotFound, "关键字不存在")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteKeyword 负责delete关键词相关处理。
func (s *Server) deleteKeyword(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// index 保存index，供当前处理流程使用
	index := atoiDefault(chi.URLParam(r, "index"), -1)
	if // err 保存err，供当前处理流程使用
	err := s.Store.Keywords.DeleteByIndex(r.Context(), cid, index); err != nil {
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

// listItemReplies 负责list商品回复列表相关处理。
func (s *Server) listItemReplies(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	cookieIDs, _ := s.Store.Cookies.ListOwnedIDs(r.Context(), sess.UserID) // cookieIDs 是当前用户拥有的账号 ID。
	var result []itemReplyResponse
	// cid 表示当前遍历过程中的cid
	for _, cid := range cookieIDs {
		// rows 保存rows，供当前处理流程使用
		rows, _ := s.Store.ItemReps.AllForUser(r.Context(), cid)
		// ir 表示当前遍历过程中的ir
		for _, ir := range rows {
			result = append(result, itemReplyResponse{
				ItemID: ir.ItemID, CookieID: ir.CookieID, ReplyContent: ir.ReplyContent,
			})
		}
	}
	writeJSON(w, http.StatusOK, result)
}

// getItemReply 负责get商品回复相关处理。
func (s *Server) getItemReply(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// itemID 保存商品ID，供当前处理流程使用
	itemID := chi.URLParam(r, "item_id")
	// ir、err 保存ir、err，供当前处理流程使用
	ir, err := s.Store.ItemReps.Get(r.Context(), cid, itemID)
	if err != nil {
		writeJSON(w, http.StatusOK, itemReplyResponse{ReplyContent: ""})
		return
	}
	writeJSON(w, http.StatusOK, itemReplyResponse{
		ItemID: ir.ItemID, CookieID: ir.CookieID, ReplyContent: ir.ReplyContent,
	})
}

// setItemReply 负责set商品回复相关处理。
func (s *Server) setItemReply(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// itemID 保存商品ID，供当前处理流程使用
	itemID := chi.URLParam(r, "item_id")
	// req 保存req，供当前处理流程使用
	var req struct {
		ReplyContent string `json:"reply_content"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.ItemReps.Set(r.Context(), cid, itemID, req.ReplyContent); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteItemReply 负责delete商品回复相关处理。
func (s *Server) deleteItemReply(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cookie_id")
	if !s.requireCookieOwnership(w, r, cid) {
		return
	}
	// itemID 保存商品ID，供当前处理流程使用
	itemID := chi.URLParam(r, "item_id")
	if // err 保存err，供当前处理流程使用
	err := s.Store.ItemReps.Delete(r.Context(), cid, itemID); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
