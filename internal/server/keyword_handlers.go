package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/application/keywords"
	"xianyu-go/internal/auth"
)

// keywordRequest 是普通关键词接口使用的文字回复请求 DTO。
type keywordRequest struct {
	// Keyword 是触发匹配文本。
	Keyword string `json:"keyword"`
	// Reply 是文字回复正文。
	Reply string `json:"reply"`
}

// keywordBatchItem 是带商品范围的关键词批量项 DTO。
type keywordBatchItem struct {
	// Keyword 是触发匹配文本。
	Keyword string `json:"keyword"`
	// Reply 是文字回复正文。
	Reply string `json:"reply"`
	// ItemID 是可选商品标识。
	ItemID string `json:"item_id"`
	// Type 是 text 或 image 回复类型。
	Type string `json:"type"`
	// ImageURL 是图片回复地址。
	ImageURL string `json:"image_url"`
}

// keywordBatchRequest 是关键词批量替换或单项创建请求 DTO。
type keywordBatchRequest struct {
	// Keyword 是单项创建的触发匹配文本。
	Keyword string `json:"keyword"`
	// Reply 是单项创建的文字回复正文。
	Reply string `json:"reply"`
	// ItemID 是单项创建的可选商品标识。
	ItemID string `json:"item_id"`
	// Type 是单项创建的回复类型。
	Type string `json:"type"`
	// ImageURL 是单项创建的图片回复地址。
	ImageURL string `json:"image_url"`
	// Keywords 是批量替换项；为空指针表示请求使用单项模式。
	Keywords *[]keywordBatchItem `json:"keywords"`
}

// keywordUpdateRequest 是按 ID 更新关键词的请求 DTO。
type keywordUpdateRequest struct {
	// Keyword 是触发匹配文本。
	Keyword string `json:"keyword"`
	// Reply 是文字回复正文。
	Reply string `json:"reply"`
	// ItemID 是可选商品标识。
	ItemID string `json:"item_id"`
	// Type 是 text 或 image 回复类型。
	Type string `json:"type"`
	// ImageURL 是图片回复地址。
	ImageURL string `json:"image_url"`
}

// itemReplyRequest 是指定商品回复写入请求 DTO。
type itemReplyRequest struct {
	// ReplyContent 是商品命中后的回复正文。
	ReplyContent string `json:"reply_content"`
}

// mountKeywordsReal 注册关键词回复兼容路由。
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

// keywordApplication 返回关键词回复应用服务；具体依赖由 Server 统一装配。
func (s *Server) keywordApplication() KeywordsPort {
	return s.applicationServiceSet().keywords
}

// keywordUserID 从认证上下文读取当前用户，不读取任何账号凭证。
func keywordUserID(w http.ResponseWriter, r *http.Request) (int64, bool) {
	// session 是认证中间件注入的当前用户会话。
	session := auth.SessionFromContext(r.Context())
	if session == nil {
		writeErr(w, http.StatusUnauthorized, "未授权访问")
		return 0, false
	}
	return session.UserID, true
}

// writeKeywordError 将应用层错误映射为现有关键词接口的 HTTP 语义。
func writeKeywordError(w http.ResponseWriter, err error, fallback string) {
	if err == nil {
		return
	}
	// validationErr 是可安全展示给调用方的参数校验提示。
	var validationErr *keywords.ValidationError
	if errors.As(err, &validationErr) {
		writeErr(w, http.StatusBadRequest, validationErr.Error())
		return
	}
	switch {
	case errors.Is(err, keywords.ErrForbidden):
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
	case errors.Is(err, keywords.ErrNotFound):
		writeErr(w, http.StatusNotFound, "关键字不存在")
	case errors.Is(err, keywords.ErrInvalidInput):
		writeErr(w, http.StatusBadRequest, "请求参数无效")
	default:
		writeErr(w, http.StatusInternalServerError, fallback)
	}
}

// listKeywords 返回兼容的基础关键词响应。
func (s *Server) listKeywords(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cid")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// rows 保存应用层查询结果。
	rows, err := s.keywordApplication().List(r.Context(), userID, cookieID)
	if err != nil {
		writeKeywordError(w, err, "查询失败")
		return
	}
	// result 保存 HTTP 基础响应，隐藏数据库模型和内部字段。
	result := make([]keywordBasicResponse, 0, len(rows))
	// row 是当前待映射的关键词规则。
	for _, row := range rows {
		result = append(result, keywordBasicResponse{Keyword: row.Keyword, Reply: row.Reply})
	}
	writeJSON(w, http.StatusOK, result)
}

// listKeywordsWithItemID 返回带商品范围的兼容关键词响应。
func (s *Server) listKeywordsWithItemID(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cid")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// rows 保存应用层查询结果。
	rows, err := s.keywordApplication().List(r.Context(), userID, cookieID)
	if err != nil {
		writeKeywordError(w, err, "查询失败")
		return
	}
	// result 保存带商品字段的 HTTP 响应。
	result := make([]keywordItemResponse, 0, len(rows))
	// row 是当前待映射的关键词规则。
	for _, row := range rows {
		result = append(result, keywordItemResponse{Keyword: row.Keyword, Reply: row.Reply, ItemID: row.ItemID})
	}
	writeJSON(w, http.StatusOK, result)
}

// listKeywordsWithType 返回支持 text/image 类型的兼容响应。
func (s *Server) listKeywordsWithType(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cid")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// rows 保存应用层查询结果。
	rows, err := s.keywordApplication().List(r.Context(), userID, cookieID)
	if err != nil {
		writeKeywordError(w, err, "查询失败")
		return
	}
	// result 保存带类型字段的 HTTP 响应。
	result := make([]keywordTypedResponse, 0, len(rows))
	// row 是当前待映射的关键词规则。
	for _, row := range rows {
		result = append(result, keywordTypedResponse{ID: row.ID, Keyword: row.Keyword, Reply: row.Reply, ItemID: row.ItemID, Type: row.Type, ImageURL: row.ImageURL})
	}
	writeJSON(w, http.StatusOK, result)
}

// addKeyword 创建一条普通文字关键词规则。
func (s *Server) addKeyword(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cid")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// request 是普通关键词请求 DTO。
	var request keywordRequest
	// err 表示请求 JSON 解码失败。
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, "keyword 必填")
		return
	}
	// _, err 表示创建操作的结果；ID 对兼容响应不向客户端暴露。
	if _, err := s.keywordApplication().Add(r.Context(), userID, cookieID, keywords.Draft{Keyword: request.Keyword, Reply: request.Reply, Type: "text"}); err != nil {
		writeKeywordError(w, err, "添加失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// addKeywordWithItemID 创建或批量替换带商品范围的关键词规则。
func (s *Server) addKeywordWithItemID(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cid")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// request 是单项或批量关键词请求 DTO。
	var request keywordBatchRequest
	// err 表示请求 JSON 解码失败。
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if request.Keywords != nil {
		// drafts 保存经过应用服务校验的批量关键词输入。
		drafts := make([]keywords.Draft, 0, len(*request.Keywords))
		// item 是当前待转换的批量请求项。
		for _, item := range *request.Keywords {
			drafts = append(drafts, keywords.Draft{Keyword: item.Keyword, Reply: item.Reply, ItemID: item.ItemID, Type: item.Type, ImageURL: item.ImageURL})
		}
		// err 表示批量替换规则的应用服务错误。
		if err := s.keywordApplication().Replace(r.Context(), userID, cookieID, drafts); err != nil {
			writeKeywordError(w, err, "保存失败")
			return
		}
		writeJSON(w, http.StatusOK, operationResponse{Success: true})
		return
	}
	// id 是新建规则的持久化标识；兼容响应保留该字段。
	id, err := s.keywordApplication().Add(r.Context(), userID, cookieID, keywords.Draft{Keyword: request.Keyword, Reply: request.Reply, ItemID: request.ItemID, Type: request.Type, ImageURL: request.ImageURL})
	if err != nil {
		writeKeywordError(w, err, "添加失败")
		return
	}
	writeJSON(w, http.StatusOK, mutationIDResponse{Success: true, ID: id})
}

// updateKeywordByID 按关键词 ID 更新 text/image 规则。
func (s *Server) updateKeywordByID(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cid")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// id 是路由中的关键词持久化标识。
	id, parseErr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if parseErr != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效关键词ID")
		return
	}
	// request 是关键词更新请求 DTO。
	var request keywordUpdateRequest
	// err 表示请求 JSON 解码失败。
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, "keyword 必填")
		return
	}
	// updateErr 表示应用层更新结果。
	updateErr := s.keywordApplication().Update(r.Context(), userID, cookieID, id, keywords.Draft{Keyword: request.Keyword, Reply: request.Reply, ItemID: request.ItemID, Type: request.Type, ImageURL: request.ImageURL})
	if updateErr != nil {
		writeKeywordError(w, updateErr, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteKeywordByID 按持久化 ID 删除关键词规则。
func (s *Server) deleteKeywordByID(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cid")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// id 是路由中的关键词持久化标识。
	id, parseErr := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
	if parseErr != nil || id <= 0 {
		writeErr(w, http.StatusBadRequest, "无效关键词ID")
		return
	}
	// deleteErr 表示应用层删除结果。
	deleteErr := s.keywordApplication().DeleteByID(r.Context(), userID, cookieID, id)
	if deleteErr != nil {
		writeKeywordError(w, deleteErr, "关键字不存在")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteKeyword 按兼容的零基索引删除关键词规则。
func (s *Server) deleteKeyword(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cid")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// index 是按关键词 ID 顺序解释的零基索引。
	index, parseErr := strconv.Atoi(chi.URLParam(r, "index"))
	if parseErr != nil {
		index = -1
	}
	// deleteErr 表示应用层删除结果。
	deleteErr := s.keywordApplication().DeleteByIndex(r.Context(), userID, cookieID, index)
	if deleteErr != nil {
		writeKeywordError(w, deleteErr, "关键字不存在")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// mountItemRepliesReal 注册指定商品回复兼容路由。
func (s *Server) mountItemRepliesReal(r chi.Router) {
	r.Get("/itemReplays", s.listItemReplies)
	r.Get("/item-reply/{cookie_id}/{item_id}", s.getItemReply)
	r.Put("/item-reply/{cookie_id}/{item_id}", s.setItemReply)
	r.Delete("/item-reply/{cookie_id}/{item_id}", s.deleteItemReply)
}

// listItemReplies 返回当前用户全部账号的指定商品回复。
func (s *Server) listItemReplies(w http.ResponseWriter, r *http.Request) {
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// rows 保存应用层商品回复结果。
	rows, err := s.keywordApplication().ListItemReplies(r.Context(), userID)
	if err != nil {
		// 兼容历史接口：列表查询失败仍返回空列表和 200。
		writeJSON(w, http.StatusOK, []itemReplyResponse{})
		return
	}
	// result 保存 HTTP 商品回复响应。
	result := make([]itemReplyResponse, 0, len(rows))
	// row 是当前待映射的商品回复。
	for _, row := range rows {
		result = append(result, itemReplyResponse{ItemID: row.ItemID, CookieID: row.CookieID, ReplyContent: row.ReplyContent})
	}
	writeJSON(w, http.StatusOK, result)
}

// getItemReply 返回指定商品回复；缺失时保持历史空正文响应。
func (s *Server) getItemReply(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cookie_id")
	// itemID 是路由中的商品标识。
	itemID := chi.URLParam(r, "item_id")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// row 保存应用层商品回复结果。
	row, err := s.keywordApplication().GetItemReply(r.Context(), userID, cookieID, itemID)
	if errors.Is(err, keywords.ErrNotFound) {
		writeJSON(w, http.StatusOK, itemReplyResponse{ReplyContent: ""})
		return
	}
	if err != nil {
		writeKeywordError(w, err, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, itemReplyResponse{ItemID: row.ItemID, CookieID: row.CookieID, ReplyContent: row.ReplyContent})
}

// setItemReply 覆盖指定商品回复。
func (s *Server) setItemReply(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cookie_id")
	// itemID 是路由中的商品标识。
	itemID := chi.URLParam(r, "item_id")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// request 是指定商品回复请求 DTO。
	var request itemReplyRequest
	// err 表示请求 JSON 解码失败。
	if err := decodeJSON(r, &request); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// err 表示应用层写入结果。
	if err := s.keywordApplication().SetItemReply(r.Context(), userID, cookieID, itemID, request.ReplyContent); err != nil {
		writeKeywordError(w, err, "保存失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// deleteItemReply 删除指定商品回复。
func (s *Server) deleteItemReply(w http.ResponseWriter, r *http.Request) {
	// cookieID 是路由中的账号标识。
	cookieID := chi.URLParam(r, "cookie_id")
	// itemID 是路由中的商品标识。
	itemID := chi.URLParam(r, "item_id")
	// userID 是当前认证用户标识。
	userID, ok := keywordUserID(w, r)
	if !ok {
		return
	}
	// err 表示应用层删除结果。
	if err := s.keywordApplication().DeleteItemReply(r.Context(), userID, cookieID, itemID); err != nil {
		writeKeywordError(w, err, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
