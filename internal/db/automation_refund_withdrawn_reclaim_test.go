package db

import (
	"context"
	"testing"
	"time"
)

// TestRefundWithdrawnReleasesSafelyCanceledPaidRun 验证退款撤回回到待发货后，
// 取消前无任何副作用的历史运行不再阻断兜底与重领；已有发送进度或动作结果未知的运行保持终态。
func TestRefundWithdrawnReleasesSafelyCanceledPaidRun(t *testing.T) {
	// s、cleanup 保存测试数据库及关闭责任。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是不会取消的测试上下文。
	ctx := context.Background()
	// userID、cookieID 是测试账号归属。
	userID, cookieID := seedAccount(t, s)
	// ruleID 是启用的发卡加确认发货规则。
	ruleID := seedCatchupRule(t, s, userID, cookieID, "item-1", "退款撤回规则", true,
		[]AutomationActionInput{{ActionType: "send_card", MessageTemplate: "card", Enabled: true}, {ActionType: "confirm_shipment", Enabled: true}})

	// scenarios 描述取消前的运行进度，以及退款撤回后是否允许重新自动发货。
	scenarios := []struct {
		name      string
		setup     string
		reclaimed bool
		// resumed 表示运行是否应作为「只补确认发货」的续跑候选重新开放。
		resumed bool
	}{
		{"未发卡可接管", `UPDATE automation_runs SET action_cursor=0,sent_count=0,action_started=0,attempt_count=1,status='failed',next_retry_at=0,lease_expires_at=0 WHERE id=?`, true, false},
		{"已发卡只补确认发货", `UPDATE automation_runs SET action_cursor=1,sent_count=1,action_started=0,attempt_count=1,status='failed',next_retry_at=0,lease_expires_at=0 WHERE id=?`, false, true},
		{"动作结果未知不可接管", `UPDATE automation_runs SET action_cursor=0,sent_count=0,action_started=1,attempt_count=1,status='failed',next_retry_at=0,lease_expires_at=0 WHERE id=?`, false, false},
	}
	// scenario 是一个独立订单的取消前进度。
	for index, scenario := range scenarios {
		// orderID 为每个场景使用独立订单，避免互相影响。
		orderID := "o-refund-" + string(rune('a'+index))
		seedCatchupOrder(t, s, orderID, cookieID, "item-1", "chat-"+orderID, "pending_ship")
		// runID 是退款前已创建的付款运行。
		runID := seedCatchupRun(t, s, ruleID, cookieID, orderID)
		// setupErr 保存把运行改写为取消前进度的结果。
		if _, setupErr := s.DB.ExecContext(ctx, scenario.setup, runID); setupErr != nil {
			t.Fatal(setupErr)
		}
		// run 是用于条件取消的运行快照。
		run, err := s.Automation.GetRun(ctx, runID)
		if err != nil {
			t.Fatal(err)
		}
		// 订单进入退款中后，调度器按 CancelObsoletePaidRecoveryRun 取消运行。
		// refundErr 保存订单进入退款中的写入结果。
		if refundErr := s.Orders.Upsert(ctx, orderID, OrderUpsertOpts{CookieID: cookieID, OrderStatus: "refunding", PlatformSnapshot: true}); refundErr != nil {
			t.Fatal(refundErr)
		}
		// canceled、cancelErr 保存条件取消结果。
		canceled, cancelErr := s.Automation.CancelObsoletePaidRecoveryRun(ctx, *run, "订单退款中")
		if cancelErr != nil || !canceled {
			t.Fatalf("%s: 取消失败 canceled=%v err=%v", scenario.name, canceled, cancelErr)
		}
		// 买家撤回退款，平台快照让订单回到待发货。
		// withdrawErr 保存退款撤回后订单回到待发货的写入结果。
		if withdrawErr := s.Orders.Upsert(ctx, orderID, OrderUpsertOpts{CookieID: cookieID, OrderStatus: "pending_ship", PlatformSnapshot: true}); withdrawErr != nil {
			t.Fatal(withdrawErr)
		}

		// candidates 保存无有效运行订单的兜底扫描结果。
		candidates, err := s.Automation.PendingShipOrdersWithoutPaidRunAfter(ctx, "", 200)
		if err != nil {
			t.Fatal(err)
		}
		// listed 表示该订单是否重新进入兜底候选。
		listed := false
		// candidate 是一条兜底候选订单。
		for _, candidate := range candidates {
			listed = listed || candidate.OrderID == orderID
		}
		if listed != scenario.reclaimed {
			t.Fatalf("%s: 兜底候选=%v want %v", scenario.name, listed, scenario.reclaimed)
		}
		// resumes 保存续跑扫描结果，用于核对已发卡运行只被当作确认发货续跑。
		resumes, resumeErr := s.Automation.PendingShipResumableRunsAfter(ctx, orderID[:len(orderID)-1], 5, 1)
		if resumeErr != nil {
			t.Fatal(resumeErr)
		}
		// resumable 表示该订单的运行是否出现在续跑候选中。
		resumable := len(resumes) == 1 && resumes[0].RunID == runID
		if resumable != scenario.resumed {
			t.Fatalf("%s: 续跑候选=%v want %v", scenario.name, resumable, scenario.resumed)
		}
		// reopened、reopenErr 保存重开结果；只有可续跑场景允许取得执行权。
		reopened, reopenErr := s.Automation.ReopenRunForRecovery(ctx, runID, 1, time.Now().Add(time.Minute).Unix())
		if reopenErr != nil || reopened != scenario.resumed {
			t.Fatalf("%s: 重开=%v want %v err=%v", scenario.name, reopened, scenario.resumed, reopenErr)
		}
		if reopened {
			// 重开后运行回到 running，后续同键重领必须拒绝，避免重复执行。
			continue
		}
		// newID、started 是同一幂等键重领的结果。
		newID, started, err := s.Automation.TryStartRun(ctx, AutomationRun{
			RuleID: ruleID, CookieID: cookieID, OrderID: orderID, TriggerType: "order_paid",
			TriggerKey: "catchup:" + orderID, LeaseExpiresAt: time.Now().Add(time.Minute).Unix(),
		})
		if err != nil {
			t.Fatal(err)
		}
		if started != scenario.reclaimed {
			t.Fatalf("%s: 重领=%v want %v", scenario.name, started, scenario.reclaimed)
		}
		if started && newID != runID {
			t.Fatalf("%s: 应复用原运行 %d，got %d", scenario.name, runID, newID)
		}
	}
}
