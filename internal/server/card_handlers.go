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

func (s *Server) listCards(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	cards, err := s.Store.Cards.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, newCardResponses(cards))
}

func (s *Server) getCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	cf, ok := s.requireCardOwner(w, r, id)
	if !ok {
		return
	}
	writeJSON(w, http.StatusOK, newCardResponse(*cf))
}

func (s *Server) createCard(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
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
	id, err := s.Store.Cards.Create(r.Context(), cf)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "创建失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

func (s *Server) updateCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	cf, err := decodeCard(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
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
	if err := s.Store.Cards.Update(r.Context(), cf); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func (s *Server) deleteCard(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(chi.URLParam(r, "card_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效卡券ID")
		return
	}
	if _, ok := s.requireCardOwner(w, r, id); !ok {
		return
	}
	if err := s.Store.Cards.Delete(r.Context(), id); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func decodeCard(r *http.Request) (*db.CardFull, error) {
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
	if err := decodeJSON(r, &req); err != nil {
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

func errStr(s string) error { return &simpleError{s} }

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }
