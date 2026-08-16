// Package account 提供账号资料与登录相关的应用用例及消费者定义的端口。
package account

import (
	"context"
	"errors"
)

// ErrNotFound 表示资料刷新目标账号不存在。
var ErrNotFound = errors.New("账号不存在")

// ErrForbidden 表示当前用户无权刷新目标账号资料。
var ErrForbidden = errors.New("无权刷新账号资料")

// Summary 是资料刷新用例所需的非敏感账号摘要，不包含 Cookie、密码或加密 metadata。
type Summary struct {
	// ID 是平台账号在本地的稳定标识。
	ID string
	// UserID 是账号所属的本地用户标识。
	UserID int64
	// Remark 是用户设置的账号备注，用于资料刷新失败时的展示兜底。
	Remark string
	// Nickname 是本地缓存的平台昵称，用于资料刷新失败时的展示兜底。
	Nickname string
	// AvatarURL 是本地缓存的平台头像地址，用于资料刷新失败时的展示兜底。
	AvatarURL string
	// AutoConfirm 表示账号是否启用自动确认收货。
	AutoConfirm bool
	// PauseDuration 是账号暂停时长，单位为分钟。
	PauseDuration int
	// PausedUntil 是暂停结束时间的 Unix 秒；零值表示当前未暂停。
	PausedUntil int64
	// Username 是账号关联的登录用户名，不包含登录密码。
	Username string
	// ShowBrowser 表示密码登录流程是否允许显示浏览器。
	ShowBrowser bool
	// LastRefreshAt 是最近一次资料刷新时间，单位为 Unix 秒。
	LastRefreshAt int64
	// LoginMethod 是最近一次成功登录方式，不包含凭证内容。
	LoginMethod string
	// LastLoginAt 是最近一次成功登录时间，单位为 Unix 秒。
	LastLoginAt int64
	// CreatedAt 是账号记录创建时间的数据库字符串。
	CreatedAt string
}

// ProfileInput 是平台资料刷新端口的输入，携带已通过归属校验的账号摘要。
type ProfileInput struct {
	// UserID 是当前发起资料刷新的用户标识。
	UserID int64
	// AccountID 是待刷新的账号标识。
	AccountID string
	// Summary 是已确认归属的非敏感账号摘要。
	Summary Summary
}

// ProfileResult 是资料刷新用例返回的可展示结果，不包含任何凭证字段。
type ProfileResult struct {
	// AccountID 是完成资料刷新的账号标识。
	AccountID string
	// Nickname 是平台返回或本地兜底的昵称。
	Nickname string
	// AvatarURL 是平台返回或本地兜底的头像地址。
	AvatarURL string
	// ErrorMessage 是平台请求或资料保存失败时的可展示错误；非空不代表本地用例失败。
	ErrorMessage string
}

// ProfileSummaryRepository 定义资料刷新用例读取账号归属和摘要所需的最小端口。
type ProfileSummaryRepository interface {
	// GetOwnedSummary 按用户和账号联合查询非敏感摘要；不存在或越权时返回错误。
	GetOwnedSummary(context.Context, int64, string) (Summary, error)
}

// ProfilePort 定义平台资料刷新及本地资料保存所需的适配端口。
type ProfilePort interface {
	// RefreshProfile 调用平台并保存资料；平台业务失败通过 ProfileResult.ErrorMessage 返回。
	RefreshProfile(context.Context, ProfileInput) (ProfileResult, error)
}

// ProfileService 编排账号归属校验、平台资料刷新和可展示结果转换。
type ProfileService struct {
	// repository 提供不解密凭证的账号摘要查询能力。
	repository ProfileSummaryRepository
	// profilePort 承担平台调用、凭证会话更新和资料持久化适配。
	profilePort ProfilePort
}

// NewProfileService 构造资料刷新应用服务并校验必需端口。
func NewProfileService(repository ProfileSummaryRepository, profilePort ProfilePort) (*ProfileService, error) {
	if repository == nil {
		return nil, errors.New("账号资料摘要 repository 未初始化")
	}
	if profilePort == nil {
		return nil, errors.New("账号资料刷新端口未初始化")
	}
	return &ProfileService{repository: repository, profilePort: profilePort}, nil
}

// RefreshProfile 执行一次按用户归属校验的账号资料刷新用例。
func (s *ProfileService) RefreshProfile(ctx context.Context, userID int64, accountID string) (ProfileResult, error) {
	if s == nil || s.repository == nil || s.profilePort == nil {
		return ProfileResult{}, errors.New("账号资料刷新服务未初始化")
	}
	// summary 保存通过所有权查询得到的非敏感账号摘要。
	summary, err := s.repository.GetOwnedSummary(ctx, userID, accountID)
	if err != nil {
		return ProfileResult{}, err
	}
	// result 保存平台资料刷新端口转换后的可展示结果。
	result, err := s.profilePort.RefreshProfile(ctx, ProfileInput{UserID: userID, AccountID: accountID, Summary: summary})
	if err != nil {
		return ProfileResult{}, err
	}
	if result.AccountID == "" {
		result.AccountID = accountID
	}
	return result, nil
}
