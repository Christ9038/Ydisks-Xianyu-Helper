package server

import (
	"context"
	"errors"
	"net/http"
	"testing"

	automationapp "xianyu-go/internal/application/automation"
)

// automationRuleHandlerCoveragePort 为自动化规则 handler 注入可控查询、规范化和写入结果。
type automationRuleHandlerCoveragePort struct {
	// listRules 保存非分页规则查询结果。
	listRules []automationapp.Rule
	// listErr 保存非分页规则查询错误。
	listErr error
	// pageRules 与 pageTotal 保存分页查询结果。
	pageRules []automationapp.Rule
	pageTotal int
	// pageErr 保存分页规则查询错误。
	pageErr error
	// countResult 保存触发器统计结果。
	countResult map[string]int
	// countErr 保存触发器统计错误。
	countErr error
	// normalizeResult 保存规则草稿规范化结果。
	normalizeResult automationapp.RuleInput
	// normalizeErr 保存规则草稿规范化错误。
	normalizeErr error
	// createID 与 createErr 保存规则创建结果。
	createID  int64
	createErr error
	// updateErr 保存规则更新错误。
	updateErr error
	// deleteErr 保存规则删除错误。
	deleteErr error
}

// ListForUser 返回测试配置的非分页规则列表。
func (port *automationRuleHandlerCoveragePort) ListForUser(context.Context, int64) ([]automationapp.Rule, error) {
	return port.listRules, port.listErr
}

// ListPageForUser 返回测试配置的分页规则列表。
func (port *automationRuleHandlerCoveragePort) ListPageForUser(context.Context, automationapp.RuleFilter) ([]automationapp.Rule, int, error) {
	return port.pageRules, port.pageTotal, port.pageErr
}

// CountByTriggerForUser 返回测试配置的触发器统计结果。
func (port *automationRuleHandlerCoveragePort) CountByTriggerForUser(context.Context, automationapp.RuleFilter) (map[string]int, error) {
	return port.countResult, port.countErr
}

// Normalize 返回测试配置的规则规范化结果。
func (port *automationRuleHandlerCoveragePort) Normalize(context.Context, int64, automationapp.RuleDraft) (automationapp.RuleInput, error) {
	return port.normalizeResult, port.normalizeErr
}

// Create 返回测试配置的规则创建结果。
func (port *automationRuleHandlerCoveragePort) Create(context.Context, automationapp.RuleInput) (int64, error) {
	return port.createID, port.createErr
}

// Update 返回测试配置的规则更新错误。
func (port *automationRuleHandlerCoveragePort) Update(context.Context, int64, int64, automationapp.RuleInput) error {
	return port.updateErr
}

// Delete 返回测试配置的规则删除错误。
func (port *automationRuleHandlerCoveragePort) Delete(context.Context, int64, int64) error {
	return port.deleteErr
}

// TestAutomationRuleHandlersCoverPaginationAndUpdateErrors 覆盖自动化规则分页、创建和更新的错误映射。
func TestAutomationRuleHandlersCoverPaginationAndUpdateErrors(t *testing.T) {
	// srv、cleanup 是基础测试服务及资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是当前测试注入的自动化规则应用端口。
	port := &automationRuleHandlerCoveragePort{
		listRules:   []automationapp.Rule{{ID: 1, CookieID: "acc1", Name: "规则", TriggerType: automationapp.TriggerOrderPaid}},
		pageRules:   []automationapp.Rule{{ID: 2, CookieID: "acc1", Name: "分页规则", TriggerType: automationapp.TriggerBuyerReviewed}},
		pageTotal:   2,
		countResult: map[string]int{automationapp.TriggerOrderPaid: 1},
		normalizeResult: automationapp.RuleInput{
			CookieID: "acc1", Name: "规范化规则", TriggerType: automationapp.TriggerOrderPaid,
		},
		createID: 9,
	}
	srv.applications.automationRules = port
	// handler 是注入可控自动化规则端口后的真实路由。
	handler := srv.Router()
	// cookie 是通过真实登录流程取得的管理员会话。
	cookie := loginHelper(t, handler)

	// listRecorder 保存非分页规则列表成功响应。
	listRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/automation-rules", "")
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	// listErrorCases 保存非分页规则列表错误场景。
	listErrorCases := []struct {
		name   string
		err    error
		status int
	}{
		{"list", errors.New("list failed"), http.StatusInternalServerError},
	}
	// listErrorCase 表示当前非分页列表错误场景。
	for _, listErrorCase := range listErrorCases {
		port.listErr = listErrorCase.err
		// recorder 保存当前列表错误响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/automation-rules", "")
		if recorder.Code != listErrorCase.status {
			t.Errorf("%s status=%d want=%d", listErrorCase.name, recorder.Code, listErrorCase.status)
		}
	}
	port.listErr = nil
	// pageRecorder 保存带过滤条件的分页规则成功响应。
	pageRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/automation-rules?page=0&page_size=200&cookie_id=acc1&trigger_type=order_paid&enabled=true&search=规则", "")
	if pageRecorder.Code != http.StatusOK {
		t.Fatalf("page status=%d body=%s", pageRecorder.Code, pageRecorder.Body.String())
	}
	port.pageTotal = 0
	// emptyPageRecorder 保存总数为零时的分页响应。
	emptyPageRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/automation-rules?page=2&page_size=10", "")
	if emptyPageRecorder.Code != http.StatusOK {
		t.Fatalf("empty page status=%d", emptyPageRecorder.Code)
	}
	port.pageTotal = 2
	port.pageErr = errors.New("page failed")
	// pageErrorRecorder 保存分页查询失败响应。
	pageErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/automation-rules?page=1", "")
	if pageErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("page error status=%d", pageErrorRecorder.Code)
	}
	port.pageErr = nil
	port.countErr = errors.New("count failed")
	// countErrorRecorder 保存触发器统计失败响应。
	countErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodGet, "/api/v1/automation-rules?page=1", "")
	if countErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("count error status=%d", countErrorRecorder.Code)
	}
	port.countErr = nil

	// createBody 是创建和更新测试使用的最小规则请求体。
	createBody := `{"cookie_id":"acc1","name":"规则","trigger_type":"order_paid","actions":[]}`
	// createRecorder 保存规则创建成功响应。
	createRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/automation-rules", createBody)
	if createRecorder.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", createRecorder.Code, createRecorder.Body.String())
	}
	port.normalizeErr = automationapp.ErrPricingModeConflict
	// normalizeConflictRecorder 保存创建规范化冲突响应。
	normalizeConflictRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/automation-rules", createBody)
	if normalizeConflictRecorder.Code != http.StatusConflict {
		t.Fatalf("normalize conflict status=%d", normalizeConflictRecorder.Code)
	}
	port.normalizeErr = errors.New("invalid rule")
	// normalizeErrorRecorder 保存创建规范化参数错误响应。
	normalizeErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/automation-rules", createBody)
	if normalizeErrorRecorder.Code != http.StatusBadRequest {
		t.Fatalf("normalize error status=%d", normalizeErrorRecorder.Code)
	}
	port.normalizeErr = nil
	port.createErr = automationapp.ErrPricingModeConflict
	// createConflictRecorder 保存规则创建冲突响应。
	createConflictRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/automation-rules", createBody)
	if createConflictRecorder.Code != http.StatusConflict {
		t.Fatalf("create conflict status=%d", createConflictRecorder.Code)
	}
	port.createErr = errors.New("create failed")
	// createErrorRecorder 保存规则创建内部错误响应。
	createErrorRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPost, "/api/v1/automation-rules", createBody)
	if createErrorRecorder.Code != http.StatusInternalServerError {
		t.Fatalf("create error status=%d", createErrorRecorder.Code)
	}
	port.createErr = nil
	// updateRecorder 保存规则更新成功响应。
	updateRecorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/automation-rules/9", createBody)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	// updateErrorCases 保存规则更新错误及状态码。
	updateErrorCases := []struct {
		name   string
		err    error
		status int
	}{
		{"pricing conflict", automationapp.ErrPricingModeConflict, http.StatusConflict},
		{"not found", automationapp.ErrRuleNotFound, http.StatusNotFound},
		{"internal", errors.New("update failed"), http.StatusInternalServerError},
	}
	// updateErrorCase 表示当前规则更新错误场景。
	for _, updateErrorCase := range updateErrorCases {
		port.updateErr = updateErrorCase.err
		// recorder 保存当前规则更新错误响应。
		recorder := serveChatCoverageRequest(handler, cookie, http.MethodPut, "/api/v1/automation-rules/9", createBody)
		if recorder.Code != updateErrorCase.status {
			t.Errorf("%s status=%d want=%d", updateErrorCase.name, recorder.Code, updateErrorCase.status)
		}
	}
	port.updateErr = nil
	// validationCases 保存自动化规则请求格式和路径校验场景。
	validationCases := []struct {
		method string
		path   string
		body   string
	}{
		{http.MethodPost, "/api/v1/automation-rules", "{"},
		{http.MethodPut, "/api/v1/automation-rules/nope", createBody},
	}
	// validationCase 表示当前自动化规则校验场景。
	for _, validationCase := range validationCases {
		// recorder 保存当前自动化规则校验响应。
		recorder := serveChatCoverageRequest(handler, cookie, validationCase.method, validationCase.path, validationCase.body)
		if recorder.Code != http.StatusBadRequest {
			t.Errorf("%s %s status=%d", validationCase.method, validationCase.path, recorder.Code)
		}
	}
}
