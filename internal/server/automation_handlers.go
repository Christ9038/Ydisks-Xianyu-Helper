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

// mountAutomation 负责mount自动化相关处理。
func (s *Server) mountAutomation(r chi.Router) {
	r.Get("/automation-rules", s.listAutomationRules)
	r.Post("/automation-rules", s.createAutomationRule)
	r.Put("/automation-rules/{rule_id}", s.updateAutomationRule)
	r.Delete("/automation-rules/{rule_id}", s.deleteAutomationRule)
	r.Get("/automation-issues", s.listAutomationIssues)
	r.Post("/automation-runs/{run_id}/resolve", s.resolveAutomationRun)
	r.Post("/automation-pending-tasks/{task_id}/resolve", s.resolveDeferredAutomationTask)
}

// listAutomationIssues 负责list自动化问题列表相关处理。
func (s *Server) listAutomationIssues(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// runs、tasks、err 保存runs、tasks、err，供当前处理流程使用
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

// resolveAutomationRun 负责resolve自动化运行相关处理。
func (s *Server) resolveAutomationRun(w http.ResponseWriter, r *http.Request) {
	// runID、err 保存运行ID、err，供当前处理流程使用
	runID, err := strconv.ParseInt(chi.URLParam(r, "run_id"), 10, 64)
	if err != nil || runID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效运行ID")
		return
	}
	// req 保存req，供当前处理流程使用
	var req struct {
		Resolution string `json:"resolution"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if // err 保存err，供当前处理流程使用
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

// resolveDeferredAutomationTask 负责resolveDeferred自动化任务相关处理。
func (s *Server) resolveDeferredAutomationTask(w http.ResponseWriter, r *http.Request) {
	// taskID、err 保存任务ID、err，供当前处理流程使用
	taskID, err := strconv.ParseInt(chi.URLParam(r, "task_id"), 10, 64)
	if err != nil || taskID <= 0 {
		writeErr(w, http.StatusBadRequest, "无效任务ID")
		return
	}
	// req 保存req，供当前处理流程使用
	var req struct {
		Resolution string `json:"resolution"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "处理方式必须是 retry 或 dismiss")
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if // err 保存err，供当前处理流程使用
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
