package adapter

import (
	"context"
	"errors"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/xianyu/mtop"
)

// TestItemSessionCommitPortsCoverFlatFallbacks 覆盖商品类目、单商品和批量发布的扁平 Cookie 兼容写回。
func TestItemSessionCommitPortsCoverFlatFallbacks(t *testing.T) {
	// ctx 是本测试数据库和 Cookie 会话共用的非取消上下文。
	ctx := context.Background()
	// categoryStore、categoryCleanup 保存商品类目写回测试存储。
	categoryStore, categoryCleanup := newAdapterTestStore(t)
	defer categoryCleanup()
	// categoryDetail、categoryDetailErr 保存商品类目写回前的凭证视图。
	categoryDetail, categoryDetailErr := categoryStore.Cookies.GetCookiePlatformRuntimeData(ctx, "cid")
	if categoryDetailErr != nil {
		t.Fatal(categoryDetailErr)
	}
	// categoryPort 是绑定本地存储的商品类目适配器。
	categoryPort := NewItemPublishPort(categoryStore, nil, nil, nil, nil)
	// _, categorySession 保存没有权威快照变化的扁平会话。
	_, categorySession := mtop.WithFlatCookieSession(ctx, categoryDetail.Value)
	// categoryValue、categoryErr 保存类目兼容 Cookie 写回结果。
	categoryValue, categoryErr := categoryPort.persistCategorySession(ctx, 1, "cid", categoryDetail.Value, categoryDetail.MetadataJSON, categorySession, "unb=category")
	if categoryErr != nil || categoryValue != "unb=category" {
		t.Fatalf("category value=%q err=%v", categoryValue, categoryErr)
	}
	// _, categoryChangedErr 保存初始凭证已变化后的类目提交结果。
	_, categoryChangedErr := categoryPort.persistCategorySession(ctx, 1, "cid", categoryDetail.Value, categoryDetail.MetadataJSON, categorySession, "unb=again")
	if !errors.Is(categoryChangedErr, itemapp.ErrCategoryCredentialChanged) {
		t.Fatalf("类目凭证变化错误=%v", categoryChangedErr)
	}

	// publishStore、publishCleanup 保存单商品写回测试存储。
	publishStore, publishCleanup := newAdapterTestStore(t)
	defer publishCleanup()
	// publishDetail、publishDetailErr 保存单商品写回前的凭证视图。
	publishDetail, publishDetailErr := publishStore.Cookies.GetCookiePlatformRuntimeData(ctx, "cid")
	if publishDetailErr != nil {
		t.Fatal(publishDetailErr)
	}
	// publishPort 是绑定本地存储的单商品发布适配器。
	publishPort := NewItemPublishPort(publishStore, nil, nil, nil, nil)
	// _, publishSession 保存没有权威快照变化的扁平会话。
	_, publishSession := mtop.WithFlatCookieSession(ctx, publishDetail.Value)
	// publishValue、publishErr 保存单商品兼容 Cookie 写回结果。
	publishValue, publishErr := publishPort.persistPublishSession(ctx, itemapp.PublishInput{UserID: 1, CookieID: "cid"}, publishDetail.Value, publishDetail.MetadataJSON, publishSession, &mtop.PublishItemResult{UpdatedCookies: "unb=publish"}, nil)
	if publishErr != nil || publishValue != "unb=publish" {
		t.Fatalf("publish value=%q err=%v", publishValue, publishErr)
	}
	// _, publishCallErrValue、publishCallErr 保存平台调用失败时不采用兼容 Cookie 的结果。
	publishCallErrValue, publishCallErr := publishPort.persistPublishSession(ctx, itemapp.PublishInput{UserID: 1, CookieID: "cid"}, "unb=publish", publishDetail.MetadataJSON, publishSession, &mtop.PublishItemResult{UpdatedCookies: "unb=ignored"}, errors.New("publish failed"))
	if publishCallErr != nil || publishCallErrValue != "" {
		t.Fatalf("publish call error value=%q err=%v", publishCallErrValue, publishCallErr)
	}

	// batchStore、batchCleanup 保存批量发布写回测试存储。
	batchStore, batchCleanup := newAdapterTestStore(t)
	defer batchCleanup()
	// batchDetail、batchDetailErr 保存批量发布写回前的凭证视图。
	batchDetail, batchDetailErr := batchStore.Cookies.GetCookiePlatformRuntimeData(ctx, "cid")
	if batchDetailErr != nil {
		t.Fatal(batchDetailErr)
	}
	// batchPort 是绑定本地存储的批量发布适配器。
	batchPort := NewItemBatchPublishPort(batchStore, nil, nil, nil, nil, nil, nil)
	// _, batchSession 保存没有权威快照变化的扁平会话。
	_, batchSession := mtop.WithFlatCookieSession(ctx, batchDetail.Value)
	// batchValue、batchErr 保存批量发布兼容 Cookie 写回结果。
	batchValue, batchErr := batchPort.persistBatchSession(ctx, 1, "cid", batchDetail.Value, batchDetail.MetadataJSON, batchSession, &mtop.PublishItemResult{UpdatedCookies: "unb=batch"}, nil)
	if batchErr != nil || batchValue != "unb=batch" {
		t.Fatalf("batch value=%q err=%v", batchValue, batchErr)
	}
}

// TestItemSessionCommitPortsCoverAuthoritativeChanges 覆盖三个商品适配器提交权威 Cookie 快照的路径。
func TestItemSessionCommitPortsCoverAuthoritativeChanges(t *testing.T) {
	// ctx 是本测试数据库和 Cookie 会话共用的非取消上下文。
	ctx := context.Background()
	// categoryStore、categoryCleanup 保存商品类目权威写回测试存储。
	categoryStore, categoryCleanup := newAdapterTestStore(t)
	defer categoryCleanup()
	// categoryDetail、categoryDetailErr 保存商品类目权威写回前的凭证视图。
	categoryDetail, categoryDetailErr := categoryStore.Cookies.GetCookiePlatformRuntimeData(ctx, "cid")
	if categoryDetailErr != nil {
		t.Fatal(categoryDetailErr)
	}
	// _, categorySession 保存发生权威 Cookie 变化的会话。
	_, categorySession := mtop.WithCookieSnapshot(ctx, []BrowserCookie{{Name: "sid", Value: "new", Domain: ".goofish.com", Path: "/"}})
	categorySession.ReplaceSnapshot([]BrowserCookie{{Name: "sid", Value: "newer", Domain: ".goofish.com", Path: "/"}})
	// categoryValue、categoryErr 保存类目权威 Cookie 写回结果。
	categoryValue, categoryErr := NewItemPublishPort(categoryStore, nil, nil, nil, nil).persistCategorySession(ctx, 1, "cid", categoryDetail.Value, categoryDetail.MetadataJSON, categorySession, "")
	if categoryErr != nil || categoryValue == "" {
		t.Fatalf("category authoritative value=%q err=%v", categoryValue, categoryErr)
	}

	// publishStore、publishCleanup 保存单商品权威写回测试存储。
	publishStore, publishCleanup := newAdapterTestStore(t)
	defer publishCleanup()
	// publishDetail、publishDetailErr 保存单商品权威写回前的凭证视图。
	publishDetail, publishDetailErr := publishStore.Cookies.GetCookiePlatformRuntimeData(ctx, "cid")
	if publishDetailErr != nil {
		t.Fatal(publishDetailErr)
	}
	// _, publishSession 保存发生权威 Cookie 变化的会话。
	_, publishSession := mtop.WithCookieSnapshot(ctx, []BrowserCookie{{Name: "sid", Value: "new", Domain: ".goofish.com", Path: "/"}})
	publishSession.ReplaceSnapshot([]BrowserCookie{{Name: "sid", Value: "newer", Domain: ".goofish.com", Path: "/"}})
	// publishValue、publishErr 保存单商品权威 Cookie 写回结果。
	publishValue, publishErr := NewItemPublishPort(publishStore, nil, nil, nil, nil).persistPublishSession(ctx, itemapp.PublishInput{UserID: 1, CookieID: "cid"}, publishDetail.Value, publishDetail.MetadataJSON, publishSession, nil, errors.New("remote failed"))
	if publishErr != nil || publishValue == "" {
		t.Fatalf("publish authoritative value=%q err=%v", publishValue, publishErr)
	}

	// batchStore、batchCleanup 保存批量发布权威写回测试存储。
	batchStore, batchCleanup := newAdapterTestStore(t)
	defer batchCleanup()
	// batchDetail、batchDetailErr 保存批量发布权威写回前的凭证视图。
	batchDetail, batchDetailErr := batchStore.Cookies.GetCookiePlatformRuntimeData(ctx, "cid")
	if batchDetailErr != nil {
		t.Fatal(batchDetailErr)
	}
	// _, batchSession 保存发生权威 Cookie 变化的会话。
	_, batchSession := mtop.WithCookieSnapshot(ctx, []BrowserCookie{{Name: "sid", Value: "new", Domain: ".goofish.com", Path: "/"}})
	batchSession.ReplaceSnapshot([]BrowserCookie{{Name: "sid", Value: "newer", Domain: ".goofish.com", Path: "/"}})
	// batchValue、batchErr 保存批量发布权威 Cookie 写回结果。
	batchValue, batchErr := NewItemBatchPublishPort(batchStore, nil, nil, nil, nil, nil, nil).persistBatchSession(ctx, 1, "cid", batchDetail.Value, batchDetail.MetadataJSON, batchSession, nil, errors.New("remote failed"))
	if batchErr != nil || batchValue == "" {
		t.Fatalf("batch authoritative value=%q err=%v", batchValue, batchErr)
	}
}
