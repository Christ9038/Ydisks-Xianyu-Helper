package server

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"

	analyticsapp "xianyu-go/internal/application/analytics"
	"xianyu-go/internal/auth"
)

// analyticsApplication 返回当前 Server 绑定的订单分析应用服务。
func (s *Server) analyticsApplication() *analyticsapp.Service {
	return s.applicationServiceSet().analytics
}

// mountAnalyticsReal 挂载订单分析端点（仪表盘 BI 报表使用）。
func (s *Server) mountAnalyticsReal(r chi.Router) {
	r.Get("/dashboard/stats", s.dashboardStats)
	r.Get("/analytics/orders", s.orderAnalytics)
	r.Get("/analytics/orders/valid", s.validOrders)
}

// dashboardStats 返回当前登录用户的数据概览；管理员全局统计由 /admin/stats 提供。
func (s *Server) dashboardStats(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// result 和 err 是订单分析应用服务返回的仪表盘摘要及错误。
	result, err := s.analyticsApplication().DashboardStats(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "统计数据失败")
		return
	}
	writeJSON(w, http.StatusOK, dashboardStatsResponseFromApplication(result))
}

// orderAnalytics 汇总指定日期范围内的收益以及按日、状态、城市和商品分布。
func (s *Server) orderAnalytics(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// query 是从 HTTP 查询参数映射出的订单分析用例请求。
	query := analyticsapp.Query{
		UserID: sess.UserID, StartDate: r.URL.Query().Get("start_date"), EndDate: r.URL.Query().Get("end_date"),
		Location: analyticsapp.LocationFromOffset(r.URL.Query().Get("timezone_offset_minutes")),
	}
	// result 和 err 是订单分析应用服务返回的统计结果及错误。
	result, err := s.analyticsApplication().OrderAnalytics(r.Context(), query)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, analyticsapp.ErrorMessage(err))
		return
	}
	writeJSON(w, http.StatusOK, orderAnalyticsResponseFromApplication(result))
}

// validOrders 返回有效订单明细分页结果。
func (s *Server) validOrders(w http.ResponseWriter, r *http.Request) {
	// sess 是当前请求的认证会话。
	sess := auth.SessionFromContext(r.Context())
	// page 和 pageSize 是已经限制在安全范围内的分页参数。
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	// pageSize 是每页返回的最大订单数量。
	pageSize := atoiDefault(r.URL.Query().Get("page_size"), 500)
	if page < 1 {
		page = 1
	}
	if pageSize < 1 || pageSize > 500 {
		pageSize = 500
	}
	// query 是从 HTTP 查询参数映射出的有效订单用例请求。
	query := analyticsapp.Query{
		UserID: sess.UserID, StartDate: r.URL.Query().Get("start_date"), EndDate: r.URL.Query().Get("end_date"),
		Location: analyticsapp.LocationFromOffset(r.URL.Query().Get("timezone_offset_minutes")),
	}
	// result 和 err 是订单分析应用服务返回的分页结果及错误。
	result, err := s.analyticsApplication().ValidOrders(r.Context(), query, page, pageSize)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, analyticsapp.ErrorMessage(err))
		return
	}
	writeJSON(w, http.StatusOK, validOrdersResponseFromApplication(result))
}

// analyticsDateBoundary 保留日期边界测试使用的应用层转换入口。
func analyticsDateBoundary(raw string, endExclusive bool, location *time.Location) string {
	return analyticsapp.DateBoundary(raw, endExclusive, location)
}

// dashboardStatsResponseFromApplication 将应用摘要映射为 HTTP DTO。
func dashboardStatsResponseFromApplication(result analyticsapp.DashboardStats) dashboardStatsResponse {
	return dashboardStatsResponse{
		TotalCookies: result.TotalCookies, ActiveCookies: result.ActiveCookies, TotalCards: result.TotalCards,
		AvailableCardStock: result.AvailableCardStock, TotalKeywords: result.TotalKeywords, TotalOrders: result.TotalOrders,
	}
}

// orderAnalyticsResponseFromApplication 将应用统计映射为 HTTP DTO。
func orderAnalyticsResponseFromApplication(result analyticsapp.OrderAnalytics) orderAnalyticsResponse {
	// dailyStats 是应用层按日结果对应的传输 DTO 列表。
	dailyStats := make([]analyticsDailyStatsResponse, 0, len(result.DailyStats))
	// item 是当前按日统计结果。
	for _, item := range result.DailyStats {
		dailyStats = append(dailyStats, analyticsDailyStatsResponse{Date: item.Date, OrderCount: item.OrderCount, Amount: item.Amount})
	}
	// statusStats 是应用层按状态结果对应的传输 DTO 列表。
	statusStats := make([]analyticsStatusStatsResponse, 0, len(result.StatusStats))
	// item 是当前按状态统计结果。
	for _, item := range result.StatusStats {
		statusStats = append(statusStats, analyticsStatusStatsResponse{Status: item.Status, Count: item.Count, Amount: item.Amount})
	}
	// cityStats 是应用层按城市结果对应的传输 DTO 列表。
	cityStats := make([]analyticsCityStatsResponse, 0, len(result.CityStats))
	// item 是当前按城市统计结果。
	for _, item := range result.CityStats {
		cityStats = append(cityStats, analyticsCityStatsResponse{City: item.City, OrderCount: item.OrderCount, TotalAmount: item.TotalAmount})
	}
	// itemStats 是应用层按商品结果对应的传输 DTO 列表。
	itemStats := make([]analyticsItemStatsResponse, 0, len(result.ItemStats))
	// item 是当前按商品统计结果。
	for _, item := range result.ItemStats {
		itemStats = append(itemStats, analyticsItemStatsResponse{ItemID: item.ItemID, OrderCount: item.OrderCount, TotalAmount: item.TotalAmount, AvgAmount: item.AvgAmount})
	}
	return orderAnalyticsResponse{
		RevenueStats: analyticsRevenueStatsResponse{
			TotalOrders: result.RevenueStats.TotalOrders, TotalAmount: result.RevenueStats.TotalAmount,
			AvgAmount: result.RevenueStats.AvgAmount, UniqueBuyers: result.RevenueStats.UniqueBuyers, UniqueItems: result.RevenueStats.UniqueItems,
		},
		DailyStats: dailyStats, StatusStats: statusStats, CityStats: cityStats, ItemStats: itemStats,
	}
}

// validOrdersResponseFromApplication 将应用分页结果映射为 HTTP DTO。
func validOrdersResponseFromApplication(result analyticsapp.ValidOrders) validOrdersResponse {
	// orders 是应用层有效订单对应的传输 DTO 列表。
	orders := make([]validOrderResponse, 0, len(result.Orders))
	// item 是当前有效订单应用模型。
	for _, item := range result.Orders {
		orders = append(orders, validOrderResponse{
			OrderID: item.OrderID, ItemID: item.ItemID, BuyerID: item.BuyerID, ItemTitle: item.ItemTitle,
			ItemImage: item.ItemImage, Quantity: item.Quantity, Amount: item.Amount, OrderStatus: item.OrderStatus,
			Status: item.Status, CookieID: item.CookieID, CreatedAt: item.CreatedAt,
		})
	}
	return validOrdersResponse{Orders: orders, Total: result.Total, Page: result.Page, PageSize: result.PageSize, Truncated: result.Truncated}
}
