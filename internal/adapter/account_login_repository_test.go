package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestAccountLoginRepositoryKeepsPlatformViewNarrow 验证平台凭证 Port 只返回 Cookie 运行所需字段。
func TestAccountLoginRepositoryKeepsPlatformViewNarrow(t *testing.T) {
	// store 是包含测试账号和加密凭证的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// metadata 是带浏览器快照的加密元数据样例。
	metadata := cookierefresh.MetadataWithSnapshot(`{"device":"test"}`, []cookierefresh.BrowserCookie{{Name: "sid", Value: "snapshot", Domain: ".goofish.com", Path: "/"}})
	// updateErr 保存写入平台凭证视图的结果。
	if updateErr := store.Cookies.UpdateRenewalCookie(ctx, "cid", "sid=flat", metadata, 123); updateErr != nil {
		t.Fatalf("写入平台凭证视图失败: %v", updateErr)
	}
	// repository 是绑定 SQLite 存储的账号登录适配器。
	repository := NewAccountLoginRepository(store)
	// detail、loadErr 保存应用 Port 返回的窄凭证视图及错误。
	detail, loadErr := repository.LoadPlatformDetail(ctx, "cid")
	if loadErr != nil {
		t.Fatalf("读取平台凭证视图失败: %v", loadErr)
	}
	if detail == nil || detail.ID != "cid" || detail.UserID != 1 || detail.Value != "sid=flat" || detail.LastRefreshAt != 123 {
		t.Fatalf("平台凭证视图映射异常: %+v", detail)
	}
	if detail.MetadataJSON == "" || detail.ShowBrowser {
		t.Fatalf("平台凭证视图未保留必要 metadata 或浏览器设置: %+v", detail)
	}
}

// TestAccountLoginRepositoryUpdatesCookieSnapshot 验证扁平更新清除旧快照而扫码快照更新保留完整作用域。
func TestAccountLoginRepositoryUpdatesCookieSnapshot(t *testing.T) {
	// store 是用于验证 Cookie metadata 合并的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// repository 是绑定测试存储的账号登录适配器。
	repository := NewAccountLoginRepository(store)
	// initialMetadata 是待被扁平更新清除的旧浏览器快照。
	initialMetadata := cookierefresh.MetadataWithSnapshot(`{}`, []cookierefresh.BrowserCookie{{Name: "old", Value: "1", Domain: ".goofish.com", Path: "/"}})
	// initialErr 保存旧快照写入结果。
	if initialErr := store.Cookies.UpdateRenewalCookie(ctx, "cid", "sid=old", initialMetadata, time.Now().Unix()); initialErr != nil {
		t.Fatalf("写入旧快照失败: %v", initialErr)
	}
	// detail、detailErr 保存扁平更新前的窄凭证视图。
	detail, detailErr := repository.LoadPlatformDetail(ctx, "cid")
	if detailErr != nil {
		t.Fatalf("读取旧凭证视图失败: %v", detailErr)
	}
	// flatErr 保存扁平 Cookie 更新结果。
	if flatErr := repository.UpdateFlatCookieOwned(ctx, detail, "sid=flat"); flatErr != nil {
		t.Fatalf("扁平 Cookie 更新失败: %v", flatErr)
	}
	// flatDetail、flatLoadErr 保存扁平更新后的凭证视图。
	flatDetail, flatLoadErr := repository.LoadPlatformDetail(ctx, "cid")
	if flatLoadErr != nil {
		t.Fatalf("读取扁平 Cookie 失败: %v", flatLoadErr)
	}
	// complete 表示扁平更新后 metadata 是否仍包含完整浏览器快照。
	if _, complete := cookierefresh.SnapshotFromMetadataOK(flatDetail.MetadataJSON); complete {
		t.Fatal("扁平 Cookie 更新不应保留旧快照")
	}
	// snapshotErr 保存扫码快照更新结果。
	snapshotErr := repository.UpdateCookieSnapshotOwned(ctx, "cid", "sid=snapshot", []accountapp.CookieSnapshot{{Name: "sid", Value: "snapshot", Domain: ".goofish.com", Path: "/"}})
	if snapshotErr != nil {
		t.Fatalf("扫码快照更新失败: %v", snapshotErr)
	}
	// snapshotDetail、snapshotLoadErr 保存扫码快照更新后的凭证视图。
	snapshotDetail, snapshotLoadErr := repository.LoadPlatformDetail(ctx, "cid")
	if snapshotLoadErr != nil {
		t.Fatalf("读取扫码快照失败: %v", snapshotLoadErr)
	}
	// snapshot、snapshotOK 保存适配器合并后的 Cookie 快照及完整性。
	snapshot, snapshotOK := cookierefresh.SnapshotFromMetadataOK(snapshotDetail.MetadataJSON)
	if !snapshotOK || len(snapshot) != 1 || snapshot[0].Value != "snapshot" {
		t.Fatalf("扫码 Cookie 快照未正确保存: ok=%v snapshot=%+v", snapshotOK, snapshot)
	}
}

// TestAccountLoginRepositoryMapsOwnershipErrors 验证摘要与扫码账号查询不会泄露跨用户凭证内容。
func TestAccountLoginRepositoryMapsOwnershipErrors(t *testing.T) {
	// store 是包含一个账号所有者的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// repository 是绑定测试存储的账号登录适配器。
	repository := NewAccountLoginRepository(store)
	// forbiddenSummaryErr 保存跨用户摘要查询的应用错误。
	_, forbiddenSummaryErr := repository.GetOwnedSummary(ctx, 2, "cid")
	if !errors.Is(forbiddenSummaryErr, accountapp.ErrForbidden) {
		t.Fatalf("跨用户摘要应返回应用越权错误，got %v", forbiddenSummaryErr)
	}
	// missingAccountErr 保存不存在扫码账号的应用错误。
	_, missingAccountErr := repository.FindAccount(ctx, "missing")
	if !errors.Is(missingAccountErr, accountapp.ErrNotFound) {
		t.Fatalf("不存在扫码账号应返回应用不存在错误，got %v", missingAccountErr)
	}
	// missingStoreErr 保存未装配数据库时的快速失败结果。
	missingStoreErr := NewAccountLoginRepository(nil).CreateCookieOwned(ctx, "cid", "secret", 1)
	if missingStoreErr == nil {
		t.Fatal("未装配数据库时不应伪装凭证写入成功")
	}
	// duplicateErr 验证并发/重复创建被转换为应用层稳定错误，而不是泄露数据库错误类型。
	duplicateErr := repository.CreateCookieOwned(ctx, "cid", "sid=duplicate", 1)
	if !errors.Is(duplicateErr, accountapp.ErrAlreadyExists) {
		t.Fatalf("重复创建应返回账号已存在错误，got %v", duplicateErr)
	}
}

// TestAccountLoginRepositoryMapsMissingPlatformCredential 验证平台凭证缺失时返回应用层哨兵。
func TestAccountLoginRepositoryMapsMissingPlatformCredential(t *testing.T) {
	// store、cleanup 保存用于查询不存在账号的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试存储的账号登录适配器。
	repository := NewAccountLoginRepository(store)
	// loadErr 保存不存在平台账号的窄凭证查询错误。
	_, loadErr := repository.LoadPlatformDetail(context.Background(), "missing-platform-account")
	if !errors.Is(loadErr, accountapp.ErrCredentialNotFound) {
		t.Fatalf("缺失平台账号应返回应用错误: %v", loadErr)
	}
}

// TestAccountLoginRepositoryPropagatesDatabaseFailure 验证数据库关闭后凭证 Port 原样报告基础设施错误。
func TestAccountLoginRepositoryPropagatesDatabaseFailure(t *testing.T) {
	// store 是随后主动关闭的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定待关闭存储的账号登录适配器。
	repository := NewAccountLoginRepository(store)
	// closeErr 保存主动关闭数据库的结果。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// loadErr 保存数据库关闭后平台视图查询的错误。
	_, loadErr := repository.LoadPlatformDetail(context.Background(), "cid")
	if loadErr == nil {
		t.Fatal("数据库关闭后应返回凭证查询错误")
	}
}
