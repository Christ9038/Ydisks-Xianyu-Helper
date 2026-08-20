package automation

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// handleAIPricingMode 在订单创建事件中优先执行 AI 报价，并阻止互斥的固定价格规则继续运行。
func (c *Center) handleAIPricingMode(ctx context.Context, task Task) (bool, error) {
	if task.TriggerType != TriggerOrderCreated || c.store.AIReply == nil {
		return false, nil
	}
	// aiEnabled、autoAdjustEnabled 是账号的互斥议价模式和真实改价开关；modeErr 是读取错误。
	aiEnabled, autoAdjustEnabled, modeErr := c.store.AIReply.PricingMode(ctx, task.AccountID)
	if modeErr != nil {
		return false, fmt.Errorf("读取 AI 议价改价模式: %w", modeErr)
	}
	if !aiEnabled {
		return false, nil
	}
	if !autoAdjustEnabled {
		c.logger.Info("AI 议价已接管订单价格，但商家未开启真实自动改价", "account", task.AccountID, "order_id", task.OrderID)
		return true, nil
	}
	if task.OrderID == "" || task.ChatID == "" || task.BuyerID == "" || task.ItemID == "" {
		c.logger.Warn("订单事实不足，无法匹配 AI 报价", "account", task.AccountID, "order_id", task.OrderID)
		return true, nil
	}
	// quote 是与账号、买家、商品、会话和有效期匹配后原子领取的最新报价；claimErr 是领取错误。
	quote, claimErr := c.store.AIReply.ClaimPendingQuote(ctx, task.AccountID, task.ChatID, task.BuyerID, task.ItemID, task.OrderID, time.Now().UTC().Unix())
	if claimErr != nil {
		return true, fmt.Errorf("领取 AI 自动改价报价: %w", claimErr)
	}
	if quote == nil {
		c.logger.Info("订单没有可用的 AI 有效报价，不执行改价", "account", task.AccountID, "order_id", task.OrderID, "item_id", task.ItemID)
		return true, nil
	}
	// targetPriceCents 是 AI 单件报价按已知购买数量折算后的订单目标总价。
	targetPriceCents := quote.PriceCents
	// quantity 是订单事实中可确认的购买数量；缺失时按闲鱼单件订单处理。
	quantity := parsePositiveInt(task.Quantity)
	if quantity > 1 {
		if quote.PriceCents > 100000000/int64(quantity) {
			// finishErr 是超出平台金额边界时把报价标记为明确失败的状态保存错误。
			finishErr := c.store.AIReply.FinishQuote(ctx, quote.ID, "failed", "AI 报价按订单数量折算后超出允许金额")
			if finishErr != nil {
				return true, finishErr
			}
			return true, fmt.Errorf("%w: AI 报价按订单数量折算后超出允许金额", errActionNotPerformed)
		}
		targetPriceCents *= int64(quantity)
	}
	// adjustErr 是复用凭证恢复、Cookie 合并、暂时性平台繁忙重试和不确定结果分类后的真实改价结果。
	adjustErr := c.actions.adjustOrderPriceWithRetry(ctx, task, targetPriceCents)
	// status、errorMessage 是报价执行终态及不含凭证明文的错误摘要。
	status, errorMessage := "adjusted", ""
	if adjustErr != nil {
		status, errorMessage = "failed", adjustErr.Error()
		// uncertain 表示请求可能已经被平台执行，禁止自动重试并交由人工核对。
		var uncertain *uncertainActionError
		if errors.As(adjustErr, &uncertain) {
			status = "needs_review"
		}
	}
	// finishErr 是报价状态持久化失败原因；远端成功后失败必须按结果未知上报。
	finishErr := c.store.AIReply.FinishQuote(ctx, quote.ID, status, errorMessage)
	if finishErr != nil {
		if adjustErr == nil {
			return true, uncertainAction(fmt.Errorf("闲鱼已完成 AI 报价改价，但本地状态保存失败: %w", finishErr))
		}
		return true, errors.Join(adjustErr, fmt.Errorf("保存 AI 报价改价状态: %w", finishErr))
	}
	if adjustErr != nil {
		return true, adjustErr
	}
	c.logger.Info("AI 报价已自动应用到订单", "account", task.AccountID, "order_id", task.OrderID, "quote_id", quote.ID)
	return true, nil
}
