package server

import (
	"errors"
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
)

// accountTaskTimePattern 保存账号任务时间Pattern，供当前处理流程使用
var accountTaskTimePattern = regexp.MustCompile(`^(?:[01]\d|2[0-3]):[0-5]\d$`)

// mountAccountTasks 负责mount账号任务列表相关处理。
func (s *Server) mountAccountTasks(r chi.Router) {
	r.Get("/api/account-tasks/{cid}", s.getAccountTaskSettings)
	r.Put("/api/account-tasks/{cid}", s.updateAccountTaskSettings)
	r.Get("/api/account-tasks/{cid}/runs", s.listAccountTaskRuns)
	r.Post("/api/account-tasks/{cid}/run", s.runAccountTask)
}

// getAccountTaskSettings 负责get账号任务设置相关处理。
func (s *Server) getAccountTaskSettings(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	// settings、err 保存settings、err，供当前处理流程使用
	settings, err := s.communicationApplication().GetAccountTaskSettings(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取账号任务配置失败")
		return
	}
	writeJSON(w, http.StatusOK, newAccountTaskSettingsResponse(settings))
}

// updateAccountTaskSettings 负责update账号任务设置相关处理。
func (s *Server) updateAccountTaskSettings(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	// input 保存input，供当前处理流程使用
	var input db.AccountTaskSettings
	if decodeJSON(r, &input) != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// stored、err 保存stored、err，供当前处理流程使用
	stored, err := s.communicationApplication().UpdateAccountTaskSettings(r.Context(), accountTaskUpdateInput{CookieID: cid, Settings: input})
	if err != nil {
		if strings.Contains(err.Error(), "不能为空") || strings.Contains(err.Error(), "不能超过") || strings.Contains(err.Error(), "格式必须") {
			writeErr(w, http.StatusBadRequest, err.Error())
		} else {
			writeErr(w, http.StatusInternalServerError, "保存账号任务配置失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, newAccountTaskSettingsResponse(stored))
}

// listAccountTaskRuns 负责list账号任务运行记录相关处理。
func (s *Server) listAccountTaskRuns(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	// runs、err 保存runs、err，供当前处理流程使用
	runs, err := s.communicationApplication().ListAccountTaskRuns(r.Context(), cid, parsePositiveInt(r.URL.Query().Get("limit"), 20))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取任务记录失败")
		return
	}
	writeJSON(w, http.StatusOK, accountTaskRunsResponse{Runs: newAccountTaskRunResponses(runs)})
}

// runAccountTask 负责运行账号任务相关处理。
func (s *Server) runAccountTask(w http.ResponseWriter, r *http.Request) {
	// cid 保存cid，供当前处理流程使用
	cid := chi.URLParam(r, "cid")
	if s.automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "自动化中心未启用")
		return
	}
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	// input 保存input，供当前处理流程使用
	var input struct {
		TaskType string `json:"task_type"`
	}
	if decodeJSON(r, &input) != nil || (input.TaskType != automation.TaskAutoRate && input.TaskType != automation.TaskAutoPolish) {
		writeErr(w, http.StatusBadRequest, "不支持的任务类型")
		return
	}
	// summary、err 保存summary、err，供当前处理流程使用
	summary, err := s.communicationApplication().RunAccountTask(r.Context(), cid, input.TaskType)
	if err != nil {
		if errors.Is(err, errCommunicationUnavailable) {
			writeErr(w, http.StatusServiceUnavailable, "自动化中心未启用")
			return
		}
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, accountTaskRunResponseEnvelope{Success: true, Summary: newAccountTaskSummaryResponse(summary)})
}
