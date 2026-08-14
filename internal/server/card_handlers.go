package server

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// mountCardsReal 卡券 CRUD（真实实现）。发货规则已统一到 automation_rules。
func (s *Server) mountCardsReal(r chi.Router) {
	r.Get("/cards", s.listCards)
	r.Post("/cards", s.createCard)
	r.Post("/cards/batch", s.batchCreateCards)
	r.Post("/cards/{card_id}/append-data", s.appendCardData)
	r.Get("/cards/{card_id}/details", s.getCard)
	r.Get("/cards/{card_id}", s.getCard)
	r.Put("/cards/{card_id}", s.updateCard)
	r.Delete("/cards/{card_id}", s.deleteCard)
}

// listCards 负责list卡密列表相关处理。
func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// cards、err 保存cards、err，供当前处理流程使用
	cards, err := s.Store.Cards.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newCardResponses(cards))
}

// getCard 负责get卡密相关处理。
func (s *Server) getCard(w http.ResponseWriter, r *http.Request) {
	// id、err 保存id、err，供当前处理流程使用
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	// cf、ok 保存cf、ok，供当前处理流程使用
	cf, ok := s.requireCardOwner(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, newCardResponse(*cf))
}

// createCard 负责create卡密相关处理。
func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// cf、err 保存cf、err，供当前处理流程使用
	cf, err := decodeCard(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	cf.UserID = sess.UserID
	if cf.Type == "api" {
		writeErr(w, http.StatusBadRequest, "API 卡密暂不支持自动发货，不能新建")
		return
	}
	// id、err 保存id、err，供当前处理流程使用
	id, err := s.Store.Cards.Create(r.Context(), cf)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

// updateCard 负责update卡密相关处理。
func (s *Server) updateCard(w http.ResponseWriter, r *http.Request) {
	// id、err 保存id、err，供当前处理流程使用
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	// cf、err 保存cf、err，供当前处理流程使用
	cf, err := decodeCard(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// existing、ok 保存existing、ok，供当前处理流程使用
	existing, ok := s.requireCardOwner(w, r, id)
	if !ok {
		return
	}
	if cf.Type == "api" && existing.Type != "api" {
		writeErr(w, http.StatusBadRequest, "API 卡密暂不支持自动发货，不能转换为该类型")
		return
	}
	cf.ID = id
	cf.UserID = existing.UserID
	if // err 保存err，供当前处理流程使用
	err := s.Store.Cards.Update(r.Context(), cf); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteCard 负责delete卡密相关处理。
func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	// id、err 保存id、err，供当前处理流程使用
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := s.requireCardOwner(w, r, id); !ok {
		return
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.Cards.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// decodeCard 负责decode卡密相关处理。
func decodeCard(r *http.Request) (*db.CardFull, error) {
	// req 保存req，供当前处理流程使用
	var req struct {
		Name         string `json:"name"`
		Type         string `json:"type"`
		APIConfig    string `json:"api_config"`
		TextContent  string `json:"text_content"`
		DataContent  string `json:"data_content"`
		ImageURL     string `json:"image_url"`
		Description  string `json:"description"`
		Enabled      bool   `json:"enabled"`
		DelaySeconds int    `json:"delay_seconds"`
		IsMultiSpec  bool   `json:"is_multi_spec"`
		SpecName     string `json:"spec_name"`
		SpecValue    string `json:"spec_value"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		return nil, err
	}
	if req.Name == "" || req.Type == "" {
		return nil, errStr("名称和类型不能为空")
	}
	switch req.Type {
	case "text", "data", "image", "api":
	default:
		return nil, errStr("类型必须为 text、data、image 或 api")
	}
	if req.DelaySeconds < 0 || req.DelaySeconds > 3600 {
		return nil, errStr("延时发货必须在 0 到 3600 秒之间")
	}
	switch req.Type {
	case "text":
		if strings.TrimSpace(req.TextContent) == "" {
			return nil, errStr("文本卡密内容不能为空")
		}
	case "data":
		if strings.TrimSpace(req.DataContent) == "" {
			return nil, errStr("数据卡密内容不能为空")
		}
	case "image":
		if strings.TrimSpace(req.ImageURL) == "" {
			return nil, errStr("图片卡密 URL 不能为空")
		}
	}
	return &db.CardFull{
		Name: req.Name, Type: req.Type, APIConfig: req.APIConfig, TextContent: req.TextContent,
		DataContent: req.DataContent, ImageURL: req.ImageURL, Description: req.Description,
		Enabled: req.Enabled, DelaySeconds: req.DelaySeconds, IsMultiSpec: req.IsMultiSpec,
		SpecName: req.SpecName, SpecValue: req.SpecValue,
	}, nil
}

// itemOwnedByUser 校验 cookieID 归属当前用户且其下存在 itemID 商品。
// 由自动化规则校验复用（原 deliveryRuleItemOwned，发货规则删除后改名）。
// itemOwnedByUser 负责商品OwnedBy用户相关处理。
func (s *Server) itemOwnedByUser(r *http.Request, userID int64, cookieID, itemID string) bool {
	if itemID == "" {
		return true
	}
	if cookieID == "" {
		return false
	}
	owned, err := s.Store.Cookies.ExistsOwned(r.Context(), userID, cookieID) // owned 和 err 表示账号归属及查询错误。
	if err != nil || !owned {
		return false
	}
	_, err = s.Store.Items.Get(r.Context(), cookieID, itemID)
	return err == nil
}

// errStr 负责errStr相关处理。
func errStr(s string) error { return &simpleError{s} }

// simpleError 保存simple错误，供当前处理流程使用
type simpleError struct{ msg string }

// Error 负责错误相关处理。
func (e *simpleError) Error() string { return e.msg }
