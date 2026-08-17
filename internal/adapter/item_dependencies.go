package adapter

import (
	"context"
	"errors"
	"log/slog"

	"xianyu-go/internal/db"
)

// ItemDependencies 封装商品目录、发布、批量和同步用例所需的窄适配器构造能力。
// 它只在 adapter 内部持有 Store，避免商品装配依赖通用设施容器。
type ItemDependencies struct {
	// store 是商品适配器共享的数据库入口，不向 Server 或应用层暴露。
	store *db.Store
}

// NewItemDependencies 构造商品专用依赖，并在缺少数据库入口时立即返回错误。
func NewItemDependencies(store *db.Store) (*ItemDependencies, error) {
	if store == nil {
		return nil, errors.New("商品依赖 Store 不能为空")
	}
	return &ItemDependencies{store: store}, nil
}

// NewItemBatchRepository 创建批量发布状态仓储。
func (d *ItemDependencies) NewItemBatchRepository() *ItemBatchRepository {
	if d == nil {
		return nil
	}
	return NewItemBatchRepository(d.store)
}

// NewItemBatchPreviewPort 创建批量预检所需的归属和本地文件端口。
func (d *ItemDependencies) NewItemBatchPreviewPort() *ItemBatchPreviewPort {
	if d == nil {
		return nil
	}
	return NewItemBatchPreviewPort(d.store)
}

// NewItemBatchPublishPort 创建批量远端发布端口，并接收平台会话与图片 I/O 回调。
func (d *ItemDependencies) NewItemBatchPublishPort(client func() MTOPClient, logger *slog.Logger, update func(context.Context, string, string), recover func(context.Context, string, error), readFile ReadPublishImageFile, download DownloadPublishImageURL) *ItemBatchPublishPort {
	if d == nil {
		return nil
	}
	return NewItemBatchPublishPort(d.store, client, logger, update, recover, readFile, download)
}

// NewItemPublishPort 创建单商品发布与类目推荐共用的平台端口。
func (d *ItemDependencies) NewItemPublishPort(client func() MTOPClient, logger *slog.Logger, update func(context.Context, string, string), recover func(context.Context, string, error) bool) *ItemPublishPort {
	if d == nil {
		return nil
	}
	return NewItemPublishPort(d.store, client, logger, update, recover)
}

// NewItemPublishRepository 创建单商品发布结果仓储。
func (d *ItemDependencies) NewItemPublishRepository() *ItemPublishRepository {
	if d == nil {
		return nil
	}
	return NewItemPublishRepository(d.store)
}

// NewItemCatalogRepository 创建商品目录读写仓储。
func (d *ItemDependencies) NewItemCatalogRepository() *ItemCatalogRepository {
	if d == nil {
		return nil
	}
	return NewItemCatalogRepository(d.store)
}

// NewItemSyncRepository 创建商品同步端口，并接收平台会话回调。
func (d *ItemDependencies) NewItemSyncRepository(client func() MTOPClient, logger *slog.Logger, update func(context.Context, string, string), recover func(context.Context, string, error)) *ItemSyncRepository {
	if d == nil {
		return nil
	}
	return NewItemSyncRepository(d.store, client, logger, update, recover)
}
