package automation

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"xianyu-go/internal/db"
	"xianyu-go/internal/deliverytemplate"
)

// templateBindingCard 保存模板变量绑定的卡密组及本次订单需要的发货份数。
type templateBindingCard struct {
	// binding 保存模板变量与卡密组的绑定配置。
	binding db.DeliveryTemplateBinding
	// card 保存经过发货权限校验的完整卡密组。
	card *db.CardFull
	// count 保存按订单购买数量折算后的发货份数。
	count int
}

// templateCardReservation 保存当前消息已消费、但尚未确认发送成功的批量卡密。
type templateCardReservation struct {
	// cardID 保存需要恢复库存的卡密组标识。
	cardID int64
	// content 保存已经从批量库存取出的卡密正文。
	content string
}

// templateDeliveryState 保存模板动作跨消息复用的卡密绑定、正文和 API 取卡状态。
type templateDeliveryState struct {
	// cardName 保存模板动作展示给订单上下文的卡密库存名称。
	cardName string
	// bindingCards 保存按模板变量键索引的卡密绑定。
	bindingCards map[string]templateBindingCard
	// apiFetcher 保存 API 卡密变量使用的远端取卡客户端。
	apiFetcher APICardFetcher
	// values 保存已加载并可复用的模板变量正文。
	values map[string]string
	// loadedKeys 保存已经加载过的变量键。
	loadedKeys map[string]bool
	// apiFetched 表示本次动作是否已经成功从 API 卡密服务取得内容。
	apiFetched bool
}

// sendTemplate 预留模板变量对应的卡密内容，渲染每条消息后按模板顺序发送并返回确认发货凭证。
func (e *automationActionExecutor) sendTemplate(ctx context.Context, task Task, action db.AutomationAction) (actionExecutionResult, error) {
	if !actionMatchesOrderSpec(task, action) {
		return actionExecutionResult{}, nil
	}
	if len(action.TemplateMessages) == 0 {
		return actionExecutionResult{}, errors.New("发货模板缺少消息")
	}
	// state 保存模板动作在多条消息间复用的绑定和卡密正文。
	state, err := e.prepareTemplateDelivery(ctx, task, action)
	if err != nil {
		return actionExecutionResult{}, err
	}
	// result 保存已经确认投递成功的模板消息数量和发货凭证。
	result := actionExecutionResult{}
	for /* message 表示模板中按顺序发送的一条消息。 */ _, message := range action.TemplateMessages {
		// reservations 保存当前消息首次加载变量时消费的批量卡密。
		reservations, loadErr := e.loadTemplateMessageValues(ctx, task, action, &state, message)
		if loadErr != nil {
			return actionExecutionResult{}, loadErr
		}
		// text 保存订单字段、卡密变量和规则自定义变量都渲染后的最终消息。
		text := deliverytemplate.Replace(message, deliverytemplate.VariableValues{
			BuyerNickname: task.BuyerNickname,
			OrderID:       task.OrderID,
			BuyerID:       task.BuyerID,
			CardName:      state.cardName,
			CardValues:    state.values,
			CustomValues:  action.CustomVariables,
		})
		if strings.TrimSpace(text) == "" {
			// restoreErr 保存空消息导致的库存恢复错误。
			if restoreErr := e.restoreTemplateReservations(ctx, reservations); restoreErr != nil {
				return actionExecutionResult{}, uncertainAction(restoreErr)
			}
			e.clearTemplateMessageValues(&state, message)
			continue
		}
		// sendErr 保存模板消息发送错误。
		if sendErr := e.sendText(ctx, task, text); sendErr != nil {
			if result.sent == 0 && errors.Is(sendErr, ErrMessageNotSent) && !state.apiFetched {
				// restoreErr 保存确定未发送时的库存恢复错误。
				if restoreErr := e.restoreTemplateReservations(ctx, reservations); restoreErr != nil {
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
		// notSentErr 表示模板渲染后没有任何可确认发送的消息。
		notSentErr := fmt.Errorf("%w: 发货模板渲染后没有可发送内容", ErrMessageNotSent)
		return actionExecutionResult{}, notSentErr
	}
	return result, nil
}

// prepareTemplateDelivery 读取并校验模板绑定，构造后续消息渲染需要的动作状态。
func (e *automationActionExecutor) prepareTemplateDelivery(ctx context.Context, task Task, action db.AutomationAction) (templateDeliveryState, error) {
	// state 保存模板绑定、展示名称及卡密变量缓存。
	state := templateDeliveryState{
		cardName: action.CardName, bindingCards: make(map[string]templateBindingCard, len(action.TemplateBindings)),
		values: make(map[string]string, len(action.TemplateBindings)), loadedKeys: make(map[string]bool, len(action.TemplateBindings)),
	}
	for /* binding 表示当前模板变量到卡密组的绑定。 */ _, binding := range action.TemplateBindings {
		if state.cardName == "" {
			state.cardName = binding.CardName
		}
		// card 保存当前绑定的卡密组完整配置。
		card, err := e.store.Cards.GetForDelivery(ctx, binding.CardID)
		if err != nil {
			return templateDeliveryState{}, err
		}
		if !card.Enabled || (card.Type != "text" && card.Type != "data" && card.Type != "api") {
			return templateDeliveryState{}, fmt.Errorf("模板绑定的卡密组不可用")
		}
		if card.Type == "api" {
			if state.apiFetcher == nil && e.apiFetcher != nil {
				state.apiFetcher = e.apiFetcher()
			}
			if state.apiFetcher == nil {
				return templateDeliveryState{}, errors.New("API 卡发货客户端未初始化")
			}
		}
		// count 保存按订单数量折算后的变量卡密份数。
		count := binding.DeliveryCount
		if count <= 0 {
			count = 1
		}
		count *= deliveryQuantity(task)
		state.bindingCards[binding.VariableKey] = templateBindingCard{binding: binding, card: card, count: count}
	}
	return state, nil
}

// loadTemplateMessageValues 为当前模板消息加载尚未缓存的卡密变量，并保留可回滚的批量库存记录。
func (e *automationActionExecutor) loadTemplateMessageValues(ctx context.Context, task Task, action db.AutomationAction, state *templateDeliveryState, message string) ([]templateCardReservation, error) {
	// reservations 保存当前消息新消费的批量卡密，失败时只回滚本消息的库存。
	reservations := make([]templateCardReservation, 0)
	for /* key 表示当前消息首次出现的卡密变量键。 */ _, key := range deliverytemplate.CardKeys(message) {
		if state.loadedKeys[key] {
			continue
		}
		// binding、exists 保存变量绑定配置及是否存在绑定。
		binding, exists := state.bindingCards[key]
		if !exists {
			return nil, fmt.Errorf("模板变量缺少卡密绑定: %s", key)
		}
		// lines 保存当前变量将要替换到消息中的卡密正文列表。
		lines, added, apiFetched, apiFailure, dispatched, empty, loadErr := e.loadTemplateBindingLines(ctx, task, action, state, binding)
		if loadErr != nil {
			// restoreErr 保存当前变量加载失败后的库存恢复错误。
			if restoreErr := e.restoreTemplateReservations(ctx, append(reservations, added...)); restoreErr != nil {
				return nil, uncertainAction(errors.Join(loadErr, restoreErr))
			}
			if apiFailure {
				if empty || dispatched || state.apiFetched || apiFetched {
					return nil, uncertainAction(loadErr)
				}
				return nil, noRetryAction(loadErr)
			}
			return nil, loadErr
		}
		state.apiFetched = state.apiFetched || apiFetched
		state.values[key] = strings.Join(lines, "\n")
		state.loadedKeys[key] = true
		reservations = append(reservations, added...)
	}
	return reservations, nil
}

// loadTemplateBindingLines 按卡密类型加载一个模板变量的正文，并返回本次新增的批量库存预留。
func (e *automationActionExecutor) loadTemplateBindingLines(ctx context.Context, task Task, action db.AutomationAction, state *templateDeliveryState, binding templateBindingCard) ([]string, []templateCardReservation, bool, bool, bool, bool, error) {
	// lines 保存当前变量将要写入模板的正文列表。
	lines := make([]string, 0, binding.count)
	if binding.card.Type == "text" {
		for /* index 表示文本卡密重复填充的序号。 */ index := 0; index < binding.count; index++ {
			lines = append(lines, binding.card.TextContent)
		}
		return lines, nil, false, false, false, false, nil
	}
	if binding.card.Type == "data" {
		return e.loadTemplateBatchLines(ctx, binding, lines)
	}
	return e.loadTemplateAPILines(ctx, task, action, state, binding, lines)
}

// loadTemplateBatchLines 消费批量卡密并返回需要在当前消息失败时恢复的库存记录。
func (e *automationActionExecutor) loadTemplateBatchLines(ctx context.Context, binding templateBindingCard, lines []string) ([]string, []templateCardReservation, bool, bool, bool, bool, error) {
	// reservations 保存当前变量已经消费的批量卡密。
	reservations := make([]templateCardReservation, 0, binding.count)
	for /* index 表示批量卡密消费的序号。 */ index := 0; index < binding.count; index++ {
		// unlock 保存当前卡密组的并发保护释放函数。
		unlock := e.lockCard(binding.card.ID)
		// content、consumeErr 保存本次批量卡密消费结果及错误。
		content, consumeErr := e.store.Cards.ConsumeBatchData(ctx, binding.card.ID)
		unlock()
		if consumeErr != nil {
			return lines, reservations, false, false, false, false, consumeErr
		}
		lines = append(lines, content)
		reservations = append(reservations, templateCardReservation{cardID: binding.card.ID, content: content})
	}
	return lines, reservations, false, false, false, false, nil
}

// loadTemplateAPILines 逐单位请求 API 卡密，并把订单上下文传给远端取卡客户端。
func (e *automationActionExecutor) loadTemplateAPILines(ctx context.Context, task Task, action db.AutomationAction, state *templateDeliveryState, binding templateBindingCard, lines []string) ([]string, []templateCardReservation, bool, bool, bool, bool, error) {
	if state.apiFetcher == nil {
		return lines, nil, false, true, false, false, errors.New("API 卡发货客户端未初始化")
	}
	// apiFetched 表示当前变量是否已成功取得至少一个 API 卡密单位。
	apiFetched := false
	for /* unitIndex 表示当前 API 卡密变量从 1 开始的取卡单位序号。 */ unitIndex := 1; unitIndex <= binding.count; unitIndex++ {
		// fetched、fetchErr 保存当前模板变量 API 取卡结果及请求错误。
		fetched, fetchErr := state.apiFetcher.Fetch(ctx, APICardRequest{
			Config: binding.card.APIConfig, TriggerKey: buildTriggerKey(task), ActionID: action.ID, CardID: binding.card.ID,
			UnitIndex: unitIndex, TotalUnits: binding.count, AccountID: task.AccountID, OrderID: task.OrderID,
			ItemID: task.ItemID, BuyerID: task.BuyerID, ChatID: task.ChatID, SpecName: task.SpecName,
			SpecValue: task.SpecValue, Quantity: task.Quantity, Amount: task.Amount, TriggerType: task.TriggerType,
		})
		if fetchErr != nil {
			return lines, nil, apiFetched, true, fetched.Dispatched, false, fetchErr
		}
		if strings.TrimSpace(fetched.Content) == "" {
			return lines, nil, apiFetched, true, fetched.Dispatched, true, errors.New("API 卡发货响应没有可发送内容")
		}
		lines = append(lines, fetched.Content)
		apiFetched = true
	}
	return lines, nil, apiFetched, false, false, false, nil
}

// restoreTemplateReservations 在确定未发送时逆序恢复指定消息的批量卡密。
func (e *automationActionExecutor) restoreTemplateReservations(ctx context.Context, reservations []templateCardReservation) error {
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

// clearTemplateMessageValues 清理渲染为空的消息所加载的变量，使下一条消息可以重新取卡。
func (e *automationActionExecutor) clearTemplateMessageValues(state *templateDeliveryState, message string) {
	for /* key 表示需要重新尝试加载的空消息变量键。 */ _, key := range deliverytemplate.CardKeys(message) {
		delete(state.values, key)
		delete(state.loadedKeys, key)
	}
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
