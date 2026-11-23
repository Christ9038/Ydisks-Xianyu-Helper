package adapter

import (
	"context"
	"strconv"
	"strings"

	analyticsapp "xianyu-go/internal/application/analytics"
	"xianyu-go/internal/db"
	"xianyu-go/internal/money"
)

// AnalyticsRepository 将 Store 的只读查询能力适配为订单分析应用 Port。
type AnalyticsRepository struct {
	// store 保存数据库聚合入口，仅由该基础设施适配器访问。
	store *db.Store
}

// NewAnalyticsRepository 构造订单分析查询适配器。
func NewAnalyticsRepository(store *db.Store) *AnalyticsRepository {
	return &AnalyticsRepository{store: store}
}

// DashboardStats 返回用户范围内的仪表盘计数，不读取账号凭证字段。
func (r *AnalyticsRepository) DashboardStats(ctx context.Context, userID int64) (analyticsapp.DashboardStats, error) {
	// stats 保存基础统计结果。
	stats := analyticsapp.DashboardStats{}
	// queries 保存用户范围内的固定计数查询及其目标字段。
	queries := []struct {
		// query 是当前统计项的 SQL。
		query string
		// target 是统计项写入结果的字段名称。
		target string
	}{
		{query: `SELECT COUNT(*) FROM cookies WHERE user_id=?`, target: "cookies"},
		{query: `SELECT COUNT(*) FROM cards WHERE user_id=?`, target: "cards"},
		{query: `SELECT COUNT(*) FROM keywords k WHERE EXISTS (SELECT 1 FROM cookies c WHERE c.id=k.cookie_id AND c.user_id=?)`, target: "keywords"},
		{query: `SELECT COUNT(*) FROM orders o WHERE o.deleted_at IS NULL AND EXISTS (SELECT 1 FROM cookies c WHERE c.id=o.cookie_id AND c.user_id=?)`, target: "orders"},
	}
	// item 是当前遍历的仪表盘统计项。
	for _, item := range queries {
		// count 是当前统计项的数据库结果。
		var count int64
		if // err 是当前统计项查询错误。
		err := r.store.Analytics.QueryRowContext(ctx, item.query, userID).Scan(&count); err != nil {
			return analyticsapp.DashboardStats{}, err
		}
		switch item.target {
		case "cookies":
			stats.TotalCookies = count
		case "cards":
			stats.TotalCards = count
		case "keywords":
			stats.TotalKeywords = count
		case "orders":
			stats.TotalOrders = count
		}
	}
	// activeCookies 是没有明确禁用记录的账号数量。
	var activeCookies int64
	if // err 是活跃账号统计查询错误。
	err := r.store.Analytics.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cookies c WHERE c.user_id=?
		  AND NOT EXISTS (SELECT 1 FROM cookie_status cs WHERE cs.cookie_id=c.id AND cs.enabled=0)
	`, userID).Scan(&activeCookies); err != nil {
		return analyticsapp.DashboardStats{}, err
	}
	stats.ActiveCookies = activeCookies
	return stats, nil
}

// AvailableCardStock 计算启用数据卡密组中的非空卡密行数，不向应用层传递卡密内容。
func (r *AnalyticsRepository) AvailableCardStock(ctx context.Context, userID int64) (int64, error) {
	if r == nil || r.store == nil || r.store.Cards == nil {
		return 0, db.ErrNotFound
	}
	// stock、stockErr 保存 db 层在不泄露卡密正文时计算出的可用库存数量及错误。
	stock, stockErr := r.store.Cards.AvailableDataStock(ctx, userID)
	if stockErr != nil {
		return 0, stockErr
	}
	return stock, nil
}

// QueryRevenue 返回订单收益汇总；金额在数据库内按整数分求和，避免浮点累加误差。
func (r *AnalyticsRepository) QueryRevenue(ctx context.Context, filter analyticsapp.Filter) (analyticsapp.RevenueRecord, error) {
	// where、args、cents 和 amountFilter 是当前方言的订单范围条件与整数分金额表达式。
	where, args, cents, amountFilter := r.orderScope(filter, "amount")
	// stats 是数据库返回的收益聚合值。
	var stats analyticsapp.RevenueRecord
	// err 是收益汇总查询错误。
	err := r.store.Analytics.QueryRowContext(ctx, `
		SELECT COUNT(DISTINCT order_id), COALESCE(SUM(`+cents+`),0),
		       COUNT(DISTINCT buyer_id), COUNT(DISTINCT item_id)
		FROM orders `+where+amountFilter, args...).Scan(
		&stats.TotalOrders, &stats.TotalCents, &stats.UniqueBuyers, &stats.UniqueItems)
	return stats, err
}

// QueryDaily 返回按订单读取的日期聚合原始记录。
func (r *AnalyticsRepository) QueryDaily(ctx context.Context, filter analyticsapp.Filter) ([]analyticsapp.DailyRecord, error) {
	// where、args、cents 和 amountFilter 是当前方言的订单范围条件。
	where, args, _, amountFilter := r.orderScope(filter, "amount")
	// rows、err 是日期聚合查询结果及错误。
	rows, err := r.store.Analytics.QueryContext(ctx, `SELECT order_id,amount,created_at FROM orders `+where+amountFilter, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// records 是订单日期聚合原始记录。
	records := make([]analyticsapp.DailyRecord, 0)
	for rows.Next() {
		// record 是当前订单的原始统计字段。
		var record analyticsapp.DailyRecord
		if // err 是当前日期记录读取错误。
		err := rows.Scan(&record.OrderID, &record.Amount, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// QueryStatus 返回按原始订单状态聚合的结果。
func (r *AnalyticsRepository) QueryStatus(ctx context.Context, filter analyticsapp.Filter) ([]analyticsapp.StatusRecord, error) {
	// where、args、cents 和 amountFilter 是当前方言的订单范围条件与整数分金额表达式。
	where, args, cents, amountFilter := r.orderScope(filter, "amount")
	// rows、err 是状态聚合查询结果及错误。
	rows, err := r.store.Analytics.QueryContext(ctx, `
		SELECT COALESCE(order_status,'unknown'), COUNT(DISTINCT order_id), COALESCE(SUM(`+cents+`),0)
		FROM orders `+where+amountFilter+`
		GROUP BY order_status ORDER BY COUNT(DISTINCT order_id) DESC`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// records 是数据库按原始状态返回的聚合记录。
	records := make([]analyticsapp.StatusRecord, 0)
	for rows.Next() {
		// record 是当前状态的聚合字段。
		var record analyticsapp.StatusRecord
		if // err 是当前状态记录读取错误。
		err := rows.Scan(&record.Status, &record.Count, &record.AmountCents); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// QueryCity 返回按收货城市聚合的结果。
func (r *AnalyticsRepository) QueryCity(ctx context.Context, filter analyticsapp.Filter) ([]analyticsapp.CityRecord, error) {
	// where、args、cents 和 amountFilter 是当前方言的订单范围条件与整数分金额表达式。
	where, args, cents, amountFilter := r.orderScope(filter, "amount")
	// rows、err 是城市聚合查询结果及错误。
	rows, err := r.store.Analytics.QueryContext(ctx, `
		SELECT receiver_city, COUNT(DISTINCT order_id), COALESCE(SUM(`+cents+`),0)
		FROM orders `+where+amountFilter+`
		  AND receiver_city IS NOT NULL AND receiver_city != ''
		GROUP BY receiver_city ORDER BY COUNT(DISTINCT order_id) DESC LIMIT 50`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// records 是数据库按城市返回的聚合记录。
	records := make([]analyticsapp.CityRecord, 0)
	for rows.Next() {
		// record 是当前城市的聚合字段。
		var record analyticsapp.CityRecord
		if // err 是当前城市记录读取错误。
		err := rows.Scan(&record.City, &record.Count, &record.AmountCents); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// QueryItem 返回按商品标识聚合的结果。
func (r *AnalyticsRepository) QueryItem(ctx context.Context, filter analyticsapp.Filter) ([]analyticsapp.ItemRecord, error) {
	// where、args、cents 和 amountFilter 是当前方言的订单范围条件与整数分金额表达式。
	where, args, cents, amountFilter := r.orderScope(filter, "amount")
	// rows、err 是商品聚合查询结果及错误。
	rows, err := r.store.Analytics.QueryContext(ctx, `
		SELECT item_id, COUNT(DISTINCT order_id), COALESCE(SUM(`+cents+`),0)
		FROM orders `+where+amountFilter+`
		  AND item_id IS NOT NULL AND item_id != ''
		GROUP BY item_id ORDER BY COUNT(DISTINCT order_id) DESC LIMIT 20`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// records 是数据库按商品返回的聚合记录。
	records := make([]analyticsapp.ItemRecord, 0)
	for rows.Next() {
		// record 是当前商品的聚合字段。
		var record analyticsapp.ItemRecord
		if // err 是当前商品记录读取错误。
		err := rows.Scan(&record.ItemID, &record.Count, &record.TotalCents); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// CountValidOrders 返回有效订单总数。
func (r *AnalyticsRepository) CountValidOrders(ctx context.Context, filter analyticsapp.Filter) (int, error) {
	// where、args、cents 和 amountFilter 是当前方言的订单范围条件。
	where, args, _, amountFilter := r.orderScope(filter, "orders.amount")
	// total 是数据库返回的有效订单数量。
	var total int
	// err 是有效订单总数查询错误。
	err := r.store.Analytics.QueryRowContext(ctx, `SELECT COUNT(*) FROM orders `+where+amountFilter, args...).Scan(&total)
	return total, err
}

// ListValidOrders 返回有效订单分页明细。
func (r *AnalyticsRepository) ListValidOrders(ctx context.Context, filter analyticsapp.Filter, limit, offset int) ([]analyticsapp.ValidOrderRecord, error) {
	// where、args、cents 和 amountFilter 是当前方言的订单范围条件。
	where, args, _, amountFilter := r.orderScope(filter, "orders.amount")
	args = append(args, limit, offset)
	// rows、err 是有效订单分页查询结果及错误。
	rows, err := r.store.Analytics.QueryContext(ctx, `
		SELECT orders.order_id, COALESCE(orders.item_id,''), COALESCE(item_info.item_title,''),
		       COALESCE(item_info.item_detail,''), COALESCE(orders.buyer_id,''), COALESCE(orders.quantity,'1'),
		       orders.amount, COALESCE(orders.order_status,'unknown'), COALESCE(orders.cookie_id,''), orders.created_at
		FROM orders LEFT JOIN item_info ON item_info.cookie_id=orders.cookie_id AND item_info.item_id=orders.item_id
		`+where+amountFilter+` ORDER BY orders.created_at DESC LIMIT ? OFFSET ?`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// records 是当前分页的有效订单字段。
	records := make([]analyticsapp.ValidOrderRecord, 0)
	for rows.Next() {
		// record 是当前订单的明细字段。
		var record analyticsapp.ValidOrderRecord
		if // err 是当前有效订单记录读取错误。
		err := rows.Scan(&record.OrderID, &record.ItemID, &record.ItemTitle, &record.ItemDetail, &record.BuyerID, &record.Quantity, &record.Amount, &record.Status, &record.CookieID, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// QuerySalesDaily 返回销售统计所需的逐单原始记录（订单号、金额、状态、创建时间），不含商品连接；总计与按日聚合都在应用层以整数分完成。
func (r *AnalyticsRepository) QuerySalesDaily(ctx context.Context, filter analyticsapp.Filter) ([]analyticsapp.SalesDailyRecord, error) {
	// where、args 和 amountFilter 是当前方言的订单范围条件；按日聚合在应用层解析金额，此处不复用 SQL 金额清洗。
	where, args, _, amountFilter := r.orderScope(filter, "amount")
	// rows、err 是销售订单原始记录查询结果及错误。
	rows, err := r.store.Analytics.QueryContext(ctx, `
		SELECT order_id, amount, COALESCE(order_status,'unknown'), created_at
		FROM orders `+where+amountFilter, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// records 是销售按日聚合原始记录。
	records := make([]analyticsapp.SalesDailyRecord, 0)
	for rows.Next() {
		// record 是当前订单的原始统计字段。
		var record analyticsapp.SalesDailyRecord
		if // err 是当前销售订单记录读取错误。
		err := rows.Scan(&record.OrderID, &record.Amount, &record.Status, &record.CreatedAt); err != nil {
			return nil, err
		}
		records = append(records, record)
	}
	return records, rows.Err()
}

// orderScope 构造订单查询的用户、日期、状态和金额过滤条件。
func (r *AnalyticsRepository) orderScope(filter analyticsapp.Filter, amountColumn string) (string, []any, string, string) {
	// conditions 保存 SQL WHERE 条件片段。
	conditions := []string{"orders.deleted_at IS NULL"}
	// args 保存条件对应的参数值。
	args := make([]any, 0, len(filter.Statuses)+3)
	if filter.StartAt != "" {
		conditions = append(conditions, "orders.created_at >= ?")
		args = append(args, filter.StartAt)
	}
	if filter.EndBefore != "" {
		conditions = append(conditions, "orders.created_at < ?")
		args = append(args, filter.EndBefore)
	}
	if filter.UserID != 0 {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM cookies WHERE cookies.id = orders.cookie_id AND cookies.user_id = ?)")
		args = append(args, filter.UserID)
	}
	if len(filter.Statuses) > 0 {
		// placeholders 是状态条件需要的占位符列表。
		placeholders := make([]string, len(filter.Statuses))
		// index 是当前状态占位符的序号。
		for index := range filter.Statuses {
			placeholders[index] = "?"
			args = append(args, filter.Statuses[index])
		}
		conditions = append(conditions, "orders.order_status IN ("+strings.Join(placeholders, ",")+")")
	}
	// where 是带 WHERE 前缀和尾随空格的条件文本。
	where := "WHERE " + strings.Join(conditions, " AND ") + " "
	// cents 是当前方言的整数分金额表达式；格式非法或超过 money.MaxOrderAmountCents 的金额为 NULL，被 amountFilter 整单排除。
	cents := amountCentsExpression(r.store.Orders.Dialect, amountColumn)
	return where, args, cents, " AND " + cents + " IS NOT NULL"
}

// amountCentsExpression 返回当前数据库方言的整数分金额表达式：先用安全金额表达式得到元，再乘 100 四舍五入为整数，
// 使 SUM 在数据库内按整数累加。两位小数的金额乘 100 后距整数不超过 1e-6，四舍五入结果精确。
func amountCentsExpression(dialect db.Dialect, column string) string {
	// yuan 是通过格式与范围校验的元金额表达式，不合格时为 NULL。
	yuan := amountExpression(dialect, column)
	switch dialect {
	case db.DialectPostgres:
		return `CAST(ROUND(` + yuan + ` * 100) AS BIGINT)`
	case db.DialectMySQL:
		return `CAST(ROUND(` + yuan + ` * 100) AS SIGNED)`
	default:
		return `CAST(ROUND(` + yuan + ` * 100) AS INTEGER)`
	}
}

// maxOrderYuanText 是订单金额上限（元）的十进制文本，来自 money.MaxOrderAmountCents，用于拼入 SQL 范围条件。
var maxOrderYuanText = strconv.FormatInt(money.MaxOrderAmountCents/100, 10)

// amountExpression 返回当前数据库方言的安全金额表达式（单位：元）：
// 仅接受 `整数[.小数]` 格式且不超过 money.MaxOrderAmountCents 的金额，其余返回 NULL。
// PostgreSQL 与 MySQL 的正则限制整数部分最多 12 位，避免超长数字文本在 CAST 时报错；SQLite 的 CAST 对超长文本不会报错，由上限比较排除。
func amountExpression(dialect db.Dialect, column string) string {
	// clean 是移除货币符号和千分位后的金额文本表达式。
	clean := `TRIM(REPLACE(REPLACE(` + column + `, '¥', ''), ',', ''))`
	switch dialect {
	case db.DialectPostgres:
		return `CASE WHEN ` + clean + ` ~ '^[0-9]{1,12}([.][0-9]+)?$' THEN CASE WHEN CAST(` + clean + ` AS DOUBLE PRECISION) <= ` + maxOrderYuanText + ` THEN CAST(` + clean + ` AS DOUBLE PRECISION) END END`
	case db.DialectMySQL:
		return `CASE WHEN ` + clean + ` REGEXP '^[0-9]{1,12}([.][0-9]+)?$' THEN CASE WHEN CAST(` + clean + ` AS DOUBLE) <= ` + maxOrderYuanText + ` THEN CAST(` + clean + ` AS DOUBLE) END END`
	default:
		return `CASE WHEN ` + clean + ` GLOB '[0-9]*' AND ` + clean + ` NOT GLOB '*[^0-9.]*' AND ` + clean + ` NOT GLOB '*.*.*' AND ` + clean + ` NOT LIKE '%.' AND CAST(` + clean + ` AS REAL) <= ` + maxOrderYuanText + ` THEN CAST(` + clean + ` AS REAL) END`
	}
}

// 确保基础设施适配器实现完整的订单分析应用 Port。
var _ analyticsapp.Repository = (*AnalyticsRepository)(nil)
