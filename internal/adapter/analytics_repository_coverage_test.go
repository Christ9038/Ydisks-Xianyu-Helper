package adapter

import (
	"context"
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
