package server

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// adminAnalyticsRouteCase 描述一个管理员或统计分析兼容路由测试样例。
type adminAnalyticsRouteCase struct {
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

// requestAdminAnalyticsRoute 发送带认证会话的管理员或统计请求并返回状态码。
func requestAdminAnalyticsRoute(t *testing.T, handler http.Handler, sessionCookie *http.Cookie, method, path, body string) int {
	t.Helper()
	// request 是当前管理员或统计分析兼容入口请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(sessionCookie)
	// recorder 是捕获兼容入口响应的记录器。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if strings.HasPrefix(path, "/api/v1/") && recorder.Code >= http.StatusOK && recorder.Code < http.StatusMultipleChoices {
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	}
	return recorder.Code
}

// TestVersionedAdminAnalyticsRoutesPreserveLegacyContracts 验证管理员与统计入口复用旧 handler 和权限边界。
func TestVersionedAdminAnalyticsRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证管理员与统计分析路由的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// cases 是覆盖版本化与旧路径状态码兼容性的测试样例集合。
	cases := []adminAnalyticsRouteCase{
		{name: "admin-users", method: http.MethodGet, versionedPath: "/api/v1/admin/users", legacyPath: "/admin/users", wantStatus: http.StatusOK},
		{name: "admin-cookies", method: http.MethodGet, versionedPath: "/api/v1/admin/cookies", legacyPath: "/admin/cookies", wantStatus: http.StatusOK},
		{name: "admin-stats", method: http.MethodGet, versionedPath: "/api/v1/admin/stats", legacyPath: "/admin/stats", wantStatus: http.StatusOK},
		{name: "admin-delete-invalid", method: http.MethodDelete, versionedPath: "/api/v1/admin/users/invalid", legacyPath: "/admin/users/invalid", wantStatus: http.StatusBadRequest},
		{name: "dashboard-stats", method: http.MethodGet, versionedPath: "/api/v1/analytics/dashboard", legacyPath: "/dashboard/stats", wantStatus: http.StatusOK},
		{name: "order-analytics", method: http.MethodGet, versionedPath: "/api/v1/analytics/orders?start_date=2026-01-01&end_date=2026-01-02", legacyPath: "/analytics/orders?start_date=2026-01-01&end_date=2026-01-02", wantStatus: http.StatusOK},
		{name: "valid-orders", method: http.MethodGet, versionedPath: "/api/v1/analytics/orders/valid?start_date=2026-01-01&end_date=2026-01-02", legacyPath: "/analytics/orders/valid?start_date=2026-01-01&end_date=2026-01-02", wantStatus: http.StatusOK},
	}
	for _, routeCase := range cases { // routeCase 是当前正在执行的管理员或统计路由样例。
		// versionedStatus 是版本化入口实际返回的状态码。
		versionedStatus := requestAdminAnalyticsRoute(t, handler, sessionCookie, routeCase.method, routeCase.versionedPath, routeCase.body)
		// legacyStatus 是旧兼容入口实际返回的状态码。
		legacyStatus := requestAdminAnalyticsRoute(t, handler, sessionCookie, routeCase.method, routeCase.legacyPath, routeCase.body)
		if versionedStatus != routeCase.wantStatus || legacyStatus != routeCase.wantStatus {
			t.Errorf("%s status versioned=%d legacy=%d want=%d", routeCase.name, versionedStatus, legacyStatus, routeCase.wantStatus)
		}
	}

	// createErr 是创建普通用户测试账号时返回的错误。
	if _, createErr := store.Users.Create(context.Background(), "analytics-user", "analytics-user@example.com", "pw"); createErr != nil {
		t.Fatalf("create non-admin user: %v", createErr)
	}
	// userCookie 是普通用户登录后得到的会话，用于验证管理员入口拒绝越权访问。
	userCookie := loginAsHelper(t, handler, "analytics-user", "pw")
	for _, path := range []string{"/api/v1/admin/stats", "/admin/stats"} { // path 是当前待验证的管理员统计入口。
		if status := requestAdminAnalyticsRoute(t, handler, userCookie, http.MethodGet, path, ""); status != http.StatusForbidden {
			t.Errorf("non-admin path=%s status=%d want=%d", path, status, http.StatusForbidden)
		}
	}
}
