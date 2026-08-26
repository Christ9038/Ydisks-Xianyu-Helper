package adapter

import (
	"context"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
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
