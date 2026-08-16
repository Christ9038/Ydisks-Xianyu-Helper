// Package admin 提供管理员用户与全局统计的应用用例，不依赖 HTTP 或数据库模型。
package admin

import (
	"context"
	"errors"
)

// ErrInvalidUser 表示请求没有提供有效的管理员身份。
var ErrInvalidUser = errors.New("管理员身份无效")

// ErrSelfDelete 表示管理员试图删除当前会话对应的用户。
var ErrSelfDelete = errors.New("不能删除当前登录用户")

// UserSummary 是管理员用户列表使用的非敏感摘要。
type UserSummary struct {
	// ID 是用户稳定标识。
	ID int64
	// Username 是用户登录名。
	Username string
	// Email 是用户联系邮箱。
	Email string
	// IsActive 表示用户是否启用。
	IsActive bool
	// IsAdmin 表示用户是否拥有管理员权限。
	IsAdmin bool
	// CreatedAt 是用户创建时间文本。
	CreatedAt string
	// CookieCount 是用户拥有的账号数量。
	CookieCount int
}

// Stats 是管理员仪表盘的全局聚合结果。
type Stats struct {
	// TotalUsers 是用户总数。
	TotalUsers int64
	// TotalCookies 是账号总数。
	TotalCookies int64
	// ActiveCookies 是启用账号总数。
	ActiveCookies int64
	// TotalCards 是卡券组总数。
	TotalCards int64
	// TotalKeywords 是关键词规则总数。
	TotalKeywords int64
	// TotalOrders 是未删除订单总数。
	TotalOrders int64
}

// Repository 定义管理员用例需要的最小持久化能力。
type Repository interface {
	// ListUsers 返回不包含密码和凭证的用户摘要。
	ListUsers(context.Context) ([]UserSummary, error)
	// DeleteUser 删除用户及其由数据库层管理的关联资源。
	DeleteUser(context.Context, int64) error
	// Stats 返回管理员仪表盘聚合计数。
	Stats(context.Context) (Stats, error)
}

// Service 编排管理员用户管理和仪表盘查询。
type Service struct {
	// repository 保存管理员用例的窄持久化端口。
	repository Repository
}

// NewService 构造管理员应用服务。
func NewService(repository Repository) *Service {
	return &Service{repository: repository}
}

// ListUsers 查询管理员用户摘要。
func (s *Service) ListUsers(ctx context.Context) ([]UserSummary, error) {
	if s == nil || s.repository == nil {
		return nil, errors.New("管理员服务未初始化")
	}
	return s.repository.ListUsers(ctx)
}

// DeleteUser 删除目标用户；服务层拒绝删除当前会话用户，避免 HTTP 层重复实现规则。
func (s *Service) DeleteUser(ctx context.Context, currentUserID, targetUserID int64) error {
	if currentUserID <= 0 || targetUserID <= 0 {
		return ErrInvalidUser
	}
	if currentUserID == targetUserID {
		return ErrSelfDelete
	}
	if s == nil || s.repository == nil {
		return errors.New("管理员服务未初始化")
	}
	return s.repository.DeleteUser(ctx, targetUserID)
}

// Stats 查询管理员仪表盘统计。
func (s *Service) Stats(ctx context.Context) (Stats, error) {
	if s == nil || s.repository == nil {
		return Stats{}, errors.New("管理员服务未初始化")
	}
	return s.repository.Stats(ctx)
}
