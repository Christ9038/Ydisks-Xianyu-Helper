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
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// 准备一个卡密组用于 send_card 动作。
	cardID, _ := store.Cards.Create(ctx, &db.CardFull{Name: "卡密组1", Type: "text", TextContent: "卡密ABC", Enabled: true, UserID: 1})
	// 准备一个商品。
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','it-auto','商品A')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// 创建规则：付款后自动发货（send_card）。
	body := `{"cookie_id":"acc1","item_id":"it-auto","trigger_type":"order_paid","enabled":true,` +
		`"actions":[{"action_type":"send_card","card_id":` + itoa(cardID) + `,"delivery_count":1,"delay_seconds":0}]}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
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
	if cr["success"] != true {
		t.Fatalf("创建失败: %+v", cr)
	}
	// ruleID 保存规则ID，供当前处理流程使用
	ruleID := itoa(int64(cr["id"].(float64)))

	// 列表。
	req2 := httptest.NewRequest(http.MethodGet, "/automation-rules", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != 200 {
		t.Fatalf("list status=%d", rec2.Code)
	}
	// arr 保存arr，供当前处理流程使用
	var arr []map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &arr)
	if len(arr) != 1 {
		t.Fatalf("应1条规则，got %d", len(arr))
	}

	// 更新规则（改名为自定义）。
	updBody := `{"cookie_id":"acc1","item_id":"it-auto","name":"自定义规则","trigger_type":"order_paid","enabled":false,` +
		`"actions":[{"action_type":"send_card","card_id":` + itoa(cardID) + `,"delivery_count":2}]}`
	// req3 保存req3，供当前处理流程使用
	req3 := httptest.NewRequest(http.MethodPut, "/automation-rules/"+ruleID, strings.NewReader(updBody))
	req3.AddCookie(cookie)
	// rec3 保存rec3，供当前处理流程使用
	rec3 := httptest.NewRecorder()
	h.ServeHTTP(rec3, req3)
	if rec3.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec3.Code, rec3.Body.String())
	}

	// 删除。
	req4 := httptest.NewRequest(http.MethodDelete, "/automation-rules/"+ruleID, nil)
	req4.AddCookie(cookie)
	// rec4 保存rec4，供当前处理流程使用
	rec4 := httptest.NewRecorder()
	h.ServeHTTP(rec4, req4)
	if rec4.Code != 200 {
		t.Fatalf("delete status=%d", rec4.Code)
	}
}

// TestAutomationRulesListFiltersAndPagination 负责Test自动化规则列表ListFiltersAndPagination相关处理。
func TestAutomationRulesListFiltersAndPagination(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','item-blue','蓝色会员')`)
	// input 表示当前遍历过程中的input
	for _, input := range []db.AutomationRuleInput{
		{UserID: 1, CookieID: "acc1", ItemID: "item-blue", Name: "付款规则", TriggerType: automation.TriggerOrderPaid, Enabled: true, Priority: 100, Actions: []db.AutomationActionInput{{ActionType: automation.ActionConfirmShipment, Enabled: true}}},
		{UserID: 1, CookieID: "acc1", Name: "评价赠品", TriggerType: automation.TriggerBuyerReviewed, Enabled: false, Priority: 100, Actions: []db.AutomationActionInput{{ActionType: automation.ActionSendText, MessageTemplate: "gift", Enabled: true}}},
		{UserID: 1, CookieID: "acc1", Name: "求评价", TriggerType: automation.TriggerReviewMissingTimeout, Enabled: true, Priority: 100, Actions: []db.AutomationActionInput{{ActionType: automation.ActionSendText, MessageTemplate: "review", Enabled: true}}},
	} {
		if // err 保存err，供当前处理流程使用
		_, err := store.Automation.Create(ctx, input); err != nil {
			t.Fatalf("create rule: %v", err)
		}
	}

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// request 保存请求，供当前处理流程使用
	request := func(path string) (int, map[string]any) {
		t.Helper()
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		// body 保存请求体，供当前处理流程使用
		var body map[string]any
		if rec.Code == http.StatusOK {
			if // err 保存err，供当前处理流程使用
			err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
				t.Fatalf("decode response: %v", err)
			}
		}
		return rec.Code, body
	}

	// status、filtered 保存status、filtered，供当前处理流程使用
	status, filtered := request("/automation-rules?page=1&page_size=10&cookie_id=acc1&trigger_type=order_paid&enabled=true&search=%E8%93%9D%E8%89%B2")
	if status != http.StatusOK || filtered["total"] != float64(1) || filtered["total_pages"] != float64(1) {
		t.Fatalf("filtered status=%d body=%+v", status, filtered)
	}
	// filteredCounts、ok 保存filteredCounts、ok，供当前处理流程使用
	filteredCounts, ok := filtered["trigger_counts"].(map[string]any)
	if !ok || filteredCounts[automation.TriggerOrderPaid] != float64(1) || len(filteredCounts) != 1 {
		t.Fatalf("filtered trigger counts=%+v", filtered["trigger_counts"])
	}
	// data、ok 保存data、ok，供当前处理流程使用
	data, ok := filtered["data"].([]any)
	if !ok || len(data) != 1 || data[0].(map[string]any)["name"] != "付款规则" {
		t.Fatalf("filtered data=%+v", filtered["data"])
	}

	// status、lastPage 保存status、last页码，供当前处理流程使用
	status, lastPage := request("/automation-rules?page=99&page_size=1")
	if status != http.StatusOK || lastPage["total"] != float64(3) || lastPage["page"] != float64(3) {
		t.Fatalf("last page status=%d body=%+v", status, lastPage)
	}
	if // data、ok 保存data、ok，供当前处理流程使用
	data, ok := lastPage["data"].([]any); !ok || len(data) != 1 {
		t.Fatalf("last page data=%+v", lastPage["data"])
	}
	// lastPageCounts、ok 保存last页码Counts、ok，供当前处理流程使用
	lastPageCounts, ok := lastPage["trigger_counts"].(map[string]any)
	if !ok || lastPageCounts[automation.TriggerOrderPaid] != float64(1) ||
		lastPageCounts[automation.TriggerBuyerReviewed] != float64(1) ||
		lastPageCounts[automation.TriggerReviewMissingTimeout] != float64(1) {
		t.Fatalf("last page trigger counts=%+v", lastPage["trigger_counts"])
	}

	status, _ = request("/automation-rules?page=1&enabled=maybe")
	if status != http.StatusBadRequest {
		t.Fatalf("invalid enabled status=%d", status)
	}
}

// TestAutomationRuleBadTriggerType 不支持的触发类型 400。
func TestAutomationRuleBadTriggerType(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1","trigger_type":"bogus","actions":[{"action_type":"confirm_shipment"}]}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("不支持触发类型应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleBadCookie 账号不属于当前用户 400。
func TestAutomationRuleBadCookie(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"other-account","trigger_type":"order_paid","actions":[{"action_type":"confirm_shipment"}]}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无权账号应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleBadActionType 不支持的动作类型 400。
func TestAutomationRuleBadActionType(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[{"action_type":"bogus"}]}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("不支持动作类型应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleRejectsAPICard 负责Test自动化规则RejectsAPI卡密相关处理。
func TestAutomationRuleRejectsAPICard(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// cardID、err 保存卡密ID、err，供当前处理流程使用
	cardID, err := store.Cards.Create(context.Background(), &db.CardFull{
		Name: "旧 API 卡密", Type: "api", APIConfig: `{"url":"https://example.com"}`, Enabled: true, UserID: 1,
	})
	if err != nil {
		t.Fatal(err)
	}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[{"action_type":"send_card","card_id":` + itoa(cardID) + `}]}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest || !strings.Contains(rec.Body.String(), "API 卡密暂不支持") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
}

// TestAutomationRuleSendCardMissingCardID send_card 缺 card_id 400。
func TestAutomationRuleSendCardMissingCardID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[{"action_type":"send_card","card_id":0}]}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("send_card 缺 card_id 应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleSendTextMissingTemplate send_text 缺文案 400。
func TestAutomationRuleSendTextMissingTemplate(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[{"action_type":"send_text","message_template":""}]}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("send_text 缺文案应 400，got %d", rec.Code)
	}
}

// TestAutomationIssueEndpointsAndActiveDeleteConflict 负责Test自动化问题EndpointsAndActiveDeleteConflict相关处理。
func TestAutomationIssueEndpointsAndActiveDeleteConflict(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// admin 保存admin，供当前处理流程使用
	admin, _ := store.Users.GetByUsername(ctx, "admin")
	// ruleID、err 保存规则ID、err，供当前处理流程使用
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{UserID: admin.ID, CookieID: "acc1", Name: "issue",
		TriggerType: automation.TriggerBuyerReviewed, Enabled: true,
		Actions: []db.AutomationActionInput{{ActionType: automation.ActionSendText, MessageTemplate: "x", Enabled: true}}})
	if err != nil {
		t.Fatal(err)
	}
	// runID 保存运行ID，供当前处理流程使用
	runID, _, _ := store.Automation.TryStartRun(ctx, db.AutomationRun{RuleID: ruleID, CookieID: "acc1", OrderID: "o",
		TriggerType: automation.TriggerBuyerReviewed, TriggerKey: "k", RawEventJSON: `{}`, LeaseExpiresAt: 1})
	_, _ = store.Automation.StartRunAction(ctx, runID, 1, 0, 1)
	_ = store.Automation.QuarantineRun(ctx, runID, 1, "unknown")
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// listReq 保存listReq，供当前处理流程使用
	listReq := httptest.NewRequest(http.MethodGet, "/automation-issues", nil)
	listReq.AddCookie(cookie)
	// listRec 保存listRec，供当前处理流程使用
	listRec := httptest.NewRecorder()
	h.ServeHTTP(listRec, listReq)
	if listRec.Code != http.StatusOK || !strings.Contains(listRec.Body.String(), "unknown") {
		t.Fatalf("list status=%d body=%s", listRec.Code, listRec.Body.String())
	}

	// deleteReq 保存deleteReq，供当前处理流程使用
	deleteReq := httptest.NewRequest(http.MethodDelete, "/automation-rules/"+itoa(ruleID), nil)
	deleteReq.AddCookie(cookie)
	// deleteRec 保存deleteRec，供当前处理流程使用
	deleteRec := httptest.NewRecorder()
	h.ServeHTTP(deleteRec, deleteReq)
	if deleteRec.Code != http.StatusConflict {
		t.Fatalf("active delete status=%d body=%s", deleteRec.Code, deleteRec.Body.String())
	}

	// resolveReq 保存resolveReq，供当前处理流程使用
	resolveReq := httptest.NewRequest(http.MethodPost, "/automation-runs/"+itoa(runID)+"/resolve", strings.NewReader(`{"resolution":"cancel"}`))
	resolveReq.AddCookie(cookie)
	// resolveRec 保存resolveRec，供当前处理流程使用
	resolveRec := httptest.NewRecorder()
	h.ServeHTTP(resolveRec, resolveReq)
	if resolveRec.Code != http.StatusOK {
		t.Fatalf("resolve status=%d body=%s", resolveRec.Code, resolveRec.Body.String())
	}
}

// TestAutomationRuleNoActions 缺动作 400。
func TestAutomationRuleNoActions(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[]}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("缺动作应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleRejectsIncompatibleTriggerActions 负责Test自动化规则RejectsIncompatibleTrigger动作列表相关处理。
func TestAutomationRuleRejectsIncompatibleTriggerActions(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// cardID、err 保存卡密ID、err，供当前处理流程使用
	cardID, err := store.Cards.Create(context.Background(), &db.CardFull{Name: "card", Type: "text", TextContent: "x", Enabled: true, UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// tests 保存tests，供当前处理流程使用
	tests := []string{
		`{"cookie_id":"acc1","trigger_type":"order_paid","actions":[{"action_type":"send_text","message_template":"x"}]}`,
		`{"cookie_id":"acc1","trigger_type":"buyer_reviewed","actions":[{"action_type":"confirm_shipment"}]}`,
		`{"cookie_id":"acc1","trigger_type":"review_missing_timeout","actions":[{"action_type":"send_card","card_id":` + itoa(cardID) + `}]}`,
	}
	// body 表示当前遍历过程中的请求体
	for _, body := range tests {
		// req 保存req，供当前处理流程使用
		req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader(body))
		req.AddCookie(cookie)
		// rec 保存rec，供当前处理流程使用
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("body=%s status=%d response=%s", body, rec.Code, rec.Body.String())
		}
	}
}

// TestAutomationRuleBadJSON 非法 JSON 400。
func TestAutomationRuleBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/automation-rules", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleUpdateBadID 无效 ID 400。
func TestAutomationRuleUpdateBadID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"cookie_id":"acc1","trigger_type":"order_paid","actions":[{"action_type":"confirm_shipment"}]}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/automation-rules/abc", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleDeleteBadID 无效 ID 400。
func TestAutomationRuleDeleteBadID(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodDelete, "/automation-rules/abc", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("无效 ID 应 400，got %d", rec.Code)
	}
}

// TestAutomationRuleDeleteNotFound 不存在规则 404。
func TestAutomationRuleDeleteNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodDelete, "/automation-rules/999999", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusNotFound {
		t.Fatalf("不存在规则应 404，got %d", rec.Code)
	}
}

// TestDefaultAutomationRuleName 默认规则名。
func TestDefaultAutomationRuleName(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := map[string]string{
		automation.TriggerOrderPaid:            "付款后自动发货",
		automation.TriggerBuyerReviewed:        "评价后发送赠品",
		automation.TriggerReviewMissingTimeout: "超时未评价求评价",
		"bogus":                                "自动化规则",
	}
	// trigger、want 表示当前遍历过程中的trigger、want
	for trigger, want := range cases {
		// got 保存got，供当前处理流程使用
		got := defaultAutomationRuleName(trigger, "")
		if got != want {
			t.Errorf("defaultAutomationRuleName(%q)=%q want %q", trigger, got, want)
		}
	}
	if // got 保存got，供当前处理流程使用
	got := defaultAutomationRuleName(automation.TriggerOrderPaid, "item-x"); got != "付款后自动发货 - item-x" {
		t.Errorf("带 itemID 的默认名异常: %q", got)
	}
}

// TestIsJSONObject isJSONObject 表驱动。
func TestIsJSONObject(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := map[string]bool{
		`{}`:      true,
		`{"a":1}`: true,
		`null`:    true, // null 能 unmarshal 为 map（nil），函数判定为对象
		`[]`:      false,
		`"str"`:   false,
		`invalid`: false,
		``:        false,
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 保存got，供当前处理流程使用
		got := isJSONObject(in); got != want {
			t.Errorf("isJSONObject(%q)=%v want %v", in, got, want)
		}
	}
}

// TestFirstNonZero firstNonZero 表驱动。
func TestFirstNonZero(t *testing.T) {
	if // got 保存got，供当前处理流程使用
	got := firstNonZero(0, 5); got != 5 {
		t.Fatalf("got %d", got)
	}
	if // got 保存got，供当前处理流程使用
	got := firstNonZero(3, 5); got != 3 {
		t.Fatalf("got %d", got)
	}
}
