package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"xianyu-go/internal/db"
)

// TestAutomationAuditEventsEndpoint 验证规则日志分页、过滤、运行时间线、参数校验、用户隔离和未登录拒绝，成功响应符合 OpenAPI。
func TestAutomationAuditEventsEndpoint(t *testing.T) {
	// fixture、cleanup 是隔离的测试夹具和清理函数。
	fixture, cleanup := newSaleFixture(t)
	defer cleanup()
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// admin、other 是两个互相隔离的用户。
	admin, _ := fixture.store.Users.GetByUsername(ctx, "admin")
	// other 是另一个用户，用于验证数据隔离。
	other, _ := fixture.store.Users.GetByUsername(ctx, "other")
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := fixture.store.Cookies.Save(ctx, "acc-audit", "unb=1;", admin.ID); err != nil {
		t.Fatal(err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := fixture.store.Cookies.Save(ctx, "acc-other", "unb=2;", other.ID); err != nil {
		t.Fatal(err)
	}
	// events 覆盖运行时间线与跳过事件；最后一条属于另一用户，必须不可见。
	events := []db.AutomationAuditEvent{
		{CookieID: "acc-audit", OrderID: "order-1", RunID: 9, TriggerType: "order_paid", EventType: "run_started", Status: "running", CreatedAt: 10},
		{CookieID: "acc-audit", OrderID: "order-1", RunID: 9, TriggerType: "order_paid", EventType: "action_succeeded", Status: "success", ActionType: "send_card", Summary: "发送卡密「组A」×1", CreatedAt: 11},
		{CookieID: "acc-audit", OrderID: "order-2", TriggerType: "order_paid", EventType: "skipped", Status: "skipped", Reason: "账号未开启自动发货", CreatedAt: 12},
		{CookieID: "acc-other", OrderID: "order-x", EventType: "skipped", Status: "skipped", CreatedAt: 13},
	}
	// event 是当前遍历的事件。
	for _, event := range events {
		// err 保存被测调用返回的错误，用于断言成功或期望的失败。
		if err := fixture.store.Automation.AppendAuditEvent(ctx, event); err != nil {
			t.Fatal(err)
		}
	}
	// base 是规则日志接口路径。
	base := "/api/v1/automation-audit-events"

	// recorder 记录被测处理器写出的响应，用于断言状态码和正文。；request 是发往被测处理器的模拟 HTTP 请求。
	recorder, request := fixture.get(base)
	assertOpenAPISuccessResponse(t, request, recorder)
	// list 保存解析出的列表响应。
	var list automationAuditEventListResponse
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if !list.Success || list.Total != 3 || len(list.Data) != 3 || list.Page != 1 || list.PageSize != 20 || list.TotalPages != 1 {
		t.Fatalf("list=%+v", list)
	}
	if list.Data[0].Status != "skipped" || list.Data[0].Reason != "账号未开启自动发货" {
		t.Fatalf("应按时间倒序且携带原因: %+v", list.Data)
	}

	recorder, request = fixture.get(base + "?status=skipped&page_size=1")
	assertOpenAPISuccessResponse(t, request, recorder)
	_ = json.Unmarshal(recorder.Body.Bytes(), &list)
	if list.Total != 1 || len(list.Data) != 1 || list.PageSize != 1 {
		t.Fatalf("状态过滤: %+v", list)
	}

	recorder, request = fixture.get(base + "?run_id=9")
	assertOpenAPISuccessResponse(t, request, recorder)
	_ = json.Unmarshal(recorder.Body.Bytes(), &list)
	if list.Total != 2 || list.Data[0].EventType != "run_started" || list.Data[1].Summary != "发送卡密「组A」×1" {
		t.Fatalf("运行时间线应正序: %+v", list.Data)
	}

	recorder, request = fixture.get(base + "?search=order-2&cookie_id=acc-audit&trigger_type=order_paid")
	assertOpenAPISuccessResponse(t, request, recorder)
	_ = json.Unmarshal(recorder.Body.Bytes(), &list)
	if list.Total != 1 || list.Data[0].OrderID != "order-2" {
		t.Fatalf("订单搜索: %+v", list)
	}

	recorder, request = fixture.get(base + "?page=5")
	assertOpenAPISuccessResponse(t, request, recorder)
	_ = json.Unmarshal(recorder.Body.Bytes(), &list)
	if list.Total != 3 || len(list.Data) != 0 || list.Page != 5 {
		t.Fatalf("越界页应返回空数组: %+v", list)
	}

	recorder, _ = fixture.get(base + "?status=bogus")
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("未知状态应返回 400, got %d", recorder.Code)
	}

	// anonymous 是未携带会话的请求，必须被认证中间件拒绝。
	anonymous := httptest.NewRecorder()
	fixture.handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, base, nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应返回 401, got %d", anonymous.Code)
	}
}
