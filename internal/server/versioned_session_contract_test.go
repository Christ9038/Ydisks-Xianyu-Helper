package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestVersionedSessionRoutesPreserveLegacyContracts 验证版本化会话入口复用旧 handler 并保留旧路径。
func TestVersionedSessionRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证版本化会话路由的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// loginBody 是版本化登录请求体。
	loginBody := `{"username":"admin","password":"pw"}`
	// loginReq 是通过版本化入口登录的请求。
	loginReq := httptest.NewRequest(http.MethodPost, "/api/v1/session/login", strings.NewReader(loginBody))
	// loginRecorder 是捕获版本化登录响应的记录器。
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginReq)
	if loginRecorder.Code != http.StatusOK {
		t.Fatalf("versioned login status=%d body=%s", loginRecorder.Code, loginRecorder.Body.String())
	}
	// loginResponseValue 是版本化登录响应 DTO。
	var loginResponseValue loginResponse
	// loginDecodeErr 是版本化登录响应反序列化失败的原因。
	if loginDecodeErr := json.Unmarshal(loginRecorder.Body.Bytes(), &loginResponseValue); loginDecodeErr != nil {
		t.Fatalf("decode versioned login response: %v", loginDecodeErr)
	}
	if !loginResponseValue.Success || loginResponseValue.Username != "admin" {
		t.Fatalf("versioned login response=%+v", loginResponseValue)
	}
	// sessionCookie 是版本化登录成功后下发的会话 Cookie。
	sessionCookie := loginRecorder.Result().Cookies()[0]

	// verifyReq 是通过版本化入口校验会话的请求。
	verifyReq := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	verifyReq.AddCookie(sessionCookie)
	// verifyRecorder 是捕获版本化会话校验响应的记录器。
	verifyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(verifyRecorder, verifyReq)
	if verifyRecorder.Code != http.StatusOK {
		t.Fatalf("versioned verify status=%d body=%s", verifyRecorder.Code, verifyRecorder.Body.String())
	}
	// verifyResponseValue 是版本化会话校验响应 DTO。
	var verifyResponseValue sessionVerificationResponse
	// verifyDecodeErr 是版本化会话校验响应反序列化失败的原因。
	if verifyDecodeErr := json.Unmarshal(verifyRecorder.Body.Bytes(), &verifyResponseValue); verifyDecodeErr != nil {
		t.Fatalf("decode versioned verify response: %v", verifyDecodeErr)
	}
	if !verifyResponseValue.Authenticated || verifyResponseValue.Username != "admin" {
		t.Fatalf("versioned verify response=%+v", verifyResponseValue)
	}

	// legacyVerifyReq 是通过旧入口复核兼容性的请求。
	legacyVerifyReq := httptest.NewRequest(http.MethodGet, "/verify", nil)
	legacyVerifyReq.AddCookie(sessionCookie)
	// legacyVerifyRecorder 是捕获旧入口响应的记录器。
	legacyVerifyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyVerifyRecorder, legacyVerifyReq)
	if legacyVerifyRecorder.Code != http.StatusOK {
		t.Fatalf("legacy verify status=%d body=%s", legacyVerifyRecorder.Code, legacyVerifyRecorder.Body.String())
	}

	// logoutReq 是通过版本化入口注销会话的请求。
	logoutReq := httptest.NewRequest(http.MethodPost, "/api/v1/session/logout", nil)
	logoutReq.AddCookie(sessionCookie)
	// logoutRecorder 是捕获版本化注销响应的记录器。
	logoutRecorder := httptest.NewRecorder()
	handler.ServeHTTP(logoutRecorder, logoutReq)
	if logoutRecorder.Code != http.StatusOK {
		t.Fatalf("versioned logout status=%d body=%s", logoutRecorder.Code, logoutRecorder.Body.String())
	}

	// legacyLoginReq 是验证旧登录入口仍可用的请求。
	legacyLoginReq := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(loginBody))
	// legacyLoginRecorder 是捕获旧登录响应的记录器。
	legacyLoginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyLoginRecorder, legacyLoginReq)
	if legacyLoginRecorder.Code != http.StatusOK {
		t.Fatalf("legacy login status=%d body=%s", legacyLoginRecorder.Code, legacyLoginRecorder.Body.String())
	}
}
