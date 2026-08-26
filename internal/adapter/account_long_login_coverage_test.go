package adapter

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// TestLongLoginHelperBranches 覆盖长登录 URL、Cookie 视图和结果脱敏转换。
func TestLongLoginHelperBranches(t *testing.T) {
	// queryURLValue 保存查询长登录操作使用的官方 URL。
	queryURLValue := queryURL(nil)
	// enabled 保存设置长登录操作的开关值。
	enabled := true
	// setURLValue 保存设置长登录操作使用的官方 URL。
	setURLValue := queryURL(&enabled)
	if queryURLValue != xrenew.QueryLoginSettingsURL || setURLValue != xrenew.SetLoginSettingsURL {
		t.Fatalf("urls=%q/%q", queryURLValue, setURLValue)
	}
	// flatHeader、flatSnapshot 保存历史扁平 Cookie 账号的请求视图。
	flatHeader, flatSnapshot := longLoginCookies(&accountapp.CredentialDetail{Value: "sid=flat"}, queryURLValue)
	if flatHeader != "sid=flat" || flatSnapshot != nil {
		t.Fatalf("flat cookies=%q snapshot=%v", flatHeader, flatSnapshot)
	}
	// snapshotMetadata 保存完整浏览器 Cookie 快照元数据。
	snapshotMetadata := cookierefresh.MetadataWithSnapshot(`{}`, []cookierefresh.BrowserCookie{{Name: "sid", Value: "snapshot", Domain: ".goofish.com", Path: "/"}})
	// snapshotHeader、snapshot 保存完整 Cookie Jar 的请求视图。
	snapshotHeader, snapshot := longLoginCookies(&accountapp.CredentialDetail{MetadataJSON: snapshotMetadata}, queryURLValue)
	if snapshotHeader == "" || len(snapshot) != 1 {
		t.Fatalf("snapshot cookies=%q snapshot=%v", snapshotHeader, snapshot)
	}
	// nilHeader、nilSnapshot 保存空凭证视图的安全结果。
	nilHeader, nilSnapshot := longLoginCookies(nil, queryURLValue)
	if nilHeader != "" || nilSnapshot != nil {
		t.Fatalf("nil cookies=%q snapshot=%v", nilHeader, nilSnapshot)
	}
	// result 保存平台返回的非敏感长登录状态。
	result := toLongLoginResult(&xrenew.LongLoginSettings{CanOpenLongLogin: true, Enabled: true})
	if !result.CanOpenLongLogin || !result.Enabled || toLongLoginResult(nil) != (accountapp.LongLoginResult{}) {
		t.Fatalf("result=%+v", result)
	}
}

// TestLongLoginAdapterSetAndSnapshotBranches 验证设置长登录、完整 Cookie 快照和平台错误映射分支。
func TestLongLoginAdapterSetAndSnapshotBranches(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储和关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试长登录请求共用的非取消上下文。
	ctx := context.Background()
	// updatedCookie 保存运行时 Cookie 同步回调收到的最新值。
	updatedCookie := ""
	// setClient 保存设置长登录分支使用的平台客户端替身。
	setClient := &longLoginClientFake{result: &xrenew.LongLoginSettings{CanOpenLongLogin: true, Enabled: false, SetCookies: []string{"sid=set"}}}
	// setAdapter 保存绑定设置长登录平台客户端的适配器。
	setAdapter := NewLongLoginAdapter(NewAccountLoginRepository(store), func() LongLoginClient { return setClient }, func(_ context.Context, _ string, value string) { updatedCookie = value }, nil)
	// setResult、setErr 保存设置长登录状态的应用结果和错误。
	setResult, setErr := setAdapter.SetLongLogin(ctx, "cid", false)
	if setErr != nil || setResult.Enabled || !strings.Contains(updatedCookie, "sid=set") {
		t.Fatalf("设置长登录异常 result=%+v err=%v updated=%q", setResult, setErr, updatedCookie)
	}
	// snapshotMetadata 保存预置的完整浏览器 Cookie 快照元数据。
	snapshotMetadata := cookierefresh.MetadataWithSnapshot(`{"legacy":true}`, []cookierefresh.BrowserCookie{{Name: "sid", Value: "old", Domain: ".goofish.com", Path: "/"}})
	// snapshotWriteErr 保存完整 Cookie 快照写入测试账号的结果。
	snapshotWriteErr := store.Cookies.UpdateRenewalCookie(ctx, "cid", "sid=old", snapshotMetadata, time.Now().Unix())
	if snapshotWriteErr != nil {
		t.Fatalf("写入测试快照失败: %v", snapshotWriteErr)
	}
	// snapshotClient 保存返回权威完整 Cookie 快照的平台客户端替身。
	snapshotClient := &longLoginClientFake{result: &xrenew.LongLoginSettings{
		CanOpenLongLogin: true, Enabled: true, CookieSnapshotComplete: true,
		CookieSnapshot: []cookierefresh.BrowserCookie{{Name: "sid", Value: "new", Domain: ".goofish.com", Path: "/"}},
	}}
	// snapshotAdapter 保存绑定完整快照平台结果的长登录适配器。
	snapshotAdapter := NewLongLoginAdapter(NewAccountLoginRepository(store), func() LongLoginClient { return snapshotClient }, nil, nil)
	// snapshotResult、snapshotErr 保存完整快照查询的应用结果和错误。
	snapshotResult, snapshotErr := snapshotAdapter.QueryLongLogin(ctx, "cid")
	if snapshotErr != nil || !snapshotResult.Enabled {
		t.Fatalf("完整快照查询异常 result=%+v err=%v", snapshotResult, snapshotErr)
	}
	// detail、detailErr 保存完整快照写回后的凭证平台视图。
	detail, detailErr := NewAccountLoginRepository(store).LoadPlatformDetail(ctx, "cid")
	if detailErr != nil || detail == nil || detail.Value == "" {
		t.Fatalf("完整快照写回异常 detail=%+v err=%v", detail, detailErr)
	}
	// platformErr 保存平台查询失败时应保留的底层错误。
	platformErr := errors.New("long login platform unavailable")
	// errorAdapter 保存返回平台错误的长登录适配器。
	errorAdapter := NewLongLoginAdapter(NewAccountLoginRepository(store), func() LongLoginClient {
		return &longLoginClientFake{err: platformErr}
	}, nil, nil)
	// _, queryErr 保存平台错误映射后的查询错误。
	_, queryErr := errorAdapter.QueryLongLogin(ctx, "cid")
	if !errors.Is(queryErr, platformErr) || !errors.Is(queryErr, accountapp.ErrLongLoginPlatform) {
		t.Fatalf("平台错误映射异常: %v", queryErr)
	}
}
