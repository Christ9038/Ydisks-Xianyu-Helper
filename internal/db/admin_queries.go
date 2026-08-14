package db

import (
	"context"
	"database/sql"
)

// AdminUserSummary 是管理员用户列表的持久化查询结果。
type AdminUserSummary struct {
	// ID 是用户标识。
	ID int64
	// Username 是用户名。
	Username string
	// Email 是用户邮箱。
	Email string
	// IsActive 表示用户是否启用。
	IsActive bool
	// IsAdmin 表示用户是否为管理员。
	IsAdmin bool
	// CreatedAt 是用户创建时间文本。
	CreatedAt string
	// CookieCount 是用户拥有的账号数量。
	CookieCount int
}

// AdminCookieSummary 是管理员账号列表的持久化查询结果。
type AdminCookieSummary struct {
	// ID 是账号标识。
	ID string
	// UserID 是账号所属用户标识。
	UserID int64
	// Remark 是账号备注。
	Remark string
	// CreatedAt 是账号创建时间文本。
	CreatedAt string
	// Owner 是所属用户名。
	Owner string
}

// AdminStats 是管理员仪表盘的聚合计数。
type AdminStats struct {
	// TotalUsers 是用户总数。
	TotalUsers int64
	// TotalCookies 是账号总数。
	TotalCookies int64
	// ActiveCookies 是启用账号总数。
	ActiveCookies int64
	// TotalCards 是卡密组总数。
	TotalCards int64
	// TotalKeywords 是关键词规则总数。
	TotalKeywords int64
	// TotalOrders 是未删除订单总数。
	TotalOrders int64
}

// AdminQueries 提供管理员专用的聚合查询。
type AdminQueries struct {
	// DB 是管理员查询使用的数据库连接。
	DB *sql.DB
}

// ListUsers 查询管理员用户列表及其账号数量。
func (q *AdminQueries) ListUsers(ctx context.Context) ([]AdminUserSummary, error) {
	// rows 和 err 是用户列表查询结果集及错误。
	rows, err := q.DB.QueryContext(ctx, `
		SELECT u.id, u.username, u.email, u.is_active, u.is_admin, u.created_at,
		       (SELECT COUNT(*) FROM cookies c WHERE c.user_id=u.id)
		  FROM users u ORDER BY u.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 是转换后的管理员用户摘要列表。
	var out []AdminUserSummary
	for rows.Next() {
		// item 是当前用户摘要。
		var item AdminUserSummary
		// active 和 admin 是数据库中的布尔整数。
		var active, admin int
		// err 是当前用户行扫描错误。
		if err := rows.Scan(&item.ID, &item.Username, &item.Email, &active, &admin, &item.CreatedAt, &item.CookieCount); err != nil {
			return nil, err
		}
		item.IsActive = active != 0
		item.IsAdmin = admin != 0
		out = append(out, item)
	}
	return out, rows.Err()
}

// ListCookies 查询管理员账号列表及所属用户名。
func (q *AdminQueries) ListCookies(ctx context.Context) ([]AdminCookieSummary, error) {
	// rows 和 err 是管理员账号查询结果集及错误。
	rows, err := q.DB.QueryContext(ctx, `
		SELECT c.id, c.user_id, COALESCE(c.remark,''), c.created_at, COALESCE(u.username,'')
		  FROM cookies c LEFT JOIN users u ON c.user_id=u.id ORDER BY c.created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// out 是转换后的管理员账号摘要列表。
	var out []AdminCookieSummary
	for rows.Next() {
		// item 是当前账号摘要。
		var item AdminCookieSummary
		// err 是当前账号行扫描错误。
		if err := rows.Scan(&item.ID, &item.UserID, &item.Remark, &item.CreatedAt, &item.Owner); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

// Stats 查询管理员仪表盘全部计数。
func (q *AdminQueries) Stats(ctx context.Context) (AdminStats, error) {
	// stats 是管理员统计结果。
	var stats AdminStats
	// queries 是管理员统计使用的固定 SQL 与目标字段。
	queries := []struct {
		query string
		dest  *int64
	}{
		{`SELECT COUNT(*) FROM users`, &stats.TotalUsers},
		{`SELECT COUNT(*) FROM cookies`, &stats.TotalCookies},
		{`SELECT COUNT(*) FROM cards`, &stats.TotalCards},
		{`SELECT COUNT(*) FROM orders WHERE deleted_at IS NULL`, &stats.TotalOrders},
		{`SELECT COUNT(*) FROM keywords`, &stats.TotalKeywords},
	}
	// item 是当前固定统计查询。
	for _, item := range queries {
		// err 是当前统计项查询错误。
		if err := q.DB.QueryRowContext(ctx, item.query).Scan(item.dest); err != nil {
			return AdminStats{}, err
		}
	}
	// err 是活跃账号统计查询错误。
	if err := q.DB.QueryRowContext(ctx, `
		SELECT COUNT(*) FROM cookies c
		 WHERE NOT EXISTS (SELECT 1 FROM cookie_status cs WHERE cs.cookie_id=c.id AND cs.enabled=0)
	`).Scan(&stats.ActiveCookies); err != nil {
		return AdminStats{}, err
	}
	return stats, nil
}
