package adapter

import (
	"context"
	"testing"

	"xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestAccountSettingsAndCookieSessionGuards 验证账号设置锁和平台 Cookie 会话包装的边界语义。
func TestAccountSettingsAndCookieSessionGuards(t *testing.T) {
	// nilRepository 保存缺少 Store 的账号设置适配器。
	nilRepository := NewAccountSettingsRepository(nil)
	// unlock 保存缺少 Store 时仍应可安全调用的空解锁函数。
	unlock := nilRepository.LockCredentials("cid")
	unlock()
	// detail 保存不同凭证形态的应用层脱敏详情。
	detail := &account.CredentialDetail{Value: "cookie"}
	if !HasStoredCookieCredential(detail) || HasStoredCookieCredential(nil) {
		t.Fatal("凭证存在性判断异常")
	}
	// metadata 保存带完整浏览器快照的凭证元数据。
	metadata := cookierefresh.MetadataWithSnapshot(`{"other":true}`, []cookierefresh.BrowserCookie{{Name: "unb", Value: "id", Domain: ".goofish.com", Path: "/"}})
	if !HasStoredCookieCredential(&account.CredentialDetail{MetadataJSON: metadata}) {
		t.Fatal("完整 Cookie 快照应被识别为有效凭证")
	}
	// ctx 是本测试 Cookie 会话包装共用的上下文。
	ctx := context.Background()
	// snapshotContext、snapshotSession 保存完整 Cookie 快照会话及上下文。
	snapshotContext, snapshotSession := WithCookieSnapshot(ctx, []BrowserCookie{{Name: "unb", Value: "id"}})
	if snapshotContext == nil || snapshotSession == nil {
		t.Fatal("完整 Cookie 会话构造失败")
	}
	// flatContext、flatSession 保存旧版扁平 Cookie 会话及上下文。
	flatContext, flatSession := WithFlatCookieSession(ctx, "unb=id")
	if flatContext == nil || flatSession == nil {
		t.Fatal("扁平 Cookie 会话构造失败")
	}
}

// TestItemAndSystemDependenciesConstructPorts 验证商品和系统依赖组在空值与完整 Store 下的构造边界。
func TestItemAndSystemDependenciesConstructPorts(t *testing.T) {
	// nilItem、nilItemErr 保存缺少商品数据库时的依赖构造结果。
	nilItem, nilItemErr := NewItemDependencies(nil)
	if nilItem != nil || nilItemErr == nil {
		t.Fatalf("空商品依赖构造异常 dependencies=%v err=%v", nilItem, nilItemErr)
	}
	// nilSystem 保存缺少系统数据库时的依赖构造结果。
	if NewSystemDependencies(nil) != nil {
		t.Fatal("空系统依赖不应构造成功")
	}
	// nilChat 保存缺少数据库时的聊天依赖构造结果。
	if NewChatDependencies(nil) != nil {
		t.Fatal("空聊天依赖不应构造成功")
	}
	// nilOrder、nilOrderErr 保存缺少订单数据库时的依赖构造结果。
	nilOrder, nilOrderErr := NewOrderDependencies(nil)
	if nilOrder != nil || nilOrderErr == nil {
		t.Fatalf("空订单依赖构造异常 dependencies=%v err=%v", nilOrder, nilOrderErr)
	}
	// store、cleanup 保存完整依赖组使用的测试数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// itemDependencies 保存商品端口构造入口。
	itemDependencies, itemErr := NewItemDependencies(store)
	if itemErr != nil || itemDependencies == nil {
		t.Fatalf("商品依赖构造失败 dependencies=%v err=%v", itemDependencies, itemErr)
	}
	if itemDependencies.NewItemBatchRepository() == nil || itemDependencies.NewItemBatchPreviewPort() == nil || itemDependencies.NewItemPublishRepository() == nil || itemDependencies.NewItemCatalogRepository() == nil {
		t.Fatal("商品数据库端口构造不完整")
	}
	if itemDependencies.NewItemBatchPublishPort(nil, nil, nil, nil, nil, nil) == nil || itemDependencies.NewItemPublishPort(nil, nil, nil, nil) == nil || itemDependencies.NewItemSyncRepository(nil, nil, nil, nil) == nil {
		t.Fatal("商品平台端口构造不完整")
	}
	// systemDependencies 保存系统端口构造入口。
	systemDependencies := NewSystemDependencies(store)
	if systemDependencies.NewDatabaseHealth() == nil || systemDependencies.NewReconciliationService(nil) == nil {
		t.Fatal("系统端口构造不完整")
	}
	// chatDependencies 保存聊天端口构造入口。
	chatDependencies := NewChatDependencies(store)
	if chatDependencies.NewChatSendingApplication(nil, nil, nil) == nil {
		t.Fatal("聊天端口构造不完整")
	}
	// orderDependencies 保存订单端口构造入口。
	orderDependencies, orderErr := NewOrderDependencies(store)
	if orderErr != nil || orderDependencies == nil || orderDependencies.NewOrderRepository() == nil || orderDependencies.NewOrderReconciliationRepository() == nil || orderDependencies.NewOrderRuntime(OrderRuntimeHooks{}, nil, nil) == nil || orderDependencies.NewOrderRefreshJobRepository() == nil {
		t.Fatalf("订单端口构造不完整 dependencies=%v err=%v", orderDependencies, orderErr)
	}
	// nilChatDependencies 保存 nil receiver 的兼容调用结果。
	var nilChatDependencies *ChatDependencies
	if nilChatDependencies.NewChatSendingApplication(nil, nil, nil) == nil {
		t.Fatal("nil 聊天依赖应委托兼容构造")
	}
	// nilOrderDependencies 保存 nil receiver 的订单依赖构造结果。
	var nilOrderDependencies *OrderDependencies
	if nilOrderDependencies.NewOrderRepository() != nil || nilOrderDependencies.NewOrderRuntime(OrderRuntimeHooks{}, nil, nil) != nil {
		t.Fatal("nil 订单依赖不应构造端口")
	}
}
