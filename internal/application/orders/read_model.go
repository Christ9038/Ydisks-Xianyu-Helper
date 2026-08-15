// Package orders 定义订单用例面向消费者的纯业务查询模型和 Port。
// 本包不得依赖数据库、HTTP、平台协议或 Server 实现。
package orders

import "context"

// OrderRow 是订单列表用例需要的纯业务展示行。
type OrderRow struct {
	// OrderID 是订单稳定标识。
	OrderID string
	// ItemID 是关联商品标识。
	ItemID string
	// ItemTitle 是关联商品标题。
	ItemTitle string
	// ItemDetail 是关联商品详情 JSON。
	ItemDetail string
	// BuyerID 是买家标识。
	BuyerID string
	// SpecName 是规格名称。
	SpecName string
	// SpecValue 是规格值。
	SpecValue string
	// Quantity 是购买数量。
	Quantity string
	// Amount 是订单金额文本。
	Amount string
	// OrderStatus 是持久化的订单状态。
	OrderStatus string
	// CookieID 是订单所属账号标识。
	CookieID string
	// IsBargain 表示订单是否为砍价订单。
	IsBargain int
	// SystemShipped 表示是否由系统确认发货。
	SystemShipped bool
	// ReceiverName 是收货人姓名。
	ReceiverName string
	// ReceiverPhone 是收货人电话。
	ReceiverPhone string
	// ReceiverAddr 是收货地址。
	ReceiverAddr string
	// ReceiverCity 是收货城市。
	ReceiverCity string
	// CreatedAt 是订单创建时间。
	CreatedAt string
	// UpdatedAt 是订单更新时间。
	UpdatedAt string
}

// Order 是订单详情和发货用例使用的纯业务实体。
type Order struct {
	// OrderID 是订单稳定标识。
	OrderID string
	// ItemID 是关联商品标识。
	ItemID string
	// BuyerID 是买家标识。
	BuyerID string
	// SpecName 是规格名称。
	SpecName string
	// SpecValue 是规格值。
	SpecValue string
	// Quantity 是购买数量。
	Quantity string
	// Amount 是订单金额文本。
	Amount string
	// OrderStatus 是持久化的订单状态。
	OrderStatus string
	// CookieID 是订单所属账号标识。
	CookieID string
	// IsBargain 表示订单是否为砍价订单。
	IsBargain int
	// ReceiverName 是收货人姓名。
	ReceiverName string
	// ReceiverPhone 是收货人电话。
	ReceiverPhone string
	// ReceiverAddress 是收货地址。
	ReceiverAddress string
	// ReceiverCity 是收货城市。
	ReceiverCity string
	// Version 是订单乐观锁版本。
	Version int
	// ChatID 是聊天会话标识。
	ChatID string
	// SystemShipped 表示是否由系统确认发货。
	SystemShipped bool
	// PaidAt 是订单付款时间文本。
	PaidAt string
	// ShippedAt 是订单发货时间文本。
	ShippedAt string
	// CompletedAt 是订单完成时间文本。
	CompletedAt string
	// BuyerReviewedAt 是买家评价时间文本。
	BuyerReviewedAt string
	// LastReviewRequestAt 是最近一次索评时间文本。
	LastReviewRequestAt string
	// ReviewRequestCount 是索评次数。
	ReviewRequestCount int
	// CreatedAt 是订单创建时间。
	CreatedAt string
	// UpdatedAt 是订单更新时间。
	UpdatedAt string
}

// ItemInfo 是订单详情需要的纯商品信息。
type ItemInfo struct {
	// ID 是商品信息记录的本地标识。
	ID int64
	// CookieID 是商品所属账号标识。
	CookieID string
	// ItemID 是平台商品标识。
	ItemID string
	// ItemTitle 是商品标题。
	ItemTitle string
	// ItemDescription 是商品描述。
	ItemDescription string
	// ItemCategory 是商品分类。
	ItemCategory string
	// ItemPrice 是商品价格文本。
	ItemPrice string
	// ItemDetail 是商品详情 JSON。
	ItemDetail string
	// IsMultiSpec 表示商品是否启用多规格。
	IsMultiSpec bool
	// MultiQuantityDelivery 表示商品是否启用多数量发货。
	MultiQuantityDelivery bool
}

// PlatformRuntimeData 是订单刷新访问平台所需的最小账号运行视图。
type PlatformRuntimeData struct {
	// ID 是闲鱼账号的稳定标识。
	ID string
	// UserID 是账号所属的本地用户标识。
	UserID int64
	// Value 是 repository 解密后的 Cookie 明文，仅在平台请求边界短暂使用。
	Value string
	// MetadataJSON 是 Cookie 快照等平台请求元数据。
	MetadataJSON string
	// ShowBrowser 表示风控恢复是否允许使用可视化浏览器。
	ShowBrowser bool
}

// ListFilter 是订单列表查询的纯业务筛选条件。
type ListFilter struct {
	// UserID 是当前用户标识。
	UserID int64
	// CookieID 是可选的账号筛选条件。
	CookieID string
	// Status 是可选的订单状态筛选条件。
	Status string
	// Search 是订单号、商品或买家搜索词。
	Search string
	// Limit 是返回条数上限。
	Limit int
	// Offset 是分页偏移量。
	Offset int
}

// Reader 定义订单列表用例需要的只读 Port。
type Reader interface {
	// ListForUser 返回当前用户可见的订单展示行和总数。
	ListForUser(ctx context.Context, filter ListFilter) ([]OrderRow, int, error)
}
