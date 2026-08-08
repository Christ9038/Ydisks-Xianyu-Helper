package server

import (
	"net/http"
	"regexp"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
)

var accountTaskTimePattern = regexp.MustCompile(`^(?:[01]\d|2[0-3]):[0-5]\d$`)

func (s *Server) mountAccountTasks(r chi.Router) {
	r.Get("/api/account-tasks/{cid}", s.getAccountTaskSettings)
	r.Put("/api/account-tasks/{cid}", s.updateAccountTaskSettings)
	r.Get("/api/account-tasks/{cid}/runs", s.listAccountTaskRuns)
	r.Post("/api/account-tasks/{cid}/run", s.runAccountTask)
}

func (s *Server) getAccountTaskSettings(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	settings, err := s.Store.AccountTasks.Get(r.Context(), cid)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取账号任务配置失败")
		return
	}
	writeJSON(w, http.StatusOK, settings)
}

func (s *Server) updateAccountTaskSettings(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	var input db.AccountTaskSettings
	if decodeJSON(r, &input) != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	input.CookieID = cid
	input.RateContent = strings.TrimSpace(input.RateContent)
	if input.AutoRateEnabled && input.RateContent == "" {
		writeErr(w, http.StatusBadRequest, "启用自动评价时评价内容不能为空")
		return
	}
	if len([]rune(input.RateContent)) > 500 {
		writeErr(w, http.StatusBadRequest, "评价内容不能超过 500 个字符")
		return
	}
	if !accountTaskTimePattern.MatchString(input.PolishTime) {
		writeErr(w, http.StatusBadRequest, "擦亮时间格式必须为 HH:mm")
		return
	}
	if err := s.Store.AccountTasks.Upsert(r.Context(), input); err != nil {
		writeErr(w, http.StatusInternalServerError, "保存账号任务配置失败")
		return
	}
	stored, _ := s.Store.AccountTasks.Get(r.Context(), cid)
	writeJSON(w, http.StatusOK, stored)
}

func (s *Server) listAccountTaskRuns(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权访问该账号")
		return
	}
	runs, err := s.Store.AccountTasks.RecentRuns(r.Context(), cid, parsePositiveInt(r.URL.Query().Get("limit"), 20))
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取任务记录失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"runs": runs})
}

func (s *Server) runAccountTask(w http.ResponseWriter, r *http.Request) {
	cid := chi.URLParam(r, "cid")
	if !s.ownsAccount(r, cid) {
		writeErr(w, http.StatusForbidden, "无权操作该账号")
		return
	}
	if s.automation == nil {
		writeErr(w, http.StatusServiceUnavailable, "自动化中心未启用")
		return
	}
	var input struct {
		TaskType string `json:"task_type"`
	}
	if decodeJSON(r, &input) != nil || (input.TaskType != automation.TaskAutoRate && input.TaskType != automation.TaskAutoPolish) {
		writeErr(w, http.StatusBadRequest, "不支持的任务类型")
		return
	}
	summary, err := s.automation.RunAccountTask(r.Context(), cid, input.TaskType)
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"summary": summary, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "summary": summary})
}
