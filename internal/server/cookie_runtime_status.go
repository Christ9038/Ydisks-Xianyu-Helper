package server

import (
	"net/http"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/auth"
)

// listCookieRuntimeStatus 返回本地账号引擎状态，不请求闲鱼 API；停用但仍存活的实例必须作为冲突诊断返回。
func (s *Server) listCookieRuntimeStatus(w http.ResponseWriter, r *http.Request) {
	// sess 是当前认证会话，只用于按用户过滤非敏感账号标识。
	sess := auth.SessionFromContext(r.Context())
	// accountSummary 提供账号归属、启用状态和非敏感 ID 查询。
	accountSummary := s.accountSummaryApplication()
	// cookieIDs、listErr 保存当前用户账号标识列表及读取错误。
	cookieIDs, listErr := accountSummary.ListOwnedIDs(r.Context(), sess.UserID)
	if listErr != nil {
		writeErr(w, http.StatusInternalServerError, "获取账号失败")
		return
	}
	// runtime、runtimeErr 保存应用层返回的账号运行状态快照及读取错误。
	runtime, runtimeErr := s.accountRuntimeApplication().RuntimeStatuses(r.Context())
	if runtimeErr != nil {
		writeErr(w, http.StatusInternalServerError, "获取账号运行状态失败")
		return
	}
	// result 是按当前用户账号 ID 返回的非敏感运行状态映射。
	result := make(map[string]accountapp.RuntimeStatus, len(cookieIDs))
	// cid 是当前需要合并数据库启用状态和运行实例快照的账号标识。
	for _, cid := range cookieIDs {
		// enabled、statusErr 保存数据库启用状态及归属复核错误。
		enabled, statusErr := accountSummary.StatusOwned(r.Context(), sess.UserID, cid)
		if statusErr != nil {
			result[cid] = accountapp.RuntimeStatus{State: "disabled", Message: "账号已停用", UpdatedAt: time.Now()}
			continue
		}
		// status、ok 保存运行实例快照及其存在标记。
		if status, ok := runtime[cid]; ok {
			if !enabled && runtimeStatusIsActive(status) {
				status.State = "runtime_conflict"
				status.Message = "数据库已停用，但运行实例仍存活，请重试停用"
				result[cid] = status
				continue
			}
			result[cid] = status
			continue
		}
		if !enabled {
			result[cid] = accountapp.RuntimeStatus{State: "disabled", Message: "账号已停用", UpdatedAt: time.Now()}
			continue
		}
		result[cid] = accountapp.RuntimeStatus{State: "error", Message: "账号服务未运行", UpdatedAt: time.Now()}
	}
	writeJSON(w, http.StatusOK, result)
}

// runtimeStatusIsActive 判断运行时快照是否代表仍需关注的活动实例；已退出的 error/stopped 不伪装成冲突。
func runtimeStatusIsActive(status accountapp.RuntimeStatus) bool {
	if status.Connected {
		return true
	}
	switch status.State {
	case "starting", "connecting", "online", "reconnecting", "auth_expired", "verification_required":
		return true
	default:
		return false
	}
}
