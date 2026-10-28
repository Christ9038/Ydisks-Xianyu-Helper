package chat

import (
	"context"
	"errors"
	"testing"
	"time"
)

// throttleIdentityResolver 统计平台身份查询次数，并按配置返回固定身份或错误。
type throttleIdentityResolver struct {
	// calls 是 Resolve 被调用的累计次数。
	calls int
	// identity 是成功时返回的展示身份。
	identity Identity
	// err 非空时每次查询都失败。
	err error
}

// Resolve 记录一次调用并返回预设结果。
func (r *throttleIdentityResolver) Resolve(context.Context, string, string) (Identity, error) {
	r.calls++
	return r.identity, r.err
}

// TestResolveSessionIdentitySkipsCompleteSession 验证昵称与头像已完整的会话打开时不再请求平台。
func TestResolveSessionIdentitySkipsCompleteSession(t *testing.T) {
	// resolver 用于统计平台查询次数。
	resolver := &throttleIdentityResolver{identity: Identity{PeerName: "新名", PeerAvatar: "新头像"}}
	// service 是注入统计解析器的聊天应用服务。
	service := NewWithIdentity(&fakeRepository{}, resolver)
	// session 是身份已完整的本地会话。
	session := Session{AccountID: "a", ChatID: "c", PeerUserID: "p", PeerName: "旧名", PeerAvatar: "旧头像"}
	// got 和 err 是补全结果。
	got, err := service.ResolveSessionIdentity(context.Background(), session)
	if err != nil || resolver.calls != 0 || got.PeerName != "旧名" {
		t.Fatalf("calls=%d name=%q err=%v", resolver.calls, got.PeerName, err)
	}
}

// TestResolveSessionIdentityCoolsDownAfterFailure 验证查询失败后冷却期内不再请求平台，冷却结束后恢复查询。
func TestResolveSessionIdentityCoolsDownAfterFailure(t *testing.T) {
	// resolver 每次都失败，模拟令牌过期或风控。
	resolver := &throttleIdentityResolver{err: errors.New("token expired")}
	// service 是注入统计解析器的聊天应用服务。
	service := NewWithIdentity(&fakeRepository{}, resolver)
	// session 缺少头像，需要平台补全。
	session := Session{AccountID: "a", ChatID: "c", PeerUserID: "p", PeerName: "名"}
	// attempt 表示第几次打开该会话。
	for attempt := 0; attempt < 5; attempt++ {
		_, _ = service.ResolveSessionIdentity(context.Background(), session)
	}
	if resolver.calls != 1 {
		t.Fatalf("cooldown calls=%d, want 1", resolver.calls)
	}
	// 将失败时间回拨到冷却期之外，应允许再次查询。
	service.identityFailures[identityFailureKey(session)] = time.Now().Add(-identityFailureCooldown - time.Second)
	_, _ = service.ResolveSessionIdentity(context.Background(), session)
	if resolver.calls != 2 {
		t.Fatalf("after cooldown calls=%d, want 2", resolver.calls)
	}
}
