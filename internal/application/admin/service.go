// Package admin 提供管理员用户与全局统计的应用用例，不依赖 HTTP 或数据库模型。
package admin

import (
	"context"
	"errors"
	"fmt"
)

// ErrInvalidUser 表示请求没有提供有效的管理员身份。
var ErrInvalidUser = errors.New("管理员身份无效")

// ErrSelfDelete 表示管理员试图删除当前会话对应的用户。
var ErrSelfDelete = errors.New("不能删除当前登录用户")

// ErrRuntimeStop 表示删除用户前停止其账号运行实例失败；此时持久化删除不会继续执行。
var ErrRuntimeStop = errors.New("停止用户账号运行时失败")

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
	// ListOwnedAccountIDs 返回用户拥有的账号 ID，不读取 Cookie 或其他凭证内容。
	ListOwnedAccountIDs(context.Context, int64) ([]string, error)
	// DeleteUser 删除用户及其由数据库层管理的关联资源。
	DeleteUser(context.Context, int64) error
	// Stats 返回管理员仪表盘聚合计数。
	Stats(context.Context) (Stats, error)
}

// Runtime 定义管理员删除用户前停止账号运行实例所需的最小能力。
// 实现不得暴露 account.Manager 或其他运行时具体类型。
type Runtime interface {
	// StopContext 在调用方 Context 约束内停止一个账号并等待其运行协程退出。
	StopContext(context.Context, string) error
}

// Service 编排管理员用户管理和仪表盘查询。
type Service struct {
	// repository 保存管理员用例的窄持久化端口。
	repository Repository
	// runtime 保存管理员删除用户前收束账号运行实例的窄运行时端口；为空时兼容无运行时的离线用例。
	runtime Runtime
}

// NewService 构造管理员应用服务。
func NewService(repository Repository) *Service {
	return NewServiceWithRuntime(repository, nil)
}

// NewServiceWithRuntime 构造管理员应用服务并注入账号运行时收束能力。
func NewServiceWithRuntime(repository Repository, runtime Runtime) *Service {
	return &Service{repository: repository, runtime: runtime}
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
	// accountIDs、listErr 保存目标用户拥有的账号 ID 及读取错误；查询只返回非敏感标识。
	accountIDs, listErr := s.repository.ListOwnedAccountIDs(ctx, targetUserID)
	if listErr != nil {
		return listErr
	}
	if s.runtime != nil {
		// accountID 表示当前待收束的目标用户账号；停止失败会阻止用户及其关联数据删除。
		for _, accountID := range accountIDs {
			// stopErr 表示当前账号运行实例停止或等待退出时的错误。
			if stopErr := s.runtime.StopContext(ctx, accountID); stopErr != nil {
				return fmt.Errorf("%w: %w", ErrRuntimeStop, stopErr)
			}
		}
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
