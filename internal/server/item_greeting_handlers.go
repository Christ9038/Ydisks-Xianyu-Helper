package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	itemgreetingapp "xianyu-go/internal/application/itemgreeting"
)

// itemGreetingRequest 是创建或更新商品首次回复规则的具名请求 DTO。
type itemGreetingRequest struct {
	// CookieID 是规则所属账号；更新时可省略。
	CookieID string `json:"cookie_id"`
	// Name 是规则备注名。
	Name string `json:"name"`
	// Enabled 表示规则是否生效。
	Enabled bool `json:"enabled"`
	// ReplyContent 是首次回复文字，可含 {send_user_name}、{send_user_id}、{send_message}、{item_id}。
	ReplyContent string `json:"reply_content"`
	// ReplyImageURL 是首次回复图片地址。
	ReplyImageURL string `json:"reply_image_url"`
	// ItemIDs 是规则覆盖的商品标识。
	ItemIDs []string `json:"item_ids"`
}

// itemGreetingResponse 是商品首次回复规则的具名响应 DTO。
type itemGreetingResponse struct {
	// ID 是规则主键。
	ID int64 `json:"id"`
	// CookieID 是规则所属账号。
	CookieID string `json:"cookie_id"`
	// Name 是规则备注名。
	Name string `json:"name"`
	// Enabled 表示规则是否生效。
	Enabled bool `json:"enabled"`
	// ReplyContent 是首次回复文字。
	ReplyContent string `json:"reply_content"`
	// ReplyImageURL 是首次回复图片地址。
	ReplyImageURL string `json:"reply_image_url"`
	// ItemIDs 是规则覆盖的商品标识。
	ItemIDs []string `json:"item_ids"`
}

// newItemGreetingResponse 把应用层规则转换为响应 DTO，保证 item_ids 永不为 null。
func newItemGreetingResponse(rule itemgreetingapp.Rule) itemGreetingResponse {
	// itemIDs 保存非 nil 的商品集合。
	itemIDs := rule.ItemIDs
	if itemIDs == nil {
		itemIDs = []string{}
	}
	return itemGreetingResponse{
		ID: rule.ID, CookieID: rule.CookieID, Name: rule.Name, Enabled: rule.Enabled,
		ReplyContent: rule.ReplyContent, ReplyImageURL: rule.ReplyImageURL, ItemIDs: itemIDs,
	}
}

// itemGreetingApplication 返回商品首次回复应用服务。
func (s *Server) itemGreetingApplication() ItemGreetingsPort {
	return s.applicationServiceSet().itemGreetings
}

// listItemGreetings 查询当前用户的商品首次回复规则，可按 cookie_id 过滤。
func (s *Server) listItemGreetings(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户及会话是否存在。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// rules、err 保存应用服务返回的规则及查询错误。
	rules, err := s.itemGreetingApplication().List(r.Context(), userID, r.URL.Query().Get("cookie_id"))
	if err != nil {
		writeItemGreetingError(w, r, err, "查询失败")
		return
	}
	// out 保存响应 DTO，空结果返回 []。
	out := make([]itemGreetingResponse, 0, len(rules))
	// rule 是当前处理的规则。
	for _, rule := range rules {
		out = append(out, newItemGreetingResponse(rule))
	}
	writeJSON(w, http.StatusOK, out)
}

// createItemGreeting 创建商品首次回复规则。
func (s *Server) createItemGreeting(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户及会话是否存在。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// req 是创建请求体。
	var req itemGreetingRequest
	// decodeErr 保存请求体 JSON 解析错误。
	if decodeErr := decodeJSON(r, &req); decodeErr != nil {
		writeErrRequest(w, r, http.StatusBadRequest, "请求格式错误")
		return
	}
	// rule、err 保存新建规则及写入错误。
	rule, err := s.itemGreetingApplication().Create(r.Context(), userID, req.draft())
	if err != nil {
		writeItemGreetingError(w, r, err, "保存失败")
		return
	}
	writeJSON(w, http.StatusCreated, newItemGreetingResponse(rule))
}

// updateItemGreeting 覆盖商品首次回复规则。
func (s *Server) updateItemGreeting(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户及会话是否存在。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// id 是路径中的规则主键。
	id, ok := itemGreetingID(w, r)
	if !ok {
		return
	}
	// req 是更新请求体。
	var req itemGreetingRequest
	// decodeErr 保存请求体 JSON 解析错误。
	if decodeErr := decodeJSON(r, &req); decodeErr != nil {
		writeErrRequest(w, r, http.StatusBadRequest, "请求格式错误")
		return
	}
	// rule、err 保存更新后的规则及写入错误。
	rule, err := s.itemGreetingApplication().Update(r.Context(), userID, id, req.draft())
	if err != nil {
		writeItemGreetingError(w, r, err, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, newItemGreetingResponse(rule))
}

// deleteItemGreeting 删除商品首次回复规则。
func (s *Server) deleteItemGreeting(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户及会话是否存在。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// id、ok 保存路径中的规则主键及其是否合法。
	id, ok := itemGreetingID(w, r)
	if !ok {
		return
	}
	// err 保存 Delete 的返回错误，非空时按该分支处理。
	if err := s.itemGreetingApplication().Delete(r.Context(), userID, id); err != nil {
		writeItemGreetingError(w, r, err, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// clearItemGreetingRecords 清空指定账号的商品首次回复发送记录。
func (s *Server) clearItemGreetingRecords(w http.ResponseWriter, r *http.Request) {
	// userID、ok 保存认证会话中的当前用户及会话是否存在。
	userID, ok := defaultReplyUserID(w, r)
	if !ok {
		return
	}
	// err 保存 ClearRecords 的返回错误，非空时按该分支处理。
	if err := s.itemGreetingApplication().ClearRecords(r.Context(), userID, chi.URLParam(r, "cookie_id")); err != nil {
		writeItemGreetingError(w, r, err, "清空失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// draft 把请求 DTO 转换为应用层草稿。
func (req itemGreetingRequest) draft() itemgreetingapp.Draft {
	return itemgreetingapp.Draft{
		CookieID: req.CookieID, Name: req.Name, Enabled: req.Enabled,
		ReplyContent: req.ReplyContent, ReplyImageURL: req.ReplyImageURL, ItemIDs: req.ItemIDs,
	}
}

// itemGreetingID 解析路径中的规则主键，非法值返回 400。
func itemGreetingID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	// id、err 保存解析结果；主键必须是正整数。
	id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if err != nil || id <= 0 {
		writeErrRequest(w, r, http.StatusBadRequest, "规则标识无效")
		return 0, false
	}
	return id, true
}

// writeItemGreetingError 将应用错误映射为统一错误信封；商品冲突通过 details 返回冲突商品。
func writeItemGreetingError(w http.ResponseWriter, r *http.Request, err error, fallback string) {
	// conflict 保存商品冲突的详细信息。
	var conflict *itemgreetingapp.ItemConflictError
	// invalid 保存可展示的参数错误。
	var invalid *itemgreetingapp.InvalidInputError
	switch {
	case errors.As(err, &conflict):
		writeErrDetails(w, http.StatusConflict, "conflict", "部分商品已被其他首次回复规则使用", middleware.GetReqID(r.Context()),
			map[string]any{"item_ids": conflict.ItemIDs})
	case errors.As(err, &invalid):
		writeErrRequest(w, r, http.StatusBadRequest, invalid.Reason)
	case errors.Is(err, itemgreetingapp.ErrInvalidCookieID):
		writeErrRequest(w, r, http.StatusBadRequest, "账号标识无效")
	case errors.Is(err, itemgreetingapp.ErrAccountNotFound):
		writeErrRequest(w, r, http.StatusNotFound, "账号不存在")
	case errors.Is(err, itemgreetingapp.ErrRuleNotFound):
		writeErrRequest(w, r, http.StatusNotFound, "规则不存在")
	case errors.Is(err, itemgreetingapp.ErrForbidden):
		writeErrRequest(w, r, http.StatusForbidden, "无权限操作该账号")
	default:
		writeErrRequest(w, r, http.StatusInternalServerError, fallback)
	}
}
