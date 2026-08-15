package orders

import (
	"context"
	"errors"
)

// ErrRefreshJobNotFound 表示当前用户无法读取指定订单刷新任务。
var ErrRefreshJobNotFound = errors.New("订单刷新任务不存在")

// RefreshJob 是订单刷新后台任务的应用层模型，不暴露数据库类型。
type RefreshJob struct {
	// ID 是任务唯一标识。
	ID string
	// UserID 是任务所属用户标识。
	UserID int64
	// CookieID 是可选的目标账号标识。
	CookieID string
	// FilterStatus 是订单状态筛选条件。
	FilterStatus string
	// Status 是 queued/running/succeeded/failed/cancelled 之一。
	Status string
	// ResultJSON 保存成功后的具名刷新结果 JSON。
	ResultJSON string
	// ErrorMessage 保存任务失败原因。
	ErrorMessage string
	// WorkerToken 是当前执行者租约令牌。
	WorkerToken string
	// LeaseExpiresAt 是租约过期 Unix 秒时间戳。
	LeaseExpiresAt int64
	// CreatedAt 是任务创建时间。
	CreatedAt string
	// UpdatedAt 是任务最后更新时间。
	UpdatedAt string
}

// RefreshJobRepository 定义订单刷新任务需要的持久化能力。
type RefreshJobRepository interface {
	// Create 创建一个 queued 状态的订单刷新任务。
	Create(ctx context.Context, job *RefreshJob) error
	// Get 按用户读取订单刷新任务。
	Get(ctx context.Context, userID int64, id string) (*RefreshJob, error)
	// Claim 原子抢占 queued 任务并写入租约令牌。
	Claim(ctx context.Context, id, token string, leaseExpiresAt int64) (bool, error)
	// Complete 在租约令牌匹配时写入任务终态。
	Complete(ctx context.Context, id, token, status, resultJSON, errorMessage string) (bool, error)
	// Recoverable 返回租约已过期的 running 任务。
	Recoverable(ctx context.Context, now int64, limit int) ([]RefreshJob, error)
	// RequeueExpired 将过期运行任务恢复为 queued。
	RequeueExpired(ctx context.Context, id string, now int64) (bool, error)
}
