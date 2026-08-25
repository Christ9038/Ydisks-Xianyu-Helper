package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
)

// sendCard 按规格和购买数量分配文本、图片或数据卡密。
func (e *automationActionExecutor) sendCard(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	// result 保存卡密动作的发送数量和短暂凭证；旧入口只返回数量以保持测试及兼容调用方稳定。
	result, err := e.sendCardWithProof(ctx, task, action)
	return result.sent, err
}

// sendCardWithProof 发送卡密并收集实际成功投递的文本或图片凭证，供同一运行的确认发货动作使用。
func (e *automationActionExecutor) sendCardWithProof(ctx context.Context, task Task, action db.AutomationAction) (actionExecutionResult, error) {
	if !actionMatchesOrderSpec(task, action) {
		return actionExecutionResult{}, nil
	}
	if action.CardID <= 0 {
		return actionExecutionResult{}, fmt.Errorf("发送卡密动作缺少卡密组ID")
	}
	// count 是当前订单需要发送的卡密数量。
	count := deliverySendCount(task, action)
	// card 是待发送的卡密组完整配置。
	card, err := e.store.Cards.GetForDelivery(ctx, action.CardID)
	if err != nil {
		return actionExecutionResult{}, err
	}
	if !card.Enabled {
		return actionExecutionResult{}, fmt.Errorf("卡密组 %d 已停用", card.ID)
	}
	if card.Type == "data" {
		return e.sendDataCardWithProof(ctx, task, card, count)
	}
	if card.Type == "api" {
		return e.sendAPICardWithProof(ctx, task, action, card, count)
	}
	// sent 是已经成功发送的卡密数量。
	sent := 0
	// proof 保存已经成功发送的文本和图片凭证。
	proof := shipmentDeliveryProof{}
	// i 表示当前卡密发送序号。
	for i := 0; i < count; i++ {
		// content、imageURL、readErr 分别是当前卡密组可发送的文本、图片地址和读取配置失败原因。
		content, imageURL, readErr := e.cardContent(ctx, card)
		if readErr != nil {
			return actionExecutionResult{sent: sent, proof: proof}, readErr
		}
		if imageURL != "" {
			// sendErr 保存图片消息发送错误。
			if sendErr := e.sendImage(ctx, task, imageURL, card.ID); sendErr != nil {
				return actionExecutionResult{sent: sent, proof: proof}, classifyMessageSendError(sendErr)
			}
			proof.picList = append(proof.picList, imageURL)
		}
		if strings.TrimSpace(content) != "" {
			// renderedContent 是实际发送给买家的文本，也作为确认发货凭证提交。
			renderedContent := renderTemplate(content, task)
			// sendErr 保存文字消息发送错误。
			if sendErr := e.sendText(ctx, task, renderedContent); sendErr != nil {
				return actionExecutionResult{sent: sent, proof: proof}, classifyMessageSendError(sendErr)
			}
			proof.tradeText = appendTradeText(proof.tradeText, renderedContent)
		}
		if strings.TrimSpace(content) == "" && strings.TrimSpace(imageURL) == "" {
			return actionExecutionResult{sent: sent, proof: proof}, fmt.Errorf("卡密组 %d 没有可发送内容", card.ID)
		}
		sent++
	}
	return actionExecutionResult{sent: sent, proof: proof}, nil
}

// sendAPICard 按发货单位逐次调用普通 API，并在消息发送失败时保留结果未知状态。
func (e *automationActionExecutor) sendAPICard(ctx context.Context, task Task, action db.AutomationAction, card *db.CardFull, count int) (int, error) {
	// result 保存 API 卡密发送的数量和短暂凭证；旧入口只返回数量。
	result, err := e.sendAPICardWithProof(ctx, task, action, card, count)
	return result.sent, err
}

// sendAPICardWithProof 逐单位获取并发送 API 卡密，同时收集确认发货所需的文本凭证。
func (e *automationActionExecutor) sendAPICardWithProof(ctx context.Context, task Task, action db.AutomationAction, card *db.CardFull, count int) (actionExecutionResult, error) {
	// fetcher 是构造期固定的 API 卡发货客户端。
	var fetcher APICardFetcher
	if e.apiFetcher != nil {
		fetcher = e.apiFetcher()
	}
	if fetcher == nil {
		return actionExecutionResult{}, errors.New("API 卡发货客户端未初始化")
	}
	// sent 是已经完成 API 获取和买家消息发送的单位数量。
	sent := 0
	// proof 保存已经成功发送的 API 卡密文本。
	proof := shipmentDeliveryProof{}
	// unitIndex 表示从 1 开始的当前 API 发货单位序号。
	for unitIndex := 1; unitIndex <= count; unitIndex++ {
		// result、fetchErr 保存当前单位的 API 响应与请求错误。
		result, fetchErr := fetcher.Fetch(ctx, APICardRequest{
			Config: card.APIConfig, TriggerKey: buildTriggerKey(task), ActionID: action.ID, CardID: card.ID,
			UnitIndex: unitIndex, TotalUnits: count, AccountID: task.AccountID, OrderID: task.OrderID,
			ItemID: task.ItemID, BuyerID: task.BuyerID, ChatID: task.ChatID, SpecName: task.SpecName,
			SpecValue: task.SpecValue, Quantity: task.Quantity, Amount: task.Amount, TriggerType: task.TriggerType,
		})
		if fetchErr != nil {
			if result.Dispatched {
				return actionExecutionResult{sent: sent, proof: proof}, uncertainAction(fetchErr)
			}
			return actionExecutionResult{sent: sent, proof: proof}, noRetryAction(fetchErr)
		}
		if strings.TrimSpace(result.Content) == "" {
			return actionExecutionResult{sent: sent, proof: proof}, uncertainAction(errors.New("API 卡发货响应没有可发送内容"))
		}
		// sendErr 保存已取得卡密后向买家发送消息的结果；此时失败不能安全重放 API 请求。
		if sendErr := e.sendText(ctx, task, result.Content); sendErr != nil {
			return actionExecutionResult{sent: sent, proof: proof}, uncertainAction(sendErr)
		}
		proof.tradeText = appendTradeText(proof.tradeText, result.Content)
		sent++
	}
	return actionExecutionResult{sent: sent, proof: proof}, nil
}

// sendDataCard 只在卡券锁内完成库存预留与恢复，把外部消息发送放到锁外。
func (e *automationActionExecutor) sendDataCard(ctx context.Context, task Task, card *db.CardFull, count int) (int, error) {
	// result 保存数据卡密发送的数量和短暂凭证；旧入口只返回数量。
	result, err := e.sendDataCardWithProof(ctx, task, card, count)
	return result.sent, err
}

// sendDataCardWithProof 发送库存卡密并收集确认发货所需的文本凭证。
func (e *automationActionExecutor) sendDataCardWithProof(ctx context.Context, task Task, card *db.CardFull, count int) (actionExecutionResult, error) {
	// sent 是已经成功发送的数据卡密数量。
	sent := 0
	// proof 保存已经成功发送的数据卡密文本。
	proof := shipmentDeliveryProof{}
	// i 表示当前数据卡密消费序号。
	for i := 0; i < count; i++ {
		// unlock 释放当前卡密组的并发消费锁；锁只覆盖本地库存操作。
		unlock := e.lockCard(card.ID)
		// content 是从库存中原子消费出的数据卡密。
		content, err := e.store.Cards.ConsumeBatchData(ctx, card.ID)
		unlock()
		if err != nil {
			return actionExecutionResult{sent: sent, proof: proof}, err
		}
		if strings.TrimSpace(content) != "" {
			// sendErr 保存数据卡密消息发送错误。
			if sendErr := e.sendText(ctx, task, renderTemplate(content, task)); sendErr != nil {
				if errors.Is(sendErr, ErrMessageNotSent) {
					// restoreUnlock 释放恢复库存所需的卡密组锁。
					restoreUnlock := e.lockCard(card.ID)
					// restoreErr 保存确定未发送时恢复库存的错误。
					restoreErr := e.store.Cards.RestoreBatchData(ctx, card.ID, content)
					restoreUnlock()
					if restoreErr != nil {
						return actionExecutionResult{sent: sent, proof: proof}, uncertainAction(errors.Join(sendErr, fmt.Errorf("恢复未发送卡密库存: %w", restoreErr)))
					}
					return actionExecutionResult{sent: sent, proof: proof}, sendErr
				}
				// 请求已交给传输层后无法判断远端是否收到，保留消费状态并人工核对。
				return actionExecutionResult{sent: sent, proof: proof}, uncertainAction(sendErr)
			}
			proof.tradeText = appendTradeText(proof.tradeText, content)
		}
		sent++
	}
	return actionExecutionResult{sent: sent, proof: proof}, nil
}
