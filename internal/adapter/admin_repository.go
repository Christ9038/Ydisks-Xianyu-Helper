package adapter

import (
	"context"
	"errors"

	adminapp "xianyu-go/internal/application/admin"
	"xianyu-go/internal/db"
)

// AdminRepository 将管理员数据库查询转换为非敏感应用模型。
type AdminRepository struct {
	// store 保存管理员查询所需的数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// NewAdminRepository 构造管理员数据库适配器。
func NewAdminRepository(store *db.Store) *AdminRepository {
	return &AdminRepository{store: store}
}

// ListUsers 查询用户摘要并丢弃密码及其他敏感字段。
func (r *AdminRepository) ListUsers(ctx context.Context) ([]adminapp.UserSummary, error) {
	if r == nil || r.store == nil || r.store.Admin == nil {
		return nil, errors.New("管理员查询适配器未初始化")
	}
	// rows、err 保存数据库用户摘要行及查询错误。
	rows, err := r.store.Admin.ListUsers(ctx)
	if err != nil {
		return nil, err
	}
	// users 保存已脱敏的应用层用户摘要。
	users := make([]adminapp.UserSummary, 0, len(rows))
	// row 表示当前待转换的数据库用户摘要。
	for _, row := range rows {
		users = append(users, adminapp.UserSummary{ID: row.ID, Username: row.Username, Email: row.Email, IsActive: row.IsActive, IsAdmin: row.IsAdmin, CreatedAt: row.CreatedAt, CookieCount: row.CookieCount})
	}
	return users, nil
}

// DeleteUser 删除指定用户及其关联账号。
func (r *AdminRepository) DeleteUser(ctx context.Context, userID int64) error {
	if r == nil || r.store == nil || r.store.Users == nil {
		return errors.New("管理员删除适配器未初始化")
	}
	return r.store.Users.Delete(ctx, userID)
}

// Stats 查询数据库聚合统计并转换为应用模型。
func (r *AdminRepository) Stats(ctx context.Context) (adminapp.Stats, error) {
	if r == nil || r.store == nil || r.store.Admin == nil {
		return adminapp.Stats{}, errors.New("管理员统计适配器未初始化")
	}
	// stats、err 保存数据库聚合统计及查询错误。
	stats, err := r.store.Admin.Stats(ctx)
	if err != nil {
		return adminapp.Stats{}, err
	}
	return adminapp.Stats{TotalUsers: stats.TotalUsers, TotalCookies: stats.TotalCookies, ActiveCookies: stats.ActiveCookies, TotalCards: stats.TotalCards, TotalKeywords: stats.TotalKeywords, TotalOrders: stats.TotalOrders}, nil
}

var _ adminapp.Repository = (*AdminRepository)(nil)
