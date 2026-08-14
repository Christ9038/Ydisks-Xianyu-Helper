package server

import "xianyu-go/internal/db"

// operationResponse 是密码、会话和设置变更接口共用的操作结果 DTO。
type operationResponse struct {
	// Success 表示操作是否完成。
	Success bool `json:"success"`
	// Message 是可以直接展示给用户的操作结果说明。
	Message string `json:"message,omitempty"`
	// RequiresRelogin 表示操作完成后当前会话已被撤销，需要重新登录。
	RequiresRelogin bool `json:"requires_relogin,omitempty"`
}

// messageResponse 是只返回提示文本的简单成功响应 DTO。
type messageResponse struct {
	// Message 是接口操作结果说明。
	Message string `json:"message"`
}

// sessionVerificationResponse 是会话校验接口的具名响应 DTO。
type sessionVerificationResponse struct {
	// Authenticated 表示当前请求是否带有有效会话。
	Authenticated bool `json:"authenticated"`
	// Initialized 表示系统是否已经完成管理员初始化。
	Initialized bool `json:"initialized"`
	// UserID 是当前会话用户 ID；未认证时为空值。
	UserID int64 `json:"user_id,omitempty"`
	// Username 是当前会话用户名；未认证时为空字符串。
	Username string `json:"username,omitempty"`
	// IsAdmin 表示当前会话用户是否为管理员。
	IsAdmin bool `json:"is_admin"`
}

// accountMutationResponse 是账号新增等简单变更接口的具名成功响应 DTO。
type accountMutationResponse struct {
	// Success 表示账号变更是否完成。
	Success bool `json:"success"`
	// ID 是新增或更新账号的稳定标识。
	ID string `json:"id,omitempty"`
}

// chatSessionDTO 是聊天会话对外暴露的非敏感 DTO，不直接复用数据库模型。
type chatSessionDTO struct {
	// AccountID 是账号稳定标识。
	AccountID string `json:"account_id"`
	// ChatID 是平台聊天会话标识。
	ChatID string `json:"chat_id"`
	// BuyerID 是买家平台标识。
	BuyerID string `json:"buyer_id"`
	// BuyerName 是买家昵称。
	BuyerName string `json:"buyer_name"`
	// BuyerAvatar 是买家头像地址。
	BuyerAvatar string `json:"buyer_avatar_url"`
	// ItemID 是会话关联商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是会话关联商品标题。
	ItemTitle string `json:"item_title"`
	// LastMessage 是最近一条消息摘要。
	LastMessage string `json:"last_message"`
	// LastMessageAt 是最近消息时间的 Unix 秒。
	LastMessageAt int64 `json:"last_message_at"`
	// UnreadCount 是当前会话未读消息数量。
	UnreadCount int `json:"unread_count"`
}

// newChatSessionDTO 将数据库聊天会话转换为 API DTO。
func newChatSessionDTO(session db.ChatSession) chatSessionDTO {
	return chatSessionDTO{
		AccountID: session.CookieID, ChatID: session.ChatID, BuyerID: session.BuyerID,
		BuyerName: session.BuyerName, BuyerAvatar: session.BuyerAvatar, ItemID: session.ItemID,
		ItemTitle: session.ItemTitle, LastMessage: session.LastMessage,
		LastMessageAt: session.LastMessageAt, UnreadCount: session.UnreadCount,
	}
}

// newChatSessionDTOs 批量转换聊天会话，保持接口响应不直接暴露数据库模型。
func newChatSessionDTOs(sessions []db.ChatSession) []chatSessionDTO {
	// result 是转换后的聊天会话 DTO 列表。
	result := make([]chatSessionDTO, 0, len(sessions))
	// session 是当前待转换的数据库聊天会话。
	for _, session := range sessions {
		result = append(result, newChatSessionDTO(session))
	}
	return result
}

// chatMessageDTO 是聊天消息对外暴露的具名 DTO，不直接复用数据库模型。
type chatMessageDTO struct {
	// ID 是本地消息主键。
	ID int64 `json:"id"`
	// AccountID 是账号稳定标识。
	AccountID string `json:"account_id"`
	// ChatID 是平台聊天会话标识。
	ChatID string `json:"chat_id"`
	// MessageKey 是消息幂等键。
	MessageKey string `json:"message_key"`
	// Direction 是消息方向。
	Direction string `json:"direction"`
	// SenderID 是消息发送者平台标识。
	SenderID string `json:"sender_id"`
	// SenderName 是消息发送者名称。
	SenderName string `json:"sender_name"`
	// MessageType 是消息类型。
	MessageType string `json:"message_type"`
	// Content 是消息文本或媒体地址。
	Content string `json:"content"`
	// Status 是消息投递状态。
	Status string `json:"status"`
	// SentAt 是消息发送时间的 Unix 秒。
	SentAt int64 `json:"sent_at"`
}

// newChatMessageDTO 将数据库聊天消息转换为 API DTO。
func newChatMessageDTO(message db.ChatMessage) chatMessageDTO {
	return chatMessageDTO{
		ID: message.ID, AccountID: message.CookieID, ChatID: message.ChatID,
		MessageKey: message.MessageKey, Direction: message.Direction,
		SenderID: message.SenderID, SenderName: message.SenderName,
		MessageType: message.MessageType, Content: message.Content,
		Status: message.Status, SentAt: message.SentAt,
	}
}

// newChatMessageDTOFromPointer 转换可为空的聊天消息指针，避免成功响应因空值 panic。
func newChatMessageDTOFromPointer(message *db.ChatMessage) chatMessageDTO {
	if message == nil {
		return chatMessageDTO{}
	}
	return newChatMessageDTO(*message)
}

// newChatMessageDTOs 批量转换聊天消息，保持接口响应与数据库模型解耦。
func newChatMessageDTOs(messages []db.ChatMessage) []chatMessageDTO {
	// result 是转换后的聊天消息 DTO 列表。
	result := make([]chatMessageDTO, 0, len(messages))
	// message 是当前待转换的数据库聊天消息。
	for _, message := range messages {
		result = append(result, newChatMessageDTO(message))
	}
	return result
}

// chatSessionPageResponse 是聊天会话分页接口的具名响应 DTO。
type chatSessionPageResponse struct {
	// Sessions 是当前页聊天会话。
	Sessions []chatSessionDTO `json:"sessions"`
	// HasMore 表示是否还有下一页。
	HasMore bool `json:"has_more"`
	// NextCursor 是下一页游标。
	NextCursor int64 `json:"next_cursor,omitempty"`
}

// chatMessageEnvelope 是发送聊天消息接口的具名响应 DTO。
type chatMessageEnvelope struct {
	// Message 是已经写入本地队列的消息。
	Message chatMessageDTO `json:"message"`
}

// chatMessagePageResponse 是聊天消息分页接口的具名响应 DTO。
type chatMessagePageResponse struct {
	// Messages 是当前页聊天消息。
	Messages []chatMessageDTO `json:"messages"`
	// HasMore 表示是否还有更多历史消息。
	HasMore bool `json:"has_more"`
	// NextCursor 是下一页游标。
	NextCursor int64 `json:"next_cursor,omitempty"`
	// Session 是当前聊天会话摘要。
	Session chatSessionDTO `json:"session"`
}

// orderDTO 是订单列表和详情接口共用的具名响应 DTO。
type orderDTO struct {
	// OrderID 是平台订单标识。
	OrderID string `json:"order_id"`
	// ItemID 是关联商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是关联商品标题。
	ItemTitle string `json:"item_title"`
	// ItemImage 是关联商品图片地址。
	ItemImage string `json:"item_image"`
	// BuyerID 是买家平台标识。
	BuyerID string `json:"buyer_id"`
	// SpecName 是商品规格名称。
	SpecName string `json:"spec_name"`
	// SpecValue 是商品规格值。
	SpecValue string `json:"spec_value"`
	// Quantity 是购买数量。
	Quantity string `json:"quantity"`
	// Amount 是订单实付金额。
	Amount string `json:"amount"`
	// OrderStatus 是归一化后的订单状态。
	OrderStatus string `json:"order_status"`
	// Status 是兼容前端使用的订单状态别名。
	Status string `json:"status"`
	// CookieID 是订单所属账号标识。
	CookieID string `json:"cookie_id"`
	// IsBargain 表示订单是否来自议价。
	IsBargain int `json:"is_bargain"`
	// SystemShipped 表示是否由系统完成发货。
	SystemShipped bool `json:"system_shipped"`
	// ReceiverName 是收货人姓名。
	ReceiverName string `json:"receiver_name"`
	// ReceiverPhone 是收货人电话。
	ReceiverPhone string `json:"receiver_phone"`
	// ReceiverAddress 是收货地址。
	ReceiverAddress string `json:"receiver_address"`
	// ReceiverCity 是收货城市。
	ReceiverCity string `json:"receiver_city"`
	// CreatedAt 是订单创建时间。
	CreatedAt string `json:"created_at"`
	// UpdatedAt 是订单更新时间。
	UpdatedAt string `json:"updated_at"`
}

// orderListResponse 是订单分页接口的具名响应 DTO。
type orderListResponse struct {
	// Success 表示查询是否完成。
	Success bool `json:"success"`
	// Data 是当前页订单。
	Data []orderDTO `json:"data"`
	// Total 是符合筛选条件的订单总数。
	Total int `json:"total"`
	// Page 是当前页码。
	Page int `json:"page"`
	// PageSize 是当前页大小。
	PageSize int `json:"page_size"`
	// TotalPages 是总页数。
	TotalPages int `json:"total_pages"`
}
