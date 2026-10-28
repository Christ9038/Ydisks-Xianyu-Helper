package db

import (
	"context"
	"errors"
	"testing"
)

// TestAutomationAudit_AppendDerivesOwnerAndRejectsUnknownAccount 验证所有者由账号推导，未知账号返回 ErrNotFound。
func TestAutomationAudit_AppendDerivesOwnerAndRejectsUnknownAccount(t *testing.T) {
	// s 是测试数据库；cleanup 在用例结束时释放它。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// uid、cid 是测试账号所属用户和账号标识。
	uid, cid := seedAccount(t, s)

	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := s.Automation.AppendAuditEvent(ctx, AutomationAuditEvent{CookieID: cid, OrderID: "o1", EventType: "skipped", Status: AuditStatusSkipped, Reason: "账号未开启自动发货", CreatedAt: 100}); err != nil {
		t.Fatal(err)
	}
	// page 保存按所有者读取的第一页事件。
	page, err := s.Automation.ListAuditEvents(ctx, AutomationAuditQuery{UserID: uid})
	if err != nil || page.Total != 1 || len(page.Events) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := page.Events[0]; got.UserID != uid || got.CookieID != cid || got.Reason != "账号未开启自动发货" {
		t.Fatalf("事件内容不符: %+v", got)
	}
	// missingErr 是账号不存在时的追加错误。
	missingErr := s.Automation.AppendAuditEvent(ctx, AutomationAuditEvent{CookieID: "missing", EventType: "skipped", Status: AuditStatusSkipped, CreatedAt: 100})
	if !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("未知账号应返回 ErrNotFound, got %v", missingErr)
	}
}

// TestAutomationAudit_ListFiltersIsolationAndPaging 验证用户隔离、各维度过滤、订单号转义搜索、倒序与分页。
func TestAutomationAudit_ListFiltersIsolationAndPaging(t *testing.T) {
	// s 是测试数据库；cleanup 在用例结束时释放它。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// uid、cid 是测试账号所属用户和账号标识。
	uid, cid := seedAccount(t, s)
	// events 是依次写入的事件，覆盖不同状态、触发类型和运行。
	events := []AutomationAuditEvent{
		{CookieID: cid, OrderID: "o_1", RunID: 5, TriggerType: "order_paid", EventType: "run_started", Status: AuditStatusRunning, CreatedAt: 10},
		{CookieID: cid, OrderID: "o_1", RunID: 5, TriggerType: "order_paid", EventType: "run_finished", Status: AuditStatusSuccess, CreatedAt: 11},
		{CookieID: cid, OrderID: "oX1", TriggerType: "buyer_reviewed", EventType: "skipped", Status: AuditStatusSkipped, CreatedAt: 12},
	}
	// event 是当前遍历的事件。
	for _, event := range events {
		// err 保存被测调用返回的错误，用于断言成功或期望的失败。
		if err := s.Automation.AppendAuditEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}

	// cases 描述每组过滤条件及期望命中数量。
	cases := []struct {
		name  string
		query AutomationAuditQuery
		want  int64
	}{
		{"其他用户看不到", AutomationAuditQuery{UserID: uid + 99}, 0},
		{"状态过滤", AutomationAuditQuery{UserID: uid, Status: AuditStatusSkipped}, 1},
		{"触发类型过滤", AutomationAuditQuery{UserID: uid, TriggerType: "order_paid"}, 2},
		{"运行过滤", AutomationAuditQuery{UserID: uid, RunID: 5}, 2},
		{"账号过滤", AutomationAuditQuery{UserID: uid, CookieID: cid}, 3},
		{"下划线按字面匹配", AutomationAuditQuery{UserID: uid, Search: "o_1"}, 2},
	}
	// tc 是当前遍历的表驱动用例，包含输入与期望结果。
	for _, tc := range cases {
		// page、err 保存该过滤条件的查询结果。
		page, err := s.Automation.ListAuditEvents(ctx, tc.query)
		if err != nil || page.Total != tc.want || int64(len(page.Events)) != tc.want {
			t.Fatalf("%s: total=%d len=%d err=%v", tc.name, page.Total, len(page.Events), err)
		}
	}

	// desc 是默认倒序的第一页（每页 2 条）。
	desc, err := s.Automation.ListAuditEvents(ctx, AutomationAuditQuery{UserID: uid, Limit: 2})
	if err != nil || len(desc.Events) != 2 || desc.Total != 3 || desc.Events[0].CreatedAt != 12 {
		t.Fatalf("倒序分页不符: %+v err=%v", desc, err)
	}
	// asc 是运行时间线正序，偏移 1 跳过第一条。
	asc, err := s.Automation.ListAuditEvents(ctx, AutomationAuditQuery{UserID: uid, RunID: 5, Ascending: true, Offset: 1})
	if err != nil || len(asc.Events) != 1 || asc.Events[0].EventType != "run_finished" {
		t.Fatalf("正序偏移不符: %+v err=%v", asc, err)
	}
	// clamped 验证超大分页被限制、负偏移按零处理且不报错。
	if clamped, clampErr := s.Automation.ListAuditEvents(ctx, AutomationAuditQuery{UserID: uid, Limit: 9999, Offset: -5}); clampErr != nil || len(clamped.Events) != 3 {
		t.Fatalf("clamped=%+v err=%v", clamped, clampErr)
	}
}

// TestAutomationAudit_PruneRemovesOnlyExpired 验证保留期清理只删除早于截止时间的事件。
func TestAutomationAudit_PruneRemovesOnlyExpired(t *testing.T) {
	// s 是测试数据库；cleanup 在用例结束时释放它。
	s, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// uid、cid 是测试账号所属用户和账号标识。
	uid, cid := seedAccount(t, s)
	// at 是当前遍历的事件发生时刻（Unix 秒）。
	for _, at := range []int64{10, 20, 30} {
		// err 保存被测调用返回的错误，用于断言成功或期望的失败。
		if err := s.Automation.AppendAuditEvent(ctx, AutomationAuditEvent{CookieID: cid, EventType: "skipped", Status: AuditStatusSkipped, CreatedAt: at}); err != nil {
			t.Fatal(err)
		}
	}
	// removed 是被删除的事件数。
	removed, err := s.Automation.PruneAuditEvents(ctx, 25)
	if err != nil || removed != 2 {
		t.Fatalf("removed=%d err=%v", removed, err)
	}
	// page 是清理后剩余事件。
	page, err := s.Automation.ListAuditEvents(ctx, AutomationAuditQuery{UserID: uid})
	if err != nil || page.Total != 1 || page.Events[0].CreatedAt != 30 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
}
