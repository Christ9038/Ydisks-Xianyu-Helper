package account

import "context"

// LoginStatusPort 定义登录成功后判断账号是否启用所需的最小状态查询能力。
type LoginStatusPort interface {
	// GetStatus 返回账号当前是否启用；该查询不读取或解密凭证内容。
	GetStatus(context.Context, string) bool
}

// LoginRestartPort 定义登录成功后重启账号运行时所需的最小能力。
type LoginRestartPort interface {
	// Restart 使用刚写入的持久化凭证重启账号运行实例。
	Restart(context.Context, string) error
}

// LoginSuccessService 编排登录成功后的资料刷新与运行时重启，不依赖 HTTP 或 Server。
type LoginSuccessService struct {
	// summaries 提供不含秘密的账号归属确认能力。
	summaries ProfileSummaryRepository
	// profile 负责平台资料刷新和本地资料持久化。
	profile *ProfileService
	// statuses 提供账号启用状态查询能力。
	statuses LoginStatusPort
	// runtime 提供账号运行时重启能力。
	runtime LoginRestartPort
	// report 记录不含凭证的后续动作错误；为空时忽略诊断。
	report func(string, error)
}

// NewLoginSuccessService 构造登录成功后的应用编排服务。
func NewLoginSuccessService(summaries ProfileSummaryRepository, profile *ProfileService, statuses LoginStatusPort, runtime LoginRestartPort, report func(string, error)) *LoginSuccessService {
	return &LoginSuccessService{summaries: summaries, profile: profile, statuses: statuses, runtime: runtime, report: report}
}

// AfterSuccessfulLogin 在凭证锁释放后刷新资料并按账号启用状态重启运行时。
func (s *LoginSuccessService) AfterSuccessfulLogin(ctx context.Context, userID int64, accountID string) {
	if s == nil {
		return
	}
	if s.summaries != nil {
		// summaryErr 保存登录成功后归属摘要复核错误；失败时跳过资料刷新但不阻断重启。
		_, summaryErr := s.summaries.GetOwnedSummary(ctx, userID, accountID)
		if summaryErr == nil && s.profile != nil {
			// result 和 profileErr 保存资料刷新结果及基础设施错误。
			result, profileErr := s.profile.RefreshProfile(ctx, userID, accountID)
			if profileErr != nil {
				s.reportError("登录后刷新账号资料失败", profileErr)
			} else if result.ErrorMessage != "" {
				s.reportError("登录后刷新账号资料返回业务错误", nil)
			}
		}
	}
	if s.statuses != nil && s.statuses.GetStatus(ctx, accountID) && s.runtime != nil {
		// restartErr 保存登录成功后运行时重启错误。
		if restartErr := s.runtime.Restart(ctx, accountID); restartErr != nil {
			s.reportError("账号登录后重启账号失败", restartErr)
		}
	}
}

// reportError 统一传递登录成功后续动作的脱敏诊断。
func (s *LoginSuccessService) reportError(message string, err error) {
	if s.report != nil {
		s.report(message, err)
	}
}
