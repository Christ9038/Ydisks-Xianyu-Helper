package server

import (
	"reflect"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// TestOrderModelConversions 保证订单边界转换完整保留业务字段并正确处理空值。
func TestOrderModelConversions(t *testing.T) {
	// databaseOrder 是模拟数据库读取到的完整订单实体。
	databaseOrder := &db.Order{
		OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", SpecName: "颜色",
		SpecValue: "蓝", Quantity: "2", Amount: "19.90", OrderStatus: "pending_ship",
		CookieID: "cookie-1", IsBargain: 1, ReceiverName: "收货人", ReceiverPhone: "13800000000",
		ReceiverAddr: "上海市", ReceiverCity: "上海", Version: 3, ChatID: "chat-1",
		SystemShipped: true, PaidAt: "paid", ShippedAt: "shipped", CompletedAt: "completed",
		BuyerReviewedAt: "reviewed", LastReviewRequestAt: "requested", ReviewRequestCount: 2,
		CreatedAt: "created", UpdatedAt: "updated",
	}
	// applicationOrder 是订单应用层实体的预期结果。
	applicationOrder := &orderapp.Order{
		OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", SpecName: "颜色",
		SpecValue: "蓝", Quantity: "2", Amount: "19.90", OrderStatus: "pending_ship",
		CookieID: "cookie-1", IsBargain: 1, ReceiverName: "收货人", ReceiverPhone: "13800000000",
		ReceiverAddress: "上海市", ReceiverCity: "上海", Version: 3, ChatID: "chat-1",
		SystemShipped: true, PaidAt: "paid", ShippedAt: "shipped", CompletedAt: "completed",
		BuyerReviewedAt: "reviewed", LastReviewRequestAt: "requested", ReviewRequestCount: 2,
		CreatedAt: "created", UpdatedAt: "updated",
	}
	// converted 是数据库订单转换后的应用订单。
	converted := orderFromDB(databaseOrder)
	if !reflect.DeepEqual(converted, applicationOrder) {
		t.Fatalf("订单转换结果不完整: got=%+v want=%+v", converted, applicationOrder)
	}
	if orderFromDB(nil) != nil {
		t.Fatal("空数据库订单应转换为空应用订单")
	}

	// databaseItem 是模拟数据库读取到的完整商品实体。
	databaseItem := &db.ItemInfo{
		ID: 7, CookieID: "cookie-1", ItemID: "item-1", ItemTitle: "商品",
		ItemDescription: "描述", ItemCategory: "分类", ItemPrice: "9.95", ItemDetail: `{"images":["a"]}`,
		IsMultiSpec: true, MultiQuantityDelivery: true,
	}
	// applicationItem 是商品应用层实体的预期结果。
	applicationItem := &orderapp.ItemInfo{
		ID: 7, CookieID: "cookie-1", ItemID: "item-1", ItemTitle: "商品",
		ItemDescription: "描述", ItemCategory: "分类", ItemPrice: "9.95", ItemDetail: `{"images":["a"]}`,
		IsMultiSpec: true, MultiQuantityDelivery: true,
	}
	// convertedItem 是数据库商品转换后的应用商品。
	convertedItem := itemInfoFromDB(databaseItem)
	if !reflect.DeepEqual(convertedItem, applicationItem) {
		t.Fatalf("商品转换结果不完整: got=%+v want=%+v", convertedItem, applicationItem)
	}
	if itemInfoFromDB(nil) != nil {
		t.Fatal("空数据库商品应转换为空应用商品")
	}

	// databaseRuntime 是模拟数据库读取到的平台运行视图。
	databaseRuntime := db.CookiePlatformRuntimeData{ID: "cookie-1", UserID: 9, Value: "cookie", MetadataJSON: "{}", ShowBrowser: true}
	// applicationRuntime 是平台运行视图的预期应用层结果。
	applicationRuntime := &orderapp.PlatformRuntimeData{ID: "cookie-1", UserID: 9, Value: "cookie", MetadataJSON: "{}", ShowBrowser: true}
	// convertedRuntime 是数据库平台运行视图转换后的应用模型。
	convertedRuntime := platformRuntimeDataFromDB(databaseRuntime)
	if !reflect.DeepEqual(convertedRuntime, applicationRuntime) {
		t.Fatalf("平台运行视图转换结果不完整: got=%+v want=%+v", convertedRuntime, applicationRuntime)
	}
}

// TestOrderForAutomation 保证未迁移的自动化边界不会丢失订单字段。
func TestOrderForAutomation(t *testing.T) {
	// applicationOrder 是传入自动化中心适配器的应用订单。
	applicationOrder := &orderapp.Order{
		OrderID: "order-2", ItemID: "item-2", BuyerID: "buyer-2", ReceiverAddress: "地址",
		CookieID: "cookie-2", ChatID: "chat-2", OrderStatus: "pending_ship", Version: 4,
	}
	// expectedDatabaseOrder 是自动化中心当前接口仍接收的数据库订单。
	expectedDatabaseOrder := &db.Order{
		OrderID: "order-2", ItemID: "item-2", BuyerID: "buyer-2", ReceiverAddr: "地址",
		CookieID: "cookie-2", ChatID: "chat-2", OrderStatus: "pending_ship", Version: 4,
	}
	// converted 是传给自动化中心的数据库订单。
	converted := orderForAutomation(applicationOrder)
	if !reflect.DeepEqual(converted, expectedDatabaseOrder) {
		t.Fatalf("自动化边界订单转换错误: got=%+v want=%+v", converted, expectedDatabaseOrder)
	}
	if orderForAutomation(nil) != nil {
		t.Fatal("空应用订单应转换为空数据库订单")
	}
}

// TestCookieDetailForOrderPlatform 保证订单刷新访问共享会话辅助函数时只映射最小字段。
func TestCookieDetailForOrderPlatform(t *testing.T) {
	// runtimeData 是订单应用层持有的平台运行视图。
	runtimeData := &orderapp.PlatformRuntimeData{ID: "cookie-3", UserID: 11, Value: "value", MetadataJSON: "metadata", ShowBrowser: true}
	// expectedDetail 是共享 Server 会话辅助函数使用的兼容详情。
	expectedDetail := &db.CookieDetail{ID: "cookie-3", UserID: 11, Value: "value", MetadataJSON: "metadata", ShowBrowser: true}
	// converted 是共享 Server 会话辅助函数使用的平台详情。
	converted := cookieDetailForOrderPlatform(runtimeData)
	if !reflect.DeepEqual(converted, expectedDetail) {
		t.Fatalf("平台详情边界转换错误: got=%+v want=%+v", converted, expectedDetail)
	}
	if cookieDetailForOrderPlatform(nil) != nil {
		t.Fatal("空平台运行视图应转换为空平台详情")
	}
}
