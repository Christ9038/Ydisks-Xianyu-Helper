package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestValidOrdersMatchesAnalyticsScope(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()

	_, _ = store.DB.ExecContext(ctx, `
		INSERT INTO orders (order_id, item_id, buyer_id, quantity, amount, order_status, cookie_id, created_at) VALUES
		('ord-valid', 'item1', 'buyer1', '2', '¥12.50', 'pending_ship', 'acc1', '2026-06-28 10:00:00'),
		('ord-no-amount', 'item1', 'buyer2', '1', '', 'pending_ship', 'acc1', '2026-06-28 10:00:00'),
		('ord-bad-status', 'item1', 'buyer3', '1', '9.90', 'cancelled', 'acc1', '2026-06-28 10:00:00')
	`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_detail) VALUES ('acc1','item1','测试商品','{"pic_info":{"picUrl":"https://img.example/item.png"}}')`)

	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodGet, "/analytics/orders?start_date=2026-06-28&end_date=2026-06-28", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("analytics status=%d body=%s", rec.Code, rec.Body.String())
	}
	var analytics struct {
		RevenueStats struct {
			TotalOrders int     `json:"total_orders"`
			TotalAmount float64 `json:"total_amount"`
		} `json:"revenue_stats"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &analytics); err != nil {
		t.Fatal(err)
	}
	if analytics.RevenueStats.TotalOrders != 1 || analytics.RevenueStats.TotalAmount != 12.5 {
		t.Fatalf("统计口径异常: %+v", analytics.RevenueStats)
	}

	req2 := httptest.NewRequest(http.MethodGet, "/analytics/orders/valid?start_date=2026-06-28&end_date=2026-06-28", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("valid orders status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	var valid struct {
		Orders []map[string]any `json:"orders"`
	}
	if err := json.Unmarshal(rec2.Body.Bytes(), &valid); err != nil {
		t.Fatal(err)
	}
	if len(valid.Orders) != 1 {
		t.Fatalf("有效订单明细数量应与统计订单数一致，got %d body=%s", len(valid.Orders), rec2.Body.String())
	}
	order := valid.Orders[0]
	if order["order_id"] != "ord-valid" || order["item_title"] != "测试商品" || !strings.Contains(order["item_image"].(string), "img.example") {
		t.Fatalf("有效订单明细字段异常: %+v", order)
	}
	if order["status"] != "pending_ship" || order["order_status"] != "pending_ship" {
		t.Fatalf("状态字段异常: %+v", order)
	}
}
