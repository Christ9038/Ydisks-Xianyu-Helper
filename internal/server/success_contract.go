package server

import (
	chatapp "xianyu-go/internal/application/chat"
)

// operationResponse 是密码、会话和设置变更接口共用的操作结果 DTO。
type operationResponse struct {
	// Success 表示操作是否完成。
	Success bool `json:"success"`
	// Message 是可以直接展示给用户的操作结果说明。
	Message string `json:"message,omitempty"`
	// RequiresRelogin 表示操作完成后当前会话已被撤销，需要重新登录。
	RequiresRelogin bool `json:"requires_relogin,omitempty"`
}

// longLoginResponse 是长登录设置接口的具名响应 DTO，不包含平台 Cookie。
type longLoginResponse struct {
	// CanOpenLongLogin 表示平台是否允许开启长登录。
	CanOpenLongLogin bool `json:"can_open_long_login"`
	// Enabled 表示平台当前是否已开启长登录。
	Enabled bool `json:"enabled"`
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

// mutationIDResponse 是使用数值主键的资源变更接口成功响应 DTO。
type mutationIDResponse struct {
	// Success 表示资源变更是否完成。
	Success bool `json:"success"`
	// ID 是新建资源的数值主键。
	ID int64 `json:"id"`
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
	// ItemImageURL 是会话关联商品主图的公开地址。
	ItemImageURL string `json:"item_image_url"`
	// LastMessage 是最近一条消息摘要。
	LastMessage string `json:"last_message"`
	// LastMessageAt 是最近消息时间的 Unix 秒。
	LastMessageAt int64 `json:"last_message_at"`
	// UnreadCount 是当前会话未读消息数量。
	UnreadCount int `json:"unread_count"`
}

// newChatSessionDTOFromApplication 将应用层聊天会话转换为 HTTP DTO。
func newChatSessionDTOFromApplication(session chatapp.Session) chatSessionDTO {
	return chatSessionDTO{
		AccountID: session.AccountID, ChatID: session.ChatID, BuyerID: session.BuyerID,
		BuyerName: session.BuyerName, BuyerAvatar: session.BuyerAvatar, ItemID: session.ItemID,
		ItemTitle: session.ItemTitle, ItemImageURL: session.ItemImageURL, LastMessage: session.LastMessage,
		LastMessageAt: session.LastMessageAt, UnreadCount: session.UnreadCount,
	}
}

// newChatSessionDTOsFromApplication 批量转换应用层聊天会话，保持响应不暴露数据库模型。
func newChatSessionDTOsFromApplication(sessions []chatapp.Session) []chatSessionDTO {
	// result 是转换后的聊天会话 DTO 列表。
	result := make([]chatSessionDTO, 0, len(sessions))
	// session 表示当前待转换的应用层会话。
	for _, session := range sessions {
		result = append(result, newChatSessionDTOFromApplication(session))
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
	// MediaDuration 是语音消息的秒级时长；零值表示平台未提供。
	MediaDuration int64 `json:"media_duration"`
	// Status 是消息投递状态。
	Status string `json:"status"`
	// ReadStatus 是平台已读回执状态；值为 2 时表示对方已读。
	ReadStatus int `json:"read_status"`
	// ReadAt 是平台确认已读的 Unix 毫秒时间戳；零值表示尚未确认。
	ReadAt int64 `json:"read_at"`
	// SentAt 是消息发送时间的 Unix 秒。
	SentAt int64 `json:"sent_at"`
}

// newChatMessageDTOFromApplication 将聊天应用层发送结果转换为 HTTP DTO，避免响应引用数据库模型。
func newChatMessageDTOFromApplication(message *chatapp.Message) chatMessageDTO {
	if message == nil {
		return chatMessageDTO{}
	}
	return chatMessageDTO{
		ID: message.ID, AccountID: message.AccountID, ChatID: message.ChatID,
		MessageKey: message.MessageKey, Direction: message.Direction,
		SenderID: message.SenderID, SenderName: message.SenderName,
		MessageType: message.MessageType, Content: message.Content, MediaDuration: message.MediaDuration,
		Status: message.Status, ReadStatus: message.ReadStatus, ReadAt: message.ReadAt, SentAt: message.SentAt,
	}
}

// newChatMessageDTOsFromApplication 将聊天应用层消息转换为 HTTP DTO，避免响应暴露数据库模型。
func newChatMessageDTOsFromApplication(messages []chatapp.Message) []chatMessageDTO {
	// result 是转换后的聊天消息 DTO 列表。
	result := make([]chatMessageDTO, 0, len(messages))
	// message 保存当前待转换的应用层消息。
	for _, message := range messages {
		result = append(result, chatMessageDTO{
			ID: message.ID, AccountID: message.AccountID, ChatID: message.ChatID,
			MessageKey: message.MessageKey, Direction: message.Direction, SenderID: message.SenderID,
			SenderName: message.SenderName, MessageType: message.MessageType, Content: message.Content, MediaDuration: message.MediaDuration,
			Status: message.Status, ReadStatus: message.ReadStatus, ReadAt: message.ReadAt, SentAt: message.SentAt,
		})
	}
	return result
}

// chatEventDTO 是聊天实时推送的具名传输契约，确保 WebSocket 与 HTTP 使用相同的 snake_case 消息字段。
type chatEventDTO struct {
	// Type 是实时事件类别，例如 message.created。
	Type string `json:"type"`
	// Message 是本次事件关联的非敏感聊天消息；非消息事件可以为空。
	Message *chatMessageDTO `json:"message,omitempty"`
	// Session 是本次事件关联的非敏感会话摘要；无需更新会话时可以为空。
	Session *chatSessionDTO `json:"session,omitempty"`
}

// newChatEventDTOFromApplication 将应用层实时事件转换为浏览器稳定使用的 WebSocket DTO。
func newChatEventDTOFromApplication(event chatapp.Event) chatEventDTO {
	// result 保存转换后的实时事件；指针字段只在应用事件提供对应实体时设置。
	result := chatEventDTO{Type: event.Type}
	if event.Message != nil {
		// message 保存避免 WebSocket 直接序列化应用层 PascalCase 字段的消息 DTO。
		message := newChatMessageDTOFromApplication(event.Message)
		result.Message = &message
	}
	if event.Session != nil {
		// session 保存与 HTTP 会话接口一致的 snake_case 会话 DTO。
		session := newChatSessionDTOFromApplication(*event.Session)
		result.Session = &session
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

// cookieDetailResponse 是单个账号非敏感详情接口的具名响应 DTO。
type cookieDetailResponse struct {
	// ID 是账号稳定标识。
	ID string `json:"id"`
	// Enabled 表示账号是否允许运行。
	Enabled bool `json:"enabled"`
	// AutoConfirm 表示是否自动确认订单。
	AutoConfirm bool `json:"auto_confirm"`
	// Remark 是账号备注。
	Remark string `json:"remark"`
	// PauseDuration 是暂停时长，单位为分钟。
	PauseDuration int `json:"pause_duration"`
	// PausedUntil 是暂停结束时间的 Unix 秒。
	PausedUntil int64 `json:"paused_until"`
	// Paused 表示账号当前是否处于暂停状态。
	Paused bool `json:"paused"`
	// ShowBrowser 表示密码登录是否允许显示浏览器。
	ShowBrowser bool `json:"show_browser"`
	// Username 是登录用户名，不包含登录密码。
	Username string `json:"username"`
	// Nickname 是平台账号昵称缓存。
	Nickname string `json:"nickname"`
	// AvatarURL 是平台账号头像地址。
	AvatarURL string `json:"avatar_url"`
	// LoginMethod 是最近一次成功登录方式。
	LoginMethod string `json:"login_method"`
	// LastLoginAt 是最近一次成功登录时间。
	LastLoginAt int64 `json:"last_login_at"`
	// ProfileError 是资料刷新错误说明。
	ProfileError string `json:"profile_error"`
	// HasCookie 表示数据库中存在可用账号记录。
	HasCookie bool `json:"has_cookie"`
	// AutoRateEnabled 表示自动评价计划是否启用。
	AutoRateEnabled bool `json:"auto_rate_enabled"`
	// RateContent 是自动评价文案。
	RateContent string `json:"rate_content"`
	// AutoPolishEnabled 表示自动擦亮计划是否启用。
	AutoPolishEnabled bool `json:"auto_polish_enabled"`
	// PolishTime 是自动擦亮的本地时间。
	PolishTime string `json:"polish_time"`
	// LastRateScanAt 是最近一次自动评价扫描时间。
	LastRateScanAt int64 `json:"last_rate_scan_at"`
	// LastPolishDate 是最近一次自动擦亮日期。
	LastPolishDate string `json:"last_polish_date"`
	// LastPolishAt 是最近一次自动擦亮时间。
	LastPolishAt int64 `json:"last_polish_at"`
}

// cookieSettingsResponse 是账号设置更新接口的具名成功响应 DTO。
type cookieSettingsResponse struct {
	// Success 表示账号设置是否保存成功。
	Success bool `json:"success"`
	// PausedUntil 是新的暂停结束时间 Unix 秒。
	PausedUntil int64 `json:"paused_until"`
	// Paused 表示账号当前是否处于暂停状态。
	Paused bool `json:"paused"`
}

// cookieProfileResponse 是账号资料刷新接口的具名响应 DTO。
type cookieProfileResponse struct {
	// Success 表示资料刷新是否成功。
	Success bool `json:"success"`
	// ID 是账号稳定标识。
	ID string `json:"id"`
	// Nickname 是刷新后的平台昵称。
	Nickname string `json:"nickname"`
	// AvatarURL 是刷新后的头像地址。
	AvatarURL string `json:"avatar_url"`
	// ProfileError 是资料刷新失败原因。
	ProfileError string `json:"profile_error"`
}

// autoConfirmResponse 是账号自动确认设置查询接口的具名响应 DTO。
type autoConfirmResponse struct {
	// AutoConfirm 表示是否自动确认订单。
	AutoConfirm bool `json:"auto_confirm"`
}

// pauseDurationResponse 是账号暂停时长查询接口的具名响应 DTO。
type pauseDurationResponse struct {
	// PauseDuration 是暂停时长，单位为分钟。
	PauseDuration int `json:"pause_duration"`
	// PausedUntil 是暂停结束时间的 Unix 秒。
	PausedUntil int64 `json:"paused_until"`
	// Paused 表示账号当前是否处于暂停状态。
	Paused bool `json:"paused"`
}

// itemListResponse 是本地商品列表接口的具名商品 DTO。
type itemListResponse struct {
	// ID 是本地商品记录主键。
	ID int64 `json:"id"`
	// CookieID 是商品所属账号标识。
	CookieID string `json:"cookie_id"`
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是商品标题。
	ItemTitle string `json:"item_title"`
	// ItemDescription 是商品描述。
	ItemDescription string `json:"item_description"`
	// ItemCategory 是商品分类标识。
	ItemCategory string `json:"item_category"`
	// ItemPrice 是商品价格文本。
	ItemPrice string `json:"item_price"`
	// ItemDetail 是商品详情原始 JSON。
	ItemDetail string `json:"item_detail"`
	// ItemImage 是从详情中提取的商品图片地址。
	ItemImage string `json:"item_image"`
	// IsMultiSpec 表示商品是否有多规格。
	IsMultiSpec bool `json:"is_multi_spec"`
	// MultiQuantityDelivery 表示是否按数量发货。
	MultiQuantityDelivery bool `json:"multi_quantity_delivery"`
	// IsMultiQtyShip 是按数量发货的兼容字段。
	IsMultiQtyShip bool `json:"is_multi_qty_ship"`
}

// itemDetailResponse 是单个本地商品详情接口的具名响应 DTO。
type itemDetailResponse struct {
	// CookieID 是商品所属账号标识。
	CookieID string `json:"cookie_id"`
	// ItemID 是平台商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是商品标题。
	ItemTitle string `json:"item_title"`
	// ItemDescription 是商品描述。
	ItemDescription string `json:"item_description"`
	// ItemCategory 是商品分类标识。
	ItemCategory string `json:"item_category"`
	// ItemPrice 是商品价格文本。
	ItemPrice string `json:"item_price"`
	// ItemDetail 是商品详情原始 JSON。
	ItemDetail string `json:"item_detail"`
	// IsMultiSpec 表示商品是否有多规格。
	IsMultiSpec bool `json:"is_multi_spec"`
	// MultiQuantityDelivery 表示是否按数量发货。
	MultiQuantityDelivery bool `json:"multi_quantity_delivery"`
}

// itemPublishResponse 是单个商品发布成功接口的具名响应 DTO。
type itemPublishResponse struct {
	// Success 表示商品是否发布成功。
	Success bool `json:"success"`
	// Message 是发布结果说明。
	Message string `json:"message"`
	// ItemID 是新商品的平台标识。
	ItemID string `json:"item_id"`
	// ItemURL 是新商品的平台详情地址。
	ItemURL string `json:"item_url"`
	// ItemImage 是新商品主图地址。
	ItemImage string `json:"item_image"`
	// ItemTitle 是新商品标题。
	ItemTitle string `json:"item_title"`
	// ItemPrice 是新商品价格文本。
	ItemPrice string `json:"item_price"`
	// Quantity 是新商品库存数量。
	Quantity int `json:"quantity"`
	// CategoryID 是新商品分类标识。
	CategoryID string `json:"category_id"`
	// CategoryName 是新商品分类名称。
	CategoryName string `json:"category_name"`
}

// itemSyncResponse 是商品全集同步接口的具名响应 DTO。
type itemSyncResponse struct {
	// Success 表示同步是否完成。
	Success bool `json:"success"`
	// Message 是同步结果说明。
	Message string `json:"message"`
	// TotalCount 是平台返回的商品总数。
	TotalCount int `json:"total_count"`
	// TotalPages 是平台商品总页数。
	TotalPages int `json:"total_pages"`
	// SavedCount 是本地保存的商品数量。
	SavedCount int `json:"saved_count"`
	// DeletedCount 是本地删除标记的商品数量。
	DeletedCount int `json:"deleted_count"`
}

// itemPageSyncResponse 是商品分页同步接口的具名响应 DTO。
type itemPageSyncResponse struct {
	// Success 表示同步是否完成。
	Success bool `json:"success"`
	// Message 是同步结果说明。
	Message string `json:"message"`
	// PageNumber 是当前同步页码。
	PageNumber int `json:"page_number"`
	// PageSize 是当前同步页大小。
	PageSize int `json:"page_size"`
	// CurrentCount 是当前页商品数量。
	CurrentCount int `json:"current_count"`
	// SavedCount 是本地保存的商品数量。
	SavedCount int `json:"saved_count"`
}

// automationActionResponse 是自动化规则动作的具名响应 DTO。
type automationActionResponse struct {
	// ID 是动作稳定标识。
	ID int64 `json:"id"`
	// ActionType 是动作类型。
	ActionType string `json:"action_type"`
	// CardID 是动作关联卡券组标识。
	CardID int64 `json:"card_id"`
	// CardName 是动作关联卡券组名称。
	CardName string `json:"card_name"`
	// DeliveryCount 是动作发送数量。
	DeliveryCount int `json:"delivery_count"`
	// MessageTemplate 是动作消息模板。
	MessageTemplate string `json:"message_template"`
	// DelaySeconds 是动作延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
	// ConfigJSON 是动作扩展配置 JSON。
	ConfigJSON string `json:"config_json"`
	// Enabled 表示动作是否启用。
	Enabled bool `json:"enabled"`
	// SortOrder 是动作执行顺序。
	SortOrder int `json:"sort_order"`
}

// automationRuleResponse 是自动化规则的具名响应 DTO。
type automationRuleResponse struct {
	// ID 是规则稳定标识。
	ID int64 `json:"id"`
	// CookieID 是规则所属账号标识。
	CookieID string `json:"cookie_id"`
	// ItemID 是规则关联商品标识。
	ItemID string `json:"item_id"`
	// ItemTitle 是规则关联商品标题。
	ItemTitle string `json:"item_title"`
	// Name 是规则名称。
	Name string `json:"name"`
	// TriggerType 是规则触发类型。
	TriggerType string `json:"trigger_type"`
	// Enabled 表示规则是否启用。
	Enabled bool `json:"enabled"`
	// Priority 是规则优先级。
	Priority int `json:"priority"`
	// ConfigJSON 是规则扩展配置 JSON。
	ConfigJSON string `json:"config_json"`
	// Actions 是规则动作列表。
	Actions []automationActionResponse `json:"actions"`
	// CreatedAt 是规则创建时间。
	CreatedAt string `json:"created_at"`
	// UpdatedAt 是规则更新时间。
	UpdatedAt string `json:"updated_at"`
}

// automationRulePageResponse 是自动化规则分页接口的具名响应 DTO。
type automationRulePageResponse struct {
	// Success 表示查询是否完成。
	Success bool `json:"success"`
	// Data 是当前页自动化规则。
	Data []automationRuleResponse `json:"data"`
	// Total 是符合条件的规则总数。
	Total int `json:"total"`
	// Page 是当前页码。
	Page int `json:"page"`
	// PageSize 是当前页大小。
	PageSize int `json:"page_size"`
	// TotalPages 是总页数。
	TotalPages int `json:"total_pages"`
	// TriggerCounts 是各触发类型的规则数量。
	TriggerCounts map[string]int `json:"trigger_counts"`
}

// automationRunIssueDTO 是自动化异常运行接口对外暴露的具名 DTO。
type automationRunIssueDTO struct {
	// ID 是自动化运行的稳定标识。
	ID int64 `json:"id"`
	// CookieID 是关联账号标识。
	CookieID string `json:"cookie_id"`
	// OrderID 是关联订单标识。
	OrderID string `json:"order_id"`
	// TriggerType 是触发运行的事件类型。
	TriggerType string `json:"trigger_type"`
	// ErrorMessage 是运行进入人工处理状态时记录的原因。
	ErrorMessage string `json:"error_message"`
	// IssueKind 是应用层归类的异常类型。
	IssueKind string `json:"issue_kind"`
	// AllowedResolutions 是当前异常允许的人工处理动作。
	AllowedResolutions []string `json:"allowed_resolutions"`
	// ActionCursor 是下一步动作在计划中的位置。
	ActionCursor int `json:"action_cursor"`
	// SentCount 是已经确认成功的外部动作数量。
	SentCount int `json:"sent_count"`
	// UpdatedAt 是运行状态最近更新的时间文本。
	UpdatedAt string `json:"updated_at"`
}

// deferredAutomationIssueDTO 是延期任务接口对外暴露的具名 DTO。
type deferredAutomationIssueDTO struct {
	// ID 是延期任务的稳定标识。
	ID int64 `json:"id"`
	// CookieID 是关联账号标识。
	CookieID string `json:"cookie_id"`
	// TriggerType 是触发延期任务的事件类型。
	TriggerType string `json:"trigger_type"`
	// ErrorMessage 是任务进入死信状态时记录的原因。
	ErrorMessage string `json:"error_message"`
	// AttemptCount 是任务已经尝试执行的次数。
	AttemptCount int `json:"attempt_count"`
	// UpdatedAt 是任务状态最近更新的时间文本。
	UpdatedAt string `json:"updated_at"`
}

// automationIssuesResponse 是自动化异常任务接口的具名响应 DTO。
type automationIssuesResponse struct {
	// Runs 是需要处理的自动化运行记录。
	Runs []automationRunIssueDTO `json:"runs"`
	// PendingTasks 是需要处理的延迟任务记录。
	PendingTasks []deferredAutomationIssueDTO `json:"pending_tasks"`
}

// orderDetailResponse 是订单详情接口的具名响应 DTO，同时保留旧版顶层字段兼容性。
type orderDetailResponse struct {
	// orderDTO 提供历史客户端仍读取的顶层订单字段。
	orderDTO
	// Success 表示查询是否完成。
	Success bool `json:"success"`
	// Data 是新版客户端读取的订单对象。
	Data orderDTO `json:"data"`
}

// orderUpdateRequestDTO 是订单更新接口的具名请求 DTO，保留旧客户端的状态字段别名。
type orderUpdateRequestDTO struct {
	// OrderStatus 是订单状态补丁。
	OrderStatus *string `json:"order_status"`
	// Status 是旧客户端使用的订单状态别名。
	Status *string `json:"status"`
	// ItemID 是商品标识补丁。
	ItemID *string `json:"item_id"`
	// BuyerID 是买家标识补丁。
	BuyerID *string `json:"buyer_id"`
	// SpecName 是规格名称补丁。
	SpecName *string `json:"spec_name"`
	// SpecValue 是规格值补丁。
	SpecValue *string `json:"spec_value"`
	// Quantity 是兼容字符串或数字格式的购买数量。
	Quantity *any `json:"quantity"`
	// Amount 是兼容字符串或数字格式的订单金额。
	Amount *any `json:"amount"`
	// ReceiverName 是收货人补丁。
	ReceiverName *string `json:"receiver_name"`
	// ReceiverPhone 是收货电话补丁。
	ReceiverPhone *string `json:"receiver_phone"`
	// ReceiverAddress 是收货地址补丁。
	ReceiverAddress *string `json:"receiver_address"`
	// ReceiverCity 是收货城市补丁。
	ReceiverCity *string `json:"receiver_city"`
	// ChatID 是聊天会话补丁。
	ChatID *string `json:"chat_id"`
	// SystemShipped 表示是否由系统完成发货。
	SystemShipped *bool `json:"system_shipped"`
	// ItemTitle 是关联商品标题补丁。
	ItemTitle *string `json:"item_title"`
}

// manualShipRequestDTO 是批量手动发货接口的具名请求 DTO。
type manualShipRequestDTO struct {
	// OrderIDs 是待处理订单标识列表。
	OrderIDs []string `json:"order_ids"`
	// ShipMode 是发货模式。
	ShipMode string `json:"ship_mode"`
}

// qrVerificationRequestDTO 是扫码风控验证完成接口的具名请求 DTO。
type qrVerificationRequestDTO struct {
	// TargetAccountID 是可选的待重新授权本地账号标识。
	TargetAccountID string `json:"target_account_id"`
}

// orderRefreshDetailResponse 是订单刷新后返回的远端详情 DTO。
type orderRefreshDetailResponse struct {
	// Quantity 是订单购买数量。
	Quantity string `json:"quantity"`
	// SpecName 是商品规格名称。
	SpecName string `json:"spec_name"`
	// SpecValue 是商品规格值。
	SpecValue string `json:"spec_value"`
	// OrderStatus 是归一化后的订单状态。
	OrderStatus string `json:"order_status"`
	// Amount 是订单实付金额。
	Amount string `json:"amount"`
}

// orderRefreshResponse 是订单列表刷新接口的具名响应 DTO。
type orderRefreshResponse struct {
	// PartialFailure 表示批量刷新是否存在部分失败。
	PartialFailure bool `json:"partial_failure"`
	// Message 是刷新结果说明。
	Message string `json:"message"`
	// Summary 是刷新统计摘要。
	Summary orderRefreshSummary `json:"summary"`
	// Results 是逐账号或逐订单的兼容结果行。
	Results []orderRefreshResultDTO `json:"results"`
}

// orderRefreshResultDTO 是订单刷新批次中单条结果的稳定响应 DTO；可选字段只在对应阶段产生时返回。
type orderRefreshResultDTO struct {
	// Success 表示当前账号或订单刷新是否成功。
	Success bool `json:"success"`
	// CookieID 是当前结果关联的账号标识。
	CookieID string `json:"cookie_id,omitempty"`
	// Discovered 是当前账号发现的新订单数量。
	Discovered *int `json:"discovered,omitempty"`
	// Updated 是当前账号更新的订单数量。
	Updated *int `json:"updated,omitempty"`
	// SoftDeleted 表示当前账号是否标记了失效订单。
	SoftDeleted *bool `json:"soft_deleted,omitempty"`
	// OrderID 是单订单刷新结果关联的平台订单标识。
	OrderID string `json:"order_id,omitempty"`
	// Stage 是刷新失败或完成所处的业务阶段。
	Stage string `json:"stage,omitempty"`
	// Message 是面向客户端的结果说明。
	Message string `json:"message,omitempty"`
	// Error 是当前结果的兼容错误文本。
	Error string `json:"error,omitempty"`
	// OldStatus 是刷新前的订单状态。
	OldStatus string `json:"old_status,omitempty"`
	// NewStatus 是刷新后的订单状态。
	NewStatus string `json:"new_status,omitempty"`
}

// orderRefreshSummary 是订单列表刷新统计摘要 DTO。
type orderRefreshSummary struct {
	// Discovered 是发现的新订单数量。
	Discovered int `json:"discovered"`
	// ListUpdated 是订单列表更新数量。
	ListUpdated int `json:"list_updated"`
	// SoftDeleted 是标记删除的订单数量。
	SoftDeleted int `json:"soft_deleted"`
	// DetailTotal 是需要补全详情的订单数量。
	DetailTotal int `json:"detail_total"`
	// Total 是本次处理订单总数。
	Total int `json:"total"`
	// Updated 是状态发生变化的订单数量。
	Updated int `json:"updated"`
	// NoChange 是状态未发生变化的订单数量。
	NoChange int `json:"no_change"`
	// Failed 是刷新失败数量。
	Failed int `json:"failed"`
}

// orderSingleRefreshResponse 是单订单刷新接口的具名响应 DTO。
type orderSingleRefreshResponse struct {
	// Success 表示刷新是否完成。
	Success bool `json:"success"`
	// Message 是刷新结果说明。
	Message string `json:"message"`
	// Order 是刷新后的订单详情。
	Order orderRefreshDetailResponse `json:"order"`
}

// manualShipResponse 是手动发货接口的具名响应 DTO。
type manualShipResponse struct {
	// PartialFailure 表示批量发货是否存在部分失败。
	PartialFailure bool `json:"partial_failure"`
	// Message 是发货结果说明。
	Message string `json:"message"`
	// SuccessCount 是成功发货数量。
	SuccessCount int `json:"success_count"`
	// FailedCount 是失败发货数量。
	FailedCount int `json:"failed_count"`
	// Results 是逐订单的兼容结果行。
	Results []orderMutationResultDTO `json:"results"`
}

// orderMutationResultDTO 是手动发货逐订单结果的稳定响应 DTO。
type orderMutationResultDTO struct {
	// OrderID 是当前结果关联的平台订单标识。
	OrderID string `json:"order_id"`
	// Status 是发货后订单状态或失败阶段。
	Status string `json:"status"`
	// Success 表示当前订单是否发货成功。
	Success bool `json:"success"`
	// Message 是当前订单的处理说明。
	Message string `json:"message"`
	// ReconciliationID 是可选的补偿记录标识。
	ReconciliationID string `json:"reconciliation_id,omitempty"`
	// ReconciliationWarning 是可选的补偿告警文本。
	ReconciliationWarning string `json:"reconciliation_warning,omitempty"`
}

// importOrdersResponse 是订单导入接口的具名响应 DTO。
type importOrdersResponse struct {
	// PartialFailure 表示批量导入是否存在部分失败。
	PartialFailure bool `json:"partial_failure"`
	// Message 是导入结果说明。
	Message string `json:"message"`
	// Total 是本次导入订单总数。
	Total int `json:"total"`
	// SuccessCount 是成功导入数量。
	SuccessCount int `json:"success_count"`
	// FailedCount 是失败导入数量。
	FailedCount int `json:"failed_count"`
	// Results 是逐订单的兼容结果行。
	Results []orderImportResultDTO `json:"results"`
}

// orderImportResultDTO 是订单导入逐订单结果的稳定响应 DTO。
type orderImportResultDTO struct {
	// OrderID 是当前结果关联的平台订单标识。
	OrderID string `json:"order_id"`
	// Success 表示当前订单是否导入成功。
	Success bool `json:"success"`
	// Message 是当前订单的处理说明。
	Message string `json:"message"`
}
