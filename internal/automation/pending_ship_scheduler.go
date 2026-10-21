package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// paidRecoveryReady 重新核对付款运行的来源、最新订单状态和自动发货总开关。
// 它只允许恢复 WebSocket 已创建或待发货兜底已创建的运行，绝不从历史运行推断砍价阶段或绕过账号开关。
func (s *Scheduler) paidRecoveryReady(ctx context.Context, task Task, run db.AutomationRun) (bool, error) {
	if task.TriggerType != TriggerOrderPaid {
		return true, nil
	}
	if task.Source != "ws" && task.Source != "scheduler" {
		// reason 说明旧版或人工来源付款运行不能被计划任务静默续跑。
		reason := "付款运行缺少可信的 WebSocket 或待发货兜底来源，已停止自动恢复"
		return false, s.quarantineRunForReview(ctx, run, reason)
	}
	// order、orderErr 保存计划任务执行前读取到的最新订单事实。
	order, orderErr := s.center.store.Orders.Get(ctx, run.OrderID)
	if errors.Is(orderErr, db.ErrNotFound) {
		// reason 说明没有订单事实时无法证明买家仍已付款且等待发货。
		reason := "本地缺少订单状态，无法确认订单仍为待发货，已停止自动恢复"
		return false, s.quarantineRunForReview(ctx, run, reason)
	}
	if orderErr != nil {
		// postponeErr 保存订单事实暂时不可读时的恢复延期结果，避免数据库瞬时错误触发外部动作。
		postponeErr := s.center.store.Automation.PostponeRecoveryRun(ctx, run.ID, run.AttemptCount, time.Now().UTC().Add(defaultReviewRequestScanInterval).Unix())
		if postponeErr != nil {
			return false, errors.Join(fmt.Errorf("读取付款恢复订单状态失败: %w", orderErr), fmt.Errorf("延期付款恢复运行失败: %w", postponeErr))
		}
		s.center.logger.Warn("读取付款恢复订单状态失败，已延期等待下次核对", "run_id", run.ID, "account", run.CookieID, "order_id", run.OrderID, "err", orderErr)
		return false, nil
	}
	if order.CookieID != run.CookieID {
		// reason 说明订单归属变化后不能继续使用原运行的账号凭证或会话。
		reason := "订单归属账号与付款运行不一致，已停止自动恢复"
		return false, s.quarantineRunForReview(ctx, run, reason)
	}
	if !isPendingShipOrder(order) || order.SystemShipped {
		// reason 记录自动取消原因；订单已取消、完成或发货时无需用户再次处理。
		reason := fmt.Sprintf("订单当前状态为 %s，不再需要自动发货恢复", firstNonEmpty(order.OrderStatus, "未知"))
		// canceled、cancelErr 保存过期运行是否仍与扫描快照一致并已安全取消。
		canceled, cancelErr := s.center.store.Automation.CancelObsoletePaidRecoveryRun(ctx, run, reason)
		if cancelErr != nil {
			return false, fmt.Errorf("取消已失效付款恢复运行: %w", cancelErr)
		}
		if canceled {
			s.center.logger.Info("订单已不再待发货，取消历史付款恢复运行", "run_id", run.ID, "account", run.CookieID, "order_id", run.OrderID, "order_status", order.OrderStatus, "system_shipped", order.SystemShipped)
		}
		return false, nil
	}
	if task.Source == "ws" && task.OrderRole != OrderRoleSeller {
		// _, sellerVerified、roleReason、roleErr 保存历史 WS 运行的未知角色核验结果；恢复路径不能沿用旧快照猜测卖家身份。
		_, sellerVerified, roleReason, roleErr := s.center.authorizeWebSocketSellerTaskWithOrder(ctx, task, order)
		if roleErr != nil {
			// postponeErr 保存本地商品归属暂时不可读时的延期结果，避免数据库瞬时故障直接执行外部发货。
			postponeErr := s.center.store.Automation.PostponeRecoveryRun(ctx, run.ID, run.AttemptCount, time.Now().UTC().Add(defaultReviewRequestScanInterval).Unix())
			if postponeErr != nil {
				return false, errors.Join(fmt.Errorf("核对未知角色付款运行失败: %w", roleErr), fmt.Errorf("延期付款恢复运行失败: %w", postponeErr))
			}
			s.center.logger.Warn("未知角色付款运行核验失败，已延期等待本地事实恢复", "run_id", run.ID, "account", run.CookieID, "order_id", run.OrderID, "err", roleErr)
			return false, nil
		}
		if !sellerVerified {
			// reason 说明历史运行无法由本地商品和订单身份证明是卖家事件，必须人工核对而不能重放发货。
			reason := "历史 WebSocket 付款运行缺少明确卖家角色且本地证据不足（" + roleReason + "），已停止自动恢复"
			return false, s.quarantineRunForReview(ctx, run, reason)
		}
	}
	// enabled、settingsErr 保存账号当前自动发货总开关及读取错误。
	enabled, settingsErr := s.center.autoDeliveryEnabled(ctx, run.CookieID)
	if settingsErr != nil {
		// postponeErr 保存设置暂时不可读时的恢复延期结果，禁止失败开放。
		postponeErr := s.center.store.Automation.PostponeRecoveryRun(ctx, run.ID, run.AttemptCount, time.Now().UTC().Add(defaultReviewRequestScanInterval).Unix())
		if postponeErr != nil {
			return false, errors.Join(fmt.Errorf("读取付款恢复自动发货开关失败: %w", settingsErr), fmt.Errorf("延期付款恢复运行失败: %w", postponeErr))
		}
		s.center.logger.Warn("读取付款恢复自动发货开关失败，已延期等待下次核对", "run_id", run.ID, "account", run.CookieID, "order_id", run.OrderID, "err", settingsErr)
		return false, nil
	}
	if !enabled {
		// postponeErr 保存用户关闭自动发货后把历史运行移出当前扫描窗口的结果。
		postponeErr := s.center.store.Automation.PostponeRecoveryRun(ctx, run.ID, run.AttemptCount, time.Now().UTC().Add(10*time.Minute).Unix())
		if postponeErr != nil {
			return false, fmt.Errorf("自动发货已关闭，延期付款恢复运行失败: %w", postponeErr)
		}
		s.center.logger.Info("账号已关闭自动发货，付款恢复运行等待用户重新开启", "run_id", run.ID, "account", run.CookieID, "order_id", run.OrderID)
		return false, nil
	}
	return true, nil
}

// scanPendingShipDeliveries 兜底扫描没有付款运行的待发货订单并补触发自动发货。
func (s *Scheduler) scanPendingShipDeliveries(ctx context.Context) {
	if ctx == nil {
		return
	}
	// scanCtx、cancel 为直接调用入口提供独立预算及释放函数。
	scanCtx, cancel := context.WithTimeout(ctx, s.pendingShipScanBudgetValue())
	defer cancel()
	_ = s.scanPendingShipDeliveriesWithContextAndLimit(scanCtx, s.pendingShipScanMaxTasksValue())
}

// scanPendingShipDeliveriesWithContextAndLimit 在预算与任务额度内补触发待发货订单。
func (s *Scheduler) scanPendingShipDeliveriesWithContextAndLimit(ctx context.Context, remainingTasks int) (leftTasks int) {
	leftTasks = remainingTasks
	if strings.TrimSpace(os.Getenv(pendingShipCatchupEnv)) == "0" || s == nil || s.center == nil || s.center.store == nil || s.center.store.Automation == nil || leftTasks <= 0 {
		return
	}
	// triggeredCount 统计本轮实际领取的订单数。
	triggeredCount := 0
	// afterOrderID 保存分页扫描的稳定订单游标。
	afterOrderID := ""
	// sweepStartedAt 是本轮完整扫描的起点；只有扫完全部候选页才会据此清除本轮没再出现的订单观测记录。
	sweepStartedAt := time.Now().UTC()
	for {
		if ctx.Err() != nil {
			s.center.logger.Warn("待发货兜底扫描达到本轮时间预算", "err", ctx.Err())
			return
		}
		// orders、err 保存当前候选页及查询错误。
		orders, err := s.center.store.Automation.PendingShipOrdersWithoutPaidRunAfter(ctx, afterOrderID, pendingShipScanPageSize)
		if err != nil {
			s.center.logger.Warn("扫描待发货兜底订单失败", "err", err)
			return
		}
		if len(orders) == 0 {
			s.pruneUnseenPendingShip(sweepStartedAt)
			return
		}
		// order 表示当前待发货候选订单。
		for _, order := range orders {
			if ctx.Err() != nil || leftTasks <= 0 {
				if ctx.Err() != nil {
					s.center.logger.Warn("待发货兜底扫描达到本轮时间预算", "err", ctx.Err())
				} else {
					s.center.logger.Info("待发货兜底扫描达到本轮任务上限", "count", triggeredCount)
				}
				return
			}
			afterOrderID = order.OrderID
			// catchupReady、waitReason 保存当前订单是否已越过实时事件优先窗口及未放行原因。
			catchupReady, waitReason := pendingShipCatchupReady(order, s.observePendingShip(order.OrderID, time.Now().UTC()), time.Now().UTC(), s.pendingShipSettleWindowValue())
			if !catchupReady {
				s.center.logger.Info("待发货兜底任务等待实时付款事件或人工核对", "account", order.CookieID, "order_id", order.OrderID, "reason", waitReason)
				continue
			}
			// allowed、allowErr 保存账号自动化门禁结果。
			allowed, allowErr := s.center.accountAutomationAllowed(ctx, order.CookieID)
			if allowErr != nil {
				s.center.logger.Warn("检查待发货兜底账号状态失败", "account", order.CookieID, "order_id", order.OrderID, "err", allowErr)
				continue
			}
			if !allowed {
				continue
			}
			// paid、paidErr 保存账号自动发货总开关及读取错误。
			paid, paidErr := s.center.autoDeliveryEnabled(ctx, order.CookieID)
			if paidErr != nil {
				s.center.logger.Warn("检查待发货兜底自动发货开关失败", "account", order.CookieID, "order_id", order.OrderID, "err", paidErr)
				continue
			}
			if !paid {
				continue
			}
			if !s.center.accountSenderReady(order.CookieID) {
				s.center.logger.Info("账号 WebSocket 尚未就绪，待发货兜底任务等待下次扫描", "account", order.CookieID, "order_id", order.OrderID)
				continue
			}
			if !s.claimPendingShipAttempt(order.OrderID) {
				continue
			}
			triggeredCount++
			leftTasks--
			s.center.logger.Info("付款系统消息缺失，按订单状态补触发自动发货", "account", order.CookieID, "order_id", order.OrderID, "item_id", order.ItemID)
			// taskCtx、cancel 为单个兜底任务提供执行预算及释放函数。
			taskCtx, cancel := context.WithTimeout(ctx, pendingShipTaskTimeout)
			// task 保存待发货订单转换后的自动化任务载荷。
			// task 是由本地待发货订单扫描构造的可信卖家任务；它不依赖 WS 卡片中的 role 字段。
			task := Task{Source: "scheduler", AccountID: order.CookieID, OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid, ChatID: order.ChatID, OrderID: order.OrderID, ItemID: order.ItemID, BuyerID: order.BuyerID, Text: "付款系统消息缺失，按订单状态补触发自动发货", Raw: map[string]any{"source": "scheduler", "order_id": order.OrderID}}
			// err 保存兜底任务执行错误；失败只影响当前订单。
			if err := s.center.HandleTask(taskCtx, task); err != nil {
				s.center.logger.Warn("待发货兜底任务执行失败", "account", order.CookieID, "order_id", order.OrderID, "err", err)
			}
			cancel()
		}
		if len(orders) < pendingShipScanPageSize {
			s.pruneUnseenPendingShip(sweepStartedAt)
			return
		}
	}
}

// pendingShipCatchupReady 判断已由候选查询确认阶段资格的订单能否安全转换为付款自动化事件。
// 所有订单都必须先等待实时付款卡片窗口结束；窗口从扫描器首次观测到该候选订单起算，不使用 updated_at，
// 因此订单被反复同步或写入不会无限推迟兜底。若已有 paid_at（付款事件时间）则取更晚者，砍价订单是否已完成免拼由查询层的阶段记录决定，兜底不得推断或调用免拼。
func pendingShipCatchupReady(order db.Order, firstSeen, now time.Time, window time.Duration) (bool, string) {
	// observedAt 是等待窗口的起点：首次观测时间，且不早于已记录的付款时间。
	observedAt := firstSeen
	// paidAt 保存订单已记录的付款时间，解析失败时为零值。
	if paidAt := parseDBTime(order.PaidAt); paidAt.After(observedAt) {
		observedAt = paidAt
	}
	if observedAt.IsZero() {
		return false, "missing_observed_time"
	}
	if now.Before(observedAt.Add(window)) {
		return false, "waiting_for_realtime_payment_event"
	}
	return true, ""
}

// pendingShipSettleWindowValue 返回生效的实时付款卡片等待窗口；零值使用默认值，负值表示不等待。
func (s *Scheduler) pendingShipSettleWindowValue() time.Duration {
	if s == nil || s.pendingShipSettleWindow == 0 {
		return defaultPendingShipSettleWindow
	}
	if s.pendingShipSettleWindow < 0 {
		return 0
	}
	return s.pendingShipSettleWindow
}

// pruneUnseenPendingShip 在一轮完整扫描结束后清除本轮没有再出现的订单观测记录：
// 订单曾短暂离开候选集合（例如退款后又重新待发货）时，再次出现必须重新获得完整的实时付款事件等待窗口，而不是沿用旧的首次观测时间。
// 提前结束的扫描（预算或任务上限）不会调用本方法，因此未扫到的订单只会多保留记录，不会被误清。
func (s *Scheduler) pruneUnseenPendingShip(sweepStartedAt time.Time) {
	if s == nil {
		return
	}
	s.pendingShipMu.Lock()
	defer s.pendingShipMu.Unlock()
	// orderID、item 是观测表中的订单号与记录，最近观测早于本轮起点说明本轮没有再扫到它。
	for orderID, item := range s.pendingShipObserved {
		if item.last.Before(sweepStartedAt) {
			delete(s.pendingShipObserved, orderID)
		}
	}
}

// observePendingShip 记录候选订单在本轮扫描被观测到，并返回其连续出现的首次观测时间。
// 订单缺席超过 pendingShipObservationGap 后重新出现视为再次进入待发货并重新计时；条目过多时顺带清理长期缺席者。
func (s *Scheduler) observePendingShip(orderID string, now time.Time) time.Time {
	if s == nil {
		return now
	}
	s.pendingShipMu.Lock()
	defer s.pendingShipMu.Unlock()
	if s.pendingShipObserved == nil {
		// pendingShipObserved 延迟初始化，兼容测试或历史调用方直接构造 Scheduler 的场景。
		s.pendingShipObserved = make(map[string]pendingShipObservation)
	}
	// entry、known 是该订单已有的观测记录及其是否存在。
	entry, known := s.pendingShipObserved[orderID]
	if !known || now.Sub(entry.last) > pendingShipObservationGap {
		entry = pendingShipObservation{first: now}
	}
	entry.last = now
	s.pendingShipObserved[orderID] = entry
	if len(s.pendingShipObserved) > 1024 {
		// key、item 是观测表中的订单号与记录，长期缺席的条目不再可能连续计时。
		for key, item := range s.pendingShipObserved {
			if now.Sub(item.last) > pendingShipObservationGap {
				delete(s.pendingShipObserved, key)
			}
		}
	}
	return entry.first
}

// pendingShipResumeFrozenPlan 从运行的原始事件快照恢复冻结的动作计划，并判定能否自动续跑。
func pendingShipResumeFrozenPlan(candidate db.PendingShipResume) ([]db.AutomationAction, bool, error) {
	// original 保存运行创建时冻结的任务事实与完整动作计划。
	var original Task
	// err 保存快照解析错误；历史快照损坏时不能猜测缺失的动作。
	if err := json.Unmarshal([]byte(candidate.RawEventJSON), &original); err != nil {
		return nil, false, fmt.Errorf("待发货续跑运行的原始计划无法解析: %w", err)
	}
	if original.AccountID != candidate.Order.CookieID || original.OrderID != candidate.Order.OrderID || len(original.ActionPlan) == 0 {
		return nil, false, fmt.Errorf("待发货续跑运行的原始计划缺失或归属不符")
	}
	if candidate.ActionCursor < 0 || candidate.ActionCursor > len(original.ActionPlan) {
		return nil, false, fmt.Errorf("待发货续跑运行的游标越界: %d", candidate.ActionCursor)
	}
	if !pendingShipOnlyIdempotentTail(original.ActionPlan[candidate.ActionCursor:]) {
		return nil, false, nil
	}
	return original.ActionPlan, true, nil
}

// pendingShipOnlyIdempotentTail 判断剩余动作是否只包含平台侧幂等的状态动作。
func pendingShipOnlyIdempotentTail(remaining []db.AutomationAction) bool {
	if len(remaining) == 0 {
		return false
	}
	// action 是剩余动作中的一个，需要确认其不会再次联系买家。
	for _, action := range remaining {
		if action.ActionType != ActionConfirmShipment {
			return false
		}
	}
	return true
}

// claimPendingShipAttempt 领取一次兜底尝试；处于冷却窗口内的订单返回 false。
func (s *Scheduler) claimPendingShipAttempt(orderID string) bool {
	if s == nil {
		return false
	}
	s.pendingShipMu.Lock()
	defer s.pendingShipMu.Unlock()
	if s.pendingShipCooldown == nil {
		// pendingShipCooldown 延迟初始化，兼容测试或历史调用方直接构造 Scheduler 的场景。
		s.pendingShipCooldown = make(map[string]time.Time)
	}
	// now 是本次冷却判断的统一时间基准，避免同一轮内多次取时。
	now := time.Now()
	// last、seen 保存该订单上次兜底触发时间以及是否存在。
	if last, seen := s.pendingShipCooldown[orderID]; seen && now.Sub(last) < defaultPendingShipCooldown {
		return false
	}
	s.pendingShipCooldown[orderID] = now
	if len(s.pendingShipCooldown) > 4096 {
		// key、ts 是过期条目的订单号与记录时间。
		for key, ts := range s.pendingShipCooldown {
			if now.Sub(ts) >= defaultPendingShipCooldown {
				delete(s.pendingShipCooldown, key)
			}
		}
	}
	return true
}

// releasePendingShipAttempt 释放已知未取得数据库运行权的冷却预约。
func (s *Scheduler) releasePendingShipAttempt(orderID string) {
	if s == nil {
		return
	}
	s.pendingShipMu.Lock()
	defer s.pendingShipMu.Unlock()
	delete(s.pendingShipCooldown, orderID)
}

// quarantineMissingProofResume 把凭证缺失的退款撤回续跑候选转入人工核对并通知。
// 通知 outbox 记录与状态更新在同一数据库事务内写入：只有运行确实被隔离才会产生通知，隔离失败或并发竞争时不会发出
// 与状态不一致的通知；渠道查询失败则放弃本轮、运行保持原状，下一轮重试。通知器不支持事务内入队时回退为隔离后尽力通知。
func (s *Scheduler) quarantineMissingProofResume(ctx context.Context, candidate db.PendingShipResume) {
	// reason 说明停止自动确认发货的原因，不含任何凭证内容。
	reason := "退款撤回后续跑缺少已发送的发货凭证，已停止自动确认发货，请人工核对"
	// run 由扫描候选直接构造通知上下文，不依赖回读数据库。
	run := db.AutomationRun{
		ID: candidate.RunID, RuleID: candidate.RuleID, CookieID: candidate.Order.CookieID, ItemID: candidate.Order.ItemID,
		OrderID: candidate.Order.OrderID, BuyerID: candidate.Order.BuyerID, ChatID: candidate.Order.ChatID,
		TriggerType: TriggerOrderPaid, Status: "needs_review", AttemptCount: candidate.Attempt, ActionCursor: candidate.ActionCursor,
	}
	// outbox、transactional、prepareErr 保存待事务写入的通知记录、是否支持事务内入队及渠道查询错误。
	outbox, transactional, prepareErr := s.center.notifications.prepareRunNeedsReviewOutbox(ctx, run, reason)
	if prepareErr != nil {
		s.center.logger.Error("准备缺失凭证续跑的人工处理通知失败，保留运行等待下一轮重试", "run_id", candidate.RunID, "err", prepareErr)
		return
	}
	// quarantined、err 保存条件隔离（含通知入队）结果；陈旧扫描零行更新属于正常竞争。
	quarantined, err := s.center.store.Automation.QuarantineCanceledRunMissingProof(ctx, candidate.RunID, candidate.Attempt, reason, outbox)
	if err != nil {
		s.center.logger.Error("保存缺失凭证续跑运行人工核对状态失败，下一轮重试", "run_id", candidate.RunID, "err", err)
		return
	}
	if quarantined && !transactional {
		s.center.notifyRunNeedsReview(ctx, run, reason)
	}
}
