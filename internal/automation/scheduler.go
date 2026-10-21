package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
)

// defaultReviewRequestScanInterval 用于本次流程后续判断的defaultReview请求ScanInterval
const defaultReviewRequestScanInterval = time.Minute

// defaultDeferredTaskScanInterval 是持久化延迟动作的轮询周期，保证秒级动作不会被分钟级业务扫描额外延后。
const defaultDeferredTaskScanInterval = time.Second

// legacySchedulerWaitTimeout 是兼容无 Context 等待入口的最长收束预算。
const legacySchedulerWaitTimeout = 10 * time.Second

// pendingShipCatchupEnv 控制「待发货兜底扫描」的开关：默认开启，显式设为 "0" 时整体跳过。
// 保留开关是为了在上游接口异常时能快速止血，而不必回滚二进制。
const pendingShipCatchupEnv = "XIANYU_PENDING_SHIP_CATCHUP"

// defaultPendingShipCooldown 是同一订单两次兜底触发之间的最小间隔。
// 付款事件在准备阶段失败时可能不会留下任何运行记录，若不设冷却会每分钟重试并放大上游压力。
const defaultPendingShipCooldown = 10 * time.Minute

// pendingShipObservationGap 是候选订单连续缺席超过多久后重新按新进入待发货计时（远大于分钟级扫描周期）。
const pendingShipObservationGap = 10 * time.Minute

// pendingShipObservation 保存一笔待发货候选订单被兜底扫描观测到的时间事实。
type pendingShipObservation struct {
	// first 是本次连续出现的首次观测时间，等待窗口据此计算。
	first time.Time
	// last 是最近一次观测时间，用于判断订单是否曾离开候选集合并清理过期条目。
	last time.Time
}

// defaultPendingShipSettleWindow 是新进入待发货状态的订单在通用兜底触发前必须经历的观察窗口。
// 该窗口让实时付款系统卡片优先到达并建立运行记录，避免调度器把尚在结算中的订单误判为事件丢失。
const defaultPendingShipSettleWindow = 2 * time.Minute

// pendingShipTaskTimeout 是单次兜底任务的执行预算，避免上游接口挂起拖住整个分钟级扫描。
const pendingShipTaskTimeout = 90 * time.Second

// defaultPendingShipScanBudget 是付款兜底与续跑扫描共享的单轮最长预算，防止慢平台请求阻塞其他计划任务。
const defaultPendingShipScanBudget = 30 * time.Second

// defaultPendingShipScanMaxTasks 是单轮每条待发货扫描最多实际触发的任务数，超出的订单留给下一轮处理。
const defaultPendingShipScanMaxTasks = 20

// pendingShipScanPageSize 是待发货扫描每次从数据库读取的候选页大小，避免单轮装载过多订单事实。
const pendingShipScanPageSize = 50

// pendingShipResumeMaxAttempts 是同一运行允许被兜底续跑的最大代次，防止上游持续异常时无限重开。
const pendingShipResumeMaxAttempts = 5

// Scheduler 执行计划任务类自动化。
// 计划任务只负责“发现应该触发的任务”，具体动作仍交给 Center，避免形成第二套执行链。
// Scheduler 用于本次流程后续判断的Scheduler
type Scheduler struct {
	// center 是调度器唯一使用的自动化中心，负责实际执行延迟、恢复和求评价任务。
	center *Center
	// interval 是账号任务、恢复任务和求评价任务的分钟级扫描周期。
	interval time.Duration
	// deferredInterval 是已持久化延迟动作的秒级扫描周期，不影响其他计划任务的扫描频率。
	deferredInterval time.Duration
	// runOnce 保证一个调度器实例只启动一个由调用方 Context 管理的循环。
	runOnce sync.Once
	// done 在调度循环退出后关闭，供关闭流程等待全部调度工作停止。
	done chan struct{}
	// pendingShipMu 保护 pendingShipCooldown 与 pendingShipObserved。
	pendingShipMu sync.Mutex
	// pendingShipObserved 记录兜底扫描对待发货候选订单的首次与最近观测时间，窗口只取决于首次观测，不受订单反复写入影响；
	// 仅在持有 pendingShipMu 时读写，延迟初始化。
	pendingShipObserved map[string]pendingShipObservation
	// pendingShipCooldown 记录兜底扫描最近尝试过的订单时间，避免同一订单反复重试。
	pendingShipCooldown map[string]time.Time
	// pendingShipScanBudget 限制付款兜底和续跑扫描共享的单轮执行时间；零值使用生产默认值。
	pendingShipScanBudget time.Duration
	// pendingShipSettleWindow 是候选订单等待实时付款卡片的窗口；零值使用生产默认值，负值表示不等待（仅测试使用）。
	pendingShipSettleWindow time.Duration
	// pendingShipScanMaxTasks 限制每条待发货扫描单轮实际触发的任务数；零值使用生产默认值。
	pendingShipScanMaxTasks int
}

// NewScheduler 构造计划任务调度器。
func NewScheduler(center *Center) *Scheduler {
	return &Scheduler{
		center:              center,
		interval:            defaultReviewRequestScanInterval,
		deferredInterval:    defaultDeferredTaskScanInterval,
		done:                make(chan struct{}),
		pendingShipCooldown: make(map[string]time.Time),
	}
}

// Run 周期扫描计划任务。调用方应在 goroutine 中启动，并用 ctx 控制生命周期。
func (s *Scheduler) Run(ctx context.Context) {
	// nil Context 无法提供调度器停止信号，拒绝启动以免创建无法回收的 goroutine。
	if ctx == nil {
		return
	}
	if s == nil || s.center == nil || s.center.store == nil {
		return
	}
	s.runOnce.Do(func() {
		defer close(s.done)
		if ctx.Err() != nil {
			return
		}
		// generalTicker 驱动分钟级的账号、恢复与求评价扫描。
		generalTicker := time.NewTicker(s.interval)
		defer generalTicker.Stop()
		// deferredTicker 只领取已到期的延迟动作，确保配置的秒数不会额外等待一分钟。
		deferredTicker := time.NewTicker(s.deferredInterval)
		defer deferredTicker.Stop()
		s.scanDeferredTasks(ctx)
		s.scan(ctx)
		for {
			select {
			case <-ctx.Done():
				return
			case <-generalTicker.C:
				s.scan(ctx)
			case <-deferredTicker.C:
				s.scanDeferredTasks(ctx)
			}
		}
	})
}

// Wait 等待调度器完成，并兼容不需要超时的旧调用方。
func (s *Scheduler) Wait() {
	// waitCtx、waitCancel 为兼容入口提供受限等待预算，避免调度器异常时永久阻塞调用方。
	waitCtx, waitCancel := context.WithTimeout(context.Background(), legacySchedulerWaitTimeout)
	defer waitCancel()
	_ = s.WaitContext(waitCtx)
}

// WaitContext 在 ctx 约束内等待调度器完成，避免关闭流程无限阻塞。
func (s *Scheduler) WaitContext(ctx context.Context) error {
	if s != nil && s.done != nil {
		if ctx == nil {
			return errors.New("等待自动化调度器需要关闭 Context")
		}
		select {
		case <-s.done:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return nil
}

// scan 封装scan业务协调。
func (s *Scheduler) scan(ctx context.Context) {
	s.center.scanAccountTasks(ctx)
	if // recovered、err 用于本次流程后续判断的recovered、err
	recovered, err := s.center.store.Automation.RecoverDefinitelyUnsentReviewRuns(ctx); err != nil {
		s.center.logger.Warn("恢复历史求评价未发送任务失败", "err", err)
	} else if recovered > 0 {
		s.center.logger.Info("已恢复历史求评价未发送任务，等待安全重试", "count", recovered)
	}
	// recoveryErr 汇总恢复运行状态收口失败，避免数据库写错误只记录日志后丢失。
	recoveryErr := s.runRecoveryTasks(ctx)
	if recoveryErr != nil {
		// 单独记录恢复任务状态收口错误，延迟任务由秒级扫描函数独立记录。
		s.center.logger.Error("自动化恢复任务状态收口失败", "err", recoveryErr)
	}
	// 逐页执行，避免把所有到期订单一次性装入内存。稳定 ID 游标确保本轮有界。
	afterOrderID := ""
	// waitingForWS 按账号累加本轮因实时连接未就绪而跳过的求评价订单数。
	waitingForWS := map[string]int{}
	for {
		// orders、err 用于本次流程后续判断的orders、err
		orders, err := s.center.store.Automation.DueReviewRequestOrdersAfter(ctx, afterOrderID, 200)
		if err != nil {
			s.center.logger.Warn("扫描求评价计划任务失败", "err", err)
			return
		}
		// order 表示当前遍历过程中的订单
		for _, order := range orders {
			// allowed、allowErr 用于本次流程后续判断的allowed、allowErr
			allowed, allowErr := s.center.accountAutomationAllowed(ctx, order.CookieID)
			if allowErr != nil {
				s.center.logger.Warn("检查求评价账号状态失败", "account", order.CookieID, "err", allowErr)
				continue
			}
			if !allowed {
				continue
			}
			if !s.center.accountSenderReady(order.CookieID) {
				waitingForWS[order.CookieID]++
				continue
			}
			// rules、err 用于本次流程后续判断的rules、err
			rules, err := s.center.rules.match(ctx, Task{AccountID: order.CookieID, ItemID: order.ItemID, TriggerType: TriggerReviewMissingTimeout})
			if err != nil {
				s.center.logger.Warn("查询求评价自动化规则失败", "account", order.CookieID, "order_id", order.OrderID, "item_id", order.ItemID, "err", err)
				continue
			}
			if len(rules) == 0 {
				continue
			}
			// rule 表示当前遍历过程中的规则
			for _, rule := range rules {
				if !reviewRequestRuleDue(order, rule) {
					continue
				}
				// task 用于本次流程后续判断的任务
				task := Task{Source: "scheduler", AccountID: order.CookieID, TriggerType: TriggerReviewMissingTimeout,
					ChatID: order.ChatID, OrderID: order.OrderID, ItemID: order.ItemID, BuyerID: order.BuyerID,
					Text: "发货后一段时间未评价", Raw: map[string]any{"source": "scheduler", "rule_id": rule.ID,
						"order_id": order.OrderID, "attempt": order.ReviewRequestCount + 1}}
				// executeErr 保存求评价规则本轮执行的最终结果，用于区分成功、延期和失败。
				executeErr := s.center.executeRule(ctx, task, rule)
				switch {
				case executeErr == nil:
					s.center.logger.Info("求评价计划任务执行成功", "account", order.CookieID, "order_id", order.OrderID, "rule_id", rule.ID)
				case errors.Is(executeErr, errAutomationDeferred):
					s.center.logger.Info("求评价计划任务已延期，等待下一次执行", "account", order.CookieID, "order_id", order.OrderID, "rule_id", rule.ID)
				default:
					s.center.logger.Warn("求评价计划任务执行失败", "account", order.CookieID, "order_id", order.OrderID, "rule_id", rule.ID, "err", executeErr)
				}
			}
		}
		if len(orders) < 200 {
			break
		}
		afterOrderID = orders[len(orders)-1].OrderID
	}
	// pendingCtx 为两条待发货扫描共享的单轮预算；任一慢任务达到预算都会把后续工作留给下一轮。
	pendingCtx, pendingCancel := context.WithTimeout(ctx, s.pendingShipScanBudgetValue())
	// pendingTasksLeft 保存两条待发货扫描共享的单轮任务额度，避免两条扫描合计突破上限。
	pendingTasksLeft := s.pendingShipScanMaxTasksValue()
	// 待发货兜底扫描：付款系统消息丢失时，订单不会有任何运行记录，必须由订单状态补触发。
	pendingTasksLeft = s.scanPendingShipDeliveriesWithContextAndLimit(pendingCtx, pendingTasksLeft)
	// 待发货续跑扫描：运行已经产生但未做完（例如消息动作结果不确定后被隔离），需从检查点继续。
	s.scanPendingShipResumesWithContextAndLimit(pendingCtx, pendingTasksLeft)
	pendingCancel()
	// accountID、count 表示当前遍历过程中的账号ID、count
	for accountID, count := range waitingForWS {
		s.center.logger.Info("账号 WebSocket 尚未就绪，求评价任务等待下次扫描", "account", accountID, "orders", count)
	}
}

// scanPendingShipResumes 兜底续跑「有运行但剩余动作全部为幂等状态动作」的待发货订单。
//
// 存在意义：运行可能在没有完成全部动作的情况下收口（例如消息动作结果不确定被隔离，
// 或收口过程中进程退出）。这类订单不会被 PendingShipOrdersWithoutPaidRunAfter 选中
// （它已经有一条运行），于是永久停在待发货，且没有任何重试入口。
//
// 安全边界：续跑集合由 PendingShipResumableRunsAfter 限定为「游标已越过全部发卡/模板动作」，
// 因此这里实际执行的只可能是 confirm_shipment 这类不会再次联系买家、且平台侧幂等的动作。
func (s *Scheduler) scanPendingShipResumes(ctx context.Context) {
	if ctx == nil {
		return
	}
	// scanCtx 为直接调用该扫描入口时提供的独立单轮预算；正式调度由 scan 传入共享预算上下文。
	scanCtx, cancel := context.WithTimeout(ctx, s.pendingShipScanBudgetValue())
	defer cancel()
	s.scanPendingShipResumesWithContext(scanCtx)
}

// scanPendingShipResumesWithContext 在给定预算内扫描并续跑尚未完成的待发货运行。
func (s *Scheduler) scanPendingShipResumesWithContext(ctx context.Context) {
	// remainingTasks 保存本次直接调用可使用的待发货任务额度。
	remainingTasks := s.pendingShipScanMaxTasksValue()
	_ = s.scanPendingShipResumesWithContextAndLimit(ctx, remainingTasks)
}

// scanPendingShipResumesWithContextAndLimit 在给定预算与剩余任务额度内扫描并续跑尚未完成的待发货运行。
// 返回尚未消耗的额度，保持付款兜底和续跑扫描共享单轮上限。
func (s *Scheduler) scanPendingShipResumesWithContextAndLimit(ctx context.Context, remainingTasks int) (leftTasks int) {
	leftTasks = remainingTasks
	// 与补触发共用同一个止血开关：上游异常时可一次性关闭全部待发货兜底行为。
	if strings.TrimSpace(os.Getenv(pendingShipCatchupEnv)) == "0" {
		return
	}
	if s == nil || s.center == nil || s.center.store == nil || s.center.store.Automation == nil {
		return
	}
	if leftTasks <= 0 {
		return
	}
	// triggeredCount 统计本轮已经成功重开的运行数，达到上限后把余量留给下一轮。
	triggeredCount := 0
	// afterOrderID 是逐页扫描的稳定游标，确保本轮有界。
	afterOrderID := ""
	for {
		if ctx.Err() != nil {
			s.center.logger.Warn("待发货续跑扫描达到本轮时间预算", "err", ctx.Err())
			return
		}
		// candidates、err 保存本页可续跑运行及查询错误。
		candidates, err := s.center.store.Automation.PendingShipResumableRunsAfter(ctx, afterOrderID, pendingShipResumeMaxAttempts, pendingShipScanPageSize)
		if err != nil {
			s.center.logger.Warn("扫描待发货续跑运行失败", "err", err)
			return
		}
		if len(candidates) == 0 {
			return
		}
		// candidate 表示当前遍历过程中的可续跑运行。
		for _, candidate := range candidates {
			if ctx.Err() != nil {
				s.center.logger.Warn("待发货续跑扫描达到本轮时间预算", "err", ctx.Err())
				return
			}
			if leftTasks <= 0 {
				s.center.logger.Info("待发货续跑扫描达到本轮任务上限", "count", triggeredCount)
				return
			}
			afterOrderID = candidate.Order.OrderID
			// allowed、allowErr 保存账号可用性检查结果。
			allowed, allowErr := s.center.accountAutomationAllowed(ctx, candidate.Order.CookieID)
			if allowErr != nil {
				s.center.logger.Warn("检查待发货续跑账号状态失败", "account", candidate.Order.CookieID, "order_id", candidate.Order.OrderID, "err", allowErr)
				continue
			}
			if !allowed {
				continue
			}
			// paid、paidErr 保存付款自动发货的账号级开关检查结果，必须在重开运行之前检查：
			// 开关关闭时 HandleTask 会直接返回而不收口运行，被重开的运行会留在 running 并继续持有租约，
			// 租约到期后失败运行恢复链路直接执行 executeRule，从而绕过账号自动发货总开关。
			paid, paidErr := s.center.autoDeliveryEnabled(ctx, candidate.Order.CookieID)
			if paidErr != nil {
				s.center.logger.Warn("检查待发货续跑自动发货开关失败", "account", candidate.Order.CookieID, "order_id", candidate.Order.OrderID, "err", paidErr)
				continue
			}
			if !paid {
				continue
			}
			// frozenPlan、eligible、planErr 保存运行快照里冻结的动作计划、是否可自动续跑及不可续跑原因。
			frozenPlan, eligible, planErr := pendingShipResumeFrozenPlan(candidate)
			if planErr != nil || !eligible {
				s.center.logger.Info("待发货运行不满足自动续跑条件，保留人工核对",
					"account", candidate.Order.CookieID, "order_id", candidate.Order.OrderID,
					"run_id", candidate.RunID, "action_cursor", candidate.ActionCursor, "err", planErr)
				continue
			}
			if candidate.Status == "canceled" && !candidate.HasDeliveryProof {
				// 退款撤回后的已取消运行缺失发货凭证（例如旧版本取消时已清空）：确认发货会提交空凭证，
				// 且无法证明买家收到的内容，必须停止自动确认并转人工核对，同时发送独立通知。
				s.quarantineMissingProofResume(ctx, candidate)
				continue
			}
			// 冷却窗口先做一次本地预约，避免多个调度器同时提交重开；已知抢占失败时会释放这次预约。
			if !s.claimPendingShipAttempt(candidate.Order.OrderID) {
				continue
			}
			// reopened、reopenErr 保存重开运行结果；失败说明状态或代次已变化，放弃本次续跑。
			reopened, reopenErr := s.center.store.Automation.ReopenRunForRecovery(ctx, candidate.RunID, candidate.Attempt, time.Now().UTC().Add(5*time.Minute).Unix())
			if reopenErr != nil || !reopened {
				if reopenErr == nil {
					// 已知没有取得数据库执行权，不应让失败的竞争者消耗订单冷却窗口。
					s.releasePendingShipAttempt(candidate.Order.OrderID)
				}
				s.center.logger.Warn("重开未完成待发货运行失败",
					"account", candidate.Order.CookieID, "order_id", candidate.Order.OrderID,
					"run_id", candidate.RunID, "attempt", candidate.Attempt, "reopened", reopened, "err", reopenErr)
				continue
			}
			triggeredCount++
			leftTasks--
			s.center.logger.Info("待发货运行未完成，按检查点续跑剩余状态动作",
				"account", candidate.Order.CookieID, "order_id", candidate.Order.OrderID,
				"run_id", candidate.RunID, "action_cursor", candidate.ActionCursor, "previous_status", candidate.Status)
			// taskCtx 限制单次执行预算，上游接口挂起时不能让分钟级扫描被永久拖住。
			taskCtx, cancel := context.WithTimeout(ctx, pendingShipTaskTimeout)
			// task 携带运行快照里冻结的动作计划与运行标识：执行链必须沿用运行创建时的计划，
			// 不能把数字游标套用到管理员后来修改过的规则上。
			// task 是从本地卖家订单和已冻结运行计划恢复的任务；显式标记卖家，避免被 WS 未知角色门禁误拦截。
			task := Task{Source: "scheduler", AccountID: candidate.Order.CookieID, OrderRole: OrderRoleSeller, TriggerType: TriggerOrderPaid,
				ChatID: candidate.Order.ChatID, OrderID: candidate.Order.OrderID,
				ItemID: candidate.Order.ItemID, BuyerID: candidate.Order.BuyerID,
				ActionPlan: frozenPlan,
				Text:       "待发货运行未完成，按检查点续跑",
				Raw: map[string]any{"source": "scheduler", "order_id": candidate.Order.OrderID,
					"automation_run_id": candidate.RunID, "automation_rule_id": candidate.RuleID}}
			// err 保存本次续跑任务的处理错误；只告警，不阻断其余候选运行的续跑。
			if err := s.center.HandleTask(taskCtx, task); err != nil {
				s.center.logger.Warn("待发货续跑任务执行失败",
					"account", candidate.Order.CookieID, "order_id", candidate.Order.OrderID, "err", err)
			}
			cancel()
		}
		if len(candidates) < pendingShipScanPageSize {
			return
		}
	}
}

// pendingShipScanBudgetValue 返回调度器配置的待发货扫描预算，零值回落到固定生产默认值。
func (s *Scheduler) pendingShipScanBudgetValue() time.Duration {
	if s != nil && s.pendingShipScanBudget > 0 {
		return s.pendingShipScanBudget
	}
	return defaultPendingShipScanBudget
}

// pendingShipScanMaxTasksValue 返回调度器配置的单轮任务上限，零值回落到固定生产默认值。
func (s *Scheduler) pendingShipScanMaxTasksValue() int {
	if s != nil && s.pendingShipScanMaxTasks > 0 {
		return s.pendingShipScanMaxTasks
	}
	return defaultPendingShipScanMaxTasks
}

// scanDeferredTasks 领取并重放已到期延迟动作；错误独立记录，避免影响分钟级扫描的调度节奏。
func (s *Scheduler) scanDeferredTasks(ctx context.Context) {
	// deferredErr 保存本轮延迟动作状态收口错误，必须记录以便管理员追踪人工核对任务。
	deferredErr := s.runDeferredTasks(ctx)
	if deferredErr != nil {
		s.center.logger.Error("自动化延迟任务状态收口失败", "err", deferredErr)
	}
}

// runRecoveryTasks 封装运行Recovery任务列表业务协调。
func (s *Scheduler) runRecoveryTasks(ctx context.Context) error {
	// resultErr 汇总本轮恢复任务的持久化错误，调用方可据此触发统一告警。
	var resultErr error
	// runs、err 用于本次流程后续判断的runs、err
	runs, err := s.center.store.Automation.DueRecoveryRuns(ctx, 100)
	if err != nil {
		s.center.logger.Warn("扫描失败自动化运行失败", "err", err)
		return err
	}
	// run 表示当前遍历过程中的运行
	for _, run := range runs {
		if run.ActionStarted {
			// reason 用于本次流程后续判断的原因
			reason := "进程在外部动作执行期间中断，发送结果未知，已禁止自动重放"
			// quarantineErr 表示把外部动作结果未知的运行转为人工核对状态时的错误。
			quarantineErr := s.quarantineRunForReview(ctx, run, reason)
			resultErr = errors.Join(resultErr, quarantineErr)
			continue
		}
		// task 用于本次流程后续判断的任务
		var task Task
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(run.RawEventJSON), &task); err != nil || task.AccountID == "" {
			// reason 用于本次流程后续判断的原因
			reason := "历史运行数据无法安全解析，已移入人工检查"
			// quarantineErr 表示历史任务无法解析时写入人工核对状态的错误。
			quarantineErr := s.quarantineRunForReview(ctx, run, reason)
			resultErr = errors.Join(resultErr, quarantineErr)
			continue
		}
		if task.AccountID != run.CookieID || task.TriggerType != run.TriggerType || run.OrderID != "" && task.OrderID != run.OrderID {
			// reason 说明持久化快照与运行不可变身份不一致，禁止使用快照中的账号或订单执行外部动作。
			reason := "历史运行快照与运行身份不一致，已停止自动恢复"
			// quarantineErr 保存身份不一致运行的人工核对状态写入错误。
			quarantineErr := s.quarantineRunForReview(ctx, run, reason)
			resultErr = errors.Join(resultErr, quarantineErr)
			continue
		}
		// allowed、err 用于本次流程后续判断的allowed、err
		allowed, err := s.center.accountAutomationAllowed(ctx, task.AccountID)
		if err != nil || !allowed {
			if // postponeErr 用于本次流程后续判断的postponeErr
			postponeErr := s.center.store.Automation.PostponeRecoveryRun(ctx, run.ID, run.AttemptCount, time.Now().UTC().Add(10*time.Minute).Unix()); postponeErr != nil {
				s.center.logger.Warn("延期自动化恢复任务失败", "run_id", run.ID, "err", postponeErr)
				resultErr = errors.Join(resultErr, fmt.Errorf("延期自动化恢复任务失败: %w", postponeErr))
			}
			continue
		}
		// paidReady、paidGateErr 保存付款运行是否仍符合订单状态兜底边界及门禁处理错误。
		paidReady, paidGateErr := s.paidRecoveryReady(ctx, task, run)
		if paidGateErr != nil {
			resultErr = errors.Join(resultErr, paidGateErr)
		}
		if !paidReady {
			continue
		}
		// rule、err 用于本次流程后续判断的rule、err
		rule, err := s.center.store.Automation.Get(ctx, run.RuleID)
		if err != nil && !errors.Is(err, db.ErrNotFound) {
			if ctx.Err() != nil {
				return errors.Join(resultErr, ctx.Err())
			}
			s.center.logger.Warn("读取自动化恢复规则失败，保留任务等待重试", "run_id", run.ID, "rule_id", run.RuleID, "err", err)
			continue
		}
		if errors.Is(err, db.ErrNotFound) || rule == nil || !rule.Enabled {
			// reason 用于本次流程后续判断的原因
			reason := "自动化规则不存在或已停用，无法恢复"
			// quarantineErr 表示规则不可恢复时写入人工核对状态的错误。
			quarantineErr := s.quarantineRunForReview(ctx, run, reason)
			resultErr = errors.Join(resultErr, quarantineErr)
			continue
		}
		if recoveryNeedsSender(task, *rule, run.ActionCursor) && !s.center.accountSenderReady(task.AccountID) {
			if // postponeErr 用于本次流程后续判断的postponeErr
			postponeErr := s.center.store.Automation.PostponeRecoveryRun(ctx, run.ID, run.AttemptCount, time.Now().UTC().Add(defaultReviewRequestScanInterval).Unix()); postponeErr != nil {
				s.center.logger.Warn("等待 WebSocket 时延期自动化任务失败", "run_id", run.ID, "err", postponeErr)
				resultErr = errors.Join(resultErr, fmt.Errorf("等待 WebSocket 时延期自动化任务失败: %w", postponeErr))
			}
			continue
		}
		// claimed、claimErr 用于本次流程后续判断的claimed、claimErr
		claimed, claimErr := s.center.store.Automation.ClaimRecoveryRun(ctx, run.ID, time.Now().UTC().Add(5*time.Minute).Unix())
		if claimErr != nil {
			// claimFailure 表示领取恢复运行的状态写入失败，必须返回而不能被当作并发未领取。
			claimFailure := fmt.Errorf("领取自动化恢复任务失败: %w", claimErr)
			resultErr = errors.Join(resultErr, claimFailure)
		}
		if claimErr != nil || !claimed {
			continue
		}
		if task.Raw == nil {
			task.Raw = map[string]any{}
		}
		task.Raw["automation_run_id"] = run.ID
		task.Raw["automation_rule_id"] = run.RuleID
		// executeErr 保存恢复运行本轮执行的最终结果，用于区分成功、延期和失败。
		executeErr := s.center.executeRule(ctx, task, *rule)
		switch {
		case executeErr == nil:
			s.center.logger.Info("自动化恢复任务执行成功", "run_id", run.ID, "account", task.AccountID, "order_id", task.OrderID)
		case errors.Is(executeErr, errAutomationDeferred):
			s.center.logger.Info("自动化恢复任务已延期，等待下一次执行", "run_id", run.ID, "account", task.AccountID, "order_id", task.OrderID)
		default:
			s.center.logger.Warn("重试自动化运行失败", "run_id", run.ID, "err", executeErr)
			resultErr = errors.Join(resultErr, executeErr)
		}
	}
	return resultErr
}

// quarantineRunForReview 将恢复运行置为人工核对并发送运维通知；写入失败时返回统一 needs_review 错误，禁止调用方误认为状态已收口。
func (s *Scheduler) quarantineRunForReview(ctx context.Context, run db.AutomationRun, reason string) error {
	// quarantined、quarantineErr 表示扫描快照仍有效并已隔离及写入错误；陈旧扫描不能覆盖续租或发送完成后的状态。
	quarantined, quarantineErr := s.center.store.Automation.QuarantineRecoveryRun(ctx, run, reason)
	if quarantined {
		s.center.notifyRunNeedsReview(ctx, run, reason)
	}
	if quarantineErr == nil {
		return nil
	}
	s.center.logger.Error("保存自动化恢复运行人工核对状态失败", "run_id", run.ID, "err", quarantineErr)
	return errors.Join(
		errAutomationNeedsReview,
		errAutomationQuarantine,
		fmt.Errorf("保存自动化恢复运行人工核对状态失败: %w", quarantineErr),
	)
}

// recoveryNeedsSender 封装recoveryNeedsSender业务协调。
func recoveryNeedsSender(task Task, rule db.AutomationRule, cursor int) bool {
	// actions 用于本次流程后续判断的动作列表
	actions := task.ActionPlan
	if len(actions) == 0 {
		actions = (actionPlanner{}).plan(task, rule.Actions)
	}
	if cursor < 0 || cursor >= len(actions) {
		return false
	}
	switch actions[cursor].ActionType {
	case ActionSendText, ActionSendCard, ActionSendTemplate:
		return true
	default:
		return false
	}
}

// runDeferredTasks 封装运行Deferred任务列表业务协调。
func (s *Scheduler) runDeferredTasks(ctx context.Context) error {
	// resultErr 汇总延迟任务最终状态写入失败，避免领取成功后状态异常被静默吞掉。
	var resultErr error
	// tasks、err 用于本次流程后续判断的tasks、err
	tasks, err := s.center.store.Automation.ClaimDueDeferredTasks(ctx, 100)
	if err != nil {
		s.center.logger.Warn("扫描暂停期间自动化事件失败", "err", err)
		return err
	}
	// pending 表示当前遍历过程中的pending
	for _, pending := range tasks {
		// task 用于本次流程后续判断的任务
		var task Task
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(pending.TaskJSON), &task); err != nil {
			// failureReason 是写入重试状态和人工处理通知共用的解析失败原因。
			failureReason := "解析任务失败: " + err.Error()
			// finishErr 表示解析失败后写入延迟任务重试或死信状态时的错误。
			finishErr := s.center.store.Automation.FinishDeferredTask(ctx, pending.ID, pending.ClaimVersion, false, failureReason)
			if finishErr != nil {
				s.center.logger.Error("保存解析失败的暂停事件状态失败", "task_id", pending.ID, "err", finishErr)
				s.notifyDeferredTaskNeedsReview(ctx, pending, Task{AccountID: pending.CookieID, TriggerType: pending.TriggerType}, failureReason+"；保存任务状态失败："+finishErr.Error())
				resultErr = errors.Join(
					resultErr,
					errAutomationNeedsReview,
					fmt.Errorf("保存解析失败的暂停事件状态失败: %w", finishErr),
				)
			} else {
				s.center.logger.Warn("暂停期间自动化事件重放失败", "task_id", pending.ID, "account", pending.CookieID, "err", err)
				if pending.ClaimVersion >= 5 {
					s.notifyDeferredTaskNeedsReview(ctx, pending, Task{AccountID: pending.CookieID, TriggerType: pending.TriggerType}, failureReason+"；已达到自动重试上限")
				}
			}
			continue
		}
		if task.Raw == nil {
			task.Raw = map[string]any{}
		}
		task.Raw["automation_deferred_replay"] = true
		// deferredAgain、runErr 用于本次流程后续判断的deferredAgain、runErr
		deferredAgain, runErr := s.center.handleTask(ctx, task)
		if deferredAgain {
			// handleTask 已按新的 paused_until 重置同一任务；当前 claim 不再删除。
			s.center.logger.Info("暂停期间自动化事件重放再次延期", "task_id", pending.ID, "account", task.AccountID, "trigger", task.TriggerType)
			continue
		}
		// finishErr 保存暂停事件重放终态的持久化错误；只有它成功后才记录重放成功或失败。
		finishErr := s.center.store.Automation.FinishDeferredTask(ctx, pending.ID, pending.ClaimVersion, runErr == nil, errorString(runErr))
		if finishErr != nil {
			s.center.logger.Warn("保存暂停事件重放结果失败", "task_id", pending.ID, "err", finishErr)
			s.notifyDeferredTaskNeedsReview(ctx, pending, task, "暂停事件重放后无法保存任务状态："+finishErr.Error())
			resultErr = errors.Join(resultErr, errAutomationNeedsReview, runErr, fmt.Errorf("保存暂停事件重放结果失败: %w", finishErr))
			continue
		}
		if runErr == nil {
			s.center.logger.Info("暂停期间自动化事件重放成功", "task_id", pending.ID, "account", task.AccountID, "trigger", task.TriggerType)
		} else {
			s.center.logger.Warn("暂停期间自动化事件重放失败", "task_id", pending.ID, "account", task.AccountID, "trigger", task.TriggerType, "err", runErr)
			if pending.ClaimVersion >= 5 {
				s.notifyDeferredTaskNeedsReview(ctx, pending, task, "暂停事件连续重放失败并已达到自动重试上限："+runErr.Error())
			}
		}
	}
	return resultErr
}

// notifyDeferredTaskNeedsReview 为进入死信或无法安全收口的延期自动化任务发送一次人工处理通知。
func (s *Scheduler) notifyDeferredTaskNeedsReview(ctx context.Context, pending db.DeferredAutomationTask, task Task, reason string) {
	if s == nil || s.center == nil {
		return
	}
	if task.AccountID == "" {
		task.AccountID = pending.CookieID
	}
	if task.TriggerType == "" {
		task.TriggerType = pending.TriggerType
	}
	// notificationKey 是同一延期任务共享的稳定人工处理通知键，重复扫描不会制造重复告警。
	notificationKey := fmt.Sprintf("manual-intervention:deferred-task:%d", pending.ID)
	// notifyCtx 保证任务状态写失败或原始重放预算取消后，告警仍有独立的短时入队预算。
	notifyCtx, notifyCancel := newAutomationRunCompensationContext(ctx)
	s.center.notifications.notifyManualIntervention(notifyCtx, task, "暂停自动化事件重放", reason, notificationKey)
	notifyCancel()
}

// errorString 封装错误String业务协调。
func errorString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// reviewRequestRuleDue 封装review请求规则Due业务协调。
func reviewRequestRuleDue(order db.Order, rule db.AutomationRule) bool {
	// cfg 用于本次流程后续判断的cfg
	cfg := parseReviewRuleConfig(rule.ConfigJSON)
	if cfg.MaxAttempts > 0 && order.ReviewRequestCount >= cfg.MaxAttempts {
		return false
	}
	// baseRaw 用于本次流程后续判断的base原始
	baseRaw := firstNonEmpty(order.ShippedAt, order.UpdatedAt, order.CreatedAt)
	// shippedAt 是订单发货时间基点，回溯窗口始终以它为准，与是否已求评过无关。
	shippedAt := parseDBTime(baseRaw)
	// 规则停用期间积压的旧订单在重新启用时不得被补发：发货已超出回溯窗口的订单一律跳过。
	if !shippedAt.IsZero() && time.Since(shippedAt) > time.Duration(cfg.effectiveLookbackHours())*time.Hour {
		return false
	}
	// waitHours 用于本次流程后续判断的waitHours
	waitHours := cfg.AfterShippedHours
	if order.ReviewRequestCount > 0 && strings.TrimSpace(order.LastReviewRequestAt) != "" {
		baseRaw = order.LastReviewRequestAt
		waitHours = cfg.RepeatIntervalHours
	}
	// base 用于本次流程后续判断的base
	base := parseDBTime(baseRaw)
	if base.IsZero() {
		return false
	}
	return time.Since(base) >= time.Duration(waitHours)*time.Hour
}

// reviewRuleConfig 用于本次流程后续判断的review规则配置
type reviewRuleConfig struct {
	AfterShippedHours   int
	RepeatIntervalHours int
	MaxAttempts         int
	// MaxLookbackHours 是回溯窗口小时数：发货时间早于该窗口的订单不再求评价；0 表示使用默认值。
	MaxLookbackHours int
}

// effectiveLookbackHours 返回实际生效的回溯窗口小时数。
//
// 未显式配置时取「走完全部求评流程所需时长 + 24 小时」，让正常流程不受影响，
// 同时过滤规则停用期间积压的历史订单；显式配置小于首次等待时长时上调到首次等待时长，
// 避免规则永远无法触发。
func (c reviewRuleConfig) effectiveLookbackHours() int {
	// minHours 是正常走完全部求评次数所需的最短时长。
	minHours := c.AfterShippedHours + (c.MaxAttempts-1)*c.RepeatIntervalHours
	if c.MaxLookbackHours <= 0 {
		return minHours + 24
	}
	if c.MaxLookbackHours < c.AfterShippedHours {
		return c.AfterShippedHours
	}
	return c.MaxLookbackHours
}

// parseReviewRuleConfig 封装parseReview规则配置业务协调。
func parseReviewRuleConfig(raw string) reviewRuleConfig {
	// cfg 用于本次流程后续判断的cfg
	cfg := reviewRuleConfig{AfterShippedHours: 72, RepeatIntervalHours: 24, MaxAttempts: 1}
	if strings.TrimSpace(raw) == "" {
		return cfg
	}
	// m 用于本次流程后续判断的m
	var m map[string]any
	if json.Unmarshal([]byte(raw), &m) != nil {
		return cfg
	}
	if // v 用于本次流程后续判断的v
	v := intFromAny(m["after_shipped_hours"]); v > 0 {
		cfg.AfterShippedHours = v
	}
	if // v 用于本次流程后续判断的v
	v := intFromAny(m["first_delay_hours"]); v > 0 {
		cfg.AfterShippedHours = v
	}
	if // v 用于本次流程后续判断的v
	v := intFromAny(m["repeat_interval_hours"]); v > 0 {
		cfg.RepeatIntervalHours = v
	}
	if // v 用于本次流程后续判断的v
	v := intFromAny(m["max_attempts"]); v > 0 {
		cfg.MaxAttempts = v
	}
	if // v 是显式配置的回溯窗口小时数，非正值保持默认
	v := intFromAny(m["max_lookback_hours"]); v > 0 {
		cfg.MaxLookbackHours = v
	}
	return cfg
}

// intFromAny 封装intFromAny业务协调。
func intFromAny(v any) int {
	switch // x 用于本次流程后续判断的x
	x := v.(type) {
	case float64:
		return int(x)
	case int:
		return x
	case string:
		// n 用于本次流程后续判断的n
		n, _ := strconv.Atoi(strings.TrimSpace(x))
		return n
	default:
		return 0
	}
}

// parseDBTime 封装parseDB时间业务协调。
func parseDBTime(s string) time.Time {
	// layout 表示当前遍历过程中的layout
	for _, layout := range []string{
		time.RFC3339Nano,
		"2006-01-02 15:04:05.999999999Z07:00", // Postgres TEXT(CURRENT_TIMESTAMP)
		"2006-01-02 15:04:05.999999999Z07",
		"2006-01-02 15:04:05Z07:00",
		"2006-01-02 15:04:05Z07",
		"2006-01-02 15:04:05", // SQLite/MySQL 历史值；按既有 UTC 约定解释
	} {
		if // t、err 用于本次流程后续判断的t、err
		t, err := time.ParseInLocation(layout, strings.TrimSpace(s), time.UTC); err == nil {
			return t
		}
	}
	return time.Time{}
}
