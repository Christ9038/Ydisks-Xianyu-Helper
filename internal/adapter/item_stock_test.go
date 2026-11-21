package adapter

import (
	"context"
	"errors"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/xianyu/mtop"
)

// stockClientStub 是库存端口测试使用的平台客户端替身，实现 mtop.ItemStockClient。
type stockClientStub struct {
	// mtop.Client 提供未涉及本测试的方法默认实现。
	mtop.Client
	// fetch 保存编辑详情读取的可控行为。
	fetch func(context.Context, string, string) (*mtop.ItemEditDetail, error)
	// edit 保存库存提交的可控行为。
	edit func(context.Context, string, mtop.ItemStockEditRequest) (*mtop.ItemStockEditResult, error)
}

// FetchItemEditDetail 调用测试注入的读取行为。
func (c stockClientStub) FetchItemEditDetail(ctx context.Context, cookies, itemID string) (*mtop.ItemEditDetail, error) {
	return c.fetch(ctx, cookies, itemID)
}

// EditItemStock 调用测试注入的提交行为。
func (c stockClientStub) EditItemStock(ctx context.Context, cookies string, request mtop.ItemStockEditRequest) (*mtop.ItemStockEditResult, error) {
	return c.edit(ctx, cookies, request)
}

// TestItemPublishPortGetStockConvertsDetailAndPersistsCookie 验证读取库存的转换与响应 Cookie 写回。
func TestItemPublishPortGetStockConvertsDetailAndPersistsCookie(t *testing.T) {
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 返回含两行规格的详情和更新后的 Cookie。
	client := stockClientStub{fetch: func(_ context.Context, cookies, itemID string) (*mtop.ItemEditDetail, error) {
		if cookies == "" || itemID != "item-1" {
			t.Fatalf("读取参数异常: cookies=%q itemID=%q", cookies, itemID)
		}
		return &mtop.ItemEditDetail{ItemID: "item-1", MultiSpec: true, Quantity: 9, UpdatedCookies: "unb=2; _m_h5_tk=tk2; havana_lgc2_77=lgc;",
			SKUs: []mtop.ItemStockSKU{{Key: "sku:a", Label: "红", PriceCents: 100, Quantity: 4}, {Key: "sku:b", Label: "蓝", PriceCents: 200, Quantity: 5}}}, nil
	}}
	// port 是绑定测试数据库和平台替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// snapshot、err 是读取结果。
	snapshot, err := port.GetStock(context.Background(), 1, "cid", "item-1")
	if err != nil || !snapshot.MultiSpec || snapshot.Quantity != 9 || len(snapshot.SKUs) != 2 || snapshot.SKUs[1].Label != "蓝" || snapshot.SKUs[1].PriceCents != 200 {
		t.Fatalf("snapshot=%+v err=%v", snapshot, err)
	}
	// value、valueErr 是写回后的 Cookie。
	value, valueErr := store.Cookies.GetValue(context.Background(), "cid")
	if valueErr != nil || value != "unb=2; _m_h5_tk=tk2; havana_lgc2_77=lgc;" {
		t.Fatalf("Cookie 未写回: %q %v", value, valueErr)
	}
}

// TestItemPublishPortStockRejectsWrongOwnerAndUnsupportedClient 验证归属错误和客户端能力缺失不会触达平台。
func TestItemPublishPortStockRejectsWrongOwnerAndUnsupportedClient(t *testing.T) {
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// called 记录平台是否被调用。
	called := false
	// client 在被调用时记录，用于证明没有触达平台。
	client := stockClientStub{fetch: func(context.Context, string, string) (*mtop.ItemEditDetail, error) {
		called = true
		return nil, errors.New("不应调用")
	}}
	// port 是绑定平台替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := port.GetStock(context.Background(), 99, "cid", "item-1"); !errors.Is(err, itemapp.ErrStockCredentialChanged) || called {
		t.Fatalf("越权 err=%v called=%v", err, called)
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := port.UpdateStock(context.Background(), 1, "missing", "item-1", itemapp.StockUpdate{Quantity: 1}); !errors.Is(err, itemapp.ErrStockCredentialChanged) || called {
		t.Fatalf("账号不存在 err=%v called=%v", err, called)
	}
	// unsupported 是不具备库存能力的客户端。
	unsupported := NewItemPublishPort(store, func() mtop.Client { return unsupportedItemPublishClient{} }, nil, nil, nil)
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := unsupported.GetStock(context.Background(), 1, "cid", "item-1"); !errors.Is(err, itemapp.ErrStockUnsupported) {
		t.Fatalf("能力缺失 err=%v", err)
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := (*ItemPublishPort)(nil).GetStock(context.Background(), 1, "cid", "item-1"); err == nil {
		t.Fatal("nil 端口应返回错误")
	}
}

// TestItemPublishPortGetStockKeepsDataWhenSessionPersistenceFails 验证读取成功但凭证在调用期间变化时仍返回库存。
func TestItemPublishPortGetStockKeepsDataWhenSessionPersistenceFails(t *testing.T) {
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 在读取期间改写账号 Cookie，模拟并发凭证更新。
	client := stockClientStub{fetch: func(ctx context.Context, _, itemID string) (*mtop.ItemEditDetail, error) {
		// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
		if err := store.Cookies.UpdateValueOwned(ctx, "cid", "unb=concurrent", 1); err != nil {
			t.Fatal(err)
		}
		return &mtop.ItemEditDetail{ItemID: itemID, Quantity: 3, UpdatedCookies: "unb=late"}, nil
	}}
	// port 是绑定平台替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// snapshot、err 是读取结果。
	snapshot, err := port.GetStock(context.Background(), 1, "cid", "item-1")
	if err != nil || snapshot.Quantity != 3 {
		t.Fatalf("读取成功时不应因写回失败而失败: %+v %v", snapshot, err)
	}
	// value 是当前被检查或转换的字段值。
	if value, _ := store.Cookies.GetValue(context.Background(), "cid"); value != "unb=concurrent" {
		t.Fatalf("并发更新的凭证不应被覆盖: %q", value)
	}
}

// TestItemPublishPortUpdateStockSuccessAndRequestConversion 验证修改库存的请求转换、结果和 Cookie 写回。
func TestItemPublishPortUpdateStockSuccessAndRequestConversion(t *testing.T) {
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// got 保存平台替身收到的请求。
	var got mtop.ItemStockEditRequest
	// updatedRuntime 保存同步到运行时账号的 Cookie。
	updatedRuntime := ""
	// client 记录请求并返回更新后的 Cookie。
	client := stockClientStub{edit: func(_ context.Context, _ string, request mtop.ItemStockEditRequest) (*mtop.ItemStockEditResult, error) {
		got = request
		return &mtop.ItemStockEditResult{ItemID: request.ItemID, Message: "SUCCESS::调用成功", UpdatedCookies: "unb=2; _m_h5_tk=tk2; havana_lgc2_77=lgc;"}, nil
	}}
	// port 是绑定平台替身的商品发布端口，并记录运行时 Cookie 同步。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, func(_ context.Context, _ string, cookie string) { updatedRuntime = cookie }, nil)
	// outcome、err 是修改结果。
	outcome, err := port.UpdateStock(context.Background(), 1, "cid", "item-1", itemapp.StockUpdate{SKUs: []itemapp.StockSKUChange{{Key: "sku:a", Quantity: 6, ExpectedQuantity: 3}}})
	if err != nil || outcome.ItemID != "item-1" || !outcome.CookiePersisted || outcome.Message == "" {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
	if got.ItemID != "item-1" || len(got.SKUs) != 1 || got.SKUs[0].Key != "sku:a" || got.SKUs[0].Quantity != 6 || got.SKUs[0].ExpectedQuantity != 3 {
		t.Fatalf("请求转换错误: %+v", got)
	}
	if updatedRuntime != "unb=2; _m_h5_tk=tk2; havana_lgc2_77=lgc;" {
		t.Fatalf("运行时 Cookie 未同步: %q", updatedRuntime)
	}
}

// TestItemPublishPortUpdateStockReportsSessionPersistenceFailure 验证平台已修改但会话写回失败时返回结果并标记。
func TestItemPublishPortUpdateStockReportsSessionPersistenceFailure(t *testing.T) {
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 在提交期间改写账号 Cookie，模拟并发凭证更新。
	client := stockClientStub{edit: func(ctx context.Context, _ string, request mtop.ItemStockEditRequest) (*mtop.ItemStockEditResult, error) {
		// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
		if err := store.Cookies.UpdateValueOwned(ctx, "cid", "unb=concurrent", 1); err != nil {
			t.Fatal(err)
		}
		return &mtop.ItemStockEditResult{ItemID: request.ItemID, UpdatedCookies: "unb=late"}, nil
	}}
	// port 是绑定平台替身的商品发布端口。
	port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, nil)
	// outcome、err 是修改结果。
	outcome, err := port.UpdateStock(context.Background(), 1, "cid", "item-1", itemapp.StockUpdate{Quantity: 2, ExpectedQuantity: 1})
	if !errors.Is(err, itemapp.ErrStockSessionPersistence) || outcome.CookiePersisted || outcome.ItemID != "item-1" {
		t.Fatalf("outcome=%+v err=%v", outcome, err)
	}
}

// TestItemPublishPortUpdateStockMapsPlatformErrors 验证平台错误映射为应用层哨兵错误，会话过期触发恢复，空结果被拒绝。
func TestItemPublishPortUpdateStockMapsPlatformErrors(t *testing.T) {
	// store、cleanup 是带隔离 SQLite 的账号存储及清理操作。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// recovered 记录会话恢复回调的触发次数。
	recovered := 0
	// run 用指定错误构造一次修改调用。
	run := func(editErr error, result *mtop.ItemStockEditResult) error {
		// client 返回预设错误或结果。
		client := stockClientStub{edit: func(context.Context, string, mtop.ItemStockEditRequest) (*mtop.ItemStockEditResult, error) {
			return result, editErr
		}}
		// port 记录会话恢复回调。
		port := NewItemPublishPort(store, func() mtop.Client { return client }, nil, nil, func(context.Context, string, error) bool {
			recovered++
			return true
		})
		// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
		_, err := port.UpdateStock(context.Background(), 1, "cid", "item-1", itemapp.StockUpdate{Quantity: 2, ExpectedQuantity: 1})
		return err
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := run(mtop.ErrItemStockChanged, nil); !errors.Is(err, itemapp.ErrStockChanged) {
		t.Fatalf("并发变化 err=%v", err)
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := run(mtop.ErrItemEditUnsupported, nil); !errors.Is(err, itemapp.ErrStockEditUnsupported) {
		t.Fatalf("类目不支持 err=%v", err)
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := run(errors.Join(mtop.ErrItemStockInvalid, errors.New("细节")), nil); !errors.Is(err, itemapp.ErrStockInvalidInput) {
		t.Fatalf("输入非法 err=%v", err)
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := run(&mtop.MTopResponseError{Kind: mtop.MTopErrorSessionExpired, API: "x"}, nil); !errors.Is(err, itemapp.ErrStockSessionExpired) || recovered != 1 {
		t.Fatalf("会话过期 err=%v recovered=%d", err, recovered)
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := run(&mtop.MTopResponseError{Kind: mtop.MTopErrorRiskVerification, API: "x"}, nil); !errors.Is(err, itemapp.ErrStockRiskVerification) || recovered != 1 {
		t.Fatalf("风控应映射为专用错误且不触发恢复: err=%v recovered=%d", err, recovered)
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := run(errors.New("网络错误"), nil); err == nil || errors.Is(err, itemapp.ErrStockRiskVerification) {
		t.Fatalf("未知错误应原样返回: %v", err)
	}
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := run(nil, nil); err == nil {
		t.Fatal("平台未返回结果应报错")
	}
	// emptyDetail 覆盖读取返回空详情的情况。
	emptyDetail := NewItemPublishPort(store, func() mtop.Client {
		return stockClientStub{fetch: func(context.Context, string, string) (*mtop.ItemEditDetail, error) { return nil, nil }}
	}, nil, nil, nil)
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if _, err := emptyDetail.GetStock(context.Background(), 1, "cid", "item-1"); err == nil {
		t.Fatal("平台未返回详情应报错")
	}
}
