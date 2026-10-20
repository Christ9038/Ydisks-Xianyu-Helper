package automation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"xianyu-go/internal/db"
)

// simplifiedEventResolveWindow 是简化系统事件按会话回溯近期订单的时间窗口；窗口外的订单不参与识别，保持原有延期行为。
const simplifiedEventResolveWindow = 30 * time.Minute

// resolvePaidTaskOrder 为交易系统消息回填本账号已有订单事实，避免未知角色事件因协议字段缺失而无法完成卖家核验。
func (c *Center) resolvePaidTaskOrder(ctx context.Context, task Task) (Task, error) {
	if c == nil || c.store == nil || c.store.Orders == nil || (task.TriggerType != TriggerOrderCreated && task.TriggerType != TriggerOrderPaid && task.TriggerType != TriggerBargainPending) {
		return task, nil
	}
	if task.OrderID != "" {
		// order、err 保存已有订单的最小本地事实；不存在时保留事件，交给角色门禁决定是否延期。
		order, err := c.store.Orders.Get(ctx, task.OrderID)
		if errors.Is(err, db.ErrNotFound) {
			return task, nil
		}
		if err != nil {
			return task, fmt.Errorf("按订单回填自动化事实: %w", err)
		}
		return mergeOrderIntoTask(task, order), nil
	}
	if task.ChatID == "" {
		return task, nil
	}
	if task.TriggerType != TriggerOrderCreated {
		// 付款与砍价阶段事件缺少订单号时不按会话猜测订单：买家加商品也不能唯一确定一笔交易，
		// 同一买家再次购买同一商品而新订单尚未同步时，旧的待发货订单会被误发卡，已发货旧订单会把新消息误当重复消息收口。
		// 事件保持订单号缺失，由角色门禁延期等待订单同步；订单同步后的丢消息兜底由待发货扫描负责。
		return task, nil
	}
	if task.ItemID != "" {
		// 拍下事件已带商品，说明不是只含会话标识的简化事件，不改写。
		return task, nil
	}
	// recent 保存同会话近期已被完整事件处理的订单（如已发货）；简化事件只含会话标识，
	// 命中后合并订单事实，使后续角色门禁把“订单已不是待发货”判为不可重试并安全收口，而不是反复延期后误报人工处理。
	recent, err := c.store.Orders.FindRecentByChat(ctx, task.AccountID, task.ChatID, task.BuyerID, task.ItemID, time.Now().Add(-simplifiedEventResolveWindow))
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return task, nil
		}
		return task, fmt.Errorf("按会话识别近期订单: %w", err)
	}
	if recent == nil {
		return task, nil
	}
	if c.logger != nil {
		c.logger.Info("简化交易事件已按会话关联近期订单", "account", task.AccountID, "order_id", recent.OrderID, "trigger", task.TriggerType)
	}
	return mergeOrderIntoTask(task, recent), nil
}

// prepareBuyerNickname 补齐模板渲染所需的买家昵称摘要。
func (c *Center) prepareBuyerNickname(ctx context.Context, task Task) (Task, error) {
	if task.BuyerNickname != "" || task.ChatID == "" {
		return task, nil
	}
	// nickname 保存聊天会话中可用于模板渲染的买家昵称。
	nickname, nicknameErr := c.store.Chats.BuyerNicknameForAutomation(ctx, task.AccountID, task.ChatID)
	if nicknameErr != nil {
		return task, fmt.Errorf("读取买家昵称: %w", nicknameErr)
	}
	task.BuyerNickname = nickname
	return task, nil
}

// mergeOrderIntoTask 用本地订单事实补全自动化任务中尚未获得的字段。
func mergeOrderIntoTask(task Task, order *db.Order) Task {
	if task.OrderID == "" {
		task.OrderID = order.OrderID
	}
	if task.ItemID == "" {
		task.ItemID = order.ItemID
	}
	if task.BuyerID == "" {
		task.BuyerID = order.BuyerID
	}
	if task.ChatID == "" {
		task.ChatID = order.ChatID
	}
	if task.SpecName == "" {
		task.SpecName = order.SpecName
	}
	if task.SpecValue == "" {
		task.SpecValue = order.SpecValue
	}
	if task.Quantity == "" {
		task.Quantity = order.Quantity
	}
	if task.Amount == "" {
		task.Amount = order.Amount
	}
	if task.OrderStatus == "" {
		task.OrderStatus = order.OrderStatus
	}
	// 本地订单同步只会在平台明确识别砍价活动时写入真值；任务一旦携带真值不得在后续补全中降级。
	if order.IsBargain != 0 {
		task.IsBargain = true
	}
	return task
}

// requireReliablePaidQuantity 要求付款发货在执行外部动作前已可靠取得正整数购买数量。
// 订单详情成功但缺少或无法识别 buyAmount 时，数量不得退化为 1 而少发卡密；返回错误使准备阶段延期重试。
func requireReliablePaidQuantity(task Task) error {
	if task.TriggerType != TriggerOrderPaid {
		return nil
	}
	if parsePositiveInt(task.Quantity) <= 0 {
		return fmt.Errorf("订单 %s 的购买数量缺失或无法识别，暂不自动发货", task.OrderID)
	}
	return nil
}
