package adapter

import (
	"context"
	"errors"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/xianyu/mtop"
)

// TestItemPublishPortCoversInputAndPlatformErrorBranches 验证单商品发布的空依赖、归属校验、空响应和普通平台错误分支。
func TestItemPublishPortCoversInputAndPlatformErrorBranches(t *testing.T) {
	// ctx 是本测试发布入口共用的非取消上下文。
	ctx := context.Background()
	// emptyPort 表示未装配存储的单商品发布端口。
	var emptyPort *ItemPublishPort
	// emptyPortErr 保存空发布端口返回的依赖错误。
	_, emptyPortErr := emptyPort.Publish(ctx, itemapp.PublishInput{})
	if emptyPortErr == nil {
		t.Fatal("空发布端口应拒绝执行")
	}
	// store、cleanup 保存本地商品发布测试数据库及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// platformErr 是平台返回的普通传输错误。
	platformErr := errors.New("platform unavailable")
	// publishMode 控制平台替身返回普通错误、空响应或成功结果。
	publishMode := "error"
	// recovered 记录普通平台错误是否通知恢复回调。
	recovered := false
	// client 是可切换响应类型的单商品发布平台替身。
	client := itemPublishClientStub{publish: func(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
		switch publishMode {
		case "error":
			return nil, platformErr
		case "empty":
			return nil, nil
		default:
			return &mtop.PublishItemResult{ItemID: "item"}, nil
		}
	}}
	// port 是绑定平台替身和恢复回调的单商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, func(_ context.Context, _ string, _ error) bool {
		recovered = true
		return false
	})
	// _, ownershipErr 保存错误账号归属的发布结果。
	_, ownershipErr := port.Publish(ctx, itemapp.PublishInput{UserID: 999, CookieID: "cid", Title: "商品", PriceCents: 100, Quantity: 1})
	if ownershipErr == nil {
		t.Fatal("错误账号归属应被拒绝")
	}
	// _, platformCallErr 保存普通平台错误及恢复回调结果。
	_, platformCallErr := port.Publish(ctx, itemapp.PublishInput{UserID: 1, CookieID: "cid", Title: "商品", PriceCents: 100, Quantity: 1})
	if !errors.Is(platformCallErr, platformErr) || !recovered {
		t.Fatalf("普通平台错误分支异常：err=%v recovered=%v", platformCallErr, recovered)
	}
	// publishMode 切换到无结果响应，覆盖平台成功但未返回商品的兼容分支。
	publishMode = "empty"
	// emptyOutcome、emptyErr 保存空平台响应的应用层结果。
	emptyOutcome, emptyErr := port.Publish(ctx, itemapp.PublishInput{UserID: 1, CookieID: "cid", Title: "商品", PriceCents: 100, Quantity: 1})
	if emptyErr != nil || emptyOutcome.Result != nil {
		t.Fatalf("空平台响应处理异常：outcome=%+v err=%v", emptyOutcome, emptyErr)
	}

	// emptyRepository 表示未装配存储的商品本地仓储。
	emptyRepository := NewItemPublishRepository(nil)
	// emptyRepositoryErr 保存空商品仓储返回的依赖错误。
	emptyRepositoryErr := emptyRepository.Upsert(ctx, itemapp.ItemRecord{CookieID: "cid", ItemID: "item"})
	if emptyRepositoryErr == nil {
		t.Fatal("空商品仓储应拒绝写入")
	}
	// nilClientPort 表示客户端回调返回空值的发布端口。
	nilClientPort := NewItemPublishPort(store, func() mtop.Client { return nil }, nil, nil, nil)
	if nilClientPort.mtopClient() == nil {
		t.Fatal("空客户端回调应回退默认平台客户端")
	}
	// emptyPortRecovery 验证空接收者和空错误不会误触发会话恢复。
	var emptyPortRecovery *ItemPublishPort
	if emptyPortRecovery.recoverExpired(ctx, "cid", platformErr) {
		t.Fatal("空发布端口不应报告会话恢复成功")
	}
	if port.recoverExpired(ctx, "cid", nil) {
		t.Fatal("空平台错误不应触发会话恢复")
	}
}

// TestItemPublishPortCoversCategoryErrorMappings 验证类目推荐的账号归属、平台错误和会话持久化错误映射。
func TestItemPublishPortCoversCategoryErrorMappings(t *testing.T) {
	// ctx 是本测试类目推荐入口共用的非取消上下文。
	ctx := context.Background()
	// emptyPort 表示未装配存储的空类目推荐端口。
	var emptyPort *ItemPublishPort
	// emptyPortErr 保存空端口依赖校验错误。
	_, emptyPortErr := emptyPort.RecommendCategory(ctx, 1, "cid", "关键词")
	if emptyPortErr == nil {
		t.Fatal("空类目推荐端口应拒绝执行")
	}
	// store、cleanup 保存类目账号归属校验测试数据库及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 是返回不同平台错误的类目推荐平台替身。
	client := itemPublishClientStub{recommend: func(context.Context, string, string) (mtop.PublishCategory, string, error) {
		return mtop.PublishCategory{}, "", mtop.ErrPublishCategoryUnrecognized
	}}
	// port 是绑定类目错误替身的推荐端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// _, missingErr 保存不存在账号的凭证校验错误。
	_, missingErr := port.RecommendCategory(ctx, 1, "missing", "关键词")
	if !errors.Is(missingErr, itemapp.ErrCategoryCredentialChanged) {
		t.Fatalf("不存在账号错误映射异常：%v", missingErr)
	}
	// _, ownerErr 保存错误用户访问账号的归属校验错误。
	_, ownerErr := port.RecommendCategory(ctx, 999, "cid", "关键词")
	if !errors.Is(ownerErr, itemapp.ErrCategoryCredentialChanged) {
		t.Fatalf("错误账号归属错误映射异常：%v", ownerErr)
	}
	// _, unrecognizedErr 保存平台未识别类目时的应用层错误。
	_, unrecognizedErr := port.RecommendCategory(ctx, 1, "cid", "关键词")
	if !errors.Is(unrecognizedErr, itemapp.ErrCategoryUnrecognized) {
		t.Fatalf("平台类目错误映射异常：%v", unrecognizedErr)
	}

	// persistenceStore、persistenceCleanup 保存会话写回失败测试数据库。
	persistenceStore, persistenceCleanup := newAdapterTestStore(t)
	defer persistenceCleanup()
	// persistenceClient 是在平台返回后关闭数据库的类目平台替身。
	persistenceClient := itemPublishClientStub{recommend: func(context.Context, string, string) (mtop.PublishCategory, string, error) {
		// closeErr 保存模拟数据库不可用的关闭结果。
		if closeErr := persistenceStore.DB.Close(); closeErr != nil {
			t.Fatal(closeErr)
		}
		return mtop.PublishCategory{CatID: "cat"}, "unb=next", nil
	}}
	// persistencePort 是绑定会话写回失败替身的类目推荐端口。
	persistencePort := NewItemPublishPort(persistenceStore, func() mtop.Client { return persistenceClient }, nil, nil, nil)
	// _, persistenceErr 保存平台成功但 Cookie 会话无法写回的应用层错误。
	_, persistenceErr := persistencePort.RecommendCategory(ctx, 1, "cid", "关键词")
	if !errors.Is(persistenceErr, itemapp.ErrCategoryPersistence) {
		t.Fatalf("会话持久化错误映射异常：%v", persistenceErr)
	}
}
