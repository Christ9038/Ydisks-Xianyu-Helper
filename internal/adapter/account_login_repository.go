package adapter

import (
	"context"
	"errors"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// AccountLoginRepository 将账号登录、扫码凭证和非敏感账号摘要能力适配到应用端口。
// 该类型是唯一接触 Store、加密 metadata 和浏览器 Cookie 快照的实现。
type AccountLoginRepository struct {
	// store 保存数据库聚合入口；明文凭证只在本适配器的短调用期间存在。
	store *db.Store
}

// NewAccountLoginRepository 构造账号登录数据库适配器；缺少数据库依赖时返回仍可安全失败的实例。
func NewAccountLoginRepository(store *db.Store) *AccountLoginRepository {
	return &AccountLoginRepository{store: store}
}

// LockCredentials 串行化同一账号的凭证变更；缺少 Store 时返回无副作用的解锁函数。
func (r *AccountLoginRepository) LockCredentials(accountID string) func() {
	if r == nil || r.store == nil {
		return func() {}
	}
	return r.store.LockAccountCredentials(accountID)
}

// CreateCookieOwned 原子校验账号归属并写入 Cookie，明文不会离开数据库适配器。
func (r *AccountLoginRepository) CreateCookieOwned(ctx context.Context, accountID, cookies string, userID int64) error {
	// validationErr 表示数据库或 Cookie 仓储未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return validationErr
	}
	// createErr 保存账号 Cookie 创建结果；并发占用会转换为应用层稳定错误。
	createErr := r.store.Cookies.CreateOwned(ctx, accountID, cookies, userID)
	if errors.Is(createErr, db.ErrAlreadyExists) {
		return accountapp.ErrAlreadyExists
	}
	if errors.Is(createErr, db.ErrForbidden) {
		return accountapp.ErrForbidden
	}
	if errors.Is(createErr, db.ErrNotFound) {
		return accountapp.ErrNotFound
	}
	return createErr
}

// LoadPlatformDetail 读取平台调用所需的窄凭证视图，并补充非敏感刷新版本。
func (r *AccountLoginRepository) LoadPlatformDetail(ctx context.Context, accountID string) (*accountapp.CredentialDetail, error) {
	// validationErr 表示数据库或 Cookie 仓储未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return nil, validationErr
	}
	// data 保存只包含 Cookie、metadata 和平台运行设置的窄查询结果；queryErr 保存查询错误。
	data, queryErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	if queryErr != nil {
		if errors.Is(queryErr, db.ErrNotFound) {
			// 返回应用层哨兵并保留数据库错误链，兼容仍在迁移中的基础设施调用方。
			return nil, errors.Join(accountapp.ErrCredentialNotFound, queryErr)
		}
		return nil, queryErr
	}
	// summary 保存不含凭证的账号摘要，用于乐观冲突检查。
	summary, summaryErr := r.store.Cookies.GetSummaryOwned(ctx, data.UserID, accountID)
	if summaryErr != nil {
		return nil, summaryErr
	}
	return &accountapp.CredentialDetail{
		ID: data.ID, UserID: data.UserID, Value: data.Value, MetadataJSON: data.MetadataJSON,
		ShowBrowser: data.ShowBrowser, LastRefreshAt: summary.LastRefreshAt,
	}, nil
}

// UpdateFlatCookieOwned 更新扁平 Cookie 并移除旧的完整浏览器快照。
func (r *AccountLoginRepository) UpdateFlatCookieOwned(ctx context.Context, detail *accountapp.CredentialDetail, cookies string) error {
	// validationErr 表示数据库或 Cookie 仓储未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return validationErr
	}
	if detail == nil {
		return db.ErrNotFound
	}
	// metadata 保存剔除完整快照后的加密元数据，避免旧快照与新扁平 Cookie 不一致。
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	return r.store.Cookies.UpdateRenewalCookie(ctx, detail.ID, cookies, metadata, time.Now().Unix())
}

// UpdateRenewalCookie 保存平台返回的 Cookie 和 metadata。
func (r *AccountLoginRepository) UpdateRenewalCookie(ctx context.Context, accountID, cookies, metadata string, at int64) error {
	// validationErr 表示数据库或 Cookie 仓储未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return validationErr
	}
	return r.store.Cookies.UpdateRenewalCookie(ctx, accountID, cookies, metadata, at)
}

// ClearTokens 清理凭证变化后失效的旧 Token；未装配 Token 仓储时保持兼容空操作。
func (r *AccountLoginRepository) ClearTokens(ctx context.Context, accountID string) error {
	// validationErr 表示数据库适配器未完成装配。
	validationErr := r.validateStore()
	if validationErr != nil {
		return validationErr
	}
	if r.store.Tokens == nil {
		return nil
	}
	return r.store.Tokens.Clear(ctx, accountID)
}

// GetStatus 查询账号是否启用；缺少存储或数据库故障时安全地返回停用。
func (r *AccountLoginRepository) GetStatus(ctx context.Context, accountID string) bool {
	if r == nil || r.store == nil || r.store.Cookies == nil {
		return false
	}
	return r.store.Cookies.GetStatus(ctx, accountID)
}

// UpdateProfile 保存账号昵称和头像等非敏感展示资料。
func (r *AccountLoginRepository) UpdateProfile(ctx context.Context, accountID, nickname, avatarURL string) error {
	// validationErr 表示数据库或 Cookie 仓储未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return validationErr
	}
	return r.store.Cookies.UpdateProfile(ctx, accountID, nickname, avatarURL)
}

// FindAccount 查询扫码账号的存在性和归属，不读取任何凭证字段。
func (r *AccountLoginRepository) FindAccount(ctx context.Context, accountID string) (accountapp.QRLoginAccount, error) {
	// validationErr 表示数据库或 Cookie 仓储未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return accountapp.QRLoginAccount{}, validationErr
	}
	// ownerID 保存数据库返回的非敏感账号所有者标识；queryErr 保存查询错误。
	ownerID, queryErr := r.store.Cookies.GetOwnerID(ctx, accountID)
	if queryErr != nil {
		if errors.Is(queryErr, db.ErrNotFound) {
			return accountapp.QRLoginAccount{}, accountapp.ErrNotFound
		}
		return accountapp.QRLoginAccount{}, queryErr
	}
	return accountapp.QRLoginAccount{ID: accountID, UserID: ownerID}, nil
}

// UpdateFlatCookieOwnedForQR 更新扫码登录已有账号的扁平 Cookie并清除快照。
func (r *AccountLoginRepository) UpdateFlatCookieOwnedForQR(ctx context.Context, accountID, cookies string) error {
	// validationErr 表示数据库或 Cookie 仓储未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return validationErr
	}
	// detail 保存仅供适配器内部使用的平台凭证视图；queryErr 保存查询错误。
	detail, queryErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	if queryErr != nil {
		return queryErr
	}
	// metadata 保存剔除旧快照后的加密元数据。
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	return r.store.Cookies.UpdateRenewalCookie(ctx, accountID, cookies, metadata, time.Now().Unix())
}

// UpdateCookieSnapshotOwned 更新扫码 Cookie 并合并完整浏览器快照到加密 metadata。
func (r *AccountLoginRepository) UpdateCookieSnapshotOwned(ctx context.Context, accountID, cookies string, snapshot []accountapp.CookieSnapshot) error {
	// validationErr 表示数据库或 Cookie 仓储未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return validationErr
	}
	// detail 保存仅供适配器内部使用的平台凭证视图；queryErr 保存查询错误。
	detail, queryErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	if queryErr != nil {
		return queryErr
	}
	// browserSnapshot 保存转换后的浏览器快照，明文不向应用层或 HTTP 层返回。
	browserSnapshot := make([]cookierefresh.BrowserCookie, 0, len(snapshot))
	// snapshotEntry 表示当前待转换的应用层 Cookie 快照。
	for _, snapshotEntry := range snapshot {
		browserSnapshot = append(browserSnapshot, cookierefresh.BrowserCookie{
			Name: snapshotEntry.Name, Value: snapshotEntry.Value, Domain: snapshotEntry.Domain,
			Path: snapshotEntry.Path, Expires: snapshotEntry.Expires, HTTPOnly: snapshotEntry.HTTPOnly,
			Secure: snapshotEntry.Secure, SameSite: snapshotEntry.SameSite, PartitionKey: snapshotEntry.PartitionKey,
		})
	}
	// metadata 保存合并浏览器快照后的加密元数据文本。
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, browserSnapshot)
	return r.store.Cookies.UpdateRenewalCookie(ctx, accountID, cookies, metadata, time.Now().Unix())
}

// GetOwnedSummary 查询指定用户拥有的非敏感账号摘要。
func (r *AccountLoginRepository) GetOwnedSummary(ctx context.Context, userID int64, accountID string) (accountapp.Summary, error) {
	// validationErr 表示数据库或 Cookie 仓储未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return accountapp.Summary{}, validationErr
	}
	// summary 保存数据库返回的非敏感账号摘要；queryErr 保存摘要查询错误。
	summary, queryErr := r.store.Cookies.GetSummaryOwned(ctx, userID, accountID)
	if queryErr != nil {
		if errors.Is(queryErr, db.ErrForbidden) {
			return accountapp.Summary{}, accountapp.ErrForbidden
		}
		if errors.Is(queryErr, db.ErrNotFound) {
			// ownerID 和 ownerErr 用于区分不存在账号与跨用户账号，保持 HTTP 兼容状态码。
			ownerID, ownerErr := r.store.Cookies.GetOwnerID(ctx, accountID)
			if ownerErr == nil && ownerID != userID {
				return accountapp.Summary{}, accountapp.ErrForbidden
			}
			return accountapp.Summary{}, accountapp.ErrNotFound
		}
		return accountapp.Summary{}, queryErr
	}
	return accountapp.Summary{
		ID: summary.ID, UserID: summary.UserID, Remark: summary.Remark,
		Nickname: summary.Nickname, AvatarURL: summary.AvatarURL,
		AutoConfirm: summary.AutoConfirm, PauseDuration: summary.PauseDuration,
		PausedUntil: summary.PausedUntil, Username: summary.Username,
		ShowBrowser: summary.ShowBrowser, LastRefreshAt: summary.LastRefreshAt,
		LoginMethod: summary.LoginMethod,
		LastLoginAt: summary.LastLoginAt, CreatedAt: summary.CreatedAt,
	}, nil
}

// DeleteOwned 在凭证锁内再次确认账号归属后删除账号，避免停止 fencing 期间误删被替换账号。
func (r *AccountLoginRepository) DeleteOwned(ctx context.Context, userID int64, accountID string) error {
	// validationErr 表示数据库或 Cookie 仓储未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return validationErr
	}
	// unlock 保护最终归属复核与删除之间的账号凭证状态。
	unlock := r.LockCredentials(accountID)
	defer unlock()
	// summaryErr 保存锁内最终归属复核失败的原因。
	if _, summaryErr := r.GetOwnedSummary(ctx, userID, accountID); summaryErr != nil {
		return summaryErr
	}
	return r.store.Cookies.Delete(ctx, accountID)
}

// validateStore 检查数据库聚合入口是否已完成装配。
func (r *AccountLoginRepository) validateStore() error {
	if r == nil || r.store == nil {
		return errors.New("账号登录数据库适配器未初始化")
	}
	return nil
}

// validateCookies 检查账号凭证仓储是否已完成装配。
func (r *AccountLoginRepository) validateCookies() error {
	// validationErr 表示数据库适配器未完成装配。
	validationErr := r.validateStore()
	if validationErr != nil {
		return validationErr
	}
	if r.store.Cookies == nil {
		return errors.New("账号登录 Cookie 仓储未初始化")
	}
	return nil
}

var _ accountapp.CredentialRepository = (*AccountLoginRepository)(nil)
var _ accountapp.ProfileSummaryRepository = (*AccountLoginRepository)(nil)
var _ accountapp.DeleteSummaryRepository = (*AccountLoginRepository)(nil)

// QRLoginRepository 将账号登录适配器限制为扫码持久化端口，避免应用层接触数据库模型。
type QRLoginRepository struct {
	// repository 保存共享的账号凭证适配器。
	repository *AccountLoginRepository
}

// NewQRLoginRepository 构造扫码登录专用凭证端口。
func NewQRLoginRepository(store *db.Store) accountapp.QRLoginRepository {
	return QRLoginRepository{repository: NewAccountLoginRepository(store)}
}

// LockCredentials 串行化扫码登录对同一账号的凭证变更。
func (r QRLoginRepository) LockCredentials(accountID string) func() {
	return r.repository.LockCredentials(accountID)
}

// FindAccount 查询扫码账号的存在性和归属，不读取凭证字段。
func (r QRLoginRepository) FindAccount(ctx context.Context, accountID string) (accountapp.QRLoginAccount, error) {
	return r.repository.FindAccount(ctx, accountID)
}

// CreateCookieOwned 创建扫码登录得到的新账号 Cookie。
func (r QRLoginRepository) CreateCookieOwned(ctx context.Context, accountID, cookies string, userID int64) error {
	return r.repository.CreateCookieOwned(ctx, accountID, cookies, userID)
}

// UpdateFlatCookieOwned 更新已有账号扁平 Cookie并清除完整快照。
func (r QRLoginRepository) UpdateFlatCookieOwned(ctx context.Context, accountID, cookies string) error {
	return r.repository.UpdateFlatCookieOwnedForQR(ctx, accountID, cookies)
}

// UpdateCookieSnapshotOwned 更新 Cookie 并合并完整浏览器快照。
func (r QRLoginRepository) UpdateCookieSnapshotOwned(ctx context.Context, accountID, cookies string, snapshot []accountapp.CookieSnapshot) error {
	return r.repository.UpdateCookieSnapshotOwned(ctx, accountID, cookies, snapshot)
}

// ClearTokens 清理扫码登录前遗留的旧连接 Token。
func (r QRLoginRepository) ClearTokens(ctx context.Context, accountID string) error {
	return r.repository.ClearTokens(ctx, accountID)
}

var _ accountapp.QRLoginRepository = QRLoginRepository{}
