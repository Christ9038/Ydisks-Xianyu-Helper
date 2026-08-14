package server

import (
	"net/http"
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
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// result 和 err 是订单分析应用服务返回的仪表盘摘要。
	result, err := s.analyticsApplication().DashboardStats(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "统计数据失败")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// 有效订单状态只统计以下几种。
var validOrderStatuses = []string{"pending_ship", "paid", "2", "shipped", "3", "completed", "4", "11"}

// orderAnalytics 汇总指定日期范围内的收益以及按日、状态、城市和商品分布。
func (s *Server) orderAnalytics(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// startDate、endDate 和 location 是订单分析的日期范围及时区参数。
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	location := analyticsLocation(r.URL.Query().Get("timezone_offset_minutes"))
	// where 和 params 是按用户、日期和状态归一化后的查询条件。
	where, params := buildAnalyticsWhere(startDate, endDate, sess.UserID, validOrderStatuses, location)
	// amountClean 和 amountFilter 是按数据库方言生成的金额过滤条件。
	amountClean, amountFilter := analyticsQueryAmountFilter(s.Store, "amount")
	// result 和 err 是订单分析应用服务返回的具名统计结果。
	result, err := s.analyticsApplication().OrderAnalytics(r.Context(), analyticsQuery{
		Where: where, Params: analyticsQueryParamsCopy(params), AmountClean: amountClean, AmountFilter: amountFilter, Location: location,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, analyticsErrorMessage(err))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// validOrders 有效订单明细列表（用于统计中的订单明细）。
func (s *Server) validOrders(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// startDate、endDate 和 location 是有效订单的日期范围及时区参数。
	startDate := r.URL.Query().Get("start_date")
	endDate := r.URL.Query().Get("end_date")
	location := analyticsLocation(r.URL.Query().Get("timezone_offset_minutes"))
	// where 和 params 是按用户、日期和状态归一化后的查询条件。
	where, params := buildAnalyticsWhere(startDate, endDate, sess.UserID, validOrderStatuses, location)
	// amountClean 和 amountFilter 是按数据库方言生成的金额过滤条件。
	amountClean, amountFilter := analyticsQueryAmountFilter(s.Store, "orders.amount")
	// page 和 pageSize 是已经限制在安全范围内的分页参数。
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	pageSize := atoiDefault(r.URL.Query().Get("page_size"), 500)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 500
	}
	// result 和 err 是订单分析应用服务返回的分页结果。
	result, err := s.analyticsApplication().ValidOrders(r.Context(), analyticsQuery{
		Where: where, Params: analyticsQueryParamsCopy(params), AmountClean: amountClean, AmountFilter: amountFilter,
	}, page, pageSize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, analyticsErrorMessage(err))
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// buildAnalyticsWhere 构建 WHERE 子句（user_id 经 cookies 关联过滤 + 日期 + 状态）。
// 返回 (whereClause, params)，whereClause 已含 WHERE 前缀。
func buildAnalyticsWhere(startDate, endDate string, userID int64, statuses []string, location *time.Location) (string, []any) {
	conds := []string{"orders.deleted_at IS NULL"}
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
