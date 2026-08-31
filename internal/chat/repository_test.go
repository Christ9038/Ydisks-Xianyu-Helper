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
	// ownedErr 保存账号归属查询应返回的错误。
	ownedErr error
}

// ListOwnedIDs 返回内存替身中的账号归属结果。
func (r *fakeRepository) ListOwnedIDs(context.Context, int64) ([]string, error) {
	return r.accountIDs, r.ownedErr
}

// outgoingErrorRepository 用于注入出站状态更新错误，验证聊天服务不会把未知错误误判为官方回显。
type outgoingErrorRepository struct {
	fakeRepository
	// statusErr 保存出站状态更新应返回的错误。
	statusErr error
}

// UpdateMessageStatus 返回预置的出站状态错误。
func (r *outgoingErrorRepository) UpdateMessageStatus(context.Context, string, string, string) (*db.ChatMessage, error) {
	return nil, r.statusErr
}

// SetSessionVisible 模拟更新会话列表可见状态，并保持消息历史不受影响。
func (*fakeRepository) SetSessionVisible(context.Context, string, string, bool) error { return nil }

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

// UpdateMessageContent 模拟更新历史消息的富媒体分类和地址。
func (*fakeRepository) UpdateMessageContent(context.Context, string, string, string, string) error {
	return nil
}

// UpdateMessageMediaDuration 模拟更新历史语音的秒级时长。
func (*fakeRepository) UpdateMessageMediaDuration(context.Context, string, string, int64) error {
	return nil
}

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
