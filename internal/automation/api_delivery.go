package automation

import "context"

// APICardFetcher 是自动化消费者定义的普通 API 卡发货最小接口。
type APICardFetcher interface {
	// Fetch 为一个发货单位获取卡密内容；Dispatched 表示远端可能已经产生副作用。
	Fetch(context.Context, APICardRequest) (APICardResult, error)
}

// APICardRequest 描述一次 API 卡发货单位的完整业务上下文，不包含账号 Cookie。
type APICardRequest struct {
	// Config 是已由专用仓储读取的完整 API 配置 JSON。
	Config string
	// TriggerKey 是本次自动化运行的稳定防重键。
	TriggerKey string
	// ActionID 是规则动作稳定标识。
	ActionID int64
	// CardID 是 API 卡券组稳定标识。
	CardID int64
	// UnitIndex 是从 1 开始的当前发货单位序号。
	UnitIndex int
	// TotalUnits 是本动作本次应发出的总单位数。
	TotalUnits int
	// AccountID 是账号或 Cookie 稳定标识。
	AccountID string
	// OrderID 是订单稳定标识。
	OrderID string
	// ItemID 是商品稳定标识。
	ItemID string
	// BuyerID 是买家稳定标识。
	BuyerID string
	// ChatID 是聊天会话稳定标识。
	ChatID string
	// SpecName 是订单规格名称。
	SpecName string
	// SpecValue 是订单规格值。
	SpecValue string
	// Quantity 是订单购买数量文本。
	Quantity string
	// Amount 是订单金额文本。
	Amount string
	// TriggerType 是触发事件类型。
	TriggerType string
}

// APICardResult 保存 API 卡请求取得的内容及远端副作用状态。
type APICardResult struct {
	// Content 是成功提取并准备发送给买家的卡密文本。
	Content string
	// Dispatched 表示请求已发出但最终结果可能不明。
	Dispatched bool
}
