package adapter

import (
	"context"
	"errors"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// ItemCatalogRepository 将商品读取 Port 适配到数据库商品仓储。
type ItemCatalogRepository struct {
	// store 提供商品查询能力。
	store *db.Store
}

// NewItemCatalogRepository 创建商品读取数据库适配器。
func NewItemCatalogRepository(store *db.Store) *ItemCatalogRepository {
	return &ItemCatalogRepository{store: store}
}

// ListForUser 查询用户范围商品并转换为应用模型。
func (repository *ItemCatalogRepository) ListForUser(ctx context.Context, userID int64, cookieID string) ([]itemapp.CatalogItem, error) {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return nil, errors.New("商品读取存储未初始化")
	}
	// rows 和 err 保存用户范围商品行及查询错误。
	rows, err := repository.store.Items.ListForUser(ctx, userID, cookieID)
	if err != nil {
		return nil, err
	}
	return catalogItemsFromRows(rows), nil
}

// ListByCookie 查询账号商品并转换为应用模型。
func (repository *ItemCatalogRepository) ListByCookie(ctx context.Context, cookieID string) ([]itemapp.CatalogItem, error) {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return nil, errors.New("商品读取存储未初始化")
	}
	// rows 和 err 保存账号商品行及查询错误。
	rows, err := repository.store.Items.AllForCookie(ctx, cookieID)
	if err != nil {
		return nil, err
	}
	return catalogItemsFromRows(rows), nil
}

// Get 查询单个商品并转换为应用模型。
func (repository *ItemCatalogRepository) Get(ctx context.Context, cookieID, itemID string) (itemapp.CatalogItem, error) {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return itemapp.CatalogItem{}, errors.New("商品读取存储未初始化")
	}
	// row 和 err 保存单个商品行及查询错误。
	row, err := repository.store.Items.Get(ctx, cookieID, itemID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return itemapp.CatalogItem{}, itemapp.ErrCatalogNotFound
		}
		return itemapp.CatalogItem{}, err
	}
	return itemapp.CatalogItem{ID: row.ID, CookieID: row.CookieID, ItemID: row.ItemID, ItemTitle: row.ItemTitle, ItemDescription: row.ItemDescription, ItemCategory: row.ItemCategory, ItemPrice: row.ItemPrice, ItemDetail: row.ItemDetail, IsMultiSpec: row.IsMultiSpec, MultiQuantityDelivery: row.MultiQuantityDelivery}, nil
}

// Upsert 创建或完整保存本地商品，并将应用输入转换为数据库行模型。
func (repository *ItemCatalogRepository) Upsert(ctx context.Context, cookieID string, input itemapp.CatalogWriteInput) error {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return errors.New("商品写入存储未初始化")
	}
	return repository.store.Items.Upsert(ctx, &db.ItemInfoRow{
		CookieID: cookieID, ItemID: input.ItemID, ItemTitle: input.ItemTitle, ItemDescription: input.ItemDescription,
		ItemCategory: input.ItemCategory, ItemPrice: input.ItemPrice, ItemDetail: input.ItemDetail,
		IsMultiSpec: input.IsMultiSpec, MultiQuantityDelivery: input.MultiQuantityDelivery,
	})
}

// UpsertPublishedItem 保存批量发布成功后的商品目录记录。
func (repository *ItemCatalogRepository) UpsertPublishedItem(ctx context.Context, input itemapp.BatchPublishedItem) error {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return errors.New("商品写入存储未初始化")
	}
	return repository.store.Items.Upsert(ctx, &db.ItemInfoRow{
		CookieID: input.CookieID, ItemID: input.ItemID, ItemTitle: input.ItemTitle, ItemDescription: input.ItemDescription,
		ItemCategory: input.ItemCategory, ItemPrice: input.ItemPrice, ItemDetail: input.ItemDetail,
		MultiQuantityDelivery: input.MultiQuantityDelivery,
	})
}

// Delete 逻辑删除本地商品及其商品级自动化规则。
func (repository *ItemCatalogRepository) Delete(ctx context.Context, cookieID, itemID string) error {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return errors.New("商品写入存储未初始化")
	}
	return repository.store.Items.Delete(ctx, cookieID, itemID)
}

// SetMultiSpec 更新本地商品的多规格交付开关。
func (repository *ItemCatalogRepository) SetMultiSpec(ctx context.Context, cookieID, itemID string, enabled bool) error {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return errors.New("商品写入存储未初始化")
	}
	return repository.store.Items.SetMultiSpec(ctx, cookieID, itemID, enabled)
}

// SetMultiQuantity 更新本地商品的多数量交付开关。
func (repository *ItemCatalogRepository) SetMultiQuantity(ctx context.Context, cookieID, itemID string, enabled bool) error {
	if repository == nil || repository.store == nil || repository.store.Items == nil {
		return errors.New("商品写入存储未初始化")
	}
	return repository.store.Items.SetMultiQuantity(ctx, cookieID, itemID, enabled)
}

// catalogItemsFromRows 转换数据库商品行并保持查询顺序。
func catalogItemsFromRows(rows []db.ItemInfoRow) []itemapp.CatalogItem {
	// items 保存转换后的应用商品模型并保持数据库顺序。
	items := make([]itemapp.CatalogItem, 0, len(rows))
	// row 表示当前待转换的数据库商品行。
	for _, row := range rows {
		items = append(items, itemapp.CatalogItem{ID: row.ID, CookieID: row.CookieID, ItemID: row.ItemID, ItemTitle: row.ItemTitle, ItemDescription: row.ItemDescription, ItemCategory: row.ItemCategory, ItemPrice: row.ItemPrice, ItemDetail: row.ItemDetail, IsMultiSpec: row.IsMultiSpec, MultiQuantityDelivery: row.MultiQuantityDelivery})
	}
	return items
}

var _ itemapp.CatalogRepository = (*ItemCatalogRepository)(nil)
var _ itemapp.CatalogMutationRepository = (*ItemCatalogRepository)(nil)
