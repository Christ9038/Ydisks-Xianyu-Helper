package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/httpapi"
)

// TestAPIContractHealth 验证健康检查使用具名 DTO，并在正常状态返回完整构建信息。
func TestAPIContractHealth(t *testing.T) {
	// srv 是带可用 SQLite 数据库的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// req 是无需认证的健康检查请求。
	req := httptest.NewRequest(http.MethodGet, "/health", nil)
	// rec 是捕获健康检查响应的测试记录器。
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("health status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 是反序列化后的健康检查具名 DTO。
	var response healthResponse
	// decodeErr 表示健康检查响应 JSON 反序列化失败的原因。
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode health response: %v", decodeErr)
	}
	if response.Status != "ok" || response.Database != "ok" || response.Version == "" {
		t.Fatalf("health response=%+v", response)
	}
}

// TestAPIContractAuthenticationError 验证未认证请求返回统一错误 DTO 和请求追踪标识。
func TestAPIContractAuthenticationError(t *testing.T) {
	// srv 是用于验证认证边界的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// req 是没有 session cookie 的账号列表请求。
	req := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	// rec 是捕获认证失败响应的测试记录器。
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthorized status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 是认证失败的统一错误 DTO。
	var response httpapi.ErrorResponse
	// decodeErr 表示认证错误响应 JSON 反序列化失败的原因。
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode auth error: %v", decodeErr)
	}
	if response.Code != httpapi.CodeUnauthorized || response.Message != "未授权访问" || response.RequestID == "" {
		t.Fatalf("auth error response=%+v", response)
	}
	if strings.Contains(rec.Body.String(), `"detail"`) || strings.Contains(rec.Body.String(), `"msg"`) {
		t.Fatalf("auth response contains legacy error alias: %s", rec.Body.String())
	}
}

// TestAPIContractLoginFailure 验证错误密码不再使用 HTTP 200 加 success=false 表示失败。
func TestAPIContractLoginFailure(t *testing.T) {
	// srv 是用于验证登录失败契约的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// req 是使用错误密码的登录请求。
	req := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"username":"admin","password":"wrong"}`))
	// rec 是捕获登录失败响应的测试记录器。
	rec := httptest.NewRecorder()
	srv.Router().ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("login failure status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 是登录失败的统一错误 DTO。
	var response httpapi.ErrorResponse
	// decodeErr 表示登录错误响应 JSON 反序列化失败的原因。
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode login error: %v", decodeErr)
	}
	if response.Code != "authentication_failed" || response.Message != "用户名或密码错误" {
		t.Fatalf("login error response=%+v", response)
	}
}

// TestAPIContractAccountList 验证账号列表返回具名非敏感 DTO，且不暴露 Cookie 或密码字段。
func TestAPIContractAccountList(t *testing.T) {
	// srv 是带模板账号数据的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// req 是读取账号非敏感详情的认证请求。
	req := httptest.NewRequest(http.MethodGet, "/cookies/details", nil)
	req.AddCookie(sessionCookie)
	// rec 是捕获账号列表响应的测试记录器。
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("account list status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 是账号列表具名 DTO 集合。
	var response []cookieSummaryResponse
	// decodeErr 表示账号列表响应 JSON 反序列化失败的原因。
	if decodeErr := json.Unmarshal(rec.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode account list: %v", decodeErr)
	}
	if len(response) == 0 || response[0].ID == "" || !response[0].HasCookie {
		t.Fatalf("account list response=%+v", response)
	}
	if strings.Contains(rec.Body.String(), `"value"`) || strings.Contains(rec.Body.String(), `"password"`) {
		t.Fatalf("account list exposes credential field: %s", rec.Body.String())
	}
}
