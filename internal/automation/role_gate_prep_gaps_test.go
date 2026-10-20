package automation

import (
	"context"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestAuthorizeSellerTaskEarlyRejections 覆盖未知角色卖家核验在证据不足、存储缺失与读取失败时的拒绝分支。
func TestAuthorizeSellerTaskEarlyRejections(t *testing.T) {
	// ctx 是核验调用共用的上下文。
	ctx := context.Background()
	// bare 是没有任何仓库的中心，用于覆盖存储不可用分支。
	bare := &Center{store: &db.Store{}}
	// paidTask 是缺少 role 的 WebSocket 付款任务。
	paidTask := Task{Source: "ws", TriggerType: TriggerOrderPaid, AccountID: "cid", OrderID: "o1"}
	// createdTask 是缺少 role 的 WebSocket 拍下任务。
	createdTask := Task{Source: "ws", TriggerType: TriggerOrderCreated, AccountID: "cid", ItemID: "i1"}

	t.Run("显式买家角色拒绝", func(t *testing.T) {
		// task 是显式买家角色的付款任务。
		task := paidTask
		task.OrderRole = OrderRoleBuyer
		// _, ok、reason、err 保存核验结论。
		_, ok, reason, err := bare.authorizeWebSocketSellerTask(ctx, task)
		if ok || reason != "explicit_buyer_role" || err != nil {
			t.Fatalf("ok=%v reason=%s err=%v", ok, reason, err)
		}
	})
	t.Run("拍下事件缺少商品号", func(t *testing.T) {
		// task 是没有商品号的拍下任务。
		task := createdTask
		task.ItemID = ""
		// _, ok、reason、err 保存核验结论。
		_, ok, reason, err := bare.authorizeWebSocketSellerTask(ctx, task)
		if ok || reason != "missing_local_item" || err != nil {
			t.Fatalf("ok=%v reason=%s err=%v", ok, reason, err)
		}
	})
	t.Run("拍下事件商品仓库缺失", func(t *testing.T) {
		// _, ok、reason、err 保存核验结论。
		_, ok, reason, err := bare.authorizeWebSocketSellerTask(ctx, createdTask)
		if ok || reason != "item_store_unavailable" || err != nil {
			t.Fatalf("ok=%v reason=%s err=%v", ok, reason, err)
		}
		// nilCenter 覆盖中心本身为空的防御分支。
		var nilCenter *Center
		_, ok, reason, err = nilCenter.authorizeWebSocketSellerTask(ctx, createdTask)
		if ok || reason != "item_store_unavailable" || err != nil {
			t.Fatalf("nil center: ok=%v reason=%s err=%v", ok, reason, err)
		}
	})
	t.Run("付款事件订单仓库缺失与缺订单号", func(t *testing.T) {
		// _, ok、reason、err 保存核验结论。
		_, ok, reason, err := bare.authorizeWebSocketSellerTask(ctx, paidTask)
		if ok || reason != "order_store_unavailable" || err != nil {
			t.Fatalf("ok=%v reason=%s err=%v", ok, reason, err)
		}
	})
	t.Run("数据库存储读取失败", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// center 是使用真实仓库的中心。
		center := New(store, nil, nil)
		execGapSQL(t, store, `ALTER TABLE item_info RENAME TO item_info_gap_hidden`)
		// _, ok、reason、err 保存拍下商品查询失败结论。
		_, ok, reason, err := center.authorizeWebSocketSellerTask(ctx, createdTask)
		if ok || reason != "" || err == nil || !strings.Contains(err.Error(), "拍下商品归属") {
			t.Fatalf("ok=%v reason=%s err=%v", ok, reason, err)
		}
		// noID 是缺少订单号的付款任务。
		noID := paidTask
		noID.OrderID = ""
		_, ok, reason, err = center.authorizeWebSocketSellerTask(ctx, noID)
		if ok || reason != "missing_order_id" || err != nil {
			t.Fatalf("缺订单号: ok=%v reason=%s err=%v", ok, reason, err)
		}
		execGapSQL(t, store, `ALTER TABLE orders RENAME TO orders_gap_hidden`)
		_, ok, reason, err = center.authorizeWebSocketSellerTask(ctx, paidTask)
		if ok || err == nil || !strings.Contains(err.Error(), "订单事实") {
			t.Fatalf("订单读取失败: ok=%v reason=%s err=%v", ok, reason, err)
		}
	})
	t.Run("订单事实不满足条件", func(t *testing.T) {
		// store、cleanup 保存隔离数据库。
		store, cleanup := newAutomationTestStore(t)
		defer cleanup()
		// center 是使用真实仓库的中心。
		center := New(store, nil, nil)
		// _, ok、reason、err 保存订单为空时的结论。
		_, ok, reason, err := center.authorizeWebSocketSellerTaskWithOrder(ctx, paidTask, nil)
		if ok || reason != "missing_local_order" || err != nil {
			t.Fatalf("nil 订单: ok=%v reason=%s err=%v", ok, reason, err)
		}
		// done 是已发货订单。
		done := &db.Order{OrderID: "o1", CookieID: "cid", ItemID: "i1", OrderStatus: "shipped"}
		_, ok, reason, _ = center.authorizeWebSocketSellerTaskWithOrder(ctx, paidTask, done)
		if ok || reason != "order_not_pending_ship" {
			t.Fatalf("已发货: ok=%v reason=%s", ok, reason)
		}
		// noItem 是待发货但缺少商品号的订单。
		noItem := &db.Order{OrderID: "o1", CookieID: "cid", OrderStatus: "pending_ship"}
		_, ok, reason, _ = center.authorizeWebSocketSellerTaskWithOrder(ctx, paidTask, noItem)
		if ok || reason != "incomplete_local_order_identity" {
			t.Fatalf("缺商品: ok=%v reason=%s", ok, reason)
		}
		// pending 是完整待发货订单。
		pending := &db.Order{OrderID: "o1", CookieID: "cid", ItemID: "i1", OrderStatus: "pending_ship"}
		_, ok, reason, _ = bare.authorizeWebSocketSellerTaskWithOrder(ctx, paidTask, pending)
		if ok || reason != "item_store_unavailable" {
			t.Fatalf("商品仓库缺失: ok=%v reason=%s", ok, reason)
		}
	})
}

// TestRoleGateHelpersEdgeCases 覆盖订单状态判断、延期与键生成的防御分支。
func TestRoleGateHelpersEdgeCases(t *testing.T) {
	if isPendingShipOrder(nil) {
		t.Fatal("空订单不应视为待发货")
	}
	// bare 是没有延期存储的中心。
	bare := &Center{}
	// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
	if err := bare.deferUnknownRoleTask(context.Background(), Task{}, "x"); err == nil {
		t.Fatal("缺少存储应报错")
	}
	// task 是带不可序列化原始字段的任务，用于覆盖序列化失败。
	task := Task{AccountID: "cid", TriggerType: TriggerOrderPaid, Raw: map[string]any{"bad": make(chan int)}}
	// store、cleanup 保存隔离数据库。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// center 是使用真实延期队列的中心。
	center := New(store, nil, nil)
	// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
	if err := center.deferUnknownRoleTask(context.Background(), task, "x"); err == nil || !strings.Contains(err.Error(), "序列化") {
		t.Fatalf("应返回序列化错误: %v", err)
	}
	// failing 是延期表被隐藏后的写入场景。
	execGapSQL(t, store, `DROP TABLE automation_pending_tasks`)
	// err 保存被测调用返回的错误，用于断言失败分支是否按预期触发。
	if err := center.deferUnknownRoleTask(context.Background(), Task{AccountID: "cid", TriggerType: TriggerOrderPaid, OrderID: "o"}, "x"); err == nil {
		t.Fatal("延期表缺失应报错")
	}
	// fixed 表示历史任务已固化的待核验键。
	fixed := Task{Raw: map[string]any{roleVerificationTaskKeyField: " fixed-key "}}
	// got 保存被测函数的实际返回值，供断言比较。
	if got := roleVerificationTaskKey(fixed); got != "fixed-key" {
		t.Fatalf("应复用固化键: %q", got)
	}
	// unmarshalable 的原始报文无法 JSON 编码，键应回退到字段拼接哈希。
	bad := Task{TriggerType: TriggerOrderPaid, AccountID: "cid", Raw: map[string]any{"bad": make(chan int)}}
	// got 保存被测函数的实际返回值，供断言比较。
	if got := roleVerificationTaskKey(bad); !strings.HasPrefix(got, "role_verification:") {
		t.Fatalf("回退键异常: %q", got)
	}
	// copied 验证复制键时保留原有字段。
	copied := taskWithRoleVerificationKey(Task{Raw: map[string]any{"a": 1}}, "k")
	if copied.Raw["a"] != 1 || copied.Raw[roleVerificationTaskKeyField] != "k" {
		t.Fatalf("复制结果异常: %v", copied.Raw)
	}
}

// TestResolvePaidTaskOrderFailures 覆盖订单事实回填的未命中与读取失败分支。
func TestResolvePaidTaskOrderFailures(t *testing.T) {
	// ctx 是回填调用共用的上下文。
	ctx := context.Background()
	// store、cleanup 保存隔离数据库。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// center 是使用真实订单仓库的中心。
	center := New(store, nil, nil)
	// byOrder 是携带订单号的付款任务。
	byOrder := Task{TriggerType: TriggerOrderPaid, AccountID: "cid", OrderID: "missing-order"}
	// got、err 保存订单不存在时原样返回的结果。
	got, err := center.resolvePaidTaskOrder(ctx, byOrder)
	if err != nil || got.OrderID != "missing-order" {
		t.Fatalf("缺订单应原样返回: %+v %v", got, err)
	}
	// byChat 是只有会话号的付款任务。
	byChat := Task{TriggerType: TriggerOrderPaid, AccountID: "cid", ChatID: "chat-none", BuyerID: "b", ItemID: "i"}
	got, err = center.resolvePaidTaskOrder(ctx, byChat)
	if err != nil || got.OrderID != "" {
		t.Fatalf("无待发货订单应原样返回: %+v %v", got, err)
	}
	execGapSQL(t, store, `ALTER TABLE orders RENAME TO orders_gap_hidden`)
	if _, err = center.resolvePaidTaskOrder(ctx, byOrder); err == nil || !strings.Contains(err.Error(), "按订单回填") {
		t.Fatalf("应返回订单读取错误: %v", err)
	}
	if got, err = center.resolvePaidTaskOrder(ctx, byChat); err != nil || got.OrderID != "" {
		t.Fatalf("缺订单号的付款事件不应读取会话订单: %+v %v", got, err)
	}
}
