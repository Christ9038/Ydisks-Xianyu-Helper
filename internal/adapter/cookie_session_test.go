package adapter

import (
	"context"
	"strings"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// minimalCredentialSessionPortFake 仅实现会话写回所需的方法，证明适配器不依赖完整账号仓储。
type minimalCredentialSessionPortFake struct {
	// calls 记录平台会话写回次数。
	calls int
}

// UpdateRenewalCookie 记录一次脱敏 Cookie 写回请求，不保存明文内容。
func (f *minimalCredentialSessionPortFake) UpdateRenewalCookie(context.Context, string, string, string, int64) error {
	f.calls++
	return nil
}

// TestCookieSessionAdapterPersistsAuthoritativeSnapshot 验证平台 Cookie 会话快照经凭证 Port 写回并保留作用域。
func TestCookieSessionAdapterPersistsAuthoritativeSnapshot(t *testing.T) {
	// store、cleanup 保存测试数据库及其资源清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是当前凭证会话测试共用的上下文。
	ctx := context.Background()
	// repository 是将 Cookie 写回 SQLite 的账号凭证适配器。
	repository := NewAccountLoginRepository(store)
	// detail、loadErr 保存平台调用所需的窄凭证视图及读取错误。
	detail, loadErr := repository.LoadPlatformDetail(ctx, "cid")
	if loadErr != nil {
		t.Fatalf("读取凭证视图失败: %v", loadErr)
	}
	// sessionContext、session 保存本次平台流程的 Cookie 会话。
	_, session := WithFlatCookieSession(ctx, detail.Value)
	session.ReplaceSnapshot([]BrowserCookie{{Name: "sid", Value: "new", Domain: ".goofish.com", Path: "/"}})
	// value、changed、handled、persistErr 保存会话写回结果及阶段错误。
	value, changed, handled, persistErr := PersistCookieSessionLocked(ctx, repository, detail, session)
	if persistErr != nil || !changed || !handled || value == "" {
		t.Fatalf("快照写回异常: value=%q changed=%v handled=%v err=%v", value, changed, handled, persistErr)
	}
	// updated、updatedErr 保存写回后的平台凭证视图。
	updated, updatedErr := repository.LoadPlatformDetail(ctx, detail.ID)
	if updatedErr != nil {
		t.Fatalf("读取写回凭证失败: %v", updatedErr)
	}
	// snapshot、complete 保存数据库中快照及完整性标记。
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(updated.MetadataJSON)
	if !complete || len(snapshot) != 1 || snapshot[0].Value != "new" {
		t.Fatalf("快照写回不完整: complete=%v snapshot=%+v", complete, snapshot)
	}
}

// TestCookieSessionAdapterRejectsMissingRepository 验证凭证适配器缺失时不会伪装写回成功。
func TestCookieSessionAdapterRejectsMissingRepository(t *testing.T) {
	// detail 是不含登录密码的最小平台凭证视图。
	detail := &accountapp.CredentialDetail{ID: "cid", Value: "sid=old", MetadataJSON: "{}"}
	// ctx、session 保存发生权威快照变化的当前平台会话。
	ctx, session := WithFlatCookieSession(context.Background(), detail.Value)
	session.ReplaceSnapshot([]BrowserCookie{{Name: "sid", Value: "new", Domain: ".goofish.com", Path: "/"}})
	// _, changed、handled、persistErr 保存缺失 repository 时的结果。
	_, changed, handled, persistErr := PersistCookieSessionLocked(ctx, nil, detail, session)
	if !changed || !handled || persistErr == nil || !strings.Contains(persistErr.Error(), "repository 未初始化") {
		t.Fatalf("缺失 repository 错误阶段异常: changed=%v handled=%v err=%v", changed, handled, persistErr)
	}
}

// TestCookieSessionAdapterAcceptsMinimalSessionPort 验证会话写回只要求最小凭证端口。
func TestCookieSessionAdapterAcceptsMinimalSessionPort(t *testing.T) {
	// detail 是不含登录密码的最小平台凭证视图。
	detail := &accountapp.CredentialDetail{ID: "cid", Value: "sid=old", MetadataJSON: "{}"}
	// ctx、session 保存发生权威快照变化的当前平台会话。
	ctx, session := WithFlatCookieSession(context.Background(), detail.Value)
	session.ReplaceSnapshot([]BrowserCookie{{Name: "sid", Value: "new", Domain: ".goofish.com", Path: "/"}})
	// port 保存仅实现 Cookie 写回能力的最小端口替身。
	port := &minimalCredentialSessionPortFake{}
	// _, changed、handled、persistErr 保存使用最小端口写回后的结果。
	_, changed, handled, persistErr := PersistCookieSessionLocked(ctx, port, detail, session)
	if persistErr != nil || !changed || !handled || port.calls != 1 {
		t.Fatalf("最小凭证端口写回异常: changed=%v handled=%v calls=%d err=%v", changed, handled, port.calls, persistErr)
	}
}

// TestCookieSessionAdapterHasStoredCredential 验证扁平 Cookie 和完整快照均被识别为可用凭证。
func TestCookieSessionAdapterHasStoredCredential(t *testing.T) {
	// flat 是只含扁平 Cookie 的历史平台凭证视图。
	flat := &accountapp.CredentialDetail{Value: "sid=flat"}
	if !HasStoredCookieCredential(flat) {
		t.Fatal("扁平 Cookie 应被识别为已存储凭证")
	}
	// snapshotMetadata 是包含完整 Jar 的 metadata。
	snapshotMetadata := cookierefresh.MetadataWithSnapshot(`{}`, []BrowserCookie{{Name: "sid", Value: "snapshot", Domain: ".goofish.com", Path: "/"}})
	if !HasStoredCookieCredential(&accountapp.CredentialDetail{MetadataJSON: snapshotMetadata}) {
		t.Fatal("完整 Cookie 快照应被识别为已存储凭证")
	}
	if HasStoredCookieCredential(nil) {
		t.Fatal("空凭证视图不应被识别为已存储凭证")
	}
}
