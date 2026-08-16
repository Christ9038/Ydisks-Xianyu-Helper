package adapter

import (
	"context"
	"errors"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
)

// ClearTokens 清理 Cookie 更新后失效的旧连接凭证；缺少 Token 仓储时保持兼容空操作。
func (r *AccountSettingsRepository) ClearTokens(ctx context.Context, accountID string) error {
	// validationErr 表示账号设置适配器缺少数据库依赖。
	if validationErr := r.validate(); validationErr != nil {
		return validationErr
	}
	if r.store.Tokens == nil {
		return nil
	}
	return r.store.Tokens.Clear(ctx, accountID)
}

// AccountSettingsRepository 将账号设置、登录秘密和暂停状态适配到应用层 Port。
// 明文 Cookie 与登录密码只在本适配器调用数据库加密写入时短暂存在。
type AccountSettingsRepository struct {
	// store 保存数据库聚合入口，不向应用层暴露裸数据库连接或模型。
	store *db.Store
}

// NewAccountSettingsRepository 构造账号设置数据库适配器。
func NewAccountSettingsRepository(store *db.Store) *AccountSettingsRepository {
	return &AccountSettingsRepository{store: store}
}

// LockCredentials 串行化同一账号的敏感设置写入；缺少数据库时返回空操作解锁函数。
func (r *AccountSettingsRepository) LockCredentials(accountID string) func() {
	if r == nil || r.store == nil {
		return func() {}
	}
	return r.store.LockAccountCredentials(accountID)
}

// UpdateSettings 将应用层账号设置转换为数据库事务输入并执行归属复核。
func (r *AccountSettingsRepository) UpdateSettings(ctx context.Context, input accountapp.SettingsUpdateInput) (int64, error) {
	// validationErr 表示账号设置适配器缺少数据库依赖。
	if validationErr := r.validate(); validationErr != nil {
		return 0, validationErr
	}
	// databaseInput 只在适配器边界携带数据库模型，避免上层依赖 internal/db。
	databaseInput := db.AccountSettingsUpdate{
		UserID: input.UserID, Value: input.Cookie, Remark: input.Remark,
		AutoConfirm: input.AutoConfirm, PauseDuration: input.PauseDuration,
		Username: input.Username, Password: input.Password,
		ShowBrowser: input.ShowBrowser, ChannelIDs: input.ChannelIDs,
	}
	// pausedUntil、updateErr 保存数据库返回的暂停截止时间和事务错误。
	pausedUntil, updateErr := r.store.Cookies.UpdateSettings(ctx, input.AccountID, databaseInput)
	return pausedUntil, normalizeAccountSettingsError(updateErr)
}

// UpdateLoginInfo 将登录用户名、可选密码和浏览器显示设置复用为原子账号设置更新。
func (r *AccountSettingsRepository) UpdateLoginInfo(ctx context.Context, input accountapp.LoginInfoUpdateInput) error {
	// validationErr 表示账号设置适配器缺少数据库依赖。
	if validationErr := r.validate(); validationErr != nil {
		return validationErr
	}
	// username 保存兼容旧接口的用户名覆盖值。
	username := input.Username
	// showBrowser 保存密码登录时是否显示浏览器的配置值。
	showBrowser := input.ShowBrowser
	// updateErr 保存复用账号设置事务写入登录信息时的错误。
	_, updateErr := r.UpdateSettings(ctx, accountapp.SettingsUpdateInput{
		UserID: input.UserID, AccountID: input.AccountID,
		Username: &username, Password: input.Password, ShowBrowser: &showBrowser,
	})
	return updateErr
}

// SetStatusOwned 更新账号启用状态，并在数据库访问前验证账号归属。
func (r *AccountSettingsRepository) SetStatusOwned(ctx context.Context, userID int64, accountID string, enabled bool, reason string) error {
	// validationErr 表示账号设置适配器缺少数据库依赖。
	if validationErr := r.validate(); validationErr != nil {
		return validationErr
	}
	// ownershipErr 表示状态写入前的账号归属校验错误。
	if ownershipErr := r.requireOwner(ctx, userID, accountID); ownershipErr != nil {
		return ownershipErr
	}
	return normalizeAccountSettingsError(r.store.Cookies.SetStatusWithReason(ctx, accountID, enabled, reason))
}

// StatusOwned 查询指定用户账号的启用状态，不读取 Cookie、metadata 或登录密码。
func (r *AccountSettingsRepository) StatusOwned(ctx context.Context, userID int64, accountID string) (bool, error) {
	// validationErr 表示账号设置适配器缺少数据库依赖。
	if validationErr := r.validate(); validationErr != nil {
		return false, validationErr
	}
	// ownershipErr 表示状态读取前的账号归属校验错误。
	if ownershipErr := r.requireOwner(ctx, userID, accountID); ownershipErr != nil {
		return false, ownershipErr
	}
	// enabled、statusErr 保存数据库返回的启用状态和查询错误。
	enabled, statusErr := r.store.Cookies.Status(ctx, accountID)
	return enabled, normalizeAccountSettingsError(statusErr)
}

// SetPauseOwned 更新指定用户账号的暂停时长，并保留零值唤醒待执行任务的数据库语义。
func (r *AccountSettingsRepository) SetPauseOwned(ctx context.Context, userID int64, accountID string, duration int) (int64, error) {
	// validationErr 表示账号设置适配器缺少数据库依赖。
	if validationErr := r.validate(); validationErr != nil {
		return 0, validationErr
	}
	// ownershipErr 表示暂停写入前的账号归属校验错误。
	if ownershipErr := r.requireOwner(ctx, userID, accountID); ownershipErr != nil {
		return 0, ownershipErr
	}
	// pausedUntil、pauseErr 保存数据库返回的暂停截止时间和写入错误。
	pausedUntil, pauseErr := r.store.Cookies.SetPause(ctx, accountID, duration)
	return pausedUntil, normalizeAccountSettingsError(pauseErr)
}

// GetPauseOwned 查询指定用户账号的暂停状态和配置时长。
func (r *AccountSettingsRepository) GetPauseOwned(ctx context.Context, userID int64, accountID string) (accountapp.PauseState, error) {
	// validationErr 表示账号设置适配器缺少数据库依赖。
	if validationErr := r.validate(); validationErr != nil {
		return accountapp.PauseState{}, validationErr
	}
	// ownershipErr 表示暂停读取前的账号归属校验错误。
	if ownershipErr := r.requireOwner(ctx, userID, accountID); ownershipErr != nil {
		return accountapp.PauseState{}, ownershipErr
	}
	// paused、pausedUntil、pauseErr 保存数据库返回的暂停标记、截止时间和查询错误。
	paused, pausedUntil, pauseErr := r.store.Cookies.IsPaused(ctx, accountID)
	if pauseErr != nil {
		return accountapp.PauseState{}, normalizeAccountSettingsError(pauseErr)
	}
	return accountapp.PauseState{Duration: r.store.Cookies.GetPauseDuration(ctx, accountID), PausedUntil: pausedUntil, Paused: paused}, nil
}

// requireOwner 在数据库适配器内部完成非敏感账号归属复核。
func (r *AccountSettingsRepository) requireOwner(ctx context.Context, userID int64, accountID string) error {
	// _, ownershipErr 丢弃非敏感摘要本身，只保留账号是否属于当前用户的结论。
	if _, ownershipErr := r.store.Cookies.GetSummaryOwned(ctx, userID, accountID); ownershipErr != nil {
		if errors.Is(ownershipErr, db.ErrNotFound) {
			// ownerID、ownerErr 用于区分账号不存在与账号属于其他用户，保持越权错误语义。
			// ownerID、ownerErr 保存账号所有者标识和归属查询错误。
			ownerID, ownerErr := r.store.Cookies.GetOwnerID(ctx, accountID)
			if ownerErr == nil && ownerID != userID {
				return accountapp.ErrForbidden
			}
		}
		return normalizeAccountSettingsError(ownershipErr)
	}
	return nil
}

// validate 检查账号设置适配器所需的数据库依赖是否已装配。
func (r *AccountSettingsRepository) validate() error {
	if r == nil || r.store == nil || r.store.Cookies == nil {
		return errors.New("账号设置数据库 repository 未初始化")
	}
	return nil
}

// normalizeAccountSettingsError 将数据库所有权错误转换为应用层稳定错误，不泄露数据库模型。
func normalizeAccountSettingsError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, db.ErrNotFound) {
		return accountapp.ErrNotFound
	}
	if errors.Is(err, db.ErrForbidden) {
		return accountapp.ErrForbidden
	}
	return err
}

var _ accountapp.SettingsRepository = (*AccountSettingsRepository)(nil)
