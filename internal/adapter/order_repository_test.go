package adapter

import (
	"reflect"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// TestOrderRepositoryModelConversions 保证数据库订单模型转换为应用模型时不丢失业务字段。
func TestOrderRepositoryModelConversions(t *testing.T) {
	// databaseOrder 保存模拟数据库读取的订单实体。
	databaseOrder := &db.Order{OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", SpecName: "颜色", SpecValue: "蓝", Quantity: "2", Amount: "19.90", OrderStatus: "pending_ship", CookieID: "cookie-1", IsBargain: 1, ReceiverName: "收货人", ReceiverPhone: "13800000000", ReceiverAddr: "上海市", ReceiverCity: "上海", Version: 3, ChatID: "chat-1", SystemShipped: true, PaidAt: "paid", ShippedAt: "shipped", CompletedAt: "completed", BuyerReviewedAt: "reviewed", LastReviewRequestAt: "requested", ReviewRequestCount: 2, CreatedAt: "created", UpdatedAt: "updated"}
	// expectedOrder 保存应用层应接收的订单实体。
	expectedOrder := &orderapp.Order{OrderID: "order-1", ItemID: "item-1", BuyerID: "buyer-1", SpecName: "颜色", SpecValue: "蓝", Quantity: "2", Amount: "19.90", OrderStatus: "pending_ship", CookieID: "cookie-1", IsBargain: 1, ReceiverName: "收货人", ReceiverPhone: "13800000000", ReceiverAddress: "上海市", ReceiverCity: "上海", Version: 3, ChatID: "chat-1", SystemShipped: true, PaidAt: "paid", ShippedAt: "shipped", CompletedAt: "completed", BuyerReviewedAt: "reviewed", LastReviewRequestAt: "requested", ReviewRequestCount: 2, CreatedAt: "created", UpdatedAt: "updated"}
	// convertedOrder 保存适配器转换后的应用订单。
	convertedOrder := orderFromDB(databaseOrder)
	if !reflect.DeepEqual(convertedOrder, expectedOrder) {
		t.Fatalf("订单转换结果不完整: got=%+v want=%+v", convertedOrder, expectedOrder)
	}
	if orderFromDB(nil) != nil {
		t.Fatal("空数据库订单应转换为空应用订单")
	}

	// databaseItem 保存模拟数据库读取的商品实体。
	databaseItem := &db.ItemInfo{ID: 7, CookieID: "cookie-1", ItemID: "item-1", ItemTitle: "商品", ItemDescription: "描述", ItemCategory: "分类", ItemPrice: "9.95", ItemDetail: `{"images":["a"]}`, IsMultiSpec: true, MultiQuantityDelivery: true}
	// expectedItem 保存应用层应接收的商品实体。
	expectedItem := &orderapp.ItemInfo{ID: 7, CookieID: "cookie-1", ItemID: "item-1", ItemTitle: "商品", ItemDescription: "描述", ItemCategory: "分类", ItemPrice: "9.95", ItemDetail: `{"images":["a"]}`, IsMultiSpec: true, MultiQuantityDelivery: true}
	// convertedItem 保存适配器转换后的应用商品。
	convertedItem := itemInfoFromDB(databaseItem)
	if !reflect.DeepEqual(convertedItem, expectedItem) {
		t.Fatalf("商品转换结果不完整: got=%+v want=%+v", convertedItem, expectedItem)
	}
	if itemInfoFromDB(nil) != nil {
		t.Fatal("空数据库商品应转换为空应用商品")
	}

	// databaseRuntime 保存模拟数据库读取的平台运行视图。
	databaseRuntime := db.CookiePlatformRuntimeData{ID: "cookie-1", UserID: 9, Value: "cookie", MetadataJSON: "{}", ShowBrowser: true}
	// expectedRuntime 保存应用层应接收的平台运行视图。
	expectedRuntime := &orderapp.PlatformRuntimeData{ID: "cookie-1", UserID: 9, Value: "cookie", MetadataJSON: "{}", ShowBrowser: true}
	// convertedRuntime 保存适配器转换后的平台运行视图。
	convertedRuntime := platformRuntimeDataFromDB(databaseRuntime)
	if !reflect.DeepEqual(convertedRuntime, expectedRuntime) {
		t.Fatalf("平台运行视图转换结果不完整: got=%+v want=%+v", convertedRuntime, expectedRuntime)
	}
}
