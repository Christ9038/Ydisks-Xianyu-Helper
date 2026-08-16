package items

import (
	"context"
	"errors"
)

// CatalogWriteInput 是创建或完整保存本地商品所需的应用层输入。
type CatalogWriteInput struct {
	// ItemID 是平台商品标识，也是账号范围内的业务唯一键。
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

// CatalogPatchInput 是更新本地商品时的可选字段集合；nil 表示保留旧值。
type CatalogPatchInput struct {
	// ItemTitle 是待更新的商品标题；nil 表示保留原值。
	ItemTitle *string
	// ItemDescription 是待更新的商品描述；nil 表示保留原值。
	ItemDescription *string
	// ItemCategory 是待更新的商品类目标识；nil 表示保留原值。
	ItemCategory *string
	// ItemPrice 是待更新的商品价格文本；nil 表示保留原值。
	ItemPrice *string
	// ItemDetail 是待更新的商品扩展详情；nil 表示保留原值。
	ItemDetail *string
	// IsMultiSpec 是待更新的多规格开关；nil 表示保留原值。
	IsMultiSpec *bool
	// MultiQuantityDelivery 是待更新的多数量交付开关；nil 表示保留原值。
	MultiQuantityDelivery *bool
}

// CatalogMutationRepository 定义本地商品写入和开关变更所需的最小持久化能力。
type CatalogMutationRepository interface {
	// Get 读取更新操作需要的现有商品记录。
	Get(context.Context, string, string) (CatalogItem, error)
	// Upsert 创建或完整保存本地商品记录。
	Upsert(context.Context, string, CatalogWriteInput) error
	// Delete 逻辑删除商品及其商品级自动化规则。
	Delete(context.Context, string, string) error
	// SetMultiSpec 设置商品多规格开关。
	SetMultiSpec(context.Context, string, string, bool) error
	// SetMultiQuantity 设置商品多数量交付开关。
	SetMultiQuantity(context.Context, string, string, bool) error
}

// CatalogMutationService 编排本地商品创建、更新、删除和交付开关变更。
type CatalogMutationService struct {
	// repository 保存商品写入适配器。
	repository CatalogMutationRepository
}

// NewCatalogMutationService 创建商品写应用服务并校验必需端口。
func NewCatalogMutationService(repository CatalogMutationRepository) (*CatalogMutationService, error) {
	if repository == nil {
		return nil, errors.New("商品写入仓储端口不能为空")
	}
	return &CatalogMutationService{repository: repository}, nil
}

// Create 创建或恢复指定账号下的本地商品记录。
func (service *CatalogMutationService) Create(ctx context.Context, cookieID string, input CatalogWriteInput) error {
	if service == nil || service.repository == nil {
		return errors.New("商品写入服务未初始化")
	}
	return service.repository.Upsert(ctx, cookieID, input)
}

// Update 读取现有商品、合并局部字段并保存完整记录。
func (service *CatalogMutationService) Update(ctx context.Context, cookieID, itemID string, patch CatalogPatchInput) error {
	if service == nil || service.repository == nil {
		return errors.New("商品写入服务未初始化")
	}
	// existing、err 保存更新前的商品记录及查询错误。
	existing, err := service.repository.Get(ctx, cookieID, itemID)
	if err != nil {
		return err
	}
	// input 保存合并后的完整商品写入模型。
	input := CatalogWriteInput{
		ItemID: itemID, ItemTitle: existing.ItemTitle, ItemDescription: existing.ItemDescription,
		ItemCategory: existing.ItemCategory, ItemPrice: existing.ItemPrice, ItemDetail: existing.ItemDetail,
		IsMultiSpec: existing.IsMultiSpec, MultiQuantityDelivery: existing.MultiQuantityDelivery,
	}
	if patch.ItemTitle != nil {
		input.ItemTitle = *patch.ItemTitle
	}
	if patch.ItemDescription != nil {
		input.ItemDescription = *patch.ItemDescription
	}
	if patch.ItemCategory != nil {
		input.ItemCategory = *patch.ItemCategory
	}
	if patch.ItemPrice != nil {
		input.ItemPrice = *patch.ItemPrice
	}
	if patch.ItemDetail != nil {
		input.ItemDetail = *patch.ItemDetail
	}
	if patch.IsMultiSpec != nil {
		input.IsMultiSpec = *patch.IsMultiSpec
	}
	if patch.MultiQuantityDelivery != nil {
		input.MultiQuantityDelivery = *patch.MultiQuantityDelivery
	}
	return service.repository.Upsert(ctx, cookieID, input)
}

// Delete 逻辑删除指定账号下的本地商品。
func (service *CatalogMutationService) Delete(ctx context.Context, cookieID, itemID string) error {
	if service == nil || service.repository == nil {
		return errors.New("商品写入服务未初始化")
	}
	return service.repository.Delete(ctx, cookieID, itemID)
}

// SetMultiSpec 更新指定商品的多规格交付开关。
func (service *CatalogMutationService) SetMultiSpec(ctx context.Context, cookieID, itemID string, enabled bool) error {
	if service == nil || service.repository == nil {
		return errors.New("商品写入服务未初始化")
	}
	return service.repository.SetMultiSpec(ctx, cookieID, itemID, enabled)
}

// SetMultiQuantity 更新指定商品的多数量交付开关。
func (service *CatalogMutationService) SetMultiQuantity(ctx context.Context, cookieID, itemID string, enabled bool) error {
	if service == nil || service.repository == nil {
		return errors.New("商品写入服务未初始化")
	}
	return service.repository.SetMultiQuantity(ctx, cookieID, itemID, enabled)
}
