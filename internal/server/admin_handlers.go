package server

import (
	"errors"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	adminapp "xianyu-go/internal/application/admin"
)

// mountAdminReal 管理员端点。
func (s *Server) mountAdminReal(r chi.Router) {
	r.Get("/admin/users", s.adminListUsers)
	r.Delete("/admin/users/{user_id}", s.adminDeleteUser)
	r.Get("/admin/cookies", s.adminListCookies)
	r.Get("/admin/stats", s.adminStats)
}

// adminListUsers 封装adminList用户列表业务协调。
func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request) {
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := s.applicationServiceSet().admin.ListUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 用于本次流程后续判断的out
	var out []adminUserResponse
	// row 表示当前遍历过程中的row
	for _, row := range rows {
		out = append(out, adminUserResponse{
			ID: row.ID, Username: row.Username, Email: row.Email,
			IsActive: row.IsActive, IsAdmin: row.IsAdmin,
			CreatedAt: row.CreatedAt, CookieCount: row.CookieCount,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// adminDeleteUser 封装adminDelete用户业务协调。
func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	// uid、err 用于本次流程后续判断的uid、err
	uid, err := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效用户ID")
		return
	}
	// 不允许删除自己；应用服务统一执行该业务规则。
	sess := authSess(r)
	// err 保存管理员删除应用用例的执行结果。
	if err := s.applicationServiceSet().admin.DeleteUser(r.Context(), sess.UserID, uid); err != nil {
		if errors.Is(err, adminapp.ErrSelfDelete) {
			writeErr(w, http.StatusBadRequest, "不能删除当前登录用户")
			return
		}
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// adminListCookies 封装adminListCookies业务协调。
func (s *Server) adminListCookies(w http.ResponseWriter, r *http.Request) {
	// rows、err 保存应用服务返回的管理员账号摘要及查询错误。
	rows, err := s.accountSummaryApplication().ListAdminSummaries(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	// out 用于本次流程后续判断的out
	var out []adminCookieResponse
	// row 表示当前遍历过程中的row
	for _, row := range rows {
		out = append(out, adminCookieResponse{
			ID: row.ID, UserID: row.UserID, Remark: row.Remark,
			CreatedAt: row.CreatedAt, Owner: row.Owner,
			Enabled: row.Enabled,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

// adminStats 封装adminStats业务协调。
func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	// stats 是管理员仪表盘的数据库聚合结果。
	stats, err := s.applicationServiceSet().admin.Stats(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "统计数据失败")
		return
	}

	writeJSON(w, http.StatusOK, adminStatsResponse{
		TotalUsers: stats.TotalUsers, TotalCookies: stats.TotalCookies, ActiveCookies: stats.ActiveCookies,
		TotalCards: stats.TotalCards, TotalKeywords: stats.TotalKeywords, TotalOrders: stats.TotalOrders,
		// 统计响应继续保留原有字段名称，兼容管理员仪表盘。
		// DTO 字段由具名结构统一维护，避免动态 map 漏字段。
		// 所有统计值均来自当前数据库快照。
		// 成功响应不再依赖任意键名拼接。
	})
}
