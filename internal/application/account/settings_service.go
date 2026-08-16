package account

import (
	"context"
	"errors"
)

// SettingsUpdateInput 是账号编辑用例的输入；Password 为 nil 时保留数据库中的既有密码。
type SettingsUpdateInput struct {
	// UserID 是当前认证用户的本地身份标识，用于数据库边界的归属复核。
	UserID int64
	// AccountID 是待更新的闲鱼账号稳定标识。
	AccountID string
	// Cookie 是可选的新 Cookie 明文，只在数据库适配器调用期间短暂存在。
	Cookie *string
	// Remark 是可选的账号备注更新值。
	Remark *string
	// AutoConfirm 是可选的自动确认发货开关。
	AutoConfirm *bool
	// PauseDuration 是可选的暂停时长，单位为分钟；零表示立即恢复。
	PauseDuration *int
	// Username 是可选的密码登录用户名更新值。
	Username *string
	// Password 是可选的密码登录秘密；空字符串用于明确清除密码。
	Password *string
	// ShowBrowser 是可选的密码登录浏览器显示开关。
	ShowBrowser *bool
	// ChannelIDs 是可选的通知渠道绑定集合；空切片表示解绑全部渠道。
	ChannelIDs *[]int64
}

// LoginInfoUpdateInput 是账号登录信息用例的输入，不暴露既有密码读取能力。
type LoginInfoUpdateInput struct {
	// UserID 是当前认证用户的本地身份标识。
	UserID int64
	// AccountID 是待更新账号的稳定标识。
	AccountID string
	// Username 是登录用户名；该接口保持旧 HTTP API 的空字符串覆盖语义。
	Username string
	// Password 是可选的新密码；nil 表示保留既有密码，空字符串表示清除密码。
	Password *string
	// ShowBrowser 表示登录时是否允许显示浏览器。
	ShowBrowser bool
}

// PauseState 是账号暂停查询的非敏感结果。
type PauseState struct {
	// Duration 是账号配置的暂停时长，单位为分钟。
	Duration int
	// PausedUntil 是暂停截止时间的 Unix 秒；零表示当前没有截止时间。
	PausedUntil int64
	// Paused 表示当前时间是否仍处于暂停窗口内。
	Paused bool
}

// SettingsResult 是账号设置写入后的业务结果。
type SettingsResult struct {
	// PausedUntil 是写入后暂停截止时间的 Unix 秒。
	PausedUntil int64
	// RuntimeError 是持久化成功后运行时重启失败的诊断信息；不会回滚数据库写入。
	RuntimeError error
	// TokenCleanupError 是 Cookie 写入成功后清理旧连接凭证的错误；不会回滚账号设置。
	TokenCleanupError error
}

// StatusResult 是账号启停写入后的业务结果。
type StatusResult struct {
	// RuntimeError 是状态写入成功后运行时启停失败的诊断信息。
	RuntimeError error
}

// SettingsRepository 定义账号设置用例需要的最小持久化端口。
type SettingsRepository interface {
	// LockCredentials 串行化同一账号的敏感设置写入；调用方必须及时释放返回的函数。
	LockCredentials(string) func()
	// UpdateSettings 原子更新账号设置并按 UserID 复核账号归属。
	UpdateSettings(context.Context, SettingsUpdateInput) (int64, error)
	// UpdateLoginInfo 原子更新用户名、密码和浏览器显示设置并按 UserID 复核归属。
	UpdateLoginInfo(context.Context, LoginInfoUpdateInput) error
	// SetStatusOwned 更新账号启用状态并按 UserID 复核归属。
	SetStatusOwned(context.Context, int64, string, bool, string) error
	// StatusOwned 查询指定用户账号的启用状态，不读取凭证明文。
	StatusOwned(context.Context, int64, string) (bool, error)
	// SetPauseOwned 更新账号暂停时长并按 UserID 复核归属。
	SetPauseOwned(context.Context, int64, string, int) (int64, error)
	// GetPauseOwned 查询指定用户账号的暂停状态，不读取凭证明文。
	GetPauseOwned(context.Context, int64, string) (PauseState, error)
	// ClearTokens 清理 Cookie 变更后失效的旧连接凭证。
	ClearTokens(context.Context, string) error
}

// SettingsRuntime 定义账号设置变更后运行实例的最小控制端口。
type SettingsRuntime interface {
	// Restart 在账号启用且 Cookie 变化后重新加载运行实例。
	Restart(context.Context, string) error
	// Stop 在账号停用后停止运行实例。
	Stop(string)
}

// SettingsService 编排账号设置、登录信息、状态和暂停用例，不依赖 HTTP 或数据库模型。
type SettingsService struct {
	// repository 提供账号设置的窄持久化能力。
	repository SettingsRepository
	// runtime 提供可选的账号运行实例控制能力。
	runtime SettingsRuntime
}

// NewSettingsService 构造账号设置应用服务并校验必需的持久化端口。
func NewSettingsService(repository SettingsRepository, runtime SettingsRuntime) (*SettingsService, error) {
	if repository == nil {
		return nil, errors.New("账号设置 repository 未初始化")
	}
	return &SettingsService{repository: repository, runtime: runtime}, nil
}

// UpdateSettings 原子保存账号设置；Cookie 变化后的运行时重启在释放凭证锁后执行。
func (s *SettingsService) UpdateSettings(ctx context.Context, input SettingsUpdateInput) (SettingsResult, error) {
	if s == nil || s.repository == nil {
		return SettingsResult{}, errors.New("账号设置服务未初始化")
	}
	if input.AccountID == "" {
		return SettingsResult{}, errors.New("缺少账号 ID")
	}
	// unlock 负责释放本次账号设置写入的凭证锁。
	unlock := s.repository.LockCredentials(input.AccountID)
	// pausedUntil、updateErr 保存设置事务返回的暂停截止时间和错误。
	pausedUntil, updateErr := s.repository.UpdateSettings(ctx, input)
	unlock()
	if updateErr != nil {
		return SettingsResult{}, updateErr
	}
	// result 保存设置写入及后续运行时处理结果。
	result := SettingsResult{PausedUntil: pausedUntil}
	if input.Cookie != nil {
		// tokenErr 记录 Cookie 替换后清理旧连接凭证的错误；主设置写入已成功，不能回滚。
		tokenErr := s.repository.ClearTokens(ctx, input.AccountID)
		result.TokenCleanupError = tokenErr
	}
	if input.Cookie != nil && s.runtime != nil {
		// enabled 表示写入成功后账号的最新启用状态；状态读取失败不覆盖已成功的设置写入。
		enabled, statusErr := s.repository.StatusOwned(ctx, input.UserID, input.AccountID)
		if statusErr == nil && enabled {
			result.RuntimeError = s.runtime.Restart(ctx, input.AccountID)
		}
	}
	return result, nil
}

// UpdateLoginInfo 保存账号用户名、密码和浏览器显示设置；既有密码不会被应用层读取。
func (s *SettingsService) UpdateLoginInfo(ctx context.Context, input LoginInfoUpdateInput) error {
	if s == nil || s.repository == nil {
		return errors.New("账号设置服务未初始化")
	}
	if input.AccountID == "" {
		return errors.New("缺少账号 ID")
	}
	// unlock 负责释放登录信息写入期间持有的凭证锁。
	unlock := s.repository.LockCredentials(input.AccountID)
	defer unlock()
	return s.repository.UpdateLoginInfo(ctx, input)
}

// SetStatus 更新账号启用状态并在持久化成功后控制运行实例。
func (s *SettingsService) SetStatus(ctx context.Context, userID int64, accountID string, enabled bool) (StatusResult, error) {
	if s == nil || s.repository == nil {
		return StatusResult{}, errors.New("账号设置服务未初始化")
	}
	if accountID == "" {
		return StatusResult{}, errors.New("缺少账号 ID")
	}
	// reason 由应用层统一产生，避免 HTTP 层决定持久化状态原因。
	reason := ""
	if !enabled {
		reason = "manual"
	}
	// unlock 负责释放账号状态写入期间持有的凭证锁。
	unlock := s.repository.LockCredentials(accountID)
	// statusErr 保存账号状态持久化错误。
	statusErr := s.repository.SetStatusOwned(ctx, userID, accountID, enabled, reason)
	unlock()
	if statusErr != nil {
		return StatusResult{}, statusErr
	}
	// result 保存状态写入后运行时启停的结果。
	result := StatusResult{}
	if s.runtime != nil {
		if enabled {
			result.RuntimeError = s.runtime.Restart(ctx, accountID)
		} else {
			s.runtime.Stop(accountID)
		}
	}
	return result, nil
}

// SetAutoConfirm 更新账号自动确认发货开关。
func (s *SettingsService) SetAutoConfirm(ctx context.Context, userID int64, accountID string, enabled bool) (SettingsResult, error) {
	return s.UpdateSettings(ctx, SettingsUpdateInput{UserID: userID, AccountID: accountID, AutoConfirm: &enabled})
}

// SetRemark 更新账号备注。
func (s *SettingsService) SetRemark(ctx context.Context, userID int64, accountID, remark string) (SettingsResult, error) {
	return s.UpdateSettings(ctx, SettingsUpdateInput{UserID: userID, AccountID: accountID, Remark: &remark})
}

// SetPause 更新账号暂停时长；零值会立即唤醒待执行任务。
func (s *SettingsService) SetPause(ctx context.Context, userID int64, accountID string, duration int) (SettingsResult, error) {
	if s == nil || s.repository == nil {
		return SettingsResult{}, errors.New("账号设置服务未初始化")
	}
	if accountID == "" {
		return SettingsResult{}, errors.New("缺少账号 ID")
	}
	// pausedUntil、pauseErr 保存暂停写入返回的截止时间和错误。
	pausedUntil, pauseErr := s.repository.SetPauseOwned(ctx, userID, accountID, duration)
	return SettingsResult{PausedUntil: pausedUntil}, pauseErr
}

// GetPause 查询账号暂停配置及当前暂停状态。
func (s *SettingsService) GetPause(ctx context.Context, userID int64, accountID string) (PauseState, error) {
	if s == nil || s.repository == nil {
		return PauseState{}, errors.New("账号设置服务未初始化")
	}
	if accountID == "" {
		return PauseState{}, errors.New("缺少账号 ID")
	}
	return s.repository.GetPauseOwned(ctx, userID, accountID)
}
