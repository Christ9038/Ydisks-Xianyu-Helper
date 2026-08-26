package adapter

import (
	"context"
	"errors"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// itemSyncListClient 是商品同步主链测试使用的本地 MTOP 客户端替身。
type itemSyncListClient struct {
	// orderRuntimeMTopFake 提供同步测试不涉及的其余 MTOP 接口默认实现。
	orderRuntimeMTopFake
	// allResult、pageResult 保存全量和分页商品列表结果。
	allResult  *mtop.ItemListResult
	pageResult *mtop.ItemListResult
	// allErr、pageErr 保存全量和分页请求的模拟错误。
	allErr  error
	pageErr error
	// detect 保存多规格探测的模拟函数。
	detect func(context.Context, string, string) (bool, error)
}

// FetchAllItems 返回预置的全量商品列表结果。
func (c *itemSyncListClient) FetchAllItems(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return c.allResult, c.allErr
}

// FetchItemsPage 返回预置的分页商品列表结果。
func (c *itemSyncListClient) FetchItemsPage(context.Context, string, int, int) (*mtop.ItemListResult, error) {
	return c.pageResult, c.pageErr
}

// DetectItemMultiSpec 返回预置的商品多规格探测结果。
func (c *itemSyncListClient) DetectItemMultiSpec(ctx context.Context, cookies, itemID string) (bool, error) {
	if c.detect == nil {
		return false, nil
	}
	return c.detect(ctx, cookies, itemID)
}

// TestItemSyncRepositoryRunsSQLiteAndPlatformFakePaths 验证商品全量/分页同步主链及本地 reconcile。
func TestItemSyncRepositoryRunsSQLiteAndPlatformFakePaths(t *testing.T) {
	// store、cleanup 保存隔离的 SQLite 存储和关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试同步数据库操作共用的上下文。
	ctx := context.Background()
	// updates、recoveries 保存平台响应 Cookie 更新和会话恢复通知。
	updates := make([]string, 0, 2)
	// recoveries 保存平台会话过期后的恢复通知账号。
	recoveries := make([]string, 0, 2)
	// client 保存全量、分页及多规格探测的本地平台替身。
	client := &itemSyncListClient{
		allResult:  &mtop.ItemListResult{Items: []mtop.ItemListItem{{ID: "sync-all", Title: "全量商品", Price: "10", CategoryID: "cat", ItemDetail: `{"title":"all"}`}}, PageNumber: 1, PageSize: 20, TotalPages: 1},
		pageResult: &mtop.ItemListResult{Items: []mtop.ItemListItem{{ID: "sync-page", Title: "分页商品", PriceText: "20", CategoryID: "cat-page", IsMultiSpec: true}}, PageNumber: 2, PageSize: 1, TotalPages: 3},
		detect:     func(context.Context, string, string) (bool, error) { return true, nil },
	}
	// repository 保存注入本地平台和恢复回调的同步适配器。
	repository := NewItemSyncRepository(store, func() mtop.Client { return client }, nil, func(_ context.Context, accountID, value string) {
		updates = append(updates, accountID+":"+value)
	}, func(_ context.Context, accountID string, _ error) {
		recoveries = append(recoveries, accountID)
	})
	// ownerID 保存测试账号所属用户 ID。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// owned、ownedErr 保存真实账号归属查询结果。
	owned, ownedErr := repository.OwnsAccount(ctx, owner.ID, "cid")
	if ownedErr != nil || !owned {
		t.Fatalf("账号归属判断异常 owned=%v err=%v", owned, ownedErr)
	}
	// notOwned、notOwnedErr 保存其他用户的归属判断结果。
	notOwned, notOwnedErr := repository.OwnsAccount(ctx, owner.ID+1, "cid")
	if notOwnedErr != nil || notOwned {
		t.Fatalf("错误用户不应拥有账号 owned=%v err=%v", notOwned, notOwnedErr)
	}
	// missing, missingErr 保存不存在账号的归属结果。
	missing, missingErr := repository.OwnsAccount(ctx, owner.ID, "missing-account")
	if missingErr != nil || missing {
		t.Fatalf("不存在账号归属异常 owned=%v err=%v", missing, missingErr)
	}
	// allResult、allErr 保存全量同步结果。
	allResult, allErr := repository.SyncAll(ctx, itemapp.SyncQuery{UserID: owner.ID, CookieID: "cid", PageSize: 20, MaxPages: 2})
	if allErr != nil || allResult.TotalCount != 1 || allResult.SavedCount != 1 || allResult.TotalPages != 1 {
		t.Fatalf("全量同步异常 result=%+v err=%v", allResult, allErr)
	}
	// pageResult、pageErr 保存分页同步结果。
	pageResult, pageErr := repository.SyncPage(ctx, itemapp.SyncQuery{UserID: owner.ID, CookieID: "cid", PageNumber: 2, PageSize: 1})
	if pageErr != nil || pageResult.PageNumber != 2 || pageResult.CurrentCount != 1 || pageResult.SavedCount != 1 {
		t.Fatalf("分页同步异常 result=%+v err=%v", pageResult, pageErr)
	}
	// allItems、itemsErr 保存同步后本地商品数据。
	allItems, itemsErr := store.Items.AllForCookie(ctx, "cid")
	if itemsErr != nil || len(allItems) != 2 {
		t.Fatalf("同步商品未正确落库 items=%+v err=%v", allItems, itemsErr)
	}
	if len(updates) != 0 || len(recoveries) != 0 {
		t.Fatalf("无 Cookie 变化时不应触发外部回调 updates=%v recoveries=%v", updates, recoveries)
	}
	// nilRepository 保存缺失数据库依赖的同步适配器。
	nilRepository := NewItemSyncRepository(nil, func() mtop.Client { return client }, nil, nil, nil)
	// _, nilOwnErr 保存缺失数据库时的归属错误。
	if _, nilOwnErr := nilRepository.OwnsAccount(ctx, owner.ID, "cid"); nilOwnErr == nil {
		t.Fatal("缺失同步存储应返回归属错误")
	}
}

// TestItemSyncRepositoryMapsPlatformFailureAndRecovery 验证平台失败被包装并触发账号恢复回调。
func TestItemSyncRepositoryMapsPlatformFailureAndRecovery(t *testing.T) {
	// store、cleanup 保存隔离的 SQLite 存储和关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试平台失败操作共用的上下文。
	ctx := context.Background()
	// platformErr 保存本地平台替身返回的可识别错误。
	platformErr := errors.New("platform unavailable")
	// recovered 保存会话过期后的账号恢复调用记录。
	recovered := ""
	// client 保存只返回分页错误的平台替身。
	client := &itemSyncListClient{pageErr: platformErr}
	// repository 保存失败路径使用的同步适配器。
	repository := NewItemSyncRepository(store, func() mtop.Client { return client }, nil, nil, func(_ context.Context, accountID string, err error) {
		if errors.Is(err, platformErr) {
			recovered = accountID
		}
	})
	// result、syncErr 保存失败同步的零值结果和阶段错误。
	result, syncErr := repository.SyncPage(ctx, itemapp.SyncQuery{UserID: 1, CookieID: "cid", PageNumber: 1, PageSize: 20})
	if result != (itemapp.SyncPageResult{}) || syncErr == nil || recovered != "cid" {
		t.Fatalf("平台失败映射异常 result=%+v err=%v recovered=%q", result, syncErr, recovered)
	}
	// typedErr 保存可解析失败阶段的应用错误。
	var typedErr *itemapp.SyncError
	if !errors.As(syncErr, &typedErr) || typedErr.Kind != itemapp.SyncErrorPlatform {
		t.Fatalf("平台失败阶段错误异常 err=%T %v", syncErr, syncErr)
	}
}

// TestItemSyncCredentialHelpers 验证商品同步凭证辅助判断和 Cookie 上下文包装。
func TestItemSyncCredentialHelpers(t *testing.T) {
	// detail 保存带平面 Cookie 的平台运行时凭证。
	detail := db.CookiePlatformRuntimeData{Value: "unb=id"}
	if !hasStoredCredential(detail) {
		t.Fatal("平面 Cookie 应被识别为有效凭证")
	}
	// emptyDetail 保存没有任何凭证的账号视图。
	emptyDetail := db.CookiePlatformRuntimeData{}
	if hasStoredCredential(emptyDetail) {
		t.Fatal("空凭证不应被识别为有效凭证")
	}
	// ctx 是本测试 Cookie 会话包装共用的上下文。
	ctx := context.Background()
	// sessionCtx、session 保存平面 Cookie 会话上下文及会话对象。
	sessionCtx, session := withCookieSnapshot(ctx, detail)
	if sessionCtx == nil || session == nil {
		t.Fatal("平面 Cookie 会话构造失败")
	}
	// completeDetail 保存权威浏览器快照形式的凭证视图。
	completeDetail := db.CookiePlatformRuntimeData{Value: "unb=id", MetadataJSON: `{"cookies_refresh_snapshot":[]}`}
	// completeCtx、completeSession 保存完整 Cookie Jar 会话上下文。
	completeCtx, completeSession := withCookieSnapshot(ctx, completeDetail)
	if completeCtx == nil || completeSession == nil || !hasStoredCredential(completeDetail) {
		t.Fatal("完整 Cookie 会话构造失败")
	}
}
