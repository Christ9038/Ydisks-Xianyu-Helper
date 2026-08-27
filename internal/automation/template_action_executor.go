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
	// cardName 保存模板动作关联的卡密库存名称，多个绑定时使用第一组名称。
	cardName := action.CardName
	// bindings 保存模板变量到完整卡密配置的校验结果，避免发送过程中重复读取数据库。
	type bindingCard struct {
		binding db.DeliveryTemplateBinding
		card    *db.CardFull
		count   int
	}
	// bindingCards 保存按变量键索引的模板库存配置。
	bindingCards := make(map[string]bindingCard, len(action.TemplateBindings))
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
		bindingCards[binding.VariableKey] = bindingCard{binding: binding, card: card, count: count}
	}
	// values 保存已经按消息需要加载的卡密正文，不记录到日志或持久化任务。
	values := make(map[string]string, len(bindingCards))
	// loadedKeys 保存已经加载过的变量键，确保同一变量在多条消息中复用同一批内容。
	loadedKeys := make(map[string]bool, len(bindingCards))
	// reservation 保存单次消息刚消费但尚未确认发送成功的批量卡密。
	type reservation struct {
		cardID  int64
		content string
	}
	// restoreReservations 在确定未发送时逆序恢复指定消息的批量卡密。
	restoreReservations := func(reservations []reservation) error {
		// restoreErr 保存批量卡密恢复过程中的聚合错误。
		var restoreErr error
		for /* index 表示需要逆序恢复的预留记录位置。 */ index := len(reservations) - 1; index >= 0; index-- {
			// entry 保存当前待恢复的卡密正文。
			entry := reservations[index]
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
	// result 保存已经确认投递成功的模板消息数量和按发送顺序拼接的凭证。
	result := actionExecutionResult{}
	for /* message 表示模板中按顺序发送的一条消息。 */ _, message := range action.TemplateMessages {
		// messageReservations 保存当前消息首次使用变量时消费的批量卡密，失败时只恢复这一条消息。
		messageReservations := make([]reservation, 0)
		for /* key 表示当前消息首次出现的卡密变量键。 */ _, key := range deliverytemplate.CardKeys(message) {
			if loadedKeys[key] {
				continue
			}
			// binding、exists 保存变量绑定配置及是否存在绑定。
			binding, exists := bindingCards[key]
			if !exists {
				return actionExecutionResult{}, fmt.Errorf("模板变量缺少卡密绑定: %s", key)
			}
			// lines 保存当前变量将要替换到消息中的卡密正文列表。
			lines := make([]string, 0, binding.count)
			if binding.card.Type == "text" {
				for /* index 表示文本卡密重复填充的序号。 */ index := 0; index < binding.count; index++ {
					lines = append(lines, binding.card.TextContent)
				}
			} else {
				for /* index 表示批量卡密消费的序号。 */ index := 0; index < binding.count; index++ {
					// unlock 保存当前卡密组的并发保护释放函数。
					unlock := e.lockCard(binding.card.ID)
					// content、consumeErr 保存本次批量卡密消费结果及错误。
					content, consumeErr := e.store.Cards.ConsumeBatchData(ctx, binding.card.ID)
					unlock()
					if consumeErr != nil {
						// restoreErr 保存消费失败后的库存恢复错误。
						if restoreErr := restoreReservations(messageReservations); restoreErr != nil {
							return actionExecutionResult{}, uncertainAction(errors.Join(consumeErr, restoreErr))
						}
						return actionExecutionResult{}, consumeErr
					}
					lines = append(lines, content)
					messageReservations = append(messageReservations, reservation{cardID: binding.card.ID, content: content})
				}
			}
			values[key] = strings.Join(lines, "\n")
			loadedKeys[key] = true
		}
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
			// restoreErr 保存空消息导致的库存恢复错误。
			if restoreErr := restoreReservations(messageReservations); restoreErr != nil {
				return actionExecutionResult{}, uncertainAction(restoreErr)
			}
			for /* key 表示需要重新尝试加载的空消息变量键。 */ _, key := range deliverytemplate.CardKeys(message) {
				delete(values, key)
				delete(loadedKeys, key)
			}
			continue
		}
		// sendErr 保存模板消息发送错误。
		if sendErr := e.sendText(ctx, task, text); sendErr != nil {
			if result.sent == 0 && errors.Is(sendErr, ErrMessageNotSent) {
				// restoreErr 保存确定未发送时的库存恢复错误。
				if restoreErr := restoreReservations(messageReservations); restoreErr != nil {
					return actionExecutionResult{}, uncertainAction(errors.Join(sendErr, restoreErr))
				}
				return actionExecutionResult{}, sendErr
			}
			result.reviewProof.tradeText = appendTradeText(result.reviewProof.tradeText, text)
			return result, uncertainAction(sendErr)
		}
		result.sent++
		result.proof.tradeText = appendTradeText(result.proof.tradeText, text)
	}
	if result.sent == 0 {
		// notSentErr 表示模板渲染后没有任何可确认发送的消息，必须阻止后续确认发货动作。
		notSentErr := fmt.Errorf("%w: 发货模板渲染后没有可发送内容", ErrMessageNotSent)
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
