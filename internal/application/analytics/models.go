// Package analytics 定义订单分析用例、纯查询模型和消费者侧持久化 Port。
// 本包不依赖数据库、HTTP、平台协议或 Server 实现。
package analytics

import (
	"context"
	"time"
)

// Query 是订单分析用例使用的用户范围、日期和时区条件。
type Query struct {
	// UserID 是当前登录用户的本地身份标识。
	UserID int64
	// StartDate 是用户本地日期范围的起始日期，格式为 YYYY-MM-DD。
	StartDate string
	// EndDate 是用户本地日期范围的结束日期，格式为 YYYY-MM-DD。
	EndDate string
	// Location 是用于日期边界和按日聚合的用户本地时区。
	Location *time.Location
}

// Filter 是持久化查询已经转换后的用户、UTC 日期和有效状态条件。
type Filter struct {
	// UserID 是订单所属账号对应的用户身份标识。
	UserID int64
	// StartAt 是包含边界的 UTC 起始时间文本；为空表示不限制起始时间。
	StartAt string
	// EndBefore 是排除边界的 UTC 结束时间文本；为空表示不限制结束时间。
	EndBefore string
	// Statuses 是允许参与统计的订单状态候选集合。
	Statuses []string
}

// DashboardStats 是用户仪表盘所需的非敏感统计摘要。
type DashboardStats struct {
	// TotalCookies 是用户账号总数。
	TotalCookies int64
	// ActiveCookies 是没有明确禁用记录的账号数。
	ActiveCookies int64
	// TotalCards 是用户卡密组总数。
	TotalCards int64
	// AvailableCardStock 是启用数据卡密组中的非空卡密行数。
	AvailableCardStock int64
	// TotalKeywords 是用户关键词规则总数。
	TotalKeywords int64
	// TotalOrders 是用户未删除订单总数。
	TotalOrders int64
}

// RevenueStats 是订单收益聚合结果。
type RevenueStats struct {
	// TotalOrders 是统计范围内的去重订单数。
	TotalOrders int
	// TotalAmount 是统计范围内的订单总金额。
	TotalAmount float64
	// AvgAmount 是统计范围内的订单平均金额。
	AvgAmount float64
	// UniqueBuyers 是统计范围内的去重买家数。
	UniqueBuyers int
	// UniqueItems 是统计范围内的去重商品数。
	UniqueItems int
}

// DailyRecord 是按订单返回的日期聚合原始记录。
type DailyRecord struct {
	// OrderID 是用于保留原有订单计数口径的订单标识。
	OrderID string
	// Amount 是数据库中的订单金额文本。
	Amount string
	// CreatedAt 是数据库中的订单创建时间文本。
	CreatedAt string
}

// RevenueRecord 是数据库返回的收益汇总原始记录，金额以整数分表示。
type RevenueRecord struct {
	// TotalOrders 是统计范围内的去重订单数。
	TotalOrders int
	// TotalCents 是统计范围内的订单金额合计（单位：分）。
	TotalCents int64
	// UniqueBuyers 是统计范围内的去重买家数。
	UniqueBuyers int
	// UniqueItems 是统计范围内的去重商品数。
	UniqueItems int
}

// StatusRecord 是数据库按原始状态聚合的结果。
type StatusRecord struct {
	// Status 是持久化的订单状态文本或数字码。
	Status string
	// Count 是该原始状态对应的去重订单数。
	Count int
	// AmountCents 是该原始状态对应的金额合计（单位：分）。
	AmountCents int64
}

// CityRecord 是数据库按收货城市聚合的结果。
type CityRecord struct {
	// City 是收货城市名称。
	City string
	// Count 是该城市的去重订单数。
	Count int
	// AmountCents 是该城市的金额合计（单位：分）。
	AmountCents int64
}

// ItemRecord 是数据库按商品标识聚合的结果。
type ItemRecord struct {
	// ItemID 是平台商品标识。
	ItemID string
	// Count 是该商品的去重订单数。
	Count int
	// TotalCents 是该商品的金额合计（单位：分）；平均金额由应用层按整数分计算。
	TotalCents int64
}

// ValidOrderRecord 是有效订单明细所需的非敏感字段。
type ValidOrderRecord struct {
	// OrderID 是平台订单标识。
	OrderID string
	// ItemID 是平台商品标识。
	ItemID string
	// ItemTitle 是本地商品标题。
	ItemTitle string
	// ItemDetail 是本地商品详情 JSON，用于提取主图地址。
	ItemDetail string
	// BuyerID 是买家平台标识。
	BuyerID string
	// Quantity 是订单数量文本。
	Quantity string
	// Amount 是订单金额文本。
	Amount string
	// Status 是持久化的订单状态文本或数字码。
	Status string
	// CookieID 是订单所属账号标识。
	CookieID string
	// CreatedAt 是订单创建时间文本。
	CreatedAt string
}

// OrderAnalytics 是订单分析用例的完整结果模型。
type OrderAnalytics struct {
	// RevenueStats 是收益汇总。
	RevenueStats RevenueStats
	// DailyStats 是按用户本地日期聚合的结果。
	DailyStats []DailyStats
	// StatusStats 是按归一化订单状态聚合的结果。
	StatusStats []StatusStats
	// CityStats 是按收货城市聚合的结果。
	CityStats []CityStats
	// ItemStats 是按商品标识聚合的结果。
	ItemStats []ItemStats
}

// DailyStats 是单个本地日期的订单聚合结果。
type DailyStats struct {
	// Date 是用户本地日期。
	Date string
	// OrderCount 是该日期的订单数。
	OrderCount int
	// Amount 是该日期的金额合计。
	Amount float64
}

// StatusStats 是单个归一化状态的订单聚合结果。
type StatusStats struct {
	// Status 是归一化后的状态名称。
	Status string
	// Count 是该状态的订单数。
	Count int
	// Amount 是该状态的金额合计。
	Amount float64
}

// CityStats 是单个收货城市的订单聚合结果。
type CityStats struct {
	// City 是收货城市名称。
	City string
	// OrderCount 是该城市的订单数。
	OrderCount int
	// TotalAmount 是该城市的金额合计。
	TotalAmount float64
}

// ItemStats 是单个商品的订单聚合结果。
type ItemStats struct {
	// ItemID 是平台商品标识。
	ItemID string
	// OrderCount 是该商品的订单数。
	OrderCount int
	// TotalAmount 是该商品的金额合计。
	TotalAmount float64
	// AvgAmount 是该商品的平均金额。
	AvgAmount float64
}

// SalesStats 是按时间范围统计销售额、已收货款与退货货款的用例结果。
// 口径：销售额含已发货与已完成订单；已收货款仅含已完成订单；退货货款仅含已取消订单（用户确认已取消即已退款）。
// 三项分列展示，互不抵扣；订单按创建时间归入统计区间。
type SalesStats struct {
	// SalesAmount 是销售额合计（元），包含已发货与已完成订单的金额。
	SalesAmount float64
	// SalesOrders 是计入销售额的订单数。
	SalesOrders int
	// ReceivedAmount 是已收货款合计（元），仅包含已完成订单的金额。
	ReceivedAmount float64
	// ReceivedOrders 是计入已收货款的订单数。
	ReceivedOrders int
	// RefundedAmount 是退货货款合计（元），仅包含已取消订单的金额。
	RefundedAmount float64
	// RefundedOrders 是计入退货货款的订单数。
	RefundedOrders int
	// Daily 是按用户本地日期聚合的销售额/已收货款/退货货款明细。
	Daily []SalesDailyStats
}

// SalesDailyStats 是单个本地日期的销售聚合结果。
type SalesDailyStats struct {
	// Date 是用户本地日期。
	Date string
	// SalesAmount 是当天销售额合计（元）。
	SalesAmount float64
	// ReceivedAmount 是当天已收货款合计（元）。
	ReceivedAmount float64
	// RefundedAmount 是当天退货货款合计（元）。
	RefundedAmount float64
	// OrderCount 是当天参与统计的订单数。
	OrderCount int
}

// ValidOrders 是有效订单分页结果。
type ValidOrders struct {
	// Orders 是当前页的有效订单明细。
	Orders []ValidOrder
	// Total 是筛选条件下的有效订单总数。
	Total int
	// Page 是当前页码。
	Page int
	// PageSize 是当前页大小。
	PageSize int
	// Truncated 表示结果是否还有未返回的订单。
	Truncated bool
}

// ValidOrder 是面向 HTTP 映射的有效订单明细模型。
type ValidOrder struct {
	// OrderID 是平台订单标识。
	OrderID string
	// ItemID 是平台商品标识。
	ItemID string
	// BuyerID 是买家平台标识。
	BuyerID string
	// ItemTitle 是本地商品标题。
	ItemTitle string
	// ItemImage 是从商品详情提取的主图地址。
	ItemImage string
	// Quantity 是订单数量文本。
	Quantity string
	// Amount 是订单金额文本。
	Amount string
	// OrderStatus 是兼容字段，值为归一化状态名称。
	OrderStatus string
	// Status 是归一化状态名称。
	Status string
	// CookieID 是订单所属账号标识。
	CookieID string
	// CreatedAt 是订单创建时间文本。
	CreatedAt string
}

// Repository 定义订单分析用例需要的最小查询能力。
type Repository interface {
	// DashboardStats 返回用户范围内的仪表盘计数。
	DashboardStats(ctx context.Context, userID int64) (DashboardStats, error)
	// AvailableCardStock 返回用户启用数据卡密组中的可用卡密行数。
	AvailableCardStock(ctx context.Context, userID int64) (int64, error)
	// QueryRevenue 返回收益汇总原始记录（金额为整数分）。
	QueryRevenue(ctx context.Context, filter Filter) (RevenueRecord, error)
	// QueryDaily 返回订单日期聚合所需的原始记录。
	QueryDaily(ctx context.Context, filter Filter) ([]DailyRecord, error)
	// QueryStatus 返回订单状态聚合结果。
	QueryStatus(ctx context.Context, filter Filter) ([]StatusRecord, error)
	// QueryCity 返回收货城市聚合结果。
	QueryCity(ctx context.Context, filter Filter) ([]CityRecord, error)
	// QueryItem 返回商品聚合结果。
	QueryItem(ctx context.Context, filter Filter) ([]ItemRecord, error)
	// CountValidOrders 返回有效订单总数。
	CountValidOrders(ctx context.Context, filter Filter) (int, error)
	// ListValidOrders 返回有效订单分页明细。
	ListValidOrders(ctx context.Context, filter Filter, limit, offset int) ([]ValidOrderRecord, error)
	// QuerySalesDaily 返回销售统计所需的逐单原始记录（订单号、金额、状态、创建时间）；filter.Statuses 由调用方传入销售相关状态集合。
	QuerySalesDaily(ctx context.Context, filter Filter) ([]SalesDailyRecord, error)
}

// SalesDailyRecord 是销售按日聚合使用的单订单原始记录。
type SalesDailyRecord struct {
	// OrderID 是订单标识，用于按日去重计数。
	OrderID string
	// Amount 是数据库中的订单金额文本。
	Amount string
	// Status 是持久化的订单状态文本或数字码。
	Status string
	// CreatedAt 是数据库中的订单创建时间文本。
	CreatedAt string
}
