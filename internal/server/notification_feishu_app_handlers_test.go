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
	// gotOptions 保存最近一次开始绑定的账号绑定与分类开关。
	gotOptions notificationsapp.FeishuBindingOptions
	// gotSessionID 保存最近一次状态查询的会话标识。
	gotSessionID string
	// categoriesResult 是分类开关读取的预置结果。
	categoriesResult notificationsapp.FeishuChannelCategories
	// categoriesErr 是分类开关读取的预置错误。
	categoriesErr error
	// setCategoriesErr 是分类开关写入的预置错误。
	setCategoriesErr error
	// gotChannelID 保存最近一次分类开关调用的渠道标识。
	gotChannelID int64
	// gotCategories 保存最近一次分类开关写入值。
	gotCategories notificationsapp.FeishuChannelCategories
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

// GetFeishuChannelCategories 记录分类读取参数并返回预置结果。
func (port *feishuAppBindingPortStub) GetFeishuChannelCategories(_ context.Context, userID, channelID int64) (notificationsapp.FeishuChannelCategories, error) {
	port.gotUserID = userID
	port.gotChannelID = channelID
	return port.categoriesResult, port.categoriesErr
}

// SetFeishuChannelCategories 记录分类写入参数并返回预置错误。
func (port *feishuAppBindingPortStub) SetFeishuChannelCategories(_ context.Context, userID, channelID int64, categories notificationsapp.FeishuChannelCategories) error {
	port.gotUserID = userID
	port.gotChannelID = channelID
	port.gotCategories = categories
	return port.setCategoriesErr
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
	// startRequest 是省略请求体的开始绑定请求，默认应绑定全部账号。
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
	if !port.gotOptions.BindAllAccounts || !port.gotOptions.NotifyWarning || !port.gotOptions.NotifyInfo || port.gotUserID <= 0 {
		t.Fatalf("默认绑定或分类开关异常: options=%+v userID=%d", port.gotOptions, port.gotUserID)
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
	// explicitFalseRequest 是显式关闭账号绑定和两个分类开关的开始请求。
	explicitFalseRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/feishu-app/qr", strings.NewReader(`{"bind_all_accounts":false,"notify_warning":false,"notify_info":false}`))
	explicitFalseRequest.AddCookie(sessionCookie)
	// explicitFalseRecorder 捕获显式关闭请求响应。
	explicitFalseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(explicitFalseRecorder, explicitFalseRequest)
	assertOpenAPISuccessResponse(t, explicitFalseRequest, explicitFalseRecorder)
	if port.gotOptions.BindAllAccounts || port.gotOptions.NotifyWarning || port.gotOptions.NotifyInfo {
		t.Fatalf("显式关闭开关未生效: options=%+v", port.gotOptions)
	}
	// port.categoriesResult 预置分类开关读取结果。
	port.categoriesResult = notificationsapp.FeishuChannelCategories{Warning: true, Info: false}
	// categoriesRequest 是读取飞书渠道分类开关的成功请求。
	categoriesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/channels/12/feishu-categories", nil)
	categoriesRequest.AddCookie(sessionCookie)
	// categoriesRecorder 捕获分类开关读取响应。
	categoriesRecorder := httptest.NewRecorder()
	handler.ServeHTTP(categoriesRecorder, categoriesRequest)
	assertOpenAPISuccessResponse(t, categoriesRequest, categoriesRecorder)
	// categoriesResponse 保存分类开关读取响应字段。
	var categoriesResponse feishuChannelCategoriesResponse
	// decodeErr 是分类开关响应解析失败原因；nil 表示响应结构符合契约。
	if decodeErr := json.Unmarshal(categoriesRecorder.Body.Bytes(), &categoriesResponse); decodeErr != nil {
		t.Fatalf("解析分类开关响应失败: %v", decodeErr)
	}
	if !categoriesResponse.Warning || categoriesResponse.Info || port.gotChannelID != 12 {
		t.Fatalf("分类开关响应异常: response=%+v channel=%d", categoriesResponse, port.gotChannelID)
	}
	// updateCategoriesRequest 是更新飞书渠道分类开关的成功请求。
	updateCategoriesRequest := httptest.NewRequest(http.MethodPut, "/api/v1/notifications/channels/12/feishu-categories", strings.NewReader(`{"warning":false,"info":true}`))
	updateCategoriesRequest.AddCookie(sessionCookie)
	// updateCategoriesRecorder 捕获分类开关更新响应。
	updateCategoriesRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateCategoriesRecorder, updateCategoriesRequest)
	assertOpenAPISuccessResponse(t, updateCategoriesRequest, updateCategoriesRecorder)
	if port.gotCategories.Warning || !port.gotCategories.Info || port.gotChannelID != 12 {
		t.Fatalf("分类开关写入参数异常: categories=%+v channel=%d", port.gotCategories, port.gotChannelID)
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

// TestFeishuChannelCategoriesHandlerErrorMappings 验证分类开关 GET/PUT 的参数校验、归属与统一错误映射。
func TestFeishuChannelCategoriesHandlerErrorMappings(t *testing.T) {
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
	// errorCases 保存分类开关应用错误与期望 HTTP 状态。
	errorCases := []struct {
		// name 是测试样例名称。
		name string
		// err 是端口预置错误。
		err error
		// wantStatus 是期望 HTTP 状态码。
		wantStatus int
	}{
		{name: "不支持渠道类型", err: notificationsapp.ErrChannelInvalidInput, wantStatus: http.StatusBadRequest},
		{name: "渠道不存在", err: notificationsapp.ErrChannelNotFound, wantStatus: http.StatusNotFound},
		{name: "无权操作", err: notificationsapp.ErrChannelForbidden, wantStatus: http.StatusForbidden},
		{name: "通用失败", err: errors.New("config secret leaked?"), wantStatus: http.StatusInternalServerError},
	}
	// errorCase 表示当前分类错误样例。
	for _, errorCase := range errorCases {
		t.Run(errorCase.name, func(t *testing.T) {
			port.categoriesErr = errorCase.err
			port.setCategoriesErr = errorCase.err
			// readRequest 和 readRecorder 保存分类读取错误响应。
			readRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/channels/12/feishu-categories", nil)
			readRequest.AddCookie(sessionCookie)
			// readRecorder 捕获分类读取错误响应。
			readRecorder := httptest.NewRecorder()
			handler.ServeHTTP(readRecorder, readRequest)
			if readRecorder.Code != errorCase.wantStatus {
				t.Fatalf("读取 status=%d want=%d body=%s", readRecorder.Code, errorCase.wantStatus, readRecorder.Body.String())
			}
			assertOpenAPIResponse(t, readRequest, readRecorder)
			// writeRequest 和 writeRecorder 保存分类写入错误响应。
			writeRequest := httptest.NewRequest(http.MethodPut, "/api/v1/notifications/channels/12/feishu-categories", strings.NewReader(`{"warning":true,"info":true}`))
			writeRequest.AddCookie(sessionCookie)
			// writeRecorder 捕获分类写入错误响应。
			writeRecorder := httptest.NewRecorder()
			handler.ServeHTTP(writeRecorder, writeRequest)
			if writeRecorder.Code != errorCase.wantStatus {
				t.Fatalf("写入 status=%d want=%d body=%s", writeRecorder.Code, errorCase.wantStatus, writeRecorder.Body.String())
			}
			assertOpenAPIResponse(t, writeRequest, writeRecorder)
			if strings.Contains(readRecorder.Body.String(), "config secret leaked") || strings.Contains(writeRecorder.Body.String(), "config secret leaked") {
				t.Fatalf("错误响应泄漏内部细节: read=%s write=%s", readRecorder.Body.String(), writeRecorder.Body.String())
			}
		})
	}
	// invalidRequests 保存分类路径或请求体校验样例。
	invalidRequests := []struct {
		// name 是测试样例名称。
		name string
		// method 是 HTTP 方法。
		method string
		// path 是版本化路径。
		path string
		// body 是请求体。
		body string
	}{
		{name: "非法渠道 ID", method: http.MethodGet, path: "/api/v1/notifications/channels/invalid/feishu-categories"},
		{name: "缺少开关", method: http.MethodPut, path: "/api/v1/notifications/channels/12/feishu-categories", body: `{}`},
		{name: "只提供一个开关", method: http.MethodPut, path: "/api/v1/notifications/channels/12/feishu-categories", body: `{"warning":true}`},
		{name: "非法 JSON", method: http.MethodPut, path: "/api/v1/notifications/channels/12/feishu-categories", body: `not-json`},
	}
	// invalidRequest 表示当前参数校验样例。
	for _, invalidRequest := range invalidRequests {
		t.Run(invalidRequest.name, func(t *testing.T) {
			// request 是当前非法参数请求。
			request := httptest.NewRequest(invalidRequest.method, invalidRequest.path, strings.NewReader(invalidRequest.body))
			request.AddCookie(sessionCookie)
			// recorder 捕获参数校验错误响应。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != http.StatusBadRequest {
				t.Fatalf("status=%d want=400 body=%s", recorder.Code, recorder.Body.String())
			}
			assertOpenAPIResponse(t, request, recorder)
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
		{method: http.MethodGet, path: "/api/v1/notifications/channels/12/feishu-categories"},
		{method: http.MethodPut, path: "/api/v1/notifications/channels/12/feishu-categories"},
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

// TestOpenAPIFeishuAppBindingResponses 为 OpenAPI 成功契约覆盖记录飞书扫码与分类开关 operation 的真实成功响应。
func TestOpenAPIFeishuAppBindingResponses(t *testing.T) {
	// srv、_、cleanup 分别是测试 Server、无需直接访问的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是注入飞书绑定路由的测试端口。
	port := &feishuAppBindingPortStub{
		startResult:      notificationsapp.FeishuBindingStart{SessionID: "openapi-session", QRCodeURL: "https://accounts.feishu.cn/qr?code=openapi", ExpiresIn: 300},
		statusResult:     notificationsapp.FeishuBindingStatus{Status: notificationsapp.FeishuBindingStatusSuccess, Message: "飞书应用绑定成功", ChannelID: 12},
		categoriesResult: notificationsapp.FeishuChannelCategories{Warning: true, Info: true},
	}
	srv.applications.notificationChannels = port
	// handler 是包含通知版本化路由的真实 Router。
	handler := srv.Router()
	// sessionCookie 是管理员登录后的认证会话。
	sessionCookie := loginHelper(t, handler)
	// startRequest 是开始绑定的成功请求。
	startRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/feishu-app/qr", strings.NewReader(`{"bind_all_accounts":true}`))
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
	// categoriesRequest 是成功读取分类开关的请求。
	categoriesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/channels/12/feishu-categories", nil)
	categoriesRequest.AddCookie(sessionCookie)
	// categoriesRecorder 捕获分类开关读取成功响应。
	categoriesRecorder := httptest.NewRecorder()
	handler.ServeHTTP(categoriesRecorder, categoriesRequest)
	assertOpenAPISuccessResponse(t, categoriesRequest, categoriesRecorder)
	// updateCategoriesRequest 是成功更新分类开关的请求。
	updateCategoriesRequest := httptest.NewRequest(http.MethodPut, "/api/v1/notifications/channels/12/feishu-categories", strings.NewReader(`{"warning":true,"info":false}`))
	updateCategoriesRequest.AddCookie(sessionCookie)
	// updateCategoriesRecorder 捕获分类开关更新成功响应。
	updateCategoriesRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateCategoriesRecorder, updateCategoriesRequest)
	assertOpenAPISuccessResponse(t, updateCategoriesRequest, updateCategoriesRecorder)
}
