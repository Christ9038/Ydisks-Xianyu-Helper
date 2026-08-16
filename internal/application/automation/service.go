// Package automation 提供自动化异常运维查询与人工处理用例，不依赖 HTTP 或数据库模型。
package automation

import (
	"context"
	"errors"
	"strings"
)

// ErrInvalidInput 表示自动化异常用例缺少有效的用户或持久化端口。
var ErrInvalidInput = errors.New("自动化异常参数无效")

// ErrNotFound 表示异常记录不存在、已处理或不属于当前用户。
var ErrNotFound = errors.New("自动化异常不存在或已处理")

// ErrInvalidDeferredResolution 表示延期任务只能执行重试或驳回处理。
var ErrInvalidDeferredResolution = errors.New("处理方式必须是 retry 或 dismiss")

// RunIssue 是需要人工处理的自动化运行非敏感摘要。
type RunIssue struct {
	// ID 是自动化运行的稳定标识。
	ID int64
	// CookieID 是关联账号标识，不包含 Cookie 内容。
	CookieID string
	// OrderID 是关联订单标识。
	OrderID string
	// TriggerType 是触发该运行的事件类型。
	TriggerType string
	// ErrorMessage 是运行进入人工处理状态时记录的原因。
	ErrorMessage string
	// IssueKind 是应用层根据运行事实归类的异常类型。
	IssueKind string
	// AllowedResolutions 是当前异常允许的人工处理动作。
	AllowedResolutions []string
	// ActionCursor 是下一步动作在计划中的位置。
	ActionCursor int
	// SentCount 是已经确认成功的外部动作数量。
	SentCount int
	// UpdatedAt 是运行状态最近更新的时间文本。
	UpdatedAt string
}

// DeferredIssue 是需要人工处理的延期任务非敏感摘要。
type DeferredIssue struct {
	// ID 是延期任务的稳定标识。
	ID int64
	// CookieID 是关联账号标识，不包含 Cookie 内容。
	CookieID string
	// TriggerType 是触发延期任务的事件类型。
	TriggerType string
	// ErrorMessage 是任务进入死信状态时记录的原因。
	ErrorMessage string
	// AttemptCount 是任务已经尝试执行的次数。
	AttemptCount int
	// UpdatedAt 是任务状态最近更新的时间文本。
	UpdatedAt string
}

// IssueRepository 定义自动化异常用例所需的最小持久化能力。
type IssueRepository interface {
	// ListIssues 按用户归属查询异常运行和死信延期任务摘要。
	ListIssues(ctx context.Context, userID int64) ([]RunIssue, []DeferredIssue, error)
	// ResolveRunIssue 按用户归属执行异常运行的人工处理动作。
	ResolveRunIssue(ctx context.Context, userID, runID int64, resolution string) error
	// ResolveDeferredIssue 按用户归属重试或删除死信延期任务。
	ResolveDeferredIssue(ctx context.Context, userID, taskID int64, retry bool) error
}

// IssueService 编排自动化异常查询与人工处理，不持有 HTTP 请求或数据库连接。
type IssueService struct {
	// repository 保存调用方注入的最小自动化异常持久化端口。
	repository IssueRepository
}

// NewIssueService 构造自动化异常应用服务；空端口会在调用时返回参数错误。
func NewIssueService(repository IssueRepository) *IssueService {
	return &IssueService{repository: repository}
}

// ListIssues 查询当前用户有权查看的异常运行和死信延期任务摘要。
func (s *IssueService) ListIssues(ctx context.Context, userID int64) ([]RunIssue, []DeferredIssue, error) {
	if s == nil || s.repository == nil || userID <= 0 {
		return nil, nil, ErrInvalidInput
	}
	return s.repository.ListIssues(ctx, userID)
}

// ResolveRunIssue 处理异常运行；resolution 的允许值由持久化端口依据运行事实判定。
func (s *IssueService) ResolveRunIssue(ctx context.Context, userID, runID int64, resolution string) error {
	if s == nil || s.repository == nil || userID <= 0 || runID <= 0 {
		return ErrInvalidInput
	}
	return s.repository.ResolveRunIssue(ctx, userID, runID, strings.TrimSpace(resolution))
}

// ResolveDeferredIssue 处理死信延期任务，并将 retry/dismiss 转换为端口使用的布尔语义。
func (s *IssueService) ResolveDeferredIssue(ctx context.Context, userID, taskID int64, resolution string) error {
	if s == nil || s.repository == nil || userID <= 0 || taskID <= 0 {
		return ErrInvalidInput
	}
	// normalizedResolution 保存去除空白后的人工处理动作，避免兼容客户端的首尾空格改变语义。
	normalizedResolution := strings.TrimSpace(resolution)
	if normalizedResolution != "retry" && normalizedResolution != "dismiss" {
		return ErrInvalidDeferredResolution
	}
	return s.repository.ResolveDeferredIssue(ctx, userID, taskID, normalizedResolution == "retry")
}
