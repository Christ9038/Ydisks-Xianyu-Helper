package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/auth"
)

// chatQuickReplyResponse 是账号级人工快捷回复的具名 HTTP 响应 DTO。
type chatQuickReplyResponse struct {
	// ID 是快捷回复稳定标识，用于删除操作。
	ID int64 `json:"id"`
	// AccountID 是快捷回复所属账号标识。
	AccountID string `json:"account_id"`
	// Content 是发送到聊天会话的文本模板。
	Content string `json:"content"`
	// CreatedAt 是快捷回复创建的 Unix 秒时间戳。
	CreatedAt int64 `json:"created_at"`
}

// chatQuickReplyListResponse 是快捷回复列表接口的具名响应 DTO。
type chatQuickReplyListResponse struct {
	// QuickReplies 保存当前账号可用的人工快捷回复。
	QuickReplies []chatQuickReplyResponse `json:"quick_replies"`
}

// chatQuickReplyCreateRequest 是创建账号级快捷回复的具名请求 DTO。
type chatQuickReplyCreateRequest struct {
	// AccountID 是当前用户拥有的目标账号标识。
	AccountID string `json:"account_id"`
	// Content 是待保存的人工快捷回复正文。
	Content string `json:"content"`
}

// chatBuyerNoteResponse 是按账号和买家 ID 隔离的完整备注响应 DTO。
type chatBuyerNoteResponse struct {
	// AccountID 是备注所属账号标识。
	AccountID string `json:"account_id"`
	// BuyerID 是备注所属的平台买家标识。
	BuyerID string `json:"buyer_id"`
	// Content 是完整备注正文；空字符串代表未保存备注。
	Content string `json:"content"`
	// UpdatedAt 是最近保存时间；空备注保持零值。
	UpdatedAt int64 `json:"updated_at"`
}

// chatBuyerNoteSaveRequest 是保存买家备注的具名请求 DTO。
type chatBuyerNoteSaveRequest struct {
	// AccountID 是当前用户拥有的目标账号标识。
	AccountID string `json:"account_id"`
	// Content 是完整备注正文，空字符串表示清除备注。
	Content string `json:"content"`
}

// listChatQuickReplies 查询当前账号可复用的人工快捷回复。
func (s *Server) listChatQuickReplies(w http.ResponseWriter, r *http.Request) {
	// session 保存认证中间件写入的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// accountID 保存去除空白后的目标账号标识。
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	// replies 和 listErr 保存应用服务返回的快捷回复及查询错误。
	replies, listErr := s.chatApplication().ListQuickReplies(r.Context(), session.UserID, accountID)
	if listErr != nil {
		writeChatMetadataError(w, listErr)
		return
	}
	writeJSON(w, http.StatusOK, newChatQuickReplyListResponse(replies))
}

// createChatQuickReply 校验请求后为账号保存一条人工快捷回复。
func (s *Server) createChatQuickReply(w http.ResponseWriter, r *http.Request) {
	// session 保存认证中间件写入的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// request 保存反序列化后的快捷回复创建参数。
	var request chatQuickReplyCreateRequest
	// decodeErr 保存快捷回复创建请求体反序列化失败原因。
	if decodeErr := decodeJSON(r, &request); decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// reply 和 createErr 保存创建后的快捷回复及应用层校验错误。
	reply, createErr := s.chatApplication().CreateQuickReply(r.Context(), session.UserID, request.AccountID, request.Content)
	if createErr != nil {
		writeChatMetadataError(w, createErr)
		return
	}
	writeJSON(w, http.StatusCreated, newChatQuickReplyResponse(reply))
}

// deleteChatQuickReply 删除当前账号下由路径标识的快捷回复。
func (s *Server) deleteChatQuickReply(w http.ResponseWriter, r *http.Request) {
	// session 保存认证中间件写入的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// accountID 保存查询参数提供的快捷回复所属账号标识。
	accountID := strings.TrimSpace(r.URL.Query().Get("account_id"))
	// quickReplyID 和 parseErr 保存路径快捷回复 ID 及解析错误。
	quickReplyID, parseErr := strconv.ParseInt(chi.URLParam(r, "quick_reply_id"), 10, 64)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, "快捷回复标识无效")
		return
	}
	// deleteErr 保存删除快捷回复时应用服务返回的业务或持久化错误。
	if deleteErr := s.chatApplication().DeleteQuickReply(r.Context(), session.UserID, accountID, quickReplyID); deleteErr != nil {
		writeChatMetadataError(w, deleteErr)
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// getChatBuyerNote 返回当前账号下指定买家的完整备注；未保存时仍返回空备注。
func (s *Server) getChatBuyerNote(w http.ResponseWriter, r *http.Request) {
	// session 保存认证中间件写入的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// accountID 和 buyerID 保存备注的账号与买家复合隔离键。
	accountID, buyerID := strings.TrimSpace(r.URL.Query().Get("account_id")), strings.TrimSpace(chi.URLParam(r, "buyer_id"))
	// note 和 readErr 保存应用层读取的备注及错误。
	note, readErr := s.chatApplication().GetBuyerNote(r.Context(), session.UserID, accountID, buyerID)
	if readErr != nil {
		writeChatMetadataError(w, readErr)
		return
	}
	writeJSON(w, http.StatusOK, newChatBuyerNoteResponse(note))
}

// saveChatBuyerNote 保存或清除当前账号下指定买家的备注。
func (s *Server) saveChatBuyerNote(w http.ResponseWriter, r *http.Request) {
	// session 保存认证中间件写入的当前用户身份。
	session := auth.SessionFromContext(r.Context())
	// buyerID 保存路径提供的稳定平台买家标识。
	buyerID := strings.TrimSpace(chi.URLParam(r, "buyer_id"))
	// request 保存反序列化后的备注保存参数。
	var request chatBuyerNoteSaveRequest
	// decodeErr 保存买家备注保存请求体反序列化失败原因。
	if decodeErr := decodeJSON(r, &request); decodeErr != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// note 和 saveErr 保存更新后的备注及应用层校验错误。
	note, saveErr := s.chatApplication().SaveBuyerNote(r.Context(), session.UserID, request.AccountID, buyerID, request.Content)
	if saveErr != nil {
		writeChatMetadataError(w, saveErr)
		return
	}
	writeJSON(w, http.StatusOK, newChatBuyerNoteResponse(note))
}

// newChatQuickReplyResponse 将应用层快捷回复转换为稳定 HTTP DTO。
func newChatQuickReplyResponse(reply chatapp.QuickReply) chatQuickReplyResponse {
	return chatQuickReplyResponse{ID: reply.ID, AccountID: reply.AccountID, Content: reply.Content, CreatedAt: reply.CreatedAt}
}

// newChatQuickReplyListResponse 批量转换快捷回复，避免传输层序列化应用层模型。
func newChatQuickReplyListResponse(replies []chatapp.QuickReply) chatQuickReplyListResponse {
	// response 保存将要返回给前端的具名列表 DTO。
	response := chatQuickReplyListResponse{QuickReplies: make([]chatQuickReplyResponse, 0, len(replies))}
	// reply 表示当前待转换的应用层快捷回复。
	for _, reply := range replies {
		response.QuickReplies = append(response.QuickReplies, newChatQuickReplyResponse(reply))
	}
	return response
}

// newChatBuyerNoteResponse 将应用层买家备注转换为稳定 HTTP DTO。
func newChatBuyerNoteResponse(note chatapp.BuyerNote) chatBuyerNoteResponse {
	return chatBuyerNoteResponse{AccountID: note.AccountID, BuyerID: note.BuyerID, Content: note.Content, UpdatedAt: note.UpdatedAt}
}

// writeChatMetadataError 将聊天元数据用例错误映射到统一 HTTP 状态和错误 envelope。
func writeChatMetadataError(w http.ResponseWriter, operationErr error) {
	switch {
	case errors.Is(operationErr, chatapp.ErrInvalidInput):
		writeErr(w, http.StatusBadRequest, "账号、买家或内容无效")
	case errors.Is(operationErr, chatapp.ErrMetadataForbidden):
		writeErr(w, http.StatusForbidden, "无权访问该账号")
	case errors.Is(operationErr, chatapp.ErrQuickReplyLimitReached):
		writeErrCode(w, http.StatusConflict, "chat_quick_reply_limit_reached", "快捷回复最多保存 50 条", "")
	case errors.Is(operationErr, chatapp.ErrQuickReplyNotFound):
		writeErr(w, http.StatusNotFound, "快捷回复不存在")
	default:
		writeErr(w, http.StatusInternalServerError, "聊天数据操作失败")
	}
}
