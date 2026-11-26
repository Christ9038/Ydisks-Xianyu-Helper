package db

import (
	"context"
	"testing"
)

// TestAutomationCountRunsForRuleAndCompletedOrders 验证规则运行计数，以及“确认收货后”规则能取到非系统发货的已完成订单。
func TestAutomationCountRunsForRuleAndCompletedOrders(t *testing.T) {
	// s、cleanup 是测试数据库及其清理函数。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试上下文。
	ctx := context.Background()
	// uid、cid 是测试用户与账号。
	uid, cid := seedAccount(t, s)
	// ruleID 是一条账号级求评价规则。
	ruleID, err := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "", "review_missing_timeout", true, 100,
		AutomationActionInput{ActionType: "send_review_copy", ConfigJSON: `{"source":"pool","pool":["好"]}`, Enabled: true}))
	if err != nil {
		t.Fatal(err)
	}
	// total 是新规则的运行数；countErr 是计数查询错误。
	if total, countErr := s.Automation.CountRunsForRule(ctx, ruleID); countErr != nil || total != 0 {
		t.Fatalf("新规则运行数应为 0: %d %v", total, countErr)
	}
	// execErr 是插入测试运行记录的错误。
	if _, execErr := s.DB.ExecContext(ctx, `INSERT INTO automation_runs(rule_id,cookie_id,order_id,trigger_type,trigger_key,status,attempt_count) VALUES(?,?,?,'review_missing_timeout','k1','success',1)`, ruleID, cid, "o1"); execErr != nil {
		t.Fatal(execErr)
	}
	// total 是插入一条运行后的运行数；countErr 是计数查询错误。
	if total, countErr := s.Automation.CountRunsForRule(ctx, ruleID); countErr != nil || total != 1 {
		t.Fatalf("应统计到 1 次运行: %d %v", total, countErr)
	}

	// 未系统发货但已确认收货的订单：放宽后的查询应取到它。
	if upsertErr := s.Orders.Upsert(ctx, "done-1", OrderUpsertOpts{ItemID: "i1", BuyerID: "b1", CookieID: cid, ChatID: "chat1"}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// due 是尚未完成的订单的到期查询结果，应为空；dueErr 是查询错误。
	if due, dueErr := s.Automation.DueReviewRequestOrders(ctx, 10); dueErr != nil || len(due) != 0 {
		t.Fatalf("未发货未完成的订单不应出现: %d %v", len(due), dueErr)
	}
	// markErr 是写入确认收货时间的错误。
	if markErr := s.Automation.MarkOrderEventTime(ctx, "done-1", "completed_at"); markErr != nil {
		t.Fatal(markErr)
	}
	// due 是确认收货后的到期查询结果，应包含该订单；dueErr 是查询错误。
	if due, dueErr := s.Automation.DueReviewRequestOrders(ctx, 10); dueErr != nil || len(due) != 1 || due[0].OrderID != "done-1" {
		t.Fatalf("已确认收货的订单应出现: %+v %v", due, dueErr)
	}
}
