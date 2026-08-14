package db

import (
	"context"
	"database/sql"
)

// AnalyticsQueries 提供订单分析应用服务使用的只读查询执行边界。
type AnalyticsQueries struct {
	// DB 是订单分析查询使用的数据库连接。
	DB *sql.DB
}

// QueryRowContext 执行单行订单分析查询。
func (q *AnalyticsQueries) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return q.DB.QueryRowContext(ctx, query, args...)
}

// QueryContext 执行多行订单分析查询。
func (q *AnalyticsQueries) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return q.DB.QueryContext(ctx, query, args...)
}
