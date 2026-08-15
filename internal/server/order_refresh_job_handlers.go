package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	orderapp "xianyu-go/internal/application/orders"

	"xianyu-go/internal/auth"
)

// orderRefreshJobTimeout 限制单个订单刷新后台任务的最长执行时间。
const orderRefreshJobTimeout = 30 * time.Minute

// orderRefreshJobLease 为运行中的订单刷新任务提供可恢复租约。
const orderRefreshJobLease = orderRefreshJobTimeout + time.Minute

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
	// JobID 是后台任务标识。
	JobID string `json:"job_id"`
	// Status 是 queued/running/succeeded/failed 任务状态。
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

// orderRefreshWorker 保存可被用户取消的订单刷新 worker 控制句柄。
type orderRefreshWorker struct {
	// token 是当前 worker 的租约令牌。
	token string
	// cancel 是取消当前 worker Context 的函数。
	cancel context.CancelFunc
}

// mountOrderRefreshJobRoutes 挂载订单刷新后台任务端点。
func (s *Server) mountOrderRefreshJobRoutes(r chi.Router, prefix string) {
	r.Post(prefix+"/orders/refresh", s.startOrderRefreshJob)
	r.Get(prefix+"/orders/refresh/{job_id}", s.getOrderRefreshJob)
	r.Delete(prefix+"/orders/refresh/{job_id}", s.cancelOrderRefreshJob)
}

// startOrderRefreshJob 创建订单刷新任务并立即返回任务标识。
func (s *Server) startOrderRefreshJob(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证会话。
	sess := auth.SessionFromContext(r.Context())
	// cookieID、filterStatus 保存订单刷新筛选条件。
	cookieID, filterStatus := r.FormValue("cookie_id"), r.FormValue("status")
	if cookieID != "" && !s.orders().orderOwnedByUser(r.Context(), sess.UserID, cookieID) {
		writeErr(w, http.StatusForbidden, "Cookie不存在或无权访问")
		return
	}
	// job 保存待持久化的订单刷新任务。
	job := &orderapp.RefreshJob{
		ID: "order-refresh-" + randomHex(16), UserID: sess.UserID,
		CookieID: cookieID, FilterStatus: filterStatus,
	}
	// err 表示创建任务的数据库错误。
	if err := s.orders().refreshJobs.Create(r.Context(), job); err != nil {
		writeErr(w, http.StatusInternalServerError, "创建订单刷新任务失败")
		return
	}
	// token 保存本次执行者的租约令牌。
	token := randomHex(16)
	// claimed、claimErr 保存任务抢占结果及错误。
	claimed, claimErr := s.orders().refreshJobs.Claim(r.Context(), job.ID, token, time.Now().Add(orderRefreshJobLease).Unix())
	if claimErr != nil || !claimed {
		writeErr(w, http.StatusInternalServerError, "启动订单刷新任务失败")
		return
	}
	s.startOrderRefreshWorker(job, token)
	writeJSON(w, http.StatusAccepted, orderRefreshJobStartResponse{Success: true, JobID: job.ID, Status: "running"})
}

// getOrderRefreshJob 返回当前用户拥有的订单刷新任务状态和结果。
func (s *Server) getOrderRefreshJob(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证会话。
	sess := auth.SessionFromContext(r.Context())
	// jobID 保存路径中的订单刷新任务标识。
	jobID := chi.URLParam(r, "job_id")
	// job、err 保存任务读取结果及错误。
	job, err := s.orders().refreshJobs.Get(r.Context(), sess.UserID, jobID)
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
		// result 保存成功任务的具名刷新结果。
		var result orderRefreshResponse
		// err 表示任务结果 JSON 解析错误。
		if err := json.Unmarshal([]byte(job.ResultJSON), &result); err != nil {
			if s.Logger != nil {
				s.Logger.Error("解析订单刷新任务结果失败", "job_id", job.ID, "err", err)
			}
			writeErr(w, http.StatusInternalServerError, "读取订单刷新结果失败")
			return
		}
		response.Result = &result
	}
	writeJSON(w, http.StatusOK, response)
}

// cancelOrderRefreshJob 按当前用户归属取消订单刷新任务并通知运行中的 worker。
func (s *Server) cancelOrderRefreshJob(w http.ResponseWriter, r *http.Request) {
	// sess 保存当前认证会话。
	sess := auth.SessionFromContext(r.Context())
	// jobID 保存路径中的订单刷新任务标识。
	jobID := chi.URLParam(r, "job_id")
	// cancelled、err 保存数据库取消结果及错误。
	cancelled, err := s.orders().refreshJobs.Cancel(r.Context(), sess.UserID, jobID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "取消订单刷新任务失败")
		return
	}
	if cancelled {
		s.cancelOrderRefreshWorker(jobID)
		writeJSON(w, http.StatusOK, orderRefreshJobCancelResponse{Success: true, JobID: jobID, Status: "cancelled"})
		return
	}
	// job、getErr 保存取消未生效时的当前任务状态及查询错误。
	job, getErr := s.orders().refreshJobs.Get(r.Context(), sess.UserID, jobID)
	if errors.Is(getErr, orderapp.ErrRefreshJobNotFound) {
		writeErr(w, http.StatusNotFound, "订单刷新任务不存在")
		return
	}
	if getErr != nil {
		writeErr(w, http.StatusInternalServerError, "读取订单刷新任务失败")
		return
	}
	if job.Status == "cancelled" {
		writeJSON(w, http.StatusOK, orderRefreshJobCancelResponse{Success: true, JobID: jobID, Status: "cancelled"})
		return
	}
	writeErr(w, http.StatusConflict, "订单刷新任务已结束，无法取消")
}

// startOrderRefreshWorker 启动受 Server 生命周期管理的订单刷新 worker。
func (s *Server) startOrderRefreshWorker(job *orderapp.RefreshJob, token string) {
	if job == nil {
		return
	}
	// parent 保存 Server 生命周期上下文，避免 HTTP 请求结束取消后台任务。
	parent := s.lifecycleContext()
	if parent == nil {
		parent = context.Background()
	}
	// jobCtx、cancel 限制后台任务执行时间并支持用户取消。
	jobCtx, cancel := context.WithTimeout(parent, orderRefreshJobTimeout)
	s.registerOrderRefreshWorker(job.ID, token, cancel)
	s.startBackgroundTask("订单刷新任务", func() {
		defer cancel()
		defer s.unregisterOrderRefreshWorker(job.ID, token)
		s.runOrderRefreshJob(jobCtx, job, token)
	})
}

// registerOrderRefreshWorker 登记可取消的订单刷新 worker。
func (s *Server) registerOrderRefreshWorker(jobID, token string, cancel context.CancelFunc) {
	s.orderRefreshMu.Lock()
	defer s.orderRefreshMu.Unlock()
	if s.orderRefreshCancels == nil {
		s.orderRefreshCancels = make(map[string]orderRefreshWorker)
	}
	// previous 保存同一任务的旧 worker 控制句柄。
	previous := s.orderRefreshCancels[jobID]
	if previous.cancel != nil && previous.token != token {
		previous.cancel()
	}
	s.orderRefreshCancels[jobID] = orderRefreshWorker{token: token, cancel: cancel}
}

// unregisterOrderRefreshWorker 在 worker 退出时清理控制句柄。
func (s *Server) unregisterOrderRefreshWorker(jobID, token string) {
	s.orderRefreshMu.Lock()
	defer s.orderRefreshMu.Unlock()
	// current 保存当前任务登记的 worker 控制句柄。
	current := s.orderRefreshCancels[jobID]
	if current.token == token {
		delete(s.orderRefreshCancels, jobID)
	}
}

// cancelOrderRefreshWorker 取消内存中的订单刷新 worker，不改变数据库终态。
func (s *Server) cancelOrderRefreshWorker(jobID string) bool {
	s.orderRefreshMu.Lock()
	// worker 保存当前任务的取消控制句柄。
	worker := s.orderRefreshCancels[jobID]
	s.orderRefreshMu.Unlock()
	if worker.cancel == nil {
		return false
	}
	worker.cancel()
	return true
}

// runOrderRefreshJob 执行订单刷新并以租约令牌写入成功或失败终态。
func (s *Server) runOrderRefreshJob(ctx context.Context, job *orderapp.RefreshJob, token string) {
	// result、err 保存订单刷新业务结果及错误。
	result, err := s.orders().Refresh(ctx, job.UserID, job.CookieID, job.FilterStatus)
	if err != nil {
		// completeErr 表示失败终态写入错误。
		if _, completeErr := s.orders().refreshJobs.Complete(context.Background(), job.ID, token, "failed", "{}", err.Error()); completeErr != nil && s.Logger != nil {
			s.Logger.Warn("写入订单刷新失败终态失败", "job_id", job.ID, "err", completeErr)
		}
		return
	}
	// resultJSON、marshalErr 保存具名刷新结果 JSON 及序列化错误。
	resultJSON, marshalErr := json.Marshal(result)
	if marshalErr != nil {
		_, _ = s.orders().refreshJobs.Complete(context.Background(), job.ID, token, "failed", "{}", marshalErr.Error())
		return
	}
	// completeErr 表示成功终态写入错误。
	if _, completeErr := s.orders().refreshJobs.Complete(context.Background(), job.ID, token, "succeeded", string(resultJSON), ""); completeErr != nil && s.Logger != nil {
		s.Logger.Warn("写入订单刷新成功终态失败", "job_id", job.ID, "err", completeErr)
	}
}

// RunOrderRefreshRecovery 扫描并重新执行租约过期的订单刷新任务。
func (s *Server) RunOrderRefreshRecovery(ctx context.Context) {
	s.recoverOrderRefreshJobsOnce(ctx)
	// ticker 保存恢复扫描间隔计时器。
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.recoverOrderRefreshJobsOnce(ctx)
		}
	}
}

// StartOrderRefreshRecovery 启动受 Server 生命周期管理的订单刷新恢复扫描器。
func (s *Server) StartOrderRefreshRecovery(ctx context.Context) {
	s.startBackgroundTask("订单刷新恢复扫描器", func() {
		s.RunOrderRefreshRecovery(ctx)
	})
}

// recoverOrderRefreshJobsOnce 将过期任务重新入队并启动新的 worker。
func (s *Server) recoverOrderRefreshJobsOnce(ctx context.Context) {
	// jobs、err 保存可恢复任务及扫描错误。
	jobs, err := s.orders().refreshJobs.Recoverable(ctx, time.Now().Unix(), 20)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("扫描订单刷新恢复任务失败", "err", err)
		}
		return
	}
	// job 表示当前待恢复的订单刷新任务。
	for _, job := range jobs {
		// requeued、requeueErr 保存重新入队结果及错误。
		requeued, requeueErr := s.orders().refreshJobs.RequeueExpired(ctx, job.ID, time.Now().Unix())
		if requeueErr != nil || !requeued {
			continue
		}
		// token 保存恢复 worker 的新租约令牌。
		token := randomHex(16)
		// claimed、claimErr 保存恢复任务抢占结果及错误。
		claimed, claimErr := s.orders().refreshJobs.Claim(ctx, job.ID, token, time.Now().Add(orderRefreshJobLease).Unix())
		if claimErr == nil && claimed {
			s.startOrderRefreshWorker(&job, token)
		}
	}
}
