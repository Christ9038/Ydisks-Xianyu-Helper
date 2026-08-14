package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

var (
	errAutomationDeferred    = errors.New("自动化动作已持久化等待执行")
	errAutomationNeedsReview = errors.New("自动化动作结果需要人工核对")
	errAutomationQuarantine  = errors.New("自动化人工核对状态保存失败")
	errActionNotPerformed    = errors.New("自动化外部动作明确未执行")
	// ErrMessageNotSent 表示错误发生在调用 WebSocket 发送之前，可以安全重试。
	// engine 通过 errors.Is 标记“连接尚未就绪”等运行时状态，避免被误判为
	// 远端可能已经收到消息。
	ErrMessageNotSent = errors.New("自动化消息确定未发送")
)

type preparationError struct{ err error }

func (e *preparationError) Error() string { return e.err.Error() }
func (e *preparationError) Unwrap() error { return e.err }

type uncertainActionError struct{ err error }

func (e *uncertainActionError) Error() string { return e.err.Error() }
func (e *uncertainActionError) Unwrap() error { return e.err }

func uncertainAction(err error) error {
	if err == nil {
		return nil
	}
	var existing *uncertainActionError
	if errors.As(err, &existing) {
		return err
	}
	return &uncertainActionError{err: err}
}

// SenderProvider 根据账号 ID 提供当前在线账号的发送能力。
// 计划任务和 WS 事件都复用这个抽象，避免自动化中心直接依赖 account.Manager。
type SenderProvider interface {
	Sender(cookieID string) (MessageSender, bool)
}

// MessageSender 是自动化动作需要的最小发送接口。
type MessageSender interface {
	SendText(ctx context.Context, chatID, toUserID, text string) error
	SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64) error
	UpdateCookie(cookieStr string)
}

// automationReadySender 由能够报告实时连接状态的发送器选择性实现。
// 未实现的测试或外部发送器保持向后兼容，视为已经就绪。
type automationReadySender interface {
	AutomationReady() bool
}

// OrderDetailFetcher 查询闲鱼订单详情。自动发货必须先拿到订单规格和购买数量，
// 再按规则里的规格映射发货。
type OrderDetailFetcher interface {
	FetchOrderDetail(ctx context.Context, cookieID, orderID, itemID, buyerID, cookieStr string) (*OrderDetail, error)
}

// CredentialRecoverer 在平台明确返回 Session 失效时执行一次凭证恢复。
type CredentialRecoverer interface {
	RecoverExpiredCredential(ctx context.Context, cookieID string) bool
}

// Notifier 发货结果通知器（多渠道）。可选依赖，未注入时跳过通知。
type Notifier interface {
	NotifyDelivery(accountID, buyerName, buyerID, itemID, message, chatID string)
}

// Center 是统一自动化处理中心。
// 它只接收已经分类好的系统事件或计划任务，不处理用户消息；用户消息由 engine 的回复链处理。
type Center struct {
	// facts 记录已解析的自动化事件事实，不执行规则动作。
	facts eventFactRecorder
	// rules 查询适用规则，不执行规则动作。
	rules ruleMatcher
	// planner 生成不可变动作计划，不执行外部 I/O。
	planner actionPlanner
	// runs 协调自动化运行创建、动作检查点、延迟恢复和结果隔离。
	runs automationRunCoordinator
	// actions 执行确认发货、卡密和消息动作，集中维护外部副作用边界。
	actions automationActionExecutor
	// notifications 负责把运行结果转换为可选的发货通知。
	notifications deliveryNotifier
	// taskRunner 协调账号任务执行、凭证阻断和账号状态门禁。
	taskRunner        accountTaskCoordinator
	store             *db.Store
	senders           SenderProvider
	fetcher           OrderDetailFetcher
	recoverer         CredentialRecoverer
	notifier          Notifier
	mtop              mtop.Client
	accountTaskClient AccountTaskClient
	logger            *slog.Logger
	cookieSrc         func(context.Context, string) (string, error)
}

// New 构造自动化中心。
func New(store *db.Store, senders SenderProvider, logger *slog.Logger) *Center {
	if logger == nil {
		logger = slog.Default()
	}
	client := mtop.NewClient()
	center := &Center{
		facts:             newEventFactRecorder(store),
		rules:             newRuleMatcher(store),
		planner:           actionPlanner{},
		store:             store,
		senders:           senders,
		mtop:              client,
		accountTaskClient: client,
		logger:            logger.With("subsys", "automation"),
	}
	center.actions = automationActionExecutor{
		store:   store,
		senders: senders,
		mtop: func() mtop.Client {
			return center.mtop
		},
		recoverer: func() CredentialRecoverer {
			return center.recoverer
		},
		logger: center.logger,
		cookieSource: func(ctx context.Context, cookieID string) (string, error) {
			if center.cookieSrc != nil {
				return center.cookieSrc(ctx, cookieID)
			}
			return center.store.Cookies.GetValue(ctx, cookieID)
		},
		wakeCredentialBlocked: center.wakeCredentialBlockedAutomation,
	}
	center.notifications = deliveryNotifier{
		current: func() Notifier {
			return center.notifier
		},
	}
	center.taskRunner = accountTaskCoordinator{
		store:   store,
		client:  func() AccountTaskClient { return center.accountTaskClient },
		senders: senders,
		recoverer: func() CredentialRecoverer {
			return center.recoverer
		},
		logger: center.logger,
	}
	center.runs = automationRunCoordinator{
		store:                    store,
		planner:                  center.planner,
		logger:                   center.logger,
		prepareTask:              center.prepareTask,
		actionDelaySeconds:       center.actionDelaySeconds,
		accountAutomationAllowed: center.accountAutomationAllowed,
		deferTask:                center.deferTask,
		executeAction:            center.executeAction,
		hasNotifier:              func() bool { return center.notifier != nil },
		notifyResult:             center.notifyResult,
	}
	return center
}

// SetMTop 注入 mtop 客户端（确认发货用）。未注入时使用默认 HTTP 实现。
func (c *Center) SetMTop(m mtop.Client) { c.mtop = m }

// SetAccountTaskClient overrides automatic rating/polish transport for tests.
// SetAccountTaskClient 注入自动评价和商品擦亮的任务客户端。
func (c *Center) SetAccountTaskClient(client AccountTaskClient) { c.accountTaskClient = client }

// RunAccountTask 执行指定账号任务，并保持 Center 的公开兼容入口。
func (c *Center) RunAccountTask(ctx context.Context, accountID, taskType string) (AccountTaskSummary, error) {
	return c.taskRunner.runAccountTask(ctx, accountID, taskType)
}

// scanAccountTasks 扫描启用的账号任务，并委托给账号任务协调器。
func (c *Center) scanAccountTasks(ctx context.Context) {
	c.taskRunner.scanAccountTasks(ctx)
}

// SetOrderDetailFetcher 注入订单详情查询能力。
func (c *Center) SetOrderDetailFetcher(fetcher OrderDetailFetcher) {
	c.fetcher = fetcher
	if recoverer, ok := fetcher.(CredentialRecoverer); ok {
		c.recoverer = recoverer
	}
}

// SetNotifier 注入发货结果通知器。
func (c *Center) SetNotifier(n Notifier) {
	c.notifier = n
}

// SetCookieSource 覆盖 cookie 读取逻辑，便于测试。
func (c *Center) SetCookieSource(fn func(context.Context, string) (string, error)) {
	c.cookieSrc = fn
}

// HandleTask 处理一条自动化任务。无匹配规则时安全忽略。
func (c *Center) HandleTask(ctx context.Context, task Task) error {
	_, err := c.handleTask(ctx, task)
	return err
}

func (c *Center) handleTask(ctx context.Context, task Task) (bool, error) {
	if c == nil || c.store == nil || c.store.Automation == nil {
		return false, nil
	}
	task.TriggerType = strings.TrimSpace(task.TriggerType)
	if task.TriggerType == "" || task.AccountID == "" {
		return false, nil
	}
	if err := c.facts.record(ctx, task); err != nil {
		return false, err
	}
	if task.OrderID != "" {
		if order, orderErr := c.store.Orders.Get(ctx, task.OrderID); orderErr == nil && order != nil {
			task = mergeOrderIntoTask(task, order)
		} else if orderErr != nil && !errors.Is(orderErr, db.ErrNotFound) {
			return false, fmt.Errorf("读取自动化事件订单事实: %w", orderErr)
		}
	}
	paused, until, err := c.store.Cookies.IsPaused(ctx, task.AccountID)
	if err != nil {
		return false, err
	}
	if paused {
		if err := c.deferTask(ctx, task, until); err != nil {
			return false, err
		}
		c.logger.Info("账号已暂停，自动化事件已持久化等待恢复", "account", task.AccountID, "trigger", task.TriggerType, "due_at", until)
		return true, nil
	}
	enabled, err := c.store.Cookies.Status(ctx, task.AccountID)
	if err != nil {
		return false, err
	}
	if !enabled {
		c.logger.Info("账号已停用，记录事件事实但不执行自动化", "account", task.AccountID, "trigger", task.TriggerType)
		return false, nil
	}
	rules, err := c.rules.match(ctx, task)
	if err != nil {
		return false, err
	}
	if len(rules) == 0 {
		c.logger.Info("无匹配自动化规则，忽略事件", "trigger", task.TriggerType, "order_id", task.OrderID, "item_id", task.ItemID)
		return false, nil
	}
	var firstErr error
	for _, rule := range rules {
		if err := c.executeRule(ctx, task, rule); err != nil {
			var prepErr *preparationError
			if errors.As(err, &prepErr) && !isDeferredReplay(task) {
				dueAt := time.Now().UTC().Add(time.Minute).Unix()
				if deferErr := c.deferTaskWithError(ctx, task, dueAt, prepErr.Error()); deferErr != nil {
					return false, errors.Join(err, fmt.Errorf("持久化前置失败任务: %w", deferErr))
				}
				c.logger.Warn("自动化前置处理失败，任务已持久化等待恢复", "account", task.AccountID,
					"order_id", task.OrderID, "trigger", task.TriggerType, "retry_at", dueAt, "err", err)
				return true, nil
			}
			if errors.Is(err, errAutomationDeferred) {
				return true, nil
			}
			c.logger.Error("自动化规则执行失败", "rule_id", rule.ID, "trigger", task.TriggerType, "err", err)
			if firstErr == nil {
				firstErr = err
			}
		}
	}
	return false, firstErr
}

func taskAutomationRunID(task Task) int64 {
	if task.Raw == nil {
		return 0
	}
	value := fmt.Sprint(task.Raw["automation_run_id"])
	id, _ := strconv.ParseInt(value, 10, 64)
	return id
}

func taskDelayCursor(task Task) int {
	if task.Raw == nil {
		return -1
	}
	value := fmt.Sprint(task.Raw["automation_delay_cursor"])
	cursor, err := strconv.Atoi(value)
	if err != nil {
		return -1
	}
	return cursor
}

func isDeferredReplay(task Task) bool {
	return task.Raw != nil && task.Raw["automation_deferred_replay"] == true
}

func (c *Center) deferTask(ctx context.Context, task Task, dueAt int64) error {
	return c.deferTaskWithError(ctx, task, dueAt, "")
}

func (c *Center) deferTaskWithError(ctx context.Context, task Task, dueAt int64, errMsg string) error {
	key := buildTriggerKey(task)
	if key == "" {
		return fmt.Errorf("暂停期间的自动化事件缺少可持久化防重键")
	}
	task.CookieStr = ""
	raw, err := json.Marshal(task)
	if err != nil {
		return err
	}
	return c.store.Automation.DeferTask(ctx, db.DeferredAutomationTask{
		TaskKey: task.AccountID + ":" + key, CookieID: task.AccountID,
		TriggerType: task.TriggerType, TaskJSON: string(raw), DueAt: dueAt, ErrorMessage: errMsg,
	})
}

// ManualFullDelivery 对已存在订单执行完整发货，和付款系统事件共用同一套
// 订单详情补全、规格匹配、按购买数量发卡、确认发货逻辑。
func (c *Center) ManualFullDelivery(ctx context.Context, order *db.Order) (int, error) {
	if c == nil || c.store == nil || order == nil {
		return 0, fmt.Errorf("自动化中心未初始化或订单为空")
	}
	if strings.TrimSpace(order.OrderID) == "" {
		return 0, fmt.Errorf("订单缺少订单ID")
	}
	paused, until, err := c.store.Cookies.IsPaused(ctx, order.CookieID)
	if err != nil {
		return 0, fmt.Errorf("读取账号暂停状态: %w", err)
	}
	if paused {
		return 0, fmt.Errorf("账号暂停处理中，恢复时间 %d", until)
	}
	enabled, err := c.store.Cookies.Status(ctx, order.CookieID)
	if err != nil {
		return 0, fmt.Errorf("读取账号启用状态: %w", err)
	}
	if !enabled {
		return 0, fmt.Errorf("账号已停用，无法执行完整发货")
	}
	if strings.TrimSpace(order.CookieID) == "" {
		return 0, fmt.Errorf("订单缺少账号ID")
	}
	if strings.TrimSpace(order.ItemID) == "" {
		return 0, fmt.Errorf("订单缺少商品ID，无法匹配自动化规则")
	}
	if strings.TrimSpace(order.ChatID) == "" || strings.TrimSpace(order.BuyerID) == "" {
		return 0, fmt.Errorf("订单缺少 chat_id 或 buyer_id，无法发送卡券")
	}
	task := Task{
		Source:               "manual",
		AccountID:            order.CookieID,
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
		ForceConfirmShipment: true,
		Raw:                  map[string]any{"manual": true},
	}
	task, err = c.prepareTask(ctx, task)
	if err != nil {
		return 0, err
	}
	rules, err := c.rules.match(ctx, task)
	if err != nil {
		return 0, err
	}
	if len(rules) == 0 {
		return 0, fmt.Errorf("未匹配到付款后自动发货规则")
	}
	sent := 0
	for _, rule := range rules {
		if !c.planner.hasMatchingSendCard(task, rule.Actions) {
			continue
		}
		task.ActionPlan = c.planner.plan(task, c.planner.immediateManualActions(rule.Actions))
		rawTask := task
		rawTask.CookieStr = ""
		rawJSON, _ := json.Marshal(rawTask)
		runID, started, startErr := c.store.Automation.TryStartRun(ctx, db.AutomationRun{
			RuleID:         rule.ID,
			CookieID:       task.AccountID,
			ItemID:         task.ItemID,
			OrderID:        task.OrderID,
			BuyerID:        task.BuyerID,
			ChatID:         task.ChatID,
			TriggerType:    TriggerOrderPaid,
			TriggerKey:     buildTriggerKey(task),
			RawEventJSON:   string(rawJSON),
			LeaseExpiresAt: time.Now().UTC().Add(5 * time.Minute).Unix(),
		})
		if startErr != nil {
			return 0, startErr
		}
		if !started {
			return 0, fmt.Errorf("该订单已自动或手动执行过完整发货；如仅需补记状态，请选择仅修改发货状态")
		}
		run, getErr := c.store.Automation.GetRun(ctx, runID)
		if getErr != nil {
			return 0, getErr
		}
		n, deferred, err := c.executeRunActions(ctx, task, rule.ID, run, task.ActionPlan, true)
		if deferred {
			return n, errors.New("手动完整发货不应进入延迟队列")
		}
		sent = n
		if errors.Is(err, errAutomationNeedsReview) {
			return sent, err
		}
		status, errMsg := "success", ""
		if err != nil {
			status, errMsg = "failed", err.Error()
		}
		finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		finishErr := c.store.Automation.FinishRun(finishCtx, runID, run.AttemptCount, status, sent, errMsg)
		cancel()
		if finishErr != nil {
			return sent, fmt.Errorf("保存完整发货执行结果: %w", finishErr)
		}
		if err != nil {
			return sent, err
		}
		if n > 0 {
			break
		}
	}
	if sent == 0 {
		return 0, fmt.Errorf("未匹配到订单规格对应的卡密动作")
	}
	return sent, nil
}

// executeRule 将规则执行委托给运行协调器，保持 Center 的兼容调用入口。
func (c *Center) executeRule(ctx context.Context, task Task, rule db.AutomationRule) error {
	return c.runs.executeRule(ctx, task, rule)
}

// executeRunActions 将动作执行委托给运行协调器，保持手动发货等调用方的兼容入口。
func (c *Center) executeRunActions(ctx context.Context, task Task, ruleID int64, run *db.AutomationRun, actions []db.AutomationAction, skipDelays bool) (int, bool, error) {
	return c.runs.executeRunActions(ctx, task, ruleID, run, actions, skipDelays)
}

// notifyResult 根据规则执行结果发送通知。成功且实际发出了内容才通知，
// 避免无匹配动作的空跑刷屏；失败时发失败通知。
func (c *Center) notifyResult(task Task, status string, sent int, errMsg string) {
	c.notifications.notifyResult(task, status, sent, errMsg)
}

func (c *Center) notifyRunNeedsReview(run db.AutomationRun, reason string) {
	if c == nil {
		return
	}
	c.notifications.notifyRunNeedsReview(run, reason)
}

// actionDelaySeconds 统一卡密默认延时和动作覆盖语义。旧动作没有
// delay_override 字段时自动使用卡密上的默认延时。
func (c *Center) actionDelaySeconds(ctx context.Context, action db.AutomationAction) (int, error) {
	if action.ActionType != ActionSendCard || action.CardID <= 0 {
		return action.DelaySeconds, nil
	}
	var cfg struct {
		DelayOverride bool `json:"delay_override"`
	}
	_ = json.Unmarshal([]byte(action.ConfigJSON), &cfg)
	card, err := c.store.Cards.Get(ctx, action.CardID)
	if err != nil {
		return 0, err
	}
	if !card.Enabled {
		return 0, fmt.Errorf("卡密组 %d 已停用", card.ID)
	}
	if cfg.DelayOverride {
		return action.DelaySeconds, nil
	}
	return card.DelaySeconds, nil
}

func (c *Center) prepareTask(ctx context.Context, task Task) (Task, error) {
	if task.OrderID == "" {
		return task, nil
	}
	_ = c.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID: task.AccountID,
		ItemID:   task.ItemID,
		BuyerID:  task.BuyerID,
		ChatID:   task.ChatID,
	})
	needsDetail := task.TriggerType == TriggerOrderPaid
	if existing, err := c.store.Orders.Get(ctx, task.OrderID); err == nil && existing != nil {
		task = mergeOrderIntoTask(task, existing)
		if needsDetail && (existing.Quantity == "" || existing.Amount == "") {
			needsDetail = true
		}
		// 规则是否多规格由 action.config_json 决定；这里无法提前知道命中的 action，
		// 因此交易类事件统一补齐规格，确保后续规格映射有事实依据。
		if needsDetail && (existing.SpecName == "" || existing.SpecValue == "") {
			needsDetail = true
		}
	}
	if !needsDetail || c.fetcher == nil {
		return task, nil
	}
	cookieStr := task.CookieStr
	if strings.TrimSpace(cookieStr) == "" {
		var err error
		cookieStr, err = c.cookieValue(ctx, task.AccountID)
		if err != nil {
			return task, err
		}
	}
	detail, err := c.fetcher.FetchOrderDetail(ctx, task.AccountID, task.OrderID, task.ItemID, task.BuyerID, cookieStr)
	if err != nil {
		return task, err
	}
	if detail == nil {
		return task, nil
	}
	if detail.Quantity != "" {
		task.Quantity = detail.Quantity
	}
	if detail.SpecName != "" {
		task.SpecName = detail.SpecName
	}
	if detail.SpecValue != "" {
		task.SpecValue = detail.SpecValue
	}
	if detail.Amount != "" {
		task.Amount = detail.Amount
	}
	if detail.OrderStatus != "" {
		task.OrderStatus = detail.OrderStatus
	}
	_ = c.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID:    task.AccountID,
		ItemID:      task.ItemID,
		BuyerID:     task.BuyerID,
		ChatID:      task.ChatID,
		SpecName:    task.SpecName,
		SpecValue:   task.SpecValue,
		Quantity:    task.Quantity,
		Amount:      task.Amount,
		OrderStatus: task.OrderStatus,
	})
	return task, nil
}

func mergeOrderIntoTask(task Task, order *db.Order) Task {
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
	return task
}

// executeAction 将具体动作委托给发货动作执行器。
func (c *Center) executeAction(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	return c.actions.executeAction(ctx, task, action)
}

// confirmShipment 将确认发货委托给发货动作执行器。
func (c *Center) confirmShipment(ctx context.Context, task Task) error {
	return c.actions.confirmShipment(ctx, task)
}

// wakeCredentialBlockedAutomation 在 Cookie 更新后唤醒凭证阻塞的自动化任务。
func (c *Center) wakeCredentialBlockedAutomation(ctx context.Context, accountID string) {
	if c == nil || c.store == nil || c.store.Automation == nil {
		return
	}
	if err := c.store.Automation.WakeCredentialBlocked(ctx, accountID); err != nil {
		c.logger.Warn("Cookie 更新后唤醒自动化任务失败", "account", accountID, "err", err)
	}
}

// sendCard 将卡密发送委托给发货动作执行器。
func (c *Center) sendCard(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	return c.actions.sendCard(ctx, task, action)
}

// accountAutomationAllowed 判断账号是否仍允许执行自动化动作。
func (c *Center) accountAutomationAllowed(ctx context.Context, accountID string) (bool, error) {
	return c.taskRunner.accountAutomationAllowed(ctx, accountID)
}

// accountSenderReady 判断账号是否具备可发送自动化消息的在线连接。
func (c *Center) accountSenderReady(accountID string) bool {
	if c == nil || c.senders == nil {
		return false
	}
	sender, ok := c.senders.Sender(accountID)
	if !ok {
		return false
	}
	if ready, ok := sender.(automationReadySender); ok {
		return ready.AutomationReady()
	}
	return true
}

// cardContent 获取卡密组内容的兼容入口。
func (c *Center) cardContent(ctx context.Context, card *db.CardFull) (text, imageURL string, err error) {
	return c.actions.cardContent(ctx, card)
}

// sendImage 将图片消息发送委托给发货动作执行器。
func (c *Center) sendImage(ctx context.Context, task Task, imageURL string, cardID int64) error {
	return c.actions.sendImage(ctx, task, imageURL, cardID)
}

// cookieValue 读取账号 Cookie 的兼容入口。
func (c *Center) cookieValue(ctx context.Context, cookieID string) (string, error) {
	return c.actions.cookieValue(ctx, cookieID)
}

func buildTriggerKey(task Task) string {
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
		if attempt, ok := task.Raw["attempt"]; ok {
			return fmt.Sprintf("%s:%s:%v", task.TriggerType, task.OrderID, attempt)
		}
	}
	if task.OrderID != "" {
		return task.TriggerType + ":" + task.OrderID
	}
	if task.UpdateKey != "" {
		return task.TriggerType + ":" + task.UpdateKey
	}
	return ""
}
