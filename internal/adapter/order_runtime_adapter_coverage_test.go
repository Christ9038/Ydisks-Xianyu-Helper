package adapter

import (
	"context"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/xianyu/mtop"
)

// TestOrderRuntimeAdapterNilRuntimePaths 验证订单运行时适配器在运行时未装配时的安全返回值。
func TestOrderRuntimeAdapterNilRuntimePaths(t *testing.T) {
	// ctx 是本测试使用的非取消上下文。
	ctx := context.Background()
	// adapter 是未装配订单运行时的适配器。
	adapter := OrderRuntimeAdapter{}
	if adapter.AccountRunning("cid") || adapter.AutomationReady() || adapter.MTopAvailable() || adapter.DetailAvailable() || adapter.SoldAvailable() || adapter.IsSessionExpired(nil) {
		t.Fatal("nil runtime must report unavailable capabilities")
	}
	// _, deliveryErr 保存未装配运行时的手动发货错误。
	if _, deliveryErr := adapter.ManualFullDelivery(ctx, nil); deliveryErr == nil {
		t.Fatal("nil runtime manual delivery should fail")
	}
	// consignResult 保存未装配运行时的确认发货结果。
	consignResult := adapter.ConfirmShipment(ctx, "cid", "order", 1)
	if consignResult.Err == nil {
		t.Fatal("nil runtime consign should fail")
	}
	// adjustResult 保存未装配运行时的改价结果。
	if adjustResult := adapter.AdjustPrice(ctx, "cid", "order", 1, 100); adjustResult.Err == nil {
		t.Fatal("nil runtime adjust price should fail")
	}
	// _, reconciliationErr 保存未装配运行时的补偿记录错误。
	if _, reconciliationErr := adapter.RecordOrderReconciliation(ctx, "order", "cid", "kind", "message"); reconciliationErr == nil {
		t.Fatal("nil runtime reconciliation should fail")
	}
	// _, aliasReconciliationErr 保存兼容补偿记录入口的错误。
	if _, aliasReconciliationErr := adapter.RecordReconciliation(ctx, "order", "cid", "kind", "message"); aliasReconciliationErr == nil {
		t.Fatal("nil runtime reconciliation alias should fail")
	}
	// recoveryResult 保存未装配运行时的会话恢复结果。
	if adapter.RecoverExpiredSession(ctx, "cid", context.Canceled) {
		t.Fatal("nil runtime recovery should report false")
	}
	// _, detailErr 保存未装配运行时的订单详情读取错误。
	if _, detailErr := adapter.FetchOrderDetail(ctx, nil, "order"); detailErr == nil {
		t.Fatal("nil runtime detail fetch should fail")
	}
	// _, soldErr 保存未装配运行时的已售订单读取错误。
	if _, soldErr := adapter.FetchSoldOrders(ctx, nil); soldErr == nil {
		t.Fatal("nil runtime sold fetch should fail")
	}
	// _, _, _, sessionErr 保存无详情凭证时的 Cookie 会话持久化错误。
	if _, _, _, sessionErr := adapter.PersistCookieSession(ctx, nil, orderapp.RefreshCookieUpdate{Value: "sid"}); sessionErr == nil {
		t.Fatal("nil runtime session persistence should fail")
	}
	// detail 保存有详情凭证时的应用模型。
	detail := &orderapp.PlatformRuntimeData{Value: "old"}
	// value、changed、handled、detailSessionErr 保存有详情凭证时的兼容错误结果。
	value, changed, handled, detailSessionErr := adapter.PersistCookieSession(ctx, detail, orderapp.RefreshCookieUpdate{Value: "new", Handled: true})
	if value != "new" || !changed || !handled || detailSessionErr == nil {
		t.Fatalf("session value=%q changed=%v handled=%v err=%v", value, changed, handled, detailSessionErr)
	}
	adapter.UpdateRunningCookie(ctx, "cid", "cookie")
	adapter.NotifyDelivery("cid", "buyer", "item", "chat", "message")
	adapter.ReportPersistenceFailure("order", context.Canceled)

	// factoryAdapter 保存由空依赖工厂创建的运行时适配器。
	factoryAdapter := NewOrderRuntimeAdapter(nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if factoryAdapter.runtime == nil {
		t.Fatal("nil dependency factory should still create a safe runtime adapter")
	}
}

// TestOrderRuntimeAdapterForwardsAdjustPrice 验证适配器在运行时已装配时把改价请求原样转发给运行时并返回其结果。
func TestOrderRuntimeAdapterForwardsAdjustPrice(t *testing.T) {
	// ctx 是本测试使用的非取消上下文。
	ctx := context.Background()
	// store、cleanup 是带 admin 与 cid 账号夹具的隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// admin 是账号 cid 的所有者。
	admin, err := store.Users.GetByUsername(ctx, "admin")
	if err != nil {
		t.Fatal(err)
	}
	// client 记录转发到平台的改价参数。
	client := &orderRuntimeMTopFake{adjustPrice: func(_ context.Context, _ string, orderID string, cents int64) (bool, []string, string, error) {
		if orderID != "order-fwd" || cents != 990 {
			t.Fatalf("转发参数 order=%q cents=%d", orderID, cents)
		}
		return true, []string{"SUCCESS"}, "", nil
	}}
	// adapter 是包裹真实订单运行时的适配器。
	adapter := OrderRuntimeAdapter{runtime: NewOrderRuntime(store, OrderRuntimeHooks{Client: func() mtop.Client { return client }}, nil, nil)}
	// result 是经适配器转发得到的改价结果。
	result := adapter.AdjustPrice(ctx, "cid", "order-fwd", admin.ID, 990)
	if !result.Success || result.Err != nil || client.adjustPriceCalls != 1 {
		t.Fatalf("转发结果=%+v calls=%d", result, client.adjustPriceCalls)
	}
}
