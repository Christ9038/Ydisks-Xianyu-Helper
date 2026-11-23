package adapter

import (
	"context"
	"strings"
	"testing"

	analyticsapp "xianyu-go/internal/application/analytics"
	"xianyu-go/internal/db"
)

// TestAnalyticsRepositoryQueries 覆盖订单分析适配器的统计、聚合和明细查询路径。
func TestAnalyticsRepositoryQueries(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// owner、ownerErr 保存分析数据所属用户及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// cardID、cardErr 保存数据卡券创建结果和数据库错误。
	cardID, cardErr := store.Cards.Create(ctx, &db.CardFull{Name: "分析库存", Type: "data", DataContent: "A\nB", Enabled: true, UserID: owner.ID})
	if cardErr != nil {
		t.Fatal(cardErr)
	}
	// keywordID、keywordErr 保存关键词创建结果和数据库错误。
	keywordID, keywordErr := store.Keywords.Add(ctx, "cid", "关键词", "回复", "item-1", "text", "")
	if keywordErr != nil || keywordID == 0 {
		t.Fatalf("keyword id=%d err=%v", keywordID, keywordErr)
	}
	// orderErr 保存第一条订单创建结果。
	if orderErr := store.Orders.Upsert(ctx, "analytics-1", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer-1", OrderStatus: "paid", Amount: "¥1,200.50", Quantity: "2", ReceiverCity: "上海", ReceiverName: "张三", CreatedAt: "2024-01-02 10:00:00"}); orderErr != nil {
		t.Fatal(orderErr)
	}
	// secondOrderErr 保存第二条订单创建结果。
	if secondOrderErr := store.Orders.Upsert(ctx, "analytics-2", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-2", BuyerID: "buyer-2", OrderStatus: "shipped", Amount: "2.50", Quantity: "1", ReceiverCity: "北京", CreatedAt: "2024-01-03 10:00:00"}); secondOrderErr != nil {
		t.Fatal(secondOrderErr)
	}
	// repository 保存订单分析数据库适配器。
	repository := NewAnalyticsRepository(store)
	// stats、statsErr 保存用户仪表盘统计和数据库错误。
	stats, statsErr := repository.DashboardStats(ctx, owner.ID)
	if statsErr != nil || stats.TotalCookies != 1 || stats.TotalCards != 1 || stats.TotalKeywords != 1 || stats.TotalOrders != 2 {
		t.Fatalf("dashboard stats=%+v err=%v", stats, statsErr)
	}
	// stock、stockErr 保存可用数据卡密库存和数据库错误。
	stock, stockErr := repository.AvailableCardStock(ctx, owner.ID)
	if stockErr != nil || stock != 2 {
		t.Fatalf("card stock=%d err=%v", stock, stockErr)
	}
	// filter 保存按用户和有效状态筛选的分析条件。
	filter := analyticsapp.Filter{UserID: owner.ID, Statuses: []string{"paid", "shipped"}}
	// revenue、revenueErr 保存收益聚合和数据库错误。
	revenue, revenueErr := repository.QueryRevenue(ctx, filter)
	if revenueErr != nil || revenue.TotalOrders != 2 || revenue.UniqueBuyers != 2 || revenue.UniqueItems != 2 {
		t.Fatalf("revenue=%+v err=%v", revenue, revenueErr)
	}
	// daily、dailyErr 保存按订单日期查询结果和数据库错误。
	daily, dailyErr := repository.QueryDaily(ctx, filter)
	if dailyErr != nil || len(daily) != 2 {
		t.Fatalf("daily=%+v err=%v", daily, dailyErr)
	}
	// statuses、statusErr 保存按订单状态聚合结果和数据库错误。
	statuses, statusErr := repository.QueryStatus(ctx, filter)
	if statusErr != nil || len(statuses) != 2 {
		t.Fatalf("statuses=%+v err=%v", statuses, statusErr)
	}
	// cities、cityErr 保存按收货城市聚合结果和数据库错误。
	cities, cityErr := repository.QueryCity(ctx, filter)
	if cityErr != nil || len(cities) != 2 {
		t.Fatalf("cities=%+v err=%v", cities, cityErr)
	}
	// items、itemErr 保存按商品聚合结果和数据库错误。
	items, itemErr := repository.QueryItem(ctx, filter)
	if itemErr != nil || len(items) != 2 {
		t.Fatalf("items=%+v err=%v", items, itemErr)
	}
	// validCount、countErr 保存有效订单总数和数据库错误。
	validCount, countErr := repository.CountValidOrders(ctx, filter)
	if countErr != nil || validCount != 2 {
		t.Fatalf("valid count=%d err=%v", validCount, countErr)
	}
	// validRows、rowsErr 保存有效订单分页明细和数据库错误。
	validRows, rowsErr := repository.ListValidOrders(ctx, filter, 10, 0)
	if rowsErr != nil || len(validRows) != 2 || validRows[0].OrderID == "" {
		t.Fatalf("valid rows=%+v err=%v", validRows, rowsErr)
	}
	// _ 保存已创建卡券标识，明确测试数据确实进入了统计范围。
	_ = cardID
}

// TestAnalyticsRepositorySalesQueries 覆盖销售统计适配器的按状态聚合与按日原始记录查询路径。
func TestAnalyticsRepositorySalesQueries(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// owner、ownerErr 保存销售数据所属用户及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// filter 保存按用户与销售相关状态（已发货/已完成/已取消）过滤的查询条件。
	filter := analyticsapp.Filter{UserID: owner.ID, Statuses: []string{"shipped", "completed", "cancelled"}}
	// repository 保存订单分析数据库适配器。
	repository := NewAnalyticsRepository(store)

	// shippedErr 保存已发货订单创建结果。
	if shippedErr := store.Orders.Upsert(ctx, "sales-shipped", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer-1", OrderStatus: "shipped", Amount: "10.50", Quantity: "1", CreatedAt: "2024-01-02 10:00:00"}); shippedErr != nil {
		t.Fatal(shippedErr)
	}
	// completedErr 保存已完成订单创建结果。
	if completedErr := store.Orders.Upsert(ctx, "sales-completed", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer-2", OrderStatus: "completed", Amount: "8.00", Quantity: "1", CreatedAt: "2024-01-03 11:00:00"}); completedErr != nil {
		t.Fatal(completedErr)
	}
	// cancelledErr 保存已取消（用户确认已退款）订单创建结果。
	if cancelledErr := store.Orders.Upsert(ctx, "sales-cancelled", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer-3", OrderStatus: "cancelled", Amount: "5.25", Quantity: "1", CreatedAt: "2024-01-04 12:00:00"}); cancelledErr != nil {
		t.Fatal(cancelledErr)
	}
	// excludedErr 保存待发货订单创建结果；该状态不属于销售统计口径，应被过滤。
	if excludedErr := store.Orders.Upsert(ctx, "sales-pending", db.OrderUpsertOpts{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer-4", OrderStatus: "paid", Amount: "99.00", Quantity: "1", CreatedAt: "2024-01-05 13:00:00"}); excludedErr != nil {
		t.Fatal(excludedErr)
	}

	// dailyRows、dailyErr 保存销售逐单原始记录和数据库错误。
	dailyRows, dailyErr := repository.QuerySalesDaily(ctx, filter)
	if dailyErr != nil || len(dailyRows) != 3 {
		t.Fatalf("sales daily=%+v err=%v", dailyRows, dailyErr)
	}
	// dailyIDs 收集按日记录的订单标识，验证待发货订单被排除。
	dailyIDs := make(map[string]bool)
	// row 是当前销售按日原始记录。
	for _, row := range dailyRows {
		dailyIDs[row.OrderID] = true
	}
	if !dailyIDs["sales-shipped"] || !dailyIDs["sales-completed"] || !dailyIDs["sales-cancelled"] || dailyIDs["sales-pending"] {
		t.Fatalf("sales daily ids=%+v", dailyIDs)
	}
}

// TestAnalyticsRepositoryIntegerCentsAndRangeExclusion 验证各聚合查询在数据库内按整数分求和，并整单排除格式非法或超过 1000 万元的历史脏订单（#129）。
func TestAnalyticsRepositoryIntegerCentsAndRangeExclusion(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// owner、ownerErr 保存订单所属用户及查询错误。
	owner, ownerErr := store.Users.GetByUsername(context.Background(), "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	assertAnalyticsCentsScenario(t, store, owner.ID)
}

// assertAnalyticsCentsScenario 在给定数据库方言的存储上写入历史脏订单并验证各聚合查询的整数分汇总与范围排除；
// userID 为 0 时不按账号归属过滤（外部方言库没有预置账号数据）。
func assertAnalyticsCentsScenario(t *testing.T, store *db.Store, userID int64) {
	t.Helper()
	// ctx 限制本场景所有数据库操作的生命周期。
	ctx := context.Background()
	// seeds 是直接 SQL 写入的订单（绕过写入校验以模拟历史脏数据）：正常小数金额、1 亿元、上限边界、超长数字与非法文本。
	seeds := []struct{ id, amount, status, city string }{
		{"c-a", "0.10", "cancelled", "上海"}, {"c-b", "0.20", "cancelled", "上海"}, {"c-c", "92.50", "6", "上海"},
		{"c-limit", "10000000", "cancelled", "上海"}, {"c-dirty", "100000000", "cancelled", "上海"},
		{"c-over", "10000000.01", "cancelled", "上海"}, {"c-long", strings.Repeat("9", 400), "cancelled", "上海"},
		{"c-text", "abc", "cancelled", "上海"}, {"c-yuan", "¥1,000.05", "cancelled", "北京"},
	}
	// seed 是当前待写入订单。
	for _, seed := range seeds {
		// err 是直接写入订单的错误。
		if _, err := store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,item_id,buyer_id,amount,order_status,cookie_id,receiver_city,created_at) VALUES (?,?,?,?,?,?,?,?)`,
			seed.id, "item-"+seed.id, "buyer", seed.amount, seed.status, "cid", seed.city, "2024-01-02 10:00:00"); err != nil {
			t.Fatal(err)
		}
	}
	// filter 是只含已取消状态的分析条件。
	filter := analyticsapp.Filter{UserID: userID, Statuses: []string{"cancelled", "6"}}
	// revenue、err 是收益汇总：有效订单为 c-a、c-b、c-c、c-limit、c-yuan 共 5 单。
	revenue, err := NewAnalyticsRepository(store).QueryRevenue(ctx, filter)
	if err != nil || revenue.TotalOrders != 5 {
		t.Fatalf("收益订单数应排除脏订单: %+v err=%v", revenue, err)
	}
	// wantCents 是 0.10+0.20+92.50+10000000+1000.05 元对应的分。
	const wantCents = int64(10 + 20 + 9250 + 1_000_000_000 + 100005)
	if revenue.TotalCents != wantCents {
		t.Fatalf("收益合计应为 %d 分: %+v", wantCents, revenue)
	}
	// statuses 是按原始状态聚合结果，两个原始状态的分之和应等于收益合计。
	statuses, err := NewAnalyticsRepository(store).QueryStatus(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	// statusTotal 是各状态金额分之和。
	var statusTotal int64
	// row 是当前状态聚合记录。
	for _, row := range statuses {
		statusTotal += row.AmountCents
	}
	if statusTotal != wantCents {
		t.Fatalf("状态合计应等于收益合计: %+v", statuses)
	}
	// cities 是按城市聚合结果。
	cities, err := NewAnalyticsRepository(store).QueryCity(ctx, filter)
	if err != nil || len(cities) != 2 {
		t.Fatalf("城市聚合异常: %+v err=%v", cities, err)
	}
	// cityTotal 是各城市金额分之和。
	var cityTotal int64
	// row 是当前城市聚合记录。
	for _, row := range cities {
		cityTotal += row.AmountCents
	}
	if cityTotal != wantCents {
		t.Fatalf("城市合计异常: %+v", cities)
	}
	// items 是按商品聚合结果；脏订单的商品不应出现。
	items, err := NewAnalyticsRepository(store).QueryItem(ctx, filter)
	if err != nil {
		t.Fatal(err)
	}
	// item 是当前商品聚合记录。
	for _, item := range items {
		if item.ItemID == "item-c-dirty" || item.ItemID == "item-c-over" || item.ItemID == "item-c-long" || item.ItemID == "item-c-text" {
			t.Fatalf("脏订单商品不应进入商品排行: %+v", items)
		}
	}
	// daily、err 是按订单日期原始记录，同样只含有效订单。
	daily, err := NewAnalyticsRepository(store).QueryDaily(ctx, filter)
	if err != nil || len(daily) != 5 {
		t.Fatalf("按日记录应排除脏订单: %+v err=%v", daily, err)
	}
	// count、err 是有效订单总数。
	count, err := NewAnalyticsRepository(store).CountValidOrders(ctx, filter)
	if err != nil || count != 5 {
		t.Fatalf("有效订单数应排除脏订单: %d err=%v", count, err)
	}
}

// TestAmountCentsExpressionPerDialect 验证三种数据库方言生成的整数分表达式含有各自的整数类型与范围上限。
func TestAmountCentsExpressionPerDialect(t *testing.T) {
	// dialectCase 保存方言及其整数类型标记。
	for _, dialectCase := range []struct {
		dialect db.Dialect
		marker  string
	}{{db.DialectSQLite, "AS INTEGER"}, {db.DialectMySQL, "AS SIGNED"}, {db.DialectPostgres, "AS BIGINT"}} {
		// expression 是当前方言的整数分金额表达式。
		expression := amountCentsExpression(dialectCase.dialect, "orders.amount")
		if !strings.Contains(expression, dialectCase.marker) || !strings.Contains(expression, "10000000") || !strings.Contains(expression, "* 100") {
			t.Fatalf("方言 %v 表达式异常: %s", dialectCase.dialect, expression)
		}
	}
}
