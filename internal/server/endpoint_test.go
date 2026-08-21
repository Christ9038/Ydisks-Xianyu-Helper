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

// loginAsHelper 封装登录AsHelper业务协调。
func loginAsHelper(t *testing.T, h http.Handler, username, password string) *http.Cookie {
	t.Helper()
	// body、err 用于本次流程后续判断的body、err
	body, err := json.Marshal(map[string]string{"username": username, "password": password})
	if err != nil {
		t.Fatal(err)
	}
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(string(body)))
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || len(rec.Result().Cookies()) == 0 {
		t.Fatalf("login %q status=%d body=%s", username, rec.Code, rec.Body.String())
	}
	return rec.Result().Cookies()[0]
}

// TestOrderListAndDetail 订单列表 + 详情 + 状态码归一。
func TestOrderListAndDetail(t *testing.T) {
	// srv、store、cleanup 用于本次流程后续判断的srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// 插入一条订单（order_status 用数字码 "2" 测试归一）。
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('ord1','item1','buyer1','2','acc1')`)
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_detail) VALUES ('acc1','item1','测试商品','{"pic_info":{"picUrl":"https://img.example/item.png"}}')`)

	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 列表。
	req := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
	// resp 用于本次流程后续判断的resp
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
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("detail status=%d", rec2.Code)
	}
	// det 用于本次流程后续判断的det
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
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("delete status=%d", rec3.Code)
	}
	// deletedAt 用于本次流程后续判断的deletedAt
	var deletedAt string
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT COALESCE(deleted_at,'') FROM orders WHERE order_id=?`, "ord1").Scan(&deletedAt); err != nil || deletedAt == "" {
		t.Fatalf("订单删除应为逻辑删除，deleted_at=%q err=%v", deletedAt, err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.Orders.Get(ctx, "ord1"); err == nil {
		t.Fatal("逻辑删除订单不应再出现在活动订单查询中")
	}
}

// TestCardCRUD 卡券增删改查。
func TestCardCRUD(t *testing.T) {
	// srv、cleanup 用于本次流程后续判断的srv、cleanup
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 用于本次流程后续判断的h
	h := srv.Router()
	// cookie 用于本次流程后续判断的登录凭证
	cookie := loginHelper(t, h)

	// 创建。
	body := `{"name":"测试卡","type":"text","text_content":"卡密ABC","enabled":true}`
	// req 用于本次流程后续判断的req
	req := httptest.NewRequest(http.MethodPost, "/cards", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 用于本次流程后续判断的rec
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	// cr 用于本次流程后续判断的cr
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	// id 用于本次流程后续判断的标识
	id := cr["id"].(float64)
	if id == 0 {
		t.Fatal("应返回 id")
	}

	// 列表。
	req2 := httptest.NewRequest(http.MethodGet, "/cards", nil)
	req2.AddCookie(cookie)
	// rec2 用于本次流程后续判断的rec2
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	// arr 用于本次流程后续判断的arr
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["name"] != "测试卡" {
		t.Fatalf("列表异常: %+v", arr)
	}

	// 更新。
	updBody := `{"name":"改名卡","type":"text","text_content":"卡密XYZ","enabled":true}`
	// req3 用于本次流程后续判断的req3
	req3 := httptest.NewRequest(http.MethodPut, "/cards/"+itoa(int64(id)), strings.NewReader(updBody))
	req3.AddCookie(cookie)
	// rec3 用于本次流程后续判断的rec3
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("update status=%d", rec3.Code)
	}

	// 获取验证改名。
	req4 := httptest.NewRequest(http.MethodGet, "/cards/"+itoa(int64(id)), nil)
	req4.AddCookie(cookie)
	// rec4 用于本次流程后续判断的rec4
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	// got 用于本次流程后续判断的got
	var got map[string]any
	json.Unmarshal(rec4.Body.Bytes(), &got)
	if got["name"] != "改名卡" {
		t.Errorf("改名后应=改名卡, got %v", got["name"])
	}

	// 删除。
	req5 := httptest.NewRequest(http.MethodDelete, "/cards/"+itoa(int64(id)), nil)
	req5.AddCookie(cookie)
	// rec5 用于本次流程后续判断的rec5
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, req5)
	if rec5.Code != 200 {
		t.Fatalf("delete status=%d", rec5.Code)
	}
}

// TestCardAPIDTOIsRedacted 验证 API 卡券支持具名配置请求，响应只返回脱敏摘要而不回显模板秘密。
func TestCardAPIDTOIsRedacted(t *testing.T) {
	// srv、cleanup 保存契约测试服务器及其清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是本次测试使用的完整路由处理器。
	handler := srv.Router()
	// cookie 是已认证管理员会话。
	cookie := loginHelper(t, handler)
	// request 是提交请求头和参数模板的新版 API 卡请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/cards", strings.NewReader(`{"name":"API 脱敏卡","type":"api","api_config":{"url":"https://example.com/card","method":"POST","timeout_seconds":10,"headers":{"Authorization":"Bearer super-secret"},"params":{"code":"{order_id}"},"response_path":"data.card.code"},"enabled":true}`))
	request.AddCookie(cookie)
	// response 保存创建请求的 HTTP 响应。
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("API 卡创建失败 status=%d body=%s", response.Code, response.Body.String())
	}
	// listRequest 是读取卡券摘要的请求。
	listRequest := httptest.NewRequest(http.MethodGet, "/api/v1/cards", nil)
	listRequest.AddCookie(cookie)
	// listResponse 保存卡券列表响应。
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK {
		t.Fatalf("API 卡列表失败 status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
	// rows 保存脱敏卡券列表。
	var rows []map[string]any
	// err 表示脱敏卡券列表 JSON 解码错误。
	if err := json.Unmarshal(listResponse.Body.Bytes(), &rows); err != nil || len(rows) != 1 {
		t.Fatalf("API 卡列表格式错误 rows=%v err=%v", rows, err)
	}
	// summary 保存响应中的 API 脱敏摘要。
	summary, ok := rows[0]["api_config"].(map[string]any)
	if !ok || summary["ready"] != true || summary["url"] != "https://example.com/card" {
		t.Fatalf("API 卡摘要错误: %+v", rows[0]["api_config"])
	}
	if strings.Contains(listResponse.Body.String(), "super-secret") || strings.Contains(listResponse.Body.String(), "Authorization") {
		t.Fatalf("API 卡响应泄漏敏感模板: %s", listResponse.Body.String())
	}
}

// itoa 封装itoa业务协调。
func itoa(n int64) string { return strconv.FormatInt(n, 10) }
