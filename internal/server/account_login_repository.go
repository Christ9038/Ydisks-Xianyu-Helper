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
	// summary 只补充非敏感版本字段，避免为乐观冲突检查读取或解密登录密码。
	summary, summaryErr := r.store.Cookies.GetSummaryOwned(ctx, data.UserID, accountID)
	if summaryErr != nil {
		return nil, summaryErr
	}
	return &db.CookieDetail{ID: data.ID, UserID: data.UserID, Value: data.Value, MetadataJSON: data.MetadataJSON, ShowBrowser: data.ShowBrowser, LastRefreshAt: summary.LastRefreshAt}, nil
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

// FindAccount 只读取扫码登录所需的账号归属，不解密 Cookie、密码或 metadata。
func (r storeAccountLoginRepository) FindAccount(ctx context.Context, accountID string) (accountapp.QRLoginAccount, error) {
	// ownerID 保存数据库返回的账号所属用户标识。
	ownerID, err := r.store.Cookies.GetOwnerID(ctx, accountID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return accountapp.QRLoginAccount{}, accountapp.ErrNotFound
		}
		return accountapp.QRLoginAccount{}, err
	}
	return accountapp.QRLoginAccount{ID: accountID, UserID: ownerID}, nil
}

// UpdateQRCookieFlatOwned 更新扫码登录账号的扁平 Cookie，并清除旧的完整快照。
func (r storeAccountLoginRepository) UpdateQRCookieFlatOwned(ctx context.Context, accountID, cookies string) error {
	// detail 保存平台运行视图；该窄视图只在凭证适配器内部读取 metadata。
	detail, err := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	if err != nil {
		return err
	}
	// metadata 保存移除旧快照后的加密 metadata 文本。
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	return r.store.Cookies.UpdateRenewalCookie(ctx, accountID, cookies, metadata, time.Now().Unix())
}

// UpdateQRCookieSnapshotOwned 更新扫码 Cookie，并把完整快照合并进加密 metadata。
func (r storeAccountLoginRepository) UpdateQRCookieSnapshotOwned(ctx context.Context, accountID, cookies string, snapshot []accountapp.CookieSnapshot) error {
	// detail 保存平台运行视图；完整 metadata 不向应用服务层泄露。
	detail, err := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	if err != nil {
		return err
	}
	// browserSnapshot 保存转换后的浏览器 Cookie 快照，仅在数据库适配器内使用。
	browserSnapshot := make([]cookierefresh.BrowserCookie, 0, len(snapshot))
	// snapshotEntry 表示当前待转换的应用层 Cookie 快照。
	for _, snapshotEntry := range snapshot {
		browserSnapshot = append(browserSnapshot, cookierefresh.BrowserCookie{
			Name: snapshotEntry.Name, Value: snapshotEntry.Value, Domain: snapshotEntry.Domain,
			Path: snapshotEntry.Path, Expires: snapshotEntry.Expires, HTTPOnly: snapshotEntry.HTTPOnly,
			Secure: snapshotEntry.Secure, SameSite: snapshotEntry.SameSite, PartitionKey: snapshotEntry.PartitionKey,
		})
	}
	// metadata 保存快照合并后的加密 metadata 文本。
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, browserSnapshot)
	return r.store.Cookies.UpdateRenewalCookie(ctx, accountID, cookies, metadata, time.Now().Unix())
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

// newStoreQRLoginRepository 从完整 Store 构造扫码登录应用服务所需的窄凭证端口。
func newStoreQRLoginRepository(store *db.Store) accountapp.QRLoginRepository {
	if store == nil || store.Cookies == nil {
		return nil
	}
	return storeQRLoginRepository{store: store}
}

// storeQRLoginRepository 将 Store 的扫码凭证能力限制在应用服务定义的窄端口内。
type storeQRLoginRepository struct {
	// store 提供数据库凭证读写能力，仅在本适配器内部访问。
	store *db.Store
}

// LockCredentials 串行化扫码登录对同一账号的凭证变更。
func (r storeQRLoginRepository) LockCredentials(accountID string) func() {
	return r.store.LockAccountCredentials(accountID)
}

// FindAccount 只返回账号存在性和归属，不读取 Cookie、密码或 metadata。
func (r storeQRLoginRepository) FindAccount(ctx context.Context, accountID string) (accountapp.QRLoginAccount, error) {
	return storeAccountLoginRepository(r).FindAccount(ctx, accountID)
}

// CreateCookieOwned 创建扫码登录得到的新账号 Cookie。
func (r storeQRLoginRepository) CreateCookieOwned(ctx context.Context, accountID, cookies string, userID int64) error {
	return r.store.Cookies.CreateOwned(ctx, accountID, cookies, userID)
}

// UpdateFlatCookieOwned 更新已有账号的扁平 Cookie，并清除完整快照。
func (r storeQRLoginRepository) UpdateFlatCookieOwned(ctx context.Context, accountID, cookies string) error {
	return storeAccountLoginRepository(r).UpdateQRCookieFlatOwned(ctx, accountID, cookies)
}

// UpdateCookieSnapshotOwned 更新 Cookie 并合并完整浏览器快照。
func (r storeQRLoginRepository) UpdateCookieSnapshotOwned(ctx context.Context, accountID, cookies string, snapshot []accountapp.CookieSnapshot) error {
	return storeAccountLoginRepository(r).UpdateQRCookieSnapshotOwned(ctx, accountID, cookies, snapshot)
}

// ClearTokens 清理扫码登录前遗留的旧连接 Token。
func (r storeQRLoginRepository) ClearTokens(ctx context.Context, accountID string) error {
	// clearErr 保存旧连接 Token 清理结果；凭证已成功写入时该错误不阻断扫码登录。
	return storeAccountLoginRepository(r).ClearTokens(ctx, accountID)
}

// 确保 Store 适配器始终覆盖账号登录服务所需的全部能力。
var _ accountLoginRepository = storeAccountLoginRepository{}

// 确保扫码凭证适配器始终覆盖应用服务定义的最小端口。
var _ accountapp.QRLoginRepository = storeQRLoginRepository{}
