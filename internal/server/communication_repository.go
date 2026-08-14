package server

import (
	"context"

	"xianyu-go/internal/db"
)

// communicationRepository 定义通信应用服务隔离账号任务、通知绑定和聊天历史所需的最小持久化能力。
type communicationRepository interface {
	// GetAccountTaskSettings 读取账号任务设置。
	GetAccountTaskSettings(ctx context.Context, cookieID string) (db.AccountTaskSettings, error)
	// UpsertAccountTaskSettings 保存账号任务设置。
	UpsertAccountTaskSettings(ctx context.Context, settings db.AccountTaskSettings) error
	// ListAccountTaskRuns 查询账号任务运行记录。
	ListAccountTaskRuns(ctx context.Context, cookieID string, limit int) ([]db.AccountTaskRun, error)
	// ListNotificationChannels 查询用户拥有的通知渠道。
	ListNotificationChannels(ctx context.Context, userID int64) ([]db.NotificationChannelRow, error)
	// CreateNotificationChannel 创建通知渠道。
	CreateNotificationChannel(ctx context.Context, row *db.NotificationChannelRow) (int64, error)
	// UpdateNotificationChannel 更新用户拥有的通知渠道。
	UpdateNotificationChannel(ctx context.Context, row *db.NotificationChannelRow, userID int64) error
	// GetNotificationChannel 查询用户拥有的单个通知渠道。
	GetNotificationChannel(ctx context.Context, channelID, userID int64) (*db.NotificationChannelRow, error)
	// DeleteNotificationChannel 删除用户拥有的通知渠道。
	DeleteNotificationChannel(ctx context.Context, channelID, userID int64) error
	// GetNotificationChannelConfig 查询通知渠道发送配置。
	GetNotificationChannelConfig(ctx context.Context, channelID int64) (*db.NotificationChannel, error)
	// ListNotificationBindings 查询用户账号通知绑定。
	ListNotificationBindings(ctx context.Context, userID int64) ([]db.NotificationBindingRow, error)
	// SetNotificationBindings 覆盖保存账号通知绑定。
	SetNotificationBindings(ctx context.Context, cookieID string, channelIDs []int64) error
	// GetNotificationBindingIDs 查询账号当前启用的通知渠道。
	GetNotificationBindingIDs(ctx context.Context, cookieID string) ([]int64, error)
	// SetSingleNotificationBinding 更新单个通知绑定状态。
	SetSingleNotificationBinding(ctx context.Context, cookieID string, channelID int64, enabled bool) error
	// DeleteNotificationBinding 删除用户账号的一条通知绑定。
	DeleteNotificationBinding(ctx context.Context, userID, bindingID int64) error
	// DeleteAccountNotificationBindings 删除用户账号的全部通知绑定。
	DeleteAccountNotificationBindings(ctx context.Context, userID int64, cookieID string) error
	// GetCookieValue 读取账号 Cookie 明文。
	GetCookieValue(ctx context.Context, cookieID string) (string, error)
	// UpdateCookieValue 更新已存在账号的 Cookie 明文。
	UpdateCookieValue(ctx context.Context, cookieID, value string) error
	// MarkChatRead 将聊天会话标记为已读。
	MarkChatRead(ctx context.Context, userID int64, accountID, chatID string) error
	// ListChatMessages 查询聊天历史消息。
	ListChatMessages(ctx context.Context, userID int64, accountID, chatID string, beforeID int64, limit int) ([]db.ChatMessage, error)
	// ListChatSessions 查询账号聊天会话列表。
	ListChatSessions(ctx context.Context, userID int64, accountID string, limit int) ([]db.ChatSession, error)
}

// storeCommunicationRepository 将完整 Store 适配为通信应用服务窄 repository。
type storeCommunicationRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// GetAccountTaskSettings 委托账号任务设置查询。
func (r storeCommunicationRepository) GetAccountTaskSettings(ctx context.Context, cookieID string) (db.AccountTaskSettings, error) {
	return r.store.AccountTasks.Get(ctx, cookieID)
}

// UpsertAccountTaskSettings 委托账号任务设置写入。
func (r storeCommunicationRepository) UpsertAccountTaskSettings(ctx context.Context, settings db.AccountTaskSettings) error {
	return r.store.AccountTasks.Upsert(ctx, settings)
}

// ListAccountTaskRuns 委托账号任务运行记录查询。
func (r storeCommunicationRepository) ListAccountTaskRuns(ctx context.Context, cookieID string, limit int) ([]db.AccountTaskRun, error) {
	return r.store.AccountTasks.RecentRuns(ctx, cookieID, limit)
}

// ListNotificationChannels 委托用户通知渠道查询。
func (r storeCommunicationRepository) ListNotificationChannels(ctx context.Context, userID int64) ([]db.NotificationChannelRow, error) {
	return r.store.Notifications.AllChannelsForUser(ctx, userID)
}

// CreateNotificationChannel 委托通知渠道创建。
func (r storeCommunicationRepository) CreateNotificationChannel(ctx context.Context, row *db.NotificationChannelRow) (int64, error) {
	return r.store.Notifications.CreateChannel(ctx, row)
}

// UpdateNotificationChannel 委托通知渠道更新。
func (r storeCommunicationRepository) UpdateNotificationChannel(ctx context.Context, row *db.NotificationChannelRow, userID int64) error {
	return r.store.Notifications.UpdateChannelForUser(ctx, row, userID)
}

// GetNotificationChannel 委托带用户归属的通知渠道查询。
func (r storeCommunicationRepository) GetNotificationChannel(ctx context.Context, channelID, userID int64) (*db.NotificationChannelRow, error) {
	return r.store.Notifications.GetChannelRowForUser(ctx, channelID, userID)
}

// DeleteNotificationChannel 委托通知渠道删除。
func (r storeCommunicationRepository) DeleteNotificationChannel(ctx context.Context, channelID, userID int64) error {
	return r.store.Notifications.DeleteChannelForUser(ctx, channelID, userID)
}

// GetNotificationChannelConfig 委托通知渠道发送配置查询。
func (r storeCommunicationRepository) GetNotificationChannelConfig(ctx context.Context, channelID int64) (*db.NotificationChannel, error) {
	return r.store.Notifications.GetChannel(ctx, channelID)
}

// ListNotificationBindings 委托通知绑定查询。
func (r storeCommunicationRepository) ListNotificationBindings(ctx context.Context, userID int64) ([]db.NotificationBindingRow, error) {
	return r.store.Notifications.ListBindingsForUser(ctx, userID)
}

// SetNotificationBindings 委托通知绑定覆盖写入。
func (r storeCommunicationRepository) SetNotificationBindings(ctx context.Context, cookieID string, channelIDs []int64) error {
	return r.store.Notifications.SetBindings(ctx, cookieID, channelIDs)
}

// GetNotificationBindingIDs 委托账号通知绑定查询。
func (r storeCommunicationRepository) GetNotificationBindingIDs(ctx context.Context, cookieID string) ([]int64, error) {
	return r.store.Notifications.AccountBindings(ctx, cookieID)
}

// SetSingleNotificationBinding 委托单个通知绑定状态更新。
func (r storeCommunicationRepository) SetSingleNotificationBinding(ctx context.Context, cookieID string, channelID int64, enabled bool) error {
	return r.store.Notifications.SetSingleBinding(ctx, cookieID, channelID, enabled)
}

// DeleteNotificationBinding 委托通知绑定删除。
func (r storeCommunicationRepository) DeleteNotificationBinding(ctx context.Context, userID, bindingID int64) error {
	return r.store.Notifications.DeleteBinding(ctx, userID, bindingID)
}

// DeleteAccountNotificationBindings 委托账号通知绑定删除。
func (r storeCommunicationRepository) DeleteAccountNotificationBindings(ctx context.Context, userID int64, cookieID string) error {
	return r.store.Notifications.DeleteAccountBindings(ctx, userID, cookieID)
}

// GetCookieValue 委托账号 Cookie 查询。
func (r storeCommunicationRepository) GetCookieValue(ctx context.Context, cookieID string) (string, error) {
	return r.store.Cookies.GetValue(ctx, cookieID)
}

// UpdateCookieValue 委托账号 Cookie 更新。
func (r storeCommunicationRepository) UpdateCookieValue(ctx context.Context, cookieID, value string) error {
	return r.store.Cookies.UpdateValueExisting(ctx, cookieID, value)
}

// MarkChatRead 委托聊天已读状态更新。
func (r storeCommunicationRepository) MarkChatRead(ctx context.Context, userID int64, accountID, chatID string) error {
	return r.store.Chats.MarkRead(ctx, userID, accountID, chatID)
}

// ListChatMessages 委托聊天消息查询。
func (r storeCommunicationRepository) ListChatMessages(ctx context.Context, userID int64, accountID, chatID string, beforeID int64, limit int) ([]db.ChatMessage, error) {
	return r.store.Chats.ListMessages(ctx, userID, accountID, chatID, beforeID, limit)
}

// ListChatSessions 委托聊天会话查询。
func (r storeCommunicationRepository) ListChatSessions(ctx context.Context, userID int64, accountID string, limit int) ([]db.ChatSession, error) {
	return r.store.Chats.ListSessions(ctx, userID, accountID, limit)
}

// newStoreCommunicationRepository 从完整 Store 构造通信应用服务窄 repository。
func newStoreCommunicationRepository(store *db.Store) communicationRepository {
	if store == nil || store.AccountTasks == nil || store.Notifications == nil || store.Cookies == nil || store.Chats == nil {
		return nil
	}
	return storeCommunicationRepository{store: store}
}

// 确保 Store 适配器始终覆盖通信应用服务所需的全部能力。
var _ communicationRepository = storeCommunicationRepository{}
