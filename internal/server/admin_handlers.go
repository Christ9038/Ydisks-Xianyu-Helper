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
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT id, username, email, is_active, is_admin, created_at FROM users ORDER BY id`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id int64
		var username, email, createdAt string
		var isActive, isAdmin int
		if err := rows.Scan(&id, &username, &email, &isActive, &isAdmin, &createdAt); err != nil {
			writeErr(w, http.StatusInternalServerError, "读取用户数据失败")
			return
		}
		// 统计每个用户的账号数。
		var cookieCount int
		if err := s.Store.DB.QueryRowContext(r.Context(),
			`SELECT COUNT(*) FROM cookies WHERE user_id=?`, id).Scan(&cookieCount); err != nil {
			writeErr(w, http.StatusInternalServerError, "统计用户账号失败")
			return
		}
		out = append(out, map[string]any{
			"id": id, "username": username, "email": email,
			"is_active": isActive != 0, "is_admin": isAdmin != 0,
			"created_at": createdAt, "cookie_count": cookieCount,
		})
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, "读取用户数据失败")
		return
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
		if accounts, listErr := s.Store.Cookies.AllForUser(r.Context(), uid); listErr == nil {
			for cookieID := range accounts {
				s.Manager.Stop(cookieID)
			}
		}
	}
	if err := s.Store.Users.Delete(r.Context(), uid); err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) adminListCookies(w http.ResponseWriter, r *http.Request) {
	rows, err := s.Store.DB.QueryContext(r.Context(),
		`SELECT c.id, c.user_id, COALESCE(c.remark,''), c.created_at, u.username
		 FROM cookies c LEFT JOIN users u ON c.user_id=u.id ORDER BY c.created_at DESC`)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id string
		var uid int64
		var remark, createdAt, username string
		if err := rows.Scan(&id, &uid, &remark, &createdAt, &username); err != nil {
			writeErr(w, http.StatusInternalServerError, "读取账号数据失败")
			return
		}
		out = append(out, map[string]any{
			"id": id, "user_id": uid, "remark": remark,
			"created_at": createdAt, "owner": username,
			"enabled": s.Store.Cookies.GetStatus(r.Context(), id),
		})
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, "读取账号数据失败")
		return
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) adminStats(w http.ResponseWriter, r *http.Request) {
	// 字段名与前端 AdminStats 接口对齐：
	// total_users / total_cookies / active_cookies / total_cards / total_keywords / total_orders
	var totalUsers, totalCookies, totalCards, totalOrders, totalKeywords int64
	counts := []struct {
		query string
		dest  *int64
	}{
		{`SELECT COUNT(*) FROM users`, &totalUsers},
		{`SELECT COUNT(*) FROM cookies`, &totalCookies},
		{`SELECT COUNT(*) FROM cards`, &totalCards},
		{`SELECT COUNT(*) FROM orders`, &totalOrders},
		{`SELECT COUNT(*) FROM keywords`, &totalKeywords},
	}
	for _, count := range counts {
		if err := s.Store.DB.QueryRowContext(r.Context(), count.query).Scan(count.dest); err != nil {
			writeErr(w, http.StatusInternalServerError, "统计数据失败")
			return
		}
	}

	// 活跃账号：cookie_status.enabled=1 的数量（无记录默认启用，故统计 enabled=1 或无记录的）。
	var activeCookies int64
	if err := s.Store.DB.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM cookies c
		WHERE NOT EXISTS (SELECT 1 FROM cookie_status cs WHERE cs.cookie_id=c.id AND cs.enabled=0)
	`).Scan(&activeCookies); err != nil {
		writeErr(w, http.StatusInternalServerError, "统计活跃账号失败")
		return
	}

	writeJSON(w, http.StatusOK, map[string]int64{
		"total_users":    totalUsers,
		"total_cookies":  totalCookies,
		"active_cookies": activeCookies,
		"total_cards":    totalCards,
		"total_keywords": totalKeywords,
		"total_orders":   totalOrders,
	})
}
