package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestNamedSuccessResponseContracts 验证认证、订单和聊天主链路使用具名成功响应 DTO。
func TestNamedSuccessResponseContracts(t *testing.T) {
	// srv 是用于验证成功响应 DTO 的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)

	// verifyReq 是读取当前会话状态的请求。
	verifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	verifyReq.AddCookie(sessionCookie)
	// verifyRecorder 是捕获会话状态响应的测试记录器。
	verifyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(verifyRecorder, verifyReq)
	if verifyRecorder.Code != http.StatusOK {
		t.Fatalf("verify status=%d body=%s", verifyRecorder.Code, verifyRecorder.Body.String())
	}
	// verifyResponse 是会话校验具名响应 DTO。
	var verifyResponse sessionVerificationResponse
	// verifyDecodeErr 表示会话响应 JSON 反序列化失败的原因。
	if verifyDecodeErr := json.Unmarshal(verifyRecorder.Body.Bytes(), &verifyResponse); verifyDecodeErr != nil {
		t.Fatalf("decode verify response: %v", verifyDecodeErr)
	}
	if !verifyResponse.Authenticated || !verifyResponse.Initialized || verifyResponse.Username != "admin" {
		t.Fatalf("verify response=%+v", verifyResponse)
	}

	// orderReq 是读取当前用户订单列表的请求。
	orderReq := httptest.NewRequest(http.MethodGet, "/api/orders", nil)
	orderReq.AddCookie(sessionCookie)
	// orderRecorder 是捕获订单列表响应的测试记录器。
	orderRecorder := httptest.NewRecorder()
	handler.ServeHTTP(orderRecorder, orderReq)
	if orderRecorder.Code != http.StatusOK {
		t.Fatalf("order status=%d body=%s", orderRecorder.Code, orderRecorder.Body.String())
	}
	// orderResponse 是订单列表具名响应 DTO。
	var orderResponse orderListResponse
	// orderDecodeErr 表示订单列表响应 JSON 反序列化失败的原因。
	if orderDecodeErr := json.Unmarshal(orderRecorder.Body.Bytes(), &orderResponse); orderDecodeErr != nil {
		t.Fatalf("decode order response: %v", orderDecodeErr)
	}
	if !orderResponse.Success || orderResponse.Page != 1 || orderResponse.PageSize != 20 {
		t.Fatalf("order response=%+v", orderResponse)
	}

	// chatReq 是读取账号聊天会话列表的请求。
	chatReq := httptest.NewRequest(http.MethodGet, "/api/chat/sessions?account_id=acc1", nil)
	chatReq.AddCookie(sessionCookie)
	// chatRecorder 是捕获聊天会话响应的测试记录器。
	chatRecorder := httptest.NewRecorder()
	handler.ServeHTTP(chatRecorder, chatReq)
	if chatRecorder.Code != http.StatusOK {
		t.Fatalf("chat status=%d body=%s", chatRecorder.Code, chatRecorder.Body.String())
	}
	// chatResponse 是聊天会话分页具名响应 DTO。
	var chatResponse chatSessionPageResponse
	// chatDecodeErr 表示聊天会话响应 JSON 反序列化失败的原因。
	if chatDecodeErr := json.Unmarshal(chatRecorder.Body.Bytes(), &chatResponse); chatDecodeErr != nil {
		t.Fatalf("decode chat response: %v", chatDecodeErr)
	}
	if chatResponse.Sessions == nil || chatResponse.HasMore {
		t.Fatalf("chat response=%+v", chatResponse)
	}
}

// TestRemainingSuccessResponseContracts 验证账号、商品、自动化和订单详情响应使用具名 DTO。
func TestRemainingSuccessResponseContracts(t *testing.T) {
	// srv 是用于验证剩余成功响应 DTO 的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// seedErr 是测试商品写入模板数据库失败的原因。
	if _, seedErr := store.DB.ExecContext(context.Background(), `INSERT INTO item_info (cookie_id, item_id, item_title) VALUES ('acc1','contract-item','契约商品')`); seedErr != nil {
		t.Fatalf("seed item: %v", seedErr)
	}

	// accountReq 是读取单个账号非敏感详情的请求。
	accountReq := httptest.NewRequest(http.MethodGet, "/cookie/acc1/details", nil)
	accountReq.AddCookie(sessionCookie)
	// accountRecorder 是捕获账号详情响应的记录器。
	accountRecorder := httptest.NewRecorder()
	handler.ServeHTTP(accountRecorder, accountReq)
	if accountRecorder.Code != http.StatusOK {
		t.Fatalf("account status=%d body=%s", accountRecorder.Code, accountRecorder.Body.String())
	}
	// accountResponse 是账号详情具名响应 DTO。
	var accountResponse cookieDetailResponse
	// accountDecodeErr 是账号详情响应 JSON 反序列化失败的原因。
	if accountDecodeErr := json.Unmarshal(accountRecorder.Body.Bytes(), &accountResponse); accountDecodeErr != nil {
		t.Fatalf("decode account response: %v", accountDecodeErr)
	}
	if accountResponse.ID != "acc1" || !accountResponse.HasCookie {
		t.Fatalf("account response=%+v", accountResponse)
	}

	// itemReq 是读取本地商品列表的请求。
	itemReq := httptest.NewRequest(http.MethodGet, "/items", nil)
	itemReq.AddCookie(sessionCookie)
	// itemRecorder 是捕获商品列表响应的记录器。
	itemRecorder := httptest.NewRecorder()
	handler.ServeHTTP(itemRecorder, itemReq)
	if itemRecorder.Code != http.StatusOK {
		t.Fatalf("item status=%d body=%s", itemRecorder.Code, itemRecorder.Body.String())
	}
	// itemResponse 是本地商品列表具名响应 DTO 列表。
	var itemResponse []itemListResponse
	// itemDecodeErr 是商品列表响应 JSON 反序列化失败的原因。
	if itemDecodeErr := json.Unmarshal(itemRecorder.Body.Bytes(), &itemResponse); itemDecodeErr != nil {
		t.Fatalf("decode item response: %v", itemDecodeErr)
	}
	if len(itemResponse) != 1 || itemResponse[0].ItemID != "contract-item" {
		t.Fatalf("item response=%+v", itemResponse)
	}

	// ruleReq 是读取自动化规则分页响应的请求。
	ruleReq := httptest.NewRequest(http.MethodGet, "/automation-rules?page=1", nil)
	ruleReq.AddCookie(sessionCookie)
	// ruleRecorder 是捕获自动化规则响应的记录器。
	ruleRecorder := httptest.NewRecorder()
	handler.ServeHTTP(ruleRecorder, ruleReq)
	if ruleRecorder.Code != http.StatusOK {
		t.Fatalf("rule status=%d body=%s", ruleRecorder.Code, ruleRecorder.Body.String())
	}
	// ruleResponse 是自动化规则分页具名响应 DTO。
	var ruleResponse automationRulePageResponse
	// ruleDecodeErr 是自动化规则响应 JSON 反序列化失败的原因。
	if ruleDecodeErr := json.Unmarshal(ruleRecorder.Body.Bytes(), &ruleResponse); ruleDecodeErr != nil {
		t.Fatalf("decode rule response: %v", ruleDecodeErr)
	}
	if !ruleResponse.Success || ruleResponse.Page != 1 || ruleResponse.PageSize != 10 {
		t.Fatalf("rule response=%+v", ruleResponse)
	}

	// importReq 是创建一条测试订单的请求。
	importReq := httptest.NewRequest(http.MethodPost, "/api/orders/import", strings.NewReader(`[{"order_id":"contract-order","item_id":"contract-item","status":"pending_ship","quantity":1,"amount":"1.00"}]`))
	importReq.Header.Set("Content-Type", "application/json")
	importReq.AddCookie(sessionCookie)
	// importRecorder 是捕获订单导入响应的记录器。
	importRecorder := httptest.NewRecorder()
	handler.ServeHTTP(importRecorder, importReq)
	if importRecorder.Code != http.StatusOK {
		t.Fatalf("import status=%d body=%s", importRecorder.Code, importRecorder.Body.String())
	}
	// importResponse 是订单导入具名响应 DTO。
	var importResponse importOrdersResponse
	// importDecodeErr 是订单导入响应 JSON 反序列化失败的原因。
	if importDecodeErr := json.Unmarshal(importRecorder.Body.Bytes(), &importResponse); importDecodeErr != nil {
		t.Fatalf("decode import response: %v", importDecodeErr)
	}
	if importResponse.SuccessCount != 1 || importResponse.Total != 1 {
		t.Fatalf("import response=%+v", importResponse)
	}

	// orderReq 是读取刚导入订单详情的请求。
	orderReq := httptest.NewRequest(http.MethodGet, "/api/orders/contract-order", nil)
	orderReq.AddCookie(sessionCookie)
	// orderRecorder 是捕获订单详情响应的记录器。
	orderRecorder := httptest.NewRecorder()
	handler.ServeHTTP(orderRecorder, orderReq)
	if orderRecorder.Code != http.StatusOK {
		t.Fatalf("order status=%d body=%s", orderRecorder.Code, orderRecorder.Body.String())
	}
	// orderResponse 是订单详情具名响应 DTO。
	var orderResponse orderDetailResponse
	// orderDecodeErr 是订单详情响应 JSON 反序列化失败的原因。
	if orderDecodeErr := json.Unmarshal(orderRecorder.Body.Bytes(), &orderResponse); orderDecodeErr != nil {
		t.Fatalf("decode order response: %v", orderDecodeErr)
	}
	if !orderResponse.Success || orderResponse.Data.OrderID != "contract-order" || orderResponse.OrderID != "contract-order" {
		t.Fatalf("order response=%+v", orderResponse)
	}
}
