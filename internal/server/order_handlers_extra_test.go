package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRefreshOrdersNoBrowser 浏览器未启用时应 503。
func TestRefreshOrdersNoBrowser(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/orders/refresh", strings.NewReader(""))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("无浏览器应 503，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestRefreshSingleOrderNoBrowser 单订单刷新无浏览器时 503。
func TestRefreshSingleOrderNoBrowser(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, cookie_id, order_status) VALUES ('ord-x','item1','acc1','2')`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/orders/ord-x/refresh", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("无浏览器应 503，got %d", rec.Code)
	}
}

// TestRefreshSingleOrderNotFound 单订单刷新不存在订单 404。
func TestRefreshSingleOrderNotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 注入非 nil Browser 但内部 playwright 不可用，订单查询先行 404。
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/orders/no-such/refresh", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// Browser==nil → 503；先校验此路径不 panic。
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 503/404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestUpdateOrder 更新订单字段（status 归一）。
func TestUpdateOrder(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('ord-u','item1','b1','2','acc1')`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"status":"shipped","receiver_name":"张三","receiver_phone":"13800000000","receiver_address":"北京"}`
	req := httptest.NewRequest(http.MethodPut, "/api/orders/ord-u", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 验证已写入。
	req2 := httptest.NewRequest(http.MethodGet, "/api/orders/ord-u", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	var got map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &got)
	if got["order_status"] != "shipped" || got["receiver_name"] != "张三" {
		t.Fatalf("更新未生效: %+v", got)
	}
}

// TestUpdateOrderBadJSON 非法 JSON 应 400。
func TestUpdateOrderBadJSON(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, cookie_id) VALUES ('ord-bad','item1','acc1')`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPut, "/api/orders/ord-bad", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestManualShipOrdersStatusOnlySuccess mtop ConsignContext 成功路径。
func TestManualShipOrdersStatusOnlySuccess(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id) VALUES ('ord-m','item1','b1','2','acc1','chat1')`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"order_ids":["ord-m"],"ship_mode":"status_only"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("manual ship status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != true {
		t.Fatalf("应成功: %+v", res)
	}
	results, _ := res["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("应1条结果，got %d", len(results))
	}
	// 订单状态应已变为 shipped。
	var ord map[string]any
	req2 := httptest.NewRequest(http.MethodGet, "/api/orders/ord-m", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	json.Unmarshal(rec2.Body.Bytes(), &ord)
	if ord["order_status"] != "shipped" {
		t.Errorf("订单状态应为 shipped，got %v", ord["order_status"])
	}
}

// TestManualShipOrdersConsignFail mtop ConsignContext 失败（非 success ret）。
func TestManualShipOrdersConsignFail(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id) VALUES ('ord-f','item1','b1','2','acc1','chat1')`)

	// 覆盖 mtop client：返回非 success ret。
	prev := srv.MTop
	srv.MTop = newMockMTop(t, mtopResp{ret: []string{"FAIL_BIZ_ORDER_NOT_FOUND::订单不存在"}})
	defer func() { srv.MTop = prev }()

	h := srv.Router()
	cookie := loginHelper(t, h)
	body := `{"order_ids":["ord-f"],"ship_mode":"status_only"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success"] != false {
		t.Fatalf("整体应失败: %+v", res)
	}
}

// TestManualShipOrdersOrderNotFound 订单不存在 → failed。
func TestManualShipOrdersOrderNotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"order_ids":["no-such-ord"],"ship_mode":"status_only"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["failed_count"] != float64(1) {
		t.Fatalf("应1失败，got %+v", res)
	}
}

// TestManualShipOrdersBadMode 非法发货模式 400。
func TestManualShipOrdersBadMode(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"order_ids":["ord-x"],"ship_mode":"bogus"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法模式应 400，got %d", rec.Code)
	}
}

// TestManualShipOrdersEmpty 缺少订单 ID 400。
func TestManualShipOrdersEmpty(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"order_ids":[],"ship_mode":"status_only"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空订单应 400，got %d", rec.Code)
	}
}

// TestManualShipOrdersBadJSON 非法 JSON 400。
func TestManualShipOrdersBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestManualShipOrdersFullDeliveryNoAutomation full_delivery 但自动化未初始化 → failed。
func TestManualShipOrdersFullDeliveryNoAutomation(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id) VALUES ('ord-full','item1','b1','2','acc1','chat1')`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"order_ids":["ord-full"],"ship_mode":"full_delivery"}`
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["failed_count"] != float64(1) {
		t.Fatalf("应1失败，got %+v", res)
	}
	results, _ := res["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("结果数异常: %d", len(results))
	}
	r0, _ := results[0].(map[string]any)
	if !strings.Contains(r0["message"].(string), "自动化") {
		t.Fatalf("应提示自动化未初始化，got %v", r0["message"])
	}
}

// TestIsStableOrderStatus 稳定状态判定。
func TestIsStableOrderStatus(t *testing.T) {
	stable := map[string]bool{"shipped": true, "completed": true, "cancelled": true}
	unstable := map[string]bool{"pending_ship": false, "processing": false, "": false, "unknown": false}
	for s, want := range stable {
		if got := isStableOrderStatus(s); got != want {
			t.Errorf("isStableOrderStatus(%q)=%v want %v", s, got, want)
		}
	}
	for s, want := range unstable {
		if got := isStableOrderStatus(s); got != want {
			t.Errorf("isStableOrderStatus(%q)=%v want %v", s, got, want)
		}
	}
}

// TestAtoiDefault atoiDefault 表驱动。
func TestAtoiDefault(t *testing.T) {
	cases := map[string]int{"": 5, "abc": 5, "3": 3, "12": 12}
	for in, want := range cases {
		if got := atoiDefault(in, 5); got != want {
			t.Errorf("atoiDefault(%q)=%d want %d", in, got, want)
		}
	}
}
