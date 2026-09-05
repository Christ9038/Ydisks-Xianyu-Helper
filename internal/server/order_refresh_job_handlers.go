package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"mime"
	"net/http"
	"strings"

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

// orderRefreshRequest 是订单刷新筛选条件的 HTTP 请求 DTO；JSON 为新版首选，multipart 保留给旧客户端。
type orderRefreshRequest struct {
	// CookieID 是可选的账号筛选标识，空值表示刷新当前用户的全部账号。
	CookieID string `json:"cookie_id"`
	// Status 是可选的平台订单状态筛选值，空值表示不按状态过滤。
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
	// request、parseErr 保存已完成媒体类型校验的筛选 DTO 及格式错误。
	request, parseErr := parseOrderRefreshRequest(w, r)
	if parseErr != nil {
		writeErr(w, http.StatusBadRequest, parseErr.Error())
		return
	}
	// started、err 保存应用服务创建并启动任务的结果。
	started, err := s.orderRefreshJobsApplication().CreateAndStart(r.Context(), sess.UserID, request.CookieID, request.Status)
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

// parseOrderRefreshRequest 将 JSON、multipart 和历史 urlencoded 输入转换为同一个具名 DTO；解析失败绝不回退为空筛选。
func parseOrderRefreshRequest(w http.ResponseWriter, r *http.Request) (orderRefreshRequest, error) {
	// contentType 保存原始媒体类型头；空头需要先被识别为历史无筛选调用，mime.ParseMediaType 不接受空字符串。
	contentType := r.Header.Get("Content-Type")
	if contentType == "" {
		// 空请求体一直表示“刷新全部账号且不按状态筛选”；仅允许真正无内容的历史调用，避免损坏载荷静默退化。
		if r.ContentLength > 0 {
			return orderRefreshRequest{}, fmt.Errorf("请求格式错误，请使用 JSON 或 multipart/form-data")
		}
		return orderRefreshRequest{}, nil
	}
	// mediaType、contentTypeErr 保存请求媒体类型及其语法错误。
	mediaType, _, contentTypeErr := mime.ParseMediaType(contentType)
	if contentTypeErr != nil {
		return orderRefreshRequest{}, fmt.Errorf("请求格式错误，请使用 JSON 或 multipart/form-data")
	}
	// request 保存归一化后的订单刷新筛选条件。
	var request orderRefreshRequest
	switch mediaType {
	case "application/json":
		// Body 仅用于本次 JSON DTO 解码，限制大小后拒绝无效或截断载荷。
		r.Body = http.MaxBytesReader(w, r.Body, maxOrderRefreshRequestBytes)
		// decodeErr 保存 JSON DTO 解码错误；未知字段必须失败，避免前端拼写错误静默失效。
		decoder := json.NewDecoder(r.Body)
		decoder.DisallowUnknownFields()
		// decodeErr 保存当前 JSON 请求的实际解码错误，失败后绝不创建无筛选刷新任务。
		if decodeErr := decoder.Decode(&request); decodeErr != nil {
			return orderRefreshRequest{}, fmt.Errorf("订单刷新请求 JSON 格式错误")
		}
	case "multipart/form-data":
		// Body 由当前兼容解析分支独占，解析失败必须返回错误而不是让 FormValue 静默变为空字符串。
		r.Body = http.MaxBytesReader(w, r.Body, maxOrderRefreshRequestBytes)
		// parseErr 保存 multipart boundary、流截断或总大小错误。
		if parseErr := r.ParseMultipartForm(maxOrderRefreshRequestBytes); parseErr != nil {
			// maxBytesErr 表示请求超过小型筛选 DTO 的总大小配额。
			var maxBytesErr *http.MaxBytesError
			if errors.As(parseErr, &maxBytesErr) {
				return orderRefreshRequest{}, fmt.Errorf("订单刷新请求不能超过 1 MiB")
			}
			return orderRefreshRequest{}, fmt.Errorf("订单刷新上传表单损坏，请检查后重试")
		}
		request.CookieID = r.FormValue("cookie_id")
		request.Status = r.FormValue("status")
	case "application/x-www-form-urlencoded":
		// 历史非版本化客户端曾依赖 FormValue；继续接受，但显式检查 ParseForm 错误。
		r.Body = http.MaxBytesReader(w, r.Body, maxOrderRefreshRequestBytes)
		// formErr 保存历史表单字段解析错误，禁止让非 multipart 损坏载荷静默变为空筛选。
		if formErr := r.ParseForm(); formErr != nil {
			return orderRefreshRequest{}, fmt.Errorf("订单刷新请求格式错误")
		}
		request.CookieID = r.FormValue("cookie_id")
		request.Status = r.FormValue("status")
	default:
		return orderRefreshRequest{}, fmt.Errorf("请求格式错误，请使用 JSON 或 multipart/form-data")
	}
	// CookieID、Status 去除展示层空白，确保空筛选由调用方明确表达而非解析失败产生。
	request.CookieID = strings.TrimSpace(request.CookieID)
	request.Status = strings.TrimSpace(request.Status)
	return request, nil
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

// orderRefreshResponseFromJobResult 将 result 持久化任务结果映射为历史 HTTP DTO；
// 恢复修正计数与失败明细原样传递，旧任务缺失计数时保留零值兼容。
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
			Restored: result.Summary.Restored, Reassigned: result.Summary.Reassigned,
			Discovered: result.Summary.Discovered, ListUpdated: result.Summary.ListUpdated,
			SoftDeleted: result.Summary.SoftDeleted, DetailTotal: result.Summary.DetailTotal,
			Total: result.Summary.Total, Updated: result.Summary.Updated,
			NoChange: result.Summary.NoChange, Failed: result.Summary.Failed,
		},
		Results: results,
	}
}
