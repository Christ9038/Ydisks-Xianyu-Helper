package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/auth"
)

// chatItemDTO 是聊天商品选择器的非敏感对外商品模型。
type chatItemDTO struct {
	// ItemID 是闲鱼商品标识。
	ItemID string `json:"item_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// ImageURL 是商品主图地址。
	ImageURL string `json:"image_url"`
	// Price 是不含货币符号的平台价格文本。
	Price string `json:"price"`
	// Description 是列表可选摘要，不参与卡片发送。
	Description string `json:"description,omitempty"`
}

// chatItemPageResponse 是聊天商品的分页 HTTP 响应。
type chatItemPageResponse struct {
	// Items 是当前页商品。
	Items []chatItemDTO `json:"items"`
	// Page 是从 1 开始的页码。
	Page int `json:"page"`
	// HasMore 表示是否存在下一页。
	HasMore bool `json:"has_more"`
}

// chatItemCardSnapshotDTO 是发送接口接受的最小商品快照，不包含列表专用摘要。
type chatItemCardSnapshotDTO struct {
	// ItemID 是闲鱼商品标识。
	ItemID string `json:"item_id"`
	// Title 是发送时保存的商品标题。
	Title string `json:"title"`
	// ImageURL 是发送时保存的商品主图地址。
	ImageURL string `json:"image_url"`
	// Price 是不含货币符号的价格文本。
	Price string `json:"price"`
}

// sendChatItemCardRequest 是商品卡片发送的具名 HTTP 请求。
type sendChatItemCardRequest struct {
	// AccountID 是执行发送的账号标识。
	AccountID string `json:"account_id"`
	// ChatID 是目标个人会话标识。
	ChatID string `json:"chat_id"`
	// Item 是列表中选中的商品快照。
	Item chatItemCardSnapshotDTO `json:"item"`
}

// chatItemPort 是商品选择与发送 handler 消费的可选聊天应用能力。
type chatItemPort interface {
	// ItemCatalogAvailable 报告商品查询端口是否已装配。
	ItemCatalogAvailable() bool
	// ItemSendingAvailable 报告商品卡片发送端口是否已装配。
	ItemSendingAvailable() bool
	// ListChatItems 查询当前用户有权访问的个人会话商品。
	ListChatItems(context.Context, chatapp.ChatItemQuery) (chatapp.ChatItemPage, error)
	// SendItemCard 发送商品卡片并返回本地出站消息。
	SendItemCard(context.Context, chatapp.ItemCardInput) (*chatapp.Message, error)
}

// listChatItems 查询当前用户拥有账号的个人会话商品。
func (s *Server) listChatItems(w http.ResponseWriter, r *http.Request) {
	// session 是认证中间件注入的当前管理用户会话。
	session := auth.SessionFromContext(r.Context())
	// accountID 和 chatID 是去除首尾空白的会话定位参数。
	accountID, chatID := strings.TrimSpace(r.URL.Query().Get("account_id")), strings.TrimSpace(r.URL.Query().Get("chat_id"))
	// role 和 queryWord 是去除首尾空白的商品归属及搜索参数。
	role, queryWord := strings.TrimSpace(r.URL.Query().Get("role")), strings.TrimSpace(r.URL.Query().Get("query"))
	if accountID == "" || chatID == "" || (role != "peer" && role != "self") || len([]rune(queryWord)) > 100 {
		writeErrCode(w, http.StatusBadRequest, "chat_item_invalid", "商品查询参数无效", "")
		return
	}
	// page 是默认为 1 的目标页码。
	page := 1
	// rawPage 是可选的原始页码查询参数。
	if rawPage := strings.TrimSpace(r.URL.Query().Get("page")); rawPage != "" {
		// parsedPage 和 parseErr 是显式页码的解析结果。
		parsedPage, parseErr := strconv.Atoi(rawPage)
		if parseErr != nil || parsedPage < 1 {
			writeErrCode(w, http.StatusBadRequest, "chat_item_invalid", "商品页码无效", "")
			return
		}
		page = parsedPage
	}
	// itemApplication 和 supported 是当前聊天应用是否提供商品选择扩展。
	itemApplication, supported := s.chatApplication().(chatItemPort)
	if !supported || !itemApplication.ItemCatalogAvailable() {
		writeErrCode(w, http.StatusServiceUnavailable, "chat_item_catalog_unavailable", "聊天商品服务未启用", "")
		return
	}
	// result 和 listErr 是应用层归属校验后的商品分页结果及错误。
	result, listErr := itemApplication.ListChatItems(r.Context(), chatapp.ChatItemQuery{UserID: session.UserID, AccountID: accountID, ChatID: chatID, Role: role, Query: queryWord, Page: page})
	if listErr != nil {
		s.writeChatItemError(w, r, accountID, "list", listErr, nil)
		return
	}
	// items 是应用商品转换后的 HTTP DTO 集合。
	items := make([]chatItemDTO, 0, len(result.Items))
	for _ /* item 是当前待转换为 HTTP DTO 的应用层商品。 */, item := range result.Items {
		items = append(items, chatItemDTO{ItemID: item.ItemID, Title: item.Title, ImageURL: item.ImageURL, Price: item.Price, Description: item.Description})
	}
	writeJSON(w, http.StatusOK, chatItemPageResponse{Items: items, Page: result.Page, HasMore: result.HasMore})
}

// sendChatItemCard 将已校验的商品快照发送到应用层解析的对端会话。
func (s *Server) sendChatItemCard(w http.ResponseWriter, r *http.Request) {
	// request 是本次商品卡片的具名 JSON 请求。
	var request sendChatItemCardRequest
	// decodeErr 表示请求正文是否符合 JSON 解码约束。
	if decodeErr := decodeJSON(r, &request); decodeErr != nil {
		writeErrCode(w, http.StatusBadRequest, "chat_item_invalid", "请求格式错误", "")
		return
	}
	// itemApplication 和 supported 是当前聊天应用是否提供商品卡片发送扩展。
	itemApplication, supported := s.chatApplication().(chatItemPort)
	if !supported || !itemApplication.ItemSendingAvailable() {
		writeErrCode(w, http.StatusServiceUnavailable, "chat_item_sending_unavailable", "聊天商品发送服务未启用", "")
		return
	}
	// session 是认证中间件注入的当前管理用户会话。
	session := auth.SessionFromContext(r.Context())
	// sent 和 sendErr 是应用层商品卡片发送结果及错误。
	sent, sendErr := itemApplication.SendItemCard(r.Context(), chatapp.ItemCardInput{UserID: session.UserID, AccountID: request.AccountID, ChatID: request.ChatID,
		Item: chatapp.ChatItem{ItemID: request.Item.ItemID, Title: request.Item.Title, ImageURL: request.Item.ImageURL, Price: request.Item.Price}})
	if sendErr != nil {
		s.writeChatItemError(w, r, request.AccountID, "send", sendErr, sent)
		return
	}
	writeJSON(w, http.StatusCreated, chatMessageEnvelope{Message: newChatMessageDTOFromApplication(sent)})
}

// writeChatItemError 将商品查询与发送应用错误映射为稳定的 HTTP 错误契约。
func (s *Server) writeChatItemError(w http.ResponseWriter, r *http.Request, accountID, operation string, err error, outgoing *chatapp.Message) {
	logChatFailure(r.Context(), "item_card_"+operation, accountID, "", err)
	switch {
	case errors.Is(err, chatapp.ErrChatItemInvalid), errors.Is(err, chatapp.ErrSendInvalidInput):
		writeErrCode(w, http.StatusBadRequest, "chat_item_invalid", "商品卡片参数无效", "")
	case errors.Is(err, chatapp.ErrChatItemForbidden):
		writeErrCode(w, http.StatusForbidden, "chat_item_forbidden", "无权操作该账号", "")
	case errors.Is(err, chatapp.ErrChatSessionNotFound):
		writeErrCode(w, http.StatusNotFound, "chat_session_not_found", "聊天会话不存在", "")
	case errors.Is(err, chatapp.ErrOffline):
		writeErrCode(w, http.StatusConflict, "chat_account_offline", "账号当前离线，无法发送商品", "")
	case errors.Is(err, chatapp.ErrSendUncertain):
		writeErrDetails(w, http.StatusBadGateway, "chat_item_card_send_uncertain", "商品卡片发送结果待确认，请先到闲鱼核对，避免重复发送", "", map[string]any{"outgoing_message": newChatMessageDTOFromApplication(outgoing)})
	case errors.Is(err, chatapp.ErrUnavailable), errors.Is(err, chatapp.ErrChatItemUnavailable), errors.Is(err, chatapp.ErrSessionUnavailable):
		writeErrCode(w, http.StatusServiceUnavailable, "chat_item_service_unavailable", "聊天商品服务未启用", "")
	case errors.Is(err, chatapp.ErrStatusSave):
		writeChatStatusSaveError(w, outgoing)
	case errors.Is(err, chatapp.ErrChatItemCreate):
		writeErrCode(w, http.StatusInternalServerError, "chat_item_message_save_failed", "保存待发送商品卡片失败", "")
	case errors.Is(err, chatapp.ErrSend):
		writeErrDetails(w, http.StatusBadGateway, "chat_item_card_send_failed", "发送商品卡片失败，请重试", "", map[string]any{"outgoing_message": newChatMessageDTOFromApplication(outgoing)})
	default:
		if operation == "send" {
			writeErrCode(w, http.StatusBadGateway, "chat_item_card_send_failed", "发送商品卡片失败，请重试", "")
			return
		}
		// recovered 表示现有凭证恢复协调器已接管平台会话错误。
		recovered := s.recoverExpiredSession(r.Context(), strings.TrimSpace(accountID), err)
		_ = recovered
		writeErrCode(w, http.StatusBadGateway, "chat_item_list_failed", "获取闲鱼商品失败，请稍后重试", "")
	}
}
