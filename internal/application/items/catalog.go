package items

import (
	"context"
	"errors"
)

// ErrCatalogNotFound 表示请求的本地商品不存在。
var ErrCatalogNotFound = errors.New("商品不存在")

// CatalogItem 是商品读取用例使用的非敏感商品模型。
type CatalogItem struct {
	// ID 是本地商品记录标识。
	ID int64
	// CookieID 是商品所属账号标识。
	CookieID string
	// ItemID 是平台商品标识。
	ItemID string
	// ItemTitle 是商品标题。
	ItemTitle string
	// ItemDescription 是商品描述。
	ItemDescription string
	// ItemCategory 是平台商品类目标识。
	ItemCategory string
	// ItemPrice 是商品价格文本。
	ItemPrice string
	// ItemDetail 是商品扩展详情 JSON。
	ItemDetail string
	// IsMultiSpec 表示商品是否启用多规格交付。
	IsMultiSpec bool
	// MultiQuantityDelivery 表示商品是否启用多数量交付。
	MultiQuantityDelivery bool
}

// CatalogRepository 定义商品列表和详情读取所需的最小持久化能力。
type CatalogRepository interface {
	// ListForUser 查询用户范围内的商品，可按账号筛选。
	ListForUser(context.Context, int64, string) ([]CatalogItem, error)
	// ListByCookie 查询指定账号下的商品。
	ListByCookie(context.Context, string) ([]CatalogItem, error)
	// Get 查询指定账号下的单个商品。
	Get(context.Context, string, string) (CatalogItem, error)
}

// CatalogService 编排商品列表和详情读取用例。
type CatalogService struct {
	// repository 保存商品读取适配器。
	repository CatalogRepository
}

// NewCatalogService 创建商品读取应用服务并校验必需端口。
func NewCatalogService(repository CatalogRepository) (*CatalogService, error) {
	if repository == nil {
		return nil, errors.New("商品读取仓储端口不能为空")
	}
	return &CatalogService{repository: repository}, nil
}

// ListForUser 返回当前用户可见的商品列表。
func (service *CatalogService) ListForUser(ctx context.Context, userID int64, cookieID string) ([]CatalogItem, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("商品读取服务未初始化")
	}
	return service.repository.ListForUser(ctx, userID, cookieID)
}

// ListByCookie 返回指定账号下的商品列表。
func (service *CatalogService) ListByCookie(ctx context.Context, cookieID string) ([]CatalogItem, error) {
	if service == nil || service.repository == nil {
		return nil, errors.New("商品读取服务未初始化")
	}
	return service.repository.ListByCookie(ctx, cookieID)
}

// Get 返回指定账号下的商品详情。
func (service *CatalogService) Get(ctx context.Context, cookieID, itemID string) (CatalogItem, error) {
	if service == nil || service.repository == nil {
		return CatalogItem{}, errors.New("商品读取服务未初始化")
	}
	return service.repository.Get(ctx, cookieID, itemID)
}
