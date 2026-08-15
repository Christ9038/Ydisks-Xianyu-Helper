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
