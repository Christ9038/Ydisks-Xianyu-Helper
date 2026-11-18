package adapter

import (
	"context"
	"errors"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// CredentialVerificationStarter 定义创建人工安全验证会话的平台能力，由 qrlogin.Manager 实现。
// cookieHeader 与 snapshot 是账号当前凭证（snapshot 为 nil 表示没有完整快照），返回验证会话标识。
type CredentialVerificationStarter interface {
	// StartCredentialVerification 用账号当前凭证和平台验证地址创建验证会话。
	StartCredentialVerification(ctx context.Context, cookieHeader string, snapshot []cookierefresh.BrowserCookie, verificationURL string) (string, error)
}

// CredentialVerificationAdapter 实现 accountapp.CredentialVerificationPort：
// 读取账号最新凭证窄视图并创建验证会话。明文 Cookie 只在本次调用内存在，不进入日志或返回值。
type CredentialVerificationAdapter struct {
	// repository 提供凭证锁与平台凭证窄视图，不读取登录密码。
	repository accountapp.CredentialRepository
	// starter 是创建验证会话的平台能力。
	starter CredentialVerificationStarter
}

// NewCredentialVerificationAdapter 构造适配器并校验全部必需依赖。
func NewCredentialVerificationAdapter(repository accountapp.CredentialRepository, starter CredentialVerificationStarter) (*CredentialVerificationAdapter, error) {
	if repository == nil {
		return nil, errors.New("凭证验证凭证 repository 未初始化")
	}
	if starter == nil {
		return nil, errors.New("凭证验证会话创建能力未初始化")
	}
	return &CredentialVerificationAdapter{repository: repository, starter: starter}, nil
}

// StartCredentialVerification 在凭证锁内读取账号最新凭证，释放锁后创建验证会话。
// 续期响应的新 Cookie 已由调度器保存，这里读取的就是包含它们的最新凭证；会话创建不发网络请求，锁外执行以遵守锁规则。
func (a *CredentialVerificationAdapter) StartCredentialVerification(ctx context.Context, accountID, verificationURL string) (string, error) {
	if a == nil || a.repository == nil || a.starter == nil {
		return "", errors.New("凭证验证适配器未初始化")
	}
	// unlock 保护凭证读取与并发写回之间的一致性；会话创建在锁外进行。
	unlock := a.repository.LockCredentials(accountID)
	// detail、loadErr 是账号当前凭证窄视图及读取错误。
	detail, loadErr := a.repository.LoadPlatformDetail(ctx, accountID)
	unlock()
	if loadErr != nil {
		return "", loadErr
	}
	if detail == nil {
		return "", accountapp.ErrNotFound
	}
	// snapshot、complete 是账号完整 Cookie 快照及其是否完整；不完整时传 nil 让会话改用扁平 Cookie。
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !complete {
		snapshot = nil
	}
	return a.starter.StartCredentialVerification(ctx, detail.Value, snapshot, verificationURL)
}
