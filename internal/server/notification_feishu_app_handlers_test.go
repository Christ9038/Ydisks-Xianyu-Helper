package server

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	notificationsapp "xianyu-go/internal/application/notifications"
)

// feishuAppBindingPortStub 是飞书扫码绑定 Handler 测试使用的可控应用端口。
type feishuAppBindingPortStub struct {
	// NotificationChannelsPort 提供未覆盖方法的默认能力。
	NotificationChannelsPort
	// startResult 是开始绑定的预置结果。
	startResult notificationsapp.FeishuBindingStart
	// startErr 是开始绑定的预置错误。
	startErr error
	// statusResult 是状态查询的预置结果。
	statusResult notificationsapp.FeishuBindingStatus
	// statusErr 是状态查询的预置错误。
	statusErr error
	// gotUserID 保存最近一次调用的用户标识。
	gotUserID int64
	// gotOptions 保存最近一次开始绑定的渠道表单选项（名称、事件订阅、启用状态、账号绑定策略）。
	gotOptions notificationsapp.FeishuBindingOptions
	// gotSessionID 保存最近一次状态查询的会话标识。
	gotSessionID string
}

// StartFeishuAppBinding 记录开始绑定参数并返回预置结果。
func (port *feishuAppBindingPortStub) StartFeishuAppBinding(_ context.Context, userID int64, options notificationsapp.FeishuBindingOptions) (notificationsapp.FeishuBindingStart, error) {
	port.gotUserID = userID
	port.gotOptions = options
	return port.startResult, port.startErr
}

// GetFeishuAppBindingStatus 记录状态查询参数并返回预置结果。
func (port *feishuAppBindingPortStub) GetFeishuAppBindingStatus(_ context.Context, userID int64, sessionID string) (notificationsapp.FeishuBindingStatus, error) {
	port.gotUserID = userID
	port.gotSessionID = sessionID
	return port.statusResult, port.statusErr
}

// TestFeishuAppBindingHandlersContract 验证开始绑定与状态查询的真实 HTTP 响应符合 OpenAPI，并检查二维码图片。
func TestFeishuAppBindingHandlersContract(t *testing.T) {
	// srv、_、cleanup 分别是测试 Server、无需直接访问的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是注入飞书绑定路由的测试端口。
	port := &feishuAppBindingPortStub{
		startResult:  notificationsapp.FeishuBindingStart{SessionID: "feishu-session-1", QRCodeURL: "https://accounts.feishu.cn/qr?code=contract", ExpiresIn: 600},
		statusResult: notificationsapp.FeishuBindingStatus{Status: notificationsapp.FeishuBindingStatusWaiting, Message: "等待飞书扫码确认"},
	}
	srv.applications.notificationChannels = port
	// handler 是包含通知版本化路由的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员登录后的认证会话。
	sessionCookie := loginHelper(t, handler)
	// startRequest 是省略请求体的开始绑定请求，应使用默认值。
	startRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/feishu-app/qr", nil)
	startRequest.AddCookie(sessionCookie)
	// startRecorder 捕获开始绑定响应。
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, startRequest)
	assertOpenAPISuccessResponse(t, startRequest, startRecorder)
	// startResponse 保存开始绑定响应字段。
	var startResponse feishuAppBindingStartResponse
	// decodeErr 是开始绑定响应解析失败原因；nil 表示响应结构符合契约。
	if decodeErr := json.Unmarshal(startRecorder.Body.Bytes(), &startResponse); decodeErr != nil {
		t.Fatalf("解析开始绑定响应失败: %v body=%s", decodeErr, startRecorder.Body.String())
	}
	if startResponse.SessionID != "feishu-session-1" || startResponse.ExpiresIn != 600 {
		t.Fatalf("开始绑定响应异常: %+v", startResponse)
	}
	if !port.gotOptions.Enabled || port.gotOptions.Name != "" || port.gotOptions.EventTypes != "" || port.gotUserID <= 0 {
		t.Fatalf("省略请求体时应默认启用、使用默认名称并订阅全部事件: options=%+v userID=%d", port.gotOptions, port.gotUserID)
	}
	// qrPrefix 是二维码 data URL 的固定前缀。
	const qrPrefix = "data:image/png;base64,"
	if !strings.HasPrefix(startResponse.QRCodeURL, qrPrefix) {
		t.Fatalf("二维码前缀=%q", startResponse.QRCodeURL)
	}
	// pngBytes、base64Err 保存二维码 PNG 字节及 base64 解码错误。
	pngBytes, base64Err := base64.StdEncoding.DecodeString(strings.TrimPrefix(startResponse.QRCodeURL, qrPrefix))
	if base64Err != nil {
		t.Fatalf("二维码 base64 解码失败: %v", base64Err)
	}
	// image、imageErr 保存解码后的二维码图片及错误。
	image, imageErr := png.Decode(bytes.NewReader(pngBytes))
	if imageErr != nil {
		t.Fatalf("二维码 PNG 解码失败: %v", imageErr)
	}
	if image.Bounds().Dx() <= 0 || image.Bounds().Dy() <= 0 {
		t.Fatalf("二维码尺寸异常: %v", image.Bounds())
	}
	// statusRequest 是查询已登记会话的状态请求。
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/feishu-app/qr/feishu-session-1", nil)
	statusRequest.AddCookie(sessionCookie)
	// statusRecorder 捕获状态响应。
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusRequest)
	assertOpenAPISuccessResponse(t, statusRequest, statusRecorder)
	// statusResponse 保存状态响应字段。
	var statusResponse feishuAppBindingStatusResponse
	// decodeErr 是状态响应解析失败原因；nil 表示响应结构符合契约。
	if decodeErr := json.Unmarshal(statusRecorder.Body.Bytes(), &statusResponse); decodeErr != nil {
		t.Fatalf("解析状态响应失败: %v", decodeErr)
	}
	if statusResponse.Status != notificationsapp.FeishuBindingStatusWaiting || port.gotSessionID != "feishu-session-1" {
		t.Fatalf("状态响应异常: %+v session=%q", statusResponse, port.gotSessionID)
	}
	// explicitRequest 是显式提交名称、事件订阅和停用状态的开始请求。
	explicitRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/feishu-app/qr", strings.NewReader(`{"name":"值班飞书","event_types":"[\"chat_message_received\"]","enabled":false}`))
	explicitRequest.AddCookie(sessionCookie)
	// explicitRecorder 捕获显式表单请求响应。
	explicitRecorder := httptest.NewRecorder()
	handler.ServeHTTP(explicitRecorder, explicitRequest)
	assertOpenAPISuccessResponse(t, explicitRequest, explicitRecorder)
	if port.gotOptions.Enabled || port.gotOptions.Name != "值班飞书" || port.gotOptions.EventTypes != `["chat_message_received"]` {
		t.Fatalf("显式表单字段未透传: options=%+v", port.gotOptions)
	}
}

// TestFeishuAppBindingHandlerErrorMappings 验证统一错误 envelope 与状态码映射。
func TestFeishuAppBindingHandlerErrorMappings(t *testing.T) {
	// srv、_、cleanup 分别是测试 Server、无需直接访问的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是注入飞书绑定路由的测试端口。
	port := &feishuAppBindingPortStub{}
	srv.applications.notificationChannels = port
	// handler 是包含通知版本化路由的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员登录后的认证会话。
	sessionCookie := loginHelper(t, handler)
	// startCases 保存开始绑定错误响应样例。
	startCases := []struct {
		// name 是测试样例名称。
		name string
		// body 是请求体。
		body string
		// err 是端口预置错误。
		err error
		// wantStatus 是期望 HTTP 状态码。
		wantStatus int
	}{
		{name: "非法 JSON", body: `not-json`, wantStatus: http.StatusBadRequest},
		{name: "未启用", err: notificationsapp.ErrFeishuUnavailable, wantStatus: http.StatusInternalServerError},
		{name: "输入无效", err: notificationsapp.ErrChannelInvalidInput, wantStatus: http.StatusBadRequest},
		{name: "通用失败", err: errors.New("platform down"), wantStatus: http.StatusInternalServerError},
	}
	// startCase 表示当前开始绑定错误样例。
	for _, startCase := range startCases {
		t.Run(startCase.name, func(t *testing.T) {
			port.startErr = startCase.err
			// request 是当前开始绑定错误请求。
			request := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/feishu-app/qr", strings.NewReader(startCase.body))
			request.AddCookie(sessionCookie)
			// recorder 捕获错误响应。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != startCase.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, startCase.wantStatus, recorder.Body.String())
			}
			assertOpenAPIResponse(t, request, recorder)
			if strings.Contains(recorder.Body.String(), "platform down") || strings.Contains(recorder.Body.String(), "secret") {
				t.Fatalf("错误响应泄漏内部细节: %s", recorder.Body.String())
			}
		})
	}
	// statusCases 保存状态查询错误响应样例。
	statusCases := []struct {
		// name 是测试样例名称。
		name string
		// sessionID 是路径中的会话标识。
		sessionID string
		// err 是端口预置错误。
		err error
		// wantStatus 是期望 HTTP 状态码。
		wantStatus int
	}{
		{name: "会话不存在", sessionID: "missing", err: notificationsapp.ErrFeishuSessionNotFound, wantStatus: http.StatusNotFound},
		{name: "输入无效", sessionID: "invalid", err: notificationsapp.ErrChannelInvalidInput, wantStatus: http.StatusBadRequest},
		{name: "未启用", sessionID: "disabled", err: notificationsapp.ErrFeishuUnavailable, wantStatus: http.StatusInternalServerError},
		{name: "通用失败", sessionID: "failure", err: errors.New("database down"), wantStatus: http.StatusInternalServerError},
	}
	// statusCase 表示当前状态查询错误样例。
	for _, statusCase := range statusCases {
		t.Run(statusCase.name, func(t *testing.T) {
			port.statusErr = statusCase.err
			// request 是当前状态查询错误请求。
			request := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/feishu-app/qr/"+statusCase.sessionID, nil)
			request.AddCookie(sessionCookie)
			// recorder 捕获错误响应。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != statusCase.wantStatus {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, statusCase.wantStatus, recorder.Body.String())
			}
			assertOpenAPIResponse(t, request, recorder)
			if strings.Contains(recorder.Body.String(), "database down") {
				t.Fatalf("错误响应泄漏内部细节: %s", recorder.Body.String())
			}
		})
	}
}

// TestFeishuAppBindingHandlersRequireAuthentication 验证两个新入口都在认证组内。
func TestFeishuAppBindingHandlersRequireAuthentication(t *testing.T) {
	// srv、_、cleanup 分别是测试 Server、无需直接访问的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含通知版本化路由的真实 Router。
	handler := srv.Router()
	// cases 保存未登录请求的方法与路径。
	cases := []struct {
		// method 是 HTTP 方法。
		method string
		// path 是版本化路径。
		path string
	}{
		{method: http.MethodPost, path: "/api/v1/notifications/feishu-app/qr"},
		{method: http.MethodGet, path: "/api/v1/notifications/feishu-app/qr/any"},
	}
	// testCase 表示当前未登录请求。
	for _, testCase := range cases {
		// request 是不携带会话 Cookie 的请求。
		request := httptest.NewRequest(testCase.method, testCase.path, nil)
		// recorder 捕获未认证响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusUnauthorized {
			t.Fatalf("%s %s status=%d want=401", testCase.method, testCase.path, recorder.Code)
		}
		assertOpenAPIResponse(t, request, recorder)
	}
}

// TestOpenAPIFeishuAppBindingResponses 为 OpenAPI 成功契约覆盖记录飞书扫码 operation 的真实成功响应。
func TestOpenAPIFeishuAppBindingResponses(t *testing.T) {
	// srv、_、cleanup 分别是测试 Server、无需直接访问的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是注入飞书绑定路由的测试端口。
	port := &feishuAppBindingPortStub{
		startResult:  notificationsapp.FeishuBindingStart{SessionID: "openapi-session", QRCodeURL: "https://accounts.feishu.cn/qr?code=openapi", ExpiresIn: 300},
		statusResult: notificationsapp.FeishuBindingStatus{Status: notificationsapp.FeishuBindingStatusSuccess, Message: "飞书应用绑定成功", ChannelID: 12},
	}
	srv.applications.notificationChannels = port
	// handler 是包含通知版本化路由的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员登录后的认证会话。
	sessionCookie := loginHelper(t, handler)
	// startRequest 是开始绑定的成功请求。
	startRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/feishu-app/qr", strings.NewReader(`{"name":"契约飞书"}`))
	startRequest.AddCookie(sessionCookie)
	// startRecorder 捕获开始绑定成功响应。
	startRecorder := httptest.NewRecorder()
	handler.ServeHTTP(startRecorder, startRequest)
	assertOpenAPISuccessResponse(t, startRequest, startRecorder)
	// statusRequest 是成功状态查询请求。
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/feishu-app/qr/openapi-session", nil)
	statusRequest.AddCookie(sessionCookie)
	// statusRecorder 捕获成功状态响应。
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusRequest)
	assertOpenAPISuccessResponse(t, statusRequest, statusRecorder)
}
