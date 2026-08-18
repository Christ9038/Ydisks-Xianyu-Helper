package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// qrLoginRouteCase 描述一个二维码登录版本化兼容入口测试样例。
type qrLoginRouteCase struct {
	// name 是测试样例名称。
	name string
	// method 是请求使用的 HTTP 方法。
	method string
	// versionedPath 是版本化入口路径。
	versionedPath string
	// legacyPath 是旧兼容入口路径。
	legacyPath string
	// body 是两条入口共用的请求体。
	body string
	// wantStatus 是两条入口应返回的状态码。
	wantStatus int
}

// requestQRLoginRoute 发送带认证会话的二维码请求并返回状态码。
func requestQRLoginRoute(t *testing.T, handler http.Handler, sessionCookie *http.Cookie, method, path, body string) int {
	t.Helper()
	// request 是当前二维码兼容入口请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(sessionCookie)
	// recorder 是捕获二维码响应的测试记录器。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

// TestVersionedQRLoginRoutesPreserveLegacyContracts 验证二维码登录版本化入口复用旧 handler 和认证边界。
func TestVersionedQRLoginRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证二维码登录版本化路由的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	setTestQRLogin(srv, &fakeQRLoginService{status: map[string]any{"status": "waiting"}})
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// ownQRSession 预先建立状态查询所需的会话所有权记录。
	ownQRSession(t, srv, store, "versioned-qr-session")
	// cases 是覆盖二维码生成、状态查询、状态持久化和验证完成入口的测试样例集合。
	cases := []qrLoginRouteCase{
		{name: "generate", method: http.MethodPost, versionedPath: "/api/v1/qr-login/generate", legacyPath: "/qr-login/generate", wantStatus: http.StatusOK},
		{name: "check", method: http.MethodGet, versionedPath: "/api/v1/qr-login/check/versioned-qr-session", legacyPath: "/qr-login/check/versioned-qr-session", wantStatus: http.StatusOK},
		{name: "status", method: http.MethodGet, versionedPath: "/api/v1/qr-login/status/versioned-qr-session", legacyPath: "/qr-login/status/versioned-qr-session", wantStatus: http.StatusOK},
		{name: "complete-verification", method: http.MethodPost, versionedPath: "/api/v1/qr-login/complete-verification/no-such-session", legacyPath: "/qr-login/complete-verification/no-such-session", wantStatus: http.StatusNotFound},
	}
	for _, routeCase := range cases { // routeCase 是当前正在执行的二维码路由样例。
		// versionedStatus 是版本化入口实际返回的状态码。
		versionedStatus := requestQRLoginRoute(t, handler, sessionCookie, routeCase.method, routeCase.versionedPath, routeCase.body)
		// legacyStatus 是旧兼容入口实际返回的状态码。
		legacyStatus := requestQRLoginRoute(t, handler, sessionCookie, routeCase.method, routeCase.legacyPath, routeCase.body)
		if versionedStatus != routeCase.wantStatus || legacyStatus != routeCase.wantStatus {
			t.Errorf("%s status versioned=%d legacy=%d want=%d", routeCase.name, versionedStatus, legacyStatus, routeCase.wantStatus)
		}
	}

	// createErr 是创建普通用户测试账号时返回的错误。
	if _, createErr := store.Users.Create(context.Background(), "qr-route-user", "qr-route-user@example.com", "pw"); createErr != nil {
		t.Fatalf("create non-admin user: %v", createErr)
	}
	// userCookie 是普通用户登录后得到的会话，用于验证二维码入口仍要求认证但不额外要求管理员权限。
	userCookie := loginAsHelper(t, handler, "qr-route-user", "pw")
	// status 是普通用户读取其他用户二维码会话时返回的状态码。
	status := requestQRLoginRoute(t, handler, userCookie, http.MethodGet, "/api/v1/qr-login/check/versioned-qr-session", "")
	if status != http.StatusNotFound {
		t.Fatalf("non-owner versioned QR status=%d want=%d", status, http.StatusNotFound)
	}
}
