package adapter

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// TestItemCookiePersistencePortsCoverSessionStates 覆盖商品发布、商品同步和批量发布的 Cookie 会话状态分支。
func TestItemCookiePersistencePortsCoverSessionStates(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库和 Cookie 会话共用的非取消上下文。
	ctx := context.Background()
	// detail 保存三个商品适配器共用的初始凭证视图。
	detail := db.CookiePlatformRuntimeData{ID: "cid", UserID: 1, Value: "unb=1", MetadataJSON: `{}`}
	// publishPort、syncRepository 保存待验证的商品发布和同步适配器。
	publishPort := NewItemPublishPort(store, nil, nil, nil, nil)
	// syncRepository 保存待验证的商品同步适配器。
	syncRepository := NewItemSyncRepository(store, nil, nil, nil, nil)
	// _, _, _, nilSessionErr 验证发布适配器缺少会话时保持无副作用。
	_, _, _, nilSessionErr := publishPort.persistSession(ctx, detail, nil)
	if nilSessionErr != nil {
		t.Fatal(nilSessionErr)
	}
	// flatSession 保存未发生变化的扁平 Cookie 会话。
	_, flatSession := mtop.WithFlatCookieSession(ctx, detail.Value)
	// flatChanged、flatHandled、flatSessionErr 保存扁平会话的状态结果。
	_, flatChanged, flatHandled, flatSessionErr := publishPort.persistSession(ctx, detail, flatSession)
	if flatSessionErr != nil || flatChanged || flatHandled {
		t.Fatalf("flat publish changed=%v handled=%v err=%v", flatChanged, flatHandled, flatSessionErr)
	}
	// _, authoritativeSession 保存未发生变化的完整 Cookie 快照会话。
	_, authoritativeSession := mtop.WithCookieSnapshot(ctx, []cookierefresh.BrowserCookie{{Name: "sid", Value: "1", Domain: ".goofish.com", Path: "/"}})
	// authoritativeChanged、authoritativeHandled、authoritativeErr 保存完整会话的状态结果。
	_, authoritativeChanged, authoritativeHandled, authoritativeErr := syncRepository.persistSession(ctx, detail, authoritativeSession)
	if authoritativeErr != nil || authoritativeChanged || !authoritativeHandled {
		t.Fatalf("authoritative sync changed=%v handled=%v err=%v", authoritativeChanged, authoritativeHandled, authoritativeErr)
	}
	// _, changedSession 保存已发生权威 Cookie 变化的会话。
	_, changedSession := mtop.WithCookieSnapshot(ctx, []cookierefresh.BrowserCookie{{Name: "sid", Value: "2", Domain: ".goofish.com", Path: "/"}})
	changedSession.ReplaceSnapshot([]cookierefresh.BrowserCookie{{Name: "sid", Value: "3", Domain: ".goofish.com", Path: "/"}})
	// changedValue、changedFlag、changedHandled、changedErr 保存批量适配器写回结果。
	changedValue, changedFlag, changedHandled, changedErr := persistBatchCookieSession(ctx, store, detail, changedSession)
	if changedErr != nil || !changedFlag || !changedHandled || changedValue == detail.Value {
		t.Fatalf("changed batch value=%q changed=%v handled=%v err=%v", changedValue, changedFlag, changedHandled, changedErr)
	}
	// savedDetail、savedDetailErr 保存权威 Cookie 写回后的凭证视图。
	savedDetail, savedDetailErr := store.Cookies.GetCookiePlatformRuntimeData(ctx, detail.ID)
	if savedDetailErr != nil || savedDetail.Value == detail.Value {
		t.Fatalf("saved detail=%+v err=%v", savedDetail, savedDetailErr)
	}
	// publishChanged、publishHandled、publishErr 保存发布适配器的变化写回结果。
	_, publishChanged, publishHandled, publishErr := publishPort.persistSession(ctx, detail, changedSession)
	if publishErr != nil || !publishChanged || !publishHandled {
		t.Fatalf("changed publish changed=%v handled=%v err=%v", publishChanged, publishHandled, publishErr)
	}
	// syncChanged、syncHandled、syncErr 保存同步适配器的变化写回结果。
	_, syncChanged, syncHandled, syncErr := syncRepository.persistSession(ctx, detail, changedSession)
	if syncErr != nil || !syncChanged || !syncHandled {
		t.Fatalf("changed sync changed=%v handled=%v err=%v", syncChanged, syncHandled, syncErr)
	}
}

// TestItemCookiePersistencePortsCoverDatabaseErrors 覆盖三个商品适配器写回 Cookie 时的数据库错误。
func TestItemCookiePersistencePortsCoverDatabaseErrors(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库和 Cookie 会话共用的非取消上下文。
	ctx := context.Background()
	// detail 保存关闭数据库前读取的凭证视图。
	detail := db.CookiePlatformRuntimeData{ID: "cid", UserID: 1, Value: "unb=1", MetadataJSON: `{}`}
	// _, changedSession 保存会触发数据库写回的权威 Cookie 会话。
	_, changedSession := mtop.WithCookieSnapshot(ctx, []cookierefresh.BrowserCookie{{Name: "sid", Value: "1", Domain: ".goofish.com", Path: "/"}})
	changedSession.ReplaceSnapshot([]cookierefresh.BrowserCookie{{Name: "sid", Value: "2", Domain: ".goofish.com", Path: "/"}})
	// closeErr 保存关闭测试数据库连接的结果。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// publishPort、syncRepository 保存绑定关闭数据库的商品适配器。
	publishPort := NewItemPublishPort(store, nil, nil, nil, nil)
	// syncRepository 保存绑定关闭数据库的商品同步适配器。
	syncRepository := NewItemSyncRepository(store, nil, nil, nil, nil)
	// _, _, _, publishErr 保存发布适配器的数据库错误。
	_, _, _, publishErr := publishPort.persistSession(ctx, detail, changedSession)
	// _, _, _, syncErr 保存同步适配器的数据库错误。
	_, _, _, syncErr := syncRepository.persistSession(ctx, detail, changedSession)
	// _, _, _, batchErr 保存批量发布适配器的数据库错误。
	_, _, _, batchErr := persistBatchCookieSession(ctx, store, detail, changedSession)
	if publishErr == nil || syncErr == nil || batchErr == nil {
		t.Fatalf("closed database errors publish=%v sync=%v batch=%v", publishErr, syncErr, batchErr)
	}
}
