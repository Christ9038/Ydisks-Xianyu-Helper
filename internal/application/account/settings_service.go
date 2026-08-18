package account

import (
	"context"
	"errors"
	"fmt"
	"sync"
)

var (
	// ErrRuntimeStopConflict 表示账号实例未能在关闭预算内完全停止，持久化状态保持可重试。
	ErrRuntimeStopConflict = errors.New("账号运行实例尚未停止")
	// ErrRuntimeStartUnavailable 表示账号已写入启用或新 Cookie，但运行实例未能达到可运行状态。
	ErrRuntimeStartUnavailable = errors.New("账号运行实例未能启动")
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
	// BeginStopping 建立账号停止 fencing，阻止其他运行时入口在停用转换期间启动实例。
	BeginStopping(string) bool
	// EndStopping 在状态转换结束后释放账号停止 fencing。
	EndStopping(string)
	// StopContext 在调用方关闭预算内停止账号实例，并等待其完全退出。
	StopContext(context.Context, string) error
}

// SettingsService 编排账号设置、登录信息、状态和暂停用例，不依赖 HTTP 或数据库模型。
type SettingsService struct {
	// repository 提供账号设置的窄持久化能力。
	repository SettingsRepository
	// runtime 提供可选的账号运行实例控制能力。
	runtime SettingsRuntime
	// transitionMu 保护 transitionLocks 映射；每把锁串行化一个账号的启用、停用和重启状态转换。
	transitionMu sync.Mutex
	// transitionLocks 保存按账号分配的状态转换锁；锁不跨账号共享，也不在持锁时持有凭证锁。
	transitionLocks map[string]*sync.Mutex
}

// NewSettingsService 构造账号设置应用服务并校验必需的持久化端口。
func NewSettingsService(repository SettingsRepository, runtime SettingsRuntime) (*SettingsService, error) {
	if repository == nil {
		return nil, errors.New("账号设置 repository 未初始化")
	}
	return &SettingsService{repository: repository, runtime: runtime, transitionLocks: make(map[string]*sync.Mutex)}, nil
}

// transitionLock 返回指定账号唯一的状态转换锁，用于避免启用、停用和 Cookie 重启相互覆盖。
func (s *SettingsService) transitionLock(accountID string) *sync.Mutex {
	s.transitionMu.Lock()
	defer s.transitionMu.Unlock()
	// lock 保存当前账号的转换锁；首次使用时创建，之后在服务生命周期内复用。
	lock := s.transitionLocks[accountID]
	if lock == nil {
		lock = &sync.Mutex{}
		s.transitionLocks[accountID] = lock
	}
	return lock
}

// UpdateSettings 原子保存账号设置；Cookie、metadata 和旧 Token 在同一凭证锁内完成，运行时重启在锁外串行执行。
func (s *SettingsService) UpdateSettings(ctx context.Context, input SettingsUpdateInput) (SettingsResult, error) {
	if s == nil || s.repository == nil {
		return SettingsResult{}, errors.New("账号设置服务未初始化")
	}
	if input.AccountID == "" {
		return SettingsResult{}, errors.New("缺少账号 ID")
	}
	// unlock 负责释放本次账号设置写入的凭证锁，锁覆盖 Cookie、metadata 和旧 Token 的完整替换。
	unlock := s.repository.LockCredentials(input.AccountID)
	// pausedUntil、updateErr 保存设置事务返回的暂停截止时间和错误。
	pausedUntil, updateErr := s.repository.UpdateSettings(ctx, input)
	if updateErr != nil {
		unlock()
		return SettingsResult{}, updateErr
	}
	// result 保存设置写入及后续运行时处理结果。
	result := SettingsResult{PausedUntil: pausedUntil}
	if input.Cookie != nil {
		// tokenErr 记录 Cookie 替换后清理旧连接凭证的错误；主设置写入已成功，不能回滚。
		tokenErr := s.repository.ClearTokens(ctx, input.AccountID)
		result.TokenCleanupError = tokenErr
	}
	// Cookie、metadata 与旧 Token 都已完成转换后才释放锁；后续运行时 I/O 不得占用该锁。
	unlock()
	if input.Cookie != nil && s.runtime != nil {
		// enabled 表示写入成功后账号的最新启用状态；状态读取失败不覆盖已成功的设置写入。
		enabled, statusErr := s.repository.StatusOwned(ctx, input.UserID, input.AccountID)
		if statusErr == nil && enabled {
			// lock 串行化 Cookie 重启与同账号启停，避免已更新 Cookie 的实例被并发停用或替换。
			lock := s.transitionLock(input.AccountID)
			lock.Lock()
			// restartErr 保存锁外运行时重启错误；Cookie 主写入仍保留，但数据库必须补偿为停用避免状态失真。
			restartErr := s.runtime.Restart(ctx, input.AccountID)
			if restartErr != nil {
				// compensationErr 保存重启失败后写回停用状态的错误；两个错误共同决定调用方是否需要人工恢复。
				compensationErr := s.setStatusLocked(ctx, input.UserID, input.AccountID, false, "runtime_restart_failed")
				result.RuntimeError = fmt.Errorf("%w: %v", ErrRuntimeStartUnavailable, errors.Join(restartErr, compensationErr))
			}
			lock.Unlock()
		}
	}
	return result, nil
}

// setStatusLocked 在短凭证锁内写入账号启用状态；调用方不得在持有该锁时执行运行时或平台 I/O。
func (s *SettingsService) setStatusLocked(ctx context.Context, userID int64, accountID string, enabled bool, reason string) error {
	// unlock 仅保护持久化状态更新与相关敏感元数据一致性，不跨越运行时关闭或启动。
	unlock := s.repository.LockCredentials(accountID)
	defer unlock()
	return s.repository.SetStatusOwned(ctx, userID, accountID, enabled, reason)
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

// SetStatus 同步完成账号持久化状态与运行实例转换；失败会保留可重试状态而不是伪造成功。
func (s *SettingsService) SetStatus(ctx context.Context, userID int64, accountID string, enabled bool) (StatusResult, error) {
	if s == nil || s.repository == nil {
		return StatusResult{}, errors.New("账号设置服务未初始化")
	}
	if accountID == "" {
		return StatusResult{}, errors.New("缺少账号 ID")
	}
	// lock 串行化同一账号的启用、停用和 Cookie 重启，锁顺序始终先转换锁、后短凭证锁。
	lock := s.transitionLock(accountID)
	lock.Lock()
	defer lock.Unlock()
	// result 保存持久化状态完成后运行时收束或启动失败的显式结果。
	result := StatusResult{}
	if !enabled {
		if s.runtime != nil {
			if !s.runtime.BeginStopping(accountID) {
				result.RuntimeError = ErrRuntimeStopConflict
				return result, nil
			}
			defer s.runtime.EndStopping(accountID)
			// stopErr 保存关闭预算内无法完全停止旧实例的原因；此时禁止写数据库停用状态。
			if stopErr := s.runtime.StopContext(ctx, accountID); stopErr != nil {
				result.RuntimeError = fmt.Errorf("%w: %v", ErrRuntimeStopConflict, stopErr)
				return result, nil
			}
		}
		// statusErr 保存停止完成后写入数据库停用状态的错误。
		if statusErr := s.setStatusLocked(ctx, userID, accountID, false, "manual"); statusErr != nil {
			return StatusResult{}, statusErr
		}
		return result, nil
	}
	// statusErr 保存写入数据库启用状态的错误；写入失败时不得启动运行实例。
	if statusErr := s.setStatusLocked(ctx, userID, accountID, true, ""); statusErr != nil {
		return StatusResult{}, statusErr
	}
	if s.runtime == nil {
		return result, nil
	}
	// restartErr 保存启用后实例未能启动的原因；补偿为停用使持久化与运行时状态不再矛盾。
	if restartErr := s.runtime.Restart(ctx, accountID); restartErr != nil {
		// compensationErr 保存补偿写入失败；错误仍返回给 HTTP 层，避免伪造启用成功。
		compensationErr := s.setStatusLocked(ctx, userID, accountID, false, "runtime_start_failed")
		result.RuntimeError = fmt.Errorf("%w: %v", ErrRuntimeStartUnavailable, errors.Join(restartErr, compensationErr))
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
