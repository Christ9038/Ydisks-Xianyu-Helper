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
	EventSystemError          = "system_error"
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
var newOutboundHTTPClient = func() *http.Client { return netguard.PublicHTTPClient(10 * time.Second) }

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
		Type:      EventDeliveryResult,
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
	if n == nil || runID <= 0 || strings.TrimSpace(status) == "" {
		return
	}
	// idempotencyKey 绑定自动化运行主键与终态，避免恢复扫描或状态收口重试创建新 outbox 消息。
	idempotencyKey := fmt.Sprintf("automation-run:%d:%s", runID, strings.TrimSpace(status))
	n.notifyEvent(ctx, NotificationEvent{
		AccountID: accountID,
		Type:      EventDeliveryResult,
		Level:     "info",
		Title:     "自动化运行通知",
		Body:      message,
		Fields: map[string]string{
			"买家":   fmt.Sprintf("(ID: %s)", buyerID),
			"商品ID": itemID,
			"聊天ID": fallback(chatID, "未知"),
			"结果":   message,
		},
	}, idempotencyKey)
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
		return
	}
	if ctx == nil {
		n.logger.Warn("忽略缺少生命周期 Context 的通知入队请求")
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	// channels、err 用于本次流程后续判断的channels、err
	channels, err := n.repository.AccountChannels(ctx, ev.AccountID)
	if err != nil || len(channels) == 0 {
		return
	}
	// full 用于本次流程后续判断的full
	full := formatEvent(ev)
	// eligible 用于本次流程后续判断的eligible
	eligible := make([]db.NotificationChannel, 0, len(channels))
	// ch 表示当前遍历过程中的ch
	for _, ch := range channels {
		// allowed、err 用于本次流程后续判断的allowed、err
		allowed, err := eventAllowed(ch.EventTypes, ev.Type)
		if err != nil {
			n.logger.Warn("通知事件订阅配置无效，跳过渠道", "channel", ch.ID, "event_types", ch.EventTypes, "err", err)
			continue
		}
		if !allowed {
			continue
		}
		eligible = append(eligible, ch)
	}
	// 自动化运行终态必须先进入 outbox：即使 worker 尚未随应用生命周期启动，也不能回退到
	// 同步网络发送，否则会绕过业务幂等键和发送成功后的 uncertain 隔离。
	if n.started.Load() || strings.TrimSpace(idempotencyKey) != "" {
		// messages 保存本事件每个允许渠道的一条 outbox 记录，并共享同一个业务投递键。
		messages := make([]db.NotificationOutboxInput, 0, len(eligible))
		// ch 表示当前遍历过程中的ch
		for _, ch := range eligible {
			messages = append(messages, db.NotificationOutboxInput{ChannelID: ch.ID, EventType: ev.Type, Body: full, IdempotencyKey: idempotencyKey})
		}
		if // err 用于本次流程后续判断的err
		err := n.repository.EnqueueOutbox(ctx, messages); err != nil {
			n.logger.Error("持久化通知失败", "event_type", ev.Type, "err", err)
		}
		return
	}
	// 未启动 worker 的独立使用场景保持同步行为，便于 CLI 和单元测试显式使用。
	for _, ch := range eligible {
		if // err 用于本次流程后续判断的err
		err := n.send(ch, full); err != nil {
			n.logger.Error("发送通知失败", "channel", ch.Type, "event_type", ev.Type, "err", logsafe.Error(err))
		}
	}
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
			n.logger.Error("发送通知失败", "channel", channel.Type, "event_type", message.EventType, "attempt", message.AttemptCount, "err", logsafe.Error(sendErr))
			n.retryOutbox(ctx, message, workerToken, sendErr)
			continue
		}
		if // completed、completeErr 用于本次流程后续判断的completed、completeErr
		completed, completeErr := n.repository.CompleteOutbox(ctx, message.ID, workerToken); completeErr != nil {
			// uncertain、uncertainErr 保存发送成功后的隔离结果和隔离失败错误。
			uncertain, uncertainErr := n.repository.MarkOutboxUncertain(ctx, message.ID, workerToken, completeErr.Error())
			if uncertainErr != nil {
				n.logger.Error("确认通知投递完成失败且无法隔离消息", "outbox_id", message.ID, "err", logsafe.Error(errors.Join(completeErr, uncertainErr)))
			} else if !uncertain {
				n.logger.Warn("确认通知投递完成失败且租约已转移", "outbox_id", message.ID, "err", logsafe.Error(completeErr))
			} else {
				n.logger.Warn("通知已发送但本地确认失败，消息已隔离", "outbox_id", message.ID, "err", logsafe.Error(completeErr))
			}
		} else if !completed {
			n.logger.Warn("通知 outbox 租约已转移", "outbox_id", message.ID)
		}
	}
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
	updated, err := n.repository.RetryOutbox(ctx, message.ID, workerToken, cause.Error(), time.Now().Add(delay).Unix(), permanent)
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
	return events[eventType], nil
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
