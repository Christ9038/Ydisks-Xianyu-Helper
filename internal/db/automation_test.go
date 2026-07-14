package db

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

// --- automation.go ---

// makeAutomationRule 帮助构造一条规则输入。
func makeAutomationRule(cid string, uid int64, itemID, trigger string, enabled bool, priority int, actions ...AutomationActionInput) AutomationRuleInput {
	return AutomationRuleInput{
		UserID: uid, CookieID: cid, ItemID: itemID, Name: "rule-" + trigger,
		TriggerType: trigger, Enabled: enabled, Priority: priority,
		Actions: actions,
	}
}

// TestAutomation_ListForUserAndActions ListForUser + Actions 路径。
func TestAutomation_ListForUserAndActions(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)

	// 建一张卡券，动作里引用它。
	cardID, _ := s.Cards.Create(ctx, &CardFull{Name: "卡密", Type: "text", TextContent: "C", Enabled: true, UserID: uid})

	ruleID, err := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "i1", "paid", true, 100,
		AutomationActionInput{ActionType: "send_card", CardID: cardID, DeliveryCount: 2, Enabled: true, SortOrder: 1},
		AutomationActionInput{ActionType: "send_msg", MessageTemplate: "hi", Enabled: false, SortOrder: 2},
	))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// ListForUser 应返回该规则 + 关联 item_title。
	// 先建 item_info 以验证 LEFT JOIN 取到 title。
	s.Items.Upsert(ctx, &ItemInfoRow{CookieID: cid, ItemID: "i1", ItemTitle: "商品1"})

	rules, err := s.Automation.ListForUser(ctx, uid)
	if err != nil {
		t.Fatalf("ListForUser: %v", err)
	}
	if len(rules) != 1 {
		t.Fatalf("ListForUser len=%d want 1", len(rules))
	}
	r := rules[0]
	if r.ID != ruleID || r.CookieID != cid || r.ItemID != "i1" || r.ItemTitle != "商品1" || !r.Enabled {
		t.Fatalf("rule 字段: %#v", r)
	}
	if len(r.Actions) != 2 {
		t.Fatalf("actions len=%d want 2", len(r.Actions))
	}
	// 动作按 sort_order 升序。
	if r.Actions[0].ActionType != "send_card" || r.Actions[0].CardID != cardID || r.Actions[0].CardName != "卡密" ||
		r.Actions[0].DeliveryCount != 2 || !r.Actions[0].Enabled {
		t.Fatalf("action[0]: %#v", r.Actions[0])
	}
	if r.Actions[1].ActionType != "send_msg" || r.Actions[1].Enabled {
		t.Fatalf("action[1]: %#v", r.Actions[1])
	}

	// Actions 单独查询。
	acts, err := s.Automation.Actions(ctx, ruleID)
	if err != nil {
		t.Fatalf("Actions: %v", err)
	}
	if len(acts) != 2 || acts[0].CardName != "卡密" {
		t.Fatalf("Actions: %#v", acts)
	}
	// 不存在的规则 → 空切片，不报错。
	none, err := s.Automation.Actions(ctx, 99999)
	if err != nil || len(none) != 0 {
		t.Fatalf("Actions 不存在规则: %#v err=%v", none, err)
	}
}

// TestAutomation_MatchPriority 商品精确规则优先于账号级规则；enabled=false 不匹配。
func TestAutomation_MatchPriority(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)

	// 账号级规则（item_id 空），priority=200。
	_, err := s.Automation.Create(ctx, AutomationRuleInput{
		UserID: uid, CookieID: cid, ItemID: "", Name: "account-rule",
		TriggerType: "paid", Enabled: true, Priority: 200,
		Actions: []AutomationActionInput{{ActionType: "send_msg", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("Create account rule: %v", err)
	}
	// 商品级规则（item_id=i1），priority=100。
	itemRuleID, err := s.Automation.Create(ctx, AutomationRuleInput{
		UserID: uid, CookieID: cid, ItemID: "i1", Name: "item-rule",
		TriggerType: "paid", Enabled: true, Priority: 100,
		Actions: []AutomationActionInput{{ActionType: "send_card", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("Create item rule: %v", err)
	}
	// 禁用规则，不应被匹配。
	_, err = s.Automation.Create(ctx, AutomationRuleInput{
		UserID: uid, CookieID: cid, ItemID: "i1", Name: "disabled-rule",
		TriggerType: "paid", Enabled: false, Priority: 50,
		Actions: []AutomationActionInput{{ActionType: "send_card", Enabled: true}},
	})
	if err != nil {
		t.Fatalf("Create disabled rule: %v", err)
	}

	matched, err := s.Automation.Match(ctx, cid, "i1", "paid")
	if err != nil {
		t.Fatalf("Match: %v", err)
	}
	if len(matched) != 1 {
		t.Fatalf("Match len=%d want 1（商品级存在时不叠加账号级）", len(matched))
	}
	// 商品级规则应排第一（CASE WHEN item_id=? THEN 0 ELSE 1 END）。
	if matched[0].ID != itemRuleID {
		t.Fatalf("Match 顺序: first id=%d want item rule %d", matched[0].ID, itemRuleID)
	}
	fallback, err := s.Automation.Match(ctx, cid, "other-item", "paid")
	if err != nil || len(fallback) != 1 || fallback[0].ItemID != "" {
		t.Fatalf("无商品级规则时应回退账号级: %#v err=%v", fallback, err)
	}

	// 不匹配的 trigger_type → 空。
	none, err := s.Automation.Match(ctx, cid, "i1", "shipped")
	if err != nil || len(none) != 0 {
		t.Fatalf("shipped 不应匹配: %#v err=%v", none, err)
	}
}

func TestAutomationMatchReturnsOnlyHighestPriorityRule(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	first, err := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "i1", "paid", true, 50))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "i1", "paid", true, 100)); err != nil {
		t.Fatal(err)
	}
	matched, err := s.Automation.Match(ctx, cid, "i1", "paid")
	if err != nil || len(matched) != 1 || matched[0].ID != first {
		t.Fatalf("matched=%+v err=%v", matched, err)
	}
}

// TestAutomation_UpdateDelete Update 替换动作 + Delete 路径。
func TestAutomation_UpdateDelete(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)

	ruleID, err := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "i1", "paid", true, 100,
		AutomationActionInput{ActionType: "send_card", Enabled: true},
	))
	if err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Update：改 item_id + 替换动作为两条。
	if err := s.Automation.Update(ctx, uid, ruleID, AutomationRuleInput{
		UserID: uid, CookieID: cid, ItemID: "i2", Name: "rule-updated",
		TriggerType: "shipped", Enabled: false, Priority: 50,
		Actions: []AutomationActionInput{
			{ActionType: "send_msg", MessageTemplate: "x", Enabled: true, SortOrder: 1},
			{ActionType: "send_card", Enabled: true, SortOrder: 2},
		},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	rules, _ := s.Automation.ListForUser(ctx, uid)
	if len(rules) != 1 {
		t.Fatalf("Update 后 len=%d want 1", len(rules))
	}
	r := rules[0]
	if r.ItemID != "i2" || r.TriggerType != "shipped" || r.Enabled || r.Priority != 50 || len(r.Actions) != 2 {
		t.Fatalf("Update 后字段: %#v", r)
	}

	// Update 不存在的规则 → ErrNotFound。
	err = s.Automation.Update(ctx, uid, 99999, AutomationRuleInput{CookieID: cid, Name: "x", TriggerType: "paid"})
	if !errors.Is(err, ErrNotFound) {
		t.Fatalf("Update 不存在应 ErrNotFound, got %v", err)
	}

	// Delete。
	if err := s.Automation.Delete(ctx, uid, ruleID); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	rules, _ = s.Automation.ListForUser(ctx, uid)
	if len(rules) != 0 {
		t.Fatalf("Delete 后 len=%d want 0", len(rules))
	}
	// 重复 Delete → ErrNotFound。
	if err := s.Automation.Delete(ctx, uid, ruleID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("重复 Delete 应 ErrNotFound, got %v", err)
	}
	// 跨用户 Delete → ErrNotFound（隔离校验）。
	otherID, _ := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "i3", "paid", true, 100))
	if err := s.Automation.Delete(ctx, uid+999, otherID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨用户 Delete 应 ErrNotFound, got %v", err)
	}
}

// TestAutomation_MarkOrderEventTime 白名单字段 + 非法字段拒绝。
func TestAutomation_MarkOrderEventTime(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)
	s.Orders.Upsert(ctx, "o1", OrderUpsertOpts{ItemID: "i1", BuyerID: "b1", CookieID: cid})

	// 白名单字段全部能更新。
	for _, f := range []string{"paid_at", "shipped_at", "completed_at", "buyer_reviewed_at", "last_review_request_at"} {
		if err := s.Automation.MarkOrderEventTime(ctx, "o1", f); err != nil {
			t.Fatalf("MarkOrderEventTime(%s): %v", f, err)
		}
	}
	const originalPaidAt = "2020-01-02 03:04:05"
	if _, err := s.DB.ExecContext(ctx, `UPDATE orders SET paid_at=? WHERE order_id='o1'`, originalPaidAt); err != nil {
		t.Fatal(err)
	}
	if err := s.Automation.MarkOrderEventTime(ctx, "o1", "paid_at"); err != nil {
		t.Fatal(err)
	}
	var paidAt string
	if err := s.DB.QueryRowContext(ctx, `SELECT paid_at FROM orders WHERE order_id='o1'`).Scan(&paidAt); err != nil || paidAt != originalPaidAt {
		t.Fatalf("event timestamp overwritten: %q err=%v", paidAt, err)
	}
	// 非法字段拒绝。
	err := s.Automation.MarkOrderEventTime(ctx, "o1", "order_status")
	if err == nil || !strings.Contains(err.Error(), "不允许") {
		t.Fatalf("非法字段应拒绝, got %v", err)
	}
	err = s.Automation.MarkOrderEventTime(ctx, "o1", "created_at")
	if err == nil || !strings.Contains(err.Error(), "不允许") {
		t.Fatalf("非法字段应拒绝, got %v", err)
	}
}

// TestAutomation_IncrementReviewRequest + DueReviewRequestOrders。
func TestAutomation_ReviewRequest(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	if _, err := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "", "review_missing_timeout", true, 100,
		AutomationActionInput{ActionType: "send_text", MessageTemplate: "review", Enabled: true})); err != nil {
		t.Fatal(err)
	}

	// 建一条已发货、有 chat_id、未评价的订单 → 应被 DueReviewRequestOrders 取到。
	s.Orders.Upsert(ctx, "o1", OrderUpsertOpts{
		ItemID: "i1", BuyerID: "b1", CookieID: cid,
		ChatID: "chat1", OrderStatus: "shipped",
		SystemShipped: boolPtr(true),
	})
	// 标记 paid_at（用 MarkOrderEventTime，顺带覆盖该函数）。
	s.Automation.MarkOrderEventTime(ctx, "o1", "paid_at")

	// IncrementReviewRequest。
	if err := s.Automation.IncrementReviewRequest(ctx, "o1"); err != nil {
		t.Fatalf("IncrementReviewRequest: %v", err)
	}
	got, _ := s.Orders.Get(ctx, "o1")
	if got.ReviewRequestCount != 1 {
		t.Fatalf("ReviewRequestCount=%d want 1", got.ReviewRequestCount)
	}
	if got.LastReviewRequestAt == "" {
		t.Fatal("LastReviewRequestAt 应非空")
	}
	// 再加一次 → 2。
	s.Automation.IncrementReviewRequest(ctx, "o1")
	got, _ = s.Orders.Get(ctx, "o1")
	if got.ReviewRequestCount != 2 {
		t.Fatalf("ReviewRequestCount=%d want 2", got.ReviewRequestCount)
	}

	// DueReviewRequestOrders 应返回该订单。
	due, err := s.Automation.DueReviewRequestOrders(ctx, 100)
	if err != nil {
		t.Fatalf("DueReviewRequestOrders: %v", err)
	}
	if len(due) != 1 || due[0].OrderID != "o1" {
		t.Fatalf("due=%#v", due)
	}
	// 标记已评价后不应再出现。
	s.Automation.MarkOrderEventTime(ctx, "o1", "buyer_reviewed_at")
	due, _ = s.Automation.DueReviewRequestOrders(ctx, 100)
	if len(due) != 0 {
		t.Fatalf("已评价后 due len=%d want 0", len(due))
	}

	// limit<=0 → 默认 200。
	due, _ = s.Automation.DueReviewRequestOrders(ctx, 0)
	// 仍应为 0（已评价）。
	if len(due) != 0 {
		t.Fatalf("limit<=0 due len=%d want 0", len(due))
	}
}

// TestAutomation_CreatePriorityDefault priority<=0 时默认 100。
func TestAutomation_CreatePriorityDefault(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)

	// priority=0 → 应被改为 100。
	ruleID, err := s.Automation.Create(ctx, AutomationRuleInput{
		UserID: uid, CookieID: cid, ItemID: "i1", Name: "r",
		TriggerType: "paid", Enabled: true, Priority: 0,
	})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	rules, _ := s.Automation.ListForUser(ctx, uid)
	if len(rules) != 1 || rules[0].ID != ruleID || rules[0].Priority != 100 {
		t.Fatalf("priority 默认值: %#v", rules[0])
	}
}

// TestAutomation_TryStartRunPostgresBranch 占位：SQLite 走 LastInsertId 分支。
// 这里验证 SQLite 下 id 单调递增、FinishRun 后状态。
func TestAutomation_TryStartRunAndFinishRun(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)

	ruleID, _ := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "i1", "paid", true, 100,
		AutomationActionInput{ActionType: "send_card", Enabled: true},
	))
	run := AutomationRun{
		RuleID: ruleID, CookieID: cid, ItemID: "i1", OrderID: "o1",
		TriggerType: "paid", TriggerKey: "paid:o1",
		RawEventJSON: `{"a":1}`, // 合法 JSON，验证 validJSON 透传
	}
	id, started, err := s.Automation.TryStartRun(ctx, run)
	if err != nil || !started || id == 0 {
		t.Fatalf("TryStartRun: id=%d started=%v err=%v", id, started, err)
	}
	// FinishRun。
	if err := s.Automation.FinishRun(ctx, id, 1, "done", 1, ""); err != nil {
		t.Fatalf("FinishRun: %v", err)
	}
}

func TestAutomation_TryStartRunRecoversStaleAndUnsentFailedRuns(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	ruleID, _ := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "i1", "paid", true, 100))
	run := AutomationRun{
		RuleID: ruleID, CookieID: cid, ItemID: "i1", OrderID: "o-retry",
		TriggerType: "paid", TriggerKey: "paid:o-retry",
	}
	id, started, err := s.Automation.TryStartRun(ctx, run)
	if err != nil || !started {
		t.Fatalf("first start: id=%d started=%v err=%v", id, started, err)
	}
	if _, started, err = s.Automation.TryStartRun(ctx, run); err != nil || started {
		t.Fatalf("active lease must deduplicate: started=%v err=%v", started, err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	recoveredID, started, err := s.Automation.TryStartRun(ctx, run)
	if err != nil || !started || recoveredID != id {
		t.Fatalf("legacy/stale running row should recover: id=%d started=%v err=%v", recoveredID, started, err)
	}
	recoveredRun, err := s.Automation.GetRun(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Automation.FinishRun(ctx, id, recoveredRun.AttemptCount, "failed", 0, "temporary"); err != nil {
		t.Fatal(err)
	}
	if _, started, err = s.Automation.TryStartRun(ctx, run); err != nil || started {
		t.Fatalf("failed run must honor retry delay: started=%v err=%v", started, err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE automation_runs SET next_retry_at=0 WHERE id=?`, id); err != nil {
		t.Fatal(err)
	}
	if _, started, err = s.Automation.TryStartRun(ctx, run); err != nil || !started {
		t.Fatalf("unsent failed run should retry: started=%v err=%v", started, err)
	}
	retriedRun, err := s.Automation.GetRun(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Automation.FinishRun(ctx, id, retriedRun.AttemptCount, "failed", 1, "partial"); err != nil {
		t.Fatal(err)
	}
	if _, started, err = s.Automation.TryStartRun(ctx, run); err != nil || started {
		t.Fatalf("partially sent run must never retry automatically: started=%v err=%v", started, err)
	}
}

func TestAutomationRunAttemptFencesStaleWorker(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	ruleID, err := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "i1", "paid", true, 100,
		AutomationActionInput{ActionType: "send_text", MessageTemplate: "x", Enabled: true}))
	if err != nil {
		t.Fatal(err)
	}
	runID, started, err := s.Automation.TryStartRun(ctx, AutomationRun{
		RuleID: ruleID, CookieID: cid, TriggerType: "paid", TriggerKey: "attempt-fence", LeaseExpiresAt: 1,
	})
	if err != nil || !started {
		t.Fatalf("start=%v err=%v", started, err)
	}
	stale, err := s.Automation.GetRun(ctx, runID)
	if err != nil || stale.AttemptCount != 1 {
		t.Fatalf("stale run=%+v err=%v", stale, err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0 WHERE id=?`, runID); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Automation.ClaimRecoveryRun(ctx, runID, time.Now().UTC().Add(time.Minute).Unix())
	if err != nil || !claimed {
		t.Fatalf("claim recovery=%v err=%v", claimed, err)
	}
	current, err := s.Automation.GetRun(ctx, runID)
	if err != nil || current.AttemptCount != 2 {
		t.Fatalf("current run=%+v err=%v", current, err)
	}
	if ok, err := s.Automation.StartRunAction(ctx, runID, stale.AttemptCount, 0, time.Now().Add(time.Minute).Unix()); err != nil || ok {
		t.Fatalf("stale worker must not start action: ok=%v err=%v", ok, err)
	}
	if ok, err := s.Automation.StartRunAction(ctx, runID, current.AttemptCount, 0, time.Now().Add(time.Minute).Unix()); err != nil || !ok {
		t.Fatalf("current worker start: ok=%v err=%v", ok, err)
	}
	if err := s.Automation.FinishRun(ctx, runID, stale.AttemptCount, "failed", 0, "stale"); !errors.Is(err, ErrAutomationRunLeaseLost) {
		t.Fatalf("stale finish err=%v", err)
	}
	if err := s.Automation.QuarantineRun(ctx, runID, stale.AttemptCount, "stale"); !errors.Is(err, ErrAutomationRunLeaseLost) {
		t.Fatalf("stale quarantine err=%v", err)
	}
	afterStale, err := s.Automation.GetRun(ctx, runID)
	if err != nil || afterStale.Status != "running" || !afterStale.ActionStarted || afterStale.AttemptCount != current.AttemptCount {
		t.Fatalf("stale worker changed current run: %+v err=%v", afterStale, err)
	}
	if err := s.Automation.AdvanceRunAction(ctx, runID, current.AttemptCount, 0, 1); err != nil {
		t.Fatal(err)
	}
	if err := s.Automation.FinishRun(ctx, runID, current.AttemptCount, "success", 1, ""); err != nil {
		t.Fatal(err)
	}
}

// TestValidJSON validJSON 的非法 JSON 兜底为 {}。
func TestValidJSON(t *testing.T) {
	if got := validJSON(""); got != "{}" {
		t.Errorf("validJSON('')=%q want {}", got)
	}
	if got := validJSON("not json"); got != "{}" {
		t.Errorf("validJSON('not json')=%q want {}", got)
	}
	if got := validJSON(`{"x":1}`); got != `{"x":1}` {
		t.Errorf("validJSON 合法 JSON 应原样: %q", got)
	}
}

func TestAutomationIssuesCanBeListedAndResolved(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	ruleID, err := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "", "buyer_reviewed", true, 100,
		AutomationActionInput{ActionType: "send_text", MessageTemplate: "x", Enabled: true}))
	if err != nil {
		t.Fatal(err)
	}
	runID, started, err := s.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cid, OrderID: "issue-order",
		TriggerType: "buyer_reviewed", TriggerKey: "issue-key", RawEventJSON: `{}`, LeaseExpiresAt: 1})
	if err != nil || !started {
		t.Fatalf("start=%v err=%v", started, err)
	}
	if ok, err := s.Automation.StartRunAction(ctx, runID, 1, 0, 1); err != nil || !ok {
		t.Fatalf("action start=%v err=%v", ok, err)
	}
	if err := s.Automation.QuarantineRunResult(ctx, runID, 1, 1, "unknown"); err != nil {
		t.Fatal(err)
	}
	if err := s.Automation.DeferTask(ctx, DeferredAutomationTask{TaskKey: "dead", CookieID: cid, TriggerType: "buyer_reviewed", TaskJSON: `{}`, DueAt: 0}); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET status='dead_letter',attempt_count=5,error_message='bad' WHERE task_key='dead'`)
	runs, tasks, err := s.Automation.ListIssues(ctx, uid)
	if err != nil || len(runs) != 1 || len(tasks) != 1 {
		t.Fatalf("runs=%+v tasks=%+v err=%v", runs, tasks, err)
	}
	if runs[0].IssueKind != "external_result_unknown" || !containsString(runs[0].AllowedResolutions, "continue") {
		t.Fatalf("unexpected issue policy: %+v", runs[0])
	}
	if containsString(runs[0].AllowedResolutions, "retry") {
		t.Fatalf("unknown external result must not allow retry: %+v", runs[0])
	}
	if err := s.Automation.ResolveRunIssue(ctx, uid, runID, "retry"); err == nil {
		t.Fatal("unknown external result must reject retry")
	}
	if err := s.Automation.ResolveRunIssue(ctx, uid, runID, "continue"); err != nil {
		t.Fatal(err)
	}
	run, _ := s.Automation.GetRun(ctx, runID)
	if run.Status != "running" || run.ActionStarted || run.ActionCursor != 1 {
		t.Fatalf("resolved run=%+v", run)
	}
	if err := s.Automation.PostponeRecoveryRun(ctx, runID, 4102444800); err != nil {
		t.Fatal(err)
	}
	due, err := s.Automation.DueRecoveryRuns(ctx, 10)
	if err != nil || len(due) != 0 {
		t.Fatalf("postponed run must leave the due queue: %+v err=%v", due, err)
	}
	if err := s.Automation.ResolveDeferredIssue(ctx, uid, tasks[0].ID, true); err != nil {
		t.Fatal(err)
	}
	var status string
	var attempts int
	if err := s.DB.QueryRowContext(ctx, `SELECT status,attempt_count FROM automation_pending_tasks WHERE id=?`, tasks[0].ID).Scan(&status, &attempts); err != nil {
		t.Fatal(err)
	}
	if status != "pending" || attempts != 0 {
		t.Fatalf("status=%s attempts=%d", status, attempts)
	}
}

func TestInvalidAutomationSnapshotCanOnlyBeCanceled(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	ruleID, err := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "", "buyer_reviewed", true, 100,
		AutomationActionInput{ActionType: "send_text", MessageTemplate: "x", Enabled: true}))
	if err != nil {
		t.Fatal(err)
	}
	runID, started, err := s.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cid,
		TriggerType: "buyer_reviewed", TriggerKey: "invalid-snapshot", RawEventJSON: `{}`, LeaseExpiresAt: 1})
	if err != nil || !started {
		t.Fatalf("start=%v err=%v", started, err)
	}
	if err := s.Automation.QuarantineRun(ctx, runID, 1, "历史运行数据无法安全解析，已移入人工检查"); err != nil {
		t.Fatal(err)
	}
	runs, _, err := s.Automation.ListIssues(ctx, uid)
	if err != nil || len(runs) != 1 {
		t.Fatalf("runs=%+v err=%v", runs, err)
	}
	if runs[0].IssueKind != "invalid_snapshot" || len(runs[0].AllowedResolutions) != 1 || runs[0].AllowedResolutions[0] != "cancel" {
		t.Fatalf("policy=%+v", runs[0])
	}
	if err := s.Automation.ResolveRunIssue(ctx, uid, runID, "retry"); err == nil {
		t.Fatal("invalid snapshot must reject retry")
	}
	run, _ := s.Automation.GetRun(ctx, runID)
	if run.Status != "needs_review" {
		t.Fatalf("rejected resolution changed run: %+v", run)
	}
	if err := s.Automation.ResolveRunIssue(ctx, uid, runID, "cancel"); err != nil {
		t.Fatal(err)
	}
}

func TestAutomationIssuePolicyForDisabledRuleRequiresReenableBeforeRetry(t *testing.T) {
	rawBytes, err := json.Marshal(struct{ AccountID string }{AccountID: "acc1"})
	if err != nil {
		t.Fatal(err)
	}
	raw := string(rawBytes)
	kind, allowed := automationIssuePolicy(raw, false, false, 0, "自动化规则不存在或已停用，无法恢复")
	if kind != "rule_unavailable" || containsString(allowed, "retry") || !containsString(allowed, "cancel") {
		t.Fatalf("disabled policy kind=%q allowed=%v", kind, allowed)
	}
	kind, allowed = automationIssuePolicy(raw, false, true, 0, "自动化规则不存在或已停用，无法恢复")
	if kind != "rule_unavailable" || !containsString(allowed, "retry") || containsString(allowed, "continue") {
		t.Fatalf("reenabled policy kind=%q allowed=%v", kind, allowed)
	}
}

func TestDeferTaskRevivesDeadLetterWithFreshAttemptBudget(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)
	task := DeferredAutomationTask{TaskKey: "same", CookieID: cid, TriggerType: "buyer_reviewed", TaskJSON: `{"v":1}`, DueAt: 0}
	if err := s.Automation.DeferTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET status='dead_letter',attempt_count=5 WHERE task_key='same'`)
	task.TaskJSON = `{"v":2}`
	if err := s.Automation.DeferTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	claimed, err := s.Automation.ClaimDueDeferredTasks(ctx, 10)
	if err != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%+v err=%v", claimed, err)
	}
}

func TestDeferredTaskFencingRejectsStaleWorker(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	_, cid := seedAccount(t, s)
	if err := s.Automation.DeferTask(ctx, DeferredAutomationTask{
		TaskKey: "fenced-task", CookieID: cid, TriggerType: "buyer_reviewed", TaskJSON: `{}`, DueAt: 0,
	}); err != nil {
		t.Fatal(err)
	}
	first, err := s.Automation.ClaimDueDeferredTasks(ctx, 1)
	if err != nil || len(first) != 1 {
		t.Fatalf("first claim=%+v err=%v", first, err)
	}
	if _, err := s.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET lease_expires_at=0 WHERE id=?`, first[0].ID); err != nil {
		t.Fatal(err)
	}
	second, err := s.Automation.ClaimDueDeferredTasks(ctx, 1)
	if err != nil || len(second) != 1 {
		t.Fatalf("second claim=%+v err=%v", second, err)
	}
	if second[0].ClaimVersion <= first[0].ClaimVersion {
		t.Fatalf("claim versions first=%d second=%d", first[0].ClaimVersion, second[0].ClaimVersion)
	}
	if err := s.Automation.FinishDeferredTask(ctx, first[0].ID, first[0].ClaimVersion, true, ""); !errors.Is(err, ErrDeferredTaskLeaseLost) {
		t.Fatalf("stale finish err=%v want ErrDeferredTaskLeaseLost", err)
	}
	if err := s.Automation.RenewDeferredTaskLease(ctx, second[0].ID, second[0].ClaimVersion, time.Now().Add(time.Minute).Unix()); err != nil {
		t.Fatalf("current renew: %v", err)
	}
	if err := s.Automation.FinishDeferredTask(ctx, second[0].ID, second[0].ClaimVersion, true, ""); err != nil {
		t.Fatalf("current finish: %v", err)
	}
}

func TestAutomationRuleDeleteRejectsActiveRun(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, cid := seedAccount(t, s)
	ruleID, _ := s.Automation.Create(ctx, makeAutomationRule(cid, uid, "", "buyer_reviewed", true, 100,
		AutomationActionInput{ActionType: "send_text", MessageTemplate: "x", Enabled: true}))
	_, _, _ = s.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cid, TriggerType: "buyer_reviewed",
		TriggerKey: "active", RawEventJSON: `{}`, LeaseExpiresAt: 1})
	if err := s.Automation.Delete(ctx, uid, ruleID); !errors.Is(err, ErrAutomationRunActive) {
		t.Fatalf("delete err=%v", err)
	}
}

// TestNullInt64 nullInt64 的 <=0 → nil。
func TestNullInt64(t *testing.T) {
	if nullInt64(0) != nil {
		t.Error("nullInt64(0) 应 nil")
	}
	if nullInt64(-1) != nil {
		t.Error("nullInt64(-1) 应 nil")
	}
	if nullInt64(5) != int64(5) {
		t.Error("nullInt64(5) 应 5")
	}
}
