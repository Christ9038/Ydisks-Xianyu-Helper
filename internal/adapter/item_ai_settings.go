package adapter

import (
	"context"
	"errors"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// ItemAISettingsRepository 将商品级 AI 配置应用端口适配到数据库仓储。
type ItemAISettingsRepository struct {
	// store 提供非敏感归属查询和商品 AI 配置持久化能力。
	store *db.Store
}

// NewItemAISettingsRepository 从商品专用依赖创建配置适配器，避免组合层接触通用 Store。
func NewItemAISettingsRepository(dependencies *ItemDependencies) *ItemAISettingsRepository {
	if dependencies == nil {
		return &ItemAISettingsRepository{}
	}
	return &ItemAISettingsRepository{store: dependencies.store}
}

// AccountOwned 判断账号是否属于指定用户，查询过程不读取或解密 Cookie。
func (repository *ItemAISettingsRepository) AccountOwned(ctx context.Context, userID int64, cookieID string) (bool, error) {
	if repository == nil || repository.store == nil || repository.store.Cookies == nil {
		return false, errors.New("商品 AI 配置账号仓储未初始化")
	}
	return repository.store.Cookies.ExistsOwned(ctx, userID, cookieID)
}

// ItemExists 判断账号下当前未删除商品是否存在。
func (repository *ItemAISettingsRepository) ItemExists(ctx context.Context, cookieID, itemID string) (bool, error) {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return false, errors.New("商品 AI 配置商品仓储未初始化")
	}
	// itemErr 保存当前商品详情存在性查询错误。
	_, itemErr := repository.store.Items.Get(ctx, cookieID, itemID)
	if errors.Is(itemErr, db.ErrNotFound) {
		return false, nil
	}
	if itemErr != nil {
		return false, itemErr
	}
	return true, nil
}

// Get 读取商品级 AI 配置并转换为应用模型。
func (repository *ItemAISettingsRepository) Get(ctx context.Context, cookieID, itemID string) (itemapp.ItemAISettings, error) {
	if repository == nil || repository.store == nil || repository.store.ItemAISettings == nil {
		return itemapp.ItemAISettings{}, errors.New("商品 AI 配置存储未初始化")
	}
	// row 和 readErr 保存数据库配置行及读取错误。
	row, readErr := repository.store.ItemAISettings.Get(ctx, cookieID, itemID)
	if readErr != nil {
		return itemapp.ItemAISettings{}, readErr
	}
	return itemapp.ItemAISettings{CookieID: row.CookieID, ItemID: row.ItemID, Override: row.Override, Context: row.Context}, nil
}

// Upsert 将应用配置转换为数据库行并持久化。
func (repository *ItemAISettingsRepository) Upsert(ctx context.Context, settings itemapp.ItemAISettings) error {
	if repository == nil || repository.store == nil || repository.store.ItemAISettings == nil {
		return errors.New("商品 AI 配置存储未初始化")
	}
	return repository.store.ItemAISettings.Upsert(ctx, db.ItemAISettingsRow{
		CookieID: settings.CookieID,
		ItemID:   settings.ItemID,
		Override: settings.Override,
		Context:  settings.Context,
	})
}

// Delete 删除商品独立配置并恢复默认继承语义。
func (repository *ItemAISettingsRepository) Delete(ctx context.Context, cookieID, itemID string) error {
	if repository == nil || repository.store == nil || repository.store.ItemAISettings == nil {
		return errors.New("商品 AI 配置存储未初始化")
	}
	return repository.store.ItemAISettings.Delete(ctx, cookieID, itemID)
}

var _ itemapp.ItemAISettingsRepository = (*ItemAISettingsRepository)(nil)
