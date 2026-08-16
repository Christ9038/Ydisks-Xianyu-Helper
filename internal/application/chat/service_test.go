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
	// deleteErr 是模拟空会话清理失败的错误。
	deleteErr error
	// updateErr 是模拟会话身份缓存写入失败的错误。
	updateErr error
	// owned 是模拟账号归属查询结果。
	owned bool
	// ownedErr 是模拟账号归属查询失败的错误。
	ownedErr error
	// updatedSessions 保存最近写入的会话身份，供断言应用端口调用参数。
	updatedSessions []Session
	// markReadErr 表示会话已读状态更新需要返回的错误。
	markReadErr error
	// markReadCalls 记录会话已读状态更新次数，避免测试误判未调用端口。
	markReadCalls int
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

// DeleteEmptySessions 返回预设清理结果并记录端口已被调用。
func (r *fakeRepository) DeleteEmptySessions(_ context.Context, _ string) error {
	return r.deleteErr
}

// UpdateSessionIdentity 记录应用层请求保存的会话身份。
func (r *fakeRepository) UpdateSessionIdentity(_ context.Context, accountID, chatID, buyerID, buyerName, buyerAvatar string) error {
	r.updatedSessions = append(r.updatedSessions, Session{AccountID: accountID, ChatID: chatID, BuyerID: buyerID, BuyerName: buyerName, BuyerAvatar: buyerAvatar})
	return r.updateErr
}

// ExistsOwned 返回预设账号归属结果。
func (r *fakeRepository) ExistsOwned(_ context.Context, _ int64, _ string) (bool, error) {
	return r.owned, r.ownedErr
}

// MarkRead 记录会话已读更新并返回预设错误。
func (r *fakeRepository) MarkRead(_ context.Context, _ int64, _, _ string) error {
	r.markReadCalls++
	return r.markReadErr
}

// fakeIdentityResolver 返回预设平台展示身份，不接触真实凭证或网络。
type fakeIdentityResolver struct {
	// identity 是模拟平台返回的展示身份。
	identity Identity
	// err 是模拟平台身份查询失败的错误。
	err error
}

// historyOnlyRepository 仅实现历史读取端口，用于验证可选会话维护能力缺失时的错误。
type historyOnlyRepository struct{}

// ListMessages 返回空历史，满足历史读取端口的测试替身契约。
func (historyOnlyRepository) ListMessages(context.Context, int64, string, string, int64, int) ([]Message, error) {
	return nil, nil
}

// ListSessions 返回空会话列表，满足历史读取端口的测试替身契约。
func (historyOnlyRepository) ListSessions(context.Context, int64, string, int) ([]Session, error) {
	return nil, nil
}

// Resolve 返回预设的非敏感身份结果。
func (r fakeIdentityResolver) Resolve(_ context.Context, _, _ string) (Identity, error) {
	return r.identity, r.err
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
	service := New(historyOnlyRepository{})
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

// TestSessionPortCoversCleanupOwnershipAndIdentity 验证会话维护和身份补全都通过应用端口完成。
func TestSessionPortCoversCleanupOwnershipAndIdentity(t *testing.T) {
	// repository 是记录端口调用的测试仓储。
	repository := &fakeRepository{owned: true}
	// service 是带平台身份替身的聊天应用服务。
	service := NewWithIdentity(repository, fakeIdentityResolver{identity: Identity{BuyerName: "买家新名", BuyerAvatar: "avatar-new"}})
	// err 表示清理空会话时的应用层错误。
	if err := service.CleanupEmptySessions(context.Background(), "account-1"); err != nil {
		t.Fatalf("CleanupEmptySessions() error = %v", err)
	}
	// owned 和 err 保存账号归属端口结果。
	owned, err := service.OwnsAccount(context.Background(), 8, "account-1")
	if err != nil || !owned {
		t.Fatalf("OwnsAccount() = %v, %v", owned, err)
	}
	// resolved 和 resolveErr 保存身份补全后的会话及平台错误。
	resolved, resolveErr := service.ResolveSessionIdentity(context.Background(), Session{AccountID: "account-1", ChatID: "chat-1", BuyerID: "buyer-1"})
	if resolveErr != nil || resolved.BuyerName != "买家新名" || len(repository.updatedSessions) != 1 {
		t.Fatalf("ResolveSessionIdentity() = %+v, %v; updates=%+v", resolved, resolveErr, repository.updatedSessions)
	}
}

// TestSessionPortPreservesIdentityErrorAndCachedSession 验证平台身份失败时仍保留会话并返回错误供恢复流程处理。
func TestSessionPortPreservesIdentityErrorAndCachedSession(t *testing.T) {
	// wantErr 是模拟平台会话失效的错误。
	wantErr := errors.New("session expired")
	// repository 是接收身份缓存写入的测试仓储。
	repository := &fakeRepository{}
	// service 是返回平台错误的聊天应用服务。
	service := NewWithIdentity(repository, fakeIdentityResolver{err: wantErr})
	// session 和 err 保存身份失败后的会话及错误。
	session, err := service.ResolveSessionIdentity(context.Background(), Session{AccountID: "account-1", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "旧名称"})
	if !errors.Is(err, wantErr) || session.BuyerName != "旧名称" || len(repository.updatedSessions) != 1 {
		t.Fatalf("session=%+v err=%v updates=%+v", session, err, repository.updatedSessions)
	}
}

// TestRefreshSessionIdentitiesKeepsOfficialSessionAndUpdatesPeers 验证批量身份补全保持闲小蜜会话并更新普通联系人。
func TestRefreshSessionIdentitiesKeepsOfficialSessionAndUpdatesPeers(t *testing.T) {
	// repository 是记录身份缓存写入的测试仓储。
	repository := &fakeRepository{}
	// service 是返回统一买家身份的批量补全服务。
	service := NewWithIdentity(repository, fakeIdentityResolver{identity: Identity{BuyerName: "批量名称", BuyerAvatar: "批量头像"}})
	// sessions 是包含普通联系人和官方会话的测试列表。
	sessions := []Session{
		{AccountID: "account-1", ChatID: "chat-1", BuyerID: "buyer-1"},
		{AccountID: "account-1", ChatID: "chat-official", BuyerID: "1400", BuyerName: "闲小蜜"},
	}
	// refreshed 和 refreshErr 保存批量补全结果及首个错误。
	refreshed, refreshErr := service.RefreshSessionIdentities(context.Background(), "account-1", sessions)
	if refreshErr != nil || refreshed[0].BuyerName != "批量名称" || refreshed[1].BuyerName != "闲小蜜" || len(repository.updatedSessions) != 1 {
		t.Fatalf("refreshed=%+v err=%v updates=%+v", refreshed, refreshErr, repository.updatedSessions)
	}
}

// TestSessionPortRejectsMissingCapabilities 验证未装配会话维护能力时不会伪装成成功。
func TestSessionPortRejectsMissingCapabilities(t *testing.T) {
	// service 是只实现历史查询的应用服务。
	service := New(historyOnlyRepository{})
	// err 保存缺失会话维护端口时的应用错误。
	err := service.CleanupEmptySessions(context.Background(), "account-1")
	if !errors.Is(err, ErrSessionUnavailable) {
		t.Fatalf("CleanupEmptySessions() error = %v, want %v", err, ErrSessionUnavailable)
	}
}

// TestMarkReadValidatesAndPropagatesPortErrors 验证已读用例的输入校验、能力缺失和底层错误分支。
func TestMarkReadValidatesAndPropagatesPortErrors(t *testing.T) {
	// wantErr 是模拟会话已读写入失败的稳定错误。
	wantErr := errors.New("mark read failed")
	// repository 是记录已读调用并返回预设错误的测试端口。
	repository := &fakeRepository{markReadErr: wantErr}
	// service 是使用会话维护端口构造的聊天应用服务。
	service := New(repository)
	// err 保存有效请求透传的底层错误。
	if err := service.MarkRead(context.Background(), 7, " account-1 ", " chat-1 "); !errors.Is(err, wantErr) || repository.markReadCalls != 1 {
		t.Fatalf("MarkRead() error=%v calls=%d", err, repository.markReadCalls)
	}
	// invalidErr 保存空会话标识返回的应用输入错误。
	if invalidErr := service.MarkRead(context.Background(), 7, "account-1", ""); !errors.Is(invalidErr, ErrInvalidInput) {
		t.Fatalf("invalid MarkRead() error=%v", invalidErr)
	}
	// unavailableErr 保存仅实现历史读取端口时的能力缺失错误。
	if unavailableErr := New(historyOnlyRepository{}).MarkRead(context.Background(), 7, "account-1", "chat-1"); !errors.Is(unavailableErr, ErrSessionUnavailable) {
		t.Fatalf("unavailable MarkRead() error=%v", unavailableErr)
	}
}
