package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// replyRouteCase 描述一个关键词回复或默认回复兼容路由测试样例。
type replyRouteCase struct {
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

// requestReplyRoute 发送带认证会话的回复规则请求并返回状态码。
func requestReplyRoute(t *testing.T, handler http.Handler, sessionCookie *http.Cookie, method, path, body string) int {
	t.Helper()
	// request 是当前关键词或默认回复兼容入口请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(sessionCookie)
	// recorder 是捕获兼容入口响应的记录器。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

// TestVersionedReplyRoutesPreserveLegacyContracts 验证关键词和默认回复入口复用旧 handler 与权限边界。
func TestVersionedReplyRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证关键词和默认回复路由的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// cases 是覆盖版本化与旧路径状态码兼容性的测试样例集合。
	cases := []replyRouteCase{
		{name: "keywords-list", method: http.MethodGet, versionedPath: "/api/v1/reply-rules/acc1", legacyPath: "/keywords/acc1", wantStatus: http.StatusOK},
		{name: "keywords-create-invalid", method: http.MethodPost, versionedPath: "/api/v1/reply-rules/acc1", legacyPath: "/keywords/acc1", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "keywords-item-list", method: http.MethodGet, versionedPath: "/api/v1/reply-rules/acc1/items", legacyPath: "/keywords-with-item-id/acc1", wantStatus: http.StatusOK},
		{name: "keywords-item-create-invalid", method: http.MethodPost, versionedPath: "/api/v1/reply-rules/acc1/items", legacyPath: "/keywords-with-item-id/acc1", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "keywords-typed-list", method: http.MethodGet, versionedPath: "/api/v1/reply-rules/acc1/typed", legacyPath: "/keywords-with-type/acc1", wantStatus: http.StatusOK},
		{name: "keywords-typed-update-invalid", method: http.MethodPut, versionedPath: "/api/v1/reply-rules/acc1/typed/invalid", legacyPath: "/keywords-with-type/acc1/invalid", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "keywords-typed-delete-invalid", method: http.MethodDelete, versionedPath: "/api/v1/reply-rules/acc1/typed/invalid", legacyPath: "/keywords-with-type/acc1/invalid", wantStatus: http.StatusBadRequest},
		{name: "keywords-index-delete-missing", method: http.MethodDelete, versionedPath: "/api/v1/reply-rules/missing/index/0", legacyPath: "/keywords/missing/0", wantStatus: http.StatusNotFound},
		{name: "item-replies-list", method: http.MethodGet, versionedPath: "/api/v1/reply-rules/items", legacyPath: "/itemReplays", wantStatus: http.StatusOK},
		{name: "item-reply-get-missing", method: http.MethodGet, versionedPath: "/api/v1/reply-rules/items/acc1/missing", legacyPath: "/item-reply/acc1/missing", wantStatus: http.StatusOK},
		{name: "item-reply-update-invalid", method: http.MethodPut, versionedPath: "/api/v1/reply-rules/items/acc1/item-1", legacyPath: "/item-reply/acc1/item-1", body: `not-json`, wantStatus: http.StatusBadRequest},
		{name: "item-reply-delete", method: http.MethodDelete, versionedPath: "/api/v1/reply-rules/items/acc1/missing", legacyPath: "/item-reply/acc1/missing", wantStatus: http.StatusOK},
		{name: "default-replies-map", method: http.MethodGet, versionedPath: "/api/v1/default-replies", legacyPath: "/api/default-replies", wantStatus: http.StatusOK},
		{name: "default-replies-list", method: http.MethodGet, versionedPath: "/api/v1/default-replies/list", legacyPath: "/default-replies", wantStatus: http.StatusOK},
		{name: "default-reply-get", method: http.MethodGet, versionedPath: "/api/v1/default-replies/acc1", legacyPath: "/api/default-reply/acc1", wantStatus: http.StatusOK},
		{name: "default-reply-update-invalid", method: http.MethodPut, versionedPath: "/api/v1/default-replies/acc1", legacyPath: "/api/default-reply/acc1", body: `not-json`, wantStatus: http.StatusBadRequest},
		{name: "default-reply-delete-missing", method: http.MethodDelete, versionedPath: "/api/v1/default-replies/missing", legacyPath: "/default-replies/missing", wantStatus: http.StatusNotFound},
		{name: "default-reply-clear-missing", method: http.MethodPost, versionedPath: "/api/v1/default-replies/missing/clear-records", legacyPath: "/api/default-reply/missing/clear-records", wantStatus: http.StatusNotFound},
	}
	for _, routeCase := range cases { // routeCase 是当前正在执行的回复规则路由样例。
		// versionedStatus 是版本化入口实际返回的状态码。
		versionedStatus := requestReplyRoute(t, handler, sessionCookie, routeCase.method, routeCase.versionedPath, routeCase.body)
		// legacyStatus 是旧兼容入口实际返回的状态码。
		legacyStatus := requestReplyRoute(t, handler, sessionCookie, routeCase.method, routeCase.legacyPath, routeCase.body)
		if versionedStatus != routeCase.wantStatus || legacyStatus != routeCase.wantStatus {
			t.Errorf("%s status versioned=%d legacy=%d want=%d", routeCase.name, versionedStatus, legacyStatus, routeCase.wantStatus)
		}
	}
}
