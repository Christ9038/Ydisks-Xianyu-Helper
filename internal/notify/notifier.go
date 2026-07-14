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
	"xianyu-go/internal/netguard"
)

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
	cookieID string
	store    *db.Store
	logger   *slog.Logger
	httpc    *http.Client
	started  atomic.Bool
	workers  sync.WaitGroup
}

var newOutboundHTTPClient = func() *http.Client { return netguard.PublicHTTPClient(10 * time.Second) }
var dialPublicSMTP = netguard.DialPublicContext

// New 构造。
func New(cookieID string, store *db.Store, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{
		cookieID: cookieID,
		store:    store,
		logger:   logger.With("account", cookieID, "subsys", "notify"),
		httpc:    newOutboundHTTPClient(),
	}
}

// Start 启动持久化 outbox worker。调用返回前会先标记为异步模式，之后的业务
// 通知只写数据库，不在订单/账号处理调用栈中等待外部网络。
func (n *Notifier) Start(ctx context.Context) {
	if n == nil || n.store == nil || !n.started.CompareAndSwap(false, true) {
		return
	}
	n.workers.Add(1)
	go func() {
		defer n.workers.Done()
		n.runOutbox(ctx)
	}()
}

// Wait 等待 outbox worker 随生命周期 context 退出。
func (n *Notifier) Wait() { n.workers.Wait() }

// NotifyDelivery 发送发货结果通知。
// accountID 为 cookie_id。向该账号所有已启用渠道发送发货通知。
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

// NotifyAccountAlert 发送账号告警通知（token 失效/自动恢复失败/风控验证等）。
// level 取 AlertLevel* 常量。向该账号所有已启用渠道发送。
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
	if n.store == nil {
		return
	}
	if ev.Time.IsZero() {
		ev.Time = time.Now()
	}
	channels, err := n.store.Notifications.AccountChannels(ctx, ev.AccountID)
	if err != nil || len(channels) == 0 {
		return
	}
	full := formatEvent(ev)
	eligible := make([]db.NotificationChannel, 0, len(channels))
	for _, ch := range channels {
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
	if n.started.Load() {
		messages := make([]db.NotificationOutboxInput, 0, len(eligible))
		for _, ch := range eligible {
			messages = append(messages, db.NotificationOutboxInput{ChannelID: ch.ID, EventType: ev.Type, Body: full})
		}
		if err := n.store.Notifications.EnqueueOutbox(ctx, messages); err != nil {
			n.logger.Error("持久化通知失败", "event_type", ev.Type, "err", err)
		}
		return
	}
	// 未启动 worker 的独立使用场景保持同步行为，便于 CLI 和单元测试显式使用。
	for _, ch := range eligible {
		if err := n.send(ch, full); err != nil {
			n.logger.Error("发送通知失败", "channel", ch.Type, "event_type", ev.Type, "err", err)
		}
	}
}

func (n *Notifier) runOutbox(ctx context.Context) {
	n.drainOutbox(ctx)
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

func (n *Notifier) drainOutbox(ctx context.Context) {
	workerToken, err := notificationWorkerToken()
	if err != nil {
		n.logger.Error("生成通知 worker token 失败", "err", err)
		return
	}
	messages, err := n.store.Notifications.ClaimOutbox(ctx, workerToken, time.Now(), 20)
	if err != nil {
		if ctx.Err() == nil {
			n.logger.Warn("领取通知 outbox 失败", "err", err)
		}
		return
	}
	for _, message := range messages {
		channel, getErr := n.store.Notifications.GetChannel(ctx, message.ChannelID)
		if getErr != nil {
			n.retryOutbox(ctx, message, workerToken, getErr)
			continue
		}
		if channel == nil {
			_, _ = n.store.Notifications.CompleteOutbox(ctx, message.ID, workerToken)
			continue
		}
		if sendErr := n.send(*channel, message.Body); sendErr != nil {
			n.logger.Error("发送通知失败", "channel", channel.Type, "event_type", message.EventType, "attempt", message.AttemptCount, "err", sendErr)
			n.retryOutbox(ctx, message, workerToken, sendErr)
			continue
		}
		if completed, completeErr := n.store.Notifications.CompleteOutbox(ctx, message.ID, workerToken); completeErr != nil {
			n.logger.Warn("确认通知投递完成失败", "outbox_id", message.ID, "err", completeErr)
		} else if !completed {
			n.logger.Warn("通知 outbox 租约已转移", "outbox_id", message.ID)
		}
	}
}

func (n *Notifier) retryOutbox(ctx context.Context, message db.NotificationOutboxMessage, workerToken string, cause error) {
	permanent := message.AttemptCount >= 10
	shift := min(max(message.AttemptCount-1, 0), 7)
	delay := 5 * time.Second * time.Duration(1<<shift)
	updated, err := n.store.Notifications.RetryOutbox(ctx, message.ID, workerToken, cause.Error(), time.Now().Add(delay).Unix(), permanent)
	if err != nil {
		n.logger.Warn("更新通知重试状态失败", "outbox_id", message.ID, "err", err)
	} else if !updated {
		n.logger.Warn("通知重试状态未更新，租约可能已转移", "outbox_id", message.ID)
	}
}

func notificationWorkerToken() (string, error) {
	raw := make([]byte, 16)
	if _, err := rand.Read(raw); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw), nil
}

// SendToChannel 直接向指定渠道发送一条消息（用于前端“测试发送”）。
func (n *Notifier) SendToChannel(channelID int64, body string) error {
	if n.store == nil {
		return fmt.Errorf("通知器未初始化")
	}
	ch, err := n.store.Notifications.GetChannel(context.Background(), channelID)
	if err != nil {
		return fmt.Errorf("查询渠道失败: %w", err)
	}
	if ch == nil {
		return fmt.Errorf("渠道不存在")
	}
	return n.send(*ch, body)
}

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

func classifyAccountAlertEvent(title, body string) string {
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

func formatEvent(ev NotificationEvent) string {
	var b strings.Builder
	label := eventLabel(ev.Type)
	level := levelLabel(ev.Level)
	if level == "" {
		level = "提示"
	}
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
		keys := make([]string, 0, len(ev.Fields))
		for k := range ev.Fields {
			keys = append(keys, k)
		}
		sortStrings(keys)
		for _, k := range keys {
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
	body := strings.TrimSpace(ev.Body)
	if body != "" {
		b.WriteString("\n\n")
		b.WriteString(body)
	}
	return b.String()
}

func eventAllowed(raw, eventType string) (bool, error) {
	eventType = strings.TrimSpace(eventType)
	if eventType == "" {
		return true, nil
	}
	events, err := parseEventTypes(raw)
	if err != nil {
		return false, err
	}
	if len(events) == 0 {
		return true, nil
	}
	return events[eventType], nil
}

func parseEventTypes(raw string) (map[string]bool, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var arr []string
	if strings.HasPrefix(raw, "[") {
		if err := json.Unmarshal([]byte(raw), &arr); err != nil {
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
	out := make(map[string]bool, len(arr))
	for _, v := range arr {
		v = strings.TrimSpace(v)
		if v != "" {
			out[v] = true
		}
	}
	return out, nil
}

func sortStrings(values []string) {
	for i := 1; i < len(values); i++ {
		for j := i; j > 0 && values[j-1] > values[j]; j-- {
			values[j-1], values[j] = values[j], values[j-1]
		}
	}
}

func (n *Notifier) send(ch db.NotificationChannel, message string) error {
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
	var m map[string]any
	if err := json.Unmarshal([]byte(config), &m); err != nil {
		return map[string]any{"config": config}
	}
	return m
}

// ---- 钉钉 ----
func (n *Notifier) sendDingTalk(cfg map[string]any, message string) error {
	webhook := strOr(cfg, "webhook_url", strOr(cfg, "config", ""))
	secret := strOr(cfg, "secret", "")
	if webhook == "" {
		return fmt.Errorf("钉钉 webhook_url 为空")
	}
	if secret != "" {
		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
		stringToSign := ts + "\n" + secret
		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte(stringToSign))
		sign := base64.StdEncoding.EncodeToString(h.Sum(nil))
		parsed, err := url.Parse(webhook)
		if err != nil {
			return fmt.Errorf("钉钉 webhook 地址无效: %w", err)
		}
		query := parsed.Query()
		query.Set("timestamp", ts)
		query.Set("sign", sign)
		parsed.RawQuery = query.Encode()
		webhook = parsed.String()
	}
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
	webhook := strOr(cfg, "webhook_url", "")
	secret := strOr(cfg, "secret", "")
	if webhook == "" {
		return fmt.Errorf("飞书 webhook_url 为空")
	}
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	data := map[string]any{
		"msg_type":  "text",
		"content":   map[string]any{"text": message},
		"timestamp": ts,
	}
	if secret != "" {
		stringToSign := ts + "\n" + secret
		h := hmac.New(sha256.New, []byte(stringToSign))
		h.Write([]byte(""))
		data["sign"] = base64.StdEncoding.EncodeToString(h.Sum(nil))
	}
	return n.postJSON(webhook, data)
}

// ---- Bark ----
func (n *Notifier) sendBark(cfg map[string]any, message string) error {
	server := strings.TrimRight(strOr(cfg, "server_url", "https://api.day.app"), "/")
	deviceKey := strOr(cfg, "device_key", "")
	if deviceKey == "" {
		return fmt.Errorf("bark device_key 为空")
	}
	data := map[string]any{
		"device_key": deviceKey,
		"title":      strOr(cfg, "title", "闲鱼自动回复通知"),
		"body":       message,
		"sound":      strOr(cfg, "sound", "default"),
		"group":      strOr(cfg, "group", "xianyu"),
	}
	if icon := strOr(cfg, "icon", ""); icon != "" {
		data["icon"] = icon
	}
	if u := strOr(cfg, "url", ""); u != "" {
		data["url"] = u
	}
	return n.postJSON(server+"/push", data)
}

// ---- Webhook ----
func (n *Notifier) sendWebhook(cfg map[string]any, message string) error {
	webhook := strOr(cfg, "webhook_url", "")
	if webhook == "" {
		return fmt.Errorf("webhook_url 为空")
	}
	method := strings.ToUpper(strOr(cfg, "http_method", "POST"))
	headers := map[string]any{}
	if h := strOr(cfg, "headers", ""); h != "" {
		_ = json.Unmarshal([]byte(h), &headers)
	}
	data := map[string]any{
		"message":   message,
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		"source":    "xianyu-auto-reply",
	}
	body, _ := json.Marshal(data)
	req, err := http.NewRequest(method, webhook, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	for k, v := range headers {
		req.Header.Set(k, fmt.Sprintf("%v", v))
	}
	resp, err := n.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("webhook 状态码 %d", resp.StatusCode)
	}
	return nil
}

// ---- 企业微信 ----
func (n *Notifier) sendWeChat(cfg map[string]any, message string) error {
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
func (n *Notifier) sendTelegram(cfg map[string]any, message string) error {
	botToken := strOr(cfg, "bot_token", "")
	chatID := strOr(cfg, "chat_id", "")
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram bot_token/chat_id 不完整")
	}
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	return n.postJSON(endpoint, map[string]any{
		"chat_id": chatID,
		"text":    message,
	})
}

// ---- 邮件 ----
func (n *Notifier) sendEmail(cfg map[string]any, message string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	server := n.configOrSetting(ctx, cfg, "smtp_server", "")
	port := n.configOrSetting(ctx, cfg, "smtp_port", "587")
	user := n.configOrSetting(ctx, cfg, "smtp_user", "")
	pass := n.configOrSetting(ctx, cfg, "smtp_password", "")
	useTLS := parseConfigBool(n.configOrSetting(ctx, cfg, "smtp_use_tls", "true"), true)
	useSSL := parseConfigBool(n.configOrSetting(ctx, cfg, "smtp_use_ssl", "false"), false)
	fromAddress := n.configOrSetting(ctx, cfg, "smtp_from_address", "")
	fromName := n.configOrSetting(ctx, cfg, "smtp_from_name", "")
	legacyFrom := n.configOrSetting(ctx, cfg, "smtp_from", "")
	to := strOr(cfg, "to_email", strOr(cfg, "email", ""))
	if server == "" || user == "" || to == "" {
		return fmt.Errorf("邮件配置不完整：请配置系统 SMTP 或在邮件渠道中覆盖 SMTP，并填写收件邮箱")
	}
	if legacyFrom != "" {
		if parsed, err := mail.ParseAddress(legacyFrom); err == nil && strings.Contains(parsed.Address, "@") {
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
	from, err := mail.ParseAddress(fromAddress)
	if err != nil || !strings.Contains(from.Address, "@") {
		return fmt.Errorf("发件邮箱地址无效")
	}
	recipient, err := mail.ParseAddress(to)
	if err != nil || !strings.Contains(recipient.Address, "@") {
		return fmt.Errorf("收件邮箱地址无效")
	}
	from.Name = fromName
	fromHeader := from.Address
	if from.Name != "" {
		fromHeader = from.String()
	}
	toHeader := recipient.Address
	if recipient.Name != "" {
		toHeader = recipient.String()
	}
	addr := server + ":" + port
	auth := smtp.PlainAuth("", user, pass, server)
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

type smtpTransportOptions struct {
	UseSTARTTLS    bool
	UseImplicitTLS bool
}

func sendPublicSMTP(ctx context.Context, addr, server string, auth smtp.Auth, from, to string, message []byte, options ...smtpTransportOptions) error {
	_, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("SMTP 地址无效")
	}
	port, err := strconv.Atoi(strings.TrimSpace(rawPort))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("SMTP 端口无效")
	}
	conn, err := dialPublicSMTP(ctx, "tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	transport := smtpTransportOptions{UseSTARTTLS: true}
	if len(options) > 0 {
		transport = options[0]
	}
	if transport.UseImplicitTLS {
		tlsConn := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: server})
		if err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("SMTP SSL 握手失败: %w", err)
		}
		conn = tlsConn
	}
	client, err := smtp.NewClient(conn, server)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if transport.UseSTARTTLS {
		if ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP 服务器不支持要求的 STARTTLS")
		}
		if err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: server}); err != nil {
			return err
		}
	}
	if auth != nil {
		if err := client.Auth(auth); err != nil {
			return err
		}
	}
	if err := client.Mail(from); err != nil {
		return err
	}
	if err := client.Rcpt(to); err != nil {
		return err
	}
	w, err := client.Data()
	if err != nil {
		return err
	}
	if _, err := w.Write(message); err != nil {
		_ = w.Close()
		return err
	}
	if err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

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

func (n *Notifier) configOrSetting(ctx context.Context, cfg map[string]any, key, fallbackValue string) string {
	if v := strings.TrimSpace(strOr(cfg, key, "")); v != "" {
		return v
	}
	if n.store != nil && n.store.Settings != nil {
		if v, err := n.store.Settings.Get(ctx, key); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return fallbackValue
}

// postJSON 通用 JSON POST。
func (n *Notifier) postJSON(url string, payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(context.Background(), http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := n.httpc.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("状态码 %d", resp.StatusCode)
	}
	if err := notificationBusinessError(responseBody); err != nil {
		return err
	}
	return nil
}

func notificationBusinessError(body []byte) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	message := strings.TrimSpace(firstMapString(payload, "errmsg", "msg", "message", "description"))
	if code, ok := mapNumber(payload, "errcode"); ok && code != 0 {
		return fmt.Errorf("通知渠道返回错误 %.0f: %s", code, message)
	}
	if code, ok := mapNumber(payload, "StatusCode"); ok && code != 0 {
		return fmt.Errorf("通知渠道返回错误 %.0f: %s", code, message)
	}
	if code, ok := mapNumber(payload, "code"); ok && code != 0 && code != 200 {
		return fmt.Errorf("通知渠道返回错误 %.0f: %s", code, message)
	}
	if okValue, exists := payload["ok"].(bool); exists && !okValue {
		return fmt.Errorf("通知渠道返回失败: %s", message)
	}
	return nil
}

func mapNumber(payload map[string]any, key string) (float64, bool) {
	value, ok := payload[key]
	if !ok {
		return 0, false
	}
	switch typed := value.(type) {
	case float64:
		return typed, true
	case string:
		number, err := strconv.ParseFloat(typed, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

func firstMapString(payload map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := payload[key].(string); ok && value != "" {
			return value
		}
	}
	return "未知错误"
}

// strOr 从 map 取字符串，缺失返回 fallback。
func strOr(m map[string]any, key, fallback string) string {
	if v, ok := m[key]; ok {
		switch x := v.(type) {
		case string:
			return x
		default:
			return fmt.Sprintf("%v", x)
		}
	}
	return fallback
}

func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
