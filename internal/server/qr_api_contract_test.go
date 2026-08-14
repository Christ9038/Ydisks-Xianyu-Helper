package server

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"xianyu-go/internal/httpapi"
)

// TestAPIContractQRLoginGenerationFailure 验证二维码生成上游失败返回非 2xx 统一错误 DTO。
func TestAPIContractQRLoginGenerationFailure(t *testing.T) {
	// srv 是用于验证二维码生成错误契约的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.QRLogin = &fakeQRLoginService{generateErr: errors.New("二维码服务不可用")}
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// req 是触发二维码上游失败的生成请求。
	req := httptest.NewRequest(http.MethodPost, "/qr-login/generate", nil)
	req.AddCookie(sessionCookie)
	// recorder 是捕获二维码错误响应的测试记录器。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// response 是二维码生成失败的统一错误 DTO。
	var response httpapi.ErrorResponse
	// decodeErr 表示二维码错误响应 JSON 反序列化失败的原因。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode response: %v", decodeErr)
	}
	if response.Code != "qr_login_generate_failed" || response.Message == "" {
		t.Fatalf("response=%+v", response)
	}
}

// TestAPIContractQRLoginPersistFailure 验证扫码成功但本地持久化失败返回统一内部错误。
func TestAPIContractQRLoginPersistFailure(t *testing.T) {
	// srv 是用于验证扫码持久化错误契约的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.Manager = nil
	srv.QRLogin = &fakeQRLoginService{status: map[string]any{"status": "success"}}
	ownQRSession(t, srv, store, "persist-failure")
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// req 是触发扫码结果缺少凭证的状态查询请求。
	req := httptest.NewRequest(http.MethodGet, "/qr-login/status/persist-failure", nil)
	req.AddCookie(sessionCookie)
	// recorder 是捕获扫码持久化错误响应的测试记录器。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// response 是扫码持久化失败的统一错误 DTO。
	var response httpapi.ErrorResponse
	// decodeErr 表示扫码持久化错误响应 JSON 反序列化失败的原因。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode response: %v", decodeErr)
	}
	if response.Code != "qr_login_persist_failed" || response.Message == "" {
		t.Fatalf("response=%+v", response)
	}
}

// TestAPIContractQRVerificationFailure 验证风控验证上游失败返回统一网关错误。
func TestAPIContractQRVerificationFailure(t *testing.T) {
	// srv 是用于验证扫码风控错误契约的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.QRLogin = &fakeQRLoginService{completeErr: errors.New("验证服务不可用")}
	ownQRSession(t, srv, store, "verification-failure")
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// req 是触发风控验证上游失败的请求。
	req := httptest.NewRequest(http.MethodPost, "/qr-login/complete-verification/verification-failure", nil)
	req.AddCookie(sessionCookie)
	// recorder 是捕获风控验证错误响应的测试记录器。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, req)
	if recorder.Code != http.StatusBadGateway {
		t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// response 是风控验证失败的统一错误 DTO。
	var response httpapi.ErrorResponse
	// decodeErr 表示风控验证错误响应 JSON 反序列化失败的原因。
	if decodeErr := json.Unmarshal(recorder.Body.Bytes(), &response); decodeErr != nil {
		t.Fatalf("decode response: %v", decodeErr)
	}
	if response.Code != "qr_verification_failed" || response.Message == "" {
		t.Fatalf("response=%+v", response)
	}
}

// TestAPIContractPasswordLoginDisabled 验证历史密码登录兼容入口使用明确的未实现错误。
func TestAPIContractPasswordLoginDisabled(t *testing.T) {
	// srv 是用于验证密码登录禁用契约的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	assertUnifiedAPIError(t, handler, http.MethodPost, "/password-login", `{}`, sessionCookie, http.StatusNotImplemented, "password_login_disabled", "Go 客户端仅支持扫码登录，密码登录已禁用", false)
}
