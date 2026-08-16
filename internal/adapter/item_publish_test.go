package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/xianyu/mtop"
)

// itemPublishClientStub 是单商品发布端口测试使用的平台客户端替身。
type itemPublishClientStub struct {
	// mtop.Client 提供未涉及本测试的方法默认实现。
	mtop.Client
	// publish 保存单商品发布请求的可控行为。
	publish func(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error)
	// recommend 保存类目推荐请求的可控行为。
	recommend func(context.Context, string, string) (mtop.PublishCategory, string, error)
}

// PublishItem 调用测试注入的单商品发布行为。
func (c itemPublishClientStub) PublishItem(ctx context.Context, cookies string, request mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return c.publish(ctx, cookies, request)
}

// RecommendPublishCategory 调用测试注入的类目推荐行为。
func (c itemPublishClientStub) RecommendPublishCategory(ctx context.Context, cookies, keyword string) (mtop.PublishCategory, string, error) {
	return c.recommend(ctx, cookies, keyword)
}

// unsupportedItemPublishClient 是不提供类目推荐能力的平台客户端替身。
type unsupportedItemPublishClient struct{ mtop.Client }

// TestItemPublishPortRecommendsCategoryAndPersistsUpdatedCookie 验证类目推荐和 Cookie 写回。
func TestItemPublishPortRecommendsCategoryAndPersistsUpdatedCookie(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 是返回类目及刷新 Cookie 的平台替身。
	client := itemPublishClientStub{recommend: func(_ context.Context, cookies, keyword string) (mtop.PublishCategory, string, error) {
		if cookies == "" || keyword != "资料" {
			t.Fatalf("推荐请求参数异常: cookies=%q keyword=%q", cookies, keyword)
		}
		return mtop.PublishCategory{CatID: "cat-1", CatName: "资料", ChannelCatID: "channel-1"}, "unb=2; _m_h5_tk=tk2;", nil
	}}
	// port 是绑定测试数据库和平台替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// category、err 保存类目推荐结果。
	category, err := port.RecommendCategory(context.Background(), 1, "cid", "资料")
	if err != nil || category.CatID != "cat-1" {
		t.Fatalf("类目推荐失败: category=%+v err=%v", category, err)
	}
	// value、valueErr 保存平台返回 Cookie 的本地持久化结果。
	value, valueErr := store.Cookies.GetValue(context.Background(), "cid")
	if valueErr != nil || value != "unb=2; _m_h5_tk=tk2;" {
		t.Fatalf("类目推荐 Cookie 未保存: value=%q err=%v", value, valueErr)
	}
}

// TestItemPublishPortRejectsUnsupportedCategoryRecommendation 验证未实现平台能力时返回应用错误。
func TestItemPublishPortRejectsUnsupportedCategoryRecommendation(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// port 是返回不支持能力客户端的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return unsupportedItemPublishClient{} }, nil, nil, nil)
	// err 保存能力缺失错误。
	_, err := port.RecommendCategory(context.Background(), 1, "cid", "资料")
	if !errors.Is(err, itemapp.ErrCategoryUnsupported) {
		t.Fatalf("未返回能力缺失错误: %v", err)
	}
}

// TestItemPublishPortReleasesCredentialLockDuringRemoteCall 验证单商品发布远端调用期间不占用凭证锁。
func TestItemPublishPortReleasesCredentialLockDuringRemoteCall(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// started 表示平台发布已进入慢速调用阶段。
	started := make(chan struct{})
	// release 控制测试平台调用继续返回。
	release := make(chan struct{})
	// client 是带阻塞控制的单商品发布平台替身。
	client := itemPublishClientStub{publish: func(_ context.Context, cookies string, request mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
		if cookies == "" || request.Quantity != 2 {
			t.Fatalf("发布请求未携带预期凭证或库存：cookies=%q quantity=%d", cookies, request.Quantity)
		}
		close(started)
		<-release
		return &mtop.PublishItemResult{ItemID: "published-1", UpdatedCookies: "unb=2; _m_h5_tk=tk2;"}, nil
	}}
	// port 是绑定测试数据库和平台替身的发布适配器。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// result 保存异步发布调用的结果。
	result := make(chan struct {
		outcome itemapp.PublishOutcome
		err     error
	}, 1)
	go func() {
		// outcome、err 保存发布适配器返回的结果及错误。
		outcome, err := port.Publish(context.Background(), itemapp.PublishInput{UserID: 1, CookieID: "cid", Quantity: 2})
		result <- struct {
			outcome itemapp.PublishOutcome
			err     error
		}{outcome: outcome, err: err}
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("平台发布未进入阻塞阶段")
	}
	// lockAcquired 表示并行操作已取得同账号凭证锁。
	lockAcquired := make(chan struct{})
	go func() {
		// unlock 释放并行测试操作取得的凭证锁。
		unlock := store.LockAccountCredentials("cid")
		close(lockAcquired)
		unlock()
	}()
	select {
	case <-lockAcquired:
	case <-time.After(time.Second):
		t.Fatal("远端发布期间凭证锁仍被占用")
	}
	close(release)
	select {
	// completed 保存发布 goroutine 返回的结果和错误。
	case completed := <-result:
		if completed.err != nil || completed.outcome.Result == nil || completed.outcome.Result.ItemID != "published-1" {
			t.Fatalf("发布结果异常：outcome=%+v err=%v", completed.outcome, completed.err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("发布适配器未收束")
	}
	// value、valueErr 保存平台返回 Cookie 的本地持久化结果。
	value, valueErr := store.Cookies.GetValue(context.Background(), "cid")
	if valueErr != nil || value != "unb=2; _m_h5_tk=tk2;" {
		t.Fatalf("平台返回 Cookie 未保存：value=%q err=%v", value, valueErr)
	}
}

// TestItemPublishRepositoryUpsertMapsApplicationRecord 验证商品仓储适配器正确转换应用记录。
func TestItemPublishRepositoryUpsertMapsApplicationRecord(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试数据库的商品仓储适配器。
	repository := NewItemPublishRepository(store)
	// err 保存应用商品记录写入结果。
	err := repository.Upsert(context.Background(), itemapp.ItemRecord{CookieID: "cid", ItemID: "local-1", ItemTitle: "商品", ItemDescription: "描述", ItemCategory: "cat", ItemPrice: "12.00", ItemDetail: `{"quantity":2}`, MultiQuantityDelivery: true})
	if err != nil {
		t.Fatalf("Upsert error: %v", err)
	}
	// record、recordErr 保存数据库读取到的商品记录。
	record, recordErr := store.Items.Get(context.Background(), "cid", "local-1")
	if recordErr != nil || record == nil || record.ItemTitle != "商品" || !record.MultiQuantityDelivery {
		t.Fatalf("商品记录转换异常：record=%+v err=%v", record, recordErr)
	}
}

// TestPublishErrorToApplicationMapsPlatformError 验证发布适配器转换错误时保留分类和原始错误链。
func TestPublishErrorToApplicationMapsPlatformError(t *testing.T) {
	// platformErr 是平台客户端返回的库存权限错误替身。
	platformErr := &mtop.PublishError{
		Code: mtop.PublishErrorStockPermissionMissing,
		Ret:  []string{"库存权限缺失"},
	}
	// convertedErr 是适配器转换后的应用层错误。
	convertedErr := publishErrorToApplication(platformErr)
	// applicationErr 是转换结果中的应用层发布错误。
	var applicationErr *itemapp.PublishError
	if !errors.As(convertedErr, &applicationErr) {
		t.Fatalf("未转换为应用发布错误: %v", convertedErr)
	}
	if applicationErr.Code != itemapp.PublishErrorStockPermissionMissing || applicationErr.Error() != "库存权限缺失" {
		t.Fatalf("应用发布错误分类异常: %+v", applicationErr)
	}
	// originalErr 是通过应用错误 Unwrap 保留的平台错误。
	var originalErr *mtop.PublishError
	if !errors.As(convertedErr, &originalErr) || originalErr != platformErr {
		t.Fatalf("未保留原始平台错误链: %v", convertedErr)
	}
}

// TestPublishErrorToApplicationKeepsNonPlatformError 验证普通基础设施错误不会被错误包装。
func TestPublishErrorToApplicationKeepsNonPlatformError(t *testing.T) {
	// plainErr 是不属于平台发布错误类型的基础设施错误。
	plainErr := errors.New("transport failed")
	// convertedErr 是普通错误经过适配器转换后的结果。
	convertedErr := publishErrorToApplication(plainErr)
	if convertedErr != plainErr || !errors.Is(convertedErr, plainErr) {
		t.Fatalf("普通错误不应被改写: got=%v", convertedErr)
	}
	if publishErrorToApplication(nil) != nil {
		t.Fatal("空错误应保持为空")
	}
}
