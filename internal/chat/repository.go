package chat

import (
	"context"

	"xianyu-go/internal/db"
)

// Repository 定义聊天服务需要的最小持久化能力，避免业务层持有完整 db.Store。
type Repository interface {
	// ListOwnedIDs 返回用户拥有的账号 ID。
	ListOwnedIDs(ctx context.Context, userID int64) ([]string, error)
	// DeleteSession 删除指定账号下的聊天会话。
	DeleteSession(ctx context.Context, cookieID, chatID string) error
	// UpsertSession 写入或更新聊天会话摘要。
	UpsertSession(ctx context.Context, session db.ChatSession) error
	// SyncSessionSummary 按服务端时间更新聊天会话摘要。
	SyncSessionSummary(ctx context.Context, cookieID, chatID, summary string, sentAt, observedModifyAt int64, unread int) error
	// SaveMessage 幂等保存聊天消息并返回落库结果。
	SaveMessage(ctx context.Context, session db.ChatSession, message db.ChatMessage, unread bool) (*db.ChatMessage, bool, error)
	// UpdateMessageContent 用历史接口返回的富媒体分类和地址纠正已有占位消息。
	UpdateMessageContent(ctx context.Context, cookieID, key, messageType, content string) error
	// UpdateMessageMediaDuration 用历史接口返回的秒级时长补齐已有富媒体消息。
	UpdateMessageMediaDuration(ctx context.Context, cookieID, key string, duration int64) error
	// UpdateMessageStatus 更新外发消息状态并返回最新消息。
	UpdateMessageStatus(ctx context.Context, cookieID, key, status string) (*db.ChatMessage, error)
	// CountUnreadUserMessages 返回当前会话中非系统入站消息的未读数量。
	CountUnreadUserMessages(ctx context.Context, cookieID, chatID string) (int, error)
	// MarkMessageRead 根据平台回执标记指定出站消息已读。
	MarkMessageRead(ctx context.Context, cookieID, key string, readAt int64) (*db.ChatMessage, error)
	// MarkLatestOutgoingRead 在回执缺失消息键时标记会话最近一条未读出站消息。
	MarkLatestOutgoingRead(ctx context.Context, cookieID, chatID string, readAt int64) (*db.ChatMessage, error)
}

// storeRepository 将聚合 Store 的聊天相关 repository 适配为窄接口。
type storeRepository struct {
	// store 保存数据库聚合入口，仅用于构造适配器，不进入聊天服务状态。
	store *db.Store
}

// ListOwnedIDs 委托账号归属查询。
func (r storeRepository) ListOwnedIDs(ctx context.Context, userID int64) ([]string, error) {
	return r.store.Cookies.ListOwnedIDs(ctx, userID)
}

// DeleteSession 委托聊天会话删除。
func (r storeRepository) DeleteSession(ctx context.Context, cookieID, chatID string) error {
	return r.store.Chats.DeleteSession(ctx, cookieID, chatID)
}

// UpsertSession 委托聊天会话写入。
func (r storeRepository) UpsertSession(ctx context.Context, session db.ChatSession) error {
	return r.store.Chats.UpsertSession(ctx, session)
}

// SyncSessionSummary 委托聊天会话摘要同步。
func (r storeRepository) SyncSessionSummary(ctx context.Context, cookieID, chatID, summary string, sentAt, observedModifyAt int64, unread int) error {
	return r.store.Chats.SyncSessionSummary(ctx, cookieID, chatID, summary, sentAt, observedModifyAt, unread)
}

// SaveMessage 委托聊天消息幂等保存。
func (r storeRepository) SaveMessage(ctx context.Context, session db.ChatSession, message db.ChatMessage, unread bool) (*db.ChatMessage, bool, error) {
	return r.store.Chats.SaveMessage(ctx, session, message, unread)
}

// UpdateMessageContent 委托历史消息富媒体分类与内容更新。
func (r storeRepository) UpdateMessageContent(ctx context.Context, cookieID, key, messageType, content string) error {
	return r.store.Chats.UpdateMessageContent(ctx, cookieID, key, messageType, content)
}

// UpdateMessageMediaDuration 委托历史消息秒级时长更新。
func (r storeRepository) UpdateMessageMediaDuration(ctx context.Context, cookieID, key string, duration int64) error {
	return r.store.Chats.UpdateMessageMediaDuration(ctx, cookieID, key, duration)
}

// UpdateMessageStatus 委托外发消息状态更新。
func (r storeRepository) UpdateMessageStatus(ctx context.Context, cookieID, key, status string) (*db.ChatMessage, error) {
	return r.store.Chats.UpdateMessageStatus(ctx, cookieID, key, status)
}

// CountUnreadUserMessages 委托消息级真实用户未读数统计。
func (r storeRepository) CountUnreadUserMessages(ctx context.Context, cookieID, chatID string) (int, error) {
	return r.store.Chats.CountUnreadUserMessages(ctx, cookieID, chatID)
}

// MarkMessageRead 委托指定出站消息的已读回执写入。
func (r storeRepository) MarkMessageRead(ctx context.Context, cookieID, key string, readAt int64) (*db.ChatMessage, error) {
	return r.store.Chats.MarkMessageRead(ctx, cookieID, key, readAt)
}

// MarkLatestOutgoingRead 委托会话级出站消息已读回退写入。
func (r storeRepository) MarkLatestOutgoingRead(ctx context.Context, cookieID, chatID string, readAt int64) (*db.ChatMessage, error) {
	return r.store.Chats.MarkLatestOutgoingRead(ctx, cookieID, chatID, readAt)
}

// newStoreRepository 从完整 Store 构造聊天服务使用的窄 repository。
func newStoreRepository(store *db.Store) Repository {
	if store == nil || store.Cookies == nil || store.Chats == nil {
		return nil
	}
	return storeRepository{store: store}
}
