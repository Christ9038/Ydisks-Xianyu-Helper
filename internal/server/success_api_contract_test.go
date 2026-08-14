package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
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
