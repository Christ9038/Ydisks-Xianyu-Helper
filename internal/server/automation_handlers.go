package server

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	automationapp "xianyu-go/internal/application/automation"
	"xianyu-go/internal/auth"
)

// automationIssueResolutionRequest 是处理自动化异常或延期任务的 HTTP 请求 DTO。
type automationIssueResolutionRequest struct {
	// Resolution 是 retry、dismiss 等由应用服务校验的处理动作。
	Resolution string `json:"resolution"`
}

// mountAutomation 封装mount自动化业务协调。
func (s *Server) mountAutomation(r chi.Router) {
	r.Get("/automation-rules", s.listAutomationRules)
	r.Post("/automation-rules", s.createAutomationRule)
	r.Put("/automation-rules/{rule_id}", s.updateAutomationRule)
	r.Delete("/automation-rules/{rule_id}", s.deleteAutomationRule)
	r.Get("/delivery-templates", s.listDeliveryTemplates)
	r.Post("/delivery-templates", s.createDeliveryTemplate)
	r.Get("/delivery-templates/{template_id}", s.getDeliveryTemplate)
	r.Put("/delivery-templates/{template_id}", s.updateDeliveryTemplate)
	r.Delete("/delivery-templates/{template_id}", s.deleteDeliveryTemplate)
	r.Get("/automation-issues", s.listAutomationIssues)
	r.Post("/automation-runs/{run_id}/resolve", s.resolveAutomationRun)
	r.Post("/automation-pending-tasks/{task_id}/resolve", s.resolveDeferredAutomationTask)
}

// listAutomationIssues 封装list自动化问题列表业务协调。
func (s *Server) listAutomationIssues(w http.ResponseWriter, r *http.Request) {
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// runs、tasks、err 用于本次流程后续判断的runs、tasks、err
	runs, tasks, err := s.automationIssuesApplication().ListIssues(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询自动化异常任务失败")
		return
	}
	writeJSON(w, http.StatusOK, automationIssuesResponse{
		Runs:         automationRunIssueDTOs(runs),
		PendingTasks: deferredAutomationIssueDTOs(tasks),
	})
}

// automationRunIssueDTOs 将应用层自动化运行摘要转换为 HTTP DTO，避免响应直接暴露应用内部模型。
func automationRunIssueDTOs(issues []automationapp.RunIssue) []automationRunIssueDTO {
	// result 是待写入响应的自动化运行 DTO 列表。
	result := make([]automationRunIssueDTO, 0, len(issues))
	// issue 是当前待转换的应用层运行异常摘要。
	for _, issue := range issues {
		result = append(result, automationRunIssueDTO{
			ID: issue.ID, CookieID: issue.CookieID, OrderID: issue.OrderID,
			TriggerType: issue.TriggerType, ErrorMessage: issue.ErrorMessage,
			IssueKind: issue.IssueKind, AllowedResolutions: issue.AllowedResolutions,
			ActionCursor: issue.ActionCursor, SentCount: issue.SentCount, UpdatedAt: issue.UpdatedAt,
		})
	}
	return result
}

// deferredAutomationIssueDTOs 将应用层延期任务摘要转换为 HTTP DTO。
func deferredAutomationIssueDTOs(issues []automationapp.DeferredIssue) []deferredAutomationIssueDTO {
	// result 是待写入响应的延期任务 DTO 列表。
	result := make([]deferredAutomationIssueDTO, 0, len(issues))
	// issue 是当前待转换的应用层延期异常摘要。
	for _, issue := range issues {
		result = append(result, deferredAutomationIssueDTO{
			ID: issue.ID, CookieID: issue.CookieID, TriggerType: issue.TriggerType,
			ErrorMessage: issue.ErrorMessage, AttemptCount: issue.AttemptCount, UpdatedAt: issue.UpdatedAt,
		})
	}
	return result
}

// resolveAutomationRun 封装resolve自动化运行业务协调。
func (s *Server) resolveAutomationRun(w http.ResponseWriter, r *http.Request) {
	// runID、err 用于本次流程后续判断的运行ID、err
	runID, err := strconv.ParseInt(chi.URLParam(r, "run_id"), 10, 64)
	if err != nil || runID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效运行ID")
		return
	}
	// req 是自动化运行异常处理请求的具名传输 DTO。
	var req automationIssueResolutionRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if // err 用于本次流程后续判断的err
	err := s.automationIssuesApplication().ResolveRunIssue(r.Context(), sess.UserID, runID, strings.TrimSpace(req.Resolution)); err != nil {
		if errors.Is(err, automationapp.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "异常运行不存在或已处理")
			return
		}
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// resolveDeferredAutomationTask 封装resolveDeferred自动化任务业务协调。
func (s *Server) resolveDeferredAutomationTask(w http.ResponseWriter, r *http.Request) {
	// taskID、err 用于本次流程后续判断的任务ID、err
	taskID, err := strconv.ParseInt(chi.URLParam(r, "task_id"), 10, 64)
	if err != nil || taskID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效任务ID")
		return
	}
	// req 是延期自动化任务处理请求的具名传输 DTO。
	var req automationIssueResolutionRequest
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "处理方式必须是 retry 或 dismiss")
		return
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if // err 用于本次流程后续判断的err
	err := s.automationIssuesApplication().ResolveDeferredIssue(r.Context(), sess.UserID, taskID, req.Resolution); err != nil {
		if errors.Is(err, automationapp.ErrInvalidDeferredResolution) {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		writeErr(w, http.StatusNotFound, "异常任务不存在或已处理")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}
