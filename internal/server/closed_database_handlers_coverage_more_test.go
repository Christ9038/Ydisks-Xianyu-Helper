package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestClosedDatabaseHandlersCoverReadErrorMappings 验证默认回复、通知、关键词和用户设置查询在数据库关闭后的统一错误响应。
func TestClosedDatabaseHandlersCoverReadErrorMappings(t *testing.T) {
	// server、store、cleanup 保存关闭数据库前后共用的测试服务器及资源清理函数。
	server, store, cleanup := newTestServer(t)
	defer cleanup()
	// closeErr 保存关闭测试数据库连接的结果。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// cases 保存关闭数据库后应返回内部错误的只读 Handler 集合。
	cases := []struct {
		name string
		hand http.HandlerFunc
		req  *http.Request
	}{
		{name: "default replies", hand: server.listDefaultReplies, req: requestWithServerSession(http.MethodGet, "/default-replies", nil)},
		{name: "default reply map", hand: server.listDefaultRepliesMap, req: requestWithServerSession(http.MethodGet, "/api/default-replies", nil)},
		{name: "channels", hand: server.listChannels, req: requestWithServerSession(http.MethodGet, "/notification-channels", nil)},
		{name: "message notifications", hand: server.listMessageNotifications, req: requestWithServerSession(http.MethodGet, "/message-notifications", nil)},
		{name: "keywords", hand: server.listKeywords, req: requestWithServerSession(http.MethodGet, "/keywords/cid", map[string]string{"cid": "cid"})},
		{name: "user settings", hand: server.listUserSettings, req: requestWithServerSession(http.MethodGet, "/user-settings", nil)},
	}
	// testCase 表示当前关闭数据库的只读 Handler 用例。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// recorder 保存当前 Handler 的 HTTP 响应。
			recorder := httptest.NewRecorder()
			testCase.hand(recorder, testCase.req)
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("关闭数据库 Handler status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	// getSettingRecorder 保存用户设置单键查询在数据库关闭后的兼容空值响应。
	getSettingRecorder := httptest.NewRecorder()
	// getSettingRequest 保存用户设置单键查询请求。
	getSettingRequest := requestWithServerSession(http.MethodGet, "/user-settings/missing", nil)
	server.getUserSetting(getSettingRecorder, getSettingRequest)
	if getSettingRecorder.Code != http.StatusOK {
		t.Fatalf("关闭数据库用户设置单键查询 status=%d", getSettingRecorder.Code)
	}
}
