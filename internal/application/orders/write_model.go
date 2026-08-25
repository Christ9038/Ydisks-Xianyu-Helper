package orders

import "context"

// OrderPatch 是订单更新用例允许写入的可选字段集合。
type OrderPatch struct {
	// OrderStatus 是订单状态补丁。
	OrderStatus *string
	// ItemID 是商品标识补丁。
	ItemID *string
	// BuyerID 是买家标识补丁。
	BuyerID *string
	// SpecName 是规格名称补丁。
	SpecName *string
	// SpecValue 是规格值补丁。
	SpecValue *string
	// Quantity 是购买数量补丁。
	Quantity *string
	// Amount 是订单金额补丁。
	Amount *string
	// ReceiverName 是收货人补丁。
	ReceiverName *string
	// ReceiverPhone 是收货电话补丁。
	ReceiverPhone *string
	// ReceiverAddress 是收货地址补丁。
	ReceiverAddress *string
	// ReceiverCity 是收货城市补丁。
	ReceiverCity *string
	// ChatID 是聊天会话补丁。
	ChatID *string
	// SystemShipped 表示是否由系统完成发货。
	SystemShipped *bool
}

// ItemWrite 是订单事务需要写入的商品基础信息。
type ItemWrite struct {
	// CookieID 是商品所属账号标识。
	CookieID string
	// ItemID 是商品标识。
	ItemID string
	// ItemTitle 是商品标题。
	ItemTitle string
	// ItemPrice 是商品价格文本。
	ItemPrice string
	// ItemDetail 是商品详情 JSON。
	ItemDetail string
}

// UpsertOptions 是订单导入和同步使用的订单写入字段。
type UpsertOptions struct {
	// ItemID 是关联商品标识。
	ItemID string
	// BuyerID 是买家标识。
	BuyerID string
	// CookieID 是订单所属账号标识。
	CookieID string
	// CreatedAt 是平台订单创建时间；为空时由数据库生成本地写入时间。
	CreatedAt string
	// OrderStatus 是订单状态。
	OrderStatus string
	// SpecName 是规格名称。
	SpecName string
	// SpecValue 是规格值。
	SpecValue string
	// Quantity 是购买数量。
	Quantity string
	// Amount 是订单金额文本。
	Amount string
	// ReceiverName 是收货人姓名。
	ReceiverName string
	// ReceiverPhone 是收货人电话。
	ReceiverPhone string
	// ReceiverAddress 是收货地址。
	ReceiverAddress string
	// ReceiverCity 是收货城市。
	ReceiverCity string
	// ChatID 是聊天会话标识。
	ChatID string
	// IsBargain 表示是否为砍价订单。
	IsBargain *bool
	// SystemShipped 表示是否由系统完成发货。
	SystemShipped *bool
}

// Writer 定义订单事务内的持久化写入能力，不暴露数据库事务类型。
type Writer interface {
	// PatchOrder 更新订单可选字段。
	PatchOrder(ctx context.Context, orderID string, patch OrderPatch) error
	// UpsertItemBasic 写入商品基础信息。
	UpsertItemBasic(ctx context.Context, item ItemWrite) error
	// UpsertOrder 写入订单及其状态字段。
	UpsertOrder(ctx context.Context, orderID string, options UpsertOptions) error
}

// UnitOfWork 定义订单用例的事务边界，由基础设施适配器实现。
type UnitOfWork interface {
	// WithTransaction 在单个事务中执行订单写入，并负责提交或回滚。
	WithTransaction(ctx context.Context, work func(Writer) error) error
}
