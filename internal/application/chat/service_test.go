package chat

import (
	"context"
	"errors"
	"testing"
)

// fakeRepository 保存测试聊天历史数据，并记录最近一次用户归属参数。
type fakeRepository struct {
	// messages 是模拟聊天消息列表。
	messages []Message
	// sessions 是模拟会话摘要列表。
	sessions []Session
	// messageErr 是消息查询需要返回的错误。
	messageErr error
	// sessionErr 是会话查询需要返回的错误。
	sessionErr error
	// userID 保存最近一次消息查询使用的用户 ID。
	userID int64
	// allowedUserID 是允许读取测试账号的用户 ID；其他用户模拟归属拒绝。
	allowedUserID int64
}

// ListMessages 返回测试消息，并记录用户归属参数。
func (r *fakeRepository) ListMessages(_ context.Context, userID int64, _ string, _ string, _ int64, _ int) ([]Message, error) {
	r.userID = userID
	if r.allowedUserID != 0 && userID != r.allowedUserID {
		return nil, errors.New("cross-user access denied")
	}
	return r.messages, r.messageErr
}

// ListSessions 返回测试会话摘要或预设错误。
func (r *fakeRepository) ListSessions(_ context.Context, _ int64, _ string, _ int) ([]Session, error) {
	return r.sessions, r.sessionErr
}

// TestListStoredMessagesUsesUserScopedPort 验证查询会把用户 ID 传到归属端口并组装分页结果。
func TestListStoredMessagesUsesUserScopedPort(t *testing.T) {
	// repository 是带有一条消息和会话摘要的测试端口。
	repository := &fakeRepository{
		messages: []Message{{ID: 1, ChatID: "chat-1", Content: "你好"}},
		sessions: []Session{{ChatID: "chat-1", BuyerName: "买家甲"}},
	}
	// service 是使用测试端口构造的聊天历史服务。
	service := New(repository)
	// page 保存当前用户读取到的分页结果。
	page, err := service.ListStoredMessages(context.Background(), 42, "account-1", "chat-1", 0, 1)
	if err != nil {
		t.Fatalf("ListStoredMessages() error = %v", err)
	}
	if repository.userID != 42 || len(page.Messages) != 1 || page.Session.BuyerName != "买家甲" || !page.HasMore {
		t.Fatalf("unexpected page=%+v userID=%d", page, repository.userID)
	}
}

// TestListStoredMessagesRejectsInvalidIdentity 验证无效用户或账号标识会在访问端口前失败。
func TestListStoredMessagesRejectsInvalidIdentity(t *testing.T) {
	// service 是不应被调用的测试服务。
	service := New(&fakeRepository{})
	// testCase 表示当前待验证的无效身份场景。
	for _, testCase := range []struct {
		// name 是当前无效输入场景名称。
		name string
		// userID 是当前请求用户标识。
		userID int64
		// accountID 是当前账号标识。
		accountID string
		// chatID 是当前会话标识。
		chatID string
	}{
		{name: "missing-user", userID: 0, accountID: "account-1", chatID: "chat-1"},
		{name: "missing-account", userID: 1, accountID: "", chatID: "chat-1"},
		{name: "missing-chat", userID: 1, accountID: "account-1", chatID: ""},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			// err 保存当前无效请求返回的应用错误。
			_, err := service.ListStoredMessages(context.Background(), testCase.userID, testCase.accountID, testCase.chatID, 0, 20)
			if !errors.Is(err, ErrInvalidInput) {
				t.Fatalf("error = %v, want ErrInvalidInput", err)
			}
		})
	}
}

// TestListStoredMessagesRejectsUnavailableService 验证未装配端口或空服务不会触发 panic。
func TestListStoredMessagesRejectsUnavailableService(t *testing.T) {
	// nilService 表示尚未完成依赖装配的聊天服务指针。
	var nilService *Service
	// service 表示当前待验证的不可用聊天服务实例。
	for _, service := range []*Service{nilService, New(nil)} {
		// err 保存不可用服务返回的应用错误。
		_, err := service.ListStoredMessages(context.Background(), 1, "account-1", "chat-1", 0, 20)
		if !errors.Is(err, ErrInvalidInput) {
			t.Fatalf("error = %v, want ErrInvalidInput", err)
		}
	}
}

// TestListStoredMessagesPropagatesRepositoryFailure 验证消息持久化失败不会被伪装成成功空页。
func TestListStoredMessagesPropagatesRepositoryFailure(t *testing.T) {
	// wantErr 是模拟底层查询失败的稳定错误。
	wantErr := errors.New("repository unavailable")
	// service 是返回预设错误的聊天历史服务。
	service := New(&fakeRepository{messageErr: wantErr})
	// _, err 保存应用服务返回的分页结果和错误。
	_, err := service.ListStoredMessages(context.Background(), 7, "account-1", "chat-1", 0, 20)
	if !errors.Is(err, wantErr) {
		t.Fatalf("error = %v, want %v", err, wantErr)
	}
}

// TestListStoredMessagesKeepsMessagesWhenSessionLookupFails 验证摘要查询失败不会丢弃已成功读取的消息。
func TestListStoredMessagesKeepsMessagesWhenSessionLookupFails(t *testing.T) {
	// service 是消息成功但会话摘要失败的聊天历史服务。
	service := New(&fakeRepository{messages: []Message{{ID: 2, ChatID: "chat-1"}}, sessionErr: errors.New("session unavailable")})
	// page 和 err 保存应用服务返回的消息页及错误。
	page, err := service.ListStoredMessages(context.Background(), 1, "account-1", "chat-1", 0, 20)
	if err != nil || len(page.Messages) != 1 || page.Session.ChatID != "" {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}

// TestListStoredMessagesDoesNotCrossUserBoundary 验证其他用户的账号消息不会被应用服务当作空结果返回。
func TestListStoredMessagesDoesNotCrossUserBoundary(t *testing.T) {
	// service 是只允许用户 7 读取账号的聊天历史服务。
	service := New(&fakeRepository{allowedUserID: 7, messages: []Message{{ID: 1}}})
	// err 保存用户 8 尝试读取用户 7 账号时的归属错误。
	_, err := service.ListStoredMessages(context.Background(), 8, "account-1", "chat-1", 0, 20)
	if err == nil || err.Error() != "cross-user access denied" {
		t.Fatalf("cross-user error = %v", err)
	}
}
