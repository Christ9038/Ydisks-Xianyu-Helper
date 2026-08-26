package adapter

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// profileSessionMTopFake 是会在资料请求期间更新 Cookie 会话的平台替身。
type profileSessionMTopFake struct {
	// orderRuntimeMTopFake 提供资料测试不涉及的其他平台接口默认实现。
	orderRuntimeMTopFake
	// result、err 保存资料请求的预置结果和错误。
	result *mtop.UserProfileResult
	err    error
	// snapshot 保存请求完成时替换到权威会话中的 Cookie 快照。
	snapshot []cookierefresh.BrowserCookie
}

// FetchUserProfile 返回资料结果，并模拟平台响应设置完整 Cookie 快照。
func (f *profileSessionMTopFake) FetchUserProfile(ctx context.Context, _ string) (*mtop.UserProfileResult, error) {
	// session 保存当前平台请求绑定的 Cookie 会话。
	session := mtop.CookieSessionFromContext(ctx)
	if session != nil && f.snapshot != nil {
		session.ReplaceSnapshot(f.snapshot)
	}
	return f.result, f.err
}

// TestAccountProfileRefreshCoversMissingOwnerAndNilResult 覆盖账号缺失、归属不符和平台空结果兜底。
func TestAccountProfileRefreshCoversMissingOwnerAndNilResult(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库和平台调用共用的非取消上下文。
	ctx := context.Background()
	// repository 是绑定测试存储的账号资料适配器。
	repository := NewAccountLoginRepository(store)
	// port 是返回空资料结果的平台资料适配器。
	port := NewAccountProfilePort(repository, func() mtop.Client {
		return &profileMTopFake{}
	}, nil, nil, nil)
	// missing、missingErr 保存不存在账号的本地资料兜底结果。
	missing, missingErr := port.RefreshProfile(ctx, accountapp.ProfileInput{UserID: 1, AccountID: "missing", Summary: accountapp.Summary{ID: "missing-long-id", Nickname: "缓存昵称"}})
	if missingErr != nil || missing.Nickname != "缓存昵称" || missing.ErrorMessage == "" {
		t.Fatalf("missing profile=%+v err=%v", missing, missingErr)
	}
	// wrongOwner、wrongOwnerErr 保存账号归属不匹配时的安全兜底结果。
	wrongOwner, wrongOwnerErr := port.RefreshProfile(ctx, accountapp.ProfileInput{UserID: 2, AccountID: "cid", Summary: accountapp.Summary{ID: "cid", Remark: "备注兜底"}})
	if wrongOwnerErr != nil || wrongOwner.Nickname != "备注兜底" || wrongOwner.ErrorMessage == "" {
		t.Fatalf("wrong owner profile=%+v err=%v", wrongOwner, wrongOwnerErr)
	}
	// emptyResult、emptyErr 保存平台未返回资料对象时的稳定结果。
	emptyResult, emptyErr := port.RefreshProfile(ctx, accountapp.ProfileInput{UserID: 1, AccountID: "cid", Summary: accountapp.Summary{ID: "cid", Nickname: "本地昵称"}})
	if emptyErr != nil || emptyResult.Nickname != "本地昵称" || emptyResult.ErrorMessage != "账号资料接口未返回结果" {
		t.Fatalf("empty profile=%+v err=%v", emptyResult, emptyErr)
	}
}

// TestAccountProfileRefreshCoversPlatformFailureAndRecovery 覆盖平台资料失败、日志记录和账号恢复回调。
func TestAccountProfileRefreshCoversPlatformFailureAndRecovery(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库和平台调用共用的非取消上下文。
	ctx := context.Background()
	// platformErr 是平台资料接口返回的非敏感测试错误。
	platformErr := errors.New("profile platform unavailable")
	// recoveredAccount、recoveredErr 保存恢复回调收到的账号与错误。
	recoveredAccount := ""
	// recoveredErr 保存恢复回调收到的平台错误链。
	recoveredErr := error(nil)
	// logger 记录错误分支但不输出到测试结果的结构化日志。
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// port 是返回平台错误并触发恢复回调的资料适配器。
	port := NewAccountProfilePort(NewAccountLoginRepository(store), func() mtop.Client {
		return &profileMTopFake{err: platformErr}
	}, nil, func(_ context.Context, accountID string, err error) bool {
		recoveredAccount = accountID
		recoveredErr = err
		return true
	}, logger)
	// result、refreshErr 保存平台失败后的安全资料结果。
	result, refreshErr := port.RefreshProfile(ctx, accountapp.ProfileInput{UserID: 1, AccountID: "cid", Summary: accountapp.Summary{ID: "cid", Remark: "失败兜底"}})
	if refreshErr != nil || result.Nickname != "失败兜底" || !strings.Contains(result.ErrorMessage, platformErr.Error()) || recoveredAccount != "cid" || !errors.Is(recoveredErr, platformErr) {
		t.Fatalf("platform failure result=%+v err=%v recovered=%q/%v", result, refreshErr, recoveredAccount, recoveredErr)
	}
}

// TestAccountProfileRefreshCoversAuthoritativeSessionAndPersistenceError 覆盖权威 Cookie 写回和锁内读取失败。
func TestAccountProfileRefreshCoversAuthoritativeSessionAndPersistenceError(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库和平台调用共用的非取消上下文。
	ctx := context.Background()
	// metadata 是带完整浏览器 Cookie 快照的凭证元数据。
	metadata := cookierefresh.MetadataWithSnapshot(`{}`, []cookierefresh.BrowserCookie{{Name: "sid", Value: "old", Domain: ".goofish.com", Path: "/"}})
	// metadataErr 保存完整 Cookie 快照写入结果。
	if metadataErr := store.Cookies.UpdateRenewalCookie(ctx, "cid", "sid=old", metadata, 1); metadataErr != nil {
		t.Fatal(metadataErr)
	}
	// updatedCookie 保存权威会话写回运行时的 Cookie 值。
	updatedCookie := ""
	// port 是模拟权威 Cookie 旋转并保存资料的平台适配器。
	port := NewAccountProfilePort(NewAccountLoginRepository(store), func() mtop.Client {
		return &profileSessionMTopFake{
			result:   &mtop.UserProfileResult{Nickname: "权威昵称", AvatarURL: "//img.test/authoritative"},
			snapshot: []cookierefresh.BrowserCookie{{Name: "sid", Value: "new", Domain: ".goofish.com", Path: "/"}},
		}
	}, func(_ context.Context, _ string, value string) {
		updatedCookie = value
	}, nil, nil)
	// result、refreshErr 保存权威资料刷新结果。
	result, refreshErr := port.RefreshProfile(ctx, accountapp.ProfileInput{UserID: 1, AccountID: "cid", Summary: accountapp.Summary{ID: "cid", Nickname: "旧昵称"}})
	if refreshErr != nil || result.Nickname != "权威昵称" || result.AvatarURL != "https://img.test/authoritative" || updatedCookie != "sid=new" {
		t.Fatalf("authoritative profile=%+v err=%v cookie=%q", result, refreshErr, updatedCookie)
	}
	// detail、detailErr 保存权威 Cookie 写回后的凭证视图。
	detail, detailErr := NewAccountLoginRepository(store).LoadPlatformDetail(ctx, "cid")
	if detailErr != nil || detail.Value != "sid=new" {
		t.Fatalf("authoritative detail=%+v err=%v", detail, detailErr)
	}
	// closedStore、closedCleanup 保存用于覆盖锁内读取失败的关闭数据库。
	closedStore, closedCleanup := newAdapterTestStore(t)
	defer closedCleanup()
	// closedPort 是绑定即将关闭数据库的资料适配器。
	closedPort := NewAccountProfilePort(NewAccountLoginRepository(closedStore), nil, nil, nil, nil)
	// closedDetail、closedDetailErr 保存关闭数据库后的平台凭证查询结果。
	closedDetail, closedDetailErr := NewAccountLoginRepository(closedStore).LoadPlatformDetail(ctx, "cid")
	if closedDetailErr != nil || closedDetail == nil {
		t.Fatalf("closed detail setup=%+v err=%v", closedDetail, closedDetailErr)
	}
	// closeErr 保存关闭测试数据库连接的结果。
	if closeErr := closedStore.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// _, failedChanged、failedHandled、failedErr 保存凭证锁内重读失败的结果。
	_, failedChanged, failedHandled, failedErr := closedPort.persistProfileSession(ctx, closedDetail, mtopSessionForProfile(ctx, closedDetail.Value))
	if failedChanged || !failedHandled || failedErr == nil {
		t.Fatalf("closed persistence changed=%v handled=%v err=%v", failedChanged, failedHandled, failedErr)
	}
}

// mtopSessionForProfile 创建资料持久化测试使用的扁平 Cookie 会话。
func mtopSessionForProfile(ctx context.Context, value string) *CookieSession {
	// session 保存待传入资料持久化方法的请求会话。
	_, session := WithFlatCookieSession(ctx, value)
	return session
}
