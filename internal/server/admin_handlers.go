package server

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
)

// mountAdminReal 管理员端点。
func (s *Server) mountAdminReal(r chi.Router) {
	r.Get("/admin/users", s.adminListUsers)
	r.Delete("/admin/users/{user_id}", s.adminDeleteUser)
	r.Get("/admin/cookies", s.adminListCookies)
	r.Get("/admin/stats", s.adminStats)
}

func (s *Server) adminListUsers(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.Admin.ListUsers(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	var out []adminUserResponse
	for _, row := range rows {
		out = append(out, adminUserResponse{
			ID: row.ID, Username: row.Username, Email: row.Email,
			IsActive: row.IsActive, IsAdmin: row.IsAdmin,
			CreatedAt: row.CreatedAt, CookieCount: row.CookieCount,
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminDeleteUser(w http.ResponseWriter, r *http.Request) {
	uid, err := strconv.ParseInt(chi.URLParam(r, "user_id"), 10, 64)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "无效用户ID")
		return
	}
	// 不允许删除自己。
	sess := authSess(r)
	if sess.UserID == uid {
		writeErr(w, http.StatusBadRequest, "不能删除当前登录用户")
		return
	}
	if s.Manager != nil {
		if accountIDs, listErr := s.Store.Cookies.ListOwnedIDs(r.Context(), uid); listErr == nil { // accountIDs 是待停止的账号 ID。
			for _, cookieID := range accountIDs {
				s.Manager.Stop(cookieID)
			}
		}
	}
	if err := s.Store.Users.Delete(r.Context(), uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

func (s *Server) adminListCookies(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.Admin.ListCookies(r.Context())
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	var out []adminCookieResponse
	for _, row := range rows {
		out = append(out, adminCookieResponse{
			ID: row.ID, UserID: row.UserID, Remark: row.Remark,
			CreatedAt: row.CreatedAt, Owner: row.Owner,
			Enabled: s.Store.Cookies.GetStatus(r.Context(), row.ID),
		})
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	// stats 是管理员仪表盘的数据库聚合结果。
	stats, err := s.Store.Admin.Stats(r.Context())
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
