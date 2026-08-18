package adapter

import (
	"context"
	"errors"
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

// TestOrderRepositoryUnitOfWorkRollsBackCrossRepositoryWrites 验证应用层 Writer 回调失败时订单和商品写入作为整体回滚。
func TestOrderRepositoryUnitOfWorkRollsBackCrossRepositoryWrites(t *testing.T) {
	// store、cleanup 保存已迁移的 SQLite Store 及测试结束时关闭连接池的函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 保存只暴露订单应用 Port 的基础设施适配器。
	repository := NewOrderRepository(store)
	// ctx 保存本次事务写入和断言使用的取消上下文。
	ctx := context.Background()
	// rollbackErr 表示应用用例在两次写入后主动拒绝提交的业务错误。
	rollbackErr := errors.New("强制回滚订单和商品")
	// transactionErr 保存 Unit of Work 向调用方返回的回调错误。
	transactionErr := repository.WithTransaction(ctx, func(writer orderapp.Writer) error {
		// orderWriteErr 保存订单主体写入结果，成功后仍须等待整个事务提交才对外可见。
		orderWriteErr := writer.UpsertOrder(ctx, "uow-order", orderapp.UpsertOptions{CookieID: "cid", ItemID: "uow-item", OrderStatus: "pending_ship", Amount: "9.90"})
		if orderWriteErr != nil {
			return orderWriteErr
		}
		// itemWriteErr 保存关联商品基础信息写入结果，和订单共享同一个 Unit of Work。
		itemWriteErr := writer.UpsertItemBasic(ctx, orderapp.ItemWrite{CookieID: "cid", ItemID: "uow-item", ItemTitle: "事务商品", ItemPrice: "9.90"})
		if itemWriteErr != nil {
			return itemWriteErr
		}
		return rollbackErr
	})
	if !errors.Is(transactionErr, rollbackErr) {
		t.Fatalf("事务应透传回调错误，got %v", transactionErr)
	}
	// order、orderReadErr 保存回滚后订单读取结果；不存在证明先前订单写入没有部分提交。
	order, orderReadErr := store.Orders.Get(ctx, "uow-order")
	if !errors.Is(orderReadErr, db.ErrNotFound) || order != nil {
		t.Fatalf("回滚后订单不应存在，order=%+v err=%v", order, orderReadErr)
	}
	// item、itemReadErr 保存回滚后商品读取结果；不存在证明跨仓储写入具有原子性。
	item, itemReadErr := store.Items.Get(ctx, "cid", "uow-item")
	if !errors.Is(itemReadErr, db.ErrNotFound) || item != nil {
		t.Fatalf("回滚后商品不应存在，item=%+v err=%v", item, itemReadErr)
	}
}
