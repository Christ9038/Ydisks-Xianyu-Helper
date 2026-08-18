package chat

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// fakeRepository 是验证聊天服务窄 repository 依赖的内存替身。
type fakeRepository struct {
	// accountIDs 保存模拟的用户账号归属结果。
	accountIDs []string
}

// ListOwnedIDs 返回内存替身中的账号归属结果。
func (r *fakeRepository) ListOwnedIDs(context.Context, int64) ([]string, error) {
	return r.accountIDs, nil
}

// DeleteSession 模拟删除聊天会话。
func (*fakeRepository) DeleteSession(context.Context, string, string) error { return nil }

// UpsertSession 模拟写入聊天会话。
func (*fakeRepository) UpsertSession(context.Context, db.ChatSession) error { return nil }

// SyncSessionSummary 模拟同步聊天会话摘要。
func (*fakeRepository) SyncSessionSummary(context.Context, string, string, string, int64, int64, int) error {
	return nil
}

// SaveMessage 模拟幂等保存聊天消息。
func (*fakeRepository) SaveMessage(_ context.Context, _ db.ChatSession, message db.ChatMessage, _ bool) (*db.ChatMessage, bool, error) {
	return &message, true, nil
}

// UpdateMessageType 模拟更新历史消息类型。
func (*fakeRepository) UpdateMessageType(context.Context, string, string, string) error { return nil }

// UpdateMessageStatus 模拟更新外发消息状态。
func (*fakeRepository) UpdateMessageStatus(_ context.Context, _ string, _ string, status string) (*db.ChatMessage, error) {
	return &db.ChatMessage{Status: status}, nil
}

// CountUnreadUserMessages 为窄仓储测试提供空的真实用户未读统计结果。
func (*fakeRepository) CountUnreadUserMessages(context.Context, string, string) (int, error) {
	return 0, nil
}

// MarkMessageRead 为窄仓储测试模拟指定出站消息的已读更新。
func (*fakeRepository) MarkMessageRead(_ context.Context, _ string, key string, readAt int64) (*db.ChatMessage, error) {
	return &db.ChatMessage{MessageKey: key, ReadStatus: 2, ReadAt: readAt}, nil
}

// MarkLatestOutgoingRead 为窄仓储测试模拟缺失消息键时的会话级回退更新。
func (*fakeRepository) MarkLatestOutgoingRead(_ context.Context, _ string, chatID string, readAt int64) (*db.ChatMessage, error) {
	return &db.ChatMessage{ChatID: chatID, ReadStatus: 2, ReadAt: readAt}, nil
}

// TestServiceUsesNarrowRepository 验证聊天服务可以脱离完整 db.Store 运行。
func TestServiceUsesNarrowRepository(t *testing.T) {
	// repository 是只实现聊天所需方法的内存替身。
	repository := &fakeRepository{accountIDs: []string{"account-1", "account-2"}}
	// service 用于本次流程后续判断的service
	service := NewWithRepository(repository)
	// cancel、err 用于本次流程后续判断的cancel、err
	_, cancel, err := service.Subscribe(context.Background(), 42)
	if err != nil {
		t.Fatalf("Subscribe() error = %v", err)
	}
	cancel()
}
