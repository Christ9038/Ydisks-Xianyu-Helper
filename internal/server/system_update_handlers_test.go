package server

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	updateapp "xianyu-go/internal/application/systemupdate"
)

// systemUpdatePortFake 为 HTTP 更新接口测试提供独立的成功与失败结果。
type systemUpdatePortFake struct {
	// statusSnapshot、checkSnapshot 和 applySnapshot 分别是三个操作的预置响应。
	statusSnapshot updateapp.Snapshot
	checkSnapshot  updateapp.Snapshot
	applySnapshot  updateapp.Snapshot
	// statusErr、checkErr 和 applyErr 分别是三个操作的预置错误。
	statusErr error
	checkErr  error
	applyErr  error
}

// Status 返回预置只读状态。
func (port systemUpdatePortFake) Status(context.Context) (updateapp.Snapshot, error) {
	return port.statusSnapshot, port.statusErr
}

// Check 返回预置正式版检查结果。
func (port systemUpdatePortFake) Check(context.Context) (updateapp.Snapshot, error) {
	return port.checkSnapshot, port.checkErr
}

// ApplyLatestStable 返回预置任务受理结果。
func (port systemUpdatePortFake) ApplyLatestStable(context.Context) (updateapp.Snapshot, error) {
	return port.applySnapshot, port.applyErr
}

// TestSystemUpdateHandlersEnforceAdminAndSuccessContracts 验证管理员权限以及 200/202 成功契约。
func TestSystemUpdateHandlersEnforceAdminAndSuccessContracts(t *testing.T) {
	// srv、store 和 cleanup 分别是独立测试服务、数据库与释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是包含管理员中间件的完整真实路由。
	handler := srv.Router()
	// adminCookie 是管理员真实登录后获得的会话。
	adminCookie := loginHelper(t, handler)

	// statusRequest、statusRecorder 分别是版本状态请求及其响应记录器。
	statusRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/update", nil)
	statusRequest.AddCookie(adminCookie)
	// statusRecorder 捕获版本状态响应。
	statusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(statusRecorder, statusRequest)
	assertOpenAPISuccessResponse(t, statusRequest, statusRecorder)

	// checkRequest、checkRecorder 分别是主动检查请求及其响应记录器。
	checkRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update/check", nil)
	checkRequest.AddCookie(adminCookie)
	// checkRecorder 捕获主动检查响应。
	checkRecorder := httptest.NewRecorder()
	handler.ServeHTTP(checkRecorder, checkRequest)
	assertOpenAPISuccessResponse(t, checkRequest, checkRecorder)

	// applyRequest、applyRecorder 分别是固定更新命令及其异步受理响应。
	applyRequest := httptest.NewRequest(http.MethodPost, "/api/v1/admin/system/update/apply", nil)
	applyRequest.AddCookie(adminCookie)
	// applyRecorder 捕获异步更新受理响应。
	applyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(applyRecorder, applyRequest)
	assertOpenAPIExpectedStatusResponse(t, applyRequest, applyRecorder, http.StatusAccepted)

	// createErr 是创建普通用户测试账号的数据库错误。
	if _, createErr := store.Users.Create(context.Background(), "update-user", "update-user@example.com", "pw"); createErr != nil {
		t.Fatalf("create non-admin user: %v", createErr)
	}
	// userCookie 是普通用户真实登录后获得的会话。
	userCookie := loginAsHelper(t, handler, "update-user", "pw")
	// forbiddenRequest、forbiddenRecorder 验证普通用户不能读取宿主机更新状态。
	forbiddenRequest := httptest.NewRequest(http.MethodGet, "/api/v1/admin/system/update", nil)
	forbiddenRequest.AddCookie(userCookie)
	// forbiddenRecorder 捕获普通用户越权响应。
	forbiddenRecorder := httptest.NewRecorder()
	handler.ServeHTTP(forbiddenRecorder, forbiddenRequest)
	if forbiddenRecorder.Code != http.StatusForbidden {
		t.Fatalf("non-admin status=%d body=%s", forbiddenRecorder.Code, forbiddenRecorder.Body.String())
	}
}

// TestSystemUpdateHandlersMapExecutorErrors 验证执行器连接错误和策略拒绝使用不同状态码。
func TestSystemUpdateHandlersMapExecutorErrors(t *testing.T) {
	// srv、cleanup 分别是错误映射测试服务及其释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是错误替身注入前已经挂载的完整路由。
	handler := srv.Router()
	// adminCookie 是管理员真实登录会话。
	adminCookie := loginHelper(t, handler)
	// cases 保存路径、操作错误和预期 HTTP 状态。
	cases := []struct {
		// name 是子测试名称。
		name string
		// path 是版本化管理员更新路径。
		path string
		// port 是当前请求使用的更新应用替身。
		port systemUpdatePortFake
		// want 是预期 HTTP 状态码。
		want int
	}{
		{name: "status unavailable", path: "/api/v1/admin/system/update", port: systemUpdatePortFake{statusErr: errors.New("status failed")}, want: http.StatusServiceUnavailable},
		{name: "check unavailable", path: "/api/v1/admin/system/update/check", port: systemUpdatePortFake{checkErr: errors.New("check failed")}, want: http.StatusServiceUnavailable},
		{name: "apply unavailable", path: "/api/v1/admin/system/update/apply", port: systemUpdatePortFake{applyErr: updateapp.ErrUpdaterUnavailable}, want: http.StatusServiceUnavailable},
		{name: "apply blocked", path: "/api/v1/admin/system/update/apply", port: systemUpdatePortFake{applyErr: updateapp.ErrUpdateBlocked}, want: http.StatusConflict},
		{name: "apply dependency failed", path: "/api/v1/admin/system/update/apply", port: systemUpdatePortFake{applyErr: errors.New("github unavailable")}, want: http.StatusServiceUnavailable},
	}
	for _, testCase := range cases { // testCase 是当前执行的错误映射样例。
		t.Run(testCase.name, func(t *testing.T) {
			srv.applications.systemUpdate = testCase.port
			// method 只有只读状态使用 GET，其余固定命令使用 POST。
			method := http.MethodPost
			if testCase.path == "/api/v1/admin/system/update" {
				method = http.MethodGet
			}
			// request、recorder 分别是当前错误样例请求和响应记录器。
			request := httptest.NewRequest(method, testCase.path, nil)
			request.AddCookie(adminCookie)
			// recorder 捕获当前错误映射响应。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			if recorder.Code != testCase.want {
				t.Fatalf("status=%d want=%d body=%s", recorder.Code, testCase.want, recorder.Body.String())
			}
		})
	}
}
