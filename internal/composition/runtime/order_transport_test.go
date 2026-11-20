package runtime

import (
	"context"
	"strings"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
)

// cleanableQRFake 是同时实现可选 DeleteSession 能力的二维码服务替身。
type cleanableQRFake struct {
	runtimeQRLoginFake
	// deleted 记录被释放的二维码会话标识。
	deleted []string
}

// DeleteSession 记录被释放的二维码会话。
func (fake *cleanableQRFake) DeleteSession(sessionID string) {
	fake.deleted = append(fake.deleted, sessionID)
}

// TestQRLoginTransportDeleteForwardsToOptionalCleaner 覆盖二维码服务提供清理扩展能力时的转发。
func TestQRLoginTransportDeleteForwardsToOptionalCleaner(t *testing.T) {
	// fake 保存支持清理的二维码服务。
	fake := &cleanableQRFake{}
	qrLoginTransport{service: fake}.DeleteSession("s1")
	if len(fake.deleted) != 1 || fake.deleted[0] != "s1" {
		t.Fatalf("deleted=%v", fake.deleted)
	}
}

// lastError 丢弃用例的业务返回值，只保留末尾错误用于断言转发是否到达对应用例。
func lastError(
	// ignored 是用例返回的业务结果，本测试不关心其内容。
	_ any,
	// err 是用例返回的错误，用于判断转发目标。
	err error,
) error {
	return err
}

// TestOrdersTransportForwardsToEveryUseCase 通过未初始化依赖的确定性错误证明每个订单入口都转发到对应用例。
func TestOrdersTransportForwardsToEveryUseCase(t *testing.T) {
	// transport 保存使用空仓储构造的订单 transport；各用例会返回各自的未初始化错误。
	transport := ordersTransport{services: orderapp.NewServiceSet(nil, nil, nil, nil, nil, nil, 10)}
	// ctx 是所有转发调用共用的请求 Context。
	ctx := context.Background()
	// cases 把入口名称映射为调用动作和期望的错误关键词。
	cases := []struct {
		// name 是被测 transport 方法名。
		name string
		// call 执行该方法并返回错误。
		call func() error
		// want 是期望错误中包含的用例标识。
		want string
	}{
		{"RefreshSingle", func() error { return lastError(transport.RefreshSingle(ctx, 1, "o")) }, "订单刷新依赖"},
		{"Refresh", func() error { return lastError(transport.Refresh(ctx, 1, "c", "s")) }, "订单刷新依赖"},
		{"List", func() error { return lastError(transport.List(ctx, orderapp.ListQuery{})) }, "订单列表"},
		{"Get", func() error { return lastError(transport.Get(ctx, 1, "o")) }, "订单详情"},
		{"GetView", func() error { return lastError(transport.GetView(ctx, 1, "o")) }, "订单详情"},
		{"Delete", func() error { return transport.Delete(ctx, 1, "o") }, "订单删除"},
		{"Update", func() error { return transport.Update(ctx, 1, "o", orderapp.UpdateRequest{}) }, "订单更新"},
		{"Import", func() error { return lastError(transport.Import(ctx, 1, nil)) }, "订单导入"},
		{"ManualShip", func() error { return lastError(transport.ManualShip(ctx, orderapp.ManualShipRequest{})) }, "手动发货"},
	}
	// tc 当前待验证的订单入口用例。
	for _, tc := range cases {
		// callErr 保存该入口的转发错误。
		if callErr := tc.call(); callErr == nil || !strings.Contains(callErr.Error(), tc.want) {
			t.Fatalf("%s err=%v want contains %q", tc.name, callErr, tc.want)
		}
	}
}

// TestOrderRefreshJobsTransportForwardsRequests 覆盖刷新任务三个入口及缺少生命周期 Context 提供者的路径。
func TestOrderRefreshJobsTransportForwardsRequests(t *testing.T) {
	// ctx 是请求 Context。
	ctx := context.Background()
	// lifecycle 当前提供的生命周期 Context 函数，nil 或返回 nil 均视为未初始化。
	for _, lifecycle := range []func() context.Context{nil, func() context.Context { return ctx }} {
		// transport 保存未初始化服务的刷新任务 transport。
		transport := orderRefreshJobsTransport{service: &orderapp.RefreshJobService{}, lifecycleContext: lifecycle}
		// createErr 保存创建并启动任务的转发错误，应指出服务未初始化。
		createErr := lastError(transport.CreateAndStart(ctx, 1, "c", "s"))
		if createErr == nil || !strings.Contains(createErr.Error(), "未初始化") {
			t.Fatalf("create err=%v", createErr)
		}
		// getErr 保存读取任务的转发错误，应非空。
		if getErr := lastError(transport.GetJob(ctx, 1, "j")); getErr == nil {
			t.Fatal("get job should forward error")
		}
		// cancelErr 保存取消任务的转发错误，应非空。
		if cancelErr := lastError(transport.CancelForUser(ctx, 1, "j")); cancelErr == nil {
			t.Fatal("cancel should forward error")
		}
	}
}
