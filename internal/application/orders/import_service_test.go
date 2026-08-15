package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// importRepositoryFake 是订单导入应用服务测试使用的内存 repository。
type importRepositoryFake struct {
	// ownedIDs 保存测试用户拥有的账号标识。
	ownedIDs []string
	// listErr 保存账号列表查询错误。
	listErr error
	// txErr 保存事务创建或提交错误。
	txErr error
	// itemErr 保存商品写入错误。
	itemErr error
	// writer 保存最近一次事务写入器。
	writer *importWriterFake
}

// ListOwnedIDs 返回测试用户拥有的账号标识。
func (f *importRepositoryFake) ListOwnedIDs(context.Context, int64) ([]string, error) {
	return f.ownedIDs, f.listErr
}

// WithTransaction 执行测试事务回调并记录写入。
func (f *importRepositoryFake) WithTransaction(ctx context.Context, work func(Writer) error) error {
	if f.txErr != nil {
		return f.txErr
	}
	// writer 保存当前事务使用的测试写入器。
	f.writer = &importWriterFake{itemErr: f.itemErr}
	return work(f.writer)
}

// importWriterFake 是订单导入应用服务测试使用的事务写入器。
type importWriterFake struct {
	// orderID 保存写入订单标识。
	orderID string
	// options 保存订单写入字段。
	options UpsertOptions
	// item 保存商品基础信息写入字段。
	item ItemWrite
	// orderErr 保存订单写入错误。
	orderErr error
	// itemErr 保存商品写入错误。
	itemErr error
}

// UpsertOrder 记录测试订单写入。
func (f *importWriterFake) UpsertOrder(_ context.Context, orderID string, options UpsertOptions) error {
	f.orderID = orderID
	f.options = options
	return f.orderErr
}

// UpsertItemBasic 记录测试商品写入。
func (f *importWriterFake) UpsertItemBasic(_ context.Context, item ItemWrite) error {
	f.item = item
	return f.itemErr
}

// PatchOrder 满足订单事务写入器接口的测试兼容方法。
func (f *importWriterFake) PatchOrder(context.Context, string, OrderPatch) error {
	return nil
}

// TestImportServiceWritesOrderAndItem 验证默认账号、状态和金额归一化及事务写入。
func TestImportServiceWritesOrderAndItem(t *testing.T) {
	// repository 保存本用例使用的内存依赖。
	repository := &importRepositoryFake{ownedIDs: []string{"cookie-1"}}
	// service 保存待测试的订单导入服务。
	service := NewImportService(repository)
	// result、err 保存订单导入结果和错误。
	result, err := service.Import(context.Background(), 7, []ImportOrder{{
		OrderID: " order-1 ", ItemID: "item-1", ItemTitle: "商品", ItemPrice: "19.9", ItemDetail: "{}",
		OrderStatus: "2", Amount: "¥1,234.50", Quantity: "2",
	}})
	if err != nil || result.SuccessCount != 1 || result.FailedCount != 0 {
		t.Fatalf("导入结果异常: result=%+v err=%v", result, err)
	}
	if repository.writer == nil || repository.writer.orderID != "order-1" || repository.writer.options.OrderStatus != "pending_ship" || repository.writer.options.Amount != "1234.50" {
		t.Fatalf("订单写入异常: %+v", repository.writer)
	}
	if repository.writer.item.ItemID != "item-1" || repository.writer.item.ItemTitle != "商品" {
		t.Fatalf("商品写入异常: %+v", repository.writer.item)
	}
}

// TestImportServiceReportsRowFailures 验证单条字段和所有权错误只影响当前结果行。
func TestImportServiceReportsRowFailures(t *testing.T) {
	// repository 保存本用例使用的内存依赖。
	repository := &importRepositoryFake{ownedIDs: []string{"cookie-1", "cookie-2"}}
	// service 保存待测试的订单导入服务。
	service := NewImportService(repository)
	// result、err 保存混合导入结果和错误。
	result, err := service.Import(context.Background(), 7, []ImportOrder{
		{OrderID: "valid", CookieID: "cookie-1"},
		{OrderID: "bad-amount", CookieID: "cookie-1", Amount: "1e3"},
		{OrderID: "bad-status", CookieID: "cookie-1", OrderStatus: "unknown"},
		{OrderID: "forbidden", CookieID: "other"},
		{CookieID: "cookie-1"},
	})
	if err != nil || result.SuccessCount != 1 || result.FailedCount != 4 || len(result.Results) != 5 {
		t.Fatalf("逐条结果异常: result=%+v err=%v", result, err)
	}
	if result.Results[0].OrderID != "valid" || !result.Results[0].Success {
		t.Fatalf("成功结果异常: %+v", result.Results[0])
	}
	if result.Results[4].OrderID != "unknown" || result.Results[4].Success {
		t.Fatalf("缺少订单号结果异常: %+v", result.Results[4])
	}
}

// TestImportServiceWrapsTransactionErrors 验证订单和商品事务错误保留兼容上下文。
func TestImportServiceWrapsTransactionErrors(t *testing.T) {
	// txErr 保存事务失败原因。
	txErr := errors.New("事务失败")
	// txService 保存事务失败场景的应用服务。
	txService := NewImportService(&importRepositoryFake{ownedIDs: []string{"cookie-1"}, txErr: txErr})
	// result、err 保存事务失败导入结果和错误。
	result, err := txService.Import(context.Background(), 7, []ImportOrder{{OrderID: "order-1", CookieID: "cookie-1"}})
	if err != nil || result.FailedCount != 1 || !strings.Contains(result.Results[0].Message, "订单导入事务失败") {
		t.Fatalf("事务失败结果异常: result=%+v err=%v", result, err)
	}
	// itemRepository 保存商品写入失败场景的内存依赖。
	itemRepository := &importRepositoryFake{ownedIDs: []string{"cookie-1"}, itemErr: errors.New("商品写入失败")}
	// itemService 保存商品写入失败场景的应用服务。
	itemService := NewImportService(itemRepository)
	// result、err 保存商品写入失败的逐条结果和错误。
	result, err = itemService.Import(context.Background(), 7, []ImportOrder{{OrderID: "order-1", CookieID: "cookie-1", ItemID: "item-1"}})
	if err != nil || result.FailedCount != 1 || !strings.Contains(result.Results[0].Message, "补全商品信息失败") {
		t.Fatalf("商品失败场景不应中断批次: %v", err)
	}
}
