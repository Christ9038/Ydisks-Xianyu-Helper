package adapter

import (
	"context"
	"errors"
	"testing"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/xianyu/cookierefresh"
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
	// downShelf 保存商品平台下架请求的可控行为。
	downShelf func(context.Context, string, string) (*mtop.AccountTaskResult, error)
}

// PublishItem 调用测试注入的单商品发布行为。
func (c itemPublishClientStub) PublishItem(ctx context.Context, cookies string, request mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
	return c.publish(ctx, cookies, request)
}

// RecommendPublishCategory 调用测试注入的类目推荐行为。
func (c itemPublishClientStub) RecommendPublishCategory(ctx context.Context, cookies, keyword string) (mtop.PublishCategory, string, error) {
	return c.recommend(ctx, cookies, keyword)
}

// DownShelfItem 调用测试注入的商品平台下架行为。
func (c itemPublishClientStub) DownShelfItem(ctx context.Context, cookies, itemID string) (*mtop.AccountTaskResult, error) {
	return c.downShelf(ctx, cookies, itemID)
}

// unsupportedItemPublishClient 是不提供类目推荐能力的平台客户端替身。
type unsupportedItemPublishClient struct{ mtop.Client }

// TestItemPublishPortDownShelvesPlatformItemAndPersistsCookie 验证平台下架成功后写回 Cookie，且不执行本地商品删除。
func TestItemPublishPortDownShelvesPlatformItemAndPersistsCookie(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 是检查下架账号凭证和商品 ID 的平台客户端替身。
	client := itemPublishClientStub{downShelf: func(_ context.Context, cookies, itemID string) (*mtop.AccountTaskResult, error) {
		if cookies == "" || itemID != "item-1" {
			t.Fatalf("下架请求参数异常: cookies=%q itemID=%q", cookies, itemID)
		}
		return &mtop.AccountTaskResult{Success: true, Message: "SUCCESS::下架成功", UpdatedCookies: "unb=2; _m_h5_tk=tk2; havana_lgc2_77=lgc;"}, nil
	}}
	// port 是绑定测试数据库和平台替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// outcome、err 保存平台下架和会话持久化结果。
	outcome, err := port.DownShelf(context.Background(), 1, "cid", "item-1")
	if err != nil || !outcome.PlatformSuccess || !outcome.CookiePersisted || outcome.Status != "off_sale" || outcome.ItemID != "item-1" {
		t.Fatalf("平台下架结果异常: outcome=%+v err=%v", outcome, err)
	}
	// value、valueErr 保存平台返回的新 Cookie 是否写回本地。
	value, valueErr := store.Cookies.GetValue(context.Background(), "cid")
	if valueErr != nil || value != "unb=2; _m_h5_tk=tk2; havana_lgc2_77=lgc;" {
		t.Fatalf("平台下架 Cookie 未保存: value=%q err=%v", value, valueErr)
	}
}

// TestItemPublishPortDownShelfUnchangedJarPreservesScopedCookieMetadata 验证未变化的权威 Cookie Jar 不会被扁平响应覆盖。
func TestItemPublishPortDownShelfUnchangedJarPreservesScopedCookieMetadata(t *testing.T) {
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是测试数据库操作的上下文。
	ctx := context.Background()
	// snapshot 包含文档域与私有域的完整凭证；后者不能被平面 Cookie 更新丢弃。
	snapshot := []cookierefresh.BrowserCookie{
		{Name: "unb", Value: "fresh", Domain: ".goofish.com", Path: "/", Secure: true},
		{Name: "session", Value: "private", Domain: "passport.goofish.com", Path: "/", Secure: true, HTTPOnly: true},
	}
	// metadata 保存完整 Cookie Jar 与自定义账号元数据。
	metadata := cookierefresh.MetadataWithSnapshot(`{"source":"fixture"}`, snapshot)
	// err 是本地测试将完整 Cookie Jar 写入账号存储时的结果。
	if err := store.Cookies.UpdateRenewalCookie(ctx, "cid", "unb=legacy", metadata, 1); err != nil {
		t.Fatal(err)
	}
	// client 不修改权威 Jar，只返回由 Jar 导出的最新扁平 Cookie。
	client := itemPublishClientStub{downShelf: func(_ context.Context, _ string, _ string) (*mtop.AccountTaskResult, error) {
		return &mtop.AccountTaskResult{Success: true, Message: "SUCCESS::下架成功", UpdatedCookies: "unb=fresh"}, nil
	}}
	// port 通过已有凭证锁与会话快照管理执行一次模拟下架。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// outcome、err 表示下架成功且无需覆盖权威 Jar 的结果。
	outcome, err := port.DownShelf(ctx, 1, "cid", "item-1")
	if err != nil || !outcome.PlatformSuccess || !outcome.CookiePersisted {
		t.Fatalf("权威 Jar 不变时下架应成功: outcome=%+v err=%v", outcome, err)
	}
	// stored、readErr 保存本次完成后账号的全部元数据，不能丢失私有域会话。
	stored, readErr := store.Cookies.GetCookiePlatformRuntimeData(ctx, "cid")
	if readErr != nil || stored.MetadataJSON != metadata || stored.Value != "unb=legacy" {
		t.Fatalf("权威 Jar 被扁平 Cookie 覆盖: err=%v metadata_equal=%v value_equal=%v", readErr, stored.MetadataJSON == metadata, stored.Value == "unb=legacy")
	}
}

// TestItemPublishPortDownShelfRejectsWrongOwnerAndUnsupportedClient 验证归属错误和客户端能力缺失不会触发平台写操作。
func TestItemPublishPortDownShelfRejectsWrongOwnerAndUnsupportedClient(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// calls 记录平台下架请求次数。
	calls := 0
	// client 是记录请求次数的商品平台客户端替身。
	client := itemPublishClientStub{downShelf: func(context.Context, string, string) (*mtop.AccountTaskResult, error) {
		calls++
		return &mtop.AccountTaskResult{Success: true}, nil
	}}
	// port 是绑定测试数据库和平台替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// err 是错误用户尝试下架商品时的账号归属校验结果。
	if _, err := port.DownShelf(context.Background(), 999, "cid", "item-1"); !errors.Is(err, itemapp.ErrDownShelfCredentialChanged) {
		t.Fatalf("错误账号归属应被拒绝: %v", err)
	}
	if calls != 0 {
		t.Fatalf("归属校验失败后仍触发了平台请求: calls=%d", calls)
	}
	// unsupportedPort 是不提供商品生命周期扩展能力的平台端口。
	unsupportedPort := NewItemPublishPort(store, func() mtop.Client { return unsupportedItemPublishClient{} }, nil, nil, nil)
	// err 是当前平台客户端不支持下架能力时返回的应用错误。
	if _, err := unsupportedPort.DownShelf(context.Background(), 1, "cid", "item-1"); !errors.Is(err, itemapp.ErrDownShelfUnsupported) {
		t.Fatalf("未支持平台客户端应返回能力错误: %v", err)
	}
}

// TestItemPublishPortDownShelfReportsCookiePersistenceFailure 验证平台已下架但 Cookie 写回失败时明确返回不完整结果。
func TestItemPublishPortDownShelfReportsCookiePersistenceFailure(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 在平台确认下架后关闭测试数据库以模拟会话写回失败。
	client := itemPublishClientStub{downShelf: func(context.Context, string, string) (*mtop.AccountTaskResult, error) {
		// err 是关闭隔离测试数据库以模拟本地会话写回失败时的错误。
		if err := store.DB.Close(); err != nil {
			t.Fatal(err)
		}
		return &mtop.AccountTaskResult{Success: true, Message: "SUCCESS::下架成功"}, nil
	}}
	// port 是绑定数据库和下架平台替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// outcome、err 保存平台成功但本地写回失败的结果。
	outcome, err := port.DownShelf(context.Background(), 1, "cid", "item-1")
	if !outcome.PlatformSuccess || outcome.CookiePersisted || !errors.Is(err, itemapp.ErrDownShelfSessionPersistence) {
		t.Fatalf("平台成功与本地写回失败未被区分: outcome=%+v err=%v", outcome, err)
	}
}

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
	if err != nil || category.Category.CatID != "cat-1" || len(category.Candidates) != 1 || category.Candidates[0].CatID != "cat-1" {
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

// TestItemPublishPortMapsPreferredCategory 验证应用层类目会完整转换为 MTOP 发布请求。
func TestItemPublishPortMapsPreferredCategory(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// receivedCategory 保存适配器转换后的 MTOP 类目。
	var receivedCategory *mtop.PublishCategory
	// client 是检查人工类目映射结果的平台客户端替身。
	client := itemPublishClientStub{publish: func(_ context.Context, _ string, request mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
		receivedCategory = request.PreferredCategory
		return &mtop.PublishItemResult{ItemID: "category-item", Title: "类目商品"}, nil
	}}
	// port 是绑定测试数据库和平台替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// outcome、err 保存带人工类目发布的结果。
	outcome, err := port.Publish(context.Background(), itemapp.PublishInput{
		UserID: 1, CookieID: "cid", Title: "类目商品", PriceCents: 100, Quantity: 1,
		Category: &itemapp.PublishCategory{CatID: "5001", CatName: "虚拟服务", ChannelCatID: "6001", TBCatID: "7001"},
	})
	if err != nil || outcome.Result == nil || receivedCategory == nil {
		t.Fatalf("人工类目发布失败: outcome=%+v err=%v category=%+v", outcome, err, receivedCategory)
	}
	if receivedCategory.CatID != "5001" || receivedCategory.CatName != "虚拟服务" || receivedCategory.ChannelCatID != "6001" || receivedCategory.TBCatID != "7001" {
		t.Fatalf("人工类目转换错误: %+v", receivedCategory)
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

// TestItemPublishPortRetriesAfterSessionRecovery 验证续期成功后发布端口使用重新读取的凭证重试一次。
func TestItemPublishPortRetriesAfterSessionRecovery(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// calls 保存平台发布调用次数，用于确认最多执行一次恢复后的重试。
	calls := 0
	// secondCookies 保存第二次发布实际使用的凭证快照。
	secondCookies := ""
	// client 是首次返回会话失效、第二次成功的平台客户端替身。
	client := itemPublishClientStub{publish: func(_ context.Context, cookies string, _ mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
		calls++
		if calls == 1 {
			return nil, &mtop.SessionExpiredError{API: "publish"}
		}
		secondCookies = cookies
		return &mtop.PublishItemResult{ItemID: "retry-item", Title: "重试商品"}, nil
	}}
	// recovered 保存恢复回调是否被调用，确保发布端口不会静默跳过会话恢复。
	recovered := false
	// recovery 保存测试用会话恢复回调，并模拟恢复逻辑写入的新凭证。
	recovery := func(ctx context.Context, cookieID string, recoveryErr error) bool {
		recovered = recoveryErr != nil
		// updateErr 保存模拟续期凭证写入的持久化错误。
		if updateErr := store.Cookies.UpdateValueExisting(ctx, cookieID, "unb=1; fresh=1"); updateErr != nil {
			t.Fatalf("写入续期凭证失败: %v", updateErr)
		}
		return true
	}
	// port 是注入恢复回调后的发布适配器。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, recovery)
	// outcome、publishErr 保存恢复重试后的发布结果及错误。
	outcome, publishErr := port.Publish(context.Background(), itemapp.PublishInput{UserID: 1, CookieID: "cid", Title: "重试商品", PriceCents: 100, Quantity: 1})
	if publishErr != nil || outcome.Result == nil || outcome.Result.ItemID != "retry-item" {
		t.Fatalf("恢复后发布结果异常: outcome=%+v err=%v", outcome, publishErr)
	}
	if !recovered || calls != 2 || secondCookies != "unb=1; fresh=1" {
		t.Fatalf("续期重试未使用新凭证: recovered=%v calls=%d cookies=%q", recovered, calls, secondCookies)
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

// itemPublishCandidateClientStub 在单项推荐替身之上额外提供候选类目能力。
type itemPublishCandidateClientStub struct {
	// itemPublishClientStub 提供其余平台客户端行为。
	itemPublishClientStub
	// candidates 保存候选类目推荐请求的可控行为。
	candidates func(context.Context, string, string) ([]mtop.PublishCategory, string, error)
}

// RecommendPublishCategoryCandidates 调用测试注入的候选类目推荐行为。
func (c itemPublishCandidateClientStub) RecommendPublishCategoryCandidates(ctx context.Context, cookies, keyword string) ([]mtop.PublishCategory, string, error) {
	return c.candidates(ctx, cookies, keyword)
}

// TestItemPublishPortRecommendsCategoryCandidates 验证端口优先使用候选能力并保持主推荐在首位，且空候选按未识别处理。
func TestItemPublishPortRecommendsCategoryCandidates(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// results 保存候选替身本次要返回的类目；后续用例会替换为空列表。
	results := []mtop.PublishCategory{
		{CatID: "1", CatName: "电子资料", ChannelCatID: "11"},
		{CatID: "2", CatName: "游戏软件平台", ChannelCatID: "22", TBCatID: "222"},
	}
	// client 是同时实现单项与候选推荐的平台替身；单项推荐若被调用说明没有优先使用候选能力。
	client := itemPublishCandidateClientStub{
		itemPublishClientStub: itemPublishClientStub{recommend: func(context.Context, string, string) (mtop.PublishCategory, string, error) {
			t.Fatal("存在候选能力时不应回退到单项推荐")
			return mtop.PublishCategory{}, "", nil
		}},
		candidates: func(context.Context, string, string) ([]mtop.PublishCategory, string, error) { return results, "", nil },
	}
	// port 是绑定候选替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// result、err 保存应用层推荐结果。
	result, err := port.RecommendCategory(context.Background(), 1, "cid", "游戏")
	if err != nil || result.Category.CatID != "1" || len(result.Candidates) != 2 || result.Candidates[1].CatName != "游戏软件平台" || result.Candidates[1].TBCatID != "222" {
		t.Fatalf("候选类目结果异常: %+v err=%v", result, err)
	}
	results = nil
	// emptyErr 保存空候选时的应用层错误。
	if _, emptyErr := port.RecommendCategory(context.Background(), 1, "cid", "游戏"); !errors.Is(emptyErr, itemapp.ErrCategoryUnrecognized) {
		t.Fatalf("空候选应映射为未识别: %v", emptyErr)
	}
}

// TestItemPublishPortDownShelfFailurePaths 验证存储缺失、平台报错、会话过期恢复、未确认成功与运行时 Cookie 同步。
func TestItemPublishPortDownShelfFailurePaths(t *testing.T) {
	// nilPort 是未初始化的端口，必须返回错误而非 panic。
	var nilPort *ItemPublishPort
	// err 是空端口调用平台下架时返回的初始化错误，必须非空而不是 panic。
	if _, err := nilPort.DownShelf(context.Background(), 1, "cid", "item-1"); err == nil {
		t.Fatal("空端口必须返回错误")
	}
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// recovered 记录会话恢复回调收到的账号；running 记录同步给运行时的 Cookie。
	var recovered, running string
	// platformErr 是测试注入的平台调用错误。
	var platformErr error
	// result 是测试注入的平台下架结果。
	var result *mtop.AccountTaskResult
	// client 返回可切换的平台结果。
	client := itemPublishClientStub{downShelf: func(context.Context, string, string) (*mtop.AccountTaskResult, error) {
		return result, platformErr
	}}
	// port 绑定恢复与运行时同步回调。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil,
		func(_ context.Context, cookieID, cookie string) { running = cookieID + "=" + cookie },
		func(_ context.Context, cookieID string, _ error) bool { recovered = cookieID; return true })

	// 平台普通错误：原样返回，不触发会话恢复。
	platformErr = errors.New("platform boom")
	// err 是平台普通错误被原样返回的结果；同时 recovered 必须仍为空，表示未触发会话恢复。
	if _, err := port.DownShelf(context.Background(), 1, "cid", "item-1"); !errors.Is(err, platformErr) || recovered != "" {
		t.Fatalf("平台错误应原样返回且不恢复会话: err=%v recovered=%q", err, recovered)
	}
	// 会话过期错误：返回错误并通知账号恢复协调器。
	platformErr = &mtop.SessionExpiredError{}
	// err 是会话过期错误的返回结果；recovered 记录被通知恢复的账号，必须是 cid。
	if _, err := port.DownShelf(context.Background(), 1, "cid", "item-1"); err == nil || recovered != "cid" {
		t.Fatalf("会话过期应触发恢复: err=%v recovered=%q", err, recovered)
	}
	// 平台未确认成功：result 为空或 Success=false 都视为失败，且不能报告 off_sale。
	platformErr = nil
	// candidate 是平台未确认成功的两种形态：空结果与 Success=false 的结果。
	for _, candidate := range []*mtop.AccountTaskResult{nil, {Success: false, Message: "FAIL"}} {
		result = candidate
		// outcome、err 是平台未确认成功时下架端口的结果；必须失败且不能声明平台已下架。
		if outcome, err := port.DownShelf(context.Background(), 1, "cid", "item-1"); err == nil || outcome.PlatformSuccess {
			t.Fatalf("未确认成功必须失败: outcome=%+v err=%v", outcome, err)
		}
	}
	// 成功且返回新 Cookie：写回后同步到运行时账号。
	result = &mtop.AccountTaskResult{Success: true, Message: "ok", UpdatedCookies: "unb=9; _m_h5_tk=tk9; havana_lgc2_77=lgc;"}
	// outcome、err 是平台确认下架并返回新 Cookie 时的结果；必须成功且 Cookie 已落库。
	if outcome, err := port.DownShelf(context.Background(), 1, "cid", "item-1"); err != nil || !outcome.CookiePersisted {
		t.Fatalf("成功下架: outcome=%+v err=%v", outcome, err)
	}
	if running != "cid=unb=9; _m_h5_tk=tk9; havana_lgc2_77=lgc;" {
		t.Fatalf("运行时 Cookie 未同步: %q", running)
	}
	// 平台报错且伴随会话写回失败：错误中同时保留两类原因。
	platformErr = errors.New("platform boom 2")
	// client2 是在平台调用期间关闭数据库的替身，用于模拟平台报错同时本地写回失败。
	client2 := itemPublishClientStub{downShelf: func(context.Context, string, string) (*mtop.AccountTaskResult, error) {
		store.DB.Close()
		return nil, platformErr
	}}
	// port2 是绑定上述替身的商品发布端口。
	port2 := NewItemPublishPort(store, func() mtop.Client { return client2 }, nil, nil, nil)
	// err 是平台错误与会话写回错误同时发生时的合并错误，两类原因都必须可被 errors.Is 识别。
	if _, err := port2.DownShelf(context.Background(), 1, "cid", "item-1"); !errors.Is(err, platformErr) || !errors.Is(err, itemapp.ErrDownShelfCredentialChanged) {
		t.Fatalf("应同时保留平台错误与写回错误: %v", err)
	}
}
