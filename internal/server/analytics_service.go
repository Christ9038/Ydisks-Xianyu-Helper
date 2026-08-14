package server

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// analyticsQuery 描述订单分析查询已经归一化的数据库过滤条件。
type analyticsQuery struct {
	// Where 是包含 WHERE 前缀的订单过滤子句。
	Where string
	// Params 是 Where 对应的数据库参数。
	Params []any
	// AmountClean 是按数据库方言生成的金额表达式。
	AmountClean string
	// AmountFilter 是排除非法金额后的附加条件。
	AmountFilter string
	// Location 是用户选择的本地时区。
	Location *time.Location
}

// analyticsStage 标识订单分析失败的查询阶段。
type analyticsStage string

const (
	// analyticsStageRevenue 表示收益汇总查询失败。
	analyticsStageRevenue analyticsStage = "revenue"
	// analyticsStageDaily 表示每日统计查询失败。
	analyticsStageDaily analyticsStage = "daily"
	// analyticsStageStatus 表示状态统计查询失败。
	analyticsStageStatus analyticsStage = "status"
	// analyticsStageCity 表示城市统计查询失败。
	analyticsStageCity analyticsStage = "city"
	// analyticsStageItem 表示商品统计查询失败。
	analyticsStageItem analyticsStage = "item"
	// analyticsStageValidCount 表示有效订单总数查询失败。
	analyticsStageValidCount analyticsStage = "valid_count"
	// analyticsStageValidRows 表示有效订单明细查询失败。
	analyticsStageValidRows analyticsStage = "valid_rows"
)

// analyticsServiceError 保留订单分析失败阶段，供 HTTP 层映射兼容错误消息。
type analyticsServiceError struct {
	// stage 是失败的查询阶段。
	stage analyticsStage
	// err 是底层数据库错误。
	err error
}

// Error 返回订单分析错误文本。
func (e *analyticsServiceError) Error() string { return e.err.Error() }

// Unwrap 暴露底层数据库错误。
func (e *analyticsServiceError) Unwrap() error { return e.err }

// analyticsService 承载订单分析的数据库编排，不依赖 HTTP 请求或响应对象。
type analyticsService struct {
	// server 提供订单仓储和数据库方言依赖。
	server *Server
}

// DashboardStats 查询当前用户的数据概览和可用卡密库存。
func (svc *analyticsService) DashboardStats(ctx context.Context, userID int64) (dashboardStatsResponse, error) {
	// s 是当前订单分析应用服务依赖的 Server。
	s := svc.server
	// counts 保存仪表盘各项计数。
	counts := map[string]int64{"total_cookies": 0, "active_cookies": 0, "total_cards": 0, "available_card_stock": 0, "total_keywords": 0, "total_orders": 0}
	// queries 保存用户范围内的固定统计查询。
	queries := []struct {
		query string
		key   string
	}{
		{`SELECT COUNT(*) FROM cookies WHERE user_id=?`, "total_cookies"},
		{`SELECT COUNT(*) FROM cards WHERE user_id=?`, "total_cards"},
		{`SELECT COUNT(*) FROM keywords k WHERE EXISTS (SELECT 1 FROM cookies c WHERE c.id=k.cookie_id AND c.user_id=?)`, "total_keywords"},
		{`SELECT COUNT(*) FROM orders o WHERE o.deleted_at IS NULL AND EXISTS (SELECT 1 FROM cookies c WHERE c.id=o.cookie_id AND c.user_id=?)`, "total_orders"},
	}
	// item 是当前固定统计查询。
	for _, item := range queries {
		// count 是当前统计项的数据库结果。
		var count int64
		// err 是当前统计项查询错误。
		if err := s.Store.Analytics.QueryRowContext(ctx, item.query, userID).Scan(&count); err != nil {
			return dashboardStatsResponse{}, err
		}
		counts[item.key] = count
	}
	// activeCookies 是没有明确禁用记录的账号数量。
	var activeCookies int64
	// err 是活跃账号统计查询错误。
	if err := s.Store.Analytics.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cookies c WHERE c.user_id=?
		  AND NOT EXISTS (SELECT 1 FROM cookie_status cs WHERE cs.cookie_id=c.id AND cs.enabled=0)
	`, userID).Scan(&activeCookies); err != nil {
		return dashboardStatsResponse{}, err
	}
	counts["active_cookies"] = activeCookies
	// cards 和 err 是当前用户的卡密组列表及查询错误。
	cards, err := s.Store.Cards.AllForUser(ctx, userID)
	if err != nil {
		return dashboardStatsResponse{}, err
	}
	// card 是当前遍历到的卡密组。
	for _, card := range cards {
		if !card.Enabled || card.Type != "data" {
			continue
		}
		// line 是卡密组内容中的一行卡密。
		for _, line := range strings.Split(strings.ReplaceAll(card.DataContent, "\r\n", "\n"), "\n") {
			if strings.TrimSpace(line) != "" {
				counts["available_card_stock"]++
			}
		}
	}
	return dashboardStatsResponse{TotalCookies: counts["total_cookies"], ActiveCookies: counts["active_cookies"], TotalCards: counts["total_cards"], AvailableCardStock: counts["available_card_stock"], TotalKeywords: counts["total_keywords"], TotalOrders: counts["total_orders"]}, nil
}

// analyticsApplication 返回当前 Server 绑定的订单分析应用服务。
func (s *Server) analyticsApplication() *analyticsService {
	return s.applicationServiceSet().analytics
}

// OrderAnalytics 查询收益及按日、状态、城市和商品维度聚合的订单分析结果。
func (svc *analyticsService) OrderAnalytics(ctx context.Context, query analyticsQuery) (orderAnalyticsResponse, error) {
	// s 是当前订单分析应用服务依赖的 Server。
	s := svc.server
	// amountFilter 是已经排除非法金额的查询条件。
	amountFilter := query.AmountFilter
	// revenue 保存订单收益汇总结果。
	var revenue analyticsRevenueStatsResponse
	// err 是收益汇总查询错误。
	if err := s.Store.Analytics.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT order_id), COALESCE(SUM(`+query.AmountClean+`),0),
		       COALESCE(AVG(`+query.AmountClean+`),0), COUNT(DISTINCT buyer_id), COUNT(DISTINCT item_id)
		FROM orders `+query.Where+amountFilter, query.Params...).Scan(
		&revenue.TotalOrders, &revenue.TotalAmount, &revenue.AvgAmount, &revenue.UniqueBuyers, &revenue.UniqueItems); err != nil {
		return orderAnalyticsResponse{}, &analyticsServiceError{stage: analyticsStageRevenue, err: err}
	}

	// daily 保存按用户本地日期聚合的统计结果。
	daily := make([]analyticsDailyStatsResponse, 0)
	// rows 和 err 是每日统计查询结果集及错误。
	rows, err := s.Store.Analytics.QueryContext(ctx, `
		SELECT order_id,amount,created_at FROM orders `+query.Where+amountFilter, query.Params...)
	if err != nil {
		return orderAnalyticsResponse{}, &analyticsServiceError{stage: analyticsStageDaily, err: err}
	}
	// dailyValue 保存同一天的订单数和金额累计值。
	type dailyValue struct {
		count  int
		amount float64
	}
	// dailyMap 按日期聚合订单数据，避免依赖数据库方言的日期函数。
	dailyMap := map[string]dailyValue{}
	for rows.Next() {
		// orderID、amountRaw 和 createdAt 是当前订单的原始字段。
		var orderID, amountRaw, createdAt string
		if rows.Scan(&orderID, &amountRaw, &createdAt) != nil {
			continue
		}
		// created 是转换后的订单创建时间。
		created := parseAnalyticsDBTime(createdAt)
		if created.IsZero() {
			continue
		}
		// date 是订单在用户时区中的日期。
		if query.Location != nil {
			created = created.In(query.Location)
		}
		// date 是订单在用户时区中的日期。
		date := created.Format("2006-01-02")
		// value 是当前日期的累计统计值。
		value := dailyMap[date]
		value.count++
		value.amount += parseAnalyticsAmount(amountRaw)
		dailyMap[date] = value
	}
	// err 是每日统计游标迭代错误。
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return orderAnalyticsResponse{}, &analyticsServiceError{stage: analyticsStageDaily, err: err}
	}
	_ = rows.Close()
	// dates 是排序后的日期列表。
	dates := make([]string, 0, len(dailyMap))
	// date 是当前待排序的日期。
	for date := range dailyMap {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	// date 是当前输出日期。
	for _, date := range dates {
		// value 是当前日期的累计统计值。
		value := dailyMap[date]
		daily = append(daily, analyticsDailyStatsResponse{Date: date, OrderCount: value.count, Amount: round2(value.amount)})
	}

	// statusStats 保存按订单状态聚合的结果。
	statusStats := make([]analyticsStatusStatsResponse, 0)
	// statusValue 保存单个状态的累计统计值。
	type statusValue struct {
		count  int
		amount float64
	}
	// statusMap 按归一化状态聚合订单数据。
	statusMap := map[string]statusValue{}
	// rows 和 err 是状态统计查询结果集及错误。
	rows, err = s.Store.Analytics.QueryContext(ctx, `
		SELECT COALESCE(order_status,'unknown'), COUNT(DISTINCT order_id), COALESCE(SUM(`+query.AmountClean+`),0)
		FROM orders `+query.Where+amountFilter+`
		GROUP BY order_status ORDER BY COUNT(DISTINCT order_id) DESC`, query.Params...)
	if err != nil {
		return orderAnalyticsResponse{}, &analyticsServiceError{stage: analyticsStageStatus, err: err}
	}
	for rows.Next() {
		// status、count 和 amount 是当前状态的数据库聚合值。
		var status string
		// count 和 amount 是当前状态的订单数和金额。
		var count int
		// amount 是当前状态的订单金额。
		var amount float64
		if rows.Scan(&status, &count, &amount) == nil {
			status = db.NormalizeOrderStatus(status)
			// value 是当前状态的累计统计值。
			value := statusMap[status]
			value.count += count
			value.amount += amount
			statusMap[status] = value
		}
	}
	// err 是状态统计游标迭代错误。
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return orderAnalyticsResponse{}, &analyticsServiceError{stage: analyticsStageStatus, err: err}
	}
	_ = rows.Close()
	// statusNames 是按订单数量降序排列的状态名称。
	statusNames := make([]string, 0, len(statusMap))
	// status 是当前待排序的状态名称。
	for status := range statusMap {
		statusNames = append(statusNames, status)
	}
	sort.Slice(statusNames, func(i, j int) bool { return statusMap[statusNames[i]].count > statusMap[statusNames[j]].count })
	// status 是当前输出状态。
	for _, status := range statusNames {
		// value 是当前状态的累计统计值。
		value := statusMap[status]
		statusStats = append(statusStats, analyticsStatusStatsResponse{Status: status, Count: value.count, Amount: round2(value.amount)})
	}

	// cityStats 保存按收货城市聚合的结果。
	cityStats := make([]analyticsCityStatsResponse, 0)
	// rows 和 err 是城市统计查询结果集及错误。
	rows, err = s.Store.Analytics.QueryContext(ctx, `
		SELECT receiver_city, COUNT(DISTINCT order_id), COALESCE(SUM(`+query.AmountClean+`),0)
		FROM orders `+query.Where+amountFilter+`
		  AND receiver_city IS NOT NULL AND receiver_city != ''
		GROUP BY receiver_city ORDER BY COUNT(DISTINCT order_id) DESC LIMIT 50`, query.Params...)
	if err != nil {
		return orderAnalyticsResponse{}, &analyticsServiceError{stage: analyticsStageCity, err: err}
	}
	for rows.Next() {
		// city、count 和 amount 是当前城市的数据库聚合值。
		var city string
		// count 和 amount 是当前城市的订单数和金额。
		var count int
		// amount 是当前城市的订单金额。
		var amount float64
		if rows.Scan(&city, &count, &amount) == nil {
			cityStats = append(cityStats, analyticsCityStatsResponse{City: city, OrderCount: count, TotalAmount: round2(amount)})
		}
	}
	// err 是城市统计游标迭代错误。
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return orderAnalyticsResponse{}, &analyticsServiceError{stage: analyticsStageCity, err: err}
	}
	_ = rows.Close()

	// itemStats 保存按商品标识聚合的排行结果。
	itemStats := make([]analyticsItemStatsResponse, 0)
	// rows 和 err 是商品统计查询结果集及错误。
	rows, err = s.Store.Analytics.QueryContext(ctx, `
		SELECT item_id, COUNT(DISTINCT order_id), COALESCE(SUM(`+query.AmountClean+`),0), COALESCE(AVG(`+query.AmountClean+`),0)
		FROM orders `+query.Where+amountFilter+`
		  AND item_id IS NOT NULL AND item_id != ''
		GROUP BY item_id ORDER BY COUNT(DISTINCT order_id) DESC LIMIT 20`, query.Params...)
	if err != nil {
		return orderAnalyticsResponse{}, &analyticsServiceError{stage: analyticsStageItem, err: err}
	}
	for rows.Next() {
		// itemID、count、total 和 average 是当前商品的数据库聚合值。
		var itemID string
		// count、total 和 average 是当前商品的订单数、总金额和平均金额。
		var count int
		// total 和 average 是当前商品的总金额和平均金额。
		var total, average float64
		if rows.Scan(&itemID, &count, &total, &average) == nil {
			itemStats = append(itemStats, analyticsItemStatsResponse{ItemID: itemID, OrderCount: count, TotalAmount: round2(total), AvgAmount: round2(average)})
		}
	}
	// err 是商品统计游标迭代错误。
	if err := rows.Err(); err != nil {
		_ = rows.Close()
		return orderAnalyticsResponse{}, &analyticsServiceError{stage: analyticsStageItem, err: err}
	}
	_ = rows.Close()
	return orderAnalyticsResponse{
		RevenueStats: analyticsRevenueStatsResponse{
			TotalOrders: revenue.TotalOrders, TotalAmount: round2(revenue.TotalAmount), AvgAmount: round2(revenue.AvgAmount),
			UniqueBuyers: revenue.UniqueBuyers, UniqueItems: revenue.UniqueItems,
		},
		DailyStats: daily, StatusStats: statusStats, CityStats: cityStats, ItemStats: itemStats,
	}, nil
}

// ValidOrders 查询有效订单分页明细。
func (svc *analyticsService) ValidOrders(ctx context.Context, query analyticsQuery, page, pageSize int) (validOrdersResponse, error) {
	// s 是当前订单分析应用服务依赖的 Server。
	s := svc.server
	// amountFilter 是已经排除非法金额的查询条件。
	amountFilter := query.AmountFilter
	// total 是符合筛选条件的订单总数。
	var total int
	// err 是有效订单总数查询错误。
	if err := s.Store.Analytics.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders `+query.Where+amountFilter, query.Params...).Scan(&total); err != nil {
		return validOrdersResponse{}, &analyticsServiceError{stage: analyticsStageValidCount, err: err}
	}
	// queryParams 是分页查询附加的 LIMIT/OFFSET 参数。
	queryParams := append(append([]any{}, query.Params...), pageSize, (page-1)*pageSize)
	// rows 和 err 是有效订单明细查询结果集及错误。
	rows, err := s.Store.Analytics.QueryContext(ctx, `
		SELECT orders.order_id, COALESCE(orders.item_id,''), COALESCE(item_info.item_title,''),
		       COALESCE(item_info.item_detail,''), COALESCE(orders.buyer_id,''), COALESCE(orders.quantity,'1'),
		       orders.amount, COALESCE(orders.order_status,'unknown'), COALESCE(orders.cookie_id,''), orders.created_at
		FROM orders LEFT JOIN item_info ON item_info.cookie_id=orders.cookie_id AND item_info.item_id=orders.item_id
		`+query.Where+amountFilter+` ORDER BY orders.created_at DESC LIMIT ? OFFSET ?`, queryParams...)
	if err != nil {
		return validOrdersResponse{}, &analyticsServiceError{stage: analyticsStageValidRows, err: err}
	}
	defer rows.Close()
	// out 是有效订单响应列表。
	out := make([]validOrderResponse, 0)
	for rows.Next() {
		// orderID、itemID、itemTitle、itemDetail、buyerID、quantity、amount、status、cookieID 和 createdAt 是订单字段。
		var orderID, itemID, itemTitle, itemDetail, buyerID, quantity, amount, status, cookieID, createdAt string
		if rows.Scan(&orderID, &itemID, &itemTitle, &itemDetail, &buyerID, &quantity, &amount, &status, &cookieID, &createdAt) == nil {
			status = db.NormalizeOrderStatus(status)
			out = append(out, validOrderResponse{OrderID: orderID, ItemID: itemID, BuyerID: buyerID, ItemTitle: itemTitle, ItemImage: itemImageFromDetail(itemDetail), Quantity: quantity, Amount: amount, OrderStatus: status, Status: status, CookieID: cookieID, CreatedAt: createdAt})
		}
	}
	// err 是有效订单游标迭代错误。
	if err := rows.Err(); err != nil {
		return validOrdersResponse{}, &analyticsServiceError{stage: analyticsStageValidRows, err: err}
	}
	return validOrdersResponse{Orders: out, Total: total, Page: page, PageSize: pageSize, Truncated: (page-1)*pageSize+len(out) < total}, nil
}

// analyticsStageOf 读取分析服务错误阶段。
func analyticsStageOf(err error) (analyticsStage, bool) {
	// serviceErr 是带查询阶段的应用服务错误。
	var serviceErr *analyticsServiceError
	if !errors.As(err, &serviceErr) {
		return "", false
	}
	return serviceErr.stage, true
}

// analyticsErrorMessage 将分析服务阶段映射为原有 HTTP 错误消息。
func analyticsErrorMessage(err error) string {
	// stage 是应用服务错误对应的查询阶段。
	switch stage, _ := analyticsStageOf(err); stage {
	case analyticsStageRevenue:
		return "查询收益统计失败"
	case analyticsStageDaily:
		return "查询每日统计失败"
	case analyticsStageStatus:
		return "查询状态统计失败"
	case analyticsStageCity:
		return "查询城市统计失败"
	case analyticsStageItem:
		return "查询商品统计失败"
	default:
		return "查询失败"
	}
}

// analyticsQueryAmountFilter 根据订单分析查询构造金额过滤条件。
func analyticsQueryAmountFilter(store *db.Store, column string) (string, string) {
	// clean 是按数据库方言清洗后的金额表达式。
	clean := analyticsAmountExpression(store.Dialect, column)
	return clean, " AND " + clean + " IS NOT NULL"
}

// analyticsQueryParamsCopy 复制查询参数，避免服务层修改 handler 持有的切片。
func analyticsQueryParamsCopy(params []any) []any {
	return append([]any(nil), params...)
}
