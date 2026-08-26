package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	accountapp "xianyu-go/internal/application/account"
	adminapp "xianyu-go/internal/application/admin"
	analyticsapp "xianyu-go/internal/application/analytics"
	automationapp "xianyu-go/internal/application/automation"
)

// accountTaskHandlerErrorPort 为账号任务 handler 提供可控的应用层错误结果。
type accountTaskHandlerErrorPort struct {
	// getErr 保存读取任务设置时要返回的错误。
	getErr error
	// updateErr 保存更新任务设置时要返回的错误。
	updateErr error
	// listErr 保存读取任务运行记录时要返回的错误。
	listErr error
	// runErr 保存手动执行任务时要返回的错误。
	runErr error
}

// GetSettings 返回预置的读取设置结果或错误。
func (port accountTaskHandlerErrorPort) GetSettings(context.Context, string) (automationapp.AccountTaskSettings, error) {
	return automationapp.AccountTaskSettings{}, port.getErr
}

// UpdateSettings 返回预置的更新设置结果或错误。
func (port accountTaskHandlerErrorPort) UpdateSettings(context.Context, automationapp.AccountTaskSettings) (automationapp.AccountTaskSettings, error) {
	return automationapp.AccountTaskSettings{}, port.updateErr
}

// ListRuns 返回预置的运行记录结果或错误。
func (port accountTaskHandlerErrorPort) ListRuns(context.Context, string, int) ([]automationapp.AccountTaskRun, error) {
	return nil, port.listErr
}

// Run 返回预置的任务执行结果或错误。
func (port accountTaskHandlerErrorPort) Run(context.Context, string, string) (automationapp.TaskSummary, error) {
	return automationapp.TaskSummary{}, port.runErr
}

// TestAccountTaskHandlersCoverSuccessAndRequestValidation 验证账号任务 handler 的成功、非法 JSON 和任务类型分支。
func TestAccountTaskHandlersCoverSuccessAndRequestValidation(t *testing.T) {
	// srv、cleanup 保存测试服务器及资源释放责任。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// srv.applications.accountTasks 注入确定性的成功应用端口，隔离真实任务执行。
	srv.applications.accountTasks = contractAccountTasksPort{}
	// handler 是注入应用端口后的真实 HTTP 路由。
	handler := srv.Router()
	// cookie 是通过真实认证流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// settingsRequest 是读取账号任务设置的成功请求。
	settingsRequest := httptest.NewRequest(http.MethodGet, "/api/account-tasks/acc1", nil)
	settingsRequest.AddCookie(cookie)
	// settingsRecorder 保存读取设置的 HTTP 响应。
	settingsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(settingsRecorder, settingsRequest)
	if settingsRecorder.Code != http.StatusOK || !strings.Contains(settingsRecorder.Body.String(), "account_id") {
		t.Fatalf("读取任务设置 status=%d body=%s", settingsRecorder.Code, settingsRecorder.Body.String())
	}

	// runsRequest 是读取账号任务历史的成功请求。
	runsRequest := httptest.NewRequest(http.MethodGet, "/api/account-tasks/acc1/runs?limit=2", nil)
	runsRequest.AddCookie(cookie)
	// runsRecorder 保存读取任务历史的 HTTP 响应。
	runsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runsRecorder, runsRequest)
	if runsRecorder.Code != http.StatusOK || !strings.Contains(runsRecorder.Body.String(), "runs") {
		t.Fatalf("读取任务历史 status=%d body=%s", runsRecorder.Code, runsRecorder.Body.String())
	}

	// runRequest 是执行自动评价任务的成功请求。
	runRequest := httptest.NewRequest(http.MethodPost, "/api/account-tasks/acc1/run", strings.NewReader(`{"task_type":"auto_rate"}`))
	runRequest.Header.Set("Content-Type", "application/json")
	runRequest.AddCookie(cookie)
	// runRecorder 保存成功执行任务的 HTTP 响应。
	runRecorder := httptest.NewRecorder()
	handler.ServeHTTP(runRecorder, runRequest)
	if runRecorder.Code != http.StatusOK || !strings.Contains(runRecorder.Body.String(), "summary") {
		t.Fatalf("执行任务 status=%d body=%s", runRecorder.Code, runRecorder.Body.String())
	}

	// invalidRequests 保存应由请求层拒绝的账号任务请求。
	invalidRequests := []struct {
		// body 是请求体原文。
		body string
		// want 是预期 HTTP 状态码。
		want int
	}{
		{body: "not-json", want: http.StatusBadRequest},
		{body: `{"task_type":"unsupported"}`, want: http.StatusBadRequest},
	}
	// invalidRequest 表示当前待验证的非法任务请求。
	for _, invalidRequest := range invalidRequests {
		// request 是当前非法任务请求。
		request := httptest.NewRequest(http.MethodPost, "/api/account-tasks/acc1/run", strings.NewReader(invalidRequest.body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		// recorder 保存当前非法任务请求的 HTTP 响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != invalidRequest.want {
			t.Errorf("非法任务请求 body=%q status=%d want=%d", invalidRequest.body, recorder.Code, invalidRequest.want)
		}
	}

	// forbiddenRequest 是访问不存在账号的任务设置请求。
	forbiddenRequest := httptest.NewRequest(http.MethodGet, "/api/account-tasks/missing", nil)
	forbiddenRequest.AddCookie(cookie)
	// forbiddenRecorder 保存越权请求的 HTTP 响应。
	forbiddenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRecorder, forbiddenRequest)
	if forbiddenRecorder.Code != http.StatusForbidden {
		t.Fatalf("越权任务设置 status=%d body=%s", forbiddenRecorder.Code, forbiddenRecorder.Body.String())
	}
}

// TestAccountTaskHandlersCoverApplicationErrors 验证账号任务 handler 对应用层错误的 HTTP 映射。
func TestAccountTaskHandlersCoverApplicationErrors(t *testing.T) {
	// srv、cleanup 保存测试服务器及资源释放责任。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是测试服务器的真实 HTTP 路由。
	handler := srv.Router()
	// cookie 是通过真实认证流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// errorCases 保存 handler 错误到 HTTP 状态码的映射样本。
	errorCases := []struct {
		// path 是当前待调用的任务接口路径。
		path string
		// method 是当前待调用的 HTTP 方法。
		method string
		// body 是当前请求体。
		body string
		// port 是注入的应用层错误替身。
		port accountTaskHandlerErrorPort
		// want 是预期 HTTP 状态码。
		want int
	}{
		{path: "/api/account-tasks/acc1", method: http.MethodGet, port: accountTaskHandlerErrorPort{getErr: errors.New("get failed")}, want: http.StatusInternalServerError},
		{path: "/api/account-tasks/acc1", method: http.MethodPut, body: `{"auto_rate_enabled":true,"rate_content":"ok","auto_polish_enabled":false,"polish_time":"03:00"}`, port: accountTaskHandlerErrorPort{updateErr: errors.New("保存失败")}, want: http.StatusInternalServerError},
		{path: "/api/account-tasks/acc1", method: http.MethodPut, body: `{"auto_rate_enabled":true,"rate_content":"ok","auto_polish_enabled":false,"polish_time":"03:00"}`, port: accountTaskHandlerErrorPort{updateErr: errors.New("内容不能为空")}, want: http.StatusBadRequest},
		{path: "/api/account-tasks/acc1/runs", method: http.MethodGet, port: accountTaskHandlerErrorPort{listErr: errors.New("list failed")}, want: http.StatusInternalServerError},
		{path: "/api/account-tasks/acc1/run", method: http.MethodPost, body: `{"task_type":"auto_rate"}`, port: accountTaskHandlerErrorPort{runErr: automationapp.ErrUnavailable}, want: http.StatusServiceUnavailable},
		{path: "/api/account-tasks/acc1/run", method: http.MethodPost, body: `{"task_type":"auto_rate"}`, port: accountTaskHandlerErrorPort{runErr: errors.New("platform failed")}, want: http.StatusBadGateway},
	}
	// errorCase 表示当前待验证的应用错误映射样本。
	for _, errorCase := range errorCases {
		srv.applications.accountTasks = errorCase.port
		// request 是当前错误映射样本的 HTTP 请求。
		request := httptest.NewRequest(errorCase.method, errorCase.path, strings.NewReader(errorCase.body))
		request.Header.Set("Content-Type", "application/json")
		request.AddCookie(cookie)
		// recorder 保存当前错误映射样本的 HTTP 响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != errorCase.want {
			t.Errorf("任务错误 path=%s status=%d want=%d body=%s", errorCase.path, recorder.Code, errorCase.want, recorder.Body.String())
		}
	}
}

// TestAccountTaskRunResponseConversionCopiesAllFields 验证账号任务运行记录转换不会丢失状态和重试字段。
func TestAccountTaskRunResponseConversionCopiesAllFields(t *testing.T) {
	// runs 是覆盖 HTTP DTO 全部字段的应用层运行记录。
	runs := []automationapp.AccountTaskRun{{
		ID: 1, RunKey: "run-key", CookieID: "cid", TaskType: automationapp.TaskAutoRate,
		TargetID: "target", RunDate: "2026-08-26", Status: "failed", SuccessCount: 2,
		FailedCount: 1, ErrorMessage: "retry", NextRetryAt: 10, StartedAt: 11, FinishedAt: 12,
	}}
	// responses 是转换后的 HTTP 运行记录响应。
	responses := newApplicationAccountTaskRunResponses(runs)
	if len(responses) != 1 || responses[0].RunKey != "run-key" || responses[0].AccountID != "cid" || responses[0].FailedCount != 1 || responses[0].FinishedAt != 12 {
		t.Fatalf("运行记录转换异常 responses=%+v", responses)
	}
}

// analyticsHandlerCoveragePort 为分析 handler 提供可控的成功和错误结果。
type analyticsHandlerCoveragePort struct {
	// dashboardErr 保存仪表盘查询错误。
	dashboardErr error
	// orderErr 保存订单分析查询错误。
	orderErr error
	// validErr 保存有效订单查询错误。
	validErr error
	// gotPage 保存最近一次有效订单请求的页码。
	gotPage int
	// gotPageSize 保存最近一次有效订单请求的分页大小。
	gotPageSize int
}

// DashboardStats 返回覆盖字段的仪表盘结果或预置错误。
func (port *analyticsHandlerCoveragePort) DashboardStats(context.Context, int64) (analyticsapp.DashboardStats, error) {
	return analyticsapp.DashboardStats{TotalCookies: 1, ActiveCookies: 1, TotalCards: 2, AvailableCardStock: 3, TotalKeywords: 4, TotalOrders: 5}, port.dashboardErr
}

// OrderAnalytics 返回覆盖列表映射的订单分析结果或预置错误。
func (port *analyticsHandlerCoveragePort) OrderAnalytics(context.Context, analyticsapp.Query) (analyticsapp.OrderAnalytics, error) {
	return analyticsapp.OrderAnalytics{
		RevenueStats: analyticsapp.RevenueStats{TotalOrders: 1, TotalAmount: 2, AvgAmount: 2, UniqueBuyers: 1, UniqueItems: 1},
		DailyStats:   []analyticsapp.DailyStats{{Date: "2026-08-26", OrderCount: 1, Amount: 2}},
		StatusStats:  []analyticsapp.StatusStats{{Status: "paid", Count: 1, Amount: 2}},
		CityStats:    []analyticsapp.CityStats{{City: "上海", OrderCount: 1, TotalAmount: 2}},
		ItemStats:    []analyticsapp.ItemStats{{ItemID: "item", OrderCount: 1, TotalAmount: 2, AvgAmount: 2}},
	}, port.orderErr
}

// ValidOrders 返回记录分页结果并保存 handler 传入的安全分页参数。
func (port *analyticsHandlerCoveragePort) ValidOrders(_ context.Context, _ analyticsapp.Query, page, pageSize int) (analyticsapp.ValidOrders, error) {
	port.gotPage, port.gotPageSize = page, pageSize
	return analyticsapp.ValidOrders{Page: page, PageSize: pageSize}, port.validErr
}

// TestAnalyticsHandlersCoverSuccessPaginationAndErrors 验证分析 handler 的响应映射、分页归一和应用错误分支。
func TestAnalyticsHandlersCoverSuccessPaginationAndErrors(t *testing.T) {
	// srv、cleanup 保存测试服务器及资源释放责任。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是隔离真实数据库查询的分析应用替身。
	port := &analyticsHandlerCoveragePort{}
	srv.applications.analytics = port
	// handler 是注入分析应用替身后的真实 HTTP 路由。
	handler := srv.Router()
	// cookie 是通过真实认证流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// statsRequest 是仪表盘统计成功请求。
	statsRequest := httptest.NewRequest(http.MethodGet, "/dashboard/stats", nil)
	statsRequest.AddCookie(cookie)
	// statsRecorder 保存仪表盘统计响应。
	statsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statsRecorder, statsRequest)
	if statsRecorder.Code != http.StatusOK || !strings.Contains(statsRecorder.Body.String(), "total_cookies") {
		t.Fatalf("仪表盘统计 status=%d body=%s", statsRecorder.Code, statsRecorder.Body.String())
	}

	// orderRequest 是订单分析成功请求。
	orderRequest := httptest.NewRequest(http.MethodGet, "/analytics/orders", nil)
	orderRequest.AddCookie(cookie)
	// orderRecorder 保存订单分析响应。
	orderRecorder := httptest.NewRecorder()
	handler.ServeHTTP(orderRecorder, orderRequest)
	if orderRecorder.Code != http.StatusOK || !strings.Contains(orderRecorder.Body.String(), "revenue_stats") {
		t.Fatalf("订单分析 status=%d body=%s", orderRecorder.Code, orderRecorder.Body.String())
	}

	// validRequest 是带越界分页参数的有效订单请求。
	validRequest := httptest.NewRequest(http.MethodGet, "/analytics/orders/valid?page=0&page_size=501", nil)
	validRequest.AddCookie(cookie)
	// validRecorder 保存有效订单响应。
	validRecorder := httptest.NewRecorder()
	handler.ServeHTTP(validRecorder, validRequest)
	if validRecorder.Code != http.StatusOK || port.gotPage != 1 || port.gotPageSize != 500 {
		t.Fatalf("有效订单分页 status=%d page=%d page_size=%d body=%s", validRecorder.Code, port.gotPage, port.gotPageSize, validRecorder.Body.String())
	}

	// errorCases 保存分析应用错误对应的请求路径和 HTTP 状态码。
	errorCases := []struct {
		// path 是当前错误请求路径。
		path string
		// setError 将对应错误注入分析应用替身。
		setError func(*analyticsHandlerCoveragePort)
	}{
		{path: "/dashboard/stats", setError: func(port *analyticsHandlerCoveragePort) { port.dashboardErr = errors.New("dashboard failed") }},
		{path: "/analytics/orders", setError: func(port *analyticsHandlerCoveragePort) { port.orderErr = errors.New("orders failed") }},
		{path: "/analytics/orders/valid", setError: func(port *analyticsHandlerCoveragePort) { port.validErr = errors.New("valid orders failed") }},
	}
	// errorCase 表示当前待验证的分析错误请求。
	for _, errorCase := range errorCases {
		// errorPort 是当前错误请求专用的分析应用替身。
		errorPort := &analyticsHandlerCoveragePort{}
		errorCase.setError(errorPort)
		srv.applications.analytics = errorPort
		// request 是当前分析错误请求。
		request := httptest.NewRequest(http.MethodGet, errorCase.path, nil)
		request.AddCookie(cookie)
		// recorder 保存当前分析错误响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != http.StatusInternalServerError {
			t.Errorf("分析错误 path=%s status=%d body=%s", errorCase.path, recorder.Code, recorder.Body.String())
		}
	}
}

// adminHandlerCoveragePort 为管理员 handler 提供可控的成功和错误结果。
type adminHandlerCoveragePort struct {
	// listErr 保存用户列表查询错误。
	listErr error
	// deleteErr 保存删除用户错误。
	deleteErr error
	// statsErr 保存管理员统计查询错误。
	statsErr error
}

// ListUsers 返回覆盖响应字段的管理员用户摘要或预置错误。
func (port *adminHandlerCoveragePort) ListUsers(context.Context) ([]adminapp.UserSummary, error) {
	return []adminapp.UserSummary{{ID: 2, Username: "user", Email: "user@example.com", IsActive: true, IsAdmin: false, CreatedAt: "2026-08-26", CookieCount: 1}}, port.listErr
}

// DeleteUser 返回预置的管理员删除结果。
func (port *adminHandlerCoveragePort) DeleteUser(context.Context, int64, int64) error {
	return port.deleteErr
}

// Stats 返回覆盖响应字段的管理员统计或预置错误。
func (port *adminHandlerCoveragePort) Stats(context.Context) (adminapp.Stats, error) {
	return adminapp.Stats{TotalUsers: 1, TotalCookies: 2, ActiveCookies: 1, TotalCards: 3, TotalKeywords: 4, TotalOrders: 5}, port.statsErr
}

// TestAdminHandlersCoverApplicationSuccessAndErrors 验证管理员 handler 的映射和应用错误状态码。
func TestAdminHandlersCoverApplicationSuccessAndErrors(t *testing.T) {
	// srv、cleanup 保存测试服务器及资源释放责任。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是隔离真实管理员数据库查询的应用替身。
	port := &adminHandlerCoveragePort{}
	srv.applications.admin = port
	// handler 是注入管理员应用替身后的真实 HTTP 路由。
	handler := srv.Router()
	// cookie 是通过真实认证流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// usersRequest 是管理员用户列表成功请求。
	usersRequest := httptest.NewRequest(http.MethodGet, "/admin/users", nil)
	usersRequest.AddCookie(cookie)
	// usersRecorder 保存管理员用户列表响应。
	usersRecorder := httptest.NewRecorder()
	handler.ServeHTTP(usersRecorder, usersRequest)
	if usersRecorder.Code != http.StatusOK || !strings.Contains(usersRecorder.Body.String(), "user@example.com") {
		t.Fatalf("管理员用户列表 status=%d body=%s", usersRecorder.Code, usersRecorder.Body.String())
	}

	// statsRequest 是管理员统计成功请求。
	statsRequest := httptest.NewRequest(http.MethodGet, "/admin/stats", nil)
	statsRequest.AddCookie(cookie)
	// statsRecorder 保存管理员统计响应。
	statsRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statsRecorder, statsRequest)
	if statsRecorder.Code != http.StatusOK || !strings.Contains(statsRecorder.Body.String(), "total_orders") {
		t.Fatalf("管理员统计 status=%d body=%s", statsRecorder.Code, statsRecorder.Body.String())
	}

	// deleteRequest 是管理员删除其他用户的成功请求。
	deleteRequest := httptest.NewRequest(http.MethodDelete, "/admin/users/2", nil)
	deleteRequest.AddCookie(cookie)
	// deleteRecorder 保存删除用户响应。
	deleteRecorder := httptest.NewRecorder()
	handler.ServeHTTP(deleteRecorder, deleteRequest)
	if deleteRecorder.Code != http.StatusOK {
		t.Fatalf("管理员删除 status=%d body=%s", deleteRecorder.Code, deleteRecorder.Body.String())
	}

	// errorCases 保存管理员应用错误到 HTTP 状态码的映射样本。
	errorCases := []struct {
		// path 是当前错误请求路径。
		path string
		// method 是当前错误请求方法。
		method string
		// port 是当前注入的错误应用替身。
		port *adminHandlerCoveragePort
		// want 是预期 HTTP 状态码。
		want int
	}{
		{path: "/admin/users", method: http.MethodGet, port: &adminHandlerCoveragePort{listErr: errors.New("list failed")}, want: http.StatusInternalServerError},
		{path: "/admin/stats", method: http.MethodGet, port: &adminHandlerCoveragePort{statsErr: errors.New("stats failed")}, want: http.StatusInternalServerError},
		{path: "/admin/users/2", method: http.MethodDelete, port: &adminHandlerCoveragePort{deleteErr: errors.New("delete failed")}, want: http.StatusInternalServerError},
		{path: "/admin/users/2", method: http.MethodDelete, port: &adminHandlerCoveragePort{deleteErr: adminapp.ErrSelfDelete}, want: http.StatusBadRequest},
	}
	// errorCase 表示当前待验证的管理员错误映射样本。
	for _, errorCase := range errorCases {
		srv.applications.admin = errorCase.port
		// request 是当前管理员错误请求。
		request := httptest.NewRequest(errorCase.method, errorCase.path, nil)
		request.AddCookie(cookie)
		// recorder 保存当前管理员错误响应。
		recorder := httptest.NewRecorder()
		handler.ServeHTTP(recorder, request)
		if recorder.Code != errorCase.want {
			t.Errorf("管理员错误 path=%s status=%d want=%d body=%s", errorCase.path, recorder.Code, errorCase.want, recorder.Body.String())
		}
	}
	// summaryPort 是仅让管理员账号列表返回错误的账号摘要应用替身。
	summaryPort := accountSummaryHandlerCoveragePort{listAdminErr: errors.New("summary failed")}
	srv.applications.accountSummaries = summaryPort
	// cookiesRequest 是管理员账号列表错误请求。
	cookiesRequest := httptest.NewRequest(http.MethodGet, "/admin/cookies", nil)
	cookiesRequest.AddCookie(cookie)
	// cookiesRecorder 保存管理员账号列表错误响应。
	cookiesRecorder := httptest.NewRecorder()
	handler.ServeHTTP(cookiesRecorder, cookiesRequest)
	if cookiesRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("管理员账号列表错误 status=%d body=%s", cookiesRecorder.Code, cookiesRecorder.Body.String())
	}
}

// accountSummaryHandlerCoveragePort 为管理员账号列表提供最小的摘要应用端口。
type accountSummaryHandlerCoveragePort struct {
	// listAdminErr 保存管理员账号摘要查询错误。
	listAdminErr error
}

// ListOwnedIDs 返回空的用户账号标识列表。
func (port accountSummaryHandlerCoveragePort) ListOwnedIDs(context.Context, int64) ([]string, error) {
	return nil, nil
}

// ListSummaries 返回空的用户账号摘要列表。
func (port accountSummaryHandlerCoveragePort) ListSummaries(context.Context, int64) ([]accountapp.AccountSummary, error) {
	return nil, nil
}

// GetOwnedSummary 返回空的账号摘要。
func (port accountSummaryHandlerCoveragePort) GetOwnedSummary(context.Context, int64, string) (accountapp.AccountSummary, error) {
	return accountapp.AccountSummary{}, nil
}

// ExistsOwned 返回账号不属于当前用户。
func (port accountSummaryHandlerCoveragePort) ExistsOwned(context.Context, int64, string) (bool, error) {
	return false, nil
}

// StatusOwned 返回账号停用状态。
func (port accountSummaryHandlerCoveragePort) StatusOwned(context.Context, int64, string) (bool, error) {
	return false, nil
}

// RequireOwnership 返回无错误的最小归属检查结果。
func (port accountSummaryHandlerCoveragePort) RequireOwnership(context.Context, int64, string) error {
	return nil
}

// ListAdminSummaries 返回预置的管理员账号摘要错误。
func (port accountSummaryHandlerCoveragePort) ListAdminSummaries(context.Context) ([]accountapp.AdminAccountSummary, error) {
	return nil, port.listAdminErr
}
