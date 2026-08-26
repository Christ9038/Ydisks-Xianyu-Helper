package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
	"xianyu-go/internal/deliverytemplate"
)

// sendTemplate 预留模板变量对应的卡密内容，渲染每条消息后按模板顺序发送并返回确认发货凭证。
func (e *automationActionExecutor) sendTemplate(ctx context.Context, task Task, action db.AutomationAction) (actionExecutionResult, error) {
	if !actionMatchesOrderSpec(task, action) {
		return actionExecutionResult{}, nil
	}
	if len(action.TemplateMessages) == 0 {
		return actionExecutionResult{}, errors.New("发货模板缺少消息")
	}
	// values 保存变量键对应的卡密正文，不记录到日志或持久化任务。
	values := make(map[string]string, len(action.TemplateBindings))
	// cardName 保存模板动作关联的卡密库存名称，多个绑定时使用第一组名称。
	cardName := action.CardName
	// reserved 保存已经从批量库存消费、但尚未确认发送成功的正文。
	reserved := make([]struct {
		cardID  int64
		content string
	}, 0)
	// restoreReserved 在确定未发送时恢复本次动作预留的批量卡密。
	restoreReserved := func() error {
		// restoreErr 保存批量卡密恢复过程中的聚合错误。
		var restoreErr error
		for /* index 表示需要逆序恢复的预留记录位置。 */ index := len(reserved) - 1; index >= 0; index-- {
			// entry 保存当前待恢复的卡密正文。
			entry := reserved[index]
			// unlock 保存当前卡密组的并发保护释放函数。
			unlock := e.lockCard(entry.cardID)
			// err 保存当前卡密正文恢复错误。
			err := e.store.Cards.RestoreBatchData(ctx, entry.cardID, entry.content)
			unlock()
			if err != nil {
				restoreErr = errors.Join(restoreErr, err)
			}
		}
		return restoreErr
	}
	for /* binding 表示当前模板变量到卡密组的绑定。 */ _, binding := range action.TemplateBindings {
		if cardName == "" {
			cardName = binding.CardName
		}
		// card 保存当前绑定的卡密组完整配置。
		card, err := e.store.Cards.GetForDelivery(ctx, binding.CardID)
		if err != nil {
			return actionExecutionResult{}, err
		}
		if !card.Enabled || (card.Type != "text" && card.Type != "data") {
			return actionExecutionResult{}, fmt.Errorf("模板绑定的卡密组不可用")
		}
		// count 保存按订单购买数量折算后的当前变量卡密份数。
		count := binding.DeliveryCount
		if count <= 0 {
			count = 1
		}
		count *= deliveryQuantity(task)
		// lines 保存当前变量将要替换到消息中的卡密正文列表。
		lines := make([]string, 0, count)
		if card.Type == "text" {
			for /* index 表示文本卡密重复填充的序号。 */ index := 0; index < count; index++ {
				lines = append(lines, card.TextContent)
			}
		} else {
			for /* index 表示批量卡密消费的序号。 */ index := 0; index < count; index++ {
				// unlock 保存当前卡密组的并发保护释放函数。
				unlock := e.lockCard(card.ID)
				// content、consumeErr 保存本次批量卡密消费结果及错误。
				content, consumeErr := e.store.Cards.ConsumeBatchData(ctx, card.ID)
				unlock()
				if consumeErr != nil {
					// restoreErr 保存消费失败后的回滚错误。
					if restoreErr := restoreReserved(); restoreErr != nil {
						return actionExecutionResult{}, uncertainAction(errors.Join(consumeErr, restoreErr))
					}
					return actionExecutionResult{}, consumeErr
				}
				lines = append(lines, content)
				reserved = append(reserved, struct {
					cardID  int64
					content string
				}{cardID: card.ID, content: content})
			}
		}
		values[binding.VariableKey] = strings.Join(lines, "\n")
	}
	// result 保存已经确认投递成功的模板消息数量和按发送顺序拼接的凭证。
	result := actionExecutionResult{}
	for /* message 表示模板中按顺序发送的一条消息。 */ _, message := range action.TemplateMessages {
		// text 保存订单字段、卡密变量和规则自定义变量都渲染后的最终消息。
		text := deliverytemplate.Replace(message, deliverytemplate.VariableValues{
			BuyerNickname: task.BuyerNickname,
			OrderID:       task.OrderID,
			BuyerID:       task.BuyerID,
			CardName:      cardName,
			CardValues:    values,
			CustomValues:  action.CustomVariables,
		})
		if strings.TrimSpace(text) == "" {
			continue
		}
		// sendErr 保存模板消息发送错误。
		if sendErr := e.sendText(ctx, task, text); sendErr != nil {
			if result.sent == 0 && errors.Is(sendErr, ErrMessageNotSent) {
				// restoreErr 保存首条消息未发送时的库存恢复错误。
				if restoreErr := restoreReserved(); restoreErr != nil {
					return actionExecutionResult{}, uncertainAction(errors.Join(sendErr, restoreErr))
				}
				return actionExecutionResult{}, sendErr
			}
			return result, uncertainAction(sendErr)
		}
		result.sent++
		result.proof.tradeText = appendTradeText(result.proof.tradeText, text)
	}
	if result.sent == 0 {
		// notSentErr 表示模板渲染后没有任何可确认发送的消息，必须阻止后续确认发货动作。
		notSentErr := fmt.Errorf("%w: 发货模板渲染后没有可发送内容", ErrMessageNotSent)
		// restoreErr 保存零消息场景下回滚已预留批量卡密时产生的错误。
		if restoreErr := restoreReserved(); restoreErr != nil {
			return actionExecutionResult{}, uncertainAction(errors.Join(notSentErr, restoreErr))
		}
		return actionExecutionResult{}, notSentErr
	}
	return result, nil
}

// deliveryQuantity 把订单数量转换为至少一份的发货倍数。
func deliveryQuantity(task Task) int {
	// quantity 保存订单数量解析结果。
	quantity := parsePositiveInt(task.Quantity)
	if quantity <= 0 {
		return 1
	}
	return quantity
}
