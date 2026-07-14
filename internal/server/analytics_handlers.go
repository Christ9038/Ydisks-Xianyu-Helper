package server

import (
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
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
		"total_cookies":        0,
		"active_cookies":       0,
		"total_cards":          0,
		"available_card_stock": 0,
		"total_keywords":       0,
		"total_orders":         0,
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
	cards, err := s.Store.Cards.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "统计卡密库存失败")
		return
	}
	var available int64
	for _, card := range cards {
		if card.Enabled && card.Type == "data" {
			for _, line := range strings.Split(strings.ReplaceAll(card.DataContent, "\r\n", "\n"), "\n") {
				if strings.TrimSpace(line) != "" {
					available++
				}
			}
		}
	}
	counts["available_card_stock"] = available

	writeJSON(w, http.StatusOK, counts)
}

// 有效订单状态只统计以下几种。
var validOrderStatuses = []string{"pending_ship", "paid", "2", "shipped", "3", "completed", "4", "11"}

// orderAnalytics 汇总指定日期范围内的收益以及按日、状态、城市和商品分布。
func (s *Server) orderAnalytics(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	location := analyticsLocation(r.URL.Query().Get("timezone_offset_minutes"))

	// 构建 WHERE 条件（user_id 通过 cookies 关联过滤）。
	where, params := buildAnalyticsWhere(startDate, endDate, sess.UserID, validOrderStatuses, location)
	// 金额先做方言相关的格式校验再转换，避免 PostgreSQL 遇到历史脏数据时整页 500。
	amountClean := analyticsAmountExpression(s.Store.Dialect, "amount")
	amountFilter := ` AND ` + amountClean + ` IS NOT NULL`

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

	// 2. 按用户本地日期统计。数据库统一保存 UTC，分组在 Go 中完成，避免三种方言
	// 的时区转换函数不同以及 SQLite DATE(created_at) 把凌晨订单归到前一天。
	daily := []map[string]any{}
	rows, err := s.Store.DB.QueryContext(r.Context(), `
		SELECT order_id,amount,created_at FROM orders `+where+amountFilter, params...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询每日统计失败")
		return
	}
	type dailyValue struct {
		count  int
		amount float64
	}
	dailyMap := map[string]dailyValue{}
	for rows.Next() {
		var orderID, amountRaw, createdAt string
		if rows.Scan(&orderID, &amountRaw, &createdAt) != nil {
			continue
		}
		created := parseAnalyticsDBTime(createdAt)
		if created.IsZero() {
			continue
		}
		date := created.In(location).Format("2006-01-02")
		value := dailyMap[date]
		value.count++
		value.amount += parseAnalyticsAmount(amountRaw)
		dailyMap[date] = value
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		writeErr(w, http.StatusInternalServerError, "查询每日统计失败")
		return
	}
	_ = rows.Close()
	dates := make([]string, 0, len(dailyMap))
	for date := range dailyMap {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	for _, date := range dates {
		value := dailyMap[date]
		daily = append(daily, map[string]any{"date": date, "order_count": value.count, "amount": round2(value.amount)})
	}

	// 3. 按状态统计。
	statusStats := []map[string]any{}
	type statusValue struct {
		count  int
		amount float64
	}
	statusMap := map[string]statusValue{}
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
			status = db.NormalizeOrderStatus(status)
			value := statusMap[status]
			value.count += count
			value.amount += amount
			statusMap[status] = value
		}
	}
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		writeErr(w, http.StatusInternalServerError, "查询状态统计失败")
		return
	}
	_ = rows.Close()
	statusNames := make([]string, 0, len(statusMap))
	for status := range statusMap {
		statusNames = append(statusNames, status)
	}
	sort.Slice(statusNames, func(i, j int) bool {
		return statusMap[statusNames[i]].count > statusMap[statusNames[j]].count
	})
	for _, status := range statusNames {
		value := statusMap[status]
		statusStats = append(statusStats, map[string]any{"status": status, "count": value.count, "amount": round2(value.amount)})
	}

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
	location := analyticsLocation(r.URL.Query().Get("timezone_offset_minutes"))
	where, params := buildAnalyticsWhere(startDate, endDate, sess.UserID, validOrderStatuses, location)
	amountFilter := ` AND ` + analyticsAmountExpression(s.Store.Dialect, "orders.amount") + ` IS NOT NULL`
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	pageSize := atoiDefault(r.URL.Query().Get("page_size"), 500)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 500
	}
	offset := (page - 1) * pageSize
	var total int
	if err := s.Store.DB.QueryRowContext(r.Context(), `SELECT COUNT(*) FROM orders `+where+amountFilter, params...).Scan(&total); err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}

	queryParams := append(append([]any{}, params...), pageSize, offset)
	rows, err := s.Store.DB.QueryContext(r.Context(), `
		SELECT orders.order_id, COALESCE(orders.item_id,''), COALESCE(item_info.item_title, ''),
		       COALESCE(item_info.item_detail, ''), COALESCE(orders.buyer_id,''), COALESCE(orders.quantity, '1'),
		       orders.amount, COALESCE(orders.order_status,'unknown'), COALESCE(orders.cookie_id,''), orders.created_at
		FROM orders
		LEFT JOIN item_info ON item_info.cookie_id = orders.cookie_id AND item_info.item_id = orders.item_id
			`+where+amountFilter+` ORDER BY orders.created_at DESC LIMIT ? OFFSET ?`, queryParams...)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	defer rows.Close()
	out := []map[string]any{}
	for rows.Next() {
		var orderID, itemID, itemTitle, itemDetail, buyerID, quantity, amount, status, cookieID, createdAt string
		if rows.Scan(&orderID, &itemID, &itemTitle, &itemDetail, &buyerID, &quantity, &amount, &status, &cookieID, &createdAt) == nil {
			status = db.NormalizeOrderStatus(status)
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
	writeJSON(w, http.StatusOK, map[string]any{
		"orders": out, "total": total, "page": page, "page_size": pageSize,
		"truncated": offset+len(out) < total,
	})
}

// buildAnalyticsWhere 构建 WHERE 子句（user_id 经 cookies 关联过滤 + 日期 + 状态）。
// 返回 (whereClause, params)，whereClause 已含 WHERE 前缀。
func buildAnalyticsWhere(startDate, endDate string, userID int64, statuses []string, location *time.Location) (string, []any) {
	conds := []string{}
	params := []any{}
	if startDate != "" {
		conds = append(conds, "orders.created_at >= ?")
		params = append(params, analyticsDateBoundary(startDate, false, location))
	}
	if endDate != "" {
		conds = append(conds, "orders.created_at < ?")
		params = append(params, analyticsDateBoundary(endDate, true, location))
	}
	if userID != 0 {
		conds = append(conds, "EXISTS (SELECT 1 FROM cookies WHERE cookies.id = orders.cookie_id AND cookies.user_id = ?)")
		params = append(params, userID)
	}
	if len(statuses) > 0 {
		ph := strings.Repeat("?,", len(statuses))
		ph = strings.TrimSuffix(ph, ",")
		conds = append(conds, "orders.order_status IN ("+ph+")")
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

func analyticsDateBoundary(raw string, endExclusive bool, location *time.Location) string {
	if location == nil {
		location = time.Local
	}
	t, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), location)
	if err != nil {
		return raw
	}
	if endExclusive {
		t = t.AddDate(0, 0, 1)
	}
	return t.UTC().Format("2006-01-02 15:04:05")
}

func analyticsLocation(rawOffset string) *time.Location {
	offset, err := strconv.Atoi(strings.TrimSpace(rawOffset))
	if err != nil || offset < -14*60 || offset > 14*60 {
		return time.Local
	}
	return time.FixedZone("browser", offset*60)
}

func analyticsAmountExpression(dialect db.Dialect, column string) string {
	clean := `TRIM(REPLACE(REPLACE(` + column + `, '¥', ''), ',', ''))`
	switch dialect {
	case db.DialectPostgres:
		return `CASE WHEN ` + clean + ` ~ '^[0-9]+([.][0-9]+)?$' THEN CAST(` + clean + ` AS DOUBLE PRECISION) END`
	case db.DialectMySQL:
		return `CASE WHEN ` + clean + ` REGEXP '^[0-9]+([.][0-9]+)?$' THEN CAST(` + clean + ` AS DOUBLE) END`
	default:
		return `CASE WHEN ` + clean + ` GLOB '[0-9]*' AND ` + clean + ` NOT GLOB '*[^0-9.]*' AND ` + clean + ` NOT GLOB '*.*.*' AND ` + clean + ` NOT LIKE '%.' THEN CAST(` + clean + ` AS REAL) END`
	}
}

func parseAnalyticsDBTime(raw string) time.Time {
	for _, layout := range []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05Z07:00"} {
		if t, err := time.ParseInLocation(layout, strings.TrimSpace(raw), time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}

func parseAnalyticsAmount(raw string) float64 {
	raw = strings.TrimSpace(strings.NewReplacer("¥", "", ",", "").Replace(raw))
	value, _ := strconv.ParseFloat(raw, 64)
	return value
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
