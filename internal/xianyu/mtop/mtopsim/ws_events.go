package mtopsim

import (
	"encoding/json"
	"fmt"
)

// 本文件构造平台在交易阶段变化时推送给卖家的 WebSocket 系统消息（已解密的报文对象）。
// 自动化交易流程由这些消息驱动，因此模拟器在推进订单阶段的同时提供对应报文，
// 让测试可以完整走通“平台事件 → 事件识别 → 自动化 → MTOP 请求 → 平台状态变化”。
// 报文结构取自项目已记录的真实样本；全部报文都先序列化再解码，数字类型与生产解码结果一致。

// 平台交易卡片的固定展示文案，自动化中心据此识别交易阶段。
const (
	// cardTitleBargainPending 是砍价第一阶段卡片标题。
	cardTitleBargainPending = "我已小刀，待刀成"
	// cardTitleBargainReady 是砍价第二阶段卡片标题。
	cardTitleBargainReady = "我已成功小刀，待发货"
)

// decodeEvent 把构造好的报文序列化后重新解码，得到与生产 WebSocket 解码一致的 map 结构。
func decodeEvent(message map[string]any) map[string]any {
	// raw 是报文的 JSON 文本；构造的报文只含可序列化的基础类型。
	raw, _ := json.Marshal(message)
	// decoded 是按生产方式解码后的报文。
	decoded := map[string]any{}
	_ = json.Unmarshal(raw, &decoded)
	return decoded
}

// jsonText 把对象序列化为内嵌 JSON 字符串；平台报文中的 bizTag、extJson 和卡片内容都是这种形式。
func jsonText(value any) string {
	// raw 是对象的 JSON 文本。
	raw, _ := json.Marshal(value)
	return string(raw)
}

// noticeEvent 构造新版通知卡片：字段 1 是消息 ID，字段 2 是会话，字段 3 是方向（1 表示系统），字段 4 是通知内容。
// roleTask 是 bizTag 中带买卖角色的任务名，为空时不携带 bizTag（平台的评价通知即如此）；businessKey 是 updateKey 中的业务阶段键。
func (g *Gateway) noticeEvent(order *Order, content, redReminder, roleTask, businessKey, contentType string) map[string]any {
	// notice 是通知内容节点。
	notice := map[string]any{
		"reminderContent": content,
		"redReminder":     redReminder,
		"senderUserId":    order.BuyerID,
		"extJson":         jsonText(map[string]any{"updateKey": fmt.Sprintf("%s:%s:10:%s:26", order.ChatID, order.ID, businessKey), "contentType": contentType}),
		"reminderUrl":     fmt.Sprintf("fleamarket://message_chat?itemId=%s&peerUserId=%s&sid=%s", order.ItemID, order.BuyerID, order.ChatID),
	}
	if roleTask != "" {
		notice["bizTag"] = jsonText(map[string]any{"taskName": roleTask})
	}
	return decodeEvent(map[string]any{
		"1": fmt.Sprintf("msg-%d", g.nextSequence()),
		"2": order.ChatID + "@goofish",
		"3": 1,
		"4": notice,
	})
}

// cardEvent 构造完整信封的交易卡片：卡片标题和订单详情跳转链接位于内嵌 JSON 中，role 是跳转链接声明的接收方角色。
func (g *Gateway) cardEvent(order *Order, title, role string) map[string]any {
	// card 是交易卡片内容，标题决定交易阶段，按钮链接携带订单号和角色。
	card := jsonText(map[string]any{"dxCard": map[string]any{"item": map[string]any{"main": map[string]any{"exContent": map[string]any{
		"title":  title,
		"button": map[string]any{"text": "查看订单", "targetUrl": fmt.Sprintf("fleamarket://order_detail?id=%s&role=%s", order.ID, role)},
	}}}}})
	return decodeEvent(map[string]any{"1": map[string]any{
		"2": order.ChatID + "@goofish",
		"7": 1,
		"10": map[string]any{
			"reminderContent": "[卡片消息]",
			"senderUserId":    order.BuyerID,
			"reminderUrl":     fmt.Sprintf("fleamarket://message_chat?itemId=%s&peerUserId=%s&sid=%s", order.ItemID, order.BuyerID, order.ChatID),
			"extJson":         jsonText(map[string]any{"contentType": "6"}),
		},
		"6": map[string]any{"3": map[string]any{"4": "6", "5": card}},
	}})
}

// CreatedEvent 返回卖家收到的“买家已拍下，待付款”系统消息。
func (g *Gateway) CreatedEvent(orderID string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.noticeEvent(g.mustOrder(orderID), "[我已拍下，待付款]", "等待买家付款", "已拍下_未付款_卖家", "CREATE", "26")
}

// PaidEvent 返回卖家收到的“买家已付款，等待发货”系统消息。
func (g *Gateway) PaidEvent(orderID string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.noticeEvent(g.mustOrder(orderID), "[我已付款，等待你发货]", "等待卖家发货", "付款完成待发货_卖家-正向升级", "PAID", "26")
}

// PaidEventBuyerCopy 返回同一笔付款事件推送给买家账号的副本；它声明买家角色，任何账号都不得据此执行卖家自动化。
func (g *Gateway) PaidEventBuyerCopy(orderID string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.noticeEvent(g.mustOrder(orderID), "[我已付款，等待你发货]", "等待卖家发货", "已付款_买家", "PAID", "26")
}

// BargainPendingEvent 返回卖家收到的砍价第一阶段卡片“我已小刀，待刀成”。
func (g *Gateway) BargainPendingEvent(orderID string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cardEvent(g.mustOrder(orderID), cardTitleBargainPending, "seller")
}

// BargainReadyEvent 返回卖家收到的砍价第二阶段卡片“我已成功小刀，待发货”。
func (g *Gateway) BargainReadyEvent(orderID string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.cardEvent(g.mustOrder(orderID), cardTitleBargainReady, "seller")
}

// CompletedEvent 返回卖家收到的“买家已确认收货”系统消息。
func (g *Gateway) CompletedEvent(orderID string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	// order 是已确认收货的订单。
	order := g.mustOrder(orderID)
	return decodeEvent(map[string]any{"1": map[string]any{
		"2": order.ChatID + "@goofish",
		"7": 1,
		"10": map[string]any{
			"reminderContent": "快给ta一个评价吧～",
			"reminderTitle":   "买家已确认收货",
			"senderUserId":    order.BuyerID,
			"reminderUrl":     fmt.Sprintf("fleamarket://order_detail?id=%s&role=seller&peerUserId=%s&sid=%s", order.ID, order.BuyerID, order.ChatID),
			"extJson":         jsonText(map[string]any{"contentType": "25"}),
		},
	}})
}

// BuyerReviewedEvent 返回卖家收到的“买家已评价”系统消息；BUYER_RATE_SELLER 是该事件的稳定业务键。
// 与平台真实报文一致，评价通知不携带买卖角色，自动化中心必须依据本地订单事实核验卖家身份。
func (g *Gateway) BuyerReviewedEvent(orderID string) map[string]any {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.noticeEvent(g.mustOrder(orderID), "[我完成了评价]", "有新交易评价", "", "BUYER_RATE_SELLER", "26")
}
