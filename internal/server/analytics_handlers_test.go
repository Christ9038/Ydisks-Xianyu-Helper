package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestValidOrdersMatchesAnalyticsScope 负责Test有效订单列表MatchesAnalyticsScope相关处理。
func TestValidOrdersMatchesAnalyticsScope(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	_, _ = store.DB.ExecContext(ctx, `
		INSERT INTO orders (order_id, item_id, buyer_id, quantity, amount, order_status, cookie_id, created_at) VALUES
		('ord-valid', 'item1', 'buyer1', '2', '¥12.50', 'pending_ship', 'acc1', '2026-06-28 10:00:00'),
		('ord-no-amount', 'item1', 'buyer2', '1', '', 'pending_ship', 'acc1', '2026-06-28 10:00:00'),
		('ord-bad-status', 'item1', 'buyer3', '1', '9.90', 'cancelled', 'acc1', '2026-06-28 10:00:00')
	`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_detail) VALUES ('acc1','item1','测试商品','{"pic_info":{"picUrl":"https://img.example/item.png"}}')`)

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/analytics/orders?start_date=2026-06-28&end_date=2026-06-28", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("analytics status=%d body=%s", rec.Code, rec.Body.String())
	}
	// analytics 保存analytics，供当前处理流程使用
	var analytics struct {
		RevenueStats struct {
			TotalOrders int     `json:"total_orders"`
			TotalAmount float64 `json:"total_amount"`
		} `json:"revenue_stats"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &analytics); err != nil {
		t.Fatal(err)
	}
	if analytics.RevenueStats.TotalOrders != 1 || analytics.RevenueStats.TotalAmount != 12.5 {
		t.Fatalf("统计口径异常: %+v", analytics.RevenueStats)
	}

	// req2 保存req2，供当前处理流程使用
	req2 := httptest.NewRequest(http.MethodGet, "/analytics/orders/valid?start_date=2026-06-28&end_date=2026-06-28", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("valid orders status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	// valid 保存有效，供当前处理流程使用
	var valid struct {
		Orders []map[string]any `json:"orders"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec2.Body.Bytes(), &valid); err != nil {
		t.Fatal(err)
	}
	if len(valid.Orders) != 1 {
		t.Fatalf("有效订单明细数量应与统计订单数一致，got %d body=%s", len(valid.Orders), rec2.Body.String())
	}
	// order 保存订单，供当前处理流程使用
	order := valid.Orders[0]
	if order["order_id"] != "ord-valid" || order["item_title"] != "测试商品" || !strings.Contains(order["item_image"].(string), "img.example") {
		t.Fatalf("有效订单明细字段异常: %+v", order)
	}
	if order["status"] != "pending_ship" || order["order_status"] != "pending_ship" {
		t.Fatalf("状态字段异常: %+v", order)
	}
}

// TestValidOrdersIncludesPaidAndReportsPagination 负责Test有效订单列表IncludesPaidAndReportsPagination相关处理。
func TestValidOrdersIncludesPaidAndReportsPagination(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,amount,order_status,cookie_id,created_at) VALUES
		('paid-1','10','paid','acc1','2026-06-28 10:00:00'),
		('paid-2','20','pending_ship','acc1','2026-06-28 11:00:00')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/analytics/orders/valid?start_date=2026-06-28&end_date=2026-06-28&page_size=1", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// result 保存结果，供当前处理流程使用
	var result struct {
		Orders    []map[string]any `json:"orders"`
		Total     int              `json:"total"`
		Truncated bool             `json:"truncated"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if len(result.Orders) != 1 || result.Total != 2 || !result.Truncated {
		t.Fatalf("result=%+v", result)
	}
}

// TestDashboardStatsAreAvailableAndScopedToCurrentUser 负责TestDashboardStatsAreAvailableAndScopedToCurrent用户相关处理。
func TestDashboardStatsAreAvailableAndScopedToCurrentUser(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()

	if // ok、err 保存ok、err，供当前处理流程使用
	ok, err := store.Users.Create(ctx, "member", "member@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create member: ok=%v err=%v", ok, err)
	}
	// member、err 保存member、err，供当前处理流程使用
	member, err := store.Users.GetByUsername(ctx, "member")
	if err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.Save(ctx, "member-acc", "unb=456", member.ID); err != nil {
		t.Fatal(err)
	}
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (name,type,data_content,enabled,user_id) VALUES ('member-card','data',?,1,?)`, "CARD-1\n\nCARD-2\n", member.ID)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO keywords (cookie_id,keyword,reply) VALUES ('member-acc','hi','hello')`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,cookie_id,order_status) VALUES ('member-order','member-acc','completed')`)

	// 管理员资源不能进入 member 的统计。
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (name,type,user_id) VALUES ('admin-card','text',1)`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,cookie_id,order_status) VALUES ('admin-order','acc1','completed')`)

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// loginReq 保存登录Req，供当前处理流程使用
	loginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"member","password":"pw"}`))
	// loginRec 保存登录Rec，供当前处理流程使用
	loginRec := httptest.NewRecorder()
	h.ServeHTTP(loginRec, loginReq)
	if loginRec.Code != http.StatusOK || len(loginRec.Result().Cookies()) == 0 {
		t.Fatalf("login status=%d body=%s", loginRec.Code, loginRec.Body.String())
	}

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/dashboard/stats", nil)
	req.AddCookie(loginRec.Result().Cookies()[0])
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// stats 保存stats，供当前处理流程使用
	var stats map[string]int64
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	// key 表示当前遍历过程中的key
	for _, key := range []string{"total_cookies", "active_cookies", "total_cards", "total_keywords", "total_orders"} {
		if stats[key] != 1 {
			t.Fatalf("%s=%d want 1; stats=%+v", key, stats[key], stats)
		}
	}
	if stats["available_card_stock"] != 2 {
		t.Fatalf("available_card_stock=%d want 2; stats=%+v", stats["available_card_stock"], stats)
	}

	// adminReq 保存adminReq，供当前处理流程使用
	adminReq := httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	adminReq.AddCookie(loginRec.Result().Cookies()[0])
	// adminRec 保存adminRec，供当前处理流程使用
	adminRec := httptest.NewRecorder()
	h.ServeHTTP(adminRec, adminReq)
	if adminRec.Code != http.StatusForbidden {
		t.Fatalf("member admin stats status=%d want 403", adminRec.Code)
	}
}

// TestAnalyticsIncludesLegacyNumericValidStatuses 负责TestAnalyticsIncludesLegacyNumeric有效Statuses相关处理。
func TestAnalyticsIncludesLegacyNumericValidStatuses(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,amount,order_status,cookie_id,created_at)
		VALUES ('legacy-shipped','8.50','3','acc1','2026-06-28 12:00:00')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/analytics/orders?start_date=2026-06-28&end_date=2026-06-28", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 保存响应，供当前处理流程使用
	var response struct {
		RevenueStats struct {
			TotalOrders int `json:"total_orders"`
		} `json:"revenue_stats"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.RevenueStats.TotalOrders != 1 {
		t.Fatalf("legacy numeric valid status was excluded: %+v", response)
	}
	// validReq 保存有效Req，供当前处理流程使用
	validReq := httptest.NewRequest(http.MethodGet, "/analytics/orders/valid?start_date=2026-06-28&end_date=2026-06-28", nil)
	validReq.AddCookie(cookie)
	// validRec 保存有效Rec，供当前处理流程使用
	validRec := httptest.NewRecorder()
	h.ServeHTTP(validRec, validReq)
	if !strings.Contains(validRec.Body.String(), `"order_status":"shipped"`) {
		t.Fatalf("legacy detail status was not normalized: %s", validRec.Body.String())
	}
}

// TestAnalyticsCustomRangeDoesNotSilentlyDropDays 负责TestAnalyticsCustomRangeDoesNotSilentlyDropDays相关处理。
func TestAnalyticsCustomRangeDoesNotSilentlyDropDays(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	for // day 保存day，供当前处理流程使用
	day := 1; day <= 31; day++ {
		// date 保存日期，供当前处理流程使用
		date := time.Date(2026, 1, day, 12, 0, 0, 0, time.UTC).Format("2006-01-02 15:04:05")
		_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,amount,order_status,cookie_id,created_at) VALUES (?,?,?,?,?)`, fmt.Sprintf("day-%02d", day), "1", "completed", "acc1", date)
	}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/analytics/orders?start_date=2026-01-01&end_date=2026-01-31", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// response 保存响应，供当前处理流程使用
	var response struct {
		Daily []map[string]any `json:"daily_stats"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil || len(response.Daily) != 31 {
		t.Fatalf("daily range len=%d err=%v body=%s", len(response.Daily), err, rec.Body.String())
	}
}

// TestAnalyticsDateBoundaryConvertsLocalDayToUTC 负责TestAnalytics日期BoundaryConvertsLocalDayToUTC相关处理。
func TestAnalyticsDateBoundaryConvertsLocalDayToUTC(t *testing.T) {
	// previous 保存previous，供当前处理流程使用
	previous := time.Local
	time.Local = time.FixedZone("UTC+8", 8*60*60)
	defer func() { time.Local = previous }()
	if // got 保存got，供当前处理流程使用
	got := analyticsDateBoundary("2026-06-28", false, time.Local); got != "2026-06-27 16:00:00" {
		t.Fatalf("start boundary=%q", got)
	}
	if // got 保存got，供当前处理流程使用
	got := analyticsDateBoundary("2026-06-28", true, time.Local); got != "2026-06-28 16:00:00" {
		t.Fatalf("end boundary=%q", got)
	}
}

// TestAnalyticsUsesBrowserTimezoneAndSkipsInvalidAmounts 负责TestAnalyticsUses浏览器TimezoneAndSkipsInvalidAmounts相关处理。
func TestAnalyticsUsesBrowserTimezoneAndSkipsInvalidAmounts(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,amount,order_status,cookie_id,created_at) VALUES
		('tz-valid','10.50','completed','acc1','2026-06-27 16:30:00'),
		('tz-invalid','abc','completed','acc1','2026-06-27 17:00:00')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/analytics/orders?start_date=2026-06-28&end_date=2026-06-28&timezone_offset_minutes=480", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 保存响应，供当前处理流程使用
	var response struct {
		Revenue struct {
			TotalOrders int     `json:"total_orders"`
			TotalAmount float64 `json:"total_amount"`
		} `json:"revenue_stats"`
		Daily []map[string]any `json:"daily_stats"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Revenue.TotalOrders != 1 || response.Revenue.TotalAmount != 10.5 || len(response.Daily) != 1 || response.Daily[0]["date"] != "2026-06-28" {
		t.Fatalf("response=%+v body=%s", response, rec.Body.String())
	}
}
