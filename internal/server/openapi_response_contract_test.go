package server

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
)

// TestOpenAPISuccessContractCoverage 从唯一 OpenAPI 文档自动枚举普通 operation，确认每个 operation 都有真实成功响应断言记录。
func TestOpenAPISuccessContractCoverage(t *testing.T) {
	// scenarios 是按领域组织的真实 Router 成功场景；覆盖集合由每次成功断言的 operationId 自动产生。
	scenarios := []struct {
		// name 是失败输出使用的稳定领域名称。
		name string
		// run 会执行该领域完整的真实 HTTP 成功场景。
		run func(*testing.T)
	}{
		{name: "session-and-qr", run: TestOpenAPISessionAndQRResponses},
		{name: "accounts-and-system", run: TestOpenAPIAccountAndSystemResponses},
		{name: "query-chat-orders", run: TestOpenAPIQueryChatAndOrderResponses},
		{name: "items-and-cards", run: TestOpenAPIItemAndCardResponses},
		{name: "notifications", run: TestOpenAPINotificationResponses},
		{name: "versioned-session", run: TestVersionedSessionRoutesPreserveLegacyContracts},
		{name: "versioned-account", run: TestVersionedAccountRoutesPreserveLegacyContracts},
		{name: "versioned-account-credentials", run: TestVersionedAccountCredentialRoutesPreserveLegacyContracts},
		{name: "versioned-account-settings", run: TestVersionedAccountSettingsRoutesPreserveLegacyContracts},
		{name: "versioned-items", run: TestVersionedItemRoutesPreserveLegacyContracts},
		{name: "versioned-orders", run: TestVersionedOrderRoutesPreserveLegacyContracts},
		{name: "versioned-order-refresh", run: TestVersionedOrderRefreshAndBatchRoutesPreserveLegacyContracts},
		{name: "versioned-qr", run: TestVersionedQRLoginRoutesPreserveLegacyContracts},
		{name: "versioned-admin-analytics", run: TestVersionedAdminAnalyticsRoutesPreserveLegacyContracts},
		{name: "versioned-settings-cards-notifications", run: TestVersionedSettingsCardNotificationRoutesPreserveLegacyContracts},
		{name: "versioned-item-batches", run: TestVersionedItemBatchRoutesPreserveLegacyContracts},
		{name: "versioned-chat-tasks", run: TestVersionedChatTaskRoutesPreserveLegacyContracts},
		{name: "versioned-replies", run: TestVersionedReplyRoutesPreserveLegacyContracts},
		{name: "versioned-remaining", run: TestVersionedRemainingRoutesPreserveLegacyContracts},
		{name: "named-success", run: TestNamedSuccessResponseContracts},
		{name: "remaining-success", run: TestRemainingSuccessResponseContracts},
		{name: "settings-cards-notifications-batch", run: TestSettingsCardsNotificationsBatchContracts},
		{name: "dynamic-success", run: TestDynamicCompatibilitySuccessContracts},
		{name: "reply-and-account-task-success", run: TestReplyAndAccountTaskSuccessResponseContracts},
		{name: "analytics-admin-public-success", run: TestAnalyticsAdminAndPublicSuccessResponseContracts},
		{name: "local-resource-mutations", run: TestOpenAPILocalResourceMutationResponses},
		{name: "remaining-versioned-success", run: TestOpenAPIRemainingVersionedSuccessResponses},
	}
	// scenario 是当前执行的领域真实响应场景。
	for _, scenario := range scenarios {
		// scenario 保存当前领域成功场景，子测试名称和执行函数必须保持同一实例。
		scenario := scenario
		t.Run(scenario.name, func(t *testing.T) {
			scenario.run(t)
		})
	}
	// document 是包含全部 operation 的唯一 OpenAPI 契约。
	document := loadOpenAPIContractForCoverage(t)
	// missing 保存未被成功断言记录、且没有显式特殊校验的 operationId。
	missing := make([]string, 0)
	for _, path := range document.Paths.Keys() { // path 是当前 OpenAPI 路径模板。
		// pathItem 保存该模板下声明的全部 HTTP operation。
		pathItem := document.Paths.Find(path)
		for _, operation := range pathItem.Operations() { // operation 是当前待检查的 OpenAPI operation。
			if openAPIContractSuccessExceptionKind(operation) != "" {
				continue
			}
			// exists 表示该 operation 是否已由真实成功响应记录。
			if _, exists := openAPISuccessOperations.Load(operation.OperationID); !exists {
				missing = append(missing, operation.OperationID)
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Fatalf("缺少真实成功响应契约场景: %s", strings.Join(missing, ", "))
	}
}

// assertOpenAPIResponse 验证真实 HTTP 响应的状态码、Content-Type 和 JSON 形状符合对应 OpenAPI operation。
func assertOpenAPIResponse(t *testing.T, request *http.Request, recorder *httptest.ResponseRecorder) {
	t.Helper()
	// specPath 是从 Server 包测试目录定位的唯一 OpenAPI 契约文件。
	specPath := filepath.Join("..", "..", "api", "openapi.yaml")
	// document、loadErr 分别是解析后的契约文档和加载失败原因。
	document, loadErr := openapi3.NewLoader().LoadFromFile(specPath)
	if loadErr != nil {
		t.Fatalf("加载 OpenAPI 契约失败: %v", loadErr)
	}
	// router、routerErr 分别是将 OpenAPI operation 映射到 HTTP 请求的路由器和构建失败原因。
	router, routerErr := legacy.NewRouter(document)
	if routerErr != nil {
		t.Fatalf("构建 OpenAPI 路由器失败: %v", routerErr)
	}
	// route、pathParams、findErr 分别是匹配到的 operation、解析出的路径参数和匹配失败原因。
	route, pathParams, findErr := router.FindRoute(request)
	if findErr != nil {
		t.Fatalf("OpenAPI 未匹配请求 %s %s: %v", request.Method, request.URL.Path, findErr)
	}
	// requestInput 是响应校验需要的 operation 与路径参数上下文。
	requestInput := &openapi3filter.RequestValidationInput{Request: request, PathParams: pathParams, Route: route}
	// responseInput 是实际响应的状态、头和可重复读取的 JSON 内容。
	responseInput := &openapi3filter.ResponseValidationInput{
		RequestValidationInput: requestInput,
		Status:                 recorder.Code,
		Header:                 recorder.Result().Header,
		Body:                   io.NopCloser(bytes.NewReader(recorder.Body.Bytes())),
	}
	// validationErr 表示真实 handler 输出违反 OpenAPI 响应契约的具体原因。
	if validationErr := openapi3filter.ValidateResponse(context.Background(), responseInput); validationErr != nil {
		t.Fatalf("响应不符合 OpenAPI: %s %s status=%d body=%s err=%v", request.Method, request.URL.Path, recorder.Code, recorder.Body.String(), validationErr)
	}
}

// assertOpenAPISuccessResponse 验证预期成功的真实路由既返回 200，又满足对应 operation 的响应契约。
func assertOpenAPISuccessResponse(t *testing.T, request *http.Request, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code != http.StatusOK {
		t.Fatalf("成功响应状态错误: %s %s status=%d body=%s", request.Method, request.URL.Path, recorder.Code, recorder.Body.String())
	}
	assertOpenAPIResponse(t, request, recorder)
	// operationID 是从请求匹配的 OpenAPI operation 读取的稳定成功覆盖键。
	operationID := openAPIOperationIDForRequest(t, request)
	openAPISuccessOperations.Store(operationID, struct{}{})
}

// assertOpenAPIExpectedStatusResponse 验证非 200 成功状态的真实响应仍符合 operation 契约。
func assertOpenAPIExpectedStatusResponse(t *testing.T, request *http.Request, recorder *httptest.ResponseRecorder, wantStatus int) {
	t.Helper()
	if recorder.Code != wantStatus {
		t.Fatalf("成功响应状态错误: %s %s status=%d want=%d body=%s", request.Method, request.URL.Path, recorder.Code, wantStatus, recorder.Body.String())
	}
	assertOpenAPIResponse(t, request, recorder)
	// operationID 是从请求匹配的 OpenAPI operation 读取的稳定成功覆盖键。
	operationID := openAPIOperationIDForRequest(t, request)
	openAPISuccessOperations.Store(operationID, struct{}{})
}

// assertOpenAPIRecordedSuccessResponse 验证任意 2xx 的真实响应并记录其 operationId，供已存在的兼容路由成功场景复用。
func assertOpenAPIRecordedSuccessResponse(t *testing.T, request *http.Request, recorder *httptest.ResponseRecorder) {
	t.Helper()
	if recorder.Code < http.StatusOK || recorder.Code >= http.StatusMultipleChoices {
		t.Fatalf("成功响应状态错误: %s %s status=%d body=%s", request.Method, request.URL.Path, recorder.Code, recorder.Body.String())
	}
	assertOpenAPIResponse(t, request, recorder)
	// operationID 是从成功请求匹配出的稳定 OpenAPI operation 标识。
	operationID := openAPIOperationIDForRequest(t, request)
	openAPISuccessOperations.Store(operationID, struct{}{})
}

// contractRecordingHandler 在契约覆盖场景中捕获版本化成功响应，避免手写 operation 名单或遗漏断言点。
func contractRecordingHandler(t *testing.T, next http.Handler) http.Handler {
	t.Helper()
	return http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		// recorder 暂存真实 handler 的完整响应，便于校验后再复制到调用方记录器。
		recorder := httptest.NewRecorder()
		next.ServeHTTP(recorder, request)
		// headerValue 表示真实 handler 写出的响应头值集合。
		for headerName, headerValues := range recorder.Header() {
			responseWriter.Header()[headerName] = append([]string(nil), headerValues...)
		}
		responseWriter.WriteHeader(recorder.Code)
		_, _ = responseWriter.Write(recorder.Body.Bytes())
		if (strings.HasPrefix(request.URL.Path, "/api/v1/") || request.URL.Path == "/health") && recorder.Code >= http.StatusOK && recorder.Code < http.StatusMultipleChoices {
			assertOpenAPIRecordedSuccessResponse(t, request, recorder)
		}
	})
}

// serveOpenAPISuccess 以真实 Router 执行一个本地可确定的版本化成功请求，并记录 operationId 作为覆盖证据。
func serveOpenAPISuccess(t *testing.T, handler http.Handler, sessionCookie *http.Cookie, method string, path string, body string) *httptest.ResponseRecorder {
	t.Helper()
	// request 是本次本地资源生命周期操作使用的版本化 HTTP 请求。
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	if sessionCookie != nil {
		request.AddCookie(sessionCookie)
	}
	// recorder 保存真实 handler 的完整成功响应，供 OpenAPI 校验和调用方业务断言复用。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	return recorder
}

// openAPIOperationIDForRequest 使用唯一 OpenAPI 文档匹配真实请求并返回稳定 operationId。
func openAPIOperationIDForRequest(t *testing.T, request *http.Request) string {
	t.Helper()
	// specPath 是从 Server 测试目录定位到唯一 OpenAPI 契约文件的路径。
	specPath := filepath.Join("..", "..", "api", "openapi.yaml")
	// document、loadErr 分别是解析后的 OpenAPI 文档及其加载失败原因。
	document, loadErr := openapi3.NewLoader().LoadFromFile(specPath)
	if loadErr != nil {
		t.Fatalf("加载 OpenAPI 契约失败: %v", loadErr)
	}
	// router、routerErr 分别是请求与 operation 匹配使用的 OpenAPI 路由器及其构建错误。
	router, routerErr := legacy.NewRouter(document)
	if routerErr != nil {
		t.Fatalf("构建 OpenAPI 路由器失败: %v", routerErr)
	}
	// route、_、findErr 分别是匹配结果、无需读取的路径参数和路由匹配失败原因。
	route, _, findErr := router.FindRoute(request)
	if findErr != nil || route.Operation == nil || route.Operation.OperationID == "" {
		t.Fatalf("读取成功响应 operationId 失败: %s %s err=%v", request.Method, request.URL.Path, findErr)
	}
	return route.Operation.OperationID
}

// openAPIContractSuccessExceptionKind 返回 operation 显式登记的特殊成功校验类别；普通 HTTP operation 返回空字符串。
func openAPIContractSuccessExceptionKind(operation *openapi3.Operation) string {
	if operation == nil || operation.Extensions == nil {
		return ""
	}
	// exception、valid 分别是 YAML 扩展对象及其是否符合受限结构。
	exception, valid := operation.Extensions["x-contract-success-exception"].(map[string]any)
	if !valid {
		return ""
	}
	// kind、valid 分别是特殊校验类别及其是否为稳定字符串。
	kind, valid := exception["kind"].(string)
	if !valid {
		return ""
	}
	return kind
}

// TestOpenAPIPasswordLoginDisabledOperations 验证永久关闭的密码登录 operation 真实返回 501 统一错误，而非伪造 2xx 成功响应。
func TestOpenAPIPasswordLoginDisabledOperations(t *testing.T) {
	// srv、_、cleanup 分别是独立测试 Server、无需直接读取的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是承载版本化密码登录关闭策略的真实 chi Router。
	handler := srv.Router()
	// sessionCookie 是访问受保护密码登录 operation 所需的管理员认证会话。
	sessionCookie := loginHelper(t, handler)
	// requests 保存三个永久关闭 operation 的最小版本化请求。
	requests := []*http.Request{
		httptest.NewRequest(http.MethodPost, "/api/v1/password-login", strings.NewReader(`{"account_id":"acc1","account":"contract-user","password":"not-a-real-secret"}`)),
		httptest.NewRequest(http.MethodGet, "/api/v1/password-login/check/contract-session", nil),
		httptest.NewRequest(http.MethodDelete, "/api/v1/password-login/cancel/contract-session", nil),
	}
	// request 是当前待验证的永久关闭版本化 operation 请求。
	for _, request := range requests {
		request.AddCookie(sessionCookie)
		// recorder 捕获关闭策略的实际 HTTP 状态、Content-Type 和统一错误响应体。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusNotImplemented {
			t.Fatalf("永久关闭 operation 状态错误: %s %s status=%d body=%s", request.Method, request.URL.Path, recorder.Code, recorder.Body.String())
		}
		assertOpenAPIResponse(t, request, recorder)
	}
}

// TestOpenAPISessionAndQRResponses 验证阶段二会话与二维码主链路的成功、未认证和风控响应均满足真实契约。
func TestOpenAPISessionAndQRResponses(t *testing.T) {
	// srv、store、cleanup 分别是测试 Server、持久化夹具和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	setTestQRLogin(srv, &fakeQRLoginService{status: map[string]any{"status": "verification_required", "face_qr_url": "https://example.invalid/face.png", "verification_screenshot": "https://example.invalid/screenshot.png"}})
	// handler 是包含认证和版本化路由的真实 chi Router。
	handler := srv.Router()
	// loginRequest 是登录成功的版本化请求。
	loginRequest := httptest.NewRequest(http.MethodPost, "/api/v1/session/login", bytes.NewBufferString(`{"username":"admin","password":"pw"}`))
	// loginRecorder 是捕获版本化登录响应的测试记录器。
	loginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginRecorder, loginRequest)
	assertOpenAPISuccessResponse(t, loginRequest, loginRecorder)
	// sessionCookie 是登录响应建立的认证 Cookie，用于后续 QR operation。
	sessionCookie := loginHelper(t, handler)
	// verifyRequest 是携带认证 Cookie 的会话状态读取请求。
	verifyRequest := httptest.NewRequest(http.MethodGet, "/api/v1/session", nil)
	verifyRequest.AddCookie(sessionCookie)
	// verifyRecorder 是捕获版本化会话校验响应的测试记录器。
	verifyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(verifyRecorder, verifyRequest)
	assertOpenAPISuccessResponse(t, verifyRequest, verifyRecorder)
	// unauthenticatedQRRequest 是缺少认证 Cookie 的二维码请求，必须返回统一 401 错误 envelope。
	unauthenticatedQRRequest := httptest.NewRequest(http.MethodPost, "/api/v1/qr-login/generate", nil)
	// unauthenticatedQRRecorder 是捕获未认证二维码请求错误响应的测试记录器。
	unauthenticatedQRRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedQRRecorder, unauthenticatedQRRequest)
	assertOpenAPIResponse(t, unauthenticatedQRRequest, unauthenticatedQRRecorder)
	// qrSessionID 是为状态查询建立的归属会话标识。
	qrSessionID := "openapi-qr-session"
	// ownQRSession 建立当前管理员对二维码会话的归属记录。
	ownQRSession(t, srv, store, qrSessionID)
	// qrStatusRequest 是包含风控展示字段的二维码状态请求。
	qrStatusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/qr-login/check/"+qrSessionID, nil)
	qrStatusRequest.AddCookie(sessionCookie)
	// qrStatusRecorder 是捕获二维码风控状态响应的测试记录器。
	qrStatusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(qrStatusRecorder, qrStatusRequest)
	assertOpenAPIResponse(t, qrStatusRequest, qrStatusRecorder)
}

// TestOpenAPIAccountAndSystemResponses 验证阶段二账户与系统设置主链路的真实成功响应和未认证错误都符合 OpenAPI。
func TestOpenAPIAccountAndSystemResponses(t *testing.T) {
	// srv、_、cleanup 分别是测试 Server、无需直接访问的存储和测试资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含账户和系统设置版本化路由的真实 chi Router。
	handler := srv.Router()
	// sessionCookie 是管理员会话 Cookie，用于构造受保护 operation 的成功场景。
	sessionCookie := loginHelper(t, handler)
	// unauthenticatedRequest 是未携带会话的账户运行状态请求，必须使用统一错误响应。
	unauthenticatedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/runtime-status", nil)
	// unauthenticatedRecorder 是捕获未认证账户状态响应的记录器。
	unauthenticatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRecorder, unauthenticatedRequest)
	assertOpenAPIResponse(t, unauthenticatedRequest, unauthenticatedRecorder)

	// runtimeRequest 是读取当前用户账号运行状态的成功请求。
	runtimeRequest := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/runtime-status", nil)
	runtimeRequest.AddCookie(sessionCookie)
	// runtimeRecorder 是捕获运行状态映射的记录器。
	runtimeRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runtimeRecorder, runtimeRequest)
	assertOpenAPISuccessResponse(t, runtimeRequest, runtimeRecorder)
	if strings.Contains(runtimeRecorder.Body.String(), "cookie") || strings.Contains(runtimeRecorder.Body.String(), "password") {
		t.Fatalf("账号运行状态泄漏敏感字段: %s", runtimeRecorder.Body.String())
	}

	// longLoginRequest 是读取账号长期登录状态的请求；测试平台未提供长登录 returnValue，因此验证统一错误响应。
	longLoginRequest := httptest.NewRequest(http.MethodGet, "/api/v1/accounts/acc1/long-login", nil)
	longLoginRequest.AddCookie(sessionCookie)
	// longLoginRecorder 是捕获长期登录状态响应的记录器。
	longLoginRecorder := httptest.NewRecorder()
	handler.ServeHTTP(longLoginRecorder, longLoginRequest)
	assertOpenAPIResponse(t, longLoginRequest, longLoginRecorder)

	// settingsRequest 是保存账号备注、暂停时长和自动确认开关的聚合成功请求。
	settingsRequest := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/settings", strings.NewReader(`{"remark":"OpenAPI 账号","pause_duration":10,"auto_confirm":true}`))
	settingsRequest.AddCookie(sessionCookie)
	// settingsRecorder 是捕获账号聚合设置响应的记录器。
	settingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(settingsRecorder, settingsRequest)
	assertOpenAPISuccessResponse(t, settingsRequest, settingsRecorder)

	// autoConfirmRequest 是单独保存自动确认开关的成功请求。
	autoConfirmRequest := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/auto-confirm", strings.NewReader(`{"auto_confirm":false}`))
	autoConfirmRequest.AddCookie(sessionCookie)
	// autoConfirmRecorder 是捕获自动确认操作响应的记录器。
	autoConfirmRecorder := httptest.NewRecorder()
	handler.ServeHTTP(autoConfirmRecorder, autoConfirmRequest)
	assertOpenAPISuccessResponse(t, autoConfirmRequest, autoConfirmRecorder)

	// pauseRequest 是更新账号自动化暂停时长的成功请求。
	pauseRequest := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/pause-duration", strings.NewReader(`{"pause_duration":15}`))
	pauseRequest.AddCookie(sessionCookie)
	// pauseRecorder 是捕获暂停设置响应的记录器。
	pauseRecorder := httptest.NewRecorder()
	handler.ServeHTTP(pauseRecorder, pauseRequest)
	assertOpenAPISuccessResponse(t, pauseRequest, pauseRecorder)

	// remarkRequest 是更新非敏感账号备注的成功请求。
	remarkRequest := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/remark", strings.NewReader(`{"remark":"OpenAPI 新备注"}`))
	remarkRequest.AddCookie(sessionCookie)
	// remarkRecorder 是捕获账号备注操作响应的记录器。
	remarkRecorder := httptest.NewRecorder()
	handler.ServeHTTP(remarkRecorder, remarkRequest)
	assertOpenAPISuccessResponse(t, remarkRequest, remarkRecorder)

	// profileRequest 是刷新账号公开资料的成功请求。
	profileRequest := httptest.NewRequest(http.MethodPost, "/api/v1/accounts/acc1/refresh-profile", nil)
	profileRequest.AddCookie(sessionCookie)
	// profileRecorder 是捕获账号资料刷新响应的记录器。
	profileRecorder := httptest.NewRecorder()
	handler.ServeHTTP(profileRecorder, profileRequest)
	assertOpenAPISuccessResponse(t, profileRequest, profileRecorder)

	// loginInfoRequest 是只更新展示浏览器选项且不携带登录秘密的成功请求。
	loginInfoRequest := httptest.NewRequest(http.MethodPut, "/api/v1/accounts/acc1/login-info", strings.NewReader(`{"username":"openapi-user","show_browser":false}`))
	loginInfoRequest.AddCookie(sessionCookie)
	// loginInfoRecorder 是捕获登录资料操作响应的记录器。
	loginInfoRecorder := httptest.NewRecorder()
	handler.ServeHTTP(loginInfoRecorder, loginInfoRequest)
	assertOpenAPISuccessResponse(t, loginInfoRequest, loginInfoRecorder)
	if strings.Contains(loginInfoRecorder.Body.String(), "password") {
		t.Fatalf("账号登录资料响应泄漏密码字段: %s", loginInfoRecorder.Body.String())
	}

	// systemRequest 是读取脱敏系统设置的管理员成功请求。
	systemRequest := httptest.NewRequest(http.MethodGet, "/api/v1/settings/system", nil)
	systemRequest.AddCookie(sessionCookie)
	// systemRecorder 是捕获系统设置键值对象的记录器。
	systemRecorder := httptest.NewRecorder()
	handler.ServeHTTP(systemRecorder, systemRequest)
	assertOpenAPISuccessResponse(t, systemRequest, systemRecorder)
	if strings.Contains(systemRecorder.Body.String(), "smtp_password") || strings.Contains(systemRecorder.Body.String(), "ai_api_key") {
		t.Fatalf("系统设置响应泄漏敏感字段: %s", systemRecorder.Body.String())
	}

	// updateSystemRequest 是修改普通日志级别设置的成功请求。
	updateSystemRequest := httptest.NewRequest(http.MethodPut, "/api/v1/settings/system", strings.NewReader(`{"values":{"log_level":"info"}}`))
	updateSystemRequest.AddCookie(sessionCookie)
	// updateSystemRecorder 是捕获系统设置保存响应的记录器。
	updateSystemRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateSystemRecorder, updateSystemRequest)
	assertOpenAPISuccessResponse(t, updateSystemRequest, updateSystemRecorder)
}

// TestOpenAPIQueryChatAndOrderResponses 验证阶段三仪表盘、聊天、订单查询和异步刷新任务符合真实 OpenAPI 响应契约。
func TestOpenAPIQueryChatAndOrderResponses(t *testing.T) {
	// srv、_、cleanup 分别是测试 Server、无需直接访问的存储和测试资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含阶段三版本化路由的真实 chi Router。
	handler := srv.Router()
	// sessionCookie 是管理员会话 Cookie，用于受保护查询成功场景。
	sessionCookie := loginHelper(t, handler)
	// unauthenticatedRequest 是缺少会话的订单查询，必须保持统一错误 envelope。
	unauthenticatedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/orders", nil)
	// unauthenticatedRecorder 是捕获未认证订单查询响应的记录器。
	unauthenticatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRecorder, unauthenticatedRequest)
	assertOpenAPIResponse(t, unauthenticatedRequest, unauthenticatedRecorder)

	// dashboardRequest 是读取当前用户仪表盘统计的成功请求。
	dashboardRequest := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/dashboard", nil)
	dashboardRequest.AddCookie(sessionCookie)
	// dashboardRecorder 是捕获仪表盘统计响应的记录器。
	dashboardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(dashboardRecorder, dashboardRequest)
	assertOpenAPISuccessResponse(t, dashboardRequest, dashboardRecorder)

	// analyticsRequest 是带日期和时区参数的订单分析成功请求。
	analyticsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/analytics/orders?start_date=2026-01-01&end_date=2026-01-31&timezone_offset_minutes=480", nil)
	analyticsRequest.AddCookie(sessionCookie)
	// analyticsRecorder 是捕获订单分析响应的记录器。
	analyticsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(analyticsRecorder, analyticsRequest)
	assertOpenAPISuccessResponse(t, analyticsRequest, analyticsRecorder)

	// orderListRequest 是带稳定分页参数的订单查询成功请求。
	orderListRequest := httptest.NewRequest(http.MethodGet, "/api/v1/orders?page=1&page_size=20", nil)
	orderListRequest.AddCookie(sessionCookie)
	// orderListRecorder 是捕获订单分页响应的记录器。
	orderListRecorder := httptest.NewRecorder()
	handler.ServeHTTP(orderListRecorder, orderListRequest)
	assertOpenAPISuccessResponse(t, orderListRequest, orderListRecorder)

	// chatSessionsRequest 是带账号和游标参数的聊天会话分页成功请求。
	chatSessionsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/chat/sessions?account_id=acc1&cursor=0", nil)
	chatSessionsRequest.AddCookie(sessionCookie)
	// chatSessionsRecorder 是捕获聊天会话分页响应的记录器。
	chatSessionsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(chatSessionsRecorder, chatSessionsRequest)
	assertOpenAPISuccessResponse(t, chatSessionsRequest, chatSessionsRecorder)

	// refreshRequest 是创建订单刷新后台任务的 multipart 成功请求。
	refreshRequest := httptest.NewRequest(http.MethodPost, "/api/v1/orders/refresh", strings.NewReader("--openapi\r\nContent-Disposition: form-data; name=\"cookie_id\"\r\n\r\nacc1\r\n--openapi--\r\n"))
	refreshRequest.Header.Set("Content-Type", "multipart/form-data; boundary=openapi")
	refreshRequest.AddCookie(sessionCookie)
	// refreshRecorder 是捕获异步刷新任务创建响应的记录器。
	refreshRecorder := httptest.NewRecorder()
	handler.ServeHTTP(refreshRecorder, refreshRequest)
	assertOpenAPIExpectedStatusResponse(t, refreshRequest, refreshRecorder, http.StatusAccepted)

	// adminUsersRequest 是管理员读取用户摘要的成功请求。
	adminUsersRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/users", nil)
	adminUsersRequest.AddCookie(sessionCookie)
	// adminUsersRecorder 捕获管理员用户摘要响应。
	adminUsersRecorder := httptest.NewRecorder()
	handler.ServeHTTP(adminUsersRecorder, adminUsersRequest)
	assertOpenAPISuccessResponse(t, adminUsersRequest, adminUsersRecorder)

	// adminCookiesRequest 是管理员读取账号非敏感摘要的成功请求。
	adminCookiesRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/cookies", nil)
	adminCookiesRequest.AddCookie(sessionCookie)
	// adminCookiesRecorder 捕获管理员账号摘要响应。
	adminCookiesRecorder := httptest.NewRecorder()
	handler.ServeHTTP(adminCookiesRecorder, adminCookiesRequest)
	assertOpenAPISuccessResponse(t, adminCookiesRequest, adminCookiesRecorder)
	if strings.Contains(adminCookiesRecorder.Body.String(), "cookie_value") || strings.Contains(adminCookiesRecorder.Body.String(), "password") {
		t.Fatalf("管理员账号摘要泄漏敏感字段: %s", adminCookiesRecorder.Body.String())
	}

	// adminTasksRequest 是管理员读取后台任务状态的成功请求。
	adminTasksRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/tasks?limit=1", nil)
	adminTasksRequest.AddCookie(sessionCookie)
	// adminTasksRecorder 捕获后台任务状态响应。
	adminTasksRecorder := httptest.NewRecorder()
	handler.ServeHTTP(adminTasksRecorder, adminTasksRequest)
	assertOpenAPISuccessResponse(t, adminTasksRequest, adminTasksRecorder)
}

// TestOpenAPIItemAndCardResponses 验证阶段四商品与卡券查询的真实成功、未认证和文件错误响应符合 OpenAPI。
func TestOpenAPIItemAndCardResponses(t *testing.T) {
	// srv、_、cleanup 分别是测试 Server、无需直接访问的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含商品和卡券版本化路由的真实 chi Router。
	handler := srv.Router()
	// sessionCookie 是管理员会话 Cookie，用于受保护资源的成功场景。
	sessionCookie := loginHelper(t, handler)
	// unauthenticatedRequest 是缺少会话的商品请求，必须使用统一错误 envelope。
	unauthenticatedRequest := httptest.NewRequest(http.MethodGet, "/api/v1/items", nil)
	// unauthenticatedRecorder 捕获未认证商品响应。
	unauthenticatedRecorder := httptest.NewRecorder()
	handler.ServeHTTP(unauthenticatedRecorder, unauthenticatedRequest)
	assertOpenAPIResponse(t, unauthenticatedRequest, unauthenticatedRecorder)

	// itemsRequest 是带账号筛选的商品列表成功请求。
	itemsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/items?cookie_id=acc1", nil)
	itemsRequest.AddCookie(sessionCookie)
	// itemsRecorder 捕获商品列表响应。
	itemsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(itemsRecorder, itemsRequest)
	assertOpenAPISuccessResponse(t, itemsRequest, itemsRecorder)

	// cardsRequest 是卡券列表成功请求。
	cardsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/cards", nil)
	cardsRequest.AddCookie(sessionCookie)
	// cardsRecorder 捕获卡券列表响应。
	cardsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(cardsRecorder, cardsRequest)
	assertOpenAPISuccessResponse(t, cardsRequest, cardsRecorder)

	// createCardRequest 是创建 data 卡券的版本化请求，供后续资源生命周期操作复用。
	createCardRequest := httptest.NewRequest(http.MethodPost, "/api/v1/cards", strings.NewReader(`{"name":"OpenAPI 数据卡","type":"data","data_content":"K1","enabled":true}`))
	createCardRequest.AddCookie(sessionCookie)
	// createCardRecorder 捕获创建操作的具名数值主键响应。
	createCardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createCardRecorder, createCardRequest)
	assertOpenAPISuccessResponse(t, createCardRequest, createCardRecorder)
	// createdCard 保存创建操作返回的数值主键，后续请求必须使用真实归属资源。
	var createdCard struct {
		// ID 是本次生命周期中由服务端分配的卡券标识。
		ID int64 `json:"id"`
	}
	// decodeErr 表示无法从已校验响应中读取资源标识的失败原因。
	if decodeErr := json.Unmarshal(createCardRecorder.Body.Bytes(), &createdCard); decodeErr != nil || createdCard.ID <= 0 {
		t.Fatalf("读取创建卡券标识失败: id=%d err=%v", createdCard.ID, decodeErr)
	}
	// cardID 是 URL 路径使用的真实卡券标识字符串。
	cardID := strconv.FormatInt(createdCard.ID, 10)
	// getCardRequest 是读取刚创建资源详情的成功请求。
	getCardRequest := httptest.NewRequest(http.MethodGet, "/api/v1/cards/"+cardID, nil)
	getCardRequest.AddCookie(sessionCookie)
	// getCardRecorder 捕获详情 DTO，验证不再错误使用通用成功 envelope。
	getCardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getCardRecorder, getCardRequest)
	assertOpenAPISuccessResponse(t, getCardRequest, getCardRecorder)
	// updateCardRequest 是更新同一 data 卡券的成功请求。
	updateCardRequest := httptest.NewRequest(http.MethodPut, "/api/v1/cards/"+cardID, strings.NewReader(`{"name":"OpenAPI 数据卡更新","type":"data","data_content":"K1","enabled":true}`))
	updateCardRequest.AddCookie(sessionCookie)
	// updateCardRecorder 捕获更新成功响应。
	updateCardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateCardRecorder, updateCardRequest)
	assertOpenAPISuccessResponse(t, updateCardRequest, updateCardRecorder)
	// appendCardRequest 是为 data 卡券追加库存的成功请求。
	appendCardRequest := httptest.NewRequest(http.MethodPost, "/api/v1/cards/"+cardID+"/append-data", strings.NewReader(`{"content":"K2\nK3"}`))
	appendCardRequest.AddCookie(sessionCookie)
	// appendCardRecorder 捕获追加数量响应。
	appendCardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(appendCardRecorder, appendCardRequest)
	assertOpenAPISuccessResponse(t, appendCardRequest, appendCardRecorder)
	// deleteCardRequest 是删除同一资源的成功请求，保证测试不依赖虚构路径参数。
	deleteCardRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/cards/"+cardID, nil)
	deleteCardRequest.AddCookie(sessionCookie)
	// deleteCardRecorder 捕获删除成功响应。
	deleteCardRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteCardRecorder, deleteCardRequest)
	assertOpenAPISuccessResponse(t, deleteCardRequest, deleteCardRecorder)

	// invalidUploadRequest 是缺失文件的卡券上传请求，必须返回统一错误 envelope。
	invalidUploadRequest := httptest.NewRequest(http.MethodPost, "/api/v1/cards/batch", strings.NewReader("--openapi--\r\n"))
	invalidUploadRequest.Header.Set("Content-Type", "multipart/form-data; boundary=openapi")
	invalidUploadRequest.AddCookie(sessionCookie)
	// invalidUploadRecorder 捕获卡券上传格式错误响应。
	invalidUploadRecorder := httptest.NewRecorder()
	handler.ServeHTTP(invalidUploadRecorder, invalidUploadRequest)
	assertOpenAPIResponse(t, invalidUploadRequest, invalidUploadRecorder)
}

// TestOpenAPINotificationResponses 验证阶段五通知摘要和动态账号绑定键符合 OpenAPI 约束。
func TestOpenAPINotificationResponses(t *testing.T) {
	// srv、_、cleanup 分别是测试 Server、无需直接访问的存储和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含通知版本化路由的真实 chi Router。
	handler := srv.Router()
	// sessionCookie 是管理员会话 Cookie，用于读取当前用户通知资源。
	sessionCookie := loginHelper(t, handler)
	// channelsRequest 是读取非敏感通知渠道摘要的成功请求。
	channelsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/channels", nil)
	channelsRequest.AddCookie(sessionCookie)
	// channelsRecorder 捕获通知渠道摘要响应。
	channelsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(channelsRecorder, channelsRequest)
	assertOpenAPISuccessResponse(t, channelsRequest, channelsRecorder)
	if strings.Contains(channelsRecorder.Body.String(), "smtp_password") || strings.Contains(channelsRecorder.Body.String(), "token") {
		t.Fatalf("通知渠道摘要泄漏秘密配置: %s", channelsRecorder.Body.String())
	}

	// bindingsRequest 是读取按账号动态键组织的通知绑定成功请求。
	bindingsRequest := httptest.NewRequest(http.MethodGet, "/api/v1/notifications/messages", nil)
	bindingsRequest.AddCookie(sessionCookie)
	// bindingsRecorder 捕获动态账号键通知绑定响应。
	bindingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(bindingsRecorder, bindingsRequest)
	assertOpenAPISuccessResponse(t, bindingsRequest, bindingsRecorder)

	// createChannelRequest 是创建通知渠道的版本化成功请求，配置字段只使用测试占位值。
	createChannelRequest := httptest.NewRequest(http.MethodPost, "/api/v1/notifications/channels", strings.NewReader(`{"name":"OpenAPI 渠道","type":"webhook","config":"{}","event_types":"[]","enabled":true}`))
	createChannelRequest.AddCookie(sessionCookie)
	// createChannelRecorder 捕获渠道创建返回的数值主键。
	createChannelRecorder := httptest.NewRecorder()
	handler.ServeHTTP(createChannelRecorder, createChannelRequest)
	assertOpenAPISuccessResponse(t, createChannelRequest, createChannelRecorder)
	// createdChannel 保存创建响应中的渠道标识。
	var createdChannel struct {
		// ID 是服务端分配的通知渠道主键。
		ID int64 `json:"id"`
	}
	// channelDecodeErr 表示创建响应无法解析为具名主键 DTO 的失败原因。
	if channelDecodeErr := json.Unmarshal(createChannelRecorder.Body.Bytes(), &createdChannel); channelDecodeErr != nil || createdChannel.ID <= 0 {
		t.Fatalf("读取通知渠道标识失败: id=%d err=%v", createdChannel.ID, channelDecodeErr)
	}
	// channelID 是通知渠道路径参数的稳定字符串形式。
	channelID := strconv.FormatInt(createdChannel.ID, 10)
	// updateChannelRequest 是仅切换启用状态的部分更新成功请求。
	updateChannelRequest := httptest.NewRequest(http.MethodPut, "/api/v1/notifications/channels/"+channelID, strings.NewReader(`{"enabled":false}`))
	updateChannelRequest.AddCookie(sessionCookie)
	// updateChannelRecorder 捕获渠道更新成功响应。
	updateChannelRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateChannelRecorder, updateChannelRequest)
	assertOpenAPISuccessResponse(t, updateChannelRequest, updateChannelRecorder)
	// deleteChannelRequest 是删除已归属通知渠道的成功请求。
	deleteChannelRequest := httptest.NewRequest(http.MethodDelete, "/api/v1/notifications/channels/"+channelID, nil)
	deleteChannelRequest.AddCookie(sessionCookie)
	// deleteChannelRecorder 捕获渠道删除成功响应。
	deleteChannelRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteChannelRecorder, deleteChannelRequest)
	assertOpenAPISuccessResponse(t, deleteChannelRequest, deleteChannelRecorder)
}
