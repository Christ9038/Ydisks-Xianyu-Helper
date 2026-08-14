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
	// UpdateMessageType 更新历史消息的展示类型。
	UpdateMessageType(ctx context.Context, cookieID, key, messageType string) error
	// UpdateMessageStatus 更新外发消息状态并返回最新消息。
	UpdateMessageStatus(ctx context.Context, cookieID, key, status string) (*db.ChatMessage, error)
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

// UpdateMessageType 委托历史消息类型更新。
func (r storeRepository) UpdateMessageType(ctx context.Context, cookieID, key, messageType string) error {
	return r.store.Chats.UpdateMessageType(ctx, cookieID, key, messageType)
}

// UpdateMessageStatus 委托外发消息状态更新。
func (r storeRepository) UpdateMessageStatus(ctx context.Context, cookieID, key, status string) (*db.ChatMessage, error) {
	return r.store.Chats.UpdateMessageStatus(ctx, cookieID, key, status)
}

// newStoreRepository 从完整 Store 构造聊天服务使用的窄 repository。
func newStoreRepository(store *db.Store) Repository {
	if store == nil || store.Cookies == nil || store.Chats == nil {
		return nil
	}
	return storeRepository{store: store}
}
