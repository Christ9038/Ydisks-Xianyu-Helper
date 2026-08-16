package adapter

import (
	"context"
	"errors"

	accountapp "xianyu-go/internal/application/account"
	"xianyu-go/internal/db"
)

// AccountSummaryRepository 将数据库账号摘要查询适配为应用层 Port，不向上层暴露数据库模型或凭证字段。
type AccountSummaryRepository struct {
	// store 保存数据库聚合入口，仅用于调用窄账号摘要查询。
	store *db.Store
}

// NewAccountSummaryRepository 构造账号摘要数据库适配器。
func NewAccountSummaryRepository(store *db.Store) *AccountSummaryRepository {
	return &AccountSummaryRepository{store: store}
}

// ListOwnedIDs 查询指定用户的账号 ID 列表，不解密任何凭证。
func (r *AccountSummaryRepository) ListOwnedIDs(ctx context.Context, userID int64) ([]string, error) {
	// validationErr 表示摘要数据库适配器尚未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return nil, validationErr
	}
	return r.store.Cookies.ListOwnedIDs(ctx, userID)
}

// ListSummaries 查询指定用户的非敏感账号摘要并转换为应用模型。
func (r *AccountSummaryRepository) ListSummaries(ctx context.Context, userID int64) ([]accountapp.AccountSummary, error) {
	// validationErr 表示摘要数据库适配器尚未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return nil, validationErr
	}
	// records、queryErr 保存数据库摘要行及查询错误。
	records, queryErr := r.store.Cookies.ListSummaries(ctx, userID)
	if queryErr != nil {
		return nil, normalizeAccountSummaryError(queryErr)
	}
	// summaries 保存与数据库模型解耦的应用摘要列表。
	summaries := make([]accountapp.AccountSummary, 0, len(records))
	// record 表示当前待转换的数据库账号摘要。
	for _, record := range records {
		summaries = append(summaries, accountSummaryModel(record))
	}
	return summaries, nil
}

// GetOwnedSummary 查询指定用户的单个非敏感账号摘要。
func (r *AccountSummaryRepository) GetOwnedSummary(ctx context.Context, userID int64, accountID string) (accountapp.AccountSummary, error) {
	// validationErr 表示摘要数据库适配器尚未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return accountapp.AccountSummary{}, validationErr
	}
	// record、queryErr 保存数据库摘要行及查询错误。
	record, queryErr := r.store.Cookies.GetSummaryOwned(ctx, userID, accountID)
	if queryErr != nil {
		return accountapp.AccountSummary{}, normalizeAccountSummaryError(queryErr)
	}
	return accountSummaryModel(record), nil
}

// ExistsOwned 判断账号是否属于指定用户，仅返回非敏感存在性结论。
func (r *AccountSummaryRepository) ExistsOwned(ctx context.Context, userID int64, accountID string) (bool, error) {
	// validationErr 表示摘要数据库适配器尚未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return false, validationErr
	}
	return r.store.Cookies.ExistsOwned(ctx, userID, accountID)
}

// GetOwnerID 查询指定账号所属用户标识，不读取 Cookie、密码或 metadata。
func (r *AccountSummaryRepository) GetOwnerID(ctx context.Context, accountID string) (int64, error) {
	// validationErr 表示摘要数据库适配器尚未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return 0, validationErr
	}
	// ownerID、queryErr 保存数据库返回的账号所有者及查询错误。
	ownerID, queryErr := r.store.Cookies.GetOwnerID(ctx, accountID)
	return ownerID, normalizeAccountSummaryError(queryErr)
}

// StatusOwned 查询账号启用状态，并在状态读取前按用户复核账号归属。
func (r *AccountSummaryRepository) StatusOwned(ctx context.Context, userID int64, accountID string) (bool, error) {
	// validationErr 表示摘要数据库适配器尚未完成装配。
	validationErr := r.validateCookies()
	if validationErr != nil {
		return false, validationErr
	}
	// _, ownershipErr 丢弃摘要内容，只保留状态查询前的归属结论。
	if _, ownershipErr := r.store.Cookies.GetSummaryOwned(ctx, userID, accountID); ownershipErr != nil {
		return false, normalizeAccountSummaryError(ownershipErr)
	}
	// enabled、statusErr 保存数据库返回的状态及查询错误。
	enabled, statusErr := r.store.Cookies.Status(ctx, accountID)
	return enabled, normalizeAccountSummaryError(statusErr)
}

// ListAdminSummaries 查询管理员账号摘要并补充启用状态，不读取凭证字段。
func (r *AccountSummaryRepository) ListAdminSummaries(ctx context.Context) ([]accountapp.AdminAccountSummary, error) {
	if r == nil || r.store == nil || r.store.Admin == nil || r.store.Cookies == nil {
		return nil, errors.New("管理员账号摘要数据库适配器未初始化")
	}
	// records、queryErr 保存管理员账号摘要行及查询错误。
	records, queryErr := r.store.Admin.ListCookies(ctx)
	if queryErr != nil {
		return nil, queryErr
	}
	// summaries 保存管理员端所需的非敏感账号摘要。
	summaries := make([]accountapp.AdminAccountSummary, 0, len(records))
	// record 表示当前待补充启用状态的管理员账号摘要。
	for _, record := range records {
		// enabled 表示当前账号的启用状态；数据库故障按停用处理以保持既有响应语义。
		enabled := r.store.Cookies.GetStatus(ctx, record.ID)
		summaries = append(summaries, accountapp.AdminAccountSummary{
			ID: record.ID, UserID: record.UserID, Remark: record.Remark,
			CreatedAt: record.CreatedAt, Owner: record.Owner, Enabled: enabled,
		})
	}
	return summaries, nil
}

// validateCookies 检查账号摘要适配器是否拥有必要的数据库子仓储。
func (r *AccountSummaryRepository) validateCookies() error {
	if r == nil || r.store == nil || r.store.Cookies == nil {
		return errors.New("账号摘要数据库适配器未初始化")
	}
	return nil
}

// accountSummaryModel 将数据库摘要转换为不含敏感字段的应用模型。
func accountSummaryModel(record db.CookieSummary) accountapp.AccountSummary {
	return accountapp.AccountSummary{
		ID: record.ID, UserID: record.UserID, AutoConfirm: record.AutoConfirm,
		Remark: record.Remark, PauseDuration: record.PauseDuration, PausedUntil: record.PausedUntil,
		Username: record.Username, ShowBrowser: record.ShowBrowser, Nickname: record.Nickname,
		AvatarURL: record.AvatarURL, LastRefreshAt: record.LastRefreshAt, LoginMethod: record.LoginMethod,
		LastLoginAt: record.LastLoginAt, CreatedAt: record.CreatedAt,
	}
}

// normalizeAccountSummaryError 将数据库所有权错误转换为应用层稳定错误。
func normalizeAccountSummaryError(err error) error {
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

var _ accountapp.AccountSummaryRepository = (*AccountSummaryRepository)(nil)
var _ accountapp.AdminSummaryRepository = (*AccountSummaryRepository)(nil)
