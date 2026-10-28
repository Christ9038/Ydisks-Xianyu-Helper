package adapter

import (
	"context"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/db"
)

// TestAutomationRepositoryListAuditEventsMapsAndPropagatesErrors 验证数据库事件被转换为应用层事件，数据库不可用时错误透传。
func TestAutomationRepositoryListAuditEventsMapsAndPropagatesErrors(t *testing.T) {
	// store 是当前适配器测试使用的 SQLite 存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := store.Users.Create(ctx, "audit-owner", "audit@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// owner 是测试账号所有者，userID 是其用户标识。
	owner, err := store.Users.GetByUsername(ctx, "audit-owner")
	if err != nil {
		t.Fatal(err)
	}
	// userID 是测试账号所有者的用户标识。
	userID := owner.ID
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := store.Cookies.Save(ctx, "acc-audit", "unb=1;", userID); err != nil {
		t.Fatal(err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := store.Automation.AppendAuditEvent(ctx, db.AutomationAuditEvent{
		CookieID: "acc-audit", OrderID: "o1", RunID: 3, RuleID: 4, TriggerType: "order_paid", Source: "ws",
		EventType: "run_finished", Status: "success", ActionType: "send_card", Summary: "摘要", Reason: "原因", CreatedAt: 99,
	}); err != nil {
		t.Fatal(err)
	}
	// repository 是绑定 SQLite 存储的自动化适配器。
	repository := NewAutomationRepository(store)
	// page 是转换后的一页事件。
	page, err := repository.ListAuditEvents(ctx, automationapp.AuditQuery{UserID: userID, Limit: 10})
	if err != nil || page.Total != 1 || len(page.Events) != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got := page.Events[0]; got.CookieID != "acc-audit" || got.RunID != 3 || got.RuleID != 4 || got.Summary != "摘要" || got.CreatedAt != 99 {
		t.Fatalf("事件转换不符: %+v", got)
	}
	// empty 保存无记录用户的查询结果，用于断言返回空且非 nil 的切片。；emptyErr 保存无记录查询返回的错误。
	if empty, emptyErr := repository.ListAuditEvents(ctx, automationapp.AuditQuery{UserID: userID + 1, Limit: 10}); emptyErr != nil || empty.Events == nil || len(empty.Events) != 0 {
		t.Fatalf("无记录应返回空非 nil 切片: %+v err=%v", empty, emptyErr)
	}
	// closeErr 保存提前关闭测试数据库时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// listErr 保存数据库关闭后查询返回的错误，应当被透传。
	if _, listErr := repository.ListAuditEvents(ctx, automationapp.AuditQuery{UserID: userID}); listErr == nil {
		t.Fatal("数据库关闭后应返回错误")
	}
}
