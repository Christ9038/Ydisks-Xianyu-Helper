// Package automation 提供自动化业务用例的纯应用层编排。
package automation

import (
	"context"
	"errors"
	"strings"
)

// CredentialWakeRepository 定义凭证恢复后唤醒账号任务所需的最小端口。
type CredentialWakeRepository interface {
	// WakeCredentialBlocked 唤醒指定账号因凭证失效而暂停的自动化任务。
	WakeCredentialBlocked(context.Context, string) error
}

// CredentialWakeService 编排凭证恢复后的自动化任务唤醒，不依赖数据库或 HTTP 类型。
type CredentialWakeService struct {
	// repository 提供自动化任务唤醒的持久化能力。
	repository CredentialWakeRepository
}

// NewCredentialWakeService 构造凭证恢复唤醒应用服务并校验依赖。
func NewCredentialWakeService(repository CredentialWakeRepository) (*CredentialWakeService, error) {
	if repository == nil {
		return nil, errors.New("凭证恢复唤醒 repository 未初始化")
	}
	return &CredentialWakeService{repository: repository}, nil
}

// WakeCredentialBlocked 唤醒指定账号的凭证阻塞任务，并拒绝空账号标识。
func (s *CredentialWakeService) WakeCredentialBlocked(ctx context.Context, accountID string) error {
	if s == nil || s.repository == nil {
		return errors.New("凭证恢复唤醒服务未初始化")
	}
	accountID = strings.TrimSpace(accountID)
	if accountID == "" {
		return errors.New("账号标识不能为空")
	}
	return s.repository.WakeCredentialBlocked(ctx, accountID)
}
