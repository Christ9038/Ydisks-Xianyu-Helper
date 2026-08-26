package adapter

import (
	"context"
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/xianyu/mtop"
)

// orderRuntimeFetchFake 提供订单详情和已售订单分页的本地平台替身。
type orderRuntimeFetchFake struct {
	// orderRuntimeMTopFake 提供测试不关注的基础 MTOP 能力占位。
	*orderRuntimeMTopFake
	// detail、detailErr 保存订单详情测试结果及错误。
	detail    *mtop.OrderDetailResult
	detailErr error
	// soldPages、soldErr 保存已售订单分页结果及可注入错误。
	soldPages map[int]*mtop.SoldOrdersPage
	soldErr   error
}

// FetchOrderDetail 返回预置的订单详情结果。
func (f *orderRuntimeFetchFake) FetchOrderDetail(context.Context, string, string) (*mtop.OrderDetailResult, error) {
	return f.detail, f.detailErr
}

// FetchSoldOrdersPage 返回预置的指定页已售订单结果。
func (f *orderRuntimeFetchFake) FetchSoldOrdersPage(_ context.Context, _ string, pageNumber, _ int) (*mtop.SoldOrdersPage, error) {
	if f.soldErr != nil {
		return nil, f.soldErr
	}
	return f.soldPages[pageNumber], nil
}

// TestOrderRuntimeFetchesDetailAndSoldOrders 验证详情映射、分页聚合、状态归一化和平台错误传播。
func TestOrderRuntimeFetchesDetailAndSoldOrders(t *testing.T) {
	// ctx 是本测试订单刷新调用共用的非取消上下文。
	ctx := context.Background()
	// detailFake 保存成功返回详情和两页已售订单的平台替身。
	detailFake := &orderRuntimeFetchFake{
		orderRuntimeMTopFake: &orderRuntimeMTopFake{},
		detail:               &mtop.OrderDetailResult{Quantity: "2", SpecName: "颜色", SpecValue: "蓝", OrderStatus: "待发货", Amount: "19.90", UpdatedCookies: "sid=detail"},
		soldPages: map[int]*mtop.SoldOrdersPage{
			1: {NextPage: true, Items: []mtop.SoldOrder{{OrderID: "o1", ItemID: "i1", BuyerID: "b1", OrderStatus: "2", Quantity: "2", Amount: "19.90", ReceiverName: "买家"}}},
			2: {NextPage: false, Items: []mtop.SoldOrder{{OrderID: "o2", ItemID: "i2", BuyerID: "b2", OrderStatus: "4", Quantity: "1", Amount: "9.90"}}},
		},
	}
	// runtime 保存绑定详情和已售订单能力的订单运行时。
	runtime := NewOrderRuntime(nil, OrderRuntimeHooks{Client: func() mtop.Client { return detailFake }}, nil, nil)
	if !runtime.DetailAvailable() || !runtime.SoldAvailable() {
		t.Fatal("详情和已售订单能力应被识别为可用")
	}
	// detailResult、detailErr 保存详情应用模型和错误。
	detailResult, detailErr := runtime.FetchOrderDetail(ctx, &orderapp.PlatformRuntimeData{Value: "sid=old"}, "o1")
	if detailErr != nil || detailResult.Detail == nil || detailResult.Detail.Quantity != "2" || detailResult.Detail.SpecValue != "蓝" {
		t.Fatalf("详情映射异常 result=%+v err=%v", detailResult, detailErr)
	}
	// soldResult、soldErr 保存跨页聚合后的订单列表。
	soldResult, soldErr := runtime.FetchSoldOrders(ctx, &orderapp.PlatformRuntimeData{Value: "sid=old"})
	if soldErr != nil || len(soldResult.Orders) != 2 || soldResult.Orders[0].OrderStatus != "pending_ship" || soldResult.Orders[1].OrderStatus != "completed" {
		t.Fatalf("已售订单聚合异常 result=%+v err=%v", soldResult, soldErr)
	}
	// detailFake.detailErr 保存详情平台错误。
	detailFake.detailErr = errors.New("detail failed")
	// failedDetail、failedDetailErr 保存详情错误传播结果。
	failedDetail, failedDetailErr := runtime.FetchOrderDetail(ctx, &orderapp.PlatformRuntimeData{Value: "sid=old"}, "o1")
	if failedDetail.Detail != nil || !errors.Is(failedDetailErr, detailFake.detailErr) {
		t.Fatalf("详情错误传播异常 result=%+v err=%v", failedDetail, failedDetailErr)
	}
	// detailFake.detail、detailFake.detailErr 保存空详情响应场景。
	detailFake.detail = nil
	detailFake.detailErr = nil
	// emptyDetail、emptyDetailErr 保存平台空详情结果的错误。
	emptyDetail, emptyDetailErr := runtime.FetchOrderDetail(ctx, &orderapp.PlatformRuntimeData{Value: "sid=old"}, "o1")
	if emptyDetail.Detail != nil || emptyDetailErr == nil {
		t.Fatalf("空详情结果未报错 result=%+v err=%v", emptyDetail, emptyDetailErr)
	}
	// soldErrValue 保存已售订单分页失败时的底层错误。
	soldErrValue := errors.New("sold orders failed")
	detailFake.soldErr = soldErrValue
	// partialSold、partialSoldErr 保存分页失败时已累积订单和错误。
	partialSold, partialSoldErr := runtime.FetchSoldOrders(ctx, &orderapp.PlatformRuntimeData{Value: "sid=old"})
	if len(partialSold.Orders) != 0 || !errors.Is(partialSoldErr, soldErrValue) {
		t.Fatalf("已售订单分页错误异常 result=%+v err=%v", partialSold, partialSoldErr)
	}
}
