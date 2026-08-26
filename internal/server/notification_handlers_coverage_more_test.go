package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	notificationsapp "xianyu-go/internal/application/notifications"
)

// notificationHandlerCoveragePort 为通知 Handler 提供可编程的渠道、绑定和不确定通知结果。
type notificationHandlerCoveragePort struct {
	// NotificationChannelsPort 提供当前场景未覆盖方法的默认能力。
	NotificationChannelsPort
	// UncertainNotificationsPort 提供不确定通知查询的默认能力。
	UncertainNotificationsPort
	// operationErr 保存绑定或渠道写入操作错误。
	operationErr error
	// testErr 保存通知渠道测试发送错误。
	testErr error
	// channelErr 保存渠道列表或渠道写入查询错误。
	channelErr error
	// bindingErr 保存绑定列表和账号绑定查询错误。
	bindingErr error
	// uncertainErr 保存不确定通知查询错误。
	uncertainErr error
}

// ListForUser 返回测试预置的不确定通知用户列表或错误。
func (port *notificationHandlerCoveragePort) ListForUser(context.Context, int64, int) ([]notificationsapp.UncertainSummary, int, error) {
	return []notificationsapp.UncertainSummary{{ID: 1, ChannelID: 2, EventType: "paid", HasError: true}}, 1, port.uncertainErr
}

// ListForAdmin 返回测试预置的不确定通知管理员列表或错误。
func (port *notificationHandlerCoveragePort) ListForAdmin(context.Context, int) ([]notificationsapp.UncertainSummary, int, error) {
	return []notificationsapp.UncertainSummary{{ID: 1, ChannelID: 2, EventType: "paid", HasError: true}}, 1, port.uncertainErr
}

// ListChannels 返回测试预置的非敏感渠道摘要或错误。
func (port *notificationHandlerCoveragePort) ListChannels(context.Context, int64) ([]notificationsapp.ChannelSummary, error) {
	return []notificationsapp.ChannelSummary{{ID: 2, Name: "webhook", Type: "webhook", EventTypes: "paid", Enabled: true}}, port.channelErr
}

// CreateChannel 返回固定渠道标识或测试预置错误。
func (port *notificationHandlerCoveragePort) CreateChannel(context.Context, int64, notificationsapp.ChannelInput) (int64, error) {
	return 2, port.operationErr
}

// UpdateChannel 返回测试预置的渠道更新错误。
func (port *notificationHandlerCoveragePort) UpdateChannel(context.Context, int64, int64, notificationsapp.ChannelPatch) error {
	return port.operationErr
}

// DeleteChannel 返回测试预置的渠道删除错误。
func (port *notificationHandlerCoveragePort) DeleteChannel(context.Context, int64, int64) error {
	return port.operationErr
}

// TestChannel 返回测试预置的渠道测试发送错误。
func (port *notificationHandlerCoveragePort) TestChannel(context.Context, int64, int64, time.Time) error {
	return port.testErr
}

// ListBindings 返回测试预置的账号绑定摘要或错误。
func (port *notificationHandlerCoveragePort) ListBindings(context.Context, int64) ([]notificationsapp.BindingSummary, error) {
	return []notificationsapp.BindingSummary{{ID: 3, CookieID: "cid", ChannelID: 2, ChannelName: "webhook", Enabled: true}}, port.bindingErr
}

// GetBindingIDs 返回测试预置的账号绑定标识或错误。
func (port *notificationHandlerCoveragePort) GetBindingIDs(context.Context, int64, string) ([]int64, error) {
	return []int64{2}, port.bindingErr
}

// SetBindings 返回测试预置的批量绑定更新错误。
func (port *notificationHandlerCoveragePort) SetBindings(context.Context, int64, string, []int64) error {
	return port.operationErr
}

// SetSingleBinding 返回测试预置的单条绑定更新错误。
func (port *notificationHandlerCoveragePort) SetSingleBinding(context.Context, int64, string, int64, bool) error {
	return port.operationErr
}

// DeleteBinding 返回测试预置的单条绑定删除错误。
func (port *notificationHandlerCoveragePort) DeleteBinding(context.Context, int64, int64) error {
	return port.operationErr
}

// DeleteAccountBindings 返回测试预置的账号绑定删除错误。
func (port *notificationHandlerCoveragePort) DeleteAccountBindings(context.Context, int64, string) error {
	return port.operationErr
}

// TestNotificationHandlersCoverChannelAndBindingMappings 覆盖通知渠道测试、账号绑定及不确定通知查询的业务映射。
func TestNotificationHandlersCoverChannelAndBindingMappings(t *testing.T) {
	// server、cleanup 保存通知 Handler 使用的测试服务器及资源清理函数。
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 保存当前注入的通知应用 Port。
	port := &notificationHandlerCoveragePort{}
	server.applications.notificationChannels = port
	server.applications.uncertainNotifications = port
	// channelParams 保存通知渠道和账号绑定路由参数。
	channelParams := map[string]string{"channel_id": "2", "cid": "cid", "notification_id": "3"}
	// successCases 保存通知 Handler 成功场景。
	successCases := []struct {
		name string
		hand http.HandlerFunc
		req  *http.Request
	}{
		{name: "channels", hand: server.listChannels, req: requestWithServerSession(http.MethodGet, "/notification-channels", nil)},
		{name: "create", hand: server.createChannel, req: requestWithKeywordParams(http.MethodPost, "/notification-channels", `{"name":"webhook","type":"webhook"}`, nil)},
		{name: "update", hand: server.updateChannel, req: requestWithKeywordParams(http.MethodPut, "/notification-channels/2", `{"name":"webhook"}`, channelParams)},
		{name: "delete", hand: server.deleteChannel, req: requestWithKeywordParams(http.MethodDelete, "/notification-channels/2", "", channelParams)},
		{name: "test", hand: server.testChannel, req: requestWithKeywordParams(http.MethodPost, "/notification-channels/2/test", "", channelParams)},
		{name: "bindings", hand: server.listMessageNotifications, req: requestWithServerSession(http.MethodGet, "/message-notifications", nil)},
		{name: "account bindings", hand: server.getAccountBindings, req: requestWithKeywordParams(http.MethodGet, "/message-notifications/cid", "", channelParams)},
		{name: "single binding", hand: server.setAccountBindings, req: requestWithKeywordParams(http.MethodPost, "/message-notifications/cid", `{"channel_id":2,"enabled":true}`, channelParams)},
		{name: "batch binding", hand: server.setAccountBindings, req: requestWithKeywordParams(http.MethodPost, "/message-notifications/cid", `{"channel_ids":[2]}`, channelParams)},
		{name: "delete binding", hand: server.deleteMessageNotification, req: requestWithKeywordParams(http.MethodDelete, "/message-notifications/3", "", channelParams)},
		{name: "delete account bindings", hand: server.deleteAccountNotifications, req: requestWithKeywordParams(http.MethodDelete, "/message-notifications/account/cid", "", channelParams)},
	}
	// successCase 表示当前通知成功 Handler 场景。
	for _, successCase := range successCases {
		t.Run(successCase.name, func(t *testing.T) {
			// recorder 保存当前通知 Handler 响应。
			recorder := httptest.NewRecorder()
			successCase.hand(recorder, successCase.req)
			if recorder.Code != http.StatusOK {
				t.Fatalf("status=%d body=%s", recorder.Code, recorder.Body.String())
			}
		})
	}
	// uncertainSuccessCases 保存普通用户和管理员不确定通知成功查询。
	uncertainSuccessCases := []http.HandlerFunc{server.listUncertainNotifications, server.listAdminUncertainNotifications}
	// uncertainSuccessCase 表示当前不确定通知成功 Handler。
	for _, uncertainSuccessCase := range uncertainSuccessCases {
		// recorder 保存不确定通知响应。
		recorder := httptest.NewRecorder()
		uncertainSuccessCase(recorder, requestWithServerSession(http.MethodGet, "/notifications/uncertain", nil))
		if recorder.Code != http.StatusOK {
			t.Fatalf("uncertain status=%d", recorder.Code)
		}
	}
	// port.testErr 保存通知器未启用错误。
	port.testErr = notificationsapp.ErrNotifierUnavailable
	// unavailableRecorder 保存通知器不可用响应。
	unavailableRecorder := httptest.NewRecorder()
	server.testChannel(unavailableRecorder, requestWithKeywordParams(http.MethodPost, "/notification-channels/2/test", "", channelParams))
	if unavailableRecorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("unavailable status=%d", unavailableRecorder.Code)
	}
	// testErrors 保存渠道测试发送的错误映射。
	testErrors := []struct {
		name string
		err  error
		code int
	}{
		{name: "forbidden", err: notificationsapp.ErrChannelForbidden, code: http.StatusForbidden},
		{name: "generic", err: errors.New("channel test failed"), code: http.StatusInternalServerError},
	}
	// testError 表示当前渠道测试错误场景。
	for _, testError := range testErrors {
		port.testErr = testError.err
		// recorder 保存渠道测试错误响应。
		recorder := httptest.NewRecorder()
		server.testChannel(recorder, requestWithKeywordParams(http.MethodPost, "/notification-channels/2/test", "", channelParams))
		if recorder.Code != testError.code {
			t.Fatalf("%s status=%d", testError.name, recorder.Code)
		}
	}
	// invalidChannelRecorder 保存渠道 ID 解析错误。
	invalidChannelRecorder := httptest.NewRecorder()
	server.testChannel(invalidChannelRecorder, requestWithKeywordParams(http.MethodPost, "/notification-channels/no/test", "", map[string]string{"channel_id": "no"}))
	if invalidChannelRecorder.Code != http.StatusBadRequest {
		t.Fatalf("invalid channel status=%d", invalidChannelRecorder.Code)
	}
	// port.bindingErr 保存绑定查询错误，覆盖列表和账号绑定读取失败。
	port.bindingErr = errors.New("binding query failed")
	// bindingErrorHandlers 保存两个绑定查询错误 Handler。
	bindingErrorHandlers := []http.HandlerFunc{server.listMessageNotifications, server.getAccountBindings}
	// bindingErrorHandler 表示当前绑定查询错误 Handler。
	for _, bindingErrorHandler := range bindingErrorHandlers {
		// recorder 保存绑定查询错误响应。
		recorder := httptest.NewRecorder()
		bindingErrorHandler(recorder, requestWithKeywordParams(http.MethodGet, "/message-notifications/cid", "", channelParams))
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("binding error status=%d", recorder.Code)
		}
	}
	// port.uncertainErr 保存不确定通知查询错误。
	port.uncertainErr = errors.New("uncertain query failed")
	// uncertainErrorHandlers 保存两个不确定通知查询错误 Handler。
	uncertainErrorHandlers := []http.HandlerFunc{server.listUncertainNotifications, server.listAdminUncertainNotifications}
	// uncertainErrorHandler 表示当前不确定通知查询错误 Handler。
	for _, uncertainErrorHandler := range uncertainErrorHandlers {
		// recorder 保存不确定通知查询错误响应。
		recorder := httptest.NewRecorder()
		uncertainErrorHandler(recorder, requestWithServerSession(http.MethodGet, "/notifications/uncertain", nil))
		if recorder.Code != http.StatusInternalServerError {
			t.Fatalf("uncertain error status=%d", recorder.Code)
		}
	}
}
