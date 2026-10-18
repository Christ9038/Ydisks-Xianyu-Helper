package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// ManualFullDelivery 对已存在订单执行完整发货，复用付款后发货规则但隔离人工与自动运行的幂等键。
func (c *Center) ManualFullDelivery(ctx context.Context, order *db.Order) (sent int, deliveryErr error) {
	defer func() {
		if c == nil || c.logger == nil || order == nil {
			return
		}
		if deliveryErr == nil {
			c.logger.Info("手动完整发货成功", "account", order.CookieID, "order_id", order.OrderID, "sent_count", sent)
			return
		}
		c.logger.Warn("手动完整发货失败", "account", order.CookieID, "order_id", order.OrderID, "sent_count", sent, "err", deliveryErr)
	}()
	// task、taskErr 保存校验、补全后的人工订单发货任务及其失败原因。
	task, taskErr := c.prepareManualDeliveryTask(ctx, order)
	if taskErr != nil {
		return 0, taskErr
	}
	// manualTriggerKey 与自动付款事件使用不同的幂等键，避免闲鱼官方已确认的空自动运行阻断补发。
	manualTriggerKey := buildManualDeliveryTriggerKey(task)
	// handled、sent、priorErr 分别表示历史运行是否已处理请求、已补发数量及处理失败原因。
	handled, sent, priorErr := c.handlePriorManualDelivery(ctx, task, manualTriggerKey)
	if priorErr != nil || handled {
		return sent, priorErr
	}
	return c.executeManualDeliveryRules(ctx, task, manualTriggerKey)
}

// prepareManualDeliveryTask 校验人工发货输入、账号状态并补齐订单详情，返回可执行的付款发货任务。
func (c *Center) prepareManualDeliveryTask(ctx context.Context, order *db.Order) (Task, error) {
	if c == nil || c.store == nil || order == nil {
		return Task{}, fmt.Errorf("自动化中心未初始化或订单为空")
	}
	if strings.TrimSpace(order.OrderID) == "" {
		return Task{}, fmt.Errorf("订单缺少订单ID")
	}
	// paused、until、pauseErr 保存账号暂停状态、恢复时间和查询错误。
	paused, until, pauseErr := c.store.Cookies.IsPaused(ctx, order.CookieID)
	if pauseErr != nil {
		return Task{}, fmt.Errorf("读取账号暂停状态: %w", pauseErr)
	}
	if paused {
		return Task{}, fmt.Errorf("账号暂停处理中，恢复时间 %d", until)
	}
	// enabled、statusErr 保存账号启用状态和查询错误，停用账号不得消耗卡密。
	enabled, statusErr := c.store.Cookies.Status(ctx, order.CookieID)
	if statusErr != nil {
		return Task{}, fmt.Errorf("读取账号启用状态: %w", statusErr)
	}
	if !enabled {
		return Task{}, fmt.Errorf("账号已停用，无法执行完整发货")
	}
	if strings.TrimSpace(order.CookieID) == "" {
		return Task{}, fmt.Errorf("订单缺少账号ID")
	}
	if strings.TrimSpace(order.ItemID) == "" {
		return Task{}, fmt.Errorf("订单缺少商品ID，无法匹配自动化规则")
	}
	if strings.TrimSpace(order.ChatID) == "" || strings.TrimSpace(order.BuyerID) == "" {
		return Task{}, fmt.Errorf("订单缺少 chat_id 或 buyer_id，无法发送卡券")
	}
	// task 保存由订单事实构成、强制确认闲鱼发货状态的人工付款发货任务。
	task := Task{
		Source:               "manual",
		AccountID:            order.CookieID,
		OrderRole:            OrderRoleSeller,
		TriggerType:          TriggerOrderPaid,
		ChatID:               order.ChatID,
		OrderID:              order.OrderID,
		ItemID:               order.ItemID,
		BuyerID:              order.BuyerID,
		SpecName:             order.SpecName,
		SpecValue:            order.SpecValue,
		Quantity:             order.Quantity,
		Amount:               order.Amount,
		OrderStatus:          order.OrderStatus,
		IsBargain:            order.IsBargain != 0,
		ForceConfirmShipment: true,
		Raw:                  map[string]any{"manual": true},
	}
	// preparedTask、prepareErr 保存订单详情补全后的任务和失败原因。
	preparedTask, prepareErr := c.prepareTask(ctx, task)
	if prepareErr != nil {
		return Task{}, prepareErr
	}
	return preparedTask, nil
}

// handlePriorManualDelivery 根据同订单最近运行决定继续全新发货、原样补发或安全拒绝。
// handled 为 true 时 sent 和 err 已是本次人工请求的最终结果。
func (c *Center) handlePriorManualDelivery(ctx context.Context, task Task, manualTriggerKey string) (handled bool, sent int, err error) {
	// priorRun、priorErr 保存同订单既有自动或人工付款发货运行及读取失败原因。
	priorRun, priorErr := c.store.Automation.GetLatestOrderDeliveryRun(ctx, task.AccountID, task.OrderID)
	if priorErr != nil {
		if errors.Is(priorErr, db.ErrNotFound) {
			return false, 0, nil
		}
		return true, 0, priorErr
	}
	if priorRun.TriggerKey == manualTriggerKey {
		return c.handleExistingManualDelivery(ctx, task, priorRun)
	}
	if priorRun.Status == "running" {
		return true, 0, fmt.Errorf("该订单的自动发货仍在执行，请等待其结束后再人工补发")
	}
	if strings.HasPrefix(priorRun.ErrorMessage, db.NoRetryErrorPrefix) {
		return true, 0, fmt.Errorf("该订单此前向卡密接口请求的结果不确定，已停止自动补发以避免重复扣费，请先在自动化异常中核对")
	}
	if priorRun.DeliveryProof.RefillPending {
		return true, 0, fmt.Errorf("上次补取卡密结果未可靠保存，已禁止再次取卡，请先人工核对")
	}
	if deliveryProofPresent(priorRun.DeliveryProof) {
		if priorRun.Status == "failed" || priorRun.Status == "needs_review" {
			// sent、replayErr 保存已原子领取快照的补发数量和补发失败原因，失败后运行仍保留原快照。
			sent, replayErr := c.claimAndReplayDeliveryProof(ctx, task, priorRun)
			return true, sent, replayErr
		}
		if priorRun.Status == "success" {
			return true, 0, fmt.Errorf("该订单已存在成功的发货内容快照；如闲鱼状态未同步，请选择仅修改闲鱼发货状态")
		}
	}
	if priorRun.Status == "success" {
		// v1.0.10 会在仅完成 WebSocket 写入、尚未验证自身回显时记为成功，并在确认发货后清空快照。
		// 此类无快照历史记录不能证明买家内容已送达，允许独立的人工运行重新发货；新版本成功运行均保留快照。
		return false, 0, nil
	}
	if priorRun.Status == "needs_review" || priorRun.SentCount > 0 {
		return true, 0, fmt.Errorf("该订单此前发货结果不确定且没有可安全重发的内容快照，请先在自动化异常中核对")
	}
	return false, 0, nil
}

// handleExistingManualDelivery 处理同一人工幂等键的运行，禁止已完成或运行中的请求再次领卡。
func (c *Center) handleExistingManualDelivery(ctx context.Context, task Task, run *db.AutomationRun) (handled bool, sent int, err error) {
	if run.Status == "running" {
		return true, 0, fmt.Errorf("该订单的人工完整发货正在执行，请勿重复提交")
	}
	if run.Status == "failed" || run.Status == "needs_review" {
		if run.DeliveryProof.RefillPending {
			return true, 0, fmt.Errorf("上次补取卡密结果未可靠保存，已禁止再次取卡，请先人工核对")
		}
		if deliveryProofPresent(run.DeliveryProof) {
			// sent、replayErr 保存已原子领取的人工运行快照补发数量和失败原因。
			sent, replayErr := c.claimAndReplayDeliveryProof(ctx, task, run)
			return true, sent, replayErr
		}
		if run.Status == "failed" && run.SentCount == 0 {
			return true, 0, fmt.Errorf("上次人工发货尚未发送任何内容，正在等待安全重试；请稍后重新发起完整发货")
		}
		return true, 0, fmt.Errorf("上次人工发货结果不确定且没有可安全重发的内容快照，请先在自动化异常中核对")
	}
	return true, 0, fmt.Errorf("该订单已完成完整发货；如仅需补记闲鱼状态，请选择仅修改闲鱼发货状态")
}

// executeManualDeliveryRules 匹配订单付款规则并执行第一个可发卡规则，未命中时返回明确错误。
func (c *Center) executeManualDeliveryRules(ctx context.Context, task Task, manualTriggerKey string) (int, error) {
	// rules、matchErr 保存与订单匹配的付款发货规则和查询错误。
	rules, matchErr := c.rules.match(ctx, task)
	if matchErr != nil {
		return 0, matchErr
	}
	if len(rules) == 0 {
		return 0, fmt.Errorf("未匹配到付款后自动发货规则")
	}
	// rule 保存当前候选付款发货规则。
	for _, rule := range rules {
		// candidateTask 固化当前规则的全商品授权，供规格过滤和人工运行快照共同使用。
		candidateTask := task
		candidateTask.AllowAllItems = ruleAllowsAllItems(rule, candidateTask.TriggerType)
		if !c.planner.hasMatchingSendCard(candidateTask, rule.Actions) {
			continue
		}
		// sent、executeErr 保存当前规则的发货数量和执行失败原因，首个有效规则即结束人工流程。
		sent, executeErr := c.executeManualDeliveryRule(ctx, candidateTask, rule, manualTriggerKey)
		if executeErr != nil || sent > 0 {
			return sent, executeErr
		}
	}
	return 0, fmt.Errorf("未匹配到订单规格对应的卡密动作")
}

// executeManualDeliveryRule 为一个已匹配的规则创建人工运行、执行即时动作并原子收口运行状态。
func (c *Center) executeManualDeliveryRule(ctx context.Context, task Task, rule db.AutomationRule, manualTriggerKey string) (int, error) {
	// plannedTask 保存仅含人工即时动作的任务副本，延迟动作不得进入人工发货流程。
	plannedTask := task
	// AllowAllItems 固化人工运行对应规则的全商品授权，避免直接调用该函数时遗漏范围门禁。
	plannedTask.AllowAllItems = ruleAllowsAllItems(rule, plannedTask.TriggerType)
	plannedTask.ActionPlan = c.planner.plan(plannedTask, c.planner.immediateManualActions(rule.Actions))
	// rawTask、rawJSON、marshalErr 分别保存脱敏任务快照、其 JSON 与序列化失败原因。
	rawTask := plannedTask
	rawTask.CookieStr = ""
	// rawJSON、marshalErr 保存脱敏任务序列化结果及失败原因，失败时不能创建可执行运行。
	rawJSON, marshalErr := json.Marshal(rawTask)
	if marshalErr != nil {
		return 0, fmt.Errorf("保存完整发货运行快照: %w", marshalErr)
	}
	// runID、started、startErr 保存创建或占用人工运行的结果。
	runID, started, startErr := c.store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: rule.ID, CookieID: plannedTask.AccountID, ItemID: plannedTask.ItemID, OrderID: plannedTask.OrderID,
		BuyerID: plannedTask.BuyerID, ChatID: plannedTask.ChatID, TriggerType: TriggerOrderPaid, TriggerKey: manualTriggerKey,
		RawEventJSON: string(rawJSON), LeaseExpiresAt: time.Now().UTC().Add(5 * time.Minute).Unix(),
	})
	if startErr != nil {
		return 0, startErr
	}
	if !started {
		// existingRun、existingErr 保存本规则的人工幂等运行及其读取失败原因。
		existingRun, existingErr := c.store.Automation.GetRunByRuleAndTrigger(ctx, rule.ID, manualTriggerKey)
		if errors.Is(existingErr, db.ErrNotFound) {
			return 0, fmt.Errorf("该订单已有另一条付款发货运行，已拒绝重复发货，请先在自动化异常中核对")
		}
		if existingErr != nil {
			return 0, existingErr
		}
		// sent、handlingErr 保存已有人工运行的快照补发数量和处理失败原因。
		_, sent, handlingErr := c.handleExistingManualDelivery(ctx, plannedTask, existingRun)
		return sent, handlingErr
	}
	// run、runErr 保存刚创建运行的可比较代次及读取失败原因。
	run, runErr := c.store.Automation.GetRun(ctx, runID)
	if runErr != nil {
		return 0, runErr
	}
	// sent、deferred、executeErr 保存运行执行的发货数量、延迟标志和动作失败原因。
	sent, deferred, executeErr := c.executeRunActions(ctx, plannedTask, rule.ID, run, plannedTask.ActionPlan, true)
	if deferred {
		return sent, errors.New("手动完整发货不应进入延迟队列")
	}
	if errors.Is(executeErr, errAutomationNeedsReview) {
		return sent, executeErr
	}
	return sent, c.finishManualDeliveryRun(runID, run.AttemptCount, sent, executeErr)
}

// finishManualDeliveryRun 将人工运行收口为成功或失败；状态写入失败时隔离运行，避免重放外部动作。
func (c *Center) finishManualDeliveryRun(runID int64, attempt int, sent int, executeErr error) error {
	// status、errMsg 保存需要落库的终态及对用户可见的失败原因。
	status, errMsg := "success", ""
	if executeErr != nil {
		status, errMsg = "failed", executeErr.Error()
	}
	// finishCtx、cancel 为结果收口分配独立短时上下文，调用者请求取消不能中断安全收尾。
	finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	// finishErr 保存写入运行终态失败原因。
	finishErr := c.store.Automation.FinishRun(finishCtx, runID, attempt, status, sent, errMsg)
	if finishErr == nil {
		return executeErr
	}
	// reason 说明外部动作已经可能执行，但运行结果未能收口，必须禁止自动重放并转人工核对。
	reason := "完整发货外部动作可能已执行，但运行结果保存失败，已停止自动重放，请人工核对: " + finishErr.Error()
	// quarantineErr 保存把手动运行转为人工核对状态时的失败原因。
	quarantineErr := c.store.Automation.QuarantineRunResult(finishCtx, runID, attempt, sent, reason)
	if quarantineErr != nil {
		return errors.Join(errAutomationNeedsReview, fmt.Errorf("保存完整发货执行结果: %w", finishErr), fmt.Errorf("保存完整发货人工核对状态: %w", quarantineErr))
	}
	return errors.Join(errAutomationNeedsReview, fmt.Errorf("保存完整发货执行结果: %w", finishErr))
}

// buildManualDeliveryTriggerKey 为人工完整发货生成独立且稳定的幂等键。
func buildManualDeliveryTriggerKey(task Task) string {
	if task.OrderID == "" {
		return ""
	}
	return "manual_delivery:" + task.OrderID
}

// deliveryProofPresent 判断加密快照解密后是否至少包含一条可重发内容，兼容旧版文本和图片字段。
func deliveryProofPresent(proof db.AutomationDeliveryProof) bool {
	return len(proof.Messages) > 0 || strings.TrimSpace(proof.TradeText) != "" || len(proof.PicList) > 0
}

// claimAndReplayDeliveryProof 先以新代次原子领取快照，再发送内容，确保同一订单的并发人工请求只会有一个发送者。
func (c *Center) claimAndReplayDeliveryProof(ctx context.Context, task Task, run *db.AutomationRun) (int, error) {
	if run == nil {
		return 0, fmt.Errorf("订单发货运行不存在")
	}
	if run.DeliveryProof.RefillPending {
		return 0, fmt.Errorf("上次补取卡密结果未可靠保存，已禁止再次取卡，请先人工核对")
	}
	if !deliveryProofPresent(run.DeliveryProof) {
		return 0, fmt.Errorf("订单没有可重发的发货内容快照")
	}
	// planErr 在领取和发送前阻止遗漏后续动作、损坏快照或未收口补取，避免重复发送已知内容。
	if _, planErr := deliveryReplayPlan(run); planErr != nil {
		return 0, planErr
	}
	// leaseExpiresAt 保存本次人工补发的短期数据库占用截止时间，发送外部消息前必须先完成领取。
	leaseExpiresAt := time.Now().UTC().Add(5 * time.Minute).Unix()
	// replayAttempt、claimed、claimErr 分别保存新执行代次、是否取得唯一补发权及条件更新错误。
	replayAttempt, claimed, claimErr := c.store.Automation.ClaimDeliveryReplay(ctx, run.ID, run.AttemptCount, leaseExpiresAt)
	if claimErr != nil {
		return 0, fmt.Errorf("领取订单内容补发: %w", claimErr)
	}
	if !claimed {
		return 0, fmt.Errorf("该订单的发货内容正在补发，请勿重复提交")
	}
	// run 更新为本请求拥有的执行代次，后续收口或释放只能作用于该代次。
	run.Status = "running"
	run.AttemptCount = replayAttempt
	run.LeaseExpiresAt = leaseExpiresAt
	run.ActionStarted = true
	return c.replayDeliveryProof(ctx, task, run)
}

// replayDeliveryProof 把已领取运行的订单内容按原始顺序重新发送，并再次确认闲鱼发货状态。
// 发送、确认或收口失败时始终进入人工核对，确保失败请求不会永久占用运行或触发整批重试。
func (c *Center) replayDeliveryProof(ctx context.Context, task Task, run *db.AutomationRun) (sent int, replayErr error) {
	if run == nil || !deliveryProofPresent(run.DeliveryProof) {
		return 0, fmt.Errorf("订单没有可重发的发货内容快照")
	}
	// defer 在本次补发未收口时释放唯一发送权；使用脱离取消的短上下文，避免原请求超时后把运行永久留在执行中。
	defer func() {
		if replayErr == nil {
			return
		}
		// releaseCtx、releaseCancel 为状态释放提供有界且不继承父请求取消的补偿上下文。
		releaseCtx, releaseCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer releaseCancel()
		// replayReason 保存人工补发失败的未知结果提示，确保恢复策略不会降级为普通重试。
		replayReason := "人工补发失败，外部结果可能未知: " + replayErr.Error()
		// releaseErr 保存释放当前补发代次的条件更新错误。
		releaseErr := c.store.Automation.ReleaseDeliveryReplay(releaseCtx, run.ID, run.AttemptCount, "needs_review", replayReason)
		if releaseErr != nil {
			replayErr = errors.Join(replayErr, fmt.Errorf("恢复订单内容补发状态: %w", releaseErr))
		}
	}()
	// executionCtx、stopLease、leaseErr 维护整个补发批次的执行权，失权立即取消后续消息和取卡。
	executionCtx, stopLease, leaseErr := startRunExecutionLease(ctx, c.store.Automation, run.ID, run.AttemptCount, runExecutionLeaseInterval)
	if leaseErr != nil {
		return 0, leaseErr
	}
	defer stopLease()
	ctx = executionCtx

	// allowed、allowedErr 保存账号自动化门禁结果，账号暂停或停用时不能发送补发消息。
	allowed, allowedErr := c.accountAutomationAllowed(ctx, task.AccountID)
	if allowedErr != nil {
		return 0, allowedErr
	}
	if !allowed {
		return 0, fmt.Errorf("账号已暂停或停用，无法补发订单内容")
	}
	// messages 保存按原顺序恢复的消息，旧快照没有 Messages 时兼容图片后文本的历史顺序。
	messages := append([]db.AutomationDeliveryMessage(nil), run.DeliveryProof.Messages...)
	if run.DeliveryProof.ExpectedUnits <= 0 {
		return 0, fmt.Errorf("订单发货快照缺少逐单位记录，已保留人工核对，不能猜测剩余卡密数量")
	}
	// knownUnits 表示已有内容覆盖的确定、未知和合法跳过单位总数。
	knownUnits := run.DeliveryProof.PreparedUnits + run.DeliveryProof.UnknownUnits + len(run.DeliveryProof.SkippedTemplateMessages)
	if knownUnits > run.DeliveryProof.ExpectedUnits {
		return 0, fmt.Errorf("订单发货快照单位数量无效: %d/%d", knownUnits, run.DeliveryProof.ExpectedUnits)
	}
	if len(messages) == 0 {
		// imageURL 保存旧版快照的一张图片地址；旧版没有跨类型消息顺序，因此仅按历史图片列表恢复。
		for _, imageURL := range run.DeliveryProof.PicList {
			messages = append(messages, db.AutomationDeliveryMessage{Kind: "image", Content: imageURL})
		}
		if strings.TrimSpace(run.DeliveryProof.TradeText) != "" {
			messages = append(messages, db.AutomationDeliveryMessage{Kind: "text", Content: run.DeliveryProof.TradeText})
		}
	}
	// messageIndex、message 分别表示当前重发消息的顺序和内容，失败时保留同一快照供下次重发。
	for messageIndex, message := range messages {
		if strings.TrimSpace(message.Content) == "" {
			return messageIndex, fmt.Errorf("订单发货快照第 %d 条内容为空", messageIndex+1)
		}
		// sendErr 保存当前原样重发消息的发送结果，失败时不得重新领取卡密。
		var sendErr error
		switch message.Kind {
		case "text":
			sendErr = c.actions.sendText(ctx, task, message.Content)
		case "image", "uploaded_image":
			sendErr = c.actions.sendDeliveryImage(ctx, task, message, 0)
		default:
			return messageIndex, fmt.Errorf("订单发货快照第 %d 条消息类型无效", messageIndex+1)
		}
		if sendErr != nil {
			return messageIndex, fmt.Errorf("原样补发订单内容失败: %w", sendErr)
		}
	}
	// refillCount、refillErr 保存只针对缺失卡密单位执行的补发数量和错误；已保存内容不会再次读取库存。
	refillCount, refillErr := c.refillPartialDeliveryProof(ctx, task, run)
	if refillErr != nil {
		return len(messages) + refillCount, refillErr
	}
	if run.DeliveryProof.RefillPending || run.DeliveryProof.PreparedUnits+run.DeliveryProof.UnknownUnits+len(run.DeliveryProof.SkippedTemplateMessages) != run.DeliveryProof.ExpectedUnits {
		return len(messages) + refillCount, fmt.Errorf("补发后仍有缺失内容，不能确认整单发货，请先人工核对")
	}
	// proof 保存确认闲鱼发货接口所需历史文本和图片凭证，不由重发次数重新拼装。
	proof := shipmentDeliveryProof{tradeText: run.DeliveryProof.TradeText, picList: append([]string(nil), run.DeliveryProof.PicList...), expectedUnits: run.DeliveryProof.ExpectedUnits, preparedUnits: run.DeliveryProof.PreparedUnits, unknownUnits: run.DeliveryProof.UnknownUnits, skippedTemplateMessages: append([]db.AutomationDeliverySkip(nil), run.DeliveryProof.SkippedTemplateMessages...)}
	proof.refillPending = run.DeliveryProof.RefillPending
	// confirmErr 保存闲鱼确认发货或本地订单事实写入失败原因，消息已发送时不得重新取卡。
	if confirmErr := c.actions.confirmShipmentWithProof(ctx, task, proof); confirmErr != nil {
		return len(messages) + refillCount, confirmErr
	}
	// completeErr 保存补发运行收口失败原因；返回后由延迟释放恢复原终态，沿用既有人工补发处理策略。
	if completeErr := c.store.Automation.CompleteDeliveryReplay(ctx, run.ID, run.AttemptCount); completeErr != nil {
		return len(messages) + refillCount, fmt.Errorf("补发内容已发送但保存运行结果失败: %w", completeErr)
	}
	return len(messages) + refillCount, nil
}

// refillPartialDeliveryProof 为逐单位快照补齐尚未生成的直接卡密；API 卡密和模板卡密缺少安全续取语义时转人工核对。
func (c *Center) refillPartialDeliveryProof(ctx context.Context, task Task, run *db.AutomationRun) (int, error) {
	// remaining 表示扣除已确定和待人工确认单位后仍需生成的卡密数量。
	remaining := run.DeliveryProof.ExpectedUnits - run.DeliveryProof.PreparedUnits - run.DeliveryProof.UnknownUnits - len(run.DeliveryProof.SkippedTemplateMessages)
	if remaining <= 0 {
		return 0, nil
	}
	// refillAction、planErr 从运行创建时的动作计划读取唯一补齐来源，禁止规则编辑改变卡组。
	refillAction, planErr := deliveryReplayPlan(run)
	if planErr != nil {
		return 0, planErr
	}
	// card、cardErr 保存补发动作对应的卡密组类型；API 卡密重新请求可能重复扣费，禁止自动续取。
	card, cardErr := c.store.Cards.GetForDelivery(ctx, refillAction.CardID)
	if cardErr != nil {
		return 0, fmt.Errorf("读取补发卡密组: %w", cardErr)
	}
	if card.Type == "api" {
		return 0, fmt.Errorf("API 卡密剩余单位不能自动重新请求，已保留人工核对")
	}
	// refillTask 固定为单个订单单位，避免把原订单数量再次乘入剩余数量。
	refillTask := task
	refillTask.Quantity = "1"
	// refillAction 按尚缺单位数执行，数量不再乘入原订单数量。
	refillAction.DeliveryCount = remaining
	// pendingProof 在任何库存消费前保存占用；崩溃、取消或后续快照写入失败都不会丢失该保护。
	pendingProof := run.DeliveryProof
	pendingProof.RefillPending = true
	if markErr := c.store.Automation.UpdateDeliveryReplayProof(ctx, run.ID, run.AttemptCount, pendingProof); markErr != nil { // markErr 表示占用未持久化，此时必须在取卡之前终止。
		return 0, fmt.Errorf("保存补取卡密占用: %w", markErr)
	}
	run.DeliveryProof = pendingProof
	// result、sendErr 保存补齐动作的实际发送结果和错误。
	result, sendErr := c.actions.sendCardWithProof(ctx, refillTask, refillAction)
	// newProof 保存本次新增内容；清零计划单位数，避免合并时重复累计原订单总量。
	newProof := result.proof
	newProof.expectedUnits = 0
	// merged 保存原快照与本次补发结果合并后的完整凭证。
	merged := mergeShipmentDeliveryProof(shipmentDeliveryProof{
		tradeText: run.DeliveryProof.TradeText, picList: append([]string(nil), run.DeliveryProof.PicList...),
		messages: append([]db.AutomationDeliveryMessage(nil), run.DeliveryProof.Messages...), preparedUnits: run.DeliveryProof.PreparedUnits,
		unknownUnits: run.DeliveryProof.UnknownUnits, expectedUnits: run.DeliveryProof.ExpectedUnits,
		skippedTemplateMessages: append([]db.AutomationDeliverySkip(nil), run.DeliveryProof.SkippedTemplateMessages...),
	}, newProof)
	merged = mergeShipmentDeliveryProof(merged, result.reviewProof)
	// uncertain 保存没有可恢复内容的外部结果不确定错误，例如库存恢复失败；这类错误不能解除取卡占用。
	var uncertain *uncertainActionError
	// unresolved 表示仍有无法从返回快照核对的取卡结果，后续人工请求必须停止。
	unresolved := errors.As(sendErr, &uncertain) && result.reviewProof.unknownUnits == 0
	// savedProof 同时提交完整新内容与占用状态，避免快照和解除保护分开写入。
	savedProof := db.AutomationDeliveryProof{
		TradeText: merged.tradeText, PicList: append([]string(nil), merged.picList...), Messages: append([]db.AutomationDeliveryMessage(nil), merged.messages...),
		ExpectedUnits: merged.expectedUnits, PreparedUnits: merged.preparedUnits, UnknownUnits: merged.unknownUnits, RefillPending: unresolved || merged.refillPending,
		SkippedTemplateMessages: append([]db.AutomationDeliverySkip(nil), merged.skippedTemplateMessages...),
	}
	// saveCtx、saveCancel 让请求取消后仍能有界保存已消费内容；写入失败则保留之前的占用标记。
	saveCtx, saveCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer saveCancel()
	if saveErr := c.store.Automation.UpdateDeliveryReplayProof(saveCtx, run.ID, run.AttemptCount, savedProof); saveErr != nil { // saveErr 表示外部动作后快照未可靠保存，禁止把旧数量视作可以安全再次取卡。
		return result.sent, errors.Join(fmt.Errorf("保存补发卡密快照失败，已禁止再次取卡，请人工核对: %w", saveErr), sendErr)
	}
	run.DeliveryProof = savedProof
	if sendErr != nil {
		return result.sent, fmt.Errorf("补齐剩余卡密失败: %w", sendErr)
	}
	return result.sent, nil
}
