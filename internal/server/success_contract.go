package server

import (
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
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

// newChatMessageDTOsFromApplication 将聊天应用层消息转换为 HTTP DTO，避免响应暴露数据库模型。
func newChatMessageDTOsFromApplication(messages []chatapp.Message) []chatMessageDTO {
	// result 是转换后的聊天消息 DTO 列表。
	result := make([]chatMessageDTO, 0, len(messages))
	// message 保存当前待转换的应用层消息。
	for _, message := range messages {
		result = append(result, chatMessageDTO{
			ID: message.ID, AccountID: message.AccountID, ChatID: message.ChatID,
			MessageKey: message.MessageKey, Direction: message.Direction, SenderID: message.SenderID,
			SenderName: message.SenderName, MessageType: message.MessageType, Content: message.Content,
			Status: message.Status, SentAt: message.SentAt,
		})
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

// automationIssuesResponse 是自动化异常任务接口的具名响应 DTO。
type automationIssuesResponse struct {
	// Runs 是需要处理的自动化运行记录。
	Runs []db.AutomationRunIssue `json:"runs"`
	// PendingTasks 是需要处理的延迟任务记录。
	PendingTasks []db.DeferredAutomationIssue `json:"pending_tasks"`
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
	Results []map[string]any `json:"results"`
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
	Results []map[string]any `json:"results"`
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
	Results []map[string]any `json:"results"`
}

// aiReplySettingsResponse 是账号 AI 回复设置接口的具名响应 DTO。
type aiReplySettingsResponse struct {
	// CookieID 是账号稳定标识；默认配置响应省略该字段。
	CookieID string `json:"cookie_id,omitempty"`
	// AIEnabled 表示账号 AI 回复是否启用。
	AIEnabled bool `json:"ai_enabled"`
	// MaxDiscountPercent 是允许的最大折扣比例。
	MaxDiscountPercent int `json:"max_discount_percent"`
	// MaxDiscountAmount 是允许的最大折扣金额。
	MaxDiscountAmount int `json:"max_discount_amount"`
	// MaxBargainRounds 是允许的最大砍价轮次。
	MaxBargainRounds int `json:"max_bargain_rounds"`
	// CustomPrompts 是账号自定义提示词。
	CustomPrompts string `json:"custom_prompts"`
}

// aiModelsResponse 是 AI 模型发现接口的具名响应 DTO。
type aiModelsResponse struct {
	// Models 是远端可用模型名称列表。
	Models []string `json:"models"`
}

// userSettingResponse 是单个用户设置查询接口的具名响应 DTO。
type userSettingResponse struct {
	// Value 是设置值文本。
	Value string `json:"value"`
}

// cardResponse 是卡券详情和列表接口的具名响应 DTO。
type cardResponse struct {
	// ID 是卡券组稳定标识。
	ID int64 `json:"id"`
	// Name 是卡券组名称。
	Name string `json:"name"`
	// Type 是卡券类型。
	Type string `json:"type"`
	// APIConfig 是 API 卡券配置 JSON。
	APIConfig string `json:"api_config"`
	// TextContent 是文本卡券内容。
	TextContent string `json:"text_content"`
	// DataContent 是批量数据卡券内容。
	DataContent string `json:"data_content"`
	// ImageURL 是图片卡券地址。
	ImageURL string `json:"image_url"`
	// Description 是卡券组描述。
	Description string `json:"description"`
	// Enabled 表示卡券组是否启用。
	Enabled bool `json:"enabled"`
	// DelaySeconds 是自动发货延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
	// IsMultiSpec 表示卡券组是否按规格区分。
	IsMultiSpec bool `json:"is_multi_spec"`
	// SpecName 是卡券规格名称。
	SpecName string `json:"spec_name"`
	// SpecValue 是卡券规格值。
	SpecValue string `json:"spec_value"`
	// UserID 是卡券所属用户标识，保留旧接口字段。
	UserID int64 `json:"user_id,omitempty"`
}

// newCardResponse 将数据库卡券模型转换为 HTTP DTO。
func newCardResponse(card db.CardFull) cardResponse {
	return cardResponse{
		ID: card.ID, Name: card.Name, Type: card.Type, APIConfig: card.APIConfig,
		TextContent: card.TextContent, DataContent: card.DataContent, ImageURL: card.ImageURL,
		Description: card.Description, Enabled: card.Enabled, DelaySeconds: card.DelaySeconds,
		IsMultiSpec: card.IsMultiSpec, SpecName: card.SpecName, SpecValue: card.SpecValue, UserID: card.UserID,
	}
}

// newCardResponses 批量转换卡券列表，避免直接暴露数据库模型。
func newCardResponses(cards []db.CardFull) []cardResponse {
	// result 是转换后的卡券 DTO 列表。
	result := make([]cardResponse, 0, len(cards))
	// card 是当前待转换的卡券数据库模型。
	for _, card := range cards {
		result = append(result, newCardResponse(card))
	}
	return result
}

// cardBatchResponse 是卡券批量创建接口的具名响应 DTO。
type cardBatchResponse struct {
	// Success 表示批量解析和处理流程已完成。
	Success bool `json:"success"`
	// Total 是表格中解析出的数据行数量。
	Total int `json:"total"`
	// Created 是成功创建的卡券组数量。
	Created int `json:"created"`
	// Failed 是创建失败的数据行数量。
	Failed int `json:"failed"`
	// Rows 是逐行处理结果。
	Rows []cardBatchResultRow `json:"rows"`
}

// cardAppendResponse 是追加卡密接口的具名响应 DTO。
type cardAppendResponse struct {
	// Success 表示追加操作是否完成。
	Success bool `json:"success"`
	// Added 是实际追加的卡密数量。
	Added int `json:"added"`
}

// notificationChannelResponse 是通知渠道接口的具名响应 DTO。
type notificationChannelResponse struct {
	// ID 是通知渠道稳定标识。
	ID int64 `json:"id"`
	// Name 是通知渠道名称。
	Name string `json:"name"`
	// Type 是通知渠道类型。
	Type string `json:"type"`
	// Config 是通知渠道配置 JSON。
	Config string `json:"config"`
	// EventTypes 是订阅事件类型 JSON 或兼容分隔文本。
	EventTypes string `json:"event_types,omitempty"`
	// Enabled 表示通知渠道是否启用。
	Enabled bool `json:"enabled"`
	// UserID 是渠道所属用户标识，保留旧接口字段。
	UserID int64 `json:"user_id,omitempty"`
}

// newNotificationChannelResponse 将数据库通知渠道转换为 HTTP DTO。
func newNotificationChannelResponse(channel db.NotificationChannelRow) notificationChannelResponse {
	return notificationChannelResponse{
		ID: channel.ID, Name: channel.Name, Type: channel.Type, Config: channel.Config,
		EventTypes: channel.EventTypes, Enabled: channel.Enabled, UserID: channel.UserID,
	}
}

// newNotificationChannelResponses 批量转换通知渠道，保持数据库模型不穿透 HTTP 层。
func newNotificationChannelResponses(channels []db.NotificationChannelRow) []notificationChannelResponse {
	// result 是转换后的通知渠道 DTO 列表。
	result := make([]notificationChannelResponse, 0, len(channels))
	// channel 是当前待转换的通知渠道数据库模型。
	for _, channel := range channels {
		result = append(result, newNotificationChannelResponse(channel))
	}
	return result
}

// notificationBindingResponse 是单条账号通知绑定的具名 DTO。
type notificationBindingResponse struct {
	// ID 是绑定记录稳定标识。
	ID int64 `json:"id"`
	// ChannelID 是通知渠道标识。
	ChannelID int64 `json:"channel_id"`
	// ChannelName 是通知渠道名称。
	ChannelName string `json:"channel_name"`
	// Enabled 表示该账号绑定是否启用。
	Enabled bool `json:"enabled"`
}

// notificationBindingListResponse 是按账号分组的通知绑定响应 DTO。
type notificationBindingListResponse map[string][]notificationBindingResponse

// accountBindingsResponse 是账号与通知渠道绑定查询接口的具名响应 DTO。
type accountBindingsResponse struct {
	// CookieID 是账号稳定标识。
	CookieID string `json:"cookie_id"`
	// ChannelIDs 是当前账号绑定的通知渠道标识列表。
	ChannelIDs []int64 `json:"channel_ids"`
}

// notificationUncertainOutboxItem 是不确定通知的非敏感运维摘要。
// 该 DTO 不包含通知正文、渠道配置、凭证或最后错误原文。
type notificationUncertainOutboxItem struct {
	// ID 是通知 outbox 记录的稳定标识。
	ID int64 `json:"id"`
	// ChannelID 是关联通知渠道标识。
	ChannelID int64 `json:"channel_id"`
	// OwnerUserID 是渠道所属用户标识，仅管理员查询时返回。
	OwnerUserID int64 `json:"owner_user_id,omitempty"`
	// EventType 是通知事件分类。
	EventType string `json:"event_type"`
	// AttemptCount 是进入不确定状态前的发送尝试次数。
	AttemptCount int `json:"attempt_count"`
	// UncertainAt 是进入不确定状态的 Unix 秒时间戳。
	UncertainAt int64 `json:"uncertain_at"`
	// HasError 表示是否存在本地确认错误，但不暴露错误原文。
	HasError bool `json:"has_error"`
}

// notificationUncertainOutboxResponse 是用户或管理员查询不确定通知的具名响应。
type notificationUncertainOutboxResponse struct {
	// Total 是当前权限范围内的不确定通知总数。
	Total int `json:"total"`
	// Items 是按最近进入不确定状态排序的非敏感摘要列表。
	Items []notificationUncertainOutboxItem `json:"items"`
}

// newNotificationUncertainOutboxResponse 将数据库不确定状态摘要转换为非敏感 HTTP DTO。
// includeOwner 仅管理员列表使用，用于展示渠道所属用户但不改变正文脱敏边界。
func newNotificationUncertainOutboxResponse(items []db.NotificationUncertainSummary, total int, includeOwner bool) notificationUncertainOutboxResponse {
	// result 保存不确定通知查询的具名响应。
	result := notificationUncertainOutboxResponse{Total: total, Items: make([]notificationUncertainOutboxItem, 0, len(items))}
	// item 保存当前待转换的数据库摘要。
	for _, item := range items {
		// responseItem 保存当前摘要对应的非敏感 API 行。
		responseItem := notificationUncertainOutboxItem{
			ID: item.ID, ChannelID: item.ChannelID, EventType: item.EventType,
			AttemptCount: item.AttemptCount, UncertainAt: item.UncertainAt, HasError: item.HasError,
		}
		if includeOwner {
			responseItem.OwnerUserID = item.OwnerUserID
		}
		result.Items = append(result.Items, responseItem)
	}
	return result
}

// categoryRecommendationResponse 是商品类目推荐接口的具名响应 DTO。
type categoryRecommendationResponse struct {
	// Success 表示类目推荐是否成功。
	Success bool `json:"success"`
	// Category 是推荐的商品类目。
	Category mtop.PublishCategory `json:"category"`
}

// itemPublishBatchPreviewResponse 是商品批量发布预检接口的具名响应 DTO。
type itemPublishBatchPreviewResponse struct {
	// Success 表示预检是否完成。
	Success bool `json:"success"`
	// PreviewID 是后续启动批量发布使用的预检批次标识。
	PreviewID string `json:"preview_id"`
	// Total 是预检数据行总数。
	Total int `json:"total"`
	// Valid 是通过预检的数据行数量。
	Valid int `json:"valid"`
	// Invalid 是未通过预检的数据行数量。
	Invalid int `json:"invalid"`
	// Rows 是逐行预检结果。
	Rows []publishBatchPreviewRow `json:"rows"`
}

// batchIDResponse 是商品批量任务启动或重试接口的具名响应 DTO。
type batchIDResponse struct {
	// Success 表示任务操作是否完成。
	Success bool `json:"success"`
	// BatchID 是商品批量任务标识。
	BatchID string `json:"batch_id"`
}

// batchCancelResponse 是商品批量任务取消接口的具名响应 DTO。
type batchCancelResponse struct {
	// Success 表示取消请求是否完成。
	Success bool `json:"success"`
	// Status 是任务取消后的状态。
	Status string `json:"status"`
}

// itemPublishBatchRowResponse 是商品批量任务逐行详情 DTO。
type itemPublishBatchRowResponse struct {
	// ID 是批量任务明细行主键。
	ID int64 `json:"id"`
	// RowNo 是导入表格中的行号。
	RowNo int `json:"row_no"`
	// CookieID 是商品发布目标账号标识。
	CookieID string `json:"cookie_id"`
	// Title 是商品标题。
	Title string `json:"title"`
	// Price 是商品价格文本。
	Price string `json:"price"`
	// Quantity 是商品库存数量。
	Quantity int `json:"quantity"`
	// Images 是商品图片引用列表。
	Images []string `json:"images"`
	// Category 是商品发布类目。
	Category mtop.PublishCategory `json:"category"`
	// Automation 是发布后自动化配置。
	Automation publishAutomationConfig `json:"automation"`
	// Status 是当前明细行状态。
	Status string `json:"status"`
	// ItemID 是发布成功后的平台商品标识。
	ItemID string `json:"item_id"`
	// ItemURL 是发布成功后的商品地址。
	ItemURL string `json:"item_url"`
	// ErrorMessage 是明细行失败原因。
	ErrorMessage string `json:"error_message"`
	// FailureKind 是失败类型。
	FailureKind string `json:"failure_kind"`
}

// itemPublishBatchResponse 是商品批量任务详情接口的具名响应 DTO。
type itemPublishBatchResponse struct {
	// ID 是商品批量任务标识。
	ID string `json:"id"`
	// Status 是批量任务状态。
	Status string `json:"status"`
	// Filename 是原始上传文件名。
	Filename string `json:"filename"`
	// Total 是明细行总数。
	Total int `json:"total"`
	// Success 是成功发布的明细行数量，保留旧字段名称。
	Success int `json:"success"`
	// Failed 是失败明细行数量。
	Failed int `json:"failed"`
	// Pending 是待处理明细行数量。
	Pending int `json:"pending"`
	// Running 是正在处理明细行数量。
	Running int `json:"running"`
	// Retryable 是可重试明细行数量。
	Retryable int `json:"retryable"`
	// Rows 是批量任务逐行结果。
	Rows []itemPublishBatchRowResponse `json:"rows"`
	// Location 是批次统一发货地对象。
	Location any `json:"location"`
	// CreatedAt 是任务创建时间。
	CreatedAt string `json:"created_at"`
	// UpdatedAt 是任务更新时间。
	UpdatedAt string `json:"updated_at"`
}

// itemPublishBatchListResponse 是商品批量任务列表接口的具名响应 DTO。
type itemPublishBatchListResponse struct {
	// Batches 是当前用户的商品批量任务列表。
	Batches []itemPublishBatchResponse `json:"batches"`
}

// keywordBasicResponse 是传统关键词接口的基础响应 DTO。
type keywordBasicResponse struct {
	// Keyword 是匹配关键词。
	Keyword string `json:"keyword"`
	// Reply 是文字回复内容。
	Reply string `json:"reply"`
}

// keywordItemResponse 是带商品范围的关键词响应 DTO。
type keywordItemResponse struct {
	// Keyword 是匹配关键词。
	Keyword string `json:"keyword"`
	// Reply 是文字回复内容。
	Reply string `json:"reply"`
	// ItemID 是限定的商品标识。
	ItemID string `json:"item_id"`
}

// keywordTypedResponse 是支持文本/图片类型的关键词响应 DTO。
type keywordTypedResponse struct {
	// ID 是关键词规则主键。
	ID int64 `json:"id"`
	// Keyword 是匹配关键词。
	Keyword string `json:"keyword"`
	// Reply 是文字回复内容。
	Reply string `json:"reply"`
	// ItemID 是限定的商品标识。
	ItemID string `json:"item_id"`
	// Type 是回复类型。
	Type string `json:"type"`
	// ImageURL 是图片回复地址。
	ImageURL string `json:"image_url"`
}

// itemReplyResponse 是指定商品回复接口的具名 DTO。
type itemReplyResponse struct {
	// ItemID 是商品平台标识。
	ItemID string `json:"item_id,omitempty"`
	// CookieID 是账号稳定标识。
	CookieID string `json:"cookie_id,omitempty"`
	// ReplyContent 是指定商品的回复内容。
	ReplyContent string `json:"reply_content"`
}

// defaultReplyResponse 是默认回复接口的具名 DTO。
type defaultReplyResponse struct {
	// CookieID 是账号稳定标识；单账号查询响应可以省略。
	CookieID string `json:"cookie_id,omitempty"`
	// Enabled 表示默认回复是否启用。
	Enabled bool `json:"enabled"`
	// ReplyContent 是默认文字回复内容。
	ReplyContent string `json:"reply_content"`
	// ReplyImageURL 是默认图片回复地址。
	ReplyImageURL string `json:"reply_image_url,omitempty"`
	// ReplyOnce 表示是否只回复一次。
	ReplyOnce bool `json:"reply_once"`
}

// newDefaultReplyResponse 将数据库默认回复转换为 HTTP DTO。
func newDefaultReplyResponse(cookieID string, reply db.DefaultReply) defaultReplyResponse {
	return defaultReplyResponse{
		CookieID: cookieID, Enabled: reply.Enabled, ReplyContent: reply.ReplyContent,
		ReplyImageURL: reply.ReplyImageURL, ReplyOnce: reply.ReplyOnce,
	}
}

// accountTaskSettingsResponse 是账号任务设置接口的具名 DTO。
type accountTaskSettingsResponse struct {
	// AccountID 是账号稳定标识。
	AccountID string `json:"account_id"`
	// AutoRateEnabled 表示自动评价是否启用。
	AutoRateEnabled bool `json:"auto_rate_enabled"`
	// RateContent 是自动评价文案。
	RateContent string `json:"rate_content"`
	// AutoPolishEnabled 表示自动擦亮是否启用。
	AutoPolishEnabled bool `json:"auto_polish_enabled"`
	// PolishTime 是自动擦亮本地时间。
	PolishTime string `json:"polish_time"`
	// LastRateScanAt 是最近一次评价扫描时间。
	LastRateScanAt int64 `json:"last_rate_scan_at"`
	// LastPolishDate 是最近一次擦亮日期。
	LastPolishDate string `json:"last_polish_date"`
	// LastPolishAt 是最近一次擦亮时间。
	LastPolishAt int64 `json:"last_polish_at"`
}

// newAccountTaskSettingsResponse 将账号任务数据库设置转换为 HTTP DTO。
func newAccountTaskSettingsResponse(settings db.AccountTaskSettings) accountTaskSettingsResponse {
	return accountTaskSettingsResponse{
		AccountID: settings.CookieID, AutoRateEnabled: settings.AutoRateEnabled, RateContent: settings.RateContent,
		AutoPolishEnabled: settings.AutoPolishEnabled, PolishTime: settings.PolishTime,
		LastRateScanAt: settings.LastRateScanAt, LastPolishDate: settings.LastPolishDate, LastPolishAt: settings.LastPolishAt,
	}
}

// accountTaskRunResponse 是账号任务执行记录的具名 DTO。
type accountTaskRunResponse struct {
	// ID 是任务执行记录主键。
	ID int64 `json:"id"`
	// RunKey 是任务幂等键。
	RunKey string `json:"run_key"`
	// AccountID 是账号稳定标识。
	AccountID string `json:"account_id"`
	// TaskType 是任务类型。
	TaskType string `json:"task_type"`
	// TargetID 是任务目标标识。
	TargetID string `json:"target_id"`
	// RunDate 是任务业务日期。
	RunDate string `json:"run_date"`
	// Status 是任务执行状态。
	Status string `json:"status"`
	// SuccessCount 是任务成功数量。
	SuccessCount int `json:"success_count"`
	// FailedCount 是任务失败数量。
	FailedCount int `json:"failed_count"`
	// ErrorMessage 是任务失败说明。
	ErrorMessage string `json:"error_message"`
	// NextRetryAt 是下一次重试时间。
	NextRetryAt int64 `json:"next_retry_at"`
	// StartedAt 是任务开始时间。
	StartedAt int64 `json:"started_at"`
	// FinishedAt 是任务完成时间。
	FinishedAt int64 `json:"finished_at"`
}

// newAccountTaskRunResponse 将账号任务执行记录转换为 HTTP DTO。
func newAccountTaskRunResponse(run db.AccountTaskRun) accountTaskRunResponse {
	return accountTaskRunResponse{
		ID: run.ID, RunKey: run.RunKey, AccountID: run.CookieID, TaskType: run.TaskType,
		TargetID: run.TargetID, RunDate: run.RunDate, Status: run.Status, SuccessCount: run.SuccessCount,
		FailedCount: run.FailedCount, ErrorMessage: run.ErrorMessage, NextRetryAt: run.NextRetryAt,
		StartedAt: run.StartedAt, FinishedAt: run.FinishedAt,
	}
}

// newAccountTaskRunResponses 批量转换账号任务执行记录。
func newAccountTaskRunResponses(runs []db.AccountTaskRun) []accountTaskRunResponse {
	// result 是账号任务执行记录 DTO 列表。
	result := make([]accountTaskRunResponse, 0, len(runs))
	// run 是当前待转换的账号任务执行记录。
	for _, run := range runs {
		result = append(result, newAccountTaskRunResponse(run))
	}
	return result
}

// accountTaskSummaryResponse 是手动执行账号任务的统计 DTO。
type accountTaskSummaryResponse struct {
	// TaskType 是任务类型。
	TaskType string `json:"task_type"`
	// Found 是发现的目标数量。
	Found int `json:"found"`
	// Success 是成功处理数量。
	Success int `json:"success"`
	// Failed 是失败处理数量。
	Failed int `json:"failed"`
	// Skipped 是跳过数量。
	Skipped int `json:"skipped"`
	// Message 是任务结果说明。
	Message string `json:"message,omitempty"`
}

// accountTaskRunResponseEnvelope 是手动执行账号任务的具名响应 DTO。
type accountTaskRunResponseEnvelope struct {
	// Success 表示任务请求是否成功完成。
	Success bool `json:"success"`
	// Summary 是账号任务执行统计。
	Summary accountTaskSummaryResponse `json:"summary"`
}

// accountTaskRunsResponse 是账号任务执行记录列表的具名响应 DTO。
type accountTaskRunsResponse struct {
	// Runs 是当前账号最近的任务执行记录。
	Runs []accountTaskRunResponse `json:"runs"`
}

// newAccountTaskSummaryResponse 将自动化中心统计转换为 HTTP DTO。
func newAccountTaskSummaryResponse(summary automation.AccountTaskSummary) accountTaskSummaryResponse {
	return accountTaskSummaryResponse{
		TaskType: summary.TaskType, Found: summary.Found, Success: summary.Success,
		Failed: summary.Failed, Skipped: summary.Skipped, Message: summary.Message,
	}
}

// adminUserResponse 是管理员用户列表项的具名响应 DTO。
type adminUserResponse struct {
	// ID 是用户主键。
	ID int64 `json:"id"`
	// Username 是用户登录名。
	Username string `json:"username"`
	// Email 是用户邮箱。
	Email string `json:"email"`
	// IsActive 表示用户是否启用。
	IsActive bool `json:"is_active"`
	// IsAdmin 表示用户是否为管理员。
	IsAdmin bool `json:"is_admin"`
	// CreatedAt 是用户创建时间。
	CreatedAt string `json:"created_at"`
	// CookieCount 是用户拥有的账号数量。
	CookieCount int `json:"cookie_count"`
}

// adminCookieResponse 是管理员账号列表项的具名响应 DTO。
type adminCookieResponse struct {
	// ID 是账号稳定标识。
	ID string `json:"id"`
	// UserID 是账号所属用户主键。
	UserID int64 `json:"user_id"`
	// Remark 是账号备注。
	Remark string `json:"remark"`
	// CreatedAt 是账号创建时间。
	CreatedAt string `json:"created_at"`
	// Owner 是账号所属用户名。
	Owner string `json:"owner"`
	// Enabled 表示账号是否启用。
	Enabled bool `json:"enabled"`
}

// adminStatsResponse 是管理员全局统计的具名响应 DTO。
type adminStatsResponse struct {
	// TotalUsers 是用户总数。
	TotalUsers int64 `json:"total_users"`
	// TotalCookies 是账号总数。
	TotalCookies int64 `json:"total_cookies"`
	// ActiveCookies 是活跃账号数。
	ActiveCookies int64 `json:"active_cookies"`
	// TotalCards 是卡券总数。
	TotalCards int64 `json:"total_cards"`
	// TotalKeywords 是关键词规则总数。
	TotalKeywords int64 `json:"total_keywords"`
	// TotalOrders 是有效订单总数。
	TotalOrders int64 `json:"total_orders"`
}

// dashboardStatsResponse 是当前用户数据概览的具名响应 DTO。
type dashboardStatsResponse struct {
	// TotalCookies 是账号总数。
	TotalCookies int64 `json:"total_cookies"`
	// ActiveCookies 是活跃账号数。
	ActiveCookies int64 `json:"active_cookies"`
	// TotalCards 是卡券总数。
	TotalCards int64 `json:"total_cards"`
	// AvailableCardStock 是可用卡券库存数量。
	AvailableCardStock int64 `json:"available_card_stock"`
	// TotalKeywords 是关键词规则总数。
	TotalKeywords int64 `json:"total_keywords"`
	// TotalOrders 是订单总数。
	TotalOrders int64 `json:"total_orders"`
}

// analyticsRevenueStatsResponse 是订单收益统计的具名 DTO。
type analyticsRevenueStatsResponse struct {
	// TotalOrders 是统计范围内的订单数。
	TotalOrders int `json:"total_orders"`
	// TotalAmount 是统计范围内的订单总金额。
	TotalAmount float64 `json:"total_amount"`
	// AvgAmount 是订单平均金额。
	AvgAmount float64 `json:"avg_amount"`
	// UniqueBuyers 是买家数量。
	UniqueBuyers int `json:"unique_buyers"`
	// UniqueItems 是商品数量。
	UniqueItems int `json:"unique_items"`
}

// analyticsDailyStatsResponse 是按日期聚合的订单统计 DTO。
type analyticsDailyStatsResponse struct {
	// Date 是用户本地日期。
	Date string `json:"date"`
	// OrderCount 是当天订单数。
	OrderCount int `json:"order_count"`
	// Amount 是当天订单金额。
	Amount float64 `json:"amount"`
}

// analyticsStatusStatsResponse 是按订单状态聚合的统计 DTO。
type analyticsStatusStatsResponse struct {
	// Status 是归一化后的订单状态。
	Status string `json:"status"`
	// Count 是该状态订单数。
	Count int `json:"count"`
	// Amount 是该状态订单金额。
	Amount float64 `json:"amount"`
}

// analyticsCityStatsResponse 是按收货城市聚合的统计 DTO。
type analyticsCityStatsResponse struct {
	// City 是收货城市。
	City string `json:"city"`
	// OrderCount 是该城市订单数。
	OrderCount int `json:"order_count"`
	// TotalAmount 是该城市订单金额。
	TotalAmount float64 `json:"total_amount"`
}

// analyticsItemStatsResponse 是按商品聚合的统计 DTO。
type analyticsItemStatsResponse struct {
	// ItemID 是商品平台标识。
	ItemID string `json:"item_id"`
	// OrderCount 是该商品订单数。
	OrderCount int `json:"order_count"`
	// TotalAmount 是该商品订单金额。
	TotalAmount float64 `json:"total_amount"`
	// AvgAmount 是该商品订单平均金额。
	AvgAmount float64 `json:"avg_amount"`
}

// orderAnalyticsResponse 是订单分析接口的具名响应 DTO。
type orderAnalyticsResponse struct {
	// RevenueStats 是收益统计。
	RevenueStats analyticsRevenueStatsResponse `json:"revenue_stats"`
	// DailyStats 是按日统计。
	DailyStats []analyticsDailyStatsResponse `json:"daily_stats"`
	// StatusStats 是按状态统计。
	StatusStats []analyticsStatusStatsResponse `json:"status_stats"`
	// CityStats 是按城市统计。
	CityStats []analyticsCityStatsResponse `json:"city_stats"`
	// ItemStats 是按商品统计。
	ItemStats []analyticsItemStatsResponse `json:"item_stats"`
}

// validOrderResponse 是有效订单明细项的具名响应 DTO。
type validOrderResponse struct {
	// OrderID 是平台订单标识。
	OrderID string `json:"order_id"`
	// ItemID 是商品平台标识。
	ItemID string `json:"item_id"`
	// BuyerID 是买家平台标识。
	BuyerID string `json:"buyer_id"`
	// ItemTitle 是商品标题。
	ItemTitle string `json:"item_title"`
	// ItemImage 是商品图片地址。
	ItemImage string `json:"item_image"`
	// Quantity 是订单数量文本。
	Quantity string `json:"quantity"`
	// Amount 是订单金额文本。
	Amount string `json:"amount"`
	// OrderStatus 是兼容保留的订单状态字段。
	OrderStatus string `json:"order_status"`
	// Status 是归一化后的订单状态。
	Status string `json:"status"`
	// CookieID 是订单所属账号标识。
	CookieID string `json:"cookie_id"`
	// CreatedAt 是订单创建时间。
	CreatedAt string `json:"created_at"`
}

// validOrdersResponse 是有效订单分页查询的具名响应 DTO。
type validOrdersResponse struct {
	// Orders 是当前页有效订单。
	Orders []validOrderResponse `json:"orders"`
	// Total 是符合条件的订单总数。
	Total int `json:"total"`
	// Page 是当前页码。
	Page int `json:"page"`
	// PageSize 是当前页大小。
	PageSize int `json:"page_size"`
	// Truncated 表示是否还有未返回的订单。
	Truncated bool `json:"truncated"`
}

// qrLoginGenerateResponse 是扫码登录二维码生成的具名响应 DTO。
type qrLoginGenerateResponse struct {
	// Success 表示二维码是否生成成功。
	Success bool `json:"success"`
	// SessionID 是扫码登录会话标识。
	SessionID string `json:"session_id"`
	// QRCodeURL 是二维码图片地址。
	QRCodeURL string `json:"qr_code_url"`
	// Message 是可选的提示文本。
	Message string `json:"message,omitempty"`
}

// settingsResponse 是动态设置查询的具名边界类型，键和值仍由设置仓库定义。
type settingsResponse map[string]string

// qrLoginStatusResponse 是二维码状态的兼容响应类型，允许上游扩展非敏感字段。
type qrLoginStatusResponse map[string]any

// qrLoginVerificationResponse 是二维码风控验证完成的具名响应 DTO。
type qrLoginVerificationResponse struct {
	// Success 表示验证结果是否成功。
	Success bool `json:"success"`
	// UNB 是平台账号标识。
	UNB string `json:"unb"`
	// AccountID 是持久化后的本地账号标识。
	AccountID string `json:"account_id,omitempty"`
	// IsNewAccount 表示是否新建了本地账号。
	IsNewAccount bool `json:"is_new_account,omitempty"`
}
