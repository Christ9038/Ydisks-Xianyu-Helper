package server

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/go-chi/chi/v5"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/auth"
)

// orderRefreshJobStartResponse 是创建订单刷新任务后的响应 DTO。
type orderRefreshJobStartResponse struct {
	// Success 表示任务是否成功创建。
	Success bool `json:"success"`
	// JobID 是后台任务标识。
	JobID string `json:"job_id"`
	// Status 是任务初始状态。
	Status string `json:"status"`
}

// orderRefreshJobStatusResponse 是订单刷新任务查询响应 DTO。
type orderRefreshJobStatusResponse struct {
	// Success 表示查询是否成功。
	Success bool `json:"success"`
	// JobID 是任务标识。
	JobID string `json:"job_id"`
	// Status 是任务状态。
	Status string `json:"status"`
	// ErrorMessage 是任务失败原因。
	ErrorMessage string `json:"error_message,omitempty"`
	// Result 是任务成功后的订单刷新结果。
	Result *orderRefreshResponse `json:"result,omitempty"`
}

// orderRefreshJobCancelResponse 是取消订单刷新任务后的响应 DTO。
type orderRefreshJobCancelResponse struct {
	// Success 表示取消命令是否成功应用。
	Success bool `json:"success"`
	// JobID 是被取消的任务标识。
	JobID string `json:"job_id"`
	// Status 是取消后的任务状态。
	Status string `json:"status"`
}

// mountOrderRefreshJobRoutes 挂载订单刷新后台任务端点。
func (s *Server) mountOrderRefreshJobRoutes(r chi.Router, prefix string) {
	r.Post(prefix+"/orders/refresh", s.startOrderRefreshJob)
	r.Get(prefix+"/orders/refresh/{job_id}", s.getOrderRefreshJob)
	r.Delete(prefix+"/orders/refresh/{job_id}", s.cancelOrderRefreshJob)
}

// startOrderRefreshJob 解析筛选条件并调用应用服务创建订单刷新任务。
func (s *Server) startOrderRefreshJob(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证会话。
	sess := auth.SessionFromContext(r.Context())
	// cookieID、filterStatus 保存订单刷新筛选条件。
	cookieID, filterStatus := r.FormValue("cookie_id"), r.FormValue("status")
	// started、err 保存应用服务创建并启动任务的结果。
	started, err := s.orderRefreshJobsApplication().CreateAndStart(r.Context(), sess.UserID, cookieID, filterStatus)
	if errors.Is(err, orderapp.ErrForbidden) {
		writeErr(w, http.StatusForbidden, "Cookie不存在或无权访问")
		return
	}
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error("创建订单刷新任务失败", "err", err)
		}
		writeErr(w, http.StatusInternalServerError, "创建订单刷新任务失败")
		return
	}
	writeJSON(w, http.StatusAccepted, orderRefreshJobStartResponse{Success: true, JobID: started.Job.ID, Status: "running"})
}

// getOrderRefreshJob 返回当前用户拥有的订单刷新任务状态和结果。
func (s *Server) getOrderRefreshJob(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证会话。
	sess := auth.SessionFromContext(r.Context())
	// jobID 保存路径中的订单刷新任务标识。
	jobID := chi.URLParam(r, "job_id")
	// job、err 保存应用服务读取结果及错误。
	job, err := s.orderRefreshJobsApplication().GetJob(r.Context(), sess.UserID, jobID)
	if errors.Is(err, orderapp.ErrRefreshJobNotFound) {
		writeErr(w, http.StatusNotFound, "订单刷新任务不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "读取订单刷新任务失败")
		return
	}
	// response 保存任务状态响应 DTO。
	response := orderRefreshJobStatusResponse{Success: true, JobID: job.ID, Status: job.Status, ErrorMessage: job.ErrorMessage}
	if job.ResultJSON != "" && job.ResultJSON != "{}" {
		// result 保存应用层稳定结果模型，避免 HTTP 层承担持久化结果形状。
		var result orderapp.RefreshJobResult
		// err 表示任务结果 JSON 解析错误。
		if err := json.Unmarshal([]byte(job.ResultJSON), &result); err != nil {
			if s.Logger != nil {
				s.Logger.Error("解析订单刷新任务结果失败", "job_id", job.ID, "err", err)
			}
			writeErr(w, http.StatusInternalServerError, "读取订单刷新结果失败")
			return
		}
		// mapped 保存转换后的 HTTP 兼容结果 DTO。
		mapped := orderRefreshResponseFromJobResult(result)
		response.Result = &mapped
	}
	writeJSON(w, http.StatusOK, response)
}

// cancelOrderRefreshJob 按当前用户归属取消任务，并由应用服务通知运行中的 worker。
func (s *Server) cancelOrderRefreshJob(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证会话。
	sess := auth.SessionFromContext(r.Context())
	// jobID 保存路径中的订单刷新任务标识。
	jobID := chi.URLParam(r, "job_id")
	// result、err 保存应用服务取消结果及错误。
	result, err := s.orderRefreshJobsApplication().CancelForUser(r.Context(), sess.UserID, jobID)
	if errors.Is(err, orderapp.ErrRefreshJobNotFound) {
		writeErr(w, http.StatusNotFound, "订单刷新任务不存在")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "取消订单刷新任务失败")
		return
	}
	if result.Cancelled || (result.Job != nil && result.Job.Status == "cancelled") {
		writeJSON(w, http.StatusOK, orderRefreshJobCancelResponse{Success: true, JobID: jobID, Status: "cancelled"})
		return
	}
	writeErr(w, http.StatusConflict, "订单刷新任务已结束，无法取消")
}

// orderRefreshResponseFromJobResult 将应用层任务结果映射为历史 HTTP 响应 DTO。
func orderRefreshResponseFromJobResult(result orderapp.RefreshJobResult) orderRefreshResponse {
	// results 保存转换后的 HTTP 结果行。
	results := make([]orderRefreshResultDTO, 0, len(result.Results))
	// item 表示当前应用层任务结果行。
	for _, item := range result.Results {
		results = append(results, orderRefreshResultDTO{
			Success: item.Success, CookieID: item.CookieID, Discovered: item.Discovered,
			Updated: item.Updated, SoftDeleted: item.SoftDeleted, OrderID: item.OrderID,
			Stage: item.Stage, Message: item.Message, Error: item.Error,
			OldStatus: item.OldStatus, NewStatus: item.NewStatus,
		})
	}
	return orderRefreshResponse{
		PartialFailure: result.PartialFailure,
		Message:        result.Message,
		Summary: orderRefreshSummary{
			Discovered: result.Summary.Discovered, ListUpdated: result.Summary.ListUpdated,
			SoftDeleted: result.Summary.SoftDeleted, DetailTotal: result.Summary.DetailTotal,
			Total: result.Summary.Total, Updated: result.Summary.Updated,
			NoChange: result.Summary.NoChange, Failed: result.Summary.Failed,
		},
		Results: results,
	}
}
