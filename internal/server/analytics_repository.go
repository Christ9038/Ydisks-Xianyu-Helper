package server

import (
	"context"
	"database/sql"

	"xianyu-go/internal/db"
)

// analyticsRepository 定义分析服务执行只读 SQL、读取卡密库存和获取数据库方言所需的最小能力。
type analyticsRepository interface {
	// QueryRowContext 执行返回单行结果的分析查询。
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
	// QueryContext 执行返回多行结果的分析查询。
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	// ListCardsForUser 返回用户拥有的卡密组。
	ListCardsForUser(ctx context.Context, userID int64) ([]db.CardFull, error)
	// Dialect 返回当前数据库方言。
	Dialect() db.Dialect
}

// storeAnalyticsRepository 将完整 Store 适配为分析服务窄 repository。
type storeAnalyticsRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// QueryRowContext 委托单行分析查询。
func (r storeAnalyticsRepository) QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row {
	return r.store.Analytics.QueryRowContext(ctx, query, args...)
}

// QueryContext 委托多行分析查询。
func (r storeAnalyticsRepository) QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error) {
	return r.store.Analytics.QueryContext(ctx, query, args...)
}

// ListCardsForUser 委托用户卡密组查询。
func (r storeAnalyticsRepository) ListCardsForUser(ctx context.Context, userID int64) ([]db.CardFull, error) {
	return r.store.Cards.AllForUser(ctx, userID)
}

// Dialect 返回 Store 使用的数据库方言。
func (r storeAnalyticsRepository) Dialect() db.Dialect {
	return r.store.Dialect
}

// newStoreAnalyticsRepository 从完整 Store 构造分析服务窄 repository。
func newStoreAnalyticsRepository(store *db.Store) analyticsRepository {
	if store == nil || store.Analytics == nil || store.Cards == nil {
		return nil
	}
	return storeAnalyticsRepository{store: store}
}

// 确保 Store 适配器始终覆盖分析服务所需的全部能力。
var _ analyticsRepository = storeAnalyticsRepository{}
