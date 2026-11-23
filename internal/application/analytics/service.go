package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/money"
)

// ValidOrderStatuses 是参与订单分析的有效状态候选集合。
var ValidOrderStatuses = []string{"pending_ship", "paid", "2", "shipped", "3", "completed", "4", "11"}

// salesStatuses 是销售统计需要纳入的订单状态候选集合（含历史数字码别名），覆盖已发货、已完成与已取消。
var salesStatuses = []string{"shipped", "3", "completed", "4", "11", "cancelled", "6", "8", "10", "12"}

// Stage 标识订单分析失败的查询阶段。
type Stage string

const (
	// StageRevenue 表示收益汇总查询失败。
	StageRevenue Stage = "revenue"
	// StageDaily 表示每日统计查询失败。
	StageDaily Stage = "daily"
	// StageStatus 表示状态统计查询失败。
	StageStatus Stage = "status"
	// StageCity 表示城市统计查询失败。
	StageCity Stage = "city"
	// StageItem 表示商品统计查询失败。
	StageItem Stage = "item"
	// StageValidCount 表示有效订单总数查询失败。
	StageValidCount Stage = "valid_count"
	// StageValidRows 表示有效订单明细查询失败。
	StageValidRows Stage = "valid_rows"
	// StageSales 表示销售统计查询失败。
	StageSales Stage = "sales"
)

// StageError 保留查询阶段并包装底层错误，供传输层映射兼容消息。
type StageError struct {
	// Stage 是失败的查询阶段。
	Stage Stage
	// Err 是底层查询错误。
	Err error
}

// Error 返回底层错误文本。
func (e *StageError) Error() string {
	if e == nil || e.Err == nil {
		return "订单分析查询失败"
	}
	return e.Err.Error()
}

// Unwrap 暴露底层查询错误以支持 errors.Is 和 errors.As。
func (e *StageError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

// NewService 构造订单分析应用服务。
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// Service 编排订单分析用例，不感知 HTTP、数据库连接或 Server。
type Service struct {
	// repository 提供订单分析所需的窄查询能力。
	repository Repository
}

// LocationFromOffset 将浏览器提交的分钟偏移转换为用户时区。
func LocationFromOffset(rawOffset string) *time.Location {
	// offset 是浏览器相对 UTC 的分钟偏移。
	offset, err := strconv.Atoi(strings.TrimSpace(rawOffset))
	if err != nil || offset < -14*60 || offset > 14*60 {
		return time.Local
	}
	return time.FixedZone("browser", offset*60)
}

// DateBoundary 将用户本地日期转换为数据库使用的 UTC 边界文本。
func DateBoundary(raw string, endExclusive bool, location *time.Location) string {
	if location == nil {
		location = time.Local
	}
	// parsed 是按用户时区解释后的日期起点。
	parsed, err := time.ParseInLocation("2006-01-02", strings.TrimSpace(raw), location)
	if err != nil {
		return raw
	}
	if endExclusive {
		parsed = parsed.AddDate(0, 0, 1)
	}
	return parsed.UTC().Format("2006-01-02 15:04:05")
}

// DashboardStats 查询用户数据概览和可用卡密库存。
func (svc *Service) DashboardStats(ctx context.Context, userID int64) (DashboardStats, error) {
	// stats 是数据库返回的用户范围计数。
	stats, err := svc.repository.DashboardStats(ctx, userID)
	if err != nil {
		return DashboardStats{}, err
	}
	// stock 是基础设施按数据卡密组内容计算出的可用库存数量。
	stock, err := svc.repository.AvailableCardStock(ctx, userID)
	if err != nil {
		return DashboardStats{}, err
	}
	stats.AvailableCardStock = stock
	return stats, nil
}

// OrderAnalytics 查询收益及按日、状态、城市和商品维度聚合的订单分析结果。
func (svc *Service) OrderAnalytics(ctx context.Context, query Query) (OrderAnalytics, error) {
	// filter 是按用户本地日期转换后的持久化查询条件。
	filter := query.filter()
	// revenue 是收益汇总查询结果。
	revenue, err := svc.repository.QueryRevenue(ctx, filter)
	if err != nil {
		return OrderAnalytics{}, &StageError{Stage: StageRevenue, Err: err}
	}
	// dailyRecords 是按订单返回的日期聚合原始记录。
	dailyRecords, err := svc.repository.QueryDaily(ctx, filter)
	if err != nil {
		return OrderAnalytics{}, &StageError{Stage: StageDaily, Err: err}
	}
	// dailyMap 按用户本地日期累计订单数量和金额（分）。
	dailyMap := make(map[string]dailyValue)
	// record 是当前订单日期聚合原始记录。
	for _, record := range dailyRecords {
		// cents 是当前订单金额（分）；非法或超范围金额的订单整单排除。
		cents, amountErr := money.ParseOrderAmountCents(record.Amount)
		if amountErr != nil {
			continue
		}
		// created 是解析后的订单创建时间。
		created := parseDBTime(record.CreatedAt)
		if created.IsZero() {
			continue
		}
		if query.Location != nil {
			created = created.In(query.Location)
		}
		// date 是订单在用户时区中的日期。
		date := created.Format("2006-01-02")
		// value 是当前日期的累计统计值。
		value := dailyMap[date]
		value.count++
		value.cents += cents
		dailyMap[date] = value
	}
	// daily 是排序后的按日统计结果。
	daily := make([]DailyStats, 0, len(dailyMap))
	// dates 是待输出的日期列表。
	dates := make([]string, 0, len(dailyMap))
	// date 是当前待输出的日期键。
	for date := range dailyMap {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	// date 是当前输出的日期键。
	for _, date := range dates {
		// value 是当前日期的累计统计值。
		value := dailyMap[date]
		daily = append(daily, DailyStats{Date: date, OrderCount: value.count, Amount: centsToYuan(value.cents)})
	}

	// statusRecords 是数据库按原始状态返回的聚合结果。
	statusRecords, err := svc.repository.QueryStatus(ctx, filter)
	if err != nil {
		return OrderAnalytics{}, &StageError{Stage: StageStatus, Err: err}
	}
	// statusMap 按归一化状态累计数据库聚合结果。
	statusMap := make(map[string]statusValue)
	// record 是当前原始状态聚合记录。
	for _, record := range statusRecords {
		// status 是兼容数字码转换后的状态名称。
		status := normalizeStatus(record.Status)
		// value 是当前状态的累计统计值。
		value := statusMap[status]
		value.count += record.Count
		value.cents += record.AmountCents
		statusMap[status] = value
	}
	// statusStats 是按订单数量降序排列的状态统计结果。
	statusStats := make([]StatusStats, 0, len(statusMap))
	// statusNames 是待排序的状态名称。
	statusNames := make([]string, 0, len(statusMap))
	// status 是当前待排序的归一化状态名称。
	for status := range statusMap {
		statusNames = append(statusNames, status)
	}
	sort.Slice(statusNames, func(i, j int) bool { return statusMap[statusNames[i]].count > statusMap[statusNames[j]].count })
	// status 是当前输出的归一化状态名称。
	for _, status := range statusNames {
		// value 是当前状态的累计统计值。
		value := statusMap[status]
		statusStats = append(statusStats, StatusStats{Status: status, Count: value.count, Amount: centsToYuan(value.cents)})
	}

	// cityRecords 是数据库按收货城市返回的聚合结果。
	cityRecords, err := svc.repository.QueryCity(ctx, filter)
	if err != nil {
		return OrderAnalytics{}, &StageError{Stage: StageCity, Err: err}
	}
	// cityStats 是收货城市统计结果。
	cityStats := make([]CityStats, 0, len(cityRecords))
	// record 是当前收货城市聚合记录。
	for _, record := range cityRecords {
		cityStats = append(cityStats, CityStats{City: record.City, OrderCount: record.Count, TotalAmount: centsToYuan(record.AmountCents)})
	}

	// itemRecords 是数据库按商品返回的聚合结果。
	itemRecords, err := svc.repository.QueryItem(ctx, filter)
	if err != nil {
		return OrderAnalytics{}, &StageError{Stage: StageItem, Err: err}
	}
	// itemStats 是商品排行统计结果。
	itemStats := make([]ItemStats, 0, len(itemRecords))
	// record 是当前商品聚合记录。
	for _, record := range itemRecords {
		itemStats = append(itemStats, ItemStats{ItemID: record.ItemID, OrderCount: record.Count, TotalAmount: centsToYuan(record.TotalCents), AvgAmount: centsToYuan(averageCents(record.TotalCents, record.Count))})
	}
	return OrderAnalytics{RevenueStats: RevenueStats{
		TotalOrders: revenue.TotalOrders, TotalAmount: centsToYuan(revenue.TotalCents), AvgAmount: centsToYuan(averageCents(revenue.TotalCents, revenue.TotalOrders)),
		UniqueBuyers: revenue.UniqueBuyers, UniqueItems: revenue.UniqueItems,
	}, DailyStats: daily, StatusStats: statusStats, CityStats: cityStats, ItemStats: itemStats}, nil
}

// ValidOrders 查询有效订单分页明细。
func (svc *Service) ValidOrders(ctx context.Context, query Query, page, pageSize int) (ValidOrders, error) {
	// filter 是按用户本地日期转换后的持久化查询条件。
	filter := query.filter()
	// total 是符合筛选条件的有效订单总数。
	total, err := svc.repository.CountValidOrders(ctx, filter)
	if err != nil {
		return ValidOrders{}, &StageError{Stage: StageValidCount, Err: err}
	}
	// offset 是当前分页需要跳过的记录数。
	offset := (page - 1) * pageSize
	// records 是当前分页的订单明细原始记录。
	records, err := svc.repository.ListValidOrders(ctx, filter, pageSize, offset)
	if err != nil {
		return ValidOrders{}, &StageError{Stage: StageValidRows, Err: err}
	}
	// orders 是面向传输层的有效订单明细。
	orders := make([]ValidOrder, 0, len(records))
	// record 是当前有效订单原始记录。
	for _, record := range records {
		// status 是兼容数字码转换后的状态名称。
		status := normalizeStatus(record.Status)
		orders = append(orders, ValidOrder{
			OrderID: record.OrderID, ItemID: record.ItemID, BuyerID: record.BuyerID, ItemTitle: record.ItemTitle,
			ItemImage: itemImageFromDetail(record.ItemDetail), Quantity: record.Quantity, Amount: record.Amount,
			OrderStatus: status, Status: status, CookieID: record.CookieID, CreatedAt: record.CreatedAt,
		})
	}
	return ValidOrders{Orders: orders, Total: total, Page: page, PageSize: pageSize, Truncated: offset+len(orders) < total}, nil
}

// SalesStats 查询指定时间范围内的销售额、已收货款与退货货款，三项分列展示、互不抵扣，订单按创建时间归属。
// 口径：销售额=已发货+已完成；已收货款=已完成；退货货款=已取消（用户确认已取消即已退款）。
// 金额全程以整数分累加（money.ParseOrderAmountCents），仅在输出时换算为元；总计与按日明细来自同一批逐单记录，二者必然一致。
// 金额非法或超过 money.MaxOrderAmountCents 的历史异常订单整单排除，不参与任何合计与订单数，避免脏数据污染统计。
func (svc *Service) SalesStats(ctx context.Context, query Query) (SalesStats, error) {
	// filter 是按用户本地日期转换后的持久化查询条件，状态集合限定为销售相关状态。
	filter := query.salesFilter()
	// records 是销售范围内的逐单原始记录（订单号、金额、状态、创建时间）。
	records, err := svc.repository.QuerySalesDaily(ctx, filter)
	if err != nil {
		return SalesStats{}, &StageError{Stage: StageSales, Err: err}
	}
	// total 是累计中的总计（单位：分）。
	total := salesCents{}
	// dailyMap 按用户本地日期累计三项指标与订单数。
	dailyMap := make(map[string]*salesCents)
	// seen 记录已计入的订单号，防止同一订单被重复累计。
	seen := make(map[string]struct{}, len(records))
	// record 是当前销售订单原始记录。
	for _, record := range records {
		// cents 是当前订单金额（分）；非法或超范围金额的订单整单排除。
		cents, amountErr := money.ParseOrderAmountCents(record.Amount)
		if amountErr != nil {
			continue
		}
		// duplicated 表示该订单号此前已经计入统计。
		if _, duplicated := seen[record.OrderID]; duplicated {
			continue
		}
		seen[record.OrderID] = struct{}{}
		// status 是兼容数字码转换后的归一化状态名称。
		status := normalizeStatus(record.Status)
		total.add(status, cents)
		// created 是解析后的订单创建时间；无法解析时只计入总计，不进入按日明细。
		created := parseDBTime(record.CreatedAt)
		if created.IsZero() {
			continue
		}
		if query.Location != nil {
			created = created.In(query.Location)
		}
		// date 是订单在用户时区中的日期。
		date := created.Format("2006-01-02")
		// day 是当前日期的累计统计值。
		day, ok := dailyMap[date]
		if !ok {
			day = &salesCents{}
			dailyMap[date] = day
		}
		day.add(status, cents)
	}
	// dates 是待输出的日期列表。
	dates := make([]string, 0, len(dailyMap))
	// date 是当前待输出的日期键。
	for date := range dailyMap {
		dates = append(dates, date)
	}
	sort.Strings(dates)
	// daily 是排序后的按日销售统计结果。
	daily := make([]SalesDailyStats, 0, len(dates))
	// date 是当前输出的日期键。
	for _, date := range dates {
		// day 是当前日期的累计统计值（分）。
		day := dailyMap[date]
		daily = append(daily, SalesDailyStats{
			Date:           date,
			SalesAmount:    centsToYuan(day.sales),
			ReceivedAmount: centsToYuan(day.received),
			RefundedAmount: centsToYuan(day.refunded),
			OrderCount:     day.orders,
		})
	}
	return SalesStats{
		SalesAmount: centsToYuan(total.sales), SalesOrders: total.salesOrders,
		ReceivedAmount: centsToYuan(total.received), ReceivedOrders: total.receivedOrders,
		RefundedAmount: centsToYuan(total.refunded), RefundedOrders: total.refundedOrders,
		Daily: daily,
	}, nil
}

// salesCents 是销售统计的整数分累加器，金额单位均为分。
type salesCents struct {
	// sales 是已发货与已完成订单的金额合计，salesOrders 是对应订单数。
	sales       int64
	salesOrders int
	// received 是已完成订单的金额合计，receivedOrders 是对应订单数。
	received       int64
	receivedOrders int
	// refunded 是已取消订单的金额合计，refundedOrders 是对应订单数。
	refunded       int64
	refundedOrders int
	// orders 是参与统计的订单总数（含三类状态）。
	orders int
}

// add 把一笔金额为 cents 分、归一化状态为 status 的订单累加进对应指标。
func (acc *salesCents) add(status string, cents int64) {
	acc.orders++
	switch status {
	case "shipped", "completed":
		acc.sales += cents
		acc.salesOrders++
		if status == "completed" {
			acc.received += cents
			acc.receivedOrders++
		}
	case "cancelled":
		acc.refunded += cents
		acc.refundedOrders++
	}
}

// centsToYuan 把整数分换算为元；两位小数的分值经 float64 往返后不丢精度，仅用于对外 DTO 输出。
func centsToYuan(cents int64) float64 {
	return float64(cents) / 100
}

// ErrorMessage 将分析失败阶段映射为原有 HTTP 错误消息。
func ErrorMessage(err error) string {
	// stageErr 是带查询阶段的应用服务错误。
	var stageErr *StageError
	if !errors.As(err, &stageErr) {
		return "查询失败"
	}
	switch stageErr.Stage {
	case StageRevenue:
		return "查询收益统计失败"
	case StageDaily:
		return "查询每日统计失败"
	case StageStatus:
		return "查询状态统计失败"
	case StageCity:
		return "查询城市统计失败"
	case StageItem:
		return "查询商品统计失败"
	case StageSales:
		return "查询销售统计失败"
	default:
		return "查询失败"
	}
}

// filter 将用户本地日期查询转换为数据库 UTC 范围并复制状态集合。
func (query Query) filter() Filter {
	// statuses 是固定有效状态的独立副本，避免调用方修改全局集合。
	statuses := append([]string(nil), ValidOrderStatuses...)
	return Filter{UserID: query.UserID, StartAt: dateBoundary(query.StartDate, false, query.Location), EndBefore: dateBoundary(query.EndDate, true, query.Location), Statuses: statuses}
}

// salesFilter 将用户本地日期查询转换为数据库 UTC 范围，状态集合限定为销售相关状态（已发货/已完成/已取消及其数字码别名）。
func (query Query) salesFilter() Filter {
	// statuses 是销售统计状态的独立副本，避免调用方修改全局集合。
	statuses := append([]string(nil), salesStatuses...)
	return Filter{UserID: query.UserID, StartAt: dateBoundary(query.StartDate, false, query.Location), EndBefore: dateBoundary(query.EndDate, true, query.Location), Statuses: statuses}
}

// dailyValue 保存单个日期的订单数和金额累计值。
type dailyValue struct {
	// count 是当前日期的订单数量。
	count int
	// cents 是当前日期的金额累计值（单位：分）。
	cents int64
}

// statusValue 保存单个归一化状态的订单数和金额累计值。
type statusValue struct {
	// count 是当前状态的订单数量。
	count int
	// cents 是当前状态的金额累计值（单位：分）。
	cents int64
}

// dateBoundary 是内部日期边界转换实现。
func dateBoundary(raw string, endExclusive bool, location *time.Location) string {
	return DateBoundary(raw, endExclusive, location)
}

// parseDBTime 将数据库常见时间文本解析为 UTC 时间。
func parseDBTime(raw string) time.Time {
	// layouts 是支持的数据库时间格式集合。
	layouts := []string{"2006-01-02 15:04:05", time.RFC3339, "2006-01-02T15:04:05Z07:00"}
	// layout 是当前尝试的数据库时间格式。
	for _, layout := range layouts {
		// parsed 是当前格式尝试得到的时间值。
		parsed, err := time.ParseInLocation(layout, strings.TrimSpace(raw), time.UTC)
		if err == nil {
			return parsed
		}
	}
	return time.Time{}
}

// averageCents 返回 totalCents 在 count 笔订单上的平均分值（整数除法四舍五入）；count 非正时返回 0。
func averageCents(totalCents int64, count int) int64 {
	if count <= 0 {
		return 0
	}
	// divisor 是订单数的整数分母。
	divisor := int64(count)
	return (totalCents + divisor/2) / divisor
}

// normalizeStatus 将历史数字状态码归一为业务状态名称。
func normalizeStatus(status string) string {
	// normalized 是历史平台数字状态到业务名称的兼容映射。
	normalized, ok := map[string]string{
		"paid": "pending_ship", "1": "processing", "2": "pending_ship", "3": "shipped", "4": "completed",
		"5": "refunding", "6": "cancelled", "7": "refunding", "8": "cancelled", "9": "refunding", "10": "cancelled", "11": "completed", "12": "cancelled",
	}[status]
	if ok {
		return normalized
	}
	if status == "" {
		return "unknown"
	}
	return status
}

// itemImageFromDetail 从商品详情 JSON 中提取主图地址。
func itemImageFromDetail(detail string) string {
	if detail == "" {
		return ""
	}
	// payload 是商品详情的动态 JSON 对象。
	var payload map[string]any
	if // err 是商品详情 JSON 解析错误。
	err := json.Unmarshal([]byte(detail), &payload); err != nil {
		return ""
	}
	// picture 是商品详情中的图片信息对象。
	if picture, ok := payload["pic_info"].(map[string]any); ok {
		if // url、ok 是图片地址及其类型判断结果。
		url, ok := picture["picUrl"].(string); ok {
			return url
		}
	}
	if // url、ok 是兼容图片地址及其类型判断结果。
	url, ok := payload["item_image"].(string); ok {
		return url
	}
	return ""
}
