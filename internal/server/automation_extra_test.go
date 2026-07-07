package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
)

// TestAutomationRulesCRUD 自动化规则增删查 + 校验分支。
func TestAutomationRulesCRUD(t *testing.T) {
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	ctx := context.Background()
	// 准备一个卡密组用于 send_card 动作。
	cardID, _ := store.Cards.Create(ctx, &db.CardFull{Name: "卡密组1", Type: "text", TextContent: "卡密ABC", Enabled: true, UserID: 1})
	// 准备一个商品。
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','it-auto','商品A')`)
	h := srv.Router()
	cookie := loginHelper(t, h)

	// 创建规则：付款后自动发货（send_card）。
	body := `{"cookie_id":"acc1","item_id":"it-auto","trigger_type":"order_paid","enabled":true,` +
		`"actions":[{"action_type":"send_card","card_id":` + itoa(cardID) + `,"delivery_count":1,"delay_seconds":0}]}`
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("create status=%d body=%s", rec.Code, rec.Body.String())
	}
	var cr map[string]any
	json.Unmarshal(rec.Body.Bytes(), &cr)
	if cr["success"] != true {
		t.Fatalf("创建失败: %+v", cr)
	}
	ruleID := itoa(int64(cr["id"].(float64)))

	// 列表。
	req2 := httptest.NewRequest(http.MethodGet, "/automation-rules", nil)
	req2.AddCookie(cookie)
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list status=%d", rec2.Code)
	}
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 {
		t.Fatalf("应1条规则，got %d", len(arr))
	}

	// 更新规则（改名为自定义）。
	updBody := `{"cookie_id":"acc1","item_id":"it-auto","name":"自定义规则","trigger_type":"order_paid","enabled":false,` +
		`"actions":[{"action_type":"send_card","card_id":` + itoa(cardID) + `,"delivery_count":2}]}`
	req3 := httptest.NewRequest(http.MethodPut, "/automation-rules/"+ruleID, strings.NewReader(updBody))
	req3.AddCookie(cookie)
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec3.Code, rec3.Body.String())
	}

	// 删除。
	req4 := httptest.NewRequest(http.MethodDelete, "/automation-rules/"+ruleID, nil)
	req4.AddCookie(cookie)
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != 200 {
		t.Fatalf("delete status=%d", rec4.Code)
	}
}

// TestAutomationRuleBadTriggerType 不支持的触发类型 400。
func TestAutomationRuleBadTriggerType(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"acc1","trigger_type":"bogus","actions":[{"action_type":"confirm_shipment"}]}`
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("不支持触发类型应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleBadCookie 账号不属于当前用户 400。
func TestAutomationRuleBadCookie(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"other-account","trigger_type":"order_paid","actions":[{"action_type":"confirm_shipment"}]}`
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无权账号应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleBadActionType 不支持的动作类型 400。
func TestAutomationRuleBadActionType(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[{"action_type":"bogus"}]}`
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("不支持动作类型应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleSendCardMissingCardID send_card 缺 card_id 400。
func TestAutomationRuleSendCardMissingCardID(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[{"action_type":"send_card","card_id":0}]}`
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("send_card 缺 card_id 应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleSendTextMissingTemplate send_text 缺文案 400。
func TestAutomationRuleSendTextMissingTemplate(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[{"action_type":"send_text","message_template":""}]}`
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("send_text 缺文案应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleNoActions 缺动作 400。
func TestAutomationRuleNoActions(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[]}`
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺动作应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleBadJSON 非法 JSON 400。
func TestAutomationRuleBadJSON(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleUpdateBadID 无效 ID 400。
func TestAutomationRuleUpdateBadID(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[{"action_type":"confirm_shipment"}]}`
	req := httptest.NewRequest(http.MethodPut, "/automation-rules/abc", strings.NewReader(body))
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleDeleteBadID 无效 ID 400。
func TestAutomationRuleDeleteBadID(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodDelete, "/automation-rules/abc", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleDeleteNotFound 不存在规则 404。
func TestAutomationRuleDeleteNotFound(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	h := srv.Router()
	cookie := loginHelper(t, h)

	req := httptest.NewRequest(http.MethodDelete, "/automation-rules/999999", nil)
	req.AddCookie(cookie)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在规则应 404，got %d", rec.Code)
	}
}

// TestDefaultAutomationRuleName 默认规则名。
func TestDefaultAutomationRuleName(t *testing.T) {
	cases := map[string]string{
		automation.TriggerOrderPaid:            "付款后自动发货",
		automation.TriggerBuyerReviewed:        "评价后发送赠品",
		automation.TriggerReviewMissingTimeout: "超时未评价求评价",
		"bogus": "自动化规则",
	}
	for trigger, want := range cases {
		got := defaultAutomationRuleName(trigger, "")
		if got != want {
			t.Errorf("defaultAutomationRuleName(%q)=%q want %q", trigger, got, want)
		}
	}
	if got := defaultAutomationRuleName(automation.TriggerOrderPaid, "item-x"); got != "付款后自动发货 - item-x" {
		t.Errorf("带 itemID 的默认名异常: %q", got)
	}
}

// TestIsJSONObject isJSONObject 表驱动。
func TestIsJSONObject(t *testing.T) {
	cases := map[string]bool{
		`{}`:        true,
		`{"a":1}`:   true,
		`null`:      true, // null 能 unmarshal 为 map（nil），函数判定为对象
		`[]`:        false,
		`"str"`:     false,
		`invalid`:   false,
		``:          false,
	}
	for in, want := range cases {
		if got := isJSONObject(in); got != want {
			t.Errorf("isJSONObject(%q)=%v want %v", in, got, want)
		}
	}
}

// TestFirstNonZero firstNonZero 表驱动。
func TestFirstNonZero(t *testing.T) {
	if got := firstNonZero(0, 5); got != 5 {
		t.Fatalf("got %d", got)
	}
	if got := firstNonZero(3, 5); got != 3 {
		t.Fatalf("got %d", got)
	}
}
