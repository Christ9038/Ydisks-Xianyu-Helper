package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
)

// loginHelper 登录并返回会话 cookie。
func loginHelper(t *testing.T, h http.Handler) *http.Cookie {
	return loginAsHelper(t, h, "admin", "pw")
}

// loginAsHelper 负责登录AsHelper相关处理。
func loginAsHelper(t *testing.T, h http.Handler, username, password string) *http.Cookie {
	t.Helper()
	// body、err 保存body、err，供当前处理流程使用
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatal(err)
	}
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(string(body)))
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("login %q status=%d body=%s", username, rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()[0]
}

// TestOrderListAndDetail 订单列表 + 详情 + 状态码归一。
func TestOrderListAndDetail(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// 插入一条订单（order_status 用数字码 "2" 测试归一）。
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('ord1','item1','buyer1','2','acc1')`)
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_detail) VALUES ('acc1','item1','测试商品','{"pic_info":{"picUrl":"https://img.example/item.png"}}')`)

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 列表。
	req := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	// resp 保存resp，供当前处理流程使用
	var resp struct {
		Success bool             `json:"success"`
		Total   int              `json:"total"`
		Data    []map[string]any `json:"data"`
	}
	json.Unmarshal(rec.Body.Bytes(), &resp)
	if !resp.Success || resp.Total != 1 {
		t.Fatalf("订单列表异常: %+v", resp)
	}
	if resp.Data[0]["order_status"] != "pending_ship" {
		t.Errorf("状态归一: got %v want pending_ship", resp.Data[0]["order_status"])
	}
	if resp.Data[0]["item_title"] != "测试商品" {
		t.Errorf("item_title: %v", resp.Data[0]["item_title"])
	}
	if resp.Data[0]["item_image"] != "https://img.example/item.png" {
		t.Errorf("item_image: %v", resp.Data[0]["item_image"])
	}

	// 详情。
	req2 := httptest.NewRequest(http.MethodGet, "/api/orders/ord1", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("detail status=%d", rec2.Code)
	}
	// det 保存det，供当前处理流程使用
	var det map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &det)
	if det["order_id"] != "ord1" {
		t.Errorf("详情异常: %+v", det)
	}
	if det["item_image"] != "https://img.example/item.png" {
		t.Errorf("详情 item_image: %v", det["item_image"])
	}

	// 删除。
	req3 := httptest.NewRequest(http.MethodDelete, "/api/orders/ord1", nil)
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("delete status=%d", rec3.Code)
	}
	// deletedAt 保存deletedAt，供当前处理流程使用
	var deletedAt string
	if // err 保存err，供当前处理流程使用
	err := store.DB.QueryRowContext(ctx, `SELECT COALESCE(deleted_at,'') FROM orders WHERE order_id=?`, "ord1").Scan(&deletedAt); err != nil || deletedAt == "" {
		t.Fatalf("订单删除应为逻辑删除，deleted_at=%q err=%v", deletedAt, err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := store.Orders.Get(ctx, "ord1"); err == nil {
		t.Fatal("逻辑删除订单不应再出现在活动订单查询中")
	}
}

// TestCardCRUD 卡券增删改查。
func TestCardCRUD(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 创建。
	body := `{"name":"测试卡","type":"text","text_content":"卡密ABC","enabled":true}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/cards", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	// cr 保存cr，供当前处理流程使用
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	// id 保存标识，供当前处理流程使用
	id := cr["id"].(float64)
	if id == 0 {
		t.Fatal("应返回 id")
	}

	// 列表。
	req2 := httptest.NewRequest(http.MethodGet, "/cards", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	// arr 保存arr，供当前处理流程使用
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["name"] != "测试卡" {
		t.Fatalf("列表异常: %+v", arr)
	}

	// 更新。
	updBody := `{"name":"改名卡","type":"text","text_content":"卡密XYZ","enabled":true}`
	// req3 保存req3，供当前处理流程使用
	req3 := httptest.NewRequest(http.MethodPut, "/cards/"+itoa(int64(id)), strings.NewReader(updBody))
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("update status=%d", rec3.Code)
	}

	// 获取验证改名。
	req4 := httptest.NewRequest(http.MethodGet, "/cards/"+itoa(int64(id)), nil)
	req4.AddCookie(cookie)
	// rec4 保存rec4，供当前处理流程使用
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	// got 保存got，供当前处理流程使用
	var got map[string]any
	json.Unmarshal(rec4.Body.Bytes(), &got)
	if got["name"] != "改名卡" {
		t.Errorf("改名后应=改名卡, got %v", got["name"])
	}

	// 删除。
	req5 := httptest.NewRequest(http.MethodDelete, "/cards/"+itoa(int64(id)), nil)
	req5.AddCookie(cookie)
	// rec5 保存rec5，供当前处理流程使用
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, req5)
	if rec5.Code != 200 {
		t.Fatalf("delete status=%d", rec5.Code)
	}
}

// itoa 负责itoa相关处理。
func itoa(n int64) string { return strconv.FormatInt(n, 10) }
