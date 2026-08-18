package server

import (
	"errors"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	automationapp "xianyu-go/internal/application/automation"
)

// mountAccountTasks 封装mount账号任务列表业务协调。
func (s *Server) mountAccountTasks(r chi.Router) {
	r.Get("/api/account-tasks/{cid}", s.getAccountTaskSettings)
	r.Put("/api/account-tasks/{cid}", s.updateAccountTaskSettings)
	r.Get("/api/account-tasks/{cid}/runs", s.listAccountTaskRuns)
	r.Post("/api/account-tasks/{cid}/run", s.runAccountTask)
}

// getAccountTaskSettings 封装get账号任务设置业务协调。
func (s *Server) getAccountTaskSettings(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	// settings、err 用于本次流程后续判断的settings、err
	settings, err := s.accountTaskApplication().GetSettings(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取账号任务配置失败")
		return
	}
	writeJSON(w, http.StatusOK, newApplicationAccountTaskSettingsResponse(settings))
}

// updateAccountTaskSettings 封装update账号任务设置业务协调。
func (s *Server) updateAccountTaskSettings(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	// input 用于本次流程后续判断的input
	var input automationapp.AccountTaskSettings
	if decodeJSON(r, &input) != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// stored、err 用于本次流程后续判断的stored、err
	input.CookieID = cid
	// stored、err 保存应用服务规范化后的设置及写入错误。
	stored, err := s.accountTaskApplication().UpdateSettings(r.Context(), input)
	if err != nil {
		if strings.Contains(err.Error(), "不能为空") || strings.Contains(err.Error(), "不能超过") || strings.Contains(err.Error(), "格式必须") {
			writeErr(w, http.StatusBadRequest, err.Error())
		} else {
			writeErr(w, http.StatusInternalServerError, "保存账号任务配置失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, newApplicationAccountTaskSettingsResponse(stored))
}

// listAccountTaskRuns 封装list账号任务运行记录业务协调。
func (s *Server) listAccountTaskRuns(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	// runs、err 用于本次流程后续判断的runs、err
	runs, err := s.accountTaskApplication().ListRuns(r.Context(), cid, parsePositiveInt(r.URL.Query().Get("limit"), 20))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取任务记录失败")
		return
	}
	writeJSON(w, http.StatusOK, accountTaskRunsResponse{Runs: newApplicationAccountTaskRunResponses(runs)})
}

// runAccountTask 封装运行账号任务业务协调。
func (s *Server) runAccountTask(w http.ResponseWriter, r *http.Request) {
	// cid 用于本次流程后续判断的cid
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	// input 用于本次流程后续判断的input
	var input accountTaskRunRequest
	if decodeJSON(r, &input) != nil || (input.TaskType != automationapp.TaskAutoRate && input.TaskType != automationapp.TaskAutoPolish) {
		writeErr(w, http.StatusBadRequest, "不支持的任务类型")
		return
	}
	// summary、err 用于本次流程后续判断的summary、err
	summary, err := s.accountTaskApplication().Run(r.Context(), cid, input.TaskType)
	if err != nil {
		if errors.Is(err, automationapp.ErrUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "自动化中心未启用")
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accountTaskRunResponseEnvelope{Success: true, Summary: newApplicationAccountTaskSummaryResponse(summary)})
}

// accountTaskRunRequest 是手动执行账号任务的具名 HTTP 请求 DTO。
type accountTaskRunRequest struct {
	// TaskType 是要求执行的账号任务类型。
	TaskType string `json:"task_type"`
}
