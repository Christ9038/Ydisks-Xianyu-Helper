package server

import (
	"context"
	"errors"
	"time"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
)

// accountLoginRepository 定义账号登录服务写入凭证、审计登录、更新资料和读取账号状态所需的最小能力。
type accountLoginRepository interface {
	// LockCredentials 串行化账号登录凭证变更。
	LockCredentials(accountID string) func()
	// CreateCookieOwned 创建用户拥有的账号凭证。
	CreateCookieOwned(ctx context.Context, accountID, cookies string, userID int64) error
	// LoadPlatformDetail 读取平台登录所需的最小账号视图。
	LoadPlatformDetail(ctx context.Context, accountID string) (*db.CookieDetail, error)
	// UpdateFlatCookieOwned 更新用户拥有账号的扁平 Cookie。
	UpdateFlatCookieOwned(ctx context.Context, detail *db.CookieDetail, cookies string) error
	// UpdateRenewalCookie 更新账号 Cookie 和 metadata。
	UpdateRenewalCookie(ctx context.Context, accountID, cookies, metadata string, at int64) error
	// ClearTokens 清理账号旧连接 Token。
	ClearTokens(ctx context.Context, accountID string) error
	// GetStatus 返回账号是否启用。
	GetStatus(ctx context.Context, accountID string) bool
	// MarkLogin 保存账号最近一次成功登录方式。
	MarkLogin(ctx context.Context, accountID, method string, at int64) error
	// SetStatusWithReason 更新账号启用状态及禁用原因。
	SetStatusWithReason(ctx context.Context, accountID string, enabled bool, reason string) error
	// AddLoginLog 写入账号登录审计记录。
	AddLoginLog(ctx context.Context, log db.AccountLoginLog) error
	// UpdateProfile 保存账号展示资料。
	UpdateProfile(ctx context.Context, accountID, nickname, avatarURL string) error
}

// storeAccountLoginRepository 将完整 Store 适配为账号登录服务窄 repository。
type storeAccountLoginRepository struct {
	// store 保存数据库聚合入口，仅在适配器内部使用。
	store *db.Store
}

// storeAccountProfileRepository 将 Server 的摘要查询适配为账号应用层 Port。
type storeAccountProfileRepository struct {
	// store 提供账号摘要查询能力；凭证字段不会通过该适配器读取。
	store *db.Store
}

// GetOwnedSummary 查询指定用户拥有的非敏感账号摘要。
func (r storeAccountProfileRepository) GetOwnedSummary(ctx context.Context, userID int64, accountID string) (accountapp.Summary, error) {
	// summary 保存数据库返回的非敏感账号摘要。
	summary, err := r.store.Cookies.GetSummaryOwned(ctx, userID, accountID)
	if err != nil {
		if errors.Is(err, db.ErrForbidden) {
			return accountapp.Summary{}, accountapp.ErrForbidden
		}
		if errors.Is(err, db.ErrNotFound) {
			// ownerID 和 ownerErr 用于区分不存在账号与跨用户账号，保持 HTTP 兼容状态码。
			ownerID, ownerErr := r.store.Cookies.GetOwnerID(ctx, accountID)
			if ownerErr == nil && ownerID != userID {
				return accountapp.Summary{}, accountapp.ErrForbidden
			}
			return accountapp.Summary{}, accountapp.ErrNotFound
		}
		return accountapp.Summary{}, err
	}
	return accountapp.Summary{
		ID: summary.ID, UserID: summary.UserID, Remark: summary.Remark,
		Nickname: summary.Nickname, AvatarURL: summary.AvatarURL,
	}, nil
}

// LockCredentials 委托账号凭证锁。
func (r storeAccountLoginRepository) LockCredentials(accountID string) func() {
	return r.store.LockAccountCredentials(accountID)
}

// CreateCookieOwned 委托账号凭证创建。
func (r storeAccountLoginRepository) CreateCookieOwned(ctx context.Context, accountID, cookies string, userID int64) error {
	return r.store.Cookies.CreateOwned(ctx, accountID, cookies, userID)
}

// LoadPlatformDetail 委托平台账号视图查询并转换为 Server 内部模型。
func (r storeAccountLoginRepository) LoadPlatformDetail(ctx context.Context, accountID string) (*db.CookieDetail, error) {
	// data 和 err 保存平台运行视图查询结果。
	data, err := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	if err != nil {
		return nil, err
	}
	return &db.CookieDetail{ID: data.ID, UserID: data.UserID, Value: data.Value, MetadataJSON: data.MetadataJSON, ShowBrowser: data.ShowBrowser}, nil
}

// UpdateFlatCookieOwned 委托扁平 Cookie 更新并清除旧的完整 Cookie Jar 快照。
func (r storeAccountLoginRepository) UpdateFlatCookieOwned(ctx context.Context, detail *db.CookieDetail, cookies string) error {
	if detail == nil {
		return db.ErrNotFound
	}
	// metadata 保存移除完整 Cookie Jar 快照后的元数据。
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	return r.store.Cookies.UpdateRenewalCookie(ctx, detail.ID, cookies, metadata, time.Now().Unix())
}

// UpdateRenewalCookie 委托 Cookie 与 metadata 更新。
func (r storeAccountLoginRepository) UpdateRenewalCookie(ctx context.Context, accountID, cookies, metadata string, at int64) error {
	return r.store.Cookies.UpdateRenewalCookie(ctx, accountID, cookies, metadata, at)
}

// ClearTokens 委托账号 Token 清理；未启用 Token repository 时保持兼容空操作。
func (r storeAccountLoginRepository) ClearTokens(ctx context.Context, accountID string) error {
	if r.store.Tokens == nil {
		return nil
	}
	return r.store.Tokens.Clear(ctx, accountID)
}

// GetStatus 委托账号启用状态查询。
func (r storeAccountLoginRepository) GetStatus(ctx context.Context, accountID string) bool {
	return r.store.Cookies.GetStatus(ctx, accountID)
}

// MarkLogin 委托账号最近一次成功登录方式保存。
func (r storeAccountLoginRepository) MarkLogin(ctx context.Context, accountID, method string, at int64) error {
	return r.store.Cookies.MarkLogin(ctx, accountID, method, at)
}

// SetStatusWithReason 委托账号启用状态更新。
func (r storeAccountLoginRepository) SetStatusWithReason(ctx context.Context, accountID string, enabled bool, reason string) error {
	return r.store.Cookies.SetStatusWithReason(ctx, accountID, enabled, reason)
}

// AddLoginLog 委托账号登录审计记录写入；未启用审计 repository 时保持兼容空操作。
func (r storeAccountLoginRepository) AddLoginLog(ctx context.Context, log db.AccountLoginLog) error {
	if r.store.LoginLogs == nil {
		return nil
	}
	return r.store.LoginLogs.Add(ctx, log)
}

// UpdateProfile 委托账号展示资料更新。
func (r storeAccountLoginRepository) UpdateProfile(ctx context.Context, accountID, nickname, avatarURL string) error {
	return r.store.Cookies.UpdateProfile(ctx, accountID, nickname, avatarURL)
}

// newStoreAccountLoginRepository 从完整 Store 构造账号登录服务窄 repository。
func newStoreAccountLoginRepository(store *db.Store) accountLoginRepository {
	if store == nil || store.Cookies == nil {
		return nil
	}
	return storeAccountLoginRepository{store: store}
}

// 确保 Store 适配器始终覆盖账号登录服务所需的全部能力。
var _ accountLoginRepository = storeAccountLoginRepository{}
