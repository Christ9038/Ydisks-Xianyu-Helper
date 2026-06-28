package server

import (
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
)

// mountAnalyticsReal 订单分析端点（仪表盘 BI 报表用）。
func (s *Server) mountAnalyticsReal(r chi.Router) {
	r.Get("/analytics/orders", s.orderAnalytics)
	r.Get("/analytics/orders/valid", s.validOrders)
}

// 有效订单状态（只统计这几种，与 Python 一致）。
var validOrderStatuses = []string{"pending_ship", "shipped", "completed"}

// orderAnalytics 订单分析：收益统计 + 按日/状态/城市/商品分布。
// 移植自 Python get_order_analytics。
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
	s.Store.DB.QueryRowContext(r.Context(), `
		SELECT COUNT(DISTINCT order_id), COALESCE(SUM(`+amountClean+`),0),
		       COALESCE(AVG(`+amountClean+`),0), COUNT(DISTINCT buyer_id), COUNT(DISTINCT item_id)
		FROM orders `+where+amountFilter, params...).Scan(
		&rev.TotalOrders, &rev.TotalAmount, &rev.AvgAmount, &rev.UniqueBuyers, &rev.UniqueItems)

	// 2. 按日统计。
	daily := []map[string]any{}
	rows, err := s.Store.DB.QueryContext(r.Context(), `
		SELECT DATE(created_at), COUNT(DISTINCT order_id), COALESCE(SUM(`+amountClean+`),0)
		FROM orders `+where+amountFilter+`
		GROUP BY DATE(created_at) ORDER BY DATE(created_at) DESC LIMIT 30`, params...)
	if err == nil {
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
		rows.Close()
	}

	// 3. 按状态统计。
	statusStats := []map[string]any{}
	rows, err = s.Store.DB.QueryContext(r.Context(), `
		SELECT COALESCE(order_status,'unknown'), COUNT(DISTINCT order_id), COALESCE(SUM(`+amountClean+`),0)
		FROM orders `+where+amountFilter+`
		GROUP BY order_status ORDER BY COUNT(DISTINCT order_id) DESC`, params...)
	if err == nil {
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
		rows.Close()
	}

	// 4. 按城市统计。
	cityStats := []map[string]any{}
	rows, err = s.Store.DB.QueryContext(r.Context(), `
		SELECT receiver_city, COUNT(DISTINCT order_id), COALESCE(SUM(`+amountClean+`),0)
		FROM orders `+where+amountFilter+`
		  AND receiver_city IS NOT NULL AND receiver_city != ''
		GROUP BY receiver_city ORDER BY COUNT(DISTINCT order_id) DESC LIMIT 50`, params...)
	if err == nil {
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
		rows.Close()
	}

	// 5. 商品排行。
	itemStats := []map[string]any{}
	rows, err = s.Store.DB.QueryContext(r.Context(), `
		SELECT item_id, COUNT(DISTINCT order_id), COALESCE(SUM(`+amountClean+`),0), COALESCE(AVG(`+amountClean+`),0)
		FROM orders `+where+amountFilter+`
		  AND item_id IS NOT NULL AND item_id != ''
		GROUP BY item_id ORDER BY COUNT(DISTINCT order_id) DESC LIMIT 20`, params...)
	if err == nil {
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
		rows.Close()
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"revenue_stats": map[string]any{
			"total_orders": rev.TotalOrders, "total_amount": round2(rev.TotalAmount),
			"avg_amount": round2(rev.AvgAmount), "unique_buyers": rev.UniqueBuyers,
			"unique_items": rev.UniqueItems,
		},
		"daily_stats":   daily,
		"status_stats":  statusStats,
		"city_stats":    cityStats,
		"item_stats":    itemStats,
	})
}

// validOrders 有效订单明细列表（用于统计中的订单明细）。
func (s *Server) validOrders(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	where, params := buildAnalyticsWhere(startDate, endDate, sess.UserID, validOrderStatuses)

	rows, err := s.Store.DB.QueryContext(r.Context(), `
		SELECT order_id, item_id, buyer_id, amount, order_status, cookie_id, created_at
		FROM orders `+where+` ORDER BY created_at DESC LIMIT 500`, params...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var orderID, itemID, buyerID, amount, status, cookieID, createdAt string
		if rows.Scan(&orderID, &itemID, &buyerID, &amount, &status, &cookieID, &createdAt) == nil {
			out = append(out, map[string]any{
				"order_id": orderID, "item_id": itemID, "buyer_id": buyerID,
				"amount": amount, "order_status": status, "cookie_id": cookieID,
				"created_at": createdAt,
			})
		}
	}
	writeJSON(w, http.StatusOK, out)
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
