package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/httpapi"
	"xianyu-go/internal/xianyu/mtop"
)

// TestAPIContractChatAutomationAndItemErrors 验证聊天、自动化和商品失败均返回统一错误 DTO。
func TestAPIContractChatAutomationAndItemErrors(t *testing.T) {
	// srv 是用于验证聊天、自动化和商品错误契约的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 测试平台客户端返回缺少商品 ID 的发布结果。
	setTestMTop(srv, &stubPublishMTop{publish: func(context.Context, string, mtop.PublishItemRequest) (*mtop.PublishItemResult, error) {
		return &mtop.PublishItemResult{Title: "测试商品"}, nil
	}})
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	assertUnifiedAPIError(t, handler, http.MethodPost, "/api/chat/messages", `{}`, sessionCookie, http.StatusServiceUnavailable, httpapi.CodeServiceUnavailable, "聊天服务未启用", false)
	assertUnifiedAPIError(t, handler, http.MethodPost, "/api/account-tasks/acc1/run", `{"task_type":"auto_rate"}`, sessionCookie, http.StatusServiceUnavailable, httpapi.CodeServiceUnavailable, "自动化中心未启用", false)
	// body 和 contentType 是有效商品发布表单及其 multipart 类型。
	body, contentType := buildPublishMultipart(t, map[string]string{"cookie_id": "acc1", "title": "测试商品", "price": "12.50", "quantity": "1"})
	// req 是触发远端结果缺少商品 ID 的发布请求。
	req := httptest.NewRequest(http.MethodPost, "/items/publish", body)
	req.Header.Set("Content-Type", contentType)
	req.AddCookie(sessionCookie)
	// recorder 是捕获商品发布错误响应的测试记录器。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("publish status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// response 是商品发布失败的统一错误 DTO。
	var response httpapi.ErrorResponse
	// decodeErr 表示商品发布错误响应 JSON 反序列化失败的原因。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode publish error: %v", decodeErr)
	}
	if response.Code != "publish_result_missing_item_id" || response.Message == "" {
		t.Fatalf("response=%+v", response)
	}
	if response.Details != nil {
		t.Fatalf("unexpected details=%+v", response.Details)
	}
	if strings.Contains(recorder.Body.String(), `"success"`) {
		t.Fatalf("publish error contains legacy success field: %s", recorder.Body.String())
	}
}
