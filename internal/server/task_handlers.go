package server

import (
	"net/http"
	"strconv"
	"time"
)

// taskStatusResponse 是管理端查询 Server 后台任务状态时使用的具名响应 DTO。
type taskStatusResponse struct {
	// ID 是进程内任务标识，可用于关联日志和本次查询结果。
	ID string `json:"id"`
	// Name 是任务的稳定业务名称。
	Name string `json:"name"`
	// State 是 running、succeeded、failed、canceled 或 timed_out 之一。
	State string `json:"state"`
	// StartedAt 是任务登记时间，使用 UTC RFC3339 JSON 格式。
	StartedAt time.Time `json:"started_at"`
	// FinishedAt 是任务终态时间；运行中的任务为 null。
	FinishedAt *time.Time `json:"finished_at,omitempty"`
	// DeadlineAt 是任务上下文截止时间；无截止时间时为 null。
	DeadlineAt *time.Time `json:"deadline_at,omitempty"`
}

// listAdminTasks 返回管理员可见的 Server 后台任务状态，不包含任务参数和敏感数据。
func (s *Server) listAdminTasks(w http.ResponseWriter, r *http.Request) {
	// limit 控制单次响应的历史任务数量，避免管理端一次读取过大结果。
	limit := 128
	// rawLimit 是管理端请求的 limit 查询参数原文。
	if rawLimit := r.URL.Query().Get("limit"); rawLimit != "" {
		// parsedLimit 是调用方请求的历史条数。
		parsedLimit, parseErr := strconv.Atoi(rawLimit)
		if parseErr != nil || parsedLimit < 1 || parsedLimit > 128 {
			writeErr(w, http.StatusBadRequest, "limit 必须是 1 到 128 之间的整数")
			return
		}
		limit = parsedLimit
	}
	// snapshots 是注册表返回的后台任务状态副本。
	snapshots := s.taskRegistryForServer().list()
	// result 是经过 transport DTO 转换后的管理端响应列表。
	result := make([]taskStatusResponse, 0, minInt(limit, len(snapshots)))
	// index、snapshot 分别表示返回列表中的下标和当前后台任务状态快照。
	for index, snapshot := range snapshots {
		if index >= limit {
			break
		}
		result = append(result, taskStatusResponse{
			ID: snapshot.ID, Name: snapshot.Name, State: string(snapshot.State),
			StartedAt: snapshot.StartedAt, FinishedAt: snapshot.FinishedAt, DeadlineAt: snapshot.DeadlineAt,
		})
	}
	writeJSON(w, http.StatusOK, result)
}

// minInt 返回两个非负整数中的较小值，供响应容量计算使用。
func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}
