package automation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

var (
	errAutomationDeferred    = errors.New("自动化动作已持久化等待执行")
	errAutomationNeedsReview = errors.New("自动化动作结果需要人工核对")
)

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

// OrderDetailFetcher 查询闲鱼订单详情。自动发货必须先拿到订单规格和购买数量，
// 再按规则里的规格映射发货。
type OrderDetailFetcher interface {
	FetchOrderDetail(ctx context.Context, cookieID, orderID, itemID, buyerID, cookieStr string) (*OrderDetail, error)
}

// Notifier 发货结果通知器（多渠道）。可选依赖，未注入时跳过通知。
type Notifier interface {
	NotifyDelivery(accountID, buyerName, buyerID, itemID, message, chatID string)
}

// Center 是统一自动化处理中心。
// 它只接收已经分类好的系统事件或计划任务，不处理用户消息；用户消息由 engine 的回复链处理。
type Center struct {
	store     *db.Store
	senders   SenderProvider
	fetcher   OrderDetailFetcher
	notifier  Notifier
	mtop      mtop.Client
	logger    *slog.Logger
	cookieSrc func(context.Context, string) (string, error)
	cardLocks sync.Map
}

// New 构造自动化中心。
func New(store *db.Store, senders SenderProvider, logger *slog.Logger) *Center {
	if logger == nil {
		logger = slog.Default()
	}
	return &Center{store: store, senders: senders, mtop: mtop.NewClient(), logger: logger.With("subsys", "automation")}
}

// SetMTop 注入 mtop 客户端（确认发货用）。未注入时使用默认 HTTP 实现。
func (c *Center) SetMTop(m mtop.Client) { c.mtop = m }

// SetOrderDetailFetcher 注入订单详情查询能力。
func (c *Center) SetOrderDetailFetcher(fetcher OrderDetailFetcher) {
	c.fetcher = fetcher
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
	c.markEventFacts(ctx, task)
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
	var rules []db.AutomationRule
	if resumeRunID := taskAutomationRunID(task); resumeRunID > 0 {
		run, runErr := c.store.Automation.GetRun(ctx, resumeRunID)
		if runErr != nil {
			return false, runErr
		}
		if run.Status != "running" {
			return false, nil
		}
		rule, ruleErr := c.store.Automation.Get(ctx, run.RuleID)
		if ruleErr != nil {
			return false, ruleErr
		}
		rules = []db.AutomationRule{*rule}
	} else {
		rules, err = c.store.Automation.Match(ctx, task.AccountID, task.ItemID, task.TriggerType)
		if err != nil {
			return false, err
		}
	}
	if len(rules) == 0 {
		c.logger.Info("无匹配自动化规则，忽略事件", "trigger", task.TriggerType, "order_id", task.OrderID, "item_id", task.ItemID)
		return false, nil
	}
	var firstErr error
	for _, rule := range rules {
		if err := c.executeRule(ctx, task, rule); err != nil {
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

func (c *Center) deferTask(ctx context.Context, task Task, dueAt int64) error {
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
		TriggerType: task.TriggerType, TaskJSON: string(raw), DueAt: dueAt,
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
	rules, err := c.store.Automation.Match(ctx, task.AccountID, task.ItemID, TriggerOrderPaid)
	if err != nil {
		return 0, err
	}
	if len(rules) == 0 {
		return 0, fmt.Errorf("未匹配到付款后自动发货规则")
	}
	sent := 0
	for _, rule := range rules {
		if !hasMatchingSendCard(task, rule.Actions) {
			continue
		}
		task.ActionPlan = runnableActions(task, immediateManualActions(rule.Actions))
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

func immediateManualActions(actions []db.AutomationAction) []db.AutomationAction {
	out := make([]db.AutomationAction, len(actions))
	copy(out, actions)
	for i := range out {
		out[i].DelaySeconds = 0
		if out[i].ActionType != ActionSendCard {
			continue
		}
		cfg := map[string]any{}
		_ = json.Unmarshal([]byte(out[i].ConfigJSON), &cfg)
		cfg["delay_override"] = true
		raw, _ := json.Marshal(cfg)
		out[i].ConfigJSON = string(raw)
	}
	return out
}

func (c *Center) executeRule(ctx context.Context, task Task, rule db.AutomationRule) error {
	var err error
	task, err = c.prepareTask(ctx, task)
	if err != nil {
		return err
	}
	triggerKey := buildTriggerKey(task)
	if triggerKey == "" {
		return nil
	}
	if len(task.ActionPlan) == 0 {
		task.ActionPlan = runnableActions(task, rule.Actions)
	}
	retryTask := task
	retryTask.CookieStr = ""
	rawJSON, _ := json.Marshal(retryTask)
	var run *db.AutomationRun
	if resumeID := taskAutomationRunID(task); resumeID > 0 {
		run, err = c.store.Automation.GetRun(ctx, resumeID)
		if err != nil {
			return err
		}
		if run.Status != "running" || run.RuleID != rule.ID {
			return nil
		}
	} else {
		var runID int64
		var started bool
		runID, started, err = c.store.Automation.TryStartRun(ctx, db.AutomationRun{
			RuleID: rule.ID, CookieID: task.AccountID, ItemID: task.ItemID, OrderID: task.OrderID,
			BuyerID: task.BuyerID, ChatID: task.ChatID, TriggerType: task.TriggerType,
			TriggerKey: triggerKey, RawEventJSON: string(rawJSON),
			LeaseExpiresAt: time.Now().UTC().Add(5 * time.Minute).Unix(),
		})
		if err != nil || !started {
			return err
		}
		run, err = c.store.Automation.GetRun(ctx, runID)
		if err != nil {
			return err
		}
	}
	status := "success"
	errMsg := ""
	sent := run.SentCount
	finish := true
	defer func() {
		if !finish {
			return
		}
		finishCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		if finishErr := c.store.Automation.FinishRun(finishCtx, run.ID, run.AttemptCount, status, sent, errMsg); finishErr != nil {
			c.logger.Error("保存自动化执行结果失败", "run_id", run.ID, "err", finishErr)
		}
		cancel()
		if c.notifier != nil {
			c.notifyResult(task, status, sent, errMsg)
		}
	}()
	actions := task.ActionPlan
	if task.TriggerType == TriggerOrderPaid && !hasMatchingSendCard(task, actions) {
		status, errMsg = "failed", "未匹配到订单规格对应的卡密动作"
		return errors.New(errMsg)
	}
	var deferred bool
	sent, deferred, err = c.executeRunActions(ctx, task, rule.ID, run, actions, false)
	if deferred {
		finish = false
		return errAutomationDeferred
	}
	if errors.Is(err, errAutomationNeedsReview) {
		finish = false
		if c.notifier != nil {
			c.notifyResult(task, "needs_review", sent, err.Error())
		}
		return err
	}
	if err != nil {
		if sent > 0 {
			reason := "运行已完成部分动作，后续动作失败，已禁止从头自动重放: " + err.Error()
			_ = c.store.Automation.QuarantineRunResult(ctx, run.ID, run.AttemptCount, sent, reason)
			finish = false
			if c.notifier != nil {
				c.notifyResult(task, "needs_review", sent, reason)
			}
			return fmt.Errorf("%w: %v", errAutomationNeedsReview, err)
		}
		status, errMsg = "failed", err.Error()
		return err
	}
	if task.TriggerType == TriggerReviewMissingTimeout && task.OrderID != "" {
		if incrementErr := c.store.Automation.IncrementReviewRequest(ctx, task.OrderID); incrementErr != nil {
			reason := "求评价消息已发送，但保存提醒次数失败，已停止自动重放: " + incrementErr.Error()
			_ = c.store.Automation.QuarantineRunResult(ctx, run.ID, run.AttemptCount, sent, reason)
			finish = false
			if c.notifier != nil {
				c.notifyResult(task, "needs_review", sent, reason)
			}
			return fmt.Errorf("%w: %v", errAutomationNeedsReview, incrementErr)
		}
	}
	return nil
}

func runnableActions(task Task, actions []db.AutomationAction) []db.AutomationAction {
	out := make([]db.AutomationAction, 0, len(actions))
	if task.TriggerType == TriggerOrderPaid {
		for _, action := range actions {
			if action.Enabled && action.ActionType == ActionSendCard && actionMatchesOrderSpec(task, action) {
				out = append(out, action)
			}
		}
		for _, action := range actions {
			if action.Enabled && action.ActionType == ActionConfirmShipment {
				out = append(out, action)
			}
		}
		return out
	}
	for _, action := range actions {
		if action.Enabled {
			out = append(out, action)
		}
	}
	return out
}

func (c *Center) executeRunActions(ctx context.Context, task Task, ruleID int64, run *db.AutomationRun, actions []db.AutomationAction, skipDelays bool) (int, bool, error) {
	sent := run.SentCount
	for cursor := run.ActionCursor; cursor < len(actions); cursor++ {
		action := actions[cursor]
		if !skipDelays {
			delaySeconds, err := c.actionDelaySeconds(ctx, action)
			if err != nil {
				return sent, false, err
			}
			if delaySeconds > 0 && taskDelayCursor(task) != cursor {
				if task.Raw == nil {
					task.Raw = map[string]any{}
				}
				task.Raw["automation_run_id"] = run.ID
				task.Raw["automation_rule_id"] = ruleID
				task.Raw["automation_delay_cursor"] = cursor
				dueAt := time.Now().UTC().Add(time.Duration(delaySeconds) * time.Second)
				if err := c.store.Automation.RenewRunLease(ctx, run.ID, run.AttemptCount, dueAt.Add(5*time.Minute).Unix()); err != nil {
					return sent, false, err
				}
				if err := c.deferTask(ctx, task, dueAt.Unix()); err != nil {
					return sent, false, err
				}
				return sent, true, nil
			}
		}
		started, err := c.store.Automation.StartRunAction(ctx, run.ID, run.AttemptCount, cursor, time.Now().UTC().Add(5*time.Minute).Unix())
		if err != nil || !started {
			if err == nil {
				err = errors.New("自动化动作已被其他 worker 领取")
			}
			return sent, false, err
		}
		n, actionErr := c.executeActionNow(ctx, task, action)
		if actionErr != nil {
			var uncertain *uncertainActionError
			if n > 0 || errors.As(actionErr, &uncertain) {
				reason := "外部动作可能已部分或全部执行，已禁止自动重放，请人工核对: " + actionErr.Error()
				_ = c.store.Automation.QuarantineRunResult(ctx, run.ID, run.AttemptCount, sent+n, reason)
				return sent + n, false, fmt.Errorf("%w: %v", errAutomationNeedsReview, actionErr)
			}
			_ = c.store.Automation.AbortRunAction(ctx, run.ID, run.AttemptCount, cursor)
			return sent, false, actionErr
		}
		if err := c.store.Automation.AdvanceRunAction(ctx, run.ID, run.AttemptCount, cursor, n); err != nil {
			_ = c.store.Automation.QuarantineRun(ctx, run.ID, run.AttemptCount, "动作已执行但检查点保存失败，请人工核对，禁止自动重放: "+err.Error())
			return sent + n, false, fmt.Errorf("%w: %v", errAutomationNeedsReview, err)
		}
		sent += n
		if task.Raw != nil {
			delete(task.Raw, "automation_delay_cursor")
		}
	}
	return sent, false, nil
}

func (c *Center) executeActionNow(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	allowed, err := c.accountAutomationAllowed(ctx, task.AccountID)
	if err != nil {
		return 0, err
	}
	if !allowed {
		return 0, fmt.Errorf("账号已暂停或停用，取消自动化动作")
	}
	return c.executeAction(ctx, task, action)
}

// notifyResult 根据规则执行结果发送通知。成功且实际发出了内容才通知，
// 避免无匹配动作的空跑刷屏；失败时发失败通知。
func (c *Center) notifyResult(task Task, status string, sent int, errMsg string) {
	triggerName := map[string]string{
		TriggerOrderPaid:            "付款发货",
		TriggerBuyerReviewed:        "评价赠品",
		TriggerReviewMissingTimeout: "求评价",
	}[task.TriggerType]
	if triggerName == "" {
		triggerName = task.TriggerType
	}
	if status == "success" {
		if sent <= 0 {
			return
		}
		msg := fmt.Sprintf("✅ %s成功（订单 %s，已发送 %d 条）", triggerName, task.OrderID, sent)
		c.notifier.NotifyDelivery(task.AccountID, "", task.BuyerID, task.ItemID, msg, task.ChatID)
		return
	}
	msg := fmt.Sprintf("🚨 %s失败（订单 %s）：%s", triggerName, task.OrderID, errMsg)
	c.notifier.NotifyDelivery(task.AccountID, "", task.BuyerID, task.ItemID, msg, task.ChatID)
}

func (c *Center) notifyRunNeedsReview(run db.AutomationRun, reason string) {
	if c == nil || c.notifier == nil {
		return
	}
	task := Task{AccountID: run.CookieID, BuyerID: run.BuyerID, ItemID: run.ItemID, ChatID: run.ChatID, OrderID: run.OrderID, TriggerType: run.TriggerType}
	c.notifyResult(task, "needs_review", run.SentCount, "需要人工核对："+reason)
}

func hasMatchingSendCard(task Task, actions []db.AutomationAction) bool {
	for _, action := range actions {
		if action.Enabled && action.ActionType == ActionSendCard && actionMatchesOrderSpec(task, action) {
			return true
		}
	}
	return false
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
	needsDetail := task.TriggerType == TriggerOrderPaid || task.TriggerType == TriggerBuyerReviewed
	if existing, err := c.store.Orders.Get(ctx, task.OrderID); err == nil && existing != nil {
		task = mergeOrderIntoTask(task, existing)
		if existing.Quantity == "" || existing.Amount == "" {
			needsDetail = true
		}
		// 规则是否多规格由 action.config_json 决定；这里无法提前知道命中的 action，
		// 因此交易类事件统一补齐规格，确保后续规格映射有事实依据。
		if existing.SpecName == "" || existing.SpecValue == "" {
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

func (c *Center) executeAction(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	switch action.ActionType {
	case ActionConfirmShipment:
		return 0, c.confirmShipment(ctx, task)
	case ActionSendCard:
		return c.sendCard(ctx, task, action)
	case ActionSendText:
		text := renderTemplate(action.MessageTemplate, task)
		if strings.TrimSpace(text) == "" {
			return 0, nil
		}
		if err := c.sendText(ctx, task, text); err != nil {
			return 0, uncertainAction(err)
		}
		return 1, nil
	default:
		return 0, fmt.Errorf("未知自动化动作: %s", action.ActionType)
	}
}

func (c *Center) confirmShipment(ctx context.Context, task Task) error {
	if task.OrderID == "" {
		return fmt.Errorf("确认发货缺少订单ID")
	}
	enabled, err := c.store.Cookies.GetAutoConfirm(ctx, task.AccountID)
	if err != nil {
		return fmt.Errorf("读取自动确认发货设置: %w", err)
	}
	if !enabled && !task.ForceConfirmShipment {
		return nil
	}
	credentialUnlock := c.store.LockAccountCredentials(task.AccountID)
	credentialLocked := true
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
	}()
	detail, err := c.store.Cookies.GetDetails(ctx, task.AccountID)
	if err != nil {
		return err
	}
	if detail == nil {
		return db.ErrNotFound
	}
	_, completeSnapshot := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if strings.TrimSpace(detail.Value) == "" && !completeSnapshot {
		return fmt.Errorf("账号 %s Cookie 为空", task.AccountID)
	}
	cookieStr := detail.Value
	var mtopCtx context.Context
	var cookieSession *mtop.CookieSession
	if snapshot, ok := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON); ok {
		mtopCtx, cookieSession = mtop.WithCookieSnapshot(ctx, snapshot)
	} else {
		mtopCtx, cookieSession = mtop.WithFlatCookieSession(ctx, cookieStr)
	}
	ok, ret, updated, callErr := c.mtop.ConsignContext(mtopCtx, cookieStr, task.OrderID)
	var persistenceErrs []error
	runtimeCookie := ""
	runtimeCookieChanged := false
	value, snapshot, changed := cookieSession.State()
	// 完整 Jar 已接管时，即使响应没有 Cookie 变化，也不能因扁平字符串
	// 格式差异回退覆盖并清除 Jar。
	sessionHandled := snapshot != nil
	if changed {
		metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
		if snapshot != nil {
			metadata = cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
		}
		if saveErr := c.store.Cookies.UpdateRenewalCookie(ctx, task.AccountID, value, metadata, time.Now().Unix()); saveErr != nil {
			persistenceErrs = append(persistenceErrs, fmt.Errorf("保存确认发货响应 Cookie Jar: %w", saveErr))
		} else if value != cookieStr {
			runtimeCookie = value
			runtimeCookieChanged = true
		}
	}
	if !sessionHandled && callErr == nil && updated != "" && updated != cookieStr {
		// 注入 mock 或没有权威快照的历史账号保留扁平
		// Cookie 兼容路径；扁平结果无法维护旧 Jar 的作用域，
		// 因此不得继续保留可能已过期的 snapshot。
		metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
		if saveErr := c.store.Cookies.UpdateRenewalCookie(ctx, task.AccountID, updated, metadata, time.Now().Unix()); saveErr != nil {
			persistenceErrs = append(persistenceErrs, fmt.Errorf("保存刷新后的 Cookie: %w", saveErr))
		} else {
			runtimeCookie = updated
			runtimeCookieChanged = true
		}
	}
	credentialUnlock()
	credentialLocked = false
	if runtimeCookieChanged && c.senders != nil {
		if sender, running := c.senders.Sender(task.AccountID); running {
			sender.UpdateCookie(runtimeCookie)
		}
	}
	if callErr != nil {
		if len(persistenceErrs) > 0 {
			callErr = errors.Join(callErr, errors.Join(persistenceErrs...))
		}
		return uncertainAction(callErr)
	}
	if !ok {
		failure := fmt.Errorf("确认发货失败: %s", strings.Join(ret, "; "))
		if len(persistenceErrs) > 0 {
			return errors.Join(failure, errors.Join(persistenceErrs...))
		}
		return failure
	}
	sysShip := true
	if upsertErr := c.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID:      task.AccountID,
		ItemID:        task.ItemID,
		BuyerID:       task.BuyerID,
		ChatID:        task.ChatID,
		OrderStatus:   "shipped",
		SystemShipped: &sysShip,
	}); upsertErr != nil {
		persistenceErrs = append(persistenceErrs, fmt.Errorf("保存本地订单发货状态: %w", upsertErr))
	}
	if eventErr := c.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "shipped_at"); eventErr != nil {
		persistenceErrs = append(persistenceErrs, fmt.Errorf("保存订单发货时间: %w", eventErr))
	}
	if len(persistenceErrs) > 0 {
		return uncertainAction(fmt.Errorf("闲鱼已确认发货，但本地状态保存失败: %w", errors.Join(persistenceErrs...)))
	}
	return nil
}

func (c *Center) sendCard(ctx context.Context, task Task, action db.AutomationAction) (int, error) {
	if !actionMatchesOrderSpec(task, action) {
		return 0, nil
	}
	if action.CardID <= 0 {
		return 0, fmt.Errorf("发送卡密动作缺少卡密组ID")
	}
	count := deliverySendCount(task, action)
	card, err := c.store.Cards.Get(ctx, action.CardID)
	if err != nil {
		return 0, err
	}
	if !card.Enabled {
		return 0, fmt.Errorf("卡密组 %d 已停用", card.ID)
	}
	if card.Type == "data" {
		return c.sendDataCard(ctx, task, card, count)
	}
	sent := 0
	for i := 0; i < count; i++ {
		content, imageURL, err := c.cardContent(ctx, card)
		if err != nil {
			return sent, err
		}
		if imageURL != "" {
			if err := c.sendImage(ctx, task, imageURL, card.ID); err != nil {
				return sent, uncertainAction(err)
			}
		}
		if strings.TrimSpace(content) != "" {
			if err := c.sendText(ctx, task, renderTemplate(content, task)); err != nil {
				return sent, uncertainAction(err)
			}
		}
		if strings.TrimSpace(content) == "" && strings.TrimSpace(imageURL) == "" {
			return sent, fmt.Errorf("卡密组 %d 没有可发送内容", card.ID)
		}
		sent++
	}
	return sent, nil
}

func (c *Center) sendDataCard(ctx context.Context, task Task, card *db.CardFull, count int) (int, error) {
	unlock := c.lockCard(card.ID)
	defer unlock()
	sent := 0
	for i := 0; i < count; i++ {
		content, err := c.store.Cards.ConsumeBatchData(ctx, card.ID)
		if err != nil {
			return sent, err
		}
		if strings.TrimSpace(content) != "" {
			if err := c.sendText(ctx, task, renderTemplate(content, task)); err != nil {
				// 发送接口报错时无法判断远端是否已经收到。恢复库存会让同一卡密
				// 再次被消费，因此保守地保留已消费状态并交给人工核对。
				return sent, uncertainAction(err)
			}
		}
		sent++
	}
	return sent, nil
}

func (c *Center) accountAutomationAllowed(ctx context.Context, accountID string) (bool, error) {
	paused, _, err := c.store.Cookies.IsPaused(ctx, accountID)
	if err != nil {
		return false, fmt.Errorf("读取账号暂停状态: %w", err)
	}
	enabled, err := c.store.Cookies.Status(ctx, accountID)
	if err != nil {
		return false, fmt.Errorf("读取账号启用状态: %w", err)
	}
	return !paused && enabled, nil
}

func (c *Center) lockCard(cardID int64) func() {
	raw, _ := c.cardLocks.LoadOrStore(cardID, &sync.Mutex{})
	mu := raw.(*sync.Mutex)
	mu.Lock()
	return mu.Unlock
}

func actionMatchesOrderSpec(task Task, action db.AutomationAction) bool {
	var cfg struct {
		SpecName  string `json:"spec_name"`
		SpecValue string `json:"spec_value"`
	}
	if json.Unmarshal([]byte(action.ConfigJSON), &cfg) != nil {
		return false
	}
	if strings.TrimSpace(cfg.SpecName) == "" && strings.TrimSpace(cfg.SpecValue) == "" {
		return true
	}
	return strings.TrimSpace(task.SpecName) == strings.TrimSpace(cfg.SpecName) &&
		strings.TrimSpace(task.SpecValue) == strings.TrimSpace(cfg.SpecValue)
}

func deliverySendCount(task Task, action db.AutomationAction) int {
	perUnit := action.DeliveryCount
	if perUnit <= 0 {
		perUnit = 1
	}
	qty := parsePositiveInt(task.Quantity)
	if qty <= 0 {
		qty = 1
	}
	return perUnit * qty
}

func parsePositiveInt(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0
	}
	return n
}

func (c *Center) cardContent(ctx context.Context, card *db.CardFull) (text, imageURL string, err error) {
	switch card.Type {
	case "text":
		if strings.TrimSpace(card.TextContent) == "" {
			return "", "", fmt.Errorf("文本卡密组缺少内容")
		}
		return card.TextContent, "", nil
	case "data":
		return "", "", fmt.Errorf("data 卡密必须通过 sendDataCard 发送")
	case "image":
		if strings.TrimSpace(card.ImageURL) == "" {
			return "", "", fmt.Errorf("图片卡密组缺少图片 URL")
		}
		return "", card.ImageURL, nil
	case "api":
		return "", "", fmt.Errorf("自动化中心暂不支持 API 卡密动作")
	default:
		return "", "", fmt.Errorf("未知卡密类型: %s", card.Type)
	}
}

func (c *Center) sendText(ctx context.Context, task Task, text string) error {
	if task.ChatID == "" || task.BuyerID == "" {
		return fmt.Errorf("发送消息缺少 chat_id 或 buyer_id")
	}
	if c.senders == nil {
		return fmt.Errorf("账号发送器未初始化")
	}
	sender, ok := c.senders.Sender(task.AccountID)
	if !ok {
		return fmt.Errorf("账号未在线，无法发送自动化消息")
	}
	return sender.SendText(ctx, task.ChatID, task.BuyerID, text)
}

func (c *Center) sendImage(ctx context.Context, task Task, imageURL string, cardID int64) error {
	if task.ChatID == "" || task.BuyerID == "" {
		return fmt.Errorf("发送图片缺少 chat_id 或 buyer_id")
	}
	if c.senders == nil {
		return fmt.Errorf("账号发送器未初始化")
	}
	sender, ok := c.senders.Sender(task.AccountID)
	if !ok {
		return fmt.Errorf("账号未在线，无法发送自动化图片")
	}
	return sender.SendImage(ctx, task.ChatID, task.BuyerID, imageURL, cardID)
}

func (c *Center) cookieValue(ctx context.Context, cookieID string) (string, error) {
	if c.cookieSrc != nil {
		return c.cookieSrc(ctx, cookieID)
	}
	d, err := c.store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		return "", err
	}
	return d.Value, nil
}

func (c *Center) markEventFacts(ctx context.Context, task Task) {
	if task.OrderID == "" {
		return
	}
	if err := c.store.Orders.Upsert(ctx, task.OrderID, db.OrderUpsertOpts{
		CookieID:    task.AccountID,
		ItemID:      task.ItemID,
		BuyerID:     task.BuyerID,
		ChatID:      task.ChatID,
		OrderStatus: task.OrderStatus,
		SpecName:    task.SpecName,
		SpecValue:   task.SpecValue,
		Quantity:    task.Quantity,
		Amount:      task.Amount,
	}); err != nil {
		c.logger.Warn("记录自动化事件事实前创建订单失败", "order_id", task.OrderID, "trigger", task.TriggerType, "err", err)
		return
	}
	switch task.TriggerType {
	case TriggerOrderPaid:
		_ = c.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "paid_at")
	case TriggerBuyerReviewed:
		_ = c.store.Automation.MarkOrderEventTime(ctx, task.OrderID, "buyer_reviewed_at")
	}
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

func renderTemplate(tpl string, task Task) string {
	out := tpl
	repl := map[string]string{
		"{order_id}":     task.OrderID,
		"{item_id}":      task.ItemID,
		"{buyer_id}":     task.BuyerID,
		"{chat_id}":      task.ChatID,
		"{trigger_type}": task.TriggerType,
		"{spec_name}":    task.SpecName,
		"{spec_value}":   task.SpecValue,
		"{quantity}":     task.Quantity,
		"{amount}":       task.Amount,
	}
	for k, v := range repl {
		out = strings.ReplaceAll(out, k, v)
	}
	return out
}
