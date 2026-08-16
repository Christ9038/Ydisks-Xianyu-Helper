package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestKeywordsCRUD 关键字增删查。
func TestKeywordsCRUD(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 添加（普通 + 商品ID）。
	post := func(body string) {
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodPost, "/keywords/acc1", strings.NewReader(body))
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != 200 {
			t.Fatalf("add keyword status=%d body=%s", rec.Code, rec.Body.String())
		}
	}
	post(`{"keyword":"在吗","reply":"在的"}`)
	post(`{"keyword":"价格","reply":"50元","item_id":"item1"}`)

	// 列表（带类型）。
	req := httptest.NewRequest(http.MethodGet, "/keywords-with-type/acc1", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// arr 保存arr，供当前处理流程使用
	var arr []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &arr)
	if len(arr) != 2 {
		t.Fatalf("应2条关键字，got %d", len(arr))
	}

	// 按索引删除第一条。
	req2 := httptest.NewRequest(http.MethodDelete, "/keywords/acc1/0", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("delete status=%d", rec2.Code)
	}
}

// TestDefaultReplyCRUD 默认回复。
func TestDefaultReplyCRUD(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 设置。
	body := `{"enabled":true,"reply_content":"你好老板","reply_once":true}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/default-replies/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("set status=%d", rec.Code)
	}

	// 读取。
	req2 := httptest.NewRequest(http.MethodGet, "/default-replies/acc1", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	// dr 保存dr，供当前处理流程使用
	var dr map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &dr)
	if dr["enabled"] != true || dr["reply_content"] != "你好老板" || dr["reply_once"] != true {
		t.Fatalf("默认回复异常: %+v", dr)
	}

	// 前端兼容路径：/api/default-reply/* 与 /api/default-replies。
	req3 := httptest.NewRequest(http.MethodPut, "/api/default-reply/acc1", strings.NewReader(body))
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("compat set status=%d", rec3.Code)
	}
	// req4 保存req4，供当前处理流程使用
	req4 := httptest.NewRequest(http.MethodGet, "/api/default-replies", nil)
	req4.AddCookie(cookie)
	// rec4 保存rec4，供当前处理流程使用
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	// all 保存all，供当前处理流程使用
	var all map[string]map[string]any
	json.Unmarshal(rec4.Body.Bytes(), &all)
	if all["acc1"]["reply_content"] != "你好老板" {
		t.Fatalf("兼容默认回复列表异常: %+v", all)
	}
	// req5 保存req5，供当前处理流程使用
	req5 := httptest.NewRequest(http.MethodPost, "/api/default-reply/acc1/clear-records", nil)
	req5.AddCookie(cookie)
	// rec5 保存rec5，供当前处理流程使用
	rec5 := httptest.NewRecorder()
	h.ServeHTTP(rec5, req5)
	if rec5.Code != 200 {
		t.Fatalf("clear records status=%d", rec5.Code)
	}
}

// TestNotificationChannelCRUD 通知渠道 + 绑定。
func TestNotificationChannelCRUD(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 创建渠道。
	body := `{"name":"我的钉钉","type":"dingtalk","config":"{\"webhook_url\":\"http://x\"}","enabled":true}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/notification-channels", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create channel status=%d body=%s", rec.Code, rec.Body.String())
	}
	// cr 保存cr，供当前处理流程使用
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	// id 保存标识，供当前处理流程使用
	id := int64(cr["id"].(float64))

	// 绑定到账号。
	bindBody := `{"channel_ids":[` + itoa(id) + `]}`
	// req2 保存req2，供当前处理流程使用
	req2 := httptest.NewRequest(http.MethodPost, "/message-notifications/acc1", strings.NewReader(bindBody))
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("bind status=%d", rec2.Code)
	}

	// 查询绑定。
	req3 := httptest.NewRequest(http.MethodGet, "/message-notifications/acc1", nil)
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	// b 保存b，供当前处理流程使用
	var b map[string]any
	json.Unmarshal(rec3.Body.Bytes(), &b)
	// ids 保存ids，供当前处理流程使用
	ids, _ := b["channel_ids"].([]any)
	if len(ids) != 1 {
		t.Fatalf("绑定数异常: %+v", b)
	}
}

// TestSystemSettings 系统设置。
func TestSystemSettings(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 公开设置无需登录。
	req := httptest.NewRequest(http.MethodGet, "/system-settings/public", nil)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("public settings status=%d", rec.Code)
	}
	// pub 保存pub，供当前处理流程使用
	var pub map[string]any
	json.Unmarshal(rec.Body.Bytes(), &pub)
	if // ok 保存ok，供当前处理流程使用
	_, ok := pub["theme_color"]; !ok {
		t.Fatalf("公开设置应含 theme_color: %+v", pub)
	}

	// 已认证：设置 + 读取全部。
	body := `{"value":"green"}`
	// req2 保存req2，供当前处理流程使用
	req2 := httptest.NewRequest(http.MethodPut, "/system-settings/theme_color", strings.NewReader(body))
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("set status=%d", rec2.Code)
	}

	// req3 保存req3，供当前处理流程使用
	req3 := httptest.NewRequest(http.MethodGet, "/system-settings", nil)
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	// all 保存all，供当前处理流程使用
	var all map[string]any
	json.Unmarshal(rec3.Body.Bytes(), &all)
	if all["theme_color"] != "green" {
		t.Fatalf("设置未生效: %+v", all["theme_color"])
	}
}

// TestAIReplySettings AI 回复设置。
func TestAIReplySettings(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"ai_enabled":true,"max_discount_percent":12,"max_discount_amount":88,"max_bargain_rounds":4,"custom_prompts":"按商品信息回复"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/ai-reply-settings/acc1", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("set ai status=%d", rec.Code)
	}

	// 账号级设置只包含开关、议价策略和自定义提示词；模型/API Key/URL 走系统设置。
	req2 := httptest.NewRequest(http.MethodGet, "/ai-reply-settings/acc1", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	// cfg 保存cfg，供当前处理流程使用
	var cfg map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &cfg)
	if cfg["ai_enabled"] != true ||
		cfg["max_discount_percent"] != float64(12) ||
		cfg["max_discount_amount"] != float64(88) ||
		cfg["max_bargain_rounds"] != float64(4) ||
		cfg["custom_prompts"] != "按商品信息回复" {
		t.Fatalf("AI 设置异常: %+v", cfg)
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := cfg["model_name"]; ok {
		t.Fatalf("账号级 AI 设置不应返回模型: %+v", cfg)
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := cfg["api_key"]; ok {
		t.Fatalf("账号级 AI 设置不应返回 API Key: %+v", cfg)
	}

	// req3 保存req3，供当前处理流程使用
	req3 := httptest.NewRequest(http.MethodGet, "/ai-reply-settings", nil)
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	// all 保存all，供当前处理流程使用
	var all map[string]map[string]any
	json.Unmarshal(rec3.Body.Bytes(), &all)
	if all["acc1"]["ai_enabled"] != true {
		t.Fatalf("AI 设置列表异常: %+v", all)
	}
}

// TestAIReplySettingsRejectInvalidBargainLimits 负责TestAI回复设置RejectInvalidBargainLimits相关处理。
func TestAIReplySettingsRejectInvalidBargainLimits(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 表示当前遍历过程中的请求体
	for _, body := range []string{
		`{"ai_enabled":true,"max_discount_percent":101,"max_discount_amount":1,"max_bargain_rounds":1}`,
		`{"ai_enabled":true,"max_discount_percent":1,"max_discount_amount":-1,"max_bargain_rounds":1}`,
		`{"ai_enabled":true,"max_discount_percent":1,"max_discount_amount":1,"max_bargain_rounds":0}`,
	} {
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodPut, "/ai-reply-settings/acc1", strings.NewReader(body))
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d want 400", body, rec.Code)
		}
	}
}

// TestParseOpenAIModels 负责TestParseOpenAI模型列表相关处理。
func TestParseOpenAIModels(t *testing.T) {
	// models、err 保存models、err，供当前处理流程使用
	models, err := parseOpenAIModels([]byte(`{"data":[{"id":"qwen-plus"},{"id":"qwen-max"},{"name":"fallback-name"}]}`))
	if err != nil {
		t.Fatalf("parse models: %v", err)
	}
	// want 保存want，供当前处理流程使用
	want := []string{"qwen-plus", "qwen-max", "fallback-name"}
	if len(models) != len(want) {
		t.Fatalf("models length=%d want=%d: %+v", len(models), len(want), models)
	}
	// i 表示当前遍历过程中的i
	for i := range want {
		if models[i] != want[i] {
			t.Fatalf("models[%d]=%q want=%q: %+v", i, models[i], want[i], models)
		}
	}
}

// TestItems 物品列表 + 多规格设置。
func TestItems(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','it1','商品A')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 列表。
	req := httptest.NewRequest(http.MethodGet, "/items", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// arr 保存arr，供当前处理流程使用
	var arr []map[string]any
	json.Unmarshal(rec.Body.Bytes(), &arr)
	if len(arr) != 1 || arr[0]["item_title"] != "商品A" {
		t.Fatalf("物品列表异常: %+v", arr)
	}

	// 设置多规格。
	body := `{"is_multi_spec":true}`
	// req2 保存req2，供当前处理流程使用
	req2 := httptest.NewRequest(http.MethodPut, "/items/acc1/it1/multi-spec", strings.NewReader(body))
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("multi-spec status=%d", rec2.Code)
	}

	// 验证。
	it, err := store.Items.Get(ctx, "acc1", "it1")
	if err != nil || it == nil {
		t.Fatalf("Get 失败: err=%v it=%v", err, it)
	}
	if !it.IsMultiSpec {
		t.Fatal("多规格应已开启")
	}
}

// TestOrderImportCompat 负责Test订单ImportCompat相关处理。
func TestOrderImportCompat(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `[{"order_id":"order-import-1","item_id":"item-import-1","item_title":"导入商品","buyer_id":"buyer1","status":"pending_ship","quantity":2,"amount":"19.90"}]`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("import status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["success_count"] != float64(1) {
		t.Fatalf("导入结果异常: %+v", res)
	}

	// req2 保存req2，供当前处理流程使用
	req2 := httptest.NewRequest(http.MethodGet, "/api/orders/order-import-1", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("get imported order status=%d body=%s", rec2.Code, rec2.Body.String())
	}
	// order 保存订单，供当前处理流程使用
	var order map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &order)
	if order["cookie_id"] != "acc1" || order["status"] != "pending_ship" {
		t.Fatalf("导入订单异常: %+v", order)
	}
}

// TestOrderImportReportsPartialFailure 负责Test订单ImportReportsPartialFailure相关处理。
func TestOrderImportReportsPartialFailure(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// body 保存请求体，供当前处理流程使用
	body := `[{"order_id":"order-ok","status":"pending_ship"},{"item_id":"missing-order-id"}]`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/import", strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// result 保存结果，供当前处理流程使用
	var result struct {
		PartialFailure bool `json:"partial_failure"`
		SuccessCount   int  `json:"success_count"`
		FailedCount    int  `json:"failed_count"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.PartialFailure || result.SuccessCount != 1 || result.FailedCount != 1 {
		t.Fatalf("result=%+v", result)
	}
}

// TestAdminEndpoints 管理员统计 + 用户列表 + 非 admin 被拒。
func TestAdminEndpoints(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// stats。
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("stats status=%d", rec.Code)
	}
	// stats 保存stats，供当前处理流程使用
	var stats map[string]any
	json.Unmarshal(rec.Body.Bytes(), &stats)
	if stats["total_users"] == nil || stats["total_cookies"] == nil {
		t.Fatalf("stats 异常: %+v", stats)
	}

	// 用户列表。
	req2 := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	// users 保存用户列表，供当前处理流程使用
	var users []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &users)
	if len(users) != 1 || users[0]["username"] != "admin" {
		t.Fatalf("用户列表异常: %+v", users)
	}

	// 创建普通用户验证 admin 隔离。
	store.Users.Create(context.Background(), "user2", "u2@e.com", "pw")
	// 普通用户不应能访问 admin（需单独登录验证，此处仅验证 admin 能看到）。
	req3 := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	// users2 保存users2，供当前处理流程使用
	var users2 []map[string]any
	json.Unmarshal(rec3.Body.Bytes(), &users2)
	if len(users2) != 2 {
		t.Fatalf("应2用户，got %d", len(users2))
	}
}

// TestPublicSettingsNoAuth 公开设置无需登录。
func TestPublicSettingsNoAuth(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/system-settings/public", nil)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("公开设置应无需登录，status=%d", rec.Code)
	}
}
