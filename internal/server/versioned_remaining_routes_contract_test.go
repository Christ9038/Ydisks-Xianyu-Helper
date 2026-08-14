package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// remainingVersionedRouteCase 描述一个剩余公共版本化入口测试样例。
type remainingVersionedRouteCase struct {
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

// requestRemainingVersionedRoute 发送带认证会话的公共请求并返回状态码。
func requestRemainingVersionedRoute(t *testing.T, handler http.Handler, sessionCookie *http.Cookie, method, path, body string) int {
	t.Helper()
	// request 是当前剩余公共兼容入口请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(sessionCookie)
	// recorder 是捕获公共兼容入口响应的记录器。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

// TestVersionedRemainingRoutesPreserveLegacyContracts 验证剩余公共调用方版本化入口复用旧 handler 和权限边界。
func TestVersionedRemainingRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证剩余公共版本化路由的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// cases 是覆盖会话、账号、密码登录、自动化、订单和商品入口的测试样例集合。
	cases := []remainingVersionedRouteCase{
		{name: "change-password", method: http.MethodPost, versionedPath: "/api/v1/session/password", legacyPath: "/change-password", body: `{"current_password":"wrong","new_password":"new-password"}`, wantStatus: http.StatusUnauthorized},
		{name: "update-credentials", method: http.MethodPut, versionedPath: "/api/v1/session/credentials", legacyPath: "/account/credentials", body: `{"current_password":"wrong","new_username":"new-user"}`, wantStatus: http.StatusUnauthorized},
		{name: "change-admin-password", method: http.MethodPost, versionedPath: "/api/v1/admin/password", legacyPath: "/change-admin-password", body: `{"current_password":"wrong","new_password":"new-password"}`, wantStatus: http.StatusUnauthorized},
		{name: "delete-account", method: http.MethodDelete, versionedPath: "/api/v1/accounts/missing-account", legacyPath: "/cookies/missing-account", wantStatus: http.StatusNotFound},
		{name: "password-login", method: http.MethodPost, versionedPath: "/api/v1/password-login", legacyPath: "/password-login", body: `{}`, wantStatus: http.StatusNotImplemented},
		{name: "password-login-check", method: http.MethodGet, versionedPath: "/api/v1/password-login/check/missing-session", legacyPath: "/password-login/check/missing-session", wantStatus: http.StatusNotImplemented},
		{name: "password-login-cancel", method: http.MethodDelete, versionedPath: "/api/v1/password-login/cancel/missing-session", legacyPath: "/password-login/cancel/missing-session", wantStatus: http.StatusNotImplemented},
		{name: "automation-rules", method: http.MethodGet, versionedPath: "/api/v1/automation-rules", legacyPath: "/automation-rules", wantStatus: http.StatusOK},
		{name: "automation-issues", method: http.MethodGet, versionedPath: "/api/v1/automation-issues", legacyPath: "/automation-issues", wantStatus: http.StatusOK},
		{name: "automation-run-resolve", method: http.MethodPost, versionedPath: "/api/v1/automation-runs/invalid/resolve", legacyPath: "/automation-runs/invalid/resolve", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "automation-task-resolve", method: http.MethodPost, versionedPath: "/api/v1/automation-pending-tasks/invalid/resolve", legacyPath: "/automation-pending-tasks/invalid/resolve", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "delete-order", method: http.MethodDelete, versionedPath: "/api/v1/orders/missing-order", legacyPath: "/api/orders/missing-order", wantStatus: http.StatusNotFound},
		{name: "create-item", method: http.MethodPost, versionedPath: "/api/v1/items/missing-account", legacyPath: "/items/missing-account", body: `{}`, wantStatus: http.StatusNotFound},
	}
	for _, routeCase := range cases { // routeCase 是当前正在执行的剩余公共路由样例。
		// versionedStatus 是版本化入口实际返回的状态码。
		versionedStatus := requestRemainingVersionedRoute(t, handler, sessionCookie, routeCase.method, routeCase.versionedPath, routeCase.body)
		// legacyStatus 是旧兼容入口实际返回的状态码。
		legacyStatus := requestRemainingVersionedRoute(t, handler, sessionCookie, routeCase.method, routeCase.legacyPath, routeCase.body)
		if versionedStatus != routeCase.wantStatus || legacyStatus != routeCase.wantStatus {
			t.Errorf("%s status versioned=%d legacy=%d want=%d", routeCase.name, versionedStatus, legacyStatus, routeCase.wantStatus)
		}
	}
}
