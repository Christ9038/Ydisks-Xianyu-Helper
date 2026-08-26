package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	cardsapp "xianyu-go/internal/application/cards"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

// cardAPICoverageTester 是卡券 API 测试 Handler 使用的可编程应用替身。
type cardAPICoverageTester struct {
	// result、err 保存临时 API 测试的预置结果和错误。
	result cardsapp.APIRequestTestResult
	err    error
}

// Test 返回卡券 API 测试的预置诊断结果。
func (tester cardAPICoverageTester) Test(context.Context, cardsapp.APIRequestTestInput) (cardsapp.APIRequestTestResult, error) {
	return tester.result, tester.err
}

// requestWithServerSession 创建带认证会话和可选路径参数的直接 Handler 请求。
func requestWithServerSession(method, path string, params map[string]string) *http.Request {
	// request 是供单个 Handler 使用的本地 HTTP 请求。
	request := httptest.NewRequest(method, path, nil)
	// requestContext 保存认证会话和 chi 路径参数上下文。
	requestContext := auth.WithSession(request.Context(), &db.Session{UserID: 1, Username: "admin", IsAdmin: true})
	if len(params) > 0 {
		// routeContext 保存直接调用 Handler 时缺少的路径变量。
		routeContext := chi.NewRouteContext()
		routeContext.URLParams.Add("cid", params["cid"])
		requestContext = context.WithValue(requestContext, chi.RouteCtxKey, routeContext)
	}
	return request.WithContext(requestContext)
}

// TestLowCoverageHandlersCoverDatabaseAndResourceErrors 覆盖卡券、订单和通知 Handler 的错误映射。
func TestLowCoverageHandlersCoverDatabaseAndResourceErrors(t *testing.T) {
	// cardServer、cardStore、cardCleanup 保存卡券列表数据库错误场景。
	cardServer, cardStore, cardCleanup := newTestServer(t)
	defer cardCleanup()
	// cardRequest、cardRecorder 保存直接调用卡券列表 Handler 的请求和响应。
	cardRequest := requestWithServerSession(http.MethodGet, "/cards", nil)
	// cardRecorder 保存卡券列表数据库错误响应。
	cardRecorder := httptest.NewRecorder()
	// closeErr 保存关闭卡券测试数据库连接的结果。
	if closeErr := cardStore.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	cardServer.listCards(cardRecorder, cardRequest)
	if cardRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("卡券列表数据库错误 status=%d body=%s", cardRecorder.Code, cardRecorder.Body.String())
	}

	// orderServer、orderStore、orderCleanup 保存订单列表数据库错误场景。
	orderServer, orderStore, orderCleanup := newTestServer(t)
	defer orderCleanup()
	// orderRequest、orderRecorder 保存直接调用订单列表 Handler 的请求和响应。
	orderRequest := requestWithServerSession(http.MethodGet, "/api/orders", nil)
	// orderRecorder 保存订单列表数据库错误响应。
	orderRecorder := httptest.NewRecorder()
	// closeErr 保存关闭订单测试数据库连接的结果。
	if closeErr := orderStore.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	orderServer.listOrders(orderRecorder, orderRequest)
	if orderRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("订单列表数据库错误 status=%d body=%s", orderRecorder.Code, orderRecorder.Body.String())
	}

	// notificationServer、notificationStore、notificationCleanup 保存通知账号错误场景。
	notificationServer, notificationStore, notificationCleanup := newTestServer(t)
	defer notificationCleanup()
	// missingRequest、missingRecorder 保存不存在账号通知绑定的请求和响应。
	missingRequest := requestWithServerSession(http.MethodDelete, "/notifications/accounts/missing", map[string]string{"cid": "missing"})
	// missingRecorder 保存不存在账号通知绑定的响应。
	missingRecorder := httptest.NewRecorder()
	notificationServer.deleteAccountNotifications(missingRecorder, missingRequest)
	if missingRecorder.Code != http.StatusNotFound {
		t.Fatalf("不存在账号通知绑定 status=%d body=%s", missingRecorder.Code, missingRecorder.Body.String())
	}
	// closedRequest、closedRecorder 保存通知数据库关闭后的请求和响应。
	closedRequest := requestWithServerSession(http.MethodDelete, "/notifications/accounts/cid", map[string]string{"cid": "cid"})
	// closedRecorder 保存通知数据库错误响应。
	closedRecorder := httptest.NewRecorder()
	// closeErr 保存关闭通知测试数据库连接的结果。
	if closeErr := notificationStore.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	notificationServer.deleteAccountNotifications(closedRecorder, closedRequest)
	if closedRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("通知绑定数据库错误 status=%d body=%s", closedRecorder.Code, closedRecorder.Body.String())
	}
}

// TestCardAPIHandlerCoversValidationAndApplicationErrors 覆盖卡券 API 测试 Handler 的依赖、解码和应用错误。
func TestCardAPIHandlerCoversValidationAndApplicationErrors(t *testing.T) {
	// server、cleanup 保存卡券 API 测试 Handler 使用的服务器。
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	// request、recorder 保存当前 API 测试请求和响应。
	request := httptest.NewRequest(http.MethodPost, "/cards/test-api", strings.NewReader(`{"api_config":{}}`))
	// recorder 保存缺少 API 测试 Port 的响应。
	recorder := httptest.NewRecorder()
	server.applications.apiRequestTester = nil
	server.testCardAPI(recorder, request)
	if recorder.Code != http.StatusServiceUnavailable {
		t.Fatalf("缺少 API 测试服务 status=%d body=%s", recorder.Code, recorder.Body.String())
	}
	// malformedRecorder 保存非法 JSON 响应。
	malformedRecorder := httptest.NewRecorder()
	server.applications.apiRequestTester = cardAPICoverageTester{}
	server.testCardAPI(malformedRecorder, httptest.NewRequest(http.MethodPost, "/cards/test-api", strings.NewReader("{")))
	if malformedRecorder.Code != http.StatusBadRequest {
		t.Fatalf("非法 API 测试 JSON status=%d", malformedRecorder.Code)
	}
	// invalidConfigRecorder 保存非法 API 配置响应。
	invalidConfigRecorder := httptest.NewRecorder()
	server.testCardAPI(invalidConfigRecorder, httptest.NewRequest(http.MethodPost, "/cards/test-api", strings.NewReader(`{"api_config":[]}`)))
	if invalidConfigRecorder.Code != http.StatusBadRequest {
		t.Fatalf("非法 API 配置 status=%d", invalidConfigRecorder.Code)
	}
	// applicationErrorRecorder 保存应用层 API 测试错误响应。
	applicationErrorRecorder := httptest.NewRecorder()
	server.applications.apiRequestTester = cardAPICoverageTester{err: errors.New("api test failed")}
	server.testCardAPI(applicationErrorRecorder, httptest.NewRequest(http.MethodPost, "/cards/test-api", strings.NewReader(`{"api_config":{"url":"https://example.com"}}`)))
	if applicationErrorRecorder.Code != http.StatusBadRequest {
		t.Fatalf("应用 API 测试错误 status=%d", applicationErrorRecorder.Code)
	}
	// successRecorder 保存应用层 API 测试成功响应。
	successRecorder := httptest.NewRecorder()
	server.applications.apiRequestTester = cardAPICoverageTester{result: cardsapp.APIRequestTestResult{Status: "success", StatusCode: http.StatusOK}}
	server.testCardAPI(successRecorder, httptest.NewRequest(http.MethodPost, "/cards/test-api", strings.NewReader(`{"api_config":{"url":"https://example.com"}}`)))
	if successRecorder.Code != http.StatusOK {
		t.Fatalf("成功 API 测试 status=%d body=%s", successRecorder.Code, successRecorder.Body.String())
	}
}

// TestLoginCoversEmailCredentialPath 覆盖登录 Handler 的邮箱映射和成功建立会话路径。
func TestLoginCoversEmailCredentialPath(t *testing.T) {
	// server、cleanup 保存带管理员账号的测试服务器。
	server, store, cleanup := newTestServer(t)
	defer cleanup()
	// admin、adminErr 保存测试服务器预置管理员的邮箱地址。
	admin, adminErr := store.Users.GetAdmin(context.Background())
	if adminErr != nil || admin == nil {
		t.Fatalf("读取管理员失败 admin=%+v err=%v", admin, adminErr)
	}
	// request 是使用邮箱字段登录的本地请求。
	request := httptest.NewRequest(http.MethodPost, "/login", strings.NewReader(`{"email":"`+admin.Email+`","password":"pw"}`))
	// recorder 保存邮箱登录响应。
	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, request)
	if recorder.Code != http.StatusOK || len(recorder.Result().Cookies()) == 0 {
		t.Fatalf("邮箱登录 status=%d cookies=%d body=%s", recorder.Code, len(recorder.Result().Cookies()), recorder.Body.String())
	}
}

// assertRefreshSingleHandlerStatus 配置订单刷新 Port 并验证 Handler 的状态码映射。
func assertRefreshSingleHandlerStatus(t *testing.T, server *Server, result orderapp.SingleRefreshResult, returnedErr error, wantStatus int) {
	t.Helper()
	// refreshSingleResult、refreshSingleErr 保存本次测试预置的应用层结果和错误。
	refreshSingleResult, refreshSingleErr := result, returnedErr
	// server应用订单 Port 返回本次测试预置的刷新结果。
	server.applications.orders = orderAdapterPortFake{refreshSingleFn: func(context.Context, int64, string) (orderapp.SingleRefreshResult, error) {
		return refreshSingleResult, refreshSingleErr
	}}
	// request、recorder 保存直接调用订单刷新 Handler 的请求和响应。
	request := requestWithServerSession(http.MethodPost, "/api/orders/o1/refresh", map[string]string{"order_id": "o1"})
	// recorder 保存订单刷新 Handler 的 HTTP 响应。
	recorder := httptest.NewRecorder()
	server.refreshSingleOrder(recorder, request)
	if recorder.Code != wantStatus {
		t.Fatalf("刷新订单 err=%v status=%d want=%d body=%s", returnedErr, recorder.Code, wantStatus, recorder.Body.String())
	}
}

// TestRefreshSingleOrderHandlerCoversAllErrorMappings 覆盖单订单刷新 Handler 的全部错误和结果分支。
func TestRefreshSingleOrderHandlerCoversAllErrorMappings(t *testing.T) {
	// server、cleanup 保存订单刷新 Handler 使用的测试服务器。
	server, _, cleanup := newTestServer(t)
	defer cleanup()
	// refreshCases 保存订单刷新应用错误到 HTTP 状态码的映射用例。
	refreshCases := []struct {
		// name 是当前状态映射用例名称。
		name string
		// returnedErr 是应用层返回的错误。
		returnedErr error
		// result 是应用层返回的刷新结果。
		result orderapp.SingleRefreshResult
		// wantStatus 是 Handler 应返回的 HTTP 状态码。
		wantStatus int
	}{
		{name: "not found", returnedErr: orderapp.ErrNotFound, wantStatus: http.StatusNotFound},
		{name: "forbidden", returnedErr: orderapp.ErrForbidden, wantStatus: http.StatusForbidden},
		{name: "unsupported", returnedErr: orderapp.ErrRefreshDetailUnsupported, wantStatus: http.StatusServiceUnavailable},
		{name: "credential changed", returnedErr: orderapp.ErrRefreshCredentialChanged, wantStatus: http.StatusConflict},
		{name: "generic", returnedErr: errors.New("refresh failed"), wantStatus: http.StatusBadGateway},
		{name: "unsuccessful result", result: orderapp.SingleRefreshResult{Success: false}, wantStatus: http.StatusInternalServerError},
		{name: "success", result: orderapp.SingleRefreshResult{Success: true, Message: "ok"}, wantStatus: http.StatusOK},
	}
	// refreshCase 表示当前遍历中的订单刷新状态映射用例。
	for _, refreshCase := range refreshCases {
		t.Run(refreshCase.name, func(t *testing.T) {
			assertRefreshSingleHandlerStatus(t, server, refreshCase.result, refreshCase.returnedErr, refreshCase.wantStatus)
		})
	}
}
