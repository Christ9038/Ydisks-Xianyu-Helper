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
	t.Helper()
	body := `{"username":"admin","password":"pw"}`
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("login status=%d", rec.Code)
	}
	return rec.Result().Cookies()[0]
}

// TestOrderListAndDetail 订单列表 + 详情 + 状态码归一。
func TestOrderListAndDetail(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	// 插入一条订单（order_status 用数字码 "2" 测试归一）。
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('ord1','item1','buyer1','2','acc1')`)
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title, item_detail) VALUES ('acc1','item1','测试商品','{"pic_info":{"picUrl":"https://img.example/item.png"}}')`)

	h := srv.Router()
	cookie := loginHelper(t, h)

	// 列表。
	req := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("list status=%d body=%s", rec.Code, rec.Body.String())
	}
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
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("detail status=%d", rec2.Code)
	}
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
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("delete status=%d", rec3.Code)
	}
}

// TestCardCRUD 卡券增删改查。
func TestCardCRUD(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 创建。
	body := `{"name":"测试卡","type":"text","text_content":"卡密ABC","enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/cards", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	id := cr["id"].(float64)
	if id == 0 {
		t.Fatal("应返回 id")
	}

	// 列表。
	req2 := httptest.NewRequest(http.MethodGet, "/cards", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["name"] != "测试卡" {
		t.Fatalf("列表异常: %+v", arr)
	}

	// 更新。
	updBody := `{"name":"改名卡","type":"text","text_content":"卡密XYZ","enabled":true}`
	req3 := httptest.NewRequest(http.MethodPut, "/cards/"+itoa(int64(id)), strings.NewReader(updBody))
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("update status=%d", rec3.Code)
	}

	// 获取验证改名。
	req4 := httptest.NewRequest(http.MethodGet, "/cards/"+itoa(int64(id)), nil)
	req4.AddCookie(cookie)
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	var got map[string]any
	json.Unmarshal(rec4.Body.Bytes(), &got)
	if got["name"] != "改名卡" {
		t.Errorf("改名后应=改名卡, got %v", got["name"])
	}

	// 删除。
	req5 := httptest.NewRequest(http.MethodDelete, "/cards/"+itoa(int64(id)), nil)
	req5.AddCookie(cookie)
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, req5)
	if rec5.Code != 200 {
		t.Fatalf("delete status=%d", rec5.Code)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// TestDeliveryRuleCRUD 发货规则增删。
func TestDeliveryRuleCRUD(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	// 先建一个卡券供规则引用。
	store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES (1,'卡','text','c',1,1)`)

	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"keyword":"VIP","card_id":1,"delivery_count":1,"enabled":true}`
	req := httptest.NewRequest(http.MethodPost, "/delivery-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 列表。
	req2 := httptest.NewRequest(http.MethodGet, "/delivery-rules", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["keyword"] != "VIP" {
		t.Fatalf("规则列表异常: %+v", arr)
	}
}

func TestDeliveryRuleVariantCRUD(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO cards (id,name,type,text_content,enabled,user_id) VALUES
		(31,'30天库存','text','A',1,?),(32,'90天库存','text','B',1,?)`, admin.ID, admin.ID)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title,is_multi_spec) VALUES
		('acc1','item-vip','视频会员',1)`)

	h := srv.Router()
	cookie := loginHelper(t, h)
	body := `{"keyword":"视频会员","cookie_id":"acc1","item_id":"item-vip","enabled":true,"description":"会员发货","variants":[{"spec_name":"套餐","spec_value":"30天","card_id":31,"delivery_count":1,"enabled":true},{"spec_name":"套餐","spec_value":"90天","card_id":32,"delivery_count":2,"enabled":true}]}`
	req := httptest.NewRequest(http.MethodPost, "/delivery-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}

	listReq := httptest.NewRequest(http.MethodGet, "/delivery-rules", nil)
	listReq.AddCookie(cookie)
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	var rules []struct {
		CookieID string `json:"cookie_id"`
		ItemID   string `json:"item_id"`
		Variants []struct {
			SpecValue     string `json:"spec_value"`
			CardID        int64  `json:"card_id"`
			DeliveryCount int    `json:"delivery_count"`
		} `json:"variants"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &rules); err != nil {
		t.Fatal(err)
	}
	if len(rules) != 1 || rules[0].CookieID != "acc1" || rules[0].ItemID != "item-vip" || len(rules[0].Variants) != 2 {
		t.Fatalf("规则返回异常: %+v body=%s", rules, listRec.Body.String())
	}
	if rules[0].Variants[1].SpecValue != "90天" || rules[0].Variants[1].CardID != 32 || rules[0].Variants[1].DeliveryCount != 2 {
		t.Fatalf("规格返回异常: %+v", rules[0].Variants)
	}
}
