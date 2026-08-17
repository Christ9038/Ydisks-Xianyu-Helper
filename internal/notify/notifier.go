// Package notify 多渠道通知：dingtalk/feishu/lark/bark/webhook/wechat/telegram/email。
// 每个渠道解析 config JSON 后发送 HTTP 请求。
// email 用 SMTP（net/smtp）；其余为 HTTP POST。
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/logsafe"
	"xianyu-go/internal/netguard"
)

// EventAccountOffline 保存Event账号Offline，供当前处理流程使用
const (
	EventAccountOffline       = "account_offline"
	EventAccountRecovered     = "account_recovered"
	EventAccountDisabled      = "account_disabled"
	EventSecurityVerification = "security_verification"
	EventTokenRenewal         = "token_renewal"
	EventDeliveryResult       = "delivery_result"
	EventSystemError          = "system_error"
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

// newOutboundHTTPClient 保存newOutboundHTTPClient，供当前处理流程使用
var newOutboundHTTPClient = func() *http.Client { return netguard.PublicHTTPClient(10 * time.Second) }

// dialPublicSMTP 保存dialPublicSMTP，供当前处理流程使用
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
	_ = n.WaitContext(context.Background())
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
		ctx = context.Background()
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
// NotifyDelivery 负责Notify发货相关处理。
func (n *Notifier) NotifyDelivery(accountID, buyerName, buyerID, itemID, message, chatID string) {
	n.NotifyEvent(context.Background(), NotificationEvent{
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
// NotifyAccountAlert 负责Notify账号Alert相关处理。
func (n *Notifier) NotifyAccountAlert(accountID, level, title, body string) {
	n.NotifyAccountEvent(accountID, classifyAccountAlertEvent(title, body), level, title, body)
}

// NotifyAccountEvent 发送指定类型的账号通知。
func (n *Notifier) NotifyAccountEvent(accountID, eventType, level, title, body string) {
	n.NotifyEvent(context.Background(), NotificationEvent{
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
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	// channels、err 保存channels、err，供当前处理流程使用
	channels, err := n.repository.AccountChannels(ctx, ev.AccountID)
	if err != nil || len(channels) == 0 {
		return
	}
	// full 保存full，供当前处理流程使用
	full := formatEvent(ev)
	// eligible 保存eligible，供当前处理流程使用
	eligible := make([]db.NotificationChannel, 0, len(channels))
	// ch 表示当前遍历过程中的ch
	for _, ch := range channels {
		// allowed、err 保存allowed、err，供当前处理流程使用
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
		if // err 保存err，供当前处理流程使用
		err := n.repository.EnqueueOutbox(ctx, messages); err != nil {
			n.logger.Error("持久化通知失败", "event_type", ev.Type, "err", err)
		}
		return
	}
	// 未启动 worker 的独立使用场景保持同步行为，便于 CLI 和单元测试显式使用。
	for _, ch := range eligible {
		if // err 保存err，供当前处理流程使用
		err := n.send(ch, full); err != nil {
			n.logger.Error("发送通知失败", "channel", ch.Type, "event_type", ev.Type, "err", logsafe.Error(err))
		}
	}
}

// runOutbox 负责运行Outbox相关处理。
func (n *Notifier) runOutbox(ctx context.Context) {
	n.drainOutbox(ctx)
	// ticker 保存ticker，供当前处理流程使用
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

// drainOutbox 负责drainOutbox相关处理。
func (n *Notifier) drainOutbox(ctx context.Context) {
	// workerToken、err 保存工作器Token、err，供当前处理流程使用
	workerToken, err := notificationWorkerToken()
	if err != nil {
		n.logger.Error("生成通知 worker token 失败", "err", err)
		return
	}
	// messages、err 保存messages、err，供当前处理流程使用
	messages, err := n.repository.ClaimOutbox(ctx, workerToken, time.Now(), 20)
	if err != nil {
		if ctx.Err() == nil {
			n.logger.Warn("领取通知 outbox 失败", "err", err)
		}
		return
	}
	// message 表示当前遍历过程中的消息
	for _, message := range messages {
		// channel、getErr 保存channel、getErr，供当前处理流程使用
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
		if // sendErr 保存sendErr，供当前处理流程使用
		sendErr := n.send(*channel, message.Body); sendErr != nil {
			n.logger.Error("发送通知失败", "channel", channel.Type, "event_type", message.EventType, "attempt", message.AttemptCount, "err", logsafe.Error(sendErr))
			n.retryOutbox(ctx, message, workerToken, sendErr)
			continue
		}
		if // completed、completeErr 保存completed、completeErr，供当前处理流程使用
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

// retryOutbox 负责重试Outbox相关处理。
func (n *Notifier) retryOutbox(ctx context.Context, message db.NotificationOutboxMessage, workerToken string, cause error) {
	// permanent 保存permanent，供当前处理流程使用
	permanent := message.AttemptCount >= 10
	// shift 保存shift，供当前处理流程使用
	shift := min(max(message.AttemptCount-1, 0), 7)
	// delay 保存延迟，供当前处理流程使用
	delay := 5 * time.Second * time.Duration(1<<shift)
	// updated、err 保存updated、err，供当前处理流程使用
	updated, err := n.repository.RetryOutbox(ctx, message.ID, workerToken, cause.Error(), time.Now().Add(delay).Unix(), permanent)
	if err != nil {
		n.logger.Warn("更新通知重试状态失败", "outbox_id", message.ID, "err", err)
	} else if !updated {
		n.logger.Warn("通知重试状态未更新，租约可能已转移", "outbox_id", message.ID)
	}
}

// notificationWorkerToken 负责通知工作器令牌相关处理。
func notificationWorkerToken() (string, error) {
	// raw 保存原始，供当前处理流程使用
	raw := make([]byte, 16)
	if // err 保存err，供当前处理流程使用
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
	// ch、err 保存ch、err，供当前处理流程使用
	ch, err := n.repository.GetChannel(context.Background(), channelID)
	if err != nil {
		return fmt.Errorf("查询渠道失败: %w", err)
	}
	if ch == nil {
		return fmt.Errorf("渠道不存在")
	}
	return n.send(*ch, body)
}

// levelLabel 负责levelLabel相关处理。
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

// eventLabel 负责eventLabel相关处理。
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

// classifyAccountAlertEvent 负责classify账号AlertEvent相关处理。
func classifyAccountAlertEvent(title, body string) string {
	// msg 保存msg，供当前处理流程使用
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

// formatEvent 负责formatEvent相关处理。
func formatEvent(ev NotificationEvent) string {
	// b 保存b，供当前处理流程使用
	var b strings.Builder
	// label 保存label，供当前处理流程使用
	label := eventLabel(ev.Type)
	// level 保存level，供当前处理流程使用
	level := levelLabel(ev.Level)
	if level == "" {
		level = "提示"
	}
	// title 保存标题，供当前处理流程使用
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
		// keys 保存keys，供当前处理流程使用
		keys := make([]string, 0, len(ev.Fields))
		// k 表示当前遍历过程中的k
		for k := range ev.Fields {
			keys = append(keys, k)
		}
		sortStrings(keys)
		// k 表示当前遍历过程中的k
		for _, k := range keys {
			// v 保存v，供当前处理流程使用
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
	// body 保存请求体，供当前处理流程使用
	body := strings.TrimSpace(ev.Body)
	if body != "" {
		b.WriteString("\n\n")
		b.WriteString(body)
	}
	return b.String()
}

// eventAllowed 负责eventAllowed相关处理。
func eventAllowed(raw, eventType string) (bool, error) {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return true, nil
	}
	// events、err 保存events、err，供当前处理流程使用
	events, err := parseEventTypes(raw)
	if err != nil {
		return false, err
	}
	if len(events) == 0 {
		return true, nil
	}
	return events[eventType], nil
}

// parseEventTypes 负责parseEventTypes相关处理。
func parseEventTypes(raw string) (map[string]bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	// arr 保存arr，供当前处理流程使用
	var arr []string
	if strings.HasPrefix(raw, "[") {
		if // err 保存err，供当前处理流程使用
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
	// out 保存out，供当前处理流程使用
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

// sortStrings 负责sortStrings相关处理。
func sortStrings(values []string) {
	for // i 保存i，供当前处理流程使用
	i := 1; i < len(values); i++ {
		for // j 保存j，供当前处理流程使用
		j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}

// send 负责send相关处理。
func (n *Notifier) send(ch db.NotificationChannel, message string) error {
	// cfg 保存cfg，供当前处理流程使用
	cfg := parseConfig(ch.Config)
	switch ch.Type {
	case "ding_talk", "dingtalk":
		return n.sendDingTalk(cfg, message)
	case "feishu", "lark":
		return n.sendFeishu(cfg, message)
	case "bark":
		return n.sendBark(cfg, message)
	case "webhook":
		return n.sendWebhook(cfg, message)
	case "wechat":
		return n.sendWeChat(cfg, message)
	case "telegram":
		return n.sendTelegram(cfg, message)
	case "email":
		return n.sendEmail(cfg, message)
	case "qq":
		// QQ 渠道配置未标准化，跳过。
		return fmt.Errorf("qq 渠道暂不支持")
	default:
		return fmt.Errorf("不支持的通知渠道类型: %s", ch.Type)
	}
}

// parseConfig 解析 config JSON，失败时兼容旧格式 {"config": <raw>}。
func parseConfig(config string) map[string]any {
	if config == "" {
		return map[string]any{}
	}
	// m 保存m，供当前处理流程使用
	var m map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal([]byte(config), &m); err != nil {
		return map[string]any{"config": config}
	}
	return m
}

// ---- 钉钉 ----
func (n *Notifier) sendDingTalk(cfg map[string]any, message string) error {
	// webhook 保存webhook，供当前处理流程使用
	webhook := strOr(cfg, "webhook_url", strOr(cfg, "config", ""))
	// secret 保存secret，供当前处理流程使用
	secret := strOr(cfg, "secret", "")
	if webhook == "" {
		return fmt.Errorf("钉钉 webhook_url 为空")
	}
	if secret != "" {
		// ts 保存ts，供当前处理流程使用
		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
		// stringToSign 保存stringToSign，供当前处理流程使用
		stringToSign := ts + "\n" + secret
		// h 保存h，供当前处理流程使用
		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte(stringToSign))
		// sign 保存sign，供当前处理流程使用
		sign := base64.StdEncoding.EncodeToString(h.Sum(nil))
		// parsed、err 保存parsed、err，供当前处理流程使用
		parsed, err := url.Parse(webhook)
		if err != nil {
			return fmt.Errorf("钉钉 webhook 地址无效: %w", err)
		}
		// query 保存查询，供当前处理流程使用
		query := parsed.Query()
		query.Set("timestamp", ts)
		query.Set("sign", sign)
		parsed.RawQuery = query.Encode()
		webhook = parsed.String()
	}
	// payload 保存请求载荷，供当前处理流程使用
	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"title": "闲鱼自动回复通知",
			"text":  message,
		},
	}
	return n.postJSON(webhook, payload)
}

// ---- 飞书 ----
func (n *Notifier) sendFeishu(cfg map[string]any, message string) error {
	// webhook 保存webhook，供当前处理流程使用
	webhook := strOr(cfg, "webhook_url", "")
	// secret 保存secret，供当前处理流程使用
	secret := strOr(cfg, "secret", "")
	if webhook == "" {
		return fmt.Errorf("飞书 webhook_url 为空")
	}
	// ts 保存ts，供当前处理流程使用
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	// data 保存数据，供当前处理流程使用
	data := map[string]any{
		"msg_type":  "text",
		"content":   map[string]any{"text": message},
		"timestamp": ts,
	}
	if secret != "" {
		// stringToSign 保存stringToSign，供当前处理流程使用
		stringToSign := ts + "\n" + secret
		// h 保存h，供当前处理流程使用
		h := hmac.New(sha256.New, []byte(stringToSign))
		h.Write([]byte(""))
		data["sign"] = base64.StdEncoding.EncodeToString(h.Sum(nil))
	}
	return n.postJSON(webhook, data)
}

// ---- Bark ----
// sendBark 负责sendBark相关处理。
func (n *Notifier) sendBark(cfg map[string]any, message string) error {
	// server 保存server，供当前处理流程使用
	server := strings.TrimRight(strOr(cfg, "server_url", "https://api.day.app"), "/")
	// deviceKey 保存deviceKey，供当前处理流程使用
	deviceKey := strOr(cfg, "device_key", "")
	if deviceKey == "" {
		return fmt.Errorf("bark device_key 为空")
	}
	// data 保存数据，供当前处理流程使用
	data := map[string]any{
		"device_key": deviceKey,
		"title":      strOr(cfg, "title", "闲鱼自动回复通知"),
		"body":       message,
		"sound":      strOr(cfg, "sound", "default"),
		"group":      strOr(cfg, "group", "xianyu"),
	}
	if // icon 保存icon，供当前处理流程使用
	icon := strOr(cfg, "icon", ""); icon != "" {
		data["icon"] = icon
	}
	if // u 保存u，供当前处理流程使用
	u := strOr(cfg, "url", ""); u != "" {
		data["url"] = u
	}
	return n.postJSON(server+"/push", data)
}

// ---- Webhook ----
// sendWebhook 负责sendWebhook相关处理。
func (n *Notifier) sendWebhook(cfg map[string]any, message string) error {
	// webhook 保存webhook，供当前处理流程使用
	webhook := strOr(cfg, "webhook_url", "")
	if webhook == "" {
		return fmt.Errorf("webhook_url 为空")
	}
	// method 保存方法，供当前处理流程使用
	method := strings.ToUpper(strOr(cfg, "http_method", "POST"))
	// headers 保存headers，供当前处理流程使用
	headers := map[string]any{}
	if // h 保存h，供当前处理流程使用
	h := strOr(cfg, "headers", ""); h != "" {
		_ = json.Unmarshal([]byte(h), &headers)
	}
	// data 保存数据，供当前处理流程使用
	data := map[string]any{
		"message":   message,
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		"source":    "xianyu-auto-reply",
	}
	// body 保存请求体，供当前处理流程使用
	body, _ := json.Marshal(data)
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequest(method, webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// k、v 表示当前遍历过程中的k、v
	for k, v := range headers {
		req.Header.Set(k, fmt.Sprintf("%v", v))
	}
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := n.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if // err 保存err，供当前处理流程使用
	_, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 状态码 %d", resp.StatusCode)
	}
	return nil
}

// ---- 企业微信 ----
func (n *Notifier) sendWeChat(cfg map[string]any, message string) error {
	// webhook 保存webhook，供当前处理流程使用
	webhook := strOr(cfg, "webhook_url", "")
	if webhook == "" {
		return fmt.Errorf("微信 webhook_url 为空")
	}
	return n.postJSON(webhook, map[string]any{
		"msgtype": "text",
		"text":    map[string]any{"content": message},
	})
}

// ---- Telegram ----
// sendTelegram 负责sendTelegram相关处理。
func (n *Notifier) sendTelegram(cfg map[string]any, message string) error {
	// botToken 保存bot令牌，供当前处理流程使用
	botToken := strOr(cfg, "bot_token", "")
	// chatID 保存聊天ID，供当前处理流程使用
	chatID := strOr(cfg, "chat_id", "")
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram bot_token/chat_id 不完整")
	}
	// endpoint 保存endpoint，供当前处理流程使用
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	return n.postJSON(endpoint, map[string]any{
		"chat_id": chatID,
		"text":    message,
	})
}

// ---- 邮件 ----
func (n *Notifier) sendEmail(cfg map[string]any, message string) error {
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// server 保存server，供当前处理流程使用
	server := n.smtpConfigValue(ctx, cfg, "smtp_server", "")
	// port 保存port，供当前处理流程使用
	port := n.smtpConfigValue(ctx, cfg, "smtp_port", "587")
	// user 保存用户，供当前处理流程使用
	user := n.smtpConfigValue(ctx, cfg, "smtp_user", "")
	// pass 保存pass，供当前处理流程使用
	pass := n.smtpConfigValue(ctx, cfg, "smtp_password", "")
	// useTLS 保存useTLS，供当前处理流程使用
	useTLS := parseConfigBool(n.smtpConfigValue(ctx, cfg, "smtp_use_tls", "true"), true)
	// useSSL 保存useSSL，供当前处理流程使用
	useSSL := parseConfigBool(n.smtpConfigValue(ctx, cfg, "smtp_use_ssl", "false"), false)
	// fromAddress 保存fromAddress，供当前处理流程使用
	fromAddress := n.smtpConfigValue(ctx, cfg, "smtp_from_address", "")
	// fromName 保存from名称，供当前处理流程使用
	fromName := n.smtpConfigValue(ctx, cfg, "smtp_from_name", "")
	// legacyFrom 保存legacyFrom，供当前处理流程使用
	legacyFrom := n.smtpConfigValue(ctx, cfg, "smtp_from", "")
	// to 保存to，供当前处理流程使用
	to := strOr(cfg, "to_email", strOr(cfg, "email", ""))
	if server == "" || user == "" || to == "" {
		return fmt.Errorf("邮件配置不完整：请配置系统 SMTP 或在邮件渠道中覆盖 SMTP，并填写收件邮箱")
	}
	if legacyFrom != "" {
		if // parsed、err 保存parsed、err，供当前处理流程使用
		parsed, err := mail.ParseAddress(legacyFrom); err == nil && strings.Contains(parsed.Address, "@") {
			if fromAddress == "" {
				fromAddress = parsed.Address
			}
			if fromName == "" {
				fromName = parsed.Name
			}
		} else if fromName == "" {
			fromName = legacyFrom
		}
	}
	if fromAddress == "" {
		fromAddress = user
	}
	// from、err 保存from、err，供当前处理流程使用
	from, err := mail.ParseAddress(fromAddress)
	if err != nil || !strings.Contains(from.Address, "@") {
		return fmt.Errorf("发件邮箱地址无效")
	}
	// recipient、err 保存recipient、err，供当前处理流程使用
	recipient, err := mail.ParseAddress(to)
	if err != nil || !strings.Contains(recipient.Address, "@") {
		return fmt.Errorf("收件邮箱地址无效")
	}
	from.Name = fromName
	// fromHeader 保存fromHeader，供当前处理流程使用
	fromHeader := from.Address
	if from.Name != "" {
		fromHeader = from.String()
	}
	// toHeader 保存toHeader，供当前处理流程使用
	toHeader := recipient.Address
	if recipient.Name != "" {
		toHeader = recipient.String()
	}
	// addr 保存addr，供当前处理流程使用
	addr := server + ":" + port
	// auth 保存auth，供当前处理流程使用
	auth := smtp.PlainAuth("", user, pass, server)
	// msg 保存msg，供当前处理流程使用
	msg := strings.Join([]string{
		"From: " + fromHeader,
		"To: " + toHeader,
		"Subject: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("闲鱼自动发货通知")) + "?=",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		message,
	}, "\r\n")
	return sendPublicSMTP(ctx, addr, server, auth, from.Address, recipient.Address, []byte(msg), smtpTransportOptions{
		UseSTARTTLS:    useTLS && !useSSL,
		UseImplicitTLS: useSSL,
	})
}

// smtpTransportOptions 保存smtpTransportOptions，供当前处理流程使用
type smtpTransportOptions struct {
	UseSTARTTLS    bool
	UseImplicitTLS bool
}

// sendPublicSMTP 负责sendPublicSMTP相关处理。
func sendPublicSMTP(ctx context.Context, addr, server string, auth smtp.Auth, from, to string, message []byte, options ...smtpTransportOptions) error {
	// rawPort、err 保存原始Port、err，供当前处理流程使用
	_, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("SMTP 地址无效")
	}
	// port、err 保存port、err，供当前处理流程使用
	port, err := strconv.Atoi(strings.TrimSpace(rawPort))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("SMTP 端口无效")
	}
	// conn、err 保存conn、err，供当前处理流程使用
	conn, err := dialPublicSMTP(ctx, "tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	// transport 保存transport，供当前处理流程使用
	transport := smtpTransportOptions{UseSTARTTLS: true}
	if len(options) > 0 {
		transport = options[0]
	}
	if transport.UseImplicitTLS {
		// tlsConn 保存tlsConn，供当前处理流程使用
		tlsConn := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: server})
		if // err 保存err，供当前处理流程使用
		err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("SMTP SSL 握手失败: %w", err)
		}
		conn = tlsConn
	}
	// client、err 保存client、err，供当前处理流程使用
	client, err := smtp.NewClient(conn, server)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if transport.UseSTARTTLS {
		if // ok 保存ok，供当前处理流程使用
		ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP 服务器不支持要求的 STARTTLS")
		}
		if // err 保存err，供当前处理流程使用
		err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: server}); err != nil {
			return err
		}
	}
	if auth != nil {
		if // err 保存err，供当前处理流程使用
		err := client.Auth(auth); err != nil {
			return err
		}
	}
	if // err 保存err，供当前处理流程使用
	err := client.Mail(from); err != nil {
		return err
	}
	if // err 保存err，供当前处理流程使用
	err := client.Rcpt(to); err != nil {
		return err
	}
	// w、err 保存w、err，供当前处理流程使用
	w, err := client.Data()
	if err != nil {
		return err
	}
	if // err 保存err，供当前处理流程使用
	_, err := w.Write(message); err != nil {
		_ = w.Close()
		return err
	}
	if // err 保存err，供当前处理流程使用
	err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// parseConfigBool 负责parse配置Bool相关处理。
func parseConfigBool(raw string, fallback bool) bool {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return fallback
	}
}

// configOrSetting 负责配置Or设置相关处理。
func (n *Notifier) configOrSetting(ctx context.Context, cfg map[string]any, key, fallbackValue string) string {
	if // v 保存v，供当前处理流程使用
	v := strings.TrimSpace(strOr(cfg, key, "")); v != "" {
		return v
	}
	if n.repository != nil {
		if // v、err 保存v、err，供当前处理流程使用
		v, err := n.repository.GetSetting(ctx, key); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return fallbackValue
}

// smtpConfigValue keeps legacy per-field fallback behavior for existing rows,
// while new rows use an explicit all-system or all-channel SMTP mode.
// smtpConfigValue 负责smtp配置值相关处理。
func (n *Notifier) smtpConfigValue(ctx context.Context, cfg map[string]any, key, fallbackValue string) string {
	// modeValue、hasExplicitMode 保存模式Value、hasExplicit模式，供当前处理流程使用
	modeValue, hasExplicitMode := cfg["use_custom_smtp"]
	if !hasExplicitMode {
		return n.configOrSetting(ctx, cfg, key, fallbackValue)
	}
	if parseConfigBool(fmt.Sprintf("%v", modeValue), false) {
		if // value 保存值，供当前处理流程使用
		value := strings.TrimSpace(strOr(cfg, key, "")); value != "" {
			return value
		}
		return fallbackValue
	}
	if n.repository != nil {
		if // value、err 保存value、err，供当前处理流程使用
		value, err := n.repository.GetSetting(ctx, key); err == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return fallbackValue
}

// postJSON 通用 JSON POST。
func (n *Notifier) postJSON(url string, payload any) error {
	// body、err 保存body、err，供当前处理流程使用
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// req、err 保存req、err，供当前处理流程使用
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	// resp、err 保存resp、err，供当前处理流程使用
	resp, err := n.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// responseBody、err 保存响应Body、err，供当前处理流程使用
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("状态码 %d", resp.StatusCode)
	}
	if // err 保存err，供当前处理流程使用
	err := notificationBusinessError(responseBody); err != nil {
		return err
	}
	return nil
}

// notificationBusinessError 负责通知Business错误相关处理。
func notificationBusinessError(body []byte) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	// payload 保存请求载荷，供当前处理流程使用
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	// message 保存消息，供当前处理流程使用
	message := strings.TrimSpace(firstMapString(payload, "errmsg", "msg", "message", "description"))
	if // code、ok 保存code、ok，供当前处理流程使用
	code, ok := mapNumber(payload, "errcode"); ok && code != 0 {
		return fmt.Errorf("通知渠道返回错误 %.0f: %s", code, message)
	}
	if // code、ok 保存code、ok，供当前处理流程使用
	code, ok := mapNumber(payload, "StatusCode"); ok && code != 0 {
		return fmt.Errorf("通知渠道返回错误 %.0f: %s", code, message)
	}
	if // code、ok 保存code、ok，供当前处理流程使用
	code, ok := mapNumber(payload, "code"); ok && code != 0 && code != 200 {
		return fmt.Errorf("通知渠道返回错误 %.0f: %s", code, message)
	}
	if // okValue、exists 保存okValue、exists，供当前处理流程使用
	okValue, exists := payload["ok"].(bool); exists && !okValue {
		return fmt.Errorf("通知渠道返回失败: %s", message)
	}
	return nil
}

// mapNumber 负责mapNumber相关处理。
func mapNumber(payload map[string]any, key string) (float64, bool) {
	// value、ok 保存value、ok，供当前处理流程使用
	value, ok := payload[key]
	if !ok {
		return 0, false
	}
	switch // typed 保存typed，供当前处理流程使用
	typed := value.(type) {
	case float64:
		return typed, true
	case string:
		// number、err 保存number、err，供当前处理流程使用
		number, err := strconv.ParseFloat(typed, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

// firstMapString 负责firstMapString相关处理。
func firstMapString(payload map[string]any, keys ...string) string {
	// key 表示当前遍历过程中的key
	for _, key := range keys {
		if // value、ok 保存value、ok，供当前处理流程使用
		value, ok := payload[key].(string); ok && value != "" {
			return value
		}
	}
	return "未知错误"
}

// strOr 从 map 取字符串，缺失返回 fallback。
func strOr(m map[string]any, key, fallback string) string {
	if // v、ok 保存v、ok，供当前处理流程使用
	v, ok := m[key]; ok {
		switch // x 保存x，供当前处理流程使用
		x := v.(type) {
		case string:
			return x
		default:
			return fmt.Sprintf("%v", x)
		}
	}
	return fallback
}

// fallback 负责fallback相关处理。
func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
