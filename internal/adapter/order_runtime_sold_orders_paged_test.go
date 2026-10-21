package adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/xianyu/mtop"
)

// newPagedSoldRuntime 构造由 pageCount 页脚本驱动的订单运行时，末页 NextPage 为假；不发起任何真实平台请求。
func newPagedSoldRuntime(pageCount int) *OrderRuntime {
	// fake 的 fetch 回调每页返回一条订单并在末页结束分页。
	fake := &soldRuntimePageFake{orderRuntimeMTopFake: &orderRuntimeMTopFake{}, fetch: func(_ context.Context, _ string, pageNumber, _ int) (*mtop.SoldOrdersPage, error) {
		return &mtop.SoldOrdersPage{NextPage: pageNumber < pageCount, Items: []mtop.SoldOrder{{OrderID: fmt.Sprintf("order-%d", pageNumber), BuyerID: "buyer", ItemID: "item"}}}, nil
	}}
	return NewOrderRuntime(nil, OrderRuntimeHooks{Client: func() mtop.Client { return fake }}, nil, nil)
}

// TestOrderRuntimePagedFetchDeliversEachPageWithoutAccumulating 验证逐页交付、不累积订单，且身份完整时每页都声明卖家。
func TestOrderRuntimePagedFetchDeliversEachPageWithoutAccumulating(t *testing.T) {
	// pages 保存 handler 收到的页，用于核对顺序、内容和卖家身份。
	var pages []orderapp.RefreshSoldPage
	// result、err 保存分页读取的最终状态。
	result, err := newPagedSoldRuntime(3).FetchSoldOrdersPaged(context.Background(), &orderapp.PlatformRuntimeData{Value: "unb=101; sid=fixture"}, func(_ context.Context, page orderapp.RefreshSoldPage) error {
		pages = append(pages, page)
		return nil
	})
	if err != nil || len(result.Orders) != 0 || result.SellerID != "101" || len(pages) != 3 {
		t.Fatalf("逐页读取结果异常: pages=%d orders=%d seller=%q err=%v", len(pages), len(result.Orders), result.SellerID, err)
	}
	// index、page 是第 index+1 页及其内容。
	for index, page := range pages {
		if len(page.Orders) != 1 || page.Orders[0].OrderID != fmt.Sprintf("order-%d", index+1) || page.SellerID != "101" {
			t.Fatalf("第 %d 页内容或身份异常: %+v", index+1, page)
		}
	}
}

// TestOrderRuntimePagedFetchWithoutIdentityNeverDeclaresSeller 验证请求缺失 unb 时任何页都不声明卖家。
func TestOrderRuntimePagedFetchWithoutIdentityNeverDeclaresSeller(t *testing.T) {
	// _, err 只关心每页声明的卖家身份，handler 内断言。
	_, err := newPagedSoldRuntime(2).FetchSoldOrdersPaged(context.Background(), &orderapp.PlatformRuntimeData{Value: "sid=fixture"}, func(_ context.Context, page orderapp.RefreshSoldPage) error {
		if page.SellerID != "" {
			t.Fatalf("缺失身份时不得声明卖家: %q", page.SellerID)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("读取不应失败: %v", err)
	}
}

// TestOrderRuntimePagedFetchStopsOnHandlerError 验证 handler 报错立即终止后续分页并原样传播。
func TestOrderRuntimePagedFetchStopsOnHandlerError(t *testing.T) {
	// stopErr 是 handler 返回的错误身份。
	stopErr := errors.New("提交第二页失败")
	// delivered 统计已交付页数。
	delivered := 0
	// err 是 handler 在第二页返回的错误，应被原样传播。
	_, err := newPagedSoldRuntime(5).FetchSoldOrdersPaged(context.Background(), &orderapp.PlatformRuntimeData{Value: "unb=101"}, func(context.Context, orderapp.RefreshSoldPage) error {
		delivered++
		if delivered == 2 {
			return stopErr
		}
		return nil
	})
	if !errors.Is(err, stopErr) || delivered != 2 {
		t.Fatalf("handler 错误应终止分页: delivered=%d err=%v", delivered, err)
	}
}

// TestOrderRuntimePagedFetchRejectsNilHandlerAndKeepsFailedPageOut 验证空 handler 被拒绝，且结构不合法的页不会交付。
func TestOrderRuntimePagedFetchRejectsNilHandlerAndKeepsFailedPageOut(t *testing.T) {
	// err 是空 handler 的拒绝错误，必须非空。
	if _, err := newPagedSoldRuntime(1).FetchSoldOrdersPaged(context.Background(), &orderapp.PlatformRuntimeData{Value: "unb=101"}, nil); err == nil {
		t.Fatal("空 handler 必须被拒绝")
	}
	// fake 第二页包含缺失订单号的条目，该页不得交付。
	fake := &soldRuntimePageFake{orderRuntimeMTopFake: &orderRuntimeMTopFake{}, fetch: func(_ context.Context, _ string, pageNumber, _ int) (*mtop.SoldOrdersPage, error) {
		if pageNumber == 2 {
			return &mtop.SoldOrdersPage{Items: []mtop.SoldOrder{{OrderID: "ok"}, {OrderID: " "}}}, nil
		}
		return &mtop.SoldOrdersPage{NextPage: true, Items: []mtop.SoldOrder{{OrderID: "first"}}}, nil
	}}
	// runtime 是使用第二页含非法订单号脚本的订单运行时。
	runtime := NewOrderRuntime(nil, OrderRuntimeHooks{Client: func() mtop.Client { return fake }}, nil, nil)
	// delivered 统计已交付页数。
	delivered := 0
	// err 是非法页导致的分页完整性错误，且该页不得交付 handler。
	_, err := runtime.FetchSoldOrdersPaged(context.Background(), &orderapp.PlatformRuntimeData{Value: "unb=101"}, func(context.Context, orderapp.RefreshSoldPage) error {
		delivered++
		return nil
	})
	if err == nil || !strings.Contains(err.Error(), "订单号") || delivered != 1 {
		t.Fatalf("不合法页不得交付: delivered=%d err=%v", delivered, err)
	}
}

// TestOrderRuntimeAdapterForwardsPagedFetch 验证适配器转发分页读取，运行时缺失时返回明确错误。
func TestOrderRuntimeAdapterForwardsPagedFetch(t *testing.T) {
	// adapter 包装带两页脚本的运行时。
	adapter := OrderRuntimeAdapter{runtime: newPagedSoldRuntime(2)}
	// delivered 统计转发后交付的页数。
	delivered := 0
	// err 是适配器转发分页读取的结果，应为空。
	if _, err := adapter.FetchSoldOrdersPaged(context.Background(), &orderapp.PlatformRuntimeData{Value: "unb=101"}, func(context.Context, orderapp.RefreshSoldPage) error {
		delivered++
		return nil
	}); err != nil || delivered != 2 {
		t.Fatalf("适配器应转发分页读取: delivered=%d err=%v", delivered, err)
	}
	// err 是运行时未初始化时的明确错误，必须非空。
	if _, err := (OrderRuntimeAdapter{}).FetchSoldOrdersPaged(context.Background(), nil, nil); err == nil {
		t.Fatal("运行时未初始化必须返回错误")
	}
}
