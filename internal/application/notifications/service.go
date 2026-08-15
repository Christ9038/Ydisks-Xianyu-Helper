// Package notifications 提供通知运维查询等用例的应用层编排，不依赖 HTTP 或数据库模型。
package notifications

import (
	"context"
	"errors"
)

// ErrInvalidInput 表示通知运维查询缺少有效的用户归属或持久化端口。
var ErrInvalidInput = errors.New("通知运维查询参数无效")

// UncertainSummary 是外部通知已发送但本地确认失败时的非敏感摘要。
// 该模型不携带通知正文、渠道配置或错误原文，允许安全地传递到 HTTP 层。
type UncertainSummary struct {
	// ID 是通知 outbox 记录的稳定标识，仅用于运维定位。
	ID int64
	// ChannelID 是关联通知渠道标识，用于展示归属线索。
	ChannelID int64
	// OwnerUserID 是通知渠道所属用户；普通用户结果由应用服务保证只包含请求用户。
	OwnerUserID int64
	// EventType 是通知事件分类，不包含通知正文。
	EventType string
	// AttemptCount 是进入不确定状态前的发送尝试次数。
	AttemptCount int
	// UncertainAt 是进入不确定状态的 Unix 秒时间戳。
	UncertainAt int64
	// HasError 表示是否记录过本地确认错误，但不暴露错误原文。
	HasError bool
}

// Repository 定义通知不确定状态查询所需的最小持久化端口。
type Repository interface {
	// ListUncertainForUser 查询指定用户渠道的不确定通知摘要。
	ListUncertainForUser(ctx context.Context, userID int64, limit int) ([]UncertainSummary, error)
	// CountUncertainForUser 统计指定用户渠道的不确定通知数量。
	CountUncertainForUser(ctx context.Context, userID int64) (int, error)
	// ListUncertainForAdmin 查询所有用户渠道的不确定通知摘要。
	ListUncertainForAdmin(ctx context.Context, limit int) ([]UncertainSummary, error)
	// CountUncertainForAdmin 统计所有用户渠道的不确定通知数量。
	CountUncertainForAdmin(ctx context.Context) (int, error)
}

// Service 编排通知不确定状态查询，不持有 HTTP 请求或数据库连接。
type Service struct {
	// repository 保存调用方注入的最小通知查询端口。
	repository Repository
}

// New 创建通知不确定状态应用服务；空端口会导致构造结果不可用。
func New(repository Repository) *Service {
	return &Service{repository: repository}
}

// ListForUser 查询当前用户有权查看的不确定通知摘要及总数。
// userID 用于归属隔离，limit 控制列表上限；底层端口错误原样返回且不会泄露正文。
func (s *Service) ListForUser(ctx context.Context, userID int64, limit int) ([]UncertainSummary, int, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, 0, ErrInvalidInput
	}
	// items 保存按用户归属过滤后的非敏感通知摘要。
	items, err := s.repository.ListUncertainForUser(ctx, userID, limit)
	if err != nil {
		return nil, 0, err
	}
	// total 保存当前用户所有不确定通知的数量。
	total, err := s.repository.CountUncertainForUser(ctx, userID)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// ListForAdmin 查询管理员可见的全局不确定通知摘要及总数。
// limit 控制列表上限；结果仍只包含脱敏元数据，底层端口错误原样返回。
func (s *Service) ListForAdmin(ctx context.Context, limit int) ([]UncertainSummary, int, error) {
	if s == nil || s.repository == nil {
		return nil, 0, ErrInvalidInput
	}
	// items 保存全局非敏感通知摘要列表。
	items, err := s.repository.ListUncertainForAdmin(ctx, limit)
	if err != nil {
		return nil, 0, err
	}
	// total 保存全局不确定通知的数量。
	total, err := s.repository.CountUncertainForAdmin(ctx)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}
