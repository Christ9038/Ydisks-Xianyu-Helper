package server

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"
	"github.com/getkin/kin-openapi/openapi3filter"
	"github.com/getkin/kin-openapi/routers/legacy"
)

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
