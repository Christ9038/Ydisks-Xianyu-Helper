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
	proof := shipmentDeliveryProof{expectedUnits: count}
	// i 表示当前卡密发送序号。
	for i := 0; i < count; i++ {
		// executionErr 阻止失权运行继续取下一份卡密，已发送的 proof 必须随错误返回。
		if executionErr := checkRunExecution(ctx); executionErr != nil {
			return actionExecutionResult{sent: sent, proof: proof}, executionErr
		}

		if card.Type == "image" && card.ImagePath != "" {
			// imageResult、imageErr 保存本地图片上传后的平台快照和发送结果，失败不回退 URL。
			imageResult, imageErr := e.deliverImage(ctx, task, card.ImageURL, card.ImagePath, card.ID)
			proof = mergeShipmentDeliveryProof(proof, imageResult.proof)
			sent += imageResult.sent
			if imageErr != nil {
				return actionExecutionResult{sent: sent, proof: proof, reviewProof: imageResult.reviewProof}, imageErr
			}
			continue
		}
		// content、imageURL、readErr 分别是当前卡密组可发送的文本、图片地址和读取配置失败原因。
		content, imageURL, readErr := e.cardContent(ctx, card)
		if readErr != nil {
			return actionExecutionResult{sent: sent, proof: proof}, readErr
		}
		if imageURL != "" {
			// sendErr 保存图片消息发送错误。
			if sendErr := e.sendImage(ctx, task, imageURL, card.ID); sendErr != nil {
				// reviewProof 保存图片传输结果未知时的原始地址；不重新读取卡密组，避免人工补发改变内容。
				reviewProof := shipmentDeliveryProof{unknownUnits: 1, picList: []string{imageURL}, messages: []db.AutomationDeliveryMessage{{Kind: "image", Content: imageURL}}}
				if errors.Is(sendErr, ErrMessageNotSent) {
					return actionExecutionResult{sent: sent, proof: proof}, classifyMessageSendError(sendErr)
				}
				return actionExecutionResult{sent: sent, proof: proof, reviewProof: reviewProof}, classifyMessageSendError(sendErr)
			}
			proof.picList = append(proof.picList, imageURL)
			proof.messages = append(proof.messages, db.AutomationDeliveryMessage{Kind: "image", Content: imageURL})
		}
		if strings.TrimSpace(content) != "" {
			// renderedContent 是实际发送给买家的文本，也作为确认发货凭证提交。
			renderedContent := renderTemplate(content, task)
			// sendErr 保存文字消息发送错误。
			if sendErr := e.sendText(ctx, task, renderedContent); sendErr != nil {
				// reviewProof 保存结果未知的最终渲染文本；卡密补发必须使用该文本而非再次模板渲染。
				reviewProof := shipmentDeliveryProof{unknownUnits: 1, tradeText: renderedContent, messages: []db.AutomationDeliveryMessage{{Kind: "text", Content: renderedContent}}}
				if errors.Is(sendErr, ErrMessageNotSent) {
					return actionExecutionResult{sent: sent, proof: proof}, classifyMessageSendError(sendErr)
				}
				return actionExecutionResult{sent: sent, proof: proof, reviewProof: reviewProof}, classifyMessageSendError(sendErr)
			}
			proof.tradeText = appendTradeText(proof.tradeText, renderedContent)
			proof.messages = append(proof.messages, db.AutomationDeliveryMessage{Kind: "text", Content: renderedContent})
		}
		if strings.TrimSpace(content) == "" && strings.TrimSpace(imageURL) == "" {
			return actionExecutionResult{sent: sent, proof: proof}, fmt.Errorf("卡密组 %d 没有可发送内容", card.ID)
		}
		sent++
		proof.preparedUnits++
	}
	return actionExecutionResult{sent: sent, proof: proof}, nil
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
	proof := shipmentDeliveryProof{expectedUnits: count}
	// unitIndex 表示从 1 开始的当前 API 发货单位序号。
	for unitIndex := 1; unitIndex <= count; unitIndex++ {
		// executionErr 阻止失权运行继续取下一份卡密，已发送的 proof 必须随错误返回。
		if executionErr := checkRunExecution(ctx); executionErr != nil {
			return actionExecutionResult{sent: sent, proof: proof}, executionErr
		}

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
			// reviewProof 保存 API 已返回的唯一内容；即使发送结果未知也只能重发该快照，不能再次调用 API。
			reviewProof := shipmentDeliveryProof{unknownUnits: 1, tradeText: result.Content, messages: []db.AutomationDeliveryMessage{{Kind: "text", Content: result.Content}}}
			return actionExecutionResult{sent: sent, proof: proof, reviewProof: reviewProof}, uncertainAction(sendErr)
		}
		proof.tradeText = appendTradeText(proof.tradeText, result.Content)
		proof.messages = append(proof.messages, db.AutomationDeliveryMessage{Kind: "text", Content: result.Content})
		sent++
		proof.preparedUnits++
	}
	return actionExecutionResult{sent: sent, proof: proof}, nil
}

// sendDataCardWithProof 发送库存卡密并收集确认发货所需的文本凭证。
func (e *automationActionExecutor) sendDataCardWithProof(ctx context.Context, task Task, card *db.CardFull, count int) (actionExecutionResult, error) {
	// sent 是已经成功发送的数据卡密数量。
	sent := 0
	// proof 保存已经成功发送的数据卡密文本。
	proof := shipmentDeliveryProof{expectedUnits: count}
	// i 表示当前数据卡密消费序号。
	for i := 0; i < count; i++ {
		// executionErr 阻止失权运行继续取下一份卡密，已发送的 proof 必须随错误返回。
		if executionErr := checkRunExecution(ctx); executionErr != nil {
			return actionExecutionResult{sent: sent, proof: proof}, executionErr
		}

		// unlock 释放当前卡密组的并发消费锁；锁只覆盖本地库存操作。
		unlock := e.lockCard(card.ID)
		// content 是从库存中原子消费出的数据卡密。
		content, err := e.store.Cards.ConsumeBatchData(ctx, card.ID)
		unlock()
		if err != nil {
			return actionExecutionResult{sent: sent, proof: proof}, err
		}
		if strings.TrimSpace(content) != "" {
			// renderedContent 保存实际发送给买家的最终卡密文本，确认发货必须复用同一份内容。
			renderedContent := renderTemplate(content, task)
			// sendErr 保存数据卡密消息发送错误。
			if sendErr := e.sendText(ctx, task, renderedContent); sendErr != nil {
				if errors.Is(sendErr, ErrMessageNotSent) {
					// restoreUnlock 释放恢复库存所需的卡密组锁。
					restoreUnlock := e.lockCard(card.ID)
					// restoreErr 保存确定未发送时恢复库存的错误。
					restoreErr := e.store.Cards.RestoreBatchData(ctx, card.ID, content)
					restoreUnlock()
					if restoreErr != nil {
						// proof.refillPending 锁定本次已消费但恢复失败的库存，防止人工补发再次取卡造成重复扣减。
						proof.refillPending = true
						return actionExecutionResult{sent: sent, proof: proof}, uncertainAction(errors.Join(sendErr, fmt.Errorf("恢复未发送卡密库存: %w", restoreErr)))
					}
					return actionExecutionResult{sent: sent, proof: proof}, sendErr
				}
				// 请求已交给传输层后无法判断远端是否收到，保留消费状态并人工核对。
				// reviewProof 保存已消费但发送结果未知的数据卡密，补发必须直接使用该内容而不能再次扣库存。
				reviewProof := shipmentDeliveryProof{unknownUnits: 1, tradeText: renderedContent, messages: []db.AutomationDeliveryMessage{{Kind: "text", Content: renderedContent}}}
				return actionExecutionResult{sent: sent, proof: proof, reviewProof: reviewProof}, uncertainAction(sendErr)
			}
			proof.tradeText = appendTradeText(proof.tradeText, renderedContent)
			proof.messages = append(proof.messages, db.AutomationDeliveryMessage{Kind: "text", Content: renderedContent})
			proof.preparedUnits++
		}
		sent++
	}
	return actionExecutionResult{sent: sent, proof: proof}, nil
}
