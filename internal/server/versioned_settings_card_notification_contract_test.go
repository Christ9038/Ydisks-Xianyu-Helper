package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// settingsCardNotificationRouteCase 描述一个设置、卡券或通知兼容路由测试样例。
type settingsCardNotificationRouteCase struct {
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

// requestSettingsCardNotificationRoute 发送带认证会话的兼容请求并返回状态码。
func requestSettingsCardNotificationRoute(t *testing.T, handler http.Handler, sessionCookie *http.Cookie, method, path, body string) int {
	t.Helper()
	// request 是当前设置、卡券或通知兼容入口请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.AddCookie(sessionCookie)
	// recorder 是捕获兼容入口响应的记录器。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	return recorder.Code
}

// TestVersionedSettingsCardNotificationRoutesPreserveLegacyContracts 验证三类领域入口复用旧 handler 和权限边界。
func TestVersionedSettingsCardNotificationRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证设置、卡券和通知路由的 HTTP 测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)
	// cases 是覆盖版本化与旧路径状态码兼容性的测试样例集合。
	cases := []settingsCardNotificationRouteCase{
		{name: "public-system-settings", method: http.MethodGet, versionedPath: "/api/v1/settings/system/public", legacyPath: "/system-settings/public", wantStatus: http.StatusOK},
		{name: "system-settings-list", method: http.MethodGet, versionedPath: "/api/v1/settings/system", legacyPath: "/system-settings", wantStatus: http.StatusOK},
		{name: "system-settings-bulk", method: http.MethodPut, versionedPath: "/api/v1/settings/system", legacyPath: "/system-settings", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "system-settings-key", method: http.MethodPut, versionedPath: "/api/v1/settings/system/theme_color", legacyPath: "/system-settings/theme_color", body: `{"value":"blue"}`, wantStatus: http.StatusOK},
		{name: "ai-models-invalid", method: http.MethodPost, versionedPath: "/api/v1/settings/ai-models", legacyPath: "/ai-models", body: `not-json`, wantStatus: http.StatusBadRequest},
		{name: "ai-reply-list", method: http.MethodGet, versionedPath: "/api/v1/settings/ai-reply", legacyPath: "/ai-reply-settings", wantStatus: http.StatusOK},
		{name: "ai-reply-missing", method: http.MethodGet, versionedPath: "/api/v1/settings/ai-reply/missing", legacyPath: "/ai-reply-settings/missing", wantStatus: http.StatusNotFound},
		{name: "ai-reply-update-missing", method: http.MethodPut, versionedPath: "/api/v1/settings/ai-reply/missing", legacyPath: "/ai-reply-settings/missing", body: `{}`, wantStatus: http.StatusNotFound},
		{name: "user-settings-list", method: http.MethodGet, versionedPath: "/api/v1/settings/user", legacyPath: "/user-settings", wantStatus: http.StatusOK},
		{name: "user-setting-missing", method: http.MethodGet, versionedPath: "/api/v1/settings/user/theme", legacyPath: "/user-settings/theme", wantStatus: http.StatusOK},
		{name: "user-setting-update", method: http.MethodPut, versionedPath: "/api/v1/settings/user/theme", legacyPath: "/user-settings/theme", body: `{"value":"blue"}`, wantStatus: http.StatusOK},
		{name: "cards-list", method: http.MethodGet, versionedPath: "/api/v1/cards", legacyPath: "/cards", wantStatus: http.StatusOK},
		{name: "cards-create-invalid", method: http.MethodPost, versionedPath: "/api/v1/cards", legacyPath: "/cards", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "cards-batch-invalid", method: http.MethodPost, versionedPath: "/api/v1/cards/batch", legacyPath: "/cards/batch", wantStatus: http.StatusBadRequest},
		{name: "card-details-invalid", method: http.MethodGet, versionedPath: "/api/v1/cards/invalid/details", legacyPath: "/cards/invalid/details", wantStatus: http.StatusBadRequest},
		{name: "card-get-invalid", method: http.MethodGet, versionedPath: "/api/v1/cards/invalid", legacyPath: "/cards/invalid", wantStatus: http.StatusBadRequest},
		{name: "card-update-invalid", method: http.MethodPut, versionedPath: "/api/v1/cards/invalid", legacyPath: "/cards/invalid", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "card-delete-invalid", method: http.MethodDelete, versionedPath: "/api/v1/cards/invalid", legacyPath: "/cards/invalid", wantStatus: http.StatusBadRequest},
		{name: "card-append-invalid", method: http.MethodPost, versionedPath: "/api/v1/cards/invalid/append-data", legacyPath: "/cards/invalid/append-data", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "notification-channels-list", method: http.MethodGet, versionedPath: "/api/v1/notifications/channels", legacyPath: "/notification-channels", wantStatus: http.StatusOK},
		{name: "notification-channel-create-invalid", method: http.MethodPost, versionedPath: "/api/v1/notifications/channels", legacyPath: "/notification-channels", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "notification-channel-update-invalid", method: http.MethodPut, versionedPath: "/api/v1/notifications/channels/invalid", legacyPath: "/notification-channels/invalid", body: `{}`, wantStatus: http.StatusBadRequest},
		{name: "notification-channel-delete-invalid", method: http.MethodDelete, versionedPath: "/api/v1/notifications/channels/invalid", legacyPath: "/notification-channels/invalid", wantStatus: http.StatusBadRequest},
		{name: "notification-channel-test-invalid", method: http.MethodPost, versionedPath: "/api/v1/notifications/channels/invalid/test", legacyPath: "/notification-channels/invalid/test", wantStatus: http.StatusBadRequest},
		{name: "message-notifications-list", method: http.MethodGet, versionedPath: "/api/v1/notifications/messages", legacyPath: "/message-notifications", wantStatus: http.StatusOK},
		{name: "message-notification-delete-invalid", method: http.MethodDelete, versionedPath: "/api/v1/notifications/messages/invalid", legacyPath: "/message-notifications/invalid", wantStatus: http.StatusBadRequest},
		{name: "account-notifications-delete", method: http.MethodDelete, versionedPath: "/api/v1/notifications/messages/account/acc1", legacyPath: "/message-notifications/account/acc1", wantStatus: http.StatusOK},
		{name: "account-bindings-missing", method: http.MethodGet, versionedPath: "/api/v1/notifications/accounts/missing/bindings", legacyPath: "/message-notifications/missing", wantStatus: http.StatusNotFound},
		{name: "account-bindings-update-missing", method: http.MethodPost, versionedPath: "/api/v1/notifications/accounts/missing/bindings", legacyPath: "/message-notifications/missing", body: `{}`, wantStatus: http.StatusNotFound},
	}
	for _, routeCase := range cases { // routeCase 是当前正在执行的设置、卡券或通知路由样例。
		// versionedStatus 是版本化入口实际返回的状态码。
		versionedStatus := requestSettingsCardNotificationRoute(t, handler, sessionCookie, routeCase.method, routeCase.versionedPath, routeCase.body)
		// legacyStatus 是旧兼容入口实际返回的状态码。
		legacyStatus := requestSettingsCardNotificationRoute(t, handler, sessionCookie, routeCase.method, routeCase.legacyPath, routeCase.body)
		if versionedStatus != routeCase.wantStatus || legacyStatus != routeCase.wantStatus {
			t.Errorf("%s status versioned=%d legacy=%d want=%d", routeCase.name, versionedStatus, legacyStatus, routeCase.wantStatus)
		}
	}
}
