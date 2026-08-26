package server

import (
	"context"
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
)

// orderAdapterPortFake 为订单 HTTP 适配器提供可编程的应用层结果。
type orderAdapterPortFake struct {
	// refreshSingleFn 保存单订单刷新测试行为。
	refreshSingleFn func(context.Context, int64, string) (orderapp.SingleRefreshResult, error)
	// refreshFn 保存批量刷新测试行为。
	refreshFn func(context.Context, int64, string, string) (orderapp.RefreshResult, error)
	// listFn 保存订单列表测试行为。
	listFn func(context.Context, orderapp.ListQuery) (orderapp.ListResult, error)
	// getFn 保存订单详情测试行为。
	getFn func(context.Context, int64, string) (*orderapp.Order, error)
	// getViewFn 保存订单详情视图测试行为。
	getViewFn func(context.Context, int64, string) (orderapp.DetailResult, error)
	// deleteFn 保存订单删除测试行为。
	deleteFn func(context.Context, int64, string) error
	// updateFn 保存订单更新测试行为。
	updateFn func(context.Context, int64, string, orderapp.UpdateRequest) error
	// importFn 保存订单导入测试行为。
	importFn func(context.Context, int64, []orderapp.ImportOrder) (orderapp.ImportResult, error)
	// manualShipFn 保存手动发货测试行为。
	manualShipFn func(context.Context, orderapp.ManualShipRequest) (orderapp.ManualShipResult, error)
}

// RefreshSingle 返回测试配置的单订单刷新结果。
func (f orderAdapterPortFake) RefreshSingle(ctx context.Context, userID int64, orderID string) (orderapp.SingleRefreshResult, error) {
	return f.refreshSingleFn(ctx, userID, orderID)
}

// Refresh 返回测试配置的批量刷新结果。
func (f orderAdapterPortFake) Refresh(ctx context.Context, userID int64, cookieID, status string) (orderapp.RefreshResult, error) {
	return f.refreshFn(ctx, userID, cookieID, status)
}

// List 返回测试配置的订单列表结果。
func (f orderAdapterPortFake) List(ctx context.Context, query orderapp.ListQuery) (orderapp.ListResult, error) {
	return f.listFn(ctx, query)
}

// Get 返回测试配置的订单详情结果。
func (f orderAdapterPortFake) Get(ctx context.Context, userID int64, orderID string) (*orderapp.Order, error) {
	return f.getFn(ctx, userID, orderID)
}

// GetView 返回测试配置的订单详情视图结果。
func (f orderAdapterPortFake) GetView(ctx context.Context, userID int64, orderID string) (orderapp.DetailResult, error) {
	return f.getViewFn(ctx, userID, orderID)
}

// Delete 返回测试配置的订单删除结果。
func (f orderAdapterPortFake) Delete(ctx context.Context, userID int64, orderID string) error {
	return f.deleteFn(ctx, userID, orderID)
}

// Update 返回测试配置的订单更新结果。
func (f orderAdapterPortFake) Update(ctx context.Context, userID int64, orderID string, request orderapp.UpdateRequest) error {
	return f.updateFn(ctx, userID, orderID, request)
}

// Import 返回测试配置的订单导入结果。
func (f orderAdapterPortFake) Import(ctx context.Context, userID int64, inputs []orderapp.ImportOrder) (orderapp.ImportResult, error) {
	return f.importFn(ctx, userID, inputs)
}

// ManualShip 返回测试配置的手动发货结果。
func (f orderAdapterPortFake) ManualShip(ctx context.Context, request orderapp.ManualShipRequest) (orderapp.ManualShipResult, error) {
	return f.manualShipFn(ctx, request)
}

// TestOrderHTTPAdapterCoversRefreshAndReadBranches 覆盖订单刷新、列表和详情适配分支。
func TestOrderHTTPAdapterCoversRefreshAndReadBranches(t *testing.T) {
	// adapter 保存当前测试使用的订单 HTTP 适配器。
	adapter := &orderHTTPAdapter{services: orderAdapterPortFake{
		refreshSingleFn: func(context.Context, int64, string) (orderapp.SingleRefreshResult, error) {
			return orderapp.SingleRefreshResult{Success: true, Message: "ok", Detail: orderapp.RefreshDetail{Quantity: "2", SpecName: "颜色", SpecValue: "红", OrderStatus: "2", Amount: "12.00"}}, nil
		},
		refreshFn: func(context.Context, int64, string, string) (orderapp.RefreshResult, error) {
			return orderapp.RefreshResult{PartialFailure: true, Message: "partial", Summary: orderapp.RefreshSummary{Discovered: 1, ListUpdated: 2, SoftDeleted: 3, DetailTotal: 4, Total: 5, Updated: 6, NoChange: 7, Failed: 8}, Results: []orderapp.RefreshOrderResult{{CookieID: "c1", Success: true, Discovered: 1, Updated: 2, SoftDeleted: 1}, {OrderID: "o1", Stage: "detail", Message: "m", Error: "e", OldStatus: "a", NewStatus: "b", Success: false}}}, nil
		},
		listFn: func(context.Context, orderapp.ListQuery) (orderapp.ListResult, error) {
			return orderapp.ListResult{Rows: []orderapp.OrderRow{{OrderID: "o1", ItemID: "i1", ItemTitle: "title", ItemDetail: `{"mainPic":"https://img"}`, BuyerID: "b1", SpecName: "n", SpecValue: "v", Quantity: "1", Amount: "2.00", OrderStatus: "2", CookieID: "c1", IsBargain: 1, SystemShipped: true, ReceiverName: "张三", ReceiverPhone: "138", ReceiverAddr: "上海", ReceiverCity: "上海", CreatedAt: "2024-01-01T00:00:00Z", UpdatedAt: "2024-01-02T00:00:00Z"}}, Total: 1, Page: 1, PageSize: 20, TotalPages: 1}, nil
		},
		getFn: func(context.Context, int64, string) (*orderapp.Order, error) {
			return &orderapp.Order{OrderID: "o1", CookieID: "c1"}, nil
		},
		getViewFn: func(context.Context, int64, string) (orderapp.DetailResult, error) {
			return orderapp.DetailResult{Order: &orderapp.Order{OrderID: "o1", ItemID: "i1", CookieID: "c1", OrderStatus: "4", CreatedAt: "2024-01-01T00:00:00Z"}, Item: &orderapp.ItemInfo{ItemTitle: "title", ItemDetail: `{"mainPic":"https://img"}`}}, nil
		},
	}}
	// single、singleErr 保存单订单刷新适配结果。
	single, singleErr := adapter.RefreshSingle(context.Background(), 1, "o1")
	if singleErr != nil || single.Order.Quantity != "2" || single.Order.OrderStatus != "pending_ship" {
		t.Fatalf("single=%+v err=%v", single, singleErr)
	}
	// refresh、refreshErr 保存批量刷新适配结果。
	refresh, refreshErr := adapter.Refresh(context.Background(), 1, "c1", "")
	if refreshErr != nil || !refresh.PartialFailure || len(refresh.Results) != 2 || refresh.Results[0].SoftDeleted == nil || refresh.Results[1].OldStatus != "a" {
		t.Fatalf("refresh=%+v err=%v", refresh, refreshErr)
	}
	// list、listErr 保存订单列表适配结果。
	list, listErr := adapter.List(context.Background(), orderListQuery{UserID: 1})
	if listErr != nil || list.Total != 1 || len(list.Orders) != 1 || list.Orders[0].Status != "pending_ship" {
		t.Fatalf("list=%+v err=%v", list, listErr)
	}
	// view、viewErr 保存订单详情视图适配结果。
	view, viewErr := adapter.GetView(context.Background(), 1, "o1")
	if viewErr != nil || view.Order.ItemTitle != "title" || view.Order.OrderStatus != "completed" {
		t.Fatalf("view=%+v err=%v", view, viewErr)
	}
	// order、orderErr 保存订单详情适配结果。
	order, orderErr := adapter.Get(context.Background(), 1, "o1")
	if orderErr != nil || order == nil || order.OrderID != "o1" {
		t.Fatalf("order=%+v err=%v", order, orderErr)
	}
}

// TestOrderHTTPAdapterCoversErrorMappingBranches 验证订单适配器对应用错误的兼容映射。
func TestOrderHTTPAdapterCoversErrorMappingBranches(t *testing.T) {
	// sentinel 保存通用错误，用于确认未知错误原样透传。
	sentinel := errors.New("sentinel")
	// cases 保存每个订单适配方法需要验证的错误映射。
	cases := []struct {
		// name 是分支用例名称。
		name string
		// run 执行当前错误映射调用。
		run func(*orderHTTPAdapter) error
		// want 是期望返回的错误。
		want error
		// wantKind 是期望返回的订单错误分类，零值表示不检查分类。
		wantKind orderErrorKind
	}{
		{name: "refresh single not found", run: func(adapter *orderHTTPAdapter) error {
			// err 保存单订单不存在错误。
			_, err := adapter.RefreshSingle(context.Background(), 1, "o")
			return err
		}, want: orderapp.ErrNotFound},
		{name: "refresh single forbidden", run: func(adapter *orderHTTPAdapter) error {
			// err 保存单订单无权访问错误。
			_, err := adapter.RefreshSingle(context.Background(), 1, "o")
			return err
		}, want: orderapp.ErrForbidden},
		{name: "refresh single unsupported", run: func(adapter *orderHTTPAdapter) error {
			// err 保存详情接口不支持错误。
			_, err := adapter.RefreshSingle(context.Background(), 1, "o")
			return err
		}, want: errOrderDetailUnsupported},
		{name: "refresh single credential", run: func(adapter *orderHTTPAdapter) error {
			// err 保存凭证变化错误。
			_, err := adapter.RefreshSingle(context.Background(), 1, "o")
			return err
		}, want: errOrderCredentialChanged},
		{name: "refresh single generic", run: func(adapter *orderHTTPAdapter) error {
			// err 保存单订单刷新通用错误。
			_, err := adapter.RefreshSingle(context.Background(), 1, "o")
			return err
		}, want: sentinel},
		{name: "refresh forbidden", run: func(adapter *orderHTTPAdapter) error {
			// err 保存批量刷新无权访问错误。
			_, err := adapter.Refresh(context.Background(), 1, "", "")
			return err
		}, want: orderapp.ErrForbidden},
		{name: "refresh generic", run: func(adapter *orderHTTPAdapter) error {
			// err 保存批量刷新通用错误。
			_, err := adapter.Refresh(context.Background(), 1, "", "")
			return err
		}, want: sentinel},
		{name: "list forbidden", run: func(adapter *orderHTTPAdapter) error {
			// err 保存订单列表无权访问错误。
			_, err := adapter.List(context.Background(), orderListQuery{})
			return err
		}, want: orderapp.ErrForbidden},
		{name: "list generic", run: func(adapter *orderHTTPAdapter) error {
			// err 保存订单列表通用错误。
			_, err := adapter.List(context.Background(), orderListQuery{})
			return err
		}, want: sentinel},
		{name: "get not found", run: func(adapter *orderHTTPAdapter) error {
			// err 保存订单详情不存在错误。
			_, err := adapter.Get(context.Background(), 1, "")
			return err
		}, want: orderapp.ErrNotFound},
		{name: "get forbidden", run: func(adapter *orderHTTPAdapter) error {
			// err 保存订单详情无权访问错误。
			_, err := adapter.Get(context.Background(), 1, "")
			return err
		}, want: orderapp.ErrForbidden},
		{name: "get generic", run: func(adapter *orderHTTPAdapter) error {
			// err 保存订单详情通用错误。
			_, err := adapter.Get(context.Background(), 1, "")
			return err
		}, want: sentinel},
		{name: "get view not found", run: func(adapter *orderHTTPAdapter) error {
			// err 保存订单详情视图不存在错误。
			_, err := adapter.GetView(context.Background(), 1, "")
			return err
		}, want: orderapp.ErrNotFound},
		{name: "get view forbidden", run: func(adapter *orderHTTPAdapter) error {
			// err 保存订单详情视图无权访问错误。
			_, err := adapter.GetView(context.Background(), 1, "")
			return err
		}, want: orderapp.ErrForbidden},
		{name: "get view generic", run: func(adapter *orderHTTPAdapter) error {
			// err 保存订单详情视图通用错误。
			_, err := adapter.GetView(context.Background(), 1, "")
			return err
		}, want: sentinel},
		{name: "delete forbidden", run: func(adapter *orderHTTPAdapter) error { return adapter.Delete(context.Background(), 1, "") }, want: orderapp.ErrForbidden},
		{name: "delete not found", run: func(adapter *orderHTTPAdapter) error { return adapter.Delete(context.Background(), 1, "") }, want: orderapp.ErrNotFound},
		{name: "delete generic", run: func(adapter *orderHTTPAdapter) error { return adapter.Delete(context.Background(), 1, "") }, want: sentinel},
		{name: "update validation", run: func(adapter *orderHTTPAdapter) error {
			return adapter.Update(context.Background(), 1, "", orderUpdateRequest{})
		}, wantKind: orderErrorBadRequest},
		{name: "update forbidden", run: func(adapter *orderHTTPAdapter) error {
			return adapter.Update(context.Background(), 1, "", orderUpdateRequest{})
		}, want: orderapp.ErrForbidden},
		{name: "update not found", run: func(adapter *orderHTTPAdapter) error {
			return adapter.Update(context.Background(), 1, "", orderUpdateRequest{})
		}, want: orderapp.ErrNotFound},
		{name: "update generic", run: func(adapter *orderHTTPAdapter) error {
			return adapter.Update(context.Background(), 1, "", orderUpdateRequest{})
		}, want: sentinel},
		{name: "import generic", run: func(adapter *orderHTTPAdapter) error {
			// err 保存订单导入通用错误。
			_, err := adapter.Import(context.Background(), 1, nil)
			return err
		}, want: sentinel},
		{name: "manual ship generic", run: func(adapter *orderHTTPAdapter) error {
			// err 保存手动发货通用错误。
			_, err := adapter.ManualShip(context.Background(), manualShipRequest{})
			return err
		}, want: sentinel},
	}
	// index 表示当前错误映射用例在测试表中的位置。
	for index, testCase := range cases {
		// testCase 保存当前错误映射用例。
		t.Run(testCase.name, func(t *testing.T) {
			// adapter 保存返回当前错误的订单适配器。
			adapter := &orderHTTPAdapter{services: orderAdapterPortFake{
				refreshSingleFn: func(context.Context, int64, string) (orderapp.SingleRefreshResult, error) {
					return orderapp.SingleRefreshResult{}, []error{orderapp.ErrNotFound, orderapp.ErrForbidden, orderapp.ErrRefreshDetailUnsupported, orderapp.ErrRefreshCredentialChanged, sentinel}[index]
				},
				refreshFn: func(context.Context, int64, string, string) (orderapp.RefreshResult, error) {
					return orderapp.RefreshResult{}, []error{orderapp.ErrForbidden, sentinel}[index-5]
				},
				listFn: func(context.Context, orderapp.ListQuery) (orderapp.ListResult, error) {
					return orderapp.ListResult{}, []error{orderapp.ErrForbidden, sentinel}[index-7]
				},
				getFn: func(context.Context, int64, string) (*orderapp.Order, error) {
					return nil, []error{orderapp.ErrNotFound, orderapp.ErrForbidden, sentinel}[index-9]
				},
				getViewFn: func(context.Context, int64, string) (orderapp.DetailResult, error) {
					return orderapp.DetailResult{}, []error{orderapp.ErrNotFound, orderapp.ErrForbidden, sentinel}[index-12]
				},
				deleteFn: func(context.Context, int64, string) error {
					return []error{orderapp.ErrForbidden, orderapp.ErrNotFound, sentinel}[index-15]
				},
				updateFn: func(context.Context, int64, string, orderapp.UpdateRequest) error {
					return []error{orderapp.NewValidationError("bad"), orderapp.ErrForbidden, orderapp.ErrNotFound, sentinel}[index-18]
				},
				importFn: func(context.Context, int64, []orderapp.ImportOrder) (orderapp.ImportResult, error) {
					return orderapp.ImportResult{}, sentinel
				},
				manualShipFn: func(context.Context, orderapp.ManualShipRequest) (orderapp.ManualShipResult, error) {
					return orderapp.ManualShipResult{}, sentinel
				},
			}}
			// err 保存当前适配器调用返回的错误。
			err := testCase.run(adapter)
			// kind、kindOK 保存校验错误的业务分类结果。
			kind, kindOK := orderErrorKindOf(err)
			if testCase.wantKind != 0 {
				if !kindOK || kind != testCase.wantKind {
					t.Fatalf("kind=%v ok=%v want=%v err=%v", kind, kindOK, testCase.wantKind, err)
				}
				return
			}
			if !errors.Is(err, testCase.want) {
				t.Fatalf("err=%v want=%v", err, testCase.want)
			}
		})
	}
}

// TestOrderHTTPAdapterCoversMutationAndConverterBranches 覆盖导入、发货、更新和结果转换分支。
func TestOrderHTTPAdapterCoversMutationAndConverterBranches(t *testing.T) {
	// adapter 保存返回成功结果的订单适配器。
	adapter := &orderHTTPAdapter{services: orderAdapterPortFake{
		updateFn: func(context.Context, int64, string, orderapp.UpdateRequest) error { return nil },
		importFn: func(context.Context, int64, []orderapp.ImportOrder) (orderapp.ImportResult, error) {
			return orderapp.ImportResult{Total: 2, SuccessCount: 1, FailedCount: 1, Results: []orderapp.ImportItemResult{{OrderID: "o1", Success: true, Message: "ok"}, {OrderID: "o2", Message: "bad"}}}, nil
		},
		manualShipFn: func(context.Context, orderapp.ManualShipRequest) (orderapp.ManualShipResult, error) {
			return orderapp.ManualShipResult{SuccessCount: 1, FailedCount: 1, Results: []orderapp.ManualShipItemResult{{OrderID: "o1", Status: "succeeded", Success: true, Message: "ok", ReconciliationFieldsPresent: true, ReconciliationID: "r1", ReconciliationWarning: "warn"}, {OrderID: "o2", Status: "failed", Message: "bad"}}}, nil
		},
	}}
	// request 保存需要完整透传的订单更新字段。
	request := orderUpdateRequest{OrderStatus: stringPointer("待发货"), ItemID: stringPointer("i"), BuyerID: stringPointer("b"), SpecName: stringPointer("n"), SpecValue: stringPointer("v"), Quantity: stringPointer("1"), Amount: stringPointer("2"), ReceiverName: stringPointer("张"), ReceiverPhone: stringPointer("1"), ReceiverAddress: stringPointer("a"), ReceiverCity: stringPointer("c"), ChatID: stringPointer("chat"), SystemShipped: boolPointer(true), ItemTitle: stringPointer("title")}
	// updateErr 保存订单更新调用错误。
	if updateErr := adapter.Update(context.Background(), 1, "o", request); updateErr != nil {
		t.Fatal(updateErr)
	}
	// imported、importErr 保存订单导入响应。
	imported, importErr := adapter.Import(context.Background(), 1, []map[string]any{{"order_id": "o1", "item_detail": "d", "status": 1}})
	if importErr != nil || imported.Total != 2 || len(imported.Results) != 2 {
		t.Fatalf("imported=%+v err=%v", imported, importErr)
	}
	// shipped、shipErr 保存手动发货响应。
	shipped, shipErr := adapter.ManualShip(context.Background(), manualShipRequest{UserID: 1, OrderIDs: []string{"o1"}, ShipMode: "status_only"})
	if shipErr != nil || shipped.SuccessCount != 1 || shipped.Results[0].ReconciliationID != "r1" || shipped.Results[1].ReconciliationID != "" {
		t.Fatalf("shipped=%+v err=%v", shipped, shipErr)
	}
	// converted 保存带完整字段的刷新结果转换值。
	converted := refreshResultsFromApplication([]orderapp.RefreshOrderResult{{CookieID: "c", Success: false}, {CookieID: "c", Success: true, Discovered: 1, Updated: 2, SoftDeleted: 1, OrderID: "o", Stage: "persist", Message: "m", Error: "e", OldStatus: "a", NewStatus: "b"}})
	if len(converted) != 2 || converted[0].SoftDeleted != nil || converted[1].SoftDeleted == nil || !*converted[1].SoftDeleted {
		t.Fatalf("converted=%+v", converted)
	}
	// zeroPointer 保存零值指针，确认兼容响应不会丢失显式零值。
	zeroPointer := intPointer(0)
	if zeroPointer == nil || *zeroPointer != 0 || boolPointer(false) == nil || *boolPointer(false) {
		t.Fatal("pointer conversion lost zero value")
	}
	// kind、kindOK 保存订单错误分类读取结果。
	kind, kindOK := orderErrorKindOf(newOrderBadRequest("bad"))
	if !kindOK || kind != orderErrorBadRequest {
		t.Fatalf("kind=%v ok=%v", kind, kindOK)
	}
	// unknownKind、unknownOK 保存未知错误分类读取结果。
	unknownKind, unknownOK := orderErrorKindOf(errors.New("unknown"))
	if unknownOK || unknownKind != 0 {
		t.Fatalf("unknown kind=%v ok=%v", unknownKind, unknownOK)
	}
}

// stringPointer 创建测试所需的字符串指针。
func stringPointer(value string) *string {
	return &value
}
