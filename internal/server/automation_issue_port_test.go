package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestAutomationIssuePortHTTPErrorBranches 验证 issue 应用 Port 接入后的 SQLite HTTP 错误分支。
func TestAutomationIssuePortHTTPErrorBranches(t *testing.T) {
	// srv、cleanup 保存真实 SQLite 测试服务及清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 保存挂载认证和自动化 issue 路由的 HTTP 处理器。
	handler := srv.Router()
	// sessionCookie 保存测试管理员的认证会话。
	sessionCookie := loginHelper(t, handler)

	// invalidResolutionRequest 保存不支持处理动作的请求。
	invalidResolutionRequest := httptest.NewRequest(http.MethodPost, "/automation-pending-tasks/1/resolve", strings.NewReader(`{"resolution":"continue"}`))
	invalidResolutionRequest.AddCookie(sessionCookie)
	// invalidResolutionResponse 捕获非法处理动作的响应。
	invalidResolutionResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResolutionResponse, invalidResolutionRequest)
	if invalidResolutionResponse.Code != http.StatusBadRequest || !strings.Contains(invalidResolutionResponse.Body.String(), "retry") {
		t.Fatalf("非法延迟任务处理动作响应异常: status=%d body=%s", invalidResolutionResponse.Code, invalidResolutionResponse.Body.String())
	}

	// missingRunRequest 保存不存在自动化运行的处理请求。
	missingRunRequest := httptest.NewRequest(http.MethodPost, "/automation-runs/999999/resolve", strings.NewReader(`{"resolution":"cancel"}`))
	missingRunRequest.AddCookie(sessionCookie)
	// missingRunResponse 捕获不存在运行的响应。
	missingRunResponse := httptest.NewRecorder()
	handler.ServeHTTP(missingRunResponse, missingRunRequest)
	if missingRunResponse.Code != http.StatusNotFound {
		t.Fatalf("不存在自动化运行响应异常: status=%d body=%s", missingRunResponse.Code, missingRunResponse.Body.String())
	}

	// listRequest 保存 SQLite issue 列表查询请求。
	listRequest := httptest.NewRequest(http.MethodGet, "/automation-issues", nil)
	listRequest.AddCookie(sessionCookie)
	// listResponse 捕获 SQLite issue 列表响应。
	listResponse := httptest.NewRecorder()
	handler.ServeHTTP(listResponse, listRequest)
	if listResponse.Code != http.StatusOK || !strings.Contains(listResponse.Body.String(), `"runs"`) || !strings.Contains(listResponse.Body.String(), `"pending_tasks"`) {
		t.Fatalf("自动化 issue 列表响应异常: status=%d body=%s", listResponse.Code, listResponse.Body.String())
	}
}
