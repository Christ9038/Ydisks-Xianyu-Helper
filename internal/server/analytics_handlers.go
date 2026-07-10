package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountAnalyticsReal 订单分析端点（仪表盘 BI 报表用）。
func (s *Server) mountAnalyticsReal(r chi.Router) {
	r.Get("/dashboard/stats", s.dashboardStats)
	r.Get("/analytics/orders", s.orderAnalytics)
	r.Get("/analytics/orders/valid", s.validOrders)
}

// dashboardStats 返回当前登录用户的数据概览。管理员全局统计仍由 /admin/stats 提供，
// 避免普通用户访问管理员接口，也避免把全局资源数和用户自己的订单收益混在一起。
func (s *Server) dashboardStats(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	if sess == nil {
		writeErr(w, http.StatusUnauthorized, "未登录")
		return
	}

	counts := map[string]int64{
		"total_cookies":  0,
		"active_cookies": 0,
		"total_cards":    0,
		"total_keywords": 0,
		"total_orders":   0,
	}
	queries := []struct {
		key   string
		query string
	}{
		{"total_cookies", `SELECT COUNT(*) FROM cookies WHERE user_id=?`},
		{"total_cards", `SELECT COUNT(*) FROM cards WHERE user_id=?`},
		{"total_keywords", `SELECT COUNT(*) FROM keywords k WHERE EXISTS (
			SELECT 1 FROM cookies c WHERE c.id=k.cookie_id AND c.user_id=?)`},
		{"total_orders", `SELECT COUNT(*) FROM orders o WHERE EXISTS (
			SELECT 1 FROM cookies c WHERE c.id=o.cookie_id AND c.user_id=?)`},
	}
	for _, item := range queries {
		var count int64
		if err := s.Store.DB.QueryRowContext(r.Context(), item.query, sess.UserID).Scan(&count); err != nil {
			writeErr(w, http.StatusInternalServerError, "统计数据失败")
			return
		}
		counts[item.key] = count
	}

	var activeCookies int64
	if err := s.Store.DB.QueryRowContext(r.Context(), `
		SELECT COUNT(*) FROM cookies c
		WHERE c.user_id=?
		  AND NOT EXISTS (SELECT 1 FROM cookie_status cs WHERE cs.cookie_id=c.id AND cs.enabled=0)
	`, sess.UserID).Scan(&activeCookies); err != nil {
		writeErr(w, http.StatusInternalServerError, "统计活跃账号失败")
		return
	}
	counts["active_cookies"] = activeCookies

	writeJSON(w, http.StatusOK, counts)
}

// 有效订单状态只统计以下几种。
var validOrderStatuses = []string{"pending_ship", "shipped", "completed"}

// orderAnalytics 汇总指定日期范围内的收益以及按日、状态、城市和商品分布。
func (s *Server) orderAnalytics(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")

	// 构建 WHERE 条件（user_id 通过 cookies 关联过滤）。
	where, params := buildAnalyticsWhere(startDate, endDate, sess.UserID, validOrderStatuses)
	// 金额清洗：去掉 ¥ 和逗号后转 REAL。
	amountClean := `CAST(REPLACE(REPLACE(amount, '¥', ''), ',', '') AS REAL)`
	amountFilter := ` AND amount IS NOT NULL AND amount != '' AND amount != 'N/A'`

	// 1. 收益统计。
	var rev struct {
		TotalOrders  int
		TotalAmount  float64
		AvgAmount    float64
		UniqueBuyers int
		UniqueItems  int
	}
	if err := s.Store.DB.QueryRowContext(r.Context(), `
		SELECT COUNT(DISTINCT order_id), COALESCE(SUM(`+amountClean+`),0),
		       COALESCE(AVG(`+amountClean+`),0), COUNT(DISTINCT buyer_id), COUNT(DISTINCT item_id)
		FROM orders `+where+amountFilter, params...).Scan(
		&rev.TotalOrders, &rev.TotalAmount, &rev.AvgAmount, &rev.UniqueBuyers, &rev.UniqueItems); err != nil {
		writeErr(w, http.StatusInternalServerError, "查询收益统计失败")
		return
	}

	// 2. 按日统计。
	daily := []map[string]any{}
	rows, err := s.Store.DB.QueryContext(r.Context(), `
		SELECT DATE(created_at), COUNT(DISTINCT order_id), COALESCE(SUM(`+amountClean+`),0)
		FROM orders `+where+amountFilter+`
		GROUP BY DATE(created_at) ORDER BY DATE(created_at) DESC LIMIT 30`, params...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询每日统计失败")
		return
	}
	for rows.Next() {
		var date string
		var count int
		var amount float64
		if rows.Scan(&date, &count, &amount) == nil {
			daily = append(daily, map[string]any{
				"date": date, "order_count": count, "amount": round2(amount),
			})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		writeErr(w, http.StatusInternalServerError, "查询每日统计失败")
		return
	}
	_ = rows.Close()

	// 3. 按状态统计。
	statusStats := []map[string]any{}
	rows, err = s.Store.DB.QueryContext(r.Context(), `
		SELECT COALESCE(order_status,'unknown'), COUNT(DISTINCT order_id), COALESCE(SUM(`+amountClean+`),0)
		FROM orders `+where+amountFilter+`
		GROUP BY order_status ORDER BY COUNT(DISTINCT order_id) DESC`, params...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询状态统计失败")
		return
	}
	for rows.Next() {
		var status string
		var count int
		var amount float64
		if rows.Scan(&status, &count, &amount) == nil {
			statusStats = append(statusStats, map[string]any{
				"status": status, "count": count, "amount": round2(amount),
			})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		writeErr(w, http.StatusInternalServerError, "查询状态统计失败")
		return
	}
	_ = rows.Close()

	// 4. 按城市统计。
	cityStats := []map[string]any{}
	rows, err = s.Store.DB.QueryContext(r.Context(), `
		SELECT receiver_city, COUNT(DISTINCT order_id), COALESCE(SUM(`+amountClean+`),0)
		FROM orders `+where+amountFilter+`
		  AND receiver_city IS NOT NULL AND receiver_city != ''
		GROUP BY receiver_city ORDER BY COUNT(DISTINCT order_id) DESC LIMIT 50`, params...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询城市统计失败")
		return
	}
	for rows.Next() {
		var city string
		var count int
		var amount float64
		if rows.Scan(&city, &count, &amount) == nil {
			cityStats = append(cityStats, map[string]any{
				"city": city, "order_count": count, "total_amount": round2(amount),
			})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		writeErr(w, http.StatusInternalServerError, "查询城市统计失败")
		return
	}
	_ = rows.Close()

	// 5. 商品排行。
	itemStats := []map[string]any{}
	rows, err = s.Store.DB.QueryContext(r.Context(), `
		SELECT item_id, COUNT(DISTINCT order_id), COALESCE(SUM(`+amountClean+`),0), COALESCE(AVG(`+amountClean+`),0)
		FROM orders `+where+amountFilter+`
		  AND item_id IS NOT NULL AND item_id != ''
		GROUP BY item_id ORDER BY COUNT(DISTINCT order_id) DESC LIMIT 20`, params...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询商品统计失败")
		return
	}
	for rows.Next() {
		var itemID string
		var count int
		var total, avg float64
		if rows.Scan(&itemID, &count, &total, &avg) == nil {
			itemStats = append(itemStats, map[string]any{
				"item_id": itemID, "order_count": count,
				"total_amount": round2(total), "avg_amount": round2(avg),
			})
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		writeErr(w, http.StatusInternalServerError, "查询商品统计失败")
		return
	}
	_ = rows.Close()

	writeJSON(w, http.StatusOK, map[string]any{
		"revenue_stats": map[string]any{
			"total_orders": rev.TotalOrders, "total_amount": round2(rev.TotalAmount),
			"avg_amount": round2(rev.AvgAmount), "unique_buyers": rev.UniqueBuyers,
			"unique_items": rev.UniqueItems,
		},
		"daily_stats":  daily,
		"status_stats": statusStats,
		"city_stats":   cityStats,
		"item_stats":   itemStats,
	})
}

// validOrders 有效订单明细列表（用于统计中的订单明细）。
func (s *Server) validOrders(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	where, params := buildAnalyticsWhere(startDate, endDate, sess.UserID, validOrderStatuses)
	amountFilter := ` AND orders.amount IS NOT NULL AND orders.amount != '' AND orders.amount != 'N/A'`

	rows, err := s.Store.DB.QueryContext(r.Context(), `
		SELECT orders.order_id, orders.item_id, COALESCE(item_info.item_title, ''),
		       COALESCE(item_info.item_detail, ''), orders.buyer_id, COALESCE(orders.quantity, '1'),
		       orders.amount, orders.order_status, orders.cookie_id, orders.created_at
		FROM orders
		LEFT JOIN item_info ON item_info.cookie_id = orders.cookie_id AND item_info.item_id = orders.item_id
		`+qualifyAnalyticsWhere(where)+amountFilter+` ORDER BY orders.created_at DESC LIMIT 500`, params...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var orderID, itemID, itemTitle, itemDetail, buyerID, quantity, amount, status, cookieID, createdAt string
		if rows.Scan(&orderID, &itemID, &itemTitle, &itemDetail, &buyerID, &quantity, &amount, &status, &cookieID, &createdAt) == nil {
			out = append(out, map[string]any{
				"order_id": orderID, "item_id": itemID, "buyer_id": buyerID,
				"item_title": itemTitle, "item_image": itemImageFromDetail(itemDetail),
				"quantity": quantity, "amount": amount, "order_status": status,
				"status": status, "cookie_id": cookieID, "created_at": createdAt,
			})
		}
	}
	if err := rows.Err(); err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"orders": out})
}

func qualifyAnalyticsWhere(where string) string {
	where = strings.ReplaceAll(where, "DATE(created_at)", "DATE(orders.created_at)")
	where = strings.ReplaceAll(where, "order_status IN", "orders.order_status IN")
	return where
}

// buildAnalyticsWhere 构建 WHERE 子句（user_id 经 cookies 关联过滤 + 日期 + 状态）。
// 返回 (whereClause, params)，whereClause 已含 WHERE 前缀。
func buildAnalyticsWhere(startDate, endDate string, userID int64, statuses []string) (string, []any) {
	conds := []string{}
	params := []any{}
	if startDate != "" {
		conds = append(conds, "DATE(created_at) >= ?")
		params = append(params, startDate)
	}
	if endDate != "" {
		conds = append(conds, "DATE(created_at) <= ?")
		params = append(params, endDate)
	}
	if userID != 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM cookies WHERE cookies.id = orders.cookie_id AND cookies.user_id = ?)")
		params = append(params, userID)
	}
	if len(statuses) > 0 {
		ph := strings.Repeat("?,", len(statuses))
		ph = strings.TrimSuffix(ph, ",")
		conds = append(conds, "order_status IN ("+ph+")")
		for _, s := range statuses {
			params = append(params, s)
		}
	}
	where := ""
	if len(conds) > 0 {
		where = "WHERE " + strings.Join(conds, " AND ")
	}
	// 后续 AND 需要前置空格。
	if where != "" {
		where += " "
	}
	return where, params
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
