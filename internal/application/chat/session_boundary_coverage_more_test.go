package chat

import (
	"context"
	"errors"
	"testing"
)

// TestSessionQueryAndIdentityRejectInvalidBoundaries 验证会话查询、维护和身份补全接口的空接收者及非法标识边界。
func TestSessionQueryAndIdentityRejectInvalidBoundaries(t *testing.T) {
	// service 是具备完整会话端口的聊天服务。
	service := New(&fakeRepository{owned: true})
	// invalidListErr 保存空账号会话列表查询的输入错误。
	if _, invalidListErr := service.ListSessions(context.Background(), 7, "", 20); !errors.Is(invalidListErr, ErrInvalidInput) {
		t.Fatalf("空账号列表查询错误=%v", invalidListErr)
	}
	// invalidFindErr 保存空账号单会话查询的输入错误。
	if _, invalidFindErr := service.FindSession(context.Background(), 7, "", "chat"); !errors.Is(invalidFindErr, ErrInvalidInput) {
		t.Fatalf("空账号单会话查询错误=%v", invalidFindErr)
	}
	// invalidCleanupErr 保存空账号会话清理的输入错误。
	if invalidCleanupErr := service.CleanupEmptySessions(context.Background(), ""); !errors.Is(invalidCleanupErr, ErrInvalidInput) {
		t.Fatalf("空账号会话清理错误=%v", invalidCleanupErr)
	}
	// invalidOwnsErr 保存零用户 ID 账号归属查询的输入错误。
	if _, invalidOwnsErr := service.OwnsAccount(context.Background(), 0, "account"); !errors.Is(invalidOwnsErr, ErrInvalidInput) {
		t.Fatalf("零用户归属查询错误=%v", invalidOwnsErr)
	}
	// invalidResolveAccountErr 保存身份补全缺少账号标识的输入错误。
	if _, invalidResolveAccountErr := service.ResolveSessionIdentity(context.Background(), Session{ChatID: "chat", BuyerID: "buyer"}); !errors.Is(invalidResolveAccountErr, ErrInvalidInput) {
		t.Fatalf("缺少账号身份补全错误=%v", invalidResolveAccountErr)
	}
	// invalidResolveChatErr 保存身份补全缺少会话标识的输入错误。
	if _, invalidResolveChatErr := service.ResolveSessionIdentity(context.Background(), Session{AccountID: "account", BuyerID: "buyer"}); !errors.Is(invalidResolveChatErr, ErrInvalidInput) {
		t.Fatalf("缺少会话身份补全错误=%v", invalidResolveChatErr)
	}
	// invalidRefreshErr 保存空账号批量身份补全的输入错误。
	if _, invalidRefreshErr := service.RefreshSessionIdentities(context.Background(), "", nil); !errors.Is(invalidRefreshErr, ErrInvalidInput) {
		t.Fatalf("空账号批量身份补全错误=%v", invalidRefreshErr)
	}
	// nilService 表示未初始化的聊天服务指针。
	var nilService *Service
	// nilListErr 保存空服务查询会话列表时的输入错误。
	if _, nilListErr := nilService.ListSessions(context.Background(), 7, "account", 20); !errors.Is(nilListErr, ErrInvalidInput) {
		t.Fatalf("空服务列表查询错误=%v", nilListErr)
	}
	// nilFindErr 保存空服务查询单会话时的输入错误。
	if _, nilFindErr := nilService.FindSession(context.Background(), 7, "account", "chat"); !errors.Is(nilFindErr, ErrInvalidInput) {
		t.Fatalf("空服务单会话查询错误=%v", nilFindErr)
	}
	// nilCleanupErr 保存空服务清理会话时的输入错误。
	if nilCleanupErr := nilService.CleanupEmptySessions(context.Background(), "account"); !errors.Is(nilCleanupErr, ErrInvalidInput) {
		t.Fatalf("空服务会话清理错误=%v", nilCleanupErr)
	}
	// nilOwnsErr 保存空服务查询归属时的输入错误。
	if _, nilOwnsErr := nilService.OwnsAccount(context.Background(), 7, "account"); !errors.Is(nilOwnsErr, ErrInvalidInput) {
		t.Fatalf("空服务归属查询错误=%v", nilOwnsErr)
	}
	// nilResolveErr 保存空服务补全身份时的输入错误。
	if _, nilResolveErr := nilService.ResolveSessionIdentity(context.Background(), Session{AccountID: "account", ChatID: "chat"}); !errors.Is(nilResolveErr, ErrInvalidInput) {
		t.Fatalf("空服务身份补全错误=%v", nilResolveErr)
	}
	// nilRefreshErr 保存空服务批量补全身份时的输入错误。
	if _, nilRefreshErr := nilService.RefreshSessionIdentities(context.Background(), "account", nil); !errors.Is(nilRefreshErr, ErrInvalidInput) {
		t.Fatalf("空服务批量身份补全错误=%v", nilRefreshErr)
	}
	// queryErr 是会话列表仓储需要透传的底层错误。
	queryErr := errors.New("session list failed")
	// queryFailureService 是绑定会话列表失败仓储的聊天服务。
	queryFailureService := New(&fakeRepository{sessionErr: queryErr})
	// queryFailureErr 保存会话列表失败结果。
	if _, queryFailureErr := queryFailureService.ListSessions(context.Background(), 7, "account", 20); !errors.Is(queryFailureErr, queryErr) {
		t.Fatalf("会话列表错误=%v", queryFailureErr)
	}
	// unavailableOwnershipErr 保存缺少会话维护端口时的账号归属错误。
	if _, unavailableOwnershipErr := New(historyOnlyRepository{}).OwnsAccount(context.Background(), 7, "account"); !errors.Is(unavailableOwnershipErr, ErrSessionUnavailable) {
		t.Fatalf("缺少归属端口错误=%v", unavailableOwnershipErr)
	}
	// nilRepositoryService 表示服务对象存在但未装配历史仓储。
	nilRepositoryService := New(nil)
	// nilRepositoryReadID 保存缺少仓储时旧消息标识的解析结果。
	nilRepositoryReadID := nilRepositoryService.ResolveReadMessageID(context.Background(), "account", "chat", "legacy")
	if nilRepositoryReadID != "legacy" {
		t.Fatalf("缺少仓储时旧消息标识应保持原值：%q", nilRepositoryReadID)
	}
	// identityErr 是批量身份补全需要保留的首个平台查询错误。
	identityErr := errors.New("identity lookup failed")
	// identityFailureService 是绑定身份查询失败替身的聊天服务。
	identityFailureService := NewWithIdentity(&fakeRepository{}, fakeIdentityResolver{err: identityErr})
	// refreshedSessions、refreshFailureErr 保存批量身份补全失败结果。
	refreshedSessions, refreshFailureErr := identityFailureService.RefreshSessionIdentities(context.Background(), "account", []Session{{AccountID: "account", ChatID: "chat", BuyerID: "buyer"}})
	if len(refreshedSessions) != 1 || !errors.Is(refreshFailureErr, identityErr) {
		t.Fatalf("批量身份错误 sessions=%+v err=%v", refreshedSessions, refreshFailureErr)
	}
	// noMatchService 是诊断帧能够读取但不包含目标旧标识的聊天服务。
	noMatchService := New(&readMessageRepository{fakeRepository: &fakeRepository{}, values: []string{`{"other":"value"}`}})
	// noMatchReadID 保存没有匹配平台标识时的解析结果。
	noMatchReadID := noMatchService.ResolveReadMessageID(context.Background(), "account", "chat", "legacy")
	if noMatchReadID != "" {
		t.Fatalf("无匹配诊断帧时应返回空值：%q", noMatchReadID)
	}
	// noMatchValue 保存不含目标标识的 map 结构，用于覆盖递归查找的 false 路径。
	noMatchValue := map[string]any{"other": "value"}
	if readValueContainsID(noMatchValue, "legacy") {
		t.Fatal("不含旧标识的 map 不应命中")
	}
	// nestedMatchValue 保存通过 map 子节点递归命中的旧标识结构。
	nestedMatchValue := map[string]any{"outer": map[string]any{"message_id": "legacy"}}
	if !readValueContainsID(nestedMatchValue, "legacy") {
		t.Fatal("map 子节点中的旧标识应命中")
	}
}
