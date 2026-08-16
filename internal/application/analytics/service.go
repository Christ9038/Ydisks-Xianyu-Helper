package analytics

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ValidOrderStatuses 是参与订单分析的有效状态候选集合。
var ValidOrderStatuses = []string{"pending_ship", "paid", "2", "shipped", "3", "completed", "4", "11"}

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
	// dailyMap 按用户本地日期累计订单数量和金额。
	dailyMap := make(map[string]dailyValue)
	// record 是当前订单日期聚合原始记录。
	for _, record := range dailyRecords {
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
		value.amount += parseAmount(record.Amount)
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
		daily = append(daily, DailyStats{Date: date, OrderCount: value.count, Amount: round2(value.amount)})
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
		value.amount += record.Amount
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
		statusStats = append(statusStats, StatusStats{Status: status, Count: value.count, Amount: round2(value.amount)})
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
		cityStats = append(cityStats, CityStats{City: record.City, OrderCount: record.Count, TotalAmount: round2(record.Amount)})
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
		itemStats = append(itemStats, ItemStats{ItemID: record.ItemID, OrderCount: record.Count, TotalAmount: round2(record.TotalAmount), AvgAmount: round2(record.AvgAmount)})
	}
	return OrderAnalytics{RevenueStats: RevenueStats{
		TotalOrders: revenue.TotalOrders, TotalAmount: round2(revenue.TotalAmount), AvgAmount: round2(revenue.AvgAmount),
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

// dailyValue 保存单个日期的订单数和金额累计值。
type dailyValue struct {
	// count 是当前日期的订单数量。
	count int
	// amount 是当前日期的金额累计值。
	amount float64
}

// statusValue 保存单个归一化状态的订单数和金额累计值。
type statusValue struct {
	// count 是当前状态的订单数量。
	count int
	// amount 是当前状态的金额累计值。
	amount float64
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

// parseAmount 将订单金额文本转换为数值。
func parseAmount(raw string) float64 {
	// cleaned 是移除货币符号和千分位后的金额文本。
	cleaned := strings.TrimSpace(strings.NewReplacer("¥", "", ",", "").Replace(raw))
	// value 是解析后的金额；非法金额按零处理以保持历史统计口径。
	value, _ := strconv.ParseFloat(cleaned, 64)
	return value
}

// round2 将金额按两位小数进行四舍五入。
func round2(value float64) float64 {
	return float64(int(value*100+0.5)) / 100
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
