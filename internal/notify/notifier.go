// Package notify 多渠道通知：dingtalk/feishu/lark/bark/webhook/wechat/telegram/email。
// 每个渠道解析 config JSON 后发送 HTTP 请求。
// email 用 SMTP（net/smtp）；其余为 HTTP POST。
package notify

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/netguard"
)

// EventAccountOffline 用于本次流程后续判断的Event账号Offline
const (
	EventAccountOffline       = "account_offline"
	EventAccountRecovered     = "account_recovered"
	EventAccountDisabled      = "account_disabled"
	EventSecurityVerification = "security_verification"
	EventTokenRenewal         = "token_renewal"
	EventDeliveryResult       = "delivery_result"
	// EventAutomationOrderCreated 表示“拍下改价”自动化任务的终态通知。
	EventAutomationOrderCreated = "automation_order_created"
	// EventAutomationOrderPaid 表示“付款发货”自动化任务的终态通知。
	EventAutomationOrderPaid = "automation_order_paid"
	// EventAutomationBuyerReviewed 表示“评价赠品”自动化任务的终态通知。
	EventAutomationBuyerReviewed = "automation_buyer_reviewed"
	// EventAutomationReviewMissingTimeout 表示“求评价”自动化任务的终态通知。
	EventAutomationReviewMissingTimeout = "automation_review_missing_timeout"
	// EventManualDeliveryResult 表示人工发货结果通知；它与四种自动化任务分开筛选。
	EventManualDeliveryResult = "manual_delivery_result"
	// EventManualInterventionRequired 表示自动化已停止且必须由用户人工判断或处理。
	EventManualInterventionRequired = "manual_intervention_required"
	EventSystemError                = "system_error"
	// legacyNotifierOperationTimeout 是兼容无 Context 通知与等待入口的最长数据库或网络预算。
	legacyNotifierOperationTimeout = 10 * time.Second
)

// NotificationEvent 是一条可被渠道订阅过滤的通知事件。
type NotificationEvent struct {
	AccountID string
	Type      string
	Level     string
	Title     string
	Body      string
	Fields    map[string]string
	Time      time.Time
	// SubscriptionFallbackTypes 保存兼容旧订阅配置的候选类别；只参与渠道过滤，不改变实际入队类别和通知展示。
	SubscriptionFallbackTypes []string
}

// Notifier 通知发送器。
type Notifier struct {
	cookieID   string
	repository Repository
	logger     *slog.Logger
	httpc      *http.Client
	started    atomic.Bool
	workers    sync.WaitGroup
	done       chan struct{}
}

// newOutboundHTTPClient 用于本次流程后续判断的newOutboundHTTPClient
var newOutboundHTTPClient = func() *http.Client { return netguard.ConfiguredHTTPClient(10 * time.Second) }

// dialPublicSMTP 用于本次流程后续判断的dialPublicSMTP
var dialPublicSMTP = netguard.DialPublicContext

// New 构造。
func New(cookieID string, store *db.Store, logger *slog.Logger) *Notifier {
	return NewWithRepository(cookieID, newStoreRepository(cookieID, store), logger)
}

// NewWithRepository 使用通知器所需的窄 repository 构造通知器。
func NewWithRepository(cookieID string, repository Repository, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{
		cookieID:   cookieID,
		repository: repository,
		logger:     logger.With("account", cookieID, "subsys", "notify"),
		httpc:      newOutboundHTTPClient(),
		done:       make(chan struct{}),
	}
}

// Start 启动持久化 outbox worker。调用返回前会先标记为异步模式，之后的业务
// 通知只写数据库，不在订单/账号处理调用栈中等待外部网络。
// Start 启动当前值。
func (n *Notifier) Start(ctx context.Context) {
	// nil Context 无法提供取消边界，拒绝启动以免创建无法回收的后台 worker。
	if ctx == nil {
		return
	}
	if n == nil || n.repository == nil || !n.started.CompareAndSwap(false, true) {
		return
	}
	n.workers.Add(1)
	go func() {
		defer n.workers.Done()
		defer close(n.done)
		n.runOutbox(ctx)
	}()
	if n.logger != nil {
		n.logger.Info("通知 outbox worker 已启动")
	}
}

// Wait 等待 outbox worker 随生命周期 context 退出，并兼容旧调用方。
func (n *Notifier) Wait() {
	// waitCtx、waitCancel 为兼容入口提供受限等待预算，避免异常 outbox worker 永久阻塞调用方。
	waitCtx, waitCancel := context.WithTimeout(context.Background(), legacyNotifierOperationTimeout)
	defer waitCancel()
	_ = n.WaitContext(waitCtx)
}

// WaitContext 在 ctx 约束内等待 outbox worker 退出。
func (n *Notifier) WaitContext(ctx context.Context) error {
	if n == nil {
		return nil
	}
	if !n.started.Load() || n.done == nil {
		return nil
	}
	if ctx == nil {
		return errors.New("等待通知 worker 需要关闭 Context")
	}
	select {
	case <-n.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// NotifyDelivery 发送发货结果通知。
// accountID 为 cookie_id。向该账号所有已启用渠道发送发货通知。
// NotifyDelivery 封装Notify发货业务协调。
func (n *Notifier) NotifyDelivery(accountID, buyerName, buyerID, itemID, message, chatID string) {
	// notificationCtx、notificationCancel 为兼容入口限制 outbox 入队预算，避免订单链路产生无主数据库操作。
	notificationCtx, notificationCancel := context.WithTimeout(context.Background(), legacyNotifierOperationTimeout)
	defer notificationCancel()
	n.NotifyEvent(notificationCtx, NotificationEvent{
		AccountID: accountID,
		Type:      EventManualDeliveryResult,
		Level:     "info",
		Title:     "自动发货通知",
		Body:      message,
		Fields: map[string]string{
			"买家":   fmt.Sprintf("%s (ID: %s)", buyerName, buyerID),
			"商品ID": itemID,
			"聊天ID": fallback(chatID, "未知"),
			"结果":   message,
		},
	})
}

// NotifyAutomationRun 将一个自动化运行的终态通知持久化到 outbox。
// runID 与 status 共同构成稳定幂等键：同一次恢复重复报告同一终态时不会重复入队；
// 状态改变时保留独立通知，便于人工核对后继续执行的运行报告最终结果。
func (n *Notifier) NotifyAutomationRun(ctx context.Context, runID int64, accountID, buyerID, itemID, status, message, chatID string) {
	n.notifyAutomationRun(ctx, EventDeliveryResult, runID, accountID, buyerID, itemID, status, message, chatID)
}

// NotifyAutomationRunForTrigger 将自动化运行终态按具体触发类别持久化到 outbox。
// triggerType 使用 automation 包的稳定触发编码；未知编码回退到旧交易事件，保证兼容调用方仍能收到通知。
func (n *Notifier) NotifyAutomationRunForTrigger(ctx context.Context, triggerType string, runID int64, accountID, buyerID, itemID, status, message, chatID string) {
	n.notifyAutomationRun(ctx, automationEventType(triggerType), runID, accountID, buyerID, itemID, status, message, chatID)
}

// notifyAutomationRun 统一处理自动化终态通知的输入校验、格式化和幂等入队。
func (n *Notifier) notifyAutomationRun(ctx context.Context, eventType string, runID int64, accountID, buyerID, itemID, status, message, chatID string) {
	// event、idempotencyKey、ok 保存构造出的通知事件、稳定幂等键及入参是否有效。
	event, idempotencyKey, ok := n.automationRunEvent(eventType, runID, accountID, buyerID, itemID, status, message, chatID)
	if !ok {
		return
	}
	n.notifyEvent(ctx, event, idempotencyKey)
}

// PrepareAutomationRunOutbox 只计算自动化运行终态通知应写入的 outbox 记录，不执行任何写入。
// 调用方（数据库仓储）必须在推进业务状态的同一事务内写入这些记录，使「状态变化」与「通知」要么同时生效、要么同时不生效。
// 没有启用渠道、订阅不匹配或入参无效时返回空切片；渠道查询失败返回错误，调用方应放弃本轮并稍后重试。
func (n *Notifier) PrepareAutomationRunOutbox(ctx context.Context, triggerType string, runID int64, accountID, buyerID, itemID, status, message, chatID string) ([]db.NotificationOutboxInput, error) {
	// event、idempotencyKey、ok 保存构造出的通知事件、稳定幂等键及入参是否有效。
	event, idempotencyKey, ok := n.automationRunEvent(automationEventType(triggerType), runID, accountID, buyerID, itemID, status, message, chatID)
	if !ok || n.repository == nil || ctx == nil {
		return nil, nil
	}
	if event.Time.IsZero() {
		event.Time = time.Now()
	}
	// eligible、err 保存订阅匹配的渠道及渠道查询错误。
	eligible, err := n.eligibleChannels(ctx, event)
	if err != nil {
		return nil, err
	}
	return buildOutboxMessages(eligible, event, idempotencyKey), nil
}

// automationRunEvent 校验输入并构造自动化终态通知事件及其稳定幂等键；ok 为 false 表示输入无效应被忽略。
func (n *Notifier) automationRunEvent(eventType string, runID int64, accountID, buyerID, itemID, status, message, chatID string) (event NotificationEvent, idempotencyKey string, ok bool) {
	if n == nil || runID <= 0 || strings.TrimSpace(status) == "" {
		if n != nil && n.logger != nil {
			n.logger.Warn("忽略无效的自动化终态通知", "run_id", runID, "account_id", accountID, "status", status)
		}
		return NotificationEvent{}, "", false
	}
	// idempotencyKey 绑定自动化运行主键与终态，避免恢复扫描或状态收口重试创建新 outbox 消息。
	idempotencyKey = fmt.Sprintf("automation-run:%d:%s", runID, strings.TrimSpace(status))
	// notificationType、notificationLevel、notificationTitle 和 fallbackTypes 共同区分普通终态与必须人工介入的终态。
	notificationType, notificationLevel, notificationTitle := eventType, "info", "自动化运行通知"
	// fallbackTypes 保存人工处理终态原本所属的自动化类别，兼容现有渠道订阅。
	var fallbackTypes []string
	if strings.TrimSpace(status) == "needs_review" {
		notificationType, notificationLevel, notificationTitle = EventManualInterventionRequired, "critical", "自动化需要人工处理"
		fallbackTypes = []string{eventType}
	}
	return NotificationEvent{
		AccountID: accountID,
		Type:      notificationType,
		Level:     notificationLevel,
		Title:     notificationTitle,
		Body:      message,
		Fields: map[string]string{
			"买家":   fmt.Sprintf("(ID: %s)", buyerID),
			"商品ID": itemID,
			"聊天ID": fallback(chatID, "未知"),
			"结果":   message,
		},
		SubscriptionFallbackTypes: fallbackTypes,
	}, idempotencyKey, true
}

// NotifyManualIntervention 将没有 automation_run 主键的自动化异常按稳定业务键写入人工处理通知。
// triggerType 标识原自动化类别，action 描述需要人工处理的环节；idempotencyKey 必须由订单和阶段等稳定事实构成，禁止使用随机值。
func (n *Notifier) NotifyManualIntervention(ctx context.Context, triggerType, accountID, orderID, itemID, buyerID, action, reason, chatID, idempotencyKey string) {
	if n == nil || strings.TrimSpace(accountID) == "" || strings.TrimSpace(idempotencyKey) == "" {
		return
	}
	// message 汇总人工处理环节与原因，避免用户只看到系统错误却不知道应检查哪个订单。
	message := fmt.Sprintf("%s（订单 %s）需要人工处理：%s", fallback(action, "自动化任务"), fallback(orderID, "未知"), fallback(reason, "执行结果无法确认"))
	// sourceEventType 保存该人工处理事件原本所属的自动化类别，兼容用户已有的细分类别订阅。
	sourceEventType := automationEventType(triggerType)
	n.notifyEvent(ctx, NotificationEvent{
		AccountID: accountID,
		Type:      EventManualInterventionRequired,
		Level:     "critical",
		Title:     "自动化需要人工处理",
		Body:      message,
		Fields: map[string]string{
			"订单ID": orderID,
			"商品ID": itemID,
			"买家ID": buyerID,
			"聊天ID": fallback(chatID, "未知"),
			"处理环节": fallback(action, "自动化任务"),
			"原因":   fallback(reason, "执行结果无法确认"),
		},
		SubscriptionFallbackTypes: []string{sourceEventType},
	}, strings.TrimSpace(idempotencyKey))
}

// automationEventType 将自动化触发编码转换为通知筛选编码。
func automationEventType(triggerType string) string {
	switch strings.TrimSpace(triggerType) {
	case "order_created":
		return EventAutomationOrderCreated
	case "order_paid", "bargain_pending":
		return EventAutomationOrderPaid
	case "buyer_reviewed":
		return EventAutomationBuyerReviewed
	case "review_missing_timeout":
		return EventAutomationReviewMissingTimeout
	default:
		return EventDeliveryResult
	}
}

// NotifyAccountAlert 发送账号告警通知（token 失效/自动恢复失败/风控验证等）。
// level 取 AlertLevel* 常量。向该账号所有已启用渠道发送。
// NotifyAccountAlert 封装Notify账号Alert业务协调。
func (n *Notifier) NotifyAccountAlert(accountID, level, title, body string) {
	n.NotifyAccountEvent(accountID, classifyAccountAlertEvent(title, body), level, title, body)
}

// NotifyAccountEvent 发送指定类型的账号通知。
func (n *Notifier) NotifyAccountEvent(accountID, eventType, level, title, body string) {
	// notificationCtx、notificationCancel 为兼容入口限制 outbox 入队预算，避免账号告警脱离调用生命周期。
	notificationCtx, notificationCancel := context.WithTimeout(context.Background(), legacyNotifierOperationTimeout)
	defer notificationCancel()
	n.NotifyEvent(notificationCtx, NotificationEvent{
		AccountID: accountID,
		Type:      eventType,
		Level:     level,
		Title:     title,
		Body:      body,
	})
}

// NotifyEvent 根据事件类型筛选渠道并发送通知。
func (n *Notifier) NotifyEvent(ctx context.Context, ev NotificationEvent) {
	n.notifyEvent(ctx, ev, "")
}

// notifyEvent 根据事件类型筛选渠道并发送通知；idempotencyKey 只用于 outbox 持久化去重，不能为空时必须来自稳定业务事实。
func (n *Notifier) notifyEvent(ctx context.Context, ev NotificationEvent, idempotencyKey string) {
	if n == nil || n.repository == nil {
		if n != nil && n.logger != nil {
			n.logger.Warn("通知入队跳过：通知仓储未初始化", "event_type", ev.Type, "account_id", ev.AccountID)
		}
		return
	}
	if ctx == nil {
		n.logger.Warn("忽略缺少生命周期 Context 的通知入队请求")
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	// full 用于本次流程后续判断的full
	full := formatEvent(ev)
	// eligible、err 保存订阅匹配的渠道及渠道查询错误。
	eligible, err := n.eligibleChannels(ctx, ev)
	if err != nil || len(eligible) == 0 {
		return
	}
	// 自动化运行终态必须先进入 outbox：即使 worker 尚未随应用生命周期启动，也不能回退到
	// 同步网络发送，否则会绕过业务幂等键和发送成功后的 uncertain 隔离。
	if n.started.Load() || strings.TrimSpace(idempotencyKey) != "" {
		// messages 保存本事件每个允许渠道的一条 outbox 记录，并共享同一个业务投递键。
		messages := buildOutboxMessages(eligible, ev, idempotencyKey)
		if // err 用于本次流程后续判断的err
		err := n.repository.EnqueueOutbox(ctx, messages); err != nil {
			n.logger.Error("持久化通知失败", "event_type", ev.Type, "account_id", ev.AccountID, "channel_count", len(eligible), "err", logsafe.Error(err))
		} else {
			n.logger.Info("自动化通知已写入 outbox", "event_type", ev.Type, "account_id", ev.AccountID, "channel_count", len(eligible), "idempotency_key", idempotencyKey)
		}
		return
	}
	// 未启动 worker 的独立使用场景保持同步行为，便于 CLI 和单元测试显式使用。
	for _, ch := range eligible {
		if // err 用于本次流程后续判断的err
		err := n.send(ch, full); err != nil {
			n.logger.Error("发送通知失败", "channel", ch.Type, "event_type", ev.Type, "err", logsafe.ExternalError(err))
		} else {
			n.logger.Info("通知已发送", "channel", ch.Type, "channel_id", ch.ID, "event_type", ev.Type)
		}
	}
}

// eligibleChannels 查询账号已启用渠道并按事件订阅过滤；无渠道或无匹配订阅返回空切片，仅渠道查询失败返回错误。
func (n *Notifier) eligibleChannels(ctx context.Context, ev NotificationEvent) ([]db.NotificationChannel, error) {
	// channels、err 保存账号渠道查询结果
	channels, err := n.repository.AccountChannels(ctx, ev.AccountID)
	if err != nil {
		n.logger.Error("查询通知渠道失败", "event_type", ev.Type, "account_id", ev.AccountID, "err", logsafe.Error(err))
		return nil, err
	}
	if len(channels) == 0 {
		n.logger.Info("通知事件未找到已启用渠道", "event_type", ev.Type, "account_id", ev.AccountID)
		return nil, nil
	}
	// eligible 用于本次流程后续判断的eligible
	eligible := make([]db.NotificationChannel, 0, len(channels))
	// ch 表示当前遍历过程中的ch
	for _, ch := range channels {
		// allowed、err 用于本次流程后续判断的allowed、err
		allowed, err := eventAllowedWithFallbacks(ch.EventTypes, ev.Type, ev.SubscriptionFallbackTypes)
		if err != nil {
			n.logger.Warn("通知事件订阅配置无效，跳过渠道", "channel", ch.ID, "event_types", ch.EventTypes, "err", err)
			continue
		}
		if !allowed {
			continue
		}
		eligible = append(eligible, ch)
	}
	if len(eligible) == 0 {
		n.logger.Info("通知事件没有匹配的订阅渠道", "event_type", ev.Type, "account_id", ev.AccountID, "channel_count", len(channels))
		return nil, nil
	}
	return eligible, nil
}

// buildOutboxMessages 为每个允许渠道生成一条共享业务投递键的 outbox 记录。
func buildOutboxMessages(eligible []db.NotificationChannel, ev NotificationEvent, idempotencyKey string) []db.NotificationOutboxInput {
	// full 是格式化后的通知正文
	full := formatEvent(ev)
	// messages 保存每个渠道的一条待写入记录
	messages := make([]db.NotificationOutboxInput, 0, len(eligible))
	// ch 是一个允许接收该事件的渠道。
	for _, ch := range eligible {
		messages = append(messages, db.NotificationOutboxInput{ChannelID: ch.ID, EventType: ev.Type, Body: full, IdempotencyKey: idempotencyKey})
	}
	return messages
}

// runOutbox 封装运行Outbox业务协调。
func (n *Notifier) runOutbox(ctx context.Context) {
	n.drainOutbox(ctx)
	// ticker 用于本次流程后续判断的ticker
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			n.drainOutbox(ctx)
		}
	}
}

// drainOutbox 封装drainOutbox业务协调。
func (n *Notifier) drainOutbox(ctx context.Context) {
	// workerToken、err 用于本次流程后续判断的工作器Token、err
	workerToken, err := notificationWorkerToken()
	if err != nil {
		n.logger.Error("生成通知 worker token 失败", "err", err)
		return
	}
	// messages、err 用于本次流程后续判断的messages、err
	messages, err := n.repository.ClaimOutbox(ctx, workerToken, time.Now(), 20)
	if err != nil {
		if ctx.Err() == nil {
			n.logger.Warn("领取通知 outbox 失败", "err", err)
		}
		return
	}
	// message 表示当前遍历过程中的消息
	for _, message := range messages {
		// channel、getErr 用于本次流程后续判断的channel、getErr
		channel, getErr := n.repository.GetChannel(ctx, message.ChannelID)
		if getErr != nil {
			n.retryOutbox(ctx, message, workerToken, getErr)
			continue
		}
		if channel == nil {
			// completed、completeErr 保存无效渠道消息的清理结果和数据库错误。
			completed, completeErr := n.repository.CompleteOutbox(ctx, message.ID, workerToken)
			if completeErr != nil {
				n.logger.Warn("清理无效通知渠道消息失败", "outbox_id", message.ID, "err", logsafe.Error(completeErr))
			} else if !completed {
				n.logger.Warn("清理无效通知渠道消息时租约已转移", "outbox_id", message.ID)
			}
			continue
		}
		if // sendErr 用于本次流程后续判断的sendErr
		sendErr := n.send(*channel, message.Body); sendErr != nil {
			n.logger.Error("发送通知失败", "channel", channel.Type, "event_type", message.EventType, "attempt", message.AttemptCount, "err", logsafe.ExternalError(sendErr))
			n.retryOutbox(ctx, message, workerToken, sendErr)
			continue
		}
		// completionCtx 脱离 worker 取消但保留有界超时，确保外部成功后本地确认仍有机会完成。
		completionCtx, completionCancel := notifierCompletionContext(ctx)
		if // completed、completeErr 用于本次流程后续判断的completed、completeErr
		completed, completeErr := n.repository.CompleteOutbox(completionCtx, message.ID, workerToken); completeErr != nil {
			completionCancel()
			// uncertain、uncertainErr 保存发送成功后的隔离结果和隔离失败错误。
			uncertainCtx, uncertainCancel := notifierCompletionContext(ctx)
			// uncertain 和 uncertainErr 保存不确定隔离写入结果及基础设施错误。
			uncertain, uncertainErr := n.repository.MarkOutboxUncertain(uncertainCtx, message.ID, workerToken, completeErr.Error())
			uncertainCancel()
			if uncertainErr != nil {
				n.logger.Error("确认通知投递完成失败且无法隔离消息", "outbox_id", message.ID, "err", logsafe.Error(errors.Join(completeErr, uncertainErr)))
			} else if !uncertain {
				n.logger.Warn("确认通知投递完成失败且租约已转移", "outbox_id", message.ID, "err", logsafe.Error(completeErr))
			} else {
				n.logger.Warn("通知已发送但本地确认失败，消息已隔离", "outbox_id", message.ID, "err", logsafe.Error(completeErr))
			}
		} else if !completed {
			completionCancel()
			n.logger.Warn("通知 outbox 租约已转移", "outbox_id", message.ID)
		} else {
			completionCancel()
			n.logger.Info("通知 outbox 发送成功", "outbox_id", message.ID, "channel_id", message.ChannelID, "channel", channel.Type, "event_type", message.EventType, "attempt", message.AttemptCount)
		}
	}
}

// notifierCompletionContext 为外部通知已经成功后的本地 outbox 收口创建独立的有界上下文。
func notifierCompletionContext(parent context.Context) (context.Context, context.CancelFunc) {
	// base 是脱离 worker 取消但仍受本地超时限制的上下文根；调用方必须传入 worker 上下文。
	base := context.WithoutCancel(parent)
	return context.WithTimeout(base, 5*time.Second)
}

// retryOutbox 封装重试Outbox业务协调。
func (n *Notifier) retryOutbox(ctx context.Context, message db.NotificationOutboxMessage, workerToken string, cause error) {
	// permanent 用于本次流程后续判断的permanent
	permanent := message.AttemptCount >= 10
	// shift 用于本次流程后续判断的shift
	shift := min(max(message.AttemptCount-1, 0), 7)
	// delay 用于本次流程后续判断的延迟
	delay := 5 * time.Second * time.Duration(1<<shift)
	// updated、err 用于本次流程后续判断的updated、err
	updated, err := n.repository.RetryOutbox(ctx, message.ID, workerToken, logsafe.ExternalError(cause), time.Now().Add(delay).Unix(), permanent)
	if err != nil {
		n.logger.Warn("更新通知重试状态失败", "outbox_id", message.ID, "err", err)
	} else if !updated {
		n.logger.Warn("通知重试状态未更新，租约可能已转移", "outbox_id", message.ID)
	}
}

// notificationWorkerToken 封装通知工作器令牌业务协调。
func notificationWorkerToken() (string, error) {
	// raw 用于本次流程后续判断的原始
	raw := make([]byte, 16)
	if // err 用于本次流程后续判断的err
	_, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// SendToChannel 直接向指定渠道发送一条消息（用于前端“测试发送”）。
func (n *Notifier) SendToChannel(channelID int64, body string) error {
	if n == nil || n.repository == nil {
		return fmt.Errorf("通知器未初始化")
	}
	// queryCtx、queryCancel 为手动测试发送创建有界渠道查询预算，避免 HTTP 请求取消后继续无界访问数据库。
	queryCtx, queryCancel := context.WithTimeout(context.Background(), legacyNotifierOperationTimeout)
	defer queryCancel()
	// ch、err 分别是渠道配置及其查询错误。
	ch, err := n.repository.GetChannel(queryCtx, channelID)
	if err != nil {
		return fmt.Errorf("查询渠道失败: %w", err)
	}
	if ch == nil {
		return fmt.Errorf("渠道不存在")
	}
	return n.send(*ch, body)
}

// levelLabel 封装levelLabel业务协调。
func levelLabel(level string) string {
	switch level {
	case "critical":
		return "严重"
	case "warn":
		return "警告"
	case "info":
		return "提示"
	default:
		return level
	}
}

// eventLabel 封装eventLabel业务协调。
func eventLabel(eventType string) string {
	switch eventType {
	case EventAccountOffline:
		return "掉线通知"
	case EventAccountRecovered:
		return "恢复通知"
	case EventAccountDisabled:
		return "禁用通知"
	case EventSecurityVerification:
		return "风控验证"
	case EventTokenRenewal:
		return "续期通知"
	case EventDeliveryResult:
		return "交易通知"
	case EventAutomationOrderCreated:
		return "拍下改价"
	case EventAutomationOrderPaid:
		return "付款发货"
	case EventAutomationBuyerReviewed:
		return "评价赠品"
	case EventAutomationReviewMissingTimeout:
		return "求评价"
	case EventManualDeliveryResult:
		return "手动发货结果"
	case EventManualInterventionRequired:
		return "需要人工处理"
	case EventSystemError:
		return "系统错误"
	default:
		if eventType == "" {
			return "通知"
		}
		return eventType
	}
}

// classifyAccountAlertEvent 封装classify账号AlertEvent业务协调。
func classifyAccountAlertEvent(title, body string) string {
	// msg 用于本次流程后续判断的msg
	msg := strings.ToLower(title + " " + body)
	switch {
	case strings.Contains(msg, "风控"), strings.Contains(msg, "验证"),
		strings.Contains(msg, "滑块"), strings.Contains(msg, "captcha"),
		strings.Contains(msg, "risk"), strings.Contains(msg, "x5sec"):
		return EventSecurityVerification
	case strings.Contains(msg, "禁用"), strings.Contains(msg, "disabled"):
		return EventAccountDisabled
	case strings.Contains(msg, "掉线"), strings.Contains(msg, "离线"),
		strings.Contains(msg, "offline"), strings.Contains(msg, "session"),
		strings.Contains(msg, "登录凭证已失效"):
		return EventAccountOffline
	case strings.Contains(msg, "token"), strings.Contains(msg, "续期"), strings.Contains(msg, "renew"):
		return EventTokenRenewal
	default:
		return EventSystemError
	}
}

// formatEvent 封装formatEvent业务协调。
func formatEvent(ev NotificationEvent) string {
	// b 用于本次流程后续判断的b
	var b strings.Builder
	// label 用于本次流程后续判断的label
	label := eventLabel(ev.Type)
	// level 用于本次流程后续判断的level
	level := levelLabel(ev.Level)
	if level == "" {
		level = "提示"
	}
	// title 用于本次流程后续判断的标题
	title := strings.TrimSpace(ev.Title)
	if title == "" {
		title = label
	}
	b.WriteString("[")
	b.WriteString(level)
	b.WriteString("] ")
	b.WriteString(title)
	b.WriteString("\n\n类型: ")
	b.WriteString(label)
	if ev.AccountID != "" {
		b.WriteString("\n账号: ")
		b.WriteString(ev.AccountID)
	}
	b.WriteString("\n时间: ")
	b.WriteString(ev.Time.Format("2006-01-02 15:04:05"))
	if len(ev.Fields) > 0 {
		// keys 用于本次流程后续判断的keys
		keys := make([]string, 0, len(ev.Fields))
		// k 表示当前遍历过程中的k
		for k := range ev.Fields {
			keys = append(keys, k)
		}
		sortStrings(keys)
		// k 表示当前遍历过程中的k
		for _, k := range keys {
			// v 用于本次流程后续判断的v
			v := strings.TrimSpace(ev.Fields[k])
			if v == "" {
				continue
			}
			b.WriteByte('\n')
			b.WriteString(k)
			b.WriteString(": ")
			b.WriteString(v)
		}
	}
	// body 用于本次流程后续判断的请求体
	body := strings.TrimSpace(ev.Body)
	if body != "" {
		b.WriteString("\n\n")
		b.WriteString(body)
	}
	return b.String()
}

// eventAllowed 封装eventAllowed业务协调。
func eventAllowed(raw, eventType string) (bool, error) {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return true, nil
	}
	// events、err 用于本次流程后续判断的events、err
	events, err := parseEventTypes(raw)
	if err != nil {
		return false, err
	}
	if len(events) == 0 {
		return true, nil
	}
	if events[eventType] {
		return true, nil
	}
	// delivery_result 是旧版统一交易开关；保留它对新四类自动化事件、人工发货结果和人工处理告警的兼容放行。
	if events[EventDeliveryResult] && (isAutomationEventType(eventType) || eventType == EventManualDeliveryResult || eventType == EventManualInterventionRequired) {
		return true, nil
	}
	return false, nil
}

// eventAllowedWithFallbacks 先匹配事件实际类别，再匹配来源类别，保证新增人工处理分类不会切断旧自动化订阅。
func eventAllowedWithFallbacks(raw, eventType string, fallbackTypes []string) (bool, error) {
	// allowed、err 保存实际事件类别的订阅判断结果。
	allowed, err := eventAllowed(raw, eventType)
	if err != nil || allowed {
		return allowed, err
	}
	// fallbackType 表示当前兼容检查的来源事件类别。
	for _, fallbackType := range fallbackTypes {
		allowed, err = eventAllowed(raw, fallbackType)
		if err != nil || allowed {
			return allowed, err
		}
	}
	return false, nil
}

// isAutomationEventType 判断事件是否属于四类可单独控制的自动化任务。
func isAutomationEventType(eventType string) bool {
	switch eventType {
	case EventAutomationOrderCreated, EventAutomationOrderPaid, EventAutomationBuyerReviewed, EventAutomationReviewMissingTimeout:
		return true
	default:
		return false
	}
}

// parseEventTypes 封装parseEventTypes业务协调。
func parseEventTypes(raw string) (map[string]bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	// arr 用于本次流程后续判断的arr
	var arr []string
	if strings.HasPrefix(raw, "[") {
		if // err 用于本次流程后续判断的err
		err := json.Unmarshal([]byte(raw), &arr); err != nil {
			return nil, err
		}
	} else {
		arr = strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == ';' || r == ' ' || r == '\n' || r == '\t'
		})
	}
	if len(arr) == 0 {
		return nil, nil
	}
	// out 用于本次流程后续判断的out
	out := make(map[string]bool, len(arr))
	// v 表示当前遍历过程中的v
	for _, v := range arr {
		v = strings.TrimSpace(v)
		if v != "" {
			out[v] = true
		}
	}
	return out, nil
}

// sortStrings 封装sortStrings业务协调。
func sortStrings(values []string) {
	for // i 用于本次流程后续判断的i
	i := 1; i < len(values); i++ {
		for // j 用于本次流程后续判断的j
		j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}

// send 封装send业务协调。
