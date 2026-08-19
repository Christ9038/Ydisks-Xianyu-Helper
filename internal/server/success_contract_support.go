package server

import (
	"encoding/json"

	cardsapp "xianyu-go/internal/application/cards"
	defaultreplyapp "xianyu-go/internal/application/defaultreply"
	notificationsapp "xianyu-go/internal/application/notifications"
)

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

// newCardResponse 将应用层卡券模型转换为 HTTP DTO。
func newCardResponse(card cardsapp.Card) cardResponse {
	return cardResponse{
		ID: card.ID, Name: card.Name, Type: card.Type, APIConfig: card.APIConfig,
		TextContent: card.TextContent, DataContent: card.DataContent, ImageURL: card.ImageURL,
		Description: card.Description, Enabled: card.Enabled, DelaySeconds: card.DelaySeconds,
		IsMultiSpec: card.IsMultiSpec, SpecName: card.SpecName, SpecValue: card.SpecValue, UserID: card.UserID,
	}
}

// newCardResponses 批量转换应用层卡券列表，避免 HTTP 层暴露数据库模型。
func newCardResponses(cards []cardsapp.Card) []cardResponse {
	// result 是转换后的卡券 DTO 列表。
	result := make([]cardResponse, 0, len(cards))
	// card 是当前待转换的卡券应用模型。
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
	// EventTypes 是订阅事件类型 JSON 或兼容分隔文本。
	EventTypes string `json:"event_types,omitempty"`
	// Enabled 表示通知渠道是否启用。
	Enabled bool `json:"enabled"`
	// UserID 是渠道所属用户标识，保留旧接口字段。
	UserID int64 `json:"user_id,omitempty"`
}

// newNotificationChannelResponse 将数据库通知渠道转换为 HTTP DTO。
func newNotificationChannelResponse(channel notificationsapp.ChannelSummary) notificationChannelResponse {
	return notificationChannelResponse{
		ID: channel.ID, Name: channel.Name, Type: channel.Type,
		EventTypes: channel.EventTypes, Enabled: channel.Enabled, UserID: channel.UserID,
	}
}

// newNotificationChannelResponses 批量转换通知渠道，保持数据库模型不穿透 HTTP 层。
func newNotificationChannelResponses(channels []notificationsapp.ChannelSummary) []notificationChannelResponse {
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

// newNotificationUncertainOutboxResponse 将应用层不确定状态摘要转换为非敏感 HTTP DTO。
// includeOwner 仅管理员列表使用，用于展示渠道所属用户但不改变正文脱敏边界。
func newNotificationUncertainOutboxResponse(items []notificationsapp.UncertainSummary, total int, includeOwner bool) notificationUncertainOutboxResponse {
	// result 保存不确定通知查询的具名响应。
	result := notificationUncertainOutboxResponse{Total: total, Items: make([]notificationUncertainOutboxItem, 0, len(items))}
	// item 保存当前待转换的应用层摘要。
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
	Category publishCategoryResponse `json:"category"`
}

// publishCategoryResponse 是商品类目 HTTP 响应的稳定 DTO，不泄露平台包类型。
type publishCategoryResponse struct {
	// CatID 是平台类目标识。
	CatID string `json:"cat_id"`
	// CatName 是平台类目名称。
	CatName string `json:"cat_name"`
	// ChannelCatID 是闲鱼频道类目标识。
	ChannelCatID string `json:"channel_cat_id,omitempty"`
	// TBCatID 是淘宝类目标识。
	TBCatID string `json:"tb_cat_id,omitempty"`
}

// publishAutomationConfig 是批量发布预检与详情响应中的自动化配置 DTO。
type publishAutomationConfig struct {
	// PaidDelivery 保存付款后自动发货配置。
	PaidDelivery publishCardAutomation `json:"paid_delivery"`
	// ReviewGift 保存评价后赠品配置。
	ReviewGift publishCardAutomation `json:"review_gift"`
	// ReviewRequest 保存超时求评价配置。
	ReviewRequest publishReviewRequestCfg `json:"review_request"`
}

// publishCardAutomation 是批量发布中的卡密自动化 DTO。
type publishCardAutomation struct {
	// Enabled 表示该自动化规则是否启用。
	Enabled bool `json:"enabled"`
	// Actions 保存按顺序执行的卡密动作。
	Actions []publishCardAction `json:"actions"`
	// ParseError 保存导入动作文本的解析错误。
	ParseError string `json:"-"`
}

// publishCardAction 是单条批量卡密动作 DTO。
type publishCardAction struct {
	// CardID 是卡密组标识。
	CardID int64 `json:"card_id"`
	// DeliveryCount 是每件商品发送的卡密数量。
	DeliveryCount int `json:"delivery_count"`
	// DelaySeconds 是发送动作的延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
}

// publishReviewRequestCfg 是批量发布求评价配置 DTO。
type publishReviewRequestCfg struct {
	// Enabled 表示是否启用求评价。
	Enabled bool `json:"enabled"`
	// AfterShippedHours 是发货后的等待小时数。
	AfterShippedHours int `json:"after_shipped_hours"`
	// Message 是发送给买家的求评价文案。
	Message string `json:"message"`
	// MaxAttempts 是最多提醒次数。
	MaxAttempts int `json:"max_attempts"`
	// DelaySeconds 是提醒动作的延迟秒数。
	DelaySeconds int `json:"delay_seconds"`
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
	Category publishCategoryResponse `json:"category"`
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
	// PublishIntervalSeconds 是相邻两次最终商品发布请求的最小间隔秒数。
	PublishIntervalSeconds int `json:"publish_interval_seconds"`
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

// newDefaultReplyResponse 将默认回复应用模型转换为 HTTP DTO。
func newDefaultReplyResponse(cookieID string, reply defaultreplyapp.Reply) defaultReplyResponse {
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

// settingsEntryDTO 是单条设置键值的具名 DTO；仅在 HTTP 边界内部用于稳定排序和校验。
type settingsEntryDTO struct {
	// Key 是设置项的稳定键名。
	Key string `json:"key"`
	// Value 是设置项的脱敏文本值。
	Value string `json:"value"`
}

// settingsResponse 是设置查询的具名响应 DTO；序列化时保持旧客户端的顶层键值对象形状。
type settingsResponse struct {
	// Entries 保存设置键值，避免 DTO 直接暴露动态 map 字段。
	Entries []settingsEntryDTO `json:"-"`
}

// newSettingsResponse 将应用层动态设置键值转换为具名 HTTP 响应 DTO。
func newSettingsResponse(values map[string]string) settingsResponse {
	// entries 保存待编码的设置项；顺序不影响兼容 JSON 对象，但便于测试和日志稳定。
	entries := make([]settingsEntryDTO, 0, len(values))
	// key 是当前设置名称；value 是对应的非敏感配置值。
	for key, value := range values {
		entries = append(entries, settingsEntryDTO{Key: key, Value: value})
	}
	return settingsResponse{Entries: entries}
}

// MarshalJSON 保持设置接口原有的顶层键值对象契约，同时隐藏内部动态字段实现。
func (response settingsResponse) MarshalJSON() ([]byte, error) {
	// values 保存兼容旧客户端的顶层设置对象。
	values := make(map[string]string, len(response.Entries))
	// entry 是当前待恢复为顶层设置字段的具名条目。
	for _, entry := range response.Entries {
		values[entry.Key] = entry.Value
	}
	return json.Marshal(values)
}

// UnmarshalJSON 将旧客户端顶层设置对象解析为具名条目，供契约测试和边界适配使用。
func (response *settingsResponse) UnmarshalJSON(data []byte) error {
	// values 保存收到的顶层设置对象；设置值均按字符串处理以避免秘密类型扩散。
	var values map[string]string
	// err 表示旧版顶层设置对象反序列化失败的原因。
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	*response = newSettingsResponse(values)
	return nil
}

// qrLoginStatusResponse 是二维码状态的稳定响应 DTO，仅暴露非敏感状态和持久化结果。
type qrLoginStatusResponse struct {
	// Success 表示当前扫码状态请求是否正常完成。
	Success bool `json:"success,omitempty"`
	// Status 是扫码会话当前状态。
	Status string `json:"status"`
	// Message 是可选的平台提示文本。
	Message string `json:"message,omitempty"`
	// VerificationScreenshot 是平台风控验证页面的非敏感截图兜底地址。
	VerificationScreenshot string `json:"verification_screenshot,omitempty"`
	// FaceQRURL 是人脸风控验证二维码的图片地址，前端优先使用它引导用户扫码。
	FaceQRURL string `json:"face_qr_url,omitempty"`
	// SessionID 是可选的扫码会话标识。
	SessionID string `json:"session_id,omitempty"`
	// AccountID 是扫码成功后创建或更新的本地账号标识。
	AccountID string `json:"account_id,omitempty"`
	// IsNewAccount 表示扫码成功后是否新建本地账号。
	IsNewAccount bool `json:"is_new_account,omitempty"`
}

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
