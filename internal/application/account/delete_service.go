package account

import (
	"context"
	"errors"
	"time"
)

// ErrDeleteConflict 表示账号运行时尚未收束，删除操作不会覆盖仍存活的实例。
var ErrDeleteConflict = errors.New("账号正在停止，请稍后重试")

// DeleteSummaryRepository 定义账号删除用例所需的最小非敏感持久化端口。
type DeleteSummaryRepository interface {
	// GetOwnedSummary 按用户和账号联合查询非敏感摘要，用于删除前归属确认。
	GetOwnedSummary(context.Context, int64, string) (Summary, error)
	// DeleteOwned 在基础设施边界再次确认归属后删除账号及其关联数据。
	DeleteOwned(context.Context, int64, string) error
}

// DeleteRuntime 定义账号删除期间的运行时 fencing 与收束端口。
type DeleteRuntime interface {
	// BeginStopping 建立账号级 fencing，阻止删除期间新的运行实例启动。
	BeginStopping(string) bool
	// StopContext 在调用方 Context 限制内停止运行实例并等待其退出。
	StopContext(context.Context, string) error
	// EndStopping 释放账号级 fencing，供失败或成功后的收束路径调用。
	EndStopping(string)
}

// DeleteService 编排账号归属校验、运行时 fencing 和关联数据删除。
type DeleteService struct {
	// repository 提供非敏感账号查询和按归属删除能力。
	repository DeleteSummaryRepository
	// runtime 提供可选的账号运行时停止能力；为空时仅执行持久化删除。
	runtime DeleteRuntime
}

// NewDeleteService 构造账号删除应用服务并校验必需的持久化端口。
func NewDeleteService(repository DeleteSummaryRepository, runtime DeleteRuntime) (*DeleteService, error) {
	if repository == nil {
		return nil, errors.New("账号删除 repository 未初始化")
	}
	return &DeleteService{repository: repository, runtime: runtime}, nil
}

// Delete 删除指定用户拥有的账号；运行时停止超时或 fencing 冲突时保留账号不变。
func (s *DeleteService) Delete(ctx context.Context, userID int64, accountID string) error {
	if s == nil || s.repository == nil {
		return errors.New("账号删除服务未初始化")
	}
	if accountID == "" {
		return errors.New("缺少账号 ID")
	}
	// summary 保存删除前通过归属校验的非敏感账号摘要；凭证字段不会被读取。
	if _, err := s.repository.GetOwnedSummary(ctx, userID, accountID); err != nil {
		return err
	}
	if s.runtime != nil {
		if !s.runtime.BeginStopping(accountID) {
			return ErrDeleteConflict
		}
		defer s.runtime.EndStopping(accountID)
		// stopErr 保存运行时在调用方 Context 内收束账号实例的结果。
		// stopCtx、stopCancel 将删除等待限制在最多五秒，避免无截止请求永久占用 fencing。
		stopCtx, stopCancel := context.WithTimeout(ctx, 5*time.Second)
		defer stopCancel()
		// stopErr 保存运行时停止结果；任何停止失败都阻止后续删除。
		if err := s.runtime.StopContext(stopCtx, accountID); err != nil {
			return ErrDeleteConflict
		}
	}
	// deleteErr 在基础设施边界重新确认归属并删除关联数据，避免停止期间账号被替换后误删。
	if deleteErr := s.repository.DeleteOwned(ctx, userID, accountID); deleteErr != nil {
		return deleteErr
	}
	return nil
}
