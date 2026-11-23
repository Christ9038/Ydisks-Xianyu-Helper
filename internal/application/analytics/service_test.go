package analytics

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"
)

// analyticsRepositoryFake 是订单分析应用服务测试使用的可控 Port 实现。
type analyticsRepositoryFake struct {
	// dashboard 保存仪表盘统计结果。
	dashboard DashboardStats
	// stock 保存可用卡密库存数量。
	stock int64
	// revenue 保存收益聚合结果（金额为整数分）。
	revenue RevenueRecord
	// daily 保存日期聚合原始记录。
	daily []DailyRecord
	// statuses 保存状态聚合原始记录。
	statuses []StatusRecord
	// cities 保存城市聚合原始记录。
	cities []CityRecord
	// items 保存商品聚合原始记录。
	items []ItemRecord
	// validOrders 保存有效订单原始记录。
	validOrders []ValidOrderRecord
	// salesDaily 保存销售按日聚合原始订单记录。
	salesDaily []SalesDailyRecord
	// total 保存有效订单总数。
	total int
	// err 保存所有测试 Port 调用返回的错误。
	err error
	// dashboardErr 保存仪表盘计数查询错误。
	dashboardErr error
	// stockErr 保存卡密库存查询错误。
	stockErr error
	// revenueErr 保存收益查询错误。
	revenueErr error
	// dailyErr 保存按日查询错误。
	dailyErr error
	// statusErr 保存按状态查询错误。
	statusErr error
	// cityErr 保存按城市查询错误。
	cityErr error
	// itemErr 保存按商品查询错误。
	itemErr error
	// countErr 保存有效订单总数查询错误。
	countErr error
	// rowsErr 保存有效订单明细查询错误。
	rowsErr error
	// receivedFilter 保存最近一次收到的查询条件。
	receivedFilter Filter
	// receivedLimit 保存最近一次分页大小。
	receivedLimit int
	// receivedOffset 保存最近一次分页偏移。
	receivedOffset int
}

// DashboardStats 返回预置的仪表盘统计。
func (f *analyticsRepositoryFake) DashboardStats(context.Context, int64) (DashboardStats, error) {
	if f.dashboardErr != nil {
		return DashboardStats{}, f.dashboardErr
	}
	return f.dashboard, f.err
}

// AvailableCardStock 返回预置的可用卡密库存。
func (f *analyticsRepositoryFake) AvailableCardStock(context.Context, int64) (int64, error) {
	if f.stockErr != nil {
		return 0, f.stockErr
	}
	return f.stock, f.err
}

// QueryRevenue 返回预置的收益统计并记录筛选条件。
func (f *analyticsRepositoryFake) QueryRevenue(_ context.Context, filter Filter) (RevenueRecord, error) {
	f.receivedFilter = filter
	if f.revenueErr != nil {
		return RevenueRecord{}, f.revenueErr
	}
	return f.revenue, f.err
}

// QueryDaily 返回预置的日期记录。
func (f *analyticsRepositoryFake) QueryDaily(context.Context, Filter) ([]DailyRecord, error) {
	if f.dailyErr != nil {
		return nil, f.dailyErr
	}
	return f.daily, f.err
}

// QueryStatus 返回预置的状态记录。
func (f *analyticsRepositoryFake) QueryStatus(context.Context, Filter) ([]StatusRecord, error) {
	if f.statusErr != nil {
		return nil, f.statusErr
	}
	return f.statuses, f.err
}

// QueryCity 返回预置的城市记录。
func (f *analyticsRepositoryFake) QueryCity(context.Context, Filter) ([]CityRecord, error) {
	if f.cityErr != nil {
		return nil, f.cityErr
	}
	return f.cities, f.err
}

// QueryItem 返回预置的商品记录。
func (f *analyticsRepositoryFake) QueryItem(context.Context, Filter) ([]ItemRecord, error) {
	if f.itemErr != nil {
		return nil, f.itemErr
	}
	return f.items, f.err
}

// CountValidOrders 返回预置的有效订单总数。
func (f *analyticsRepositoryFake) CountValidOrders(context.Context, Filter) (int, error) {
	if f.countErr != nil {
		return 0, f.countErr
	}
	return f.total, f.err
}

// ListValidOrders 返回预置的有效订单记录并记录分页参数。
func (f *analyticsRepositoryFake) ListValidOrders(_ context.Context, _ Filter, limit, offset int) ([]ValidOrderRecord, error) {
	f.receivedLimit = limit
	f.receivedOffset = offset
	if f.rowsErr != nil {
		return nil, f.rowsErr
	}
	return f.validOrders, f.err
}

// QuerySalesDaily 返回预置的销售逐单原始记录。
func (f *analyticsRepositoryFake) QuerySalesDaily(context.Context, Filter) ([]SalesDailyRecord, error) {
	if f.dailyErr != nil {
		return nil, f.dailyErr
	}
	return f.salesDaily, f.err
}

// TestDashboardStatsUsesNarrowStockPort 验证仪表盘只依赖非敏感库存数量 Port。
func TestDashboardStatsUsesNarrowStockPort(t *testing.T) {
	// repository 是带有预置摘要和库存的测试 Port。
	repository := &analyticsRepositoryFake{dashboard: DashboardStats{TotalCookies: 2, TotalOrders: 5}, stock: 7}
	// service 是待验证的订单分析应用服务。
	service := NewService(repository)
	// result、err 是应用服务返回的仪表盘结果和错误。
	result, err := service.DashboardStats(context.Background(), 9)
	if err != nil {
		t.Fatalf("仪表盘查询失败: %v", err)
	}
	if result.TotalCookies != 2 || result.TotalOrders != 5 || result.AvailableCardStock != 7 {
		t.Fatalf("仪表盘结果异常: %+v", result)
	}
}

// TestOrderAnalyticsAggregatesAndConvertsLocalDate 验证订单分析聚合和本地日期转换。
func TestOrderAnalyticsAggregatesAndConvertsLocalDate(t *testing.T) {
	// repository 是带有各维度订单记录的测试 Port。
	repository := &analyticsRepositoryFake{
		revenue: RevenueRecord{TotalOrders: 2, TotalCents: 1235, UniqueBuyers: 2, UniqueItems: 1},
		daily: []DailyRecord{
			{OrderID: "one", Amount: "¥10.50", CreatedAt: "2026-06-27 16:30:00"},
			{OrderID: "two", Amount: "abc", CreatedAt: "2026-06-27 17:00:00"},
		},
		statuses: []StatusRecord{{Status: "3", Count: 1, AmountCents: 850}, {Status: "shipped", Count: 1, AmountCents: 200}},
		cities:   []CityRecord{{City: "杭州", Count: 2, AmountCents: 1050}},
		items:    []ItemRecord{{ItemID: "item-1", Count: 2, TotalCents: 1050}},
	}
	// service 是待验证的订单分析应用服务。
	service := NewService(repository)
	// location 是 UTC+8 的用户本地时区。
	location := time.FixedZone("UTC+8", 8*60*60)
	// result、err 是应用服务返回的完整分析结果和错误。
	result, err := service.OrderAnalytics(context.Background(), Query{UserID: 7, StartDate: "2026-06-28", EndDate: "2026-06-28", Location: location})
	if err != nil {
		t.Fatalf("订单分析失败: %v", err)
	}
	if len(result.DailyStats) != 1 || result.DailyStats[0].Date != "2026-06-28" || result.DailyStats[0].Amount != 10.5 {
		t.Fatalf("日期统计异常: %+v", result.DailyStats)
	}
	if len(result.StatusStats) != 1 || result.StatusStats[0].Status != "shipped" || result.StatusStats[0].Count != 2 {
		t.Fatalf("状态统计异常: %+v", result.StatusStats)
	}
	// 1235 分 / 2 单 = 617.5 分，四舍五入为 618 分（6.18 元）。
	if result.RevenueStats.TotalAmount != 12.35 || result.RevenueStats.AvgAmount != 6.18 {
		t.Fatalf("收益整数分汇总或平均值异常: %+v", result.RevenueStats)
	}
	if len(result.CityStats) != 1 || result.CityStats[0].TotalAmount != 10.5 || len(result.ItemStats) != 1 || result.ItemStats[0].TotalAmount != 10.5 || result.ItemStats[0].AvgAmount != 5.25 {
		t.Fatalf("城市或商品整数分换算异常: %+v %+v", result.CityStats, result.ItemStats)
	}
	if repository.receivedFilter.UserID != 7 || repository.receivedFilter.StartAt != "2026-06-27 16:00:00" || repository.receivedFilter.EndBefore != "2026-06-28 16:00:00" {
		t.Fatalf("筛选条件异常: %+v", repository.receivedFilter)
	}
}

// TestSalesStatsSeparatesSalesReceivedAndRefunded 验证销售统计三项指标分列口径、数字码归一、按日时区聚合，以及总计与按日明细一致。
func TestSalesStatsSeparatesSalesReceivedAndRefunded(t *testing.T) {
	// repository 是带有已发货/已完成/已取消订单、非法金额订单和非法时间订单的测试 Port。
	repository := &analyticsRepositoryFake{
		salesDaily: []SalesDailyRecord{
			{OrderID: "one", Amount: "¥10.50", Status: "3", CreatedAt: "2026-06-27 16:30:00"},
			{OrderID: "two", Amount: "8.00", Status: "completed", CreatedAt: "2026-06-27 17:00:00"},
			{OrderID: "three", Amount: "5.25", Status: "cancelled", CreatedAt: "2026-06-27 18:00:00"},
			{OrderID: "four", Amount: "bad", Status: "shipped", CreatedAt: "2026-06-27 18:30:00"},
			{OrderID: "five", Amount: "2", Status: "shipped", CreatedAt: "invalid-time"},
			{OrderID: "six", Amount: "8", Status: "4", CreatedAt: "2026-06-28 20:00:00"},
		},
	}
	// service 是待验证的订单分析应用服务。
	service := NewService(repository)
	// location 是 UTC+8 的用户本地时区。
	location := time.FixedZone("UTC+8", 8*60*60)
	// result、err 是应用服务返回的销售统计结果和错误。
	result, err := service.SalesStats(context.Background(), Query{UserID: 7, StartDate: "2026-06-28", EndDate: "2026-06-29", Location: location})
	if err != nil {
		t.Fatalf("销售统计失败: %v", err)
	}
	// 销售额=已发货(one 10.5、five 2) + 已完成(two 8、six 8)；非法金额订单 four 整单排除。
	if result.SalesAmount != 28.5 || result.SalesOrders != 4 {
		t.Fatalf("销售额口径异常: %+v", result)
	}
	if result.ReceivedAmount != 16 || result.ReceivedOrders != 2 {
		t.Fatalf("已收货款口径异常: %+v", result)
	}
	if result.RefundedAmount != 5.25 || result.RefundedOrders != 1 {
		t.Fatalf("退货货款口径异常: %+v", result)
	}
	// 按日聚合：one/two/three 在用户时区归 06-28，six 归 06-29；时间非法的 five 只计入总计。
	if len(result.Daily) != 2 || result.Daily[0].Date != "2026-06-28" || result.Daily[1].Date != "2026-06-29" {
		t.Fatalf("按日聚合异常: %+v", result.Daily)
	}
	// first、second 是两个日期的销售聚合结果。
	first, second := result.Daily[0], result.Daily[1]
	if first.SalesAmount != 18.5 || first.ReceivedAmount != 8 || first.RefundedAmount != 5.25 || first.OrderCount != 3 {
		t.Fatalf("06-28 指标异常: %+v", first)
	}
	if second.SalesAmount != 8 || second.ReceivedAmount != 8 || second.RefundedAmount != 0 || second.OrderCount != 1 {
		t.Fatalf("06-29 指标异常: %+v", second)
	}
}

// TestSalesStatsIgnoresAbsurdAmountsFromIssue129 回归 #129：金额为 1 亿元的异常已取消订单不得进入退货货款，边界内金额仍正常计入。
func TestSalesStatsIgnoresAbsurdAmountsFromIssue129(t *testing.T) {
	// repository 是含 1 亿元、超长整数、上限边界和正常退款订单的测试 Port。
	repository := &analyticsRepositoryFake{salesDaily: []SalesDailyRecord{
		{OrderID: "absurd", Amount: "100000000", Status: "cancelled", CreatedAt: "2026-06-28 01:00:00"},
		{OrderID: "huge", Amount: "99999999999999999999999999", Status: "completed", CreatedAt: "2026-06-28 01:00:00"},
		{OrderID: "just-over", Amount: "10000000.01", Status: "shipped", CreatedAt: "2026-06-28 01:00:00"},
		{OrderID: "limit", Amount: "10000000.00", Status: "completed", CreatedAt: "2026-06-28 01:00:00"},
		{OrderID: "normal", Amount: "92.50", Status: "cancelled", CreatedAt: "2026-06-28 01:00:00"},
	}}
	// result、err 是销售统计结果和错误。
	result, err := NewService(repository).SalesStats(context.Background(), Query{UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	if result.RefundedAmount != 92.5 || result.RefundedOrders != 1 {
		t.Fatalf("异常金额污染了退货货款: %+v", result)
	}
	if result.SalesAmount != 10000000 || result.SalesOrders != 1 || result.ReceivedAmount != 10000000 || result.ReceivedOrders != 1 {
		t.Fatalf("上限边界金额应计入而超限金额应排除: %+v", result)
	}
	if len(result.Daily) != 1 || result.Daily[0].OrderCount != 2 {
		t.Fatalf("被排除订单不应计入按日订单数: %+v", result.Daily)
	}
}

// TestSalesStatsAccumulatesExactCents 验证大量二进制浮点不可精确表示的金额累加后无误差，且第三位小数按分四舍五入。
func TestSalesStatsAccumulatesExactCents(t *testing.T) {
	// records 是 3000 笔 0.10/0.20/0.30 元的已完成订单，浮点累加会出现 0.1+0.2≠0.3 类误差。
	records := make([]SalesDailyRecord, 0, 3001)
	// index 是当前生成订单的序号。
	for index := 0; index < 1000; index++ {
		// offset 是三笔金额在本组内的序号，amount 是该笔订单金额文本。
		for offset, amount := range []string{"0.10", "0.20", "0.30"} {
			records = append(records, SalesDailyRecord{OrderID: fmt.Sprintf("o-%d-%d", index, offset), Amount: amount, Status: "completed", CreatedAt: "2026-06-28 01:00:00"})
		}
	}
	records = append(records, SalesDailyRecord{OrderID: "round", Amount: "1.005", Status: "completed", CreatedAt: "2026-06-28 01:00:00"})
	// result、err 是销售统计结果和错误。
	result, err := NewService(&analyticsRepositoryFake{salesDaily: records}).SalesStats(context.Background(), Query{UserID: 1})
	if err != nil {
		t.Fatal(err)
	}
	// 1000×(0.10+0.20+0.30)=600.00，再加 1.005 四舍五入为 1.01，共 601.01。
	if result.SalesAmount != 601.01 || result.ReceivedAmount != 601.01 || result.SalesOrders != 3001 {
		t.Fatalf("整数分累加结果异常: %+v", result)
	}
	if result.Daily[0].SalesAmount != result.SalesAmount || result.Daily[0].OrderCount != 3001 {
		t.Fatalf("总计与按日明细不一致: %+v", result.Daily[0])
	}
}

// TestSalesStatsCountsDuplicateOrderOnce 验证重复订单号只累计一次，避免异常数据放大金额。
func TestSalesStatsCountsDuplicateOrderOnce(t *testing.T) {
	// result、err 是含重复订单号记录的销售统计结果。
	result, err := NewService(&analyticsRepositoryFake{salesDaily: []SalesDailyRecord{
		{OrderID: "dup", Amount: "5.00", Status: "shipped", CreatedAt: "2026-06-28 01:00:00"},
		{OrderID: "dup", Amount: "5.00", Status: "shipped", CreatedAt: "2026-06-28 01:00:00"},
	}}).SalesStats(context.Background(), Query{UserID: 1})
	if err != nil || result.SalesAmount != 5 || result.SalesOrders != 1 {
		t.Fatalf("重复订单号应只计一次: %+v err=%v", result, err)
	}
}

// TestSalesStatsEmptyAndCentsHelpers 验证无订单时返回零值结果，以及分到元的换算。
func TestSalesStatsEmptyAndCentsHelpers(t *testing.T) {
	// result、err 是无订单时的销售统计结果。
	result, err := NewService(&analyticsRepositoryFake{}).SalesStats(context.Background(), Query{UserID: 1})
	if err != nil || result.SalesAmount != 0 || result.RefundedAmount != 0 || len(result.Daily) != 0 {
		t.Fatalf("空结果异常: %+v err=%v", result, err)
	}
	if centsToYuan(1_000_000_009) != 10000000.09 || centsToYuan(1) != 0.01 || centsToYuan(0) != 0 {
		t.Fatal("分到元换算异常")
	}
}

// TestSalesStatsKeepsQueryStageOnError 验证销售统计查询失败保留阶段错误语义。
func TestSalesStatsKeepsQueryStageOnError(t *testing.T) {
	// expected 是底层测试查询错误。
	expected := errors.New("sales query failed")
	// service 是返回销售逐单查询错误的订单分析应用服务。
	service := NewService(&analyticsRepositoryFake{dailyErr: expected})
	// result、err 是应用服务返回的销售统计结果和错误。
	result, err := service.SalesStats(context.Background(), Query{UserID: 1})
	if err == nil || !errors.Is(err, expected) {
		t.Fatalf("销售统计错误语义异常: err=%v", err)
	}
	// stageErr 是带查询阶段的应用服务错误。
	var stageErr *StageError
	if !errors.As(err, &stageErr) || stageErr.Stage != StageSales || ErrorMessage(err) != "查询销售统计失败" {
		t.Fatalf("销售统计阶段错误异常: %+v", stageErr)
	}
	if result.SalesAmount != 0 || result.Daily != nil {
		t.Fatalf("失败结果应为零值: %+v", result)
	}
	// dailyService 是按日查询失败的订单分析应用服务。
	dailyService := NewService(&analyticsRepositoryFake{dailyErr: expected})
	// dailyErr 是按日聚合查询失败时返回的底层错误，用于确认阶段错误包装保留原始语义。
	if _, dailyErr := dailyService.SalesStats(context.Background(), Query{UserID: 1}); dailyErr == nil || !errors.Is(dailyErr, expected) {
		t.Fatalf("按日查询错误语义异常: err=%v", dailyErr)
	}
}

// TestValidOrdersMapsLegacyStatusAndPagination 验证有效订单分页保留兼容字段和主图。
func TestValidOrdersMapsLegacyStatusAndPagination(t *testing.T) {
	// repository 是带有数字状态和商品详情的测试 Port。
	repository := &analyticsRepositoryFake{total: 4, validOrders: []ValidOrderRecord{{
		OrderID: "order-1", ItemID: "item-1", ItemTitle: "商品", ItemDetail: `{"pic_info":{"picUrl":"https://img.example/1.png"}}`,
		Status: "3", Amount: "9.90", Quantity: "1", CookieID: "cookie-1",
	}}}
	// service 是待验证的订单分析应用服务。
	service := NewService(repository)
	// result、err 是应用服务返回的分页结果和错误。
	result, err := service.ValidOrders(context.Background(), Query{UserID: 1}, 2, 2)
	if err != nil {
		t.Fatalf("有效订单查询失败: %v", err)
	}
	if result.Page != 2 || result.PageSize != 2 || !result.Truncated || repository.receivedLimit != 2 || repository.receivedOffset != 2 {
		t.Fatalf("分页结果异常: %+v limit=%d offset=%d", result, repository.receivedLimit, repository.receivedOffset)
	}
	if len(result.Orders) != 1 || result.Orders[0].Status != "shipped" || result.Orders[0].OrderStatus != "shipped" || result.Orders[0].ItemImage == "" {
		t.Fatalf("有效订单映射异常: %+v", result.Orders)
	}
}

// TestAnalyticsErrorsKeepQueryStage 验证查询失败仍保留阶段错误语义。
func TestAnalyticsErrorsKeepQueryStage(t *testing.T) {
	// expected 是底层测试查询错误。
	expected := errors.New("query failed")
	// service 是返回底层错误的订单分析应用服务。
	service := NewService(&analyticsRepositoryFake{err: expected})
	// _, err 是收益阶段查询返回的错误。
	_, err := service.OrderAnalytics(context.Background(), Query{})
	if !errors.Is(err, expected) || ErrorMessage(err) != "查询收益统计失败" {
		t.Fatalf("阶段错误异常: err=%v message=%s", err, ErrorMessage(err))
	}
}

// TestAnalyticsErrorStagesCoverAllQueries 验证每个查询阶段错误都映射为稳定的兼容消息。
func TestAnalyticsErrorStagesCoverAllQueries(t *testing.T) {
	// expected 是每个测试场景共用的底层查询错误。
	expected := errors.New("stage query failed")
	// cases 是查询阶段、构造测试 Port 和兼容错误消息的映射表。
	cases := []struct {
		// name 是测试场景名称。
		name string
		// repository 是注入对应阶段错误的测试 Port。
		repository *analyticsRepositoryFake
		// run 是触发当前阶段查询的应用用例调用。
		run func(*Service) error
		// message 是原有 HTTP 层需要保持的错误消息。
		message string
	}{
		{name: "daily", repository: &analyticsRepositoryFake{dailyErr: expected}, run: func(service *Service) error {
			// err 是每日查询阶段返回的错误。
			_, err := service.OrderAnalytics(context.Background(), Query{})
			return err
		}, message: "查询每日统计失败"},
		{name: "status", repository: &analyticsRepositoryFake{statusErr: expected}, run: func(service *Service) error {
			// err 是状态查询阶段返回的错误。
			_, err := service.OrderAnalytics(context.Background(), Query{})
			return err
		}, message: "查询状态统计失败"},
		{name: "city", repository: &analyticsRepositoryFake{cityErr: expected}, run: func(service *Service) error {
			// err 是城市查询阶段返回的错误。
			_, err := service.OrderAnalytics(context.Background(), Query{})
			return err
		}, message: "查询城市统计失败"},
		{name: "item", repository: &analyticsRepositoryFake{itemErr: expected}, run: func(service *Service) error {
			// err 是商品查询阶段返回的错误。
			_, err := service.OrderAnalytics(context.Background(), Query{})
			return err
		}, message: "查询商品统计失败"},
		{name: "valid-count", repository: &analyticsRepositoryFake{countErr: expected}, run: func(service *Service) error {
			// err 是有效订单总数查询阶段返回的错误。
			_, err := service.ValidOrders(context.Background(), Query{}, 1, 10)
			return err
		}, message: "查询失败"},
		{name: "valid-rows", repository: &analyticsRepositoryFake{rowsErr: expected}, run: func(service *Service) error {
			// err 是有效订单明细查询阶段返回的错误。
			_, err := service.ValidOrders(context.Background(), Query{}, 1, 10)
			return err
		}, message: "查询失败"},
	}
	// item 是当前遍历的阶段错误场景。
	for _, item := range cases {
		t.Run(item.name, func(t *testing.T) {
			// err 是当前阶段用例返回的错误。
			err := item.run(NewService(item.repository))
			if !errors.Is(err, expected) || ErrorMessage(err) != item.message {
				t.Fatalf("阶段错误异常: err=%v message=%s", err, ErrorMessage(err))
			}
		})
	}
	// dashboardCases 是仪表盘两个查询阶段的错误场景。
	dashboardCases := []struct {
		// name 是测试场景名称。
		name string
		// repository 是注入错误的测试 Port。
		repository *analyticsRepositoryFake
	}{
		{name: "dashboard-count", repository: &analyticsRepositoryFake{dashboardErr: expected}},
		{name: "dashboard-stock", repository: &analyticsRepositoryFake{stockErr: expected}},
	}
	// item 是当前遍历的仪表盘错误场景。
	for _, item := range dashboardCases {
		t.Run(item.name, func(t *testing.T) {
			// err 是当前仪表盘用例返回的错误。
			_, err := NewService(item.repository).DashboardStats(context.Background(), 1)
			if !errors.Is(err, expected) {
				t.Fatalf("仪表盘错误未保留: %v", err)
			}
		})
	}
}

// TestAnalyticsCompatibilityHelpersCoverFallbacks 验证日期、状态和商品图片兼容回退。
func TestAnalyticsCompatibilityHelpersCoverFallbacks(t *testing.T) {
	// location 是边界转换使用的测试时区。
	location := time.FixedZone("UTC+8", 8*60*60)
	if // got 是非法起始日期回退后的原始文本。
	got := DateBoundary("invalid", false, location); got != "invalid" {
		t.Fatalf("非法日期应保留原文本: %q", got)
	}
	if // got 是非法偏移回退后的本地时区。
	got := LocationFromOffset("invalid"); got != time.Local {
		t.Fatalf("非法时区偏移应回退本地时区: %v", got)
	}
	if // got 是空状态归一后的未知状态名称。
	got := normalizeStatus(""); got != "unknown" || normalizeStatus("custom") != "custom" {
		t.Fatalf("状态回退异常: empty=%q custom=%q", got, normalizeStatus("custom"))
	}
	// repository 是带有兼容图片字段的测试 Port。
	repository := &analyticsRepositoryFake{total: 1, validOrders: []ValidOrderRecord{{OrderID: "fallback", ItemDetail: `{"item_image":"https://img.example/fallback.png"}`}}}
	// result、err 是兼容图片字段映射结果和错误。
	result, err := NewService(repository).ValidOrders(context.Background(), Query{}, 1, 1)
	if err != nil || len(result.Orders) != 1 || result.Orders[0].ItemImage == "" {
		t.Fatalf("兼容图片回退异常: result=%+v err=%v", result, err)
	}
}

// TestDateBoundaryConvertsLocalDayToUTC 验证用户本地日期边界转换为 UTC 文本。
func TestDateBoundaryConvertsLocalDayToUTC(t *testing.T) {
	// location 是 UTC+8 的测试时区。
	location := time.FixedZone("UTC+8", 8*60*60)
	if // got 是起始日期转换后的 UTC 边界。
	got := DateBoundary("2026-06-28", false, location); got != "2026-06-27 16:00:00" {
		t.Fatalf("起始边界=%q", got)
	}
	if // got 是结束日期转换后的 UTC 边界。
	got := DateBoundary("2026-06-28", true, location); got != "2026-06-28 16:00:00" {
		t.Fatalf("结束边界=%q", got)
	}
}

// TestAnalyticsNilAndFallbackBranches 验证错误包装、解析失败和排序回退分支。
func TestAnalyticsNilAndFallbackBranches(t *testing.T) {
	if // message 是 nil 错误包装的稳定消息。
	message := (*StageError)(nil).Error(); message != "订单分析查询失败" {
		t.Fatalf("nil 阶段错误消息异常: %q", message)
	}
	if // message 是底层错误为空的非 nil 包装稳定消息。
	message := (&StageError{}).Error(); message != "订单分析查询失败" {
		t.Fatalf("空底层错误消息异常: %q", message)
	}
	if // message 是底层错误存在时透传的错误文本。
	message := (&StageError{Err: errors.New("wrapped")}).Error(); message != "wrapped" {
		t.Fatalf("底层错误文本未透传: %q", message)
	}
	if // unwrapped 是 nil 错误包装的底层错误。
	unwrapped := (*StageError)(nil).Unwrap(); unwrapped != nil {
		t.Fatalf("nil 阶段错误应无底层错误: %v", unwrapped)
	}
	if // message 是普通错误映射出的兼容查询消息。
	message := ErrorMessage(errors.New("plain error")); message != "查询失败" {
		t.Fatalf("普通错误消息异常: %q", message)
	}
	if // message 是未知阶段映射出的兜底查询消息。
	message := ErrorMessage(&StageError{Stage: StageValidRows, Err: errors.New("rows")}); message != "查询失败" {
		t.Fatalf("未知阶段消息异常: %q", message)
	}
	if // location 是合法浏览器偏移转换后的固定时区。
	location := LocationFromOffset(" 480 "); location == time.Local || location == nil {
		t.Fatalf("合法时区偏移未转换: %v", location)
	}
	if // parsed 是非法数据库时间解析后的零值。
	parsed := parseDBTime("not-a-time"); !parsed.IsZero() {
		t.Fatalf("非法数据库时间应为零值: %v", parsed)
	}
	if // image 是非法商品详情 JSON 的空图片结果。
	image := itemImageFromDetail("{"); image != "" {
		t.Fatalf("非法商品详情不应产生图片: %q", image)
	}
	if // image 是空商品详情的空图片结果。
	image := itemImageFromDetail(""); image != "" {
		t.Fatalf("空商品详情不应产生图片: %q", image)
	}
	if // image 是缺少图片字段的商品详情空结果。
	image := itemImageFromDetail(`{"pic_info":{}}`); image != "" {
		t.Fatalf("缺少图片字段不应产生图片: %q", image)
	}
	// repository 是包含非法时间和多个状态的聚合测试 Port。
	repository := &analyticsRepositoryFake{
		daily:    []DailyRecord{{CreatedAt: "invalid"}},
		statuses: []StatusRecord{{Status: "paid", Count: 1, AmountCents: 100}, {Status: "cancelled", Count: 3, AmountCents: 300}},
	}
	// result、err 是覆盖非法时间跳过和状态排序的分析结果。
	result, err := NewService(repository).OrderAnalytics(context.Background(), Query{})
	if err != nil || len(result.DailyStats) != 0 || len(result.StatusStats) != 2 || result.StatusStats[0].Status != "cancelled" {
		t.Fatalf("分析回退分支异常: result=%+v err=%v", result, err)
	}
}

// TestOrderAnalyticsDailyExcludesAbsurdAmountsAndKeepsExactCents 验证订单分析按日汇总同样按整数分累加，并整单排除超范围或非法金额（#129）。
func TestOrderAnalyticsDailyExcludesAbsurdAmountsAndKeepsExactCents(t *testing.T) {
	// records 是 1000 笔 0.10 元订单，加上 1 亿元异常订单与非法金额订单。
	records := make([]DailyRecord, 0, 1002)
	// index 是当前生成订单的序号。
	for index := 0; index < 1000; index++ {
		records = append(records, DailyRecord{OrderID: fmt.Sprintf("o-%d", index), Amount: "0.10", CreatedAt: "2026-06-28 01:00:00"})
	}
	records = append(records, DailyRecord{OrderID: "absurd", Amount: "100000000", CreatedAt: "2026-06-28 01:00:00"}, DailyRecord{OrderID: "bad", Amount: "abc", CreatedAt: "2026-06-28 01:00:00"}, DailyRecord{OrderID: "no-time", Amount: "5.00", CreatedAt: "invalid-time"})
	// result、err 是订单分析结果。
	result, err := NewService(&analyticsRepositoryFake{daily: records}).OrderAnalytics(context.Background(), Query{Location: time.UTC})
	if err != nil || len(result.DailyStats) != 1 {
		t.Fatalf("按日结果异常: %+v err=%v", result, err)
	}
	if result.DailyStats[0].Amount != 100 || result.DailyStats[0].OrderCount != 1000 {
		t.Fatalf("按日金额应为 100.00 且排除异常订单: %+v", result.DailyStats[0])
	}
}

// TestAverageCentsRoundsHalfUpAndGuardsZero 验证平均分值整数除法四舍五入，订单数为 0 时返回 0。
func TestAverageCentsRoundsHalfUpAndGuardsZero(t *testing.T) {
	if averageCents(1235, 2) != 618 || averageCents(1234, 2) != 617 || averageCents(1, 3) != 0 || averageCents(2, 3) != 1 || averageCents(100, 0) != 0 || averageCents(100, -1) != 0 {
		t.Fatal("平均分值计算异常")
	}
}
