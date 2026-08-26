package analytics

import (
	"context"
	"errors"
	"testing"
	"time"
)

// analyticsRepositoryFake 是订单分析应用服务测试使用的可控 Port 实现。
type analyticsRepositoryFake struct {
	// dashboard 保存仪表盘统计结果。
	dashboard DashboardStats
	// stock 保存可用卡密库存数量。
	stock int64
	// revenue 保存收益聚合结果。
	revenue RevenueStats
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
func (f *analyticsRepositoryFake) QueryRevenue(_ context.Context, filter Filter) (RevenueStats, error) {
	f.receivedFilter = filter
	if f.revenueErr != nil {
		return RevenueStats{}, f.revenueErr
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
		revenue: RevenueStats{TotalOrders: 2, TotalAmount: 12.345, AvgAmount: 6.789, UniqueBuyers: 2, UniqueItems: 1},
		daily: []DailyRecord{
			{OrderID: "one", Amount: "¥10.50", CreatedAt: "2026-06-27 16:30:00"},
			{OrderID: "two", Amount: "abc", CreatedAt: "2026-06-27 17:00:00"},
		},
		statuses: []StatusRecord{{Status: "3", Count: 1, Amount: 8.5}, {Status: "shipped", Count: 1, Amount: 2}},
		cities:   []CityRecord{{City: "杭州", Count: 2, Amount: 10.5}},
		items:    []ItemRecord{{ItemID: "item-1", Count: 2, TotalAmount: 10.5, AvgAmount: 5.25}},
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
	if result.RevenueStats.TotalAmount != 12.35 || result.RevenueStats.AvgAmount != 6.79 {
		t.Fatalf("收益四舍五入异常: %+v", result.RevenueStats)
	}
	if repository.receivedFilter.UserID != 7 || repository.receivedFilter.StartAt != "2026-06-27 16:00:00" || repository.receivedFilter.EndBefore != "2026-06-28 16:00:00" {
		t.Fatalf("筛选条件异常: %+v", repository.receivedFilter)
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
		statuses: []StatusRecord{{Status: "paid", Count: 1, Amount: 1}, {Status: "cancelled", Count: 3, Amount: 3}},
	}
	// result、err 是覆盖非法时间跳过和状态排序的分析结果。
	result, err := NewService(repository).OrderAnalytics(context.Background(), Query{})
	if err != nil || len(result.DailyStats) != 0 || len(result.StatusStats) != 2 || result.StatusStats[0].Status != "cancelled" {
		t.Fatalf("分析回退分支异常: result=%+v err=%v", result, err)
	}
}
