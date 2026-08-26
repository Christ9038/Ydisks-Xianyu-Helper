package orders

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// updateRepositoryFake 是订单更新应用服务测试使用的内存 repository。
type updateRepositoryFake struct {
	// order 保存待更新订单。
	order *Order
	// owned 表示订单账号是否属于当前用户。
	owned bool
	// getErr 保存订单读取错误。
	getErr error
	// ownedErr 保存账号归属查询错误。
	ownedErr error
	// txErr 保存事务执行错误。
	txErr error
	// patchErr 保存订单补丁写入错误。
	patchErr error
	// itemErr 保存商品标题写入错误。
	itemErr error
	// writer 保存事务回调收到的内存写入器。
	writer *updateWriterFake
}

// GetOrder 返回测试订单。
func (f *updateRepositoryFake) GetOrder(context.Context, string) (*Order, error) {
	return f.order, f.getErr
}

// ExistsOwned 返回测试账号归属结果。
func (f *updateRepositoryFake) ExistsOwned(context.Context, int64, string) (bool, error) {
	return f.owned, f.ownedErr
}

// WithTransaction 执行测试事务回调并记录写入内容。
func (f *updateRepositoryFake) WithTransaction(ctx context.Context, work func(Writer) error) error {
	if f.txErr != nil {
		return f.txErr
	}
	// writer 保存当前测试事务的写入器。
	f.writer = &updateWriterFake{patchErr: f.patchErr, itemErr: f.itemErr}
	return work(f.writer)
}

// updateWriterFake 是订单更新应用服务测试使用的事务写入器。
type updateWriterFake struct {
	// orderID 保存被更新的订单标识。
	orderID string
	// patch 保存收到的订单补丁。
	patch OrderPatch
	// item 保存收到的商品写入。
	item ItemWrite
	// patchErr 保存订单补丁错误。
	patchErr error
	// itemErr 保存商品写入错误。
	itemErr error
}

// PatchOrder 记录测试订单补丁。
func (f *updateWriterFake) PatchOrder(_ context.Context, orderID string, patch OrderPatch) error {
	f.orderID = orderID
	f.patch = patch
	return f.patchErr
}

// UpsertItemBasic 记录测试商品写入。
func (f *updateWriterFake) UpsertItemBasic(_ context.Context, item ItemWrite) error {
	f.item = item
	return f.itemErr
}

// UpsertOrder 满足订单事务写入器接口的测试兼容方法。
func (f *updateWriterFake) UpsertOrder(context.Context, string, UpsertOptions) error {
	return nil
}

// TestUpdateServiceNormalizesAndWrites 验证订单更新会规范字段并写入同一事务。
func TestUpdateServiceNormalizesAndWrites(t *testing.T) {
	// repository 保存本用例使用的内存依赖。
	repository := &updateRepositoryFake{order: &Order{OrderID: "order-1", CookieID: "cookie-1", ItemID: "item-1"}, owned: true}
	// service 保存待测试的订单更新服务。
	service := NewUpdateService(repository)
	// status 保存待更新的订单状态。
	status := " 2 "
	// amount 保存待更新的订单金额。
	amount := "¥1,234.50"
	// itemID 保存待更新的商品标识。
	itemID := " item-2 "
	// itemTitle 保存待更新的商品标题。
	itemTitle := " 新标题 "
	// err 保存订单更新结果。
	if err := service.Update(context.Background(), 7, "order-1", UpdateRequest{
		OrderStatus: &status, Amount: &amount, ItemID: &itemID, ItemTitle: &itemTitle,
	}); err != nil {
		t.Fatalf("更新订单失败: %v", err)
	}
	if repository.writer == nil || repository.writer.patch.OrderStatus == nil || *repository.writer.patch.OrderStatus != "pending_ship" {
		t.Fatalf("订单状态未规范化: %+v", repository.writer)
	}
	if repository.writer.patch.Amount == nil || *repository.writer.patch.Amount != "1234.50" {
		t.Fatalf("订单金额未规范化: %+v", repository.writer.patch)
	}
	if repository.writer.patch.ItemID == nil || *repository.writer.patch.ItemID != "item-2" {
		t.Fatalf("商品标识未规范化: %+v", repository.writer.patch)
	}
	if repository.writer.item.ItemID != "item-2" || repository.writer.item.ItemTitle != "新标题" {
		t.Fatalf("商品标题写入异常: %+v", repository.writer.item)
	}
}

// TestUpdateServiceRejectsInvalidFields 验证订单状态、金额和商品标题校验。
func TestUpdateServiceRejectsInvalidFields(t *testing.T) {
	// cases 保存非法字段测试表。
	cases := []struct {
		// name 是测试场景名称。
		name string
		// request 是待校验的更新请求。
		request UpdateRequest
	}{
		{name: "invalid status", request: UpdateRequest{OrderStatus: stringPointer("unknown-status")}},
		{name: "invalid amount", request: UpdateRequest{Amount: stringPointer("12x")}},
		{name: "empty item title", request: UpdateRequest{ItemTitle: stringPointer(" ")}},
	}
	// testCase 表示当前字段校验场景。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// service 保存当前场景的订单更新服务。
			service := NewUpdateService(&updateRepositoryFake{order: &Order{CookieID: "cookie-1", ItemID: "item-1"}, owned: true})
			// err 保存字段校验结果。
			err := service.Update(context.Background(), 7, "order-1", testCase.request)
			// validationErr 保存可识别的字段校验错误。
			var validationErr *ValidationError
			if !errors.As(err, &validationErr) {
				t.Fatalf("错误类型不正确: %v", err)
			}
		})
	}
}

// TestUpdateServiceOwnershipAndReadErrors 验证订单不存在、无权和存储错误边界。
func TestUpdateServiceOwnershipAndReadErrors(t *testing.T) {
	// notFoundService 保存订单不存在场景的应用服务。
	notFoundService := NewUpdateService(&updateRepositoryFake{})
	if !errors.Is(notFoundService.Update(context.Background(), 1, "missing", UpdateRequest{}), ErrNotFound) {
		t.Fatal("订单不存在时未返回 ErrNotFound")
	}
	// forbiddenService 保存账号无权场景的应用服务。
	forbiddenService := NewUpdateService(&updateRepositoryFake{order: &Order{CookieID: "cookie-1"}})
	if !errors.Is(forbiddenService.Update(context.Background(), 1, "order-1", UpdateRequest{}), ErrForbidden) {
		t.Fatal("账号无权时未返回 ErrForbidden")
	}
	// expectedErr 保存底层存储错误。
	expectedErr := errors.New("读取失败")
	// failedService 保存存储错误场景的应用服务。
	failedService := NewUpdateService(&updateRepositoryFake{getErr: expectedErr})
	if !errors.Is(failedService.Update(context.Background(), 1, "order-1", UpdateRequest{}), expectedErr) {
		t.Fatal("存储错误未透传")
	}
}

// TestOrderNormalizationHelpersCoversStatusAndAmountBoundaries 验证订单状态码和金额文本的完整归一化边界。
func TestOrderNormalizationHelpersCoversStatusAndAmountBoundaries(t *testing.T) {
	// statuses 保存平台状态码到应用状态的映射样例。
	statuses := map[string]string{"paid": "pending_ship", "1": "processing", "2": "pending_ship", "3": "shipped", "4": "completed", "11": "completed", "5": "refunding", "7": "refunding", "9": "refunding", "6": "cancelled", "8": "cancelled", "10": "cancelled", "12": "cancelled", "": "unknown", "custom": "custom"}
	// status、want 表示当前状态码及期望的应用状态。
	for status, want := range statuses {
		// got 保存当前平台状态码归一化后的应用状态。
		if got := NormalizeOrderStatus(status); got != want {
			t.Fatalf("status=%q got=%q want=%q", status, got, want)
		}
	}
	// amountCases 保存金额文本及其规范化结果。
	amountCases := []struct {
		// raw 是待规范化的金额文本。
		raw string
		// want 是规范化后的金额文本。
		want string
		// valid 表示金额是否应通过校验。
		valid bool
	}{
		{raw: " ¥1,234.50 ", want: "1234.50", valid: true},
		{raw: "￥2", want: "2", valid: true},
		{raw: "", want: "", valid: true},
		{raw: "1,23", valid: false},
		{raw: "1.234", want: "1.234", valid: true},
		{raw: "abc", valid: false},
		{raw: strings.Repeat("9", 400), valid: false},
	}
	// testCase 表示当前金额归一化样例。
	for _, testCase := range amountCases {
		// got、valid 保存金额归一化文本和校验结果。
		got, valid := NormalizeOrderAmount(testCase.raw)
		if got != testCase.want || valid != testCase.valid {
			t.Fatalf("amount=%q got=%q valid=%v want=%q/%v", testCase.raw, got, valid, testCase.want, testCase.valid)
		}
	}
	// validationErr 是 nil 请求返回的字段校验错误。
	validationErr := normalizeUpdateRequest(nil)
	if validationErr == nil {
		t.Fatal("nil update request should fail")
	}
}

// stringPointer 返回测试使用的字符串指针。
func stringPointer(value string) *string {
	return &value
}
