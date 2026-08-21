package automation

import (
	"context"

	"xianyu-go/internal/xianyu/mtop"
)

// CenterDependencies 保存自动化中心启动时必须固定的外部协作依赖。
type CenterDependencies struct {
	// MTop 提供确认发货使用的 MTOP 协议客户端；为空时使用默认实现。
	MTop mtop.Client
	// AccountTaskClient 提供自动评价与商品擦亮的协议调用；使用默认 MTOP 时可自动复用其任务能力。
	AccountTaskClient AccountTaskClient
	// OrderDetailFetcher 提供自动发货前的订单详情查询能力。
	OrderDetailFetcher OrderDetailFetcher
	// Notifier 接收发货结果通知；为空时不发送通知。
	Notifier Notifier
	// CookieSource 提供自动发货读取 Cookie 的可替换边界；为空时读取仓储。
	CookieSource func(context.Context, string) (string, error)
	// APICardFetcher 提供普通 API 卡发货请求能力；为空时 API 卡执行会明确失败。
	APICardFetcher APICardFetcher
}
