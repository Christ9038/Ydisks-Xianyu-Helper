// Package notify 多渠道通知：dingtalk/feishu/lark/bark/webhook/wechat/telegram/email。
// 每个渠道解析 config JSON 后发送 HTTP 请求。
// email 用 SMTP（net/smtp）；其余为 HTTP POST。
package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/smtp"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
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
}

// New 构造。
func New(cookieID string, store *db.Store, logger *slog.Logger) *Notifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &Notifier{
		cookieID: cookieID,
		store:    store,
		logger:   logger.With("account", cookieID, "subsys", "notify"),
		httpc:    &http.Client{Timeout: 10 * time.Second},
	}
}

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
	for _, ch := range channels {
		allowed, err := eventAllowed(ch.EventTypes, ev.Type)
		if err != nil {
			n.logger.Warn("通知事件订阅配置无效，跳过渠道", "channel", ch.ID, "event_types", ch.EventTypes, "err", err)
			continue
		}
		if !allowed {
			continue
		}
		if err := n.send(ch, full); err != nil {
			n.logger.Error("发送通知失败", "channel", ch.Type, "event_type", ev.Type, "err", err)
		}
	}
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
		webhook += "&timestamp=" + ts + "&sign=" + sign
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
	url := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	return n.postJSON(url, map[string]any{
		"chat_id":    chatID,
		"text":       message,
		"parse_mode": "HTML",
	})
}

// ---- 邮件 ----
func (n *Notifier) sendEmail(cfg map[string]any, message string) error {
	ctx := context.Background()
	server := n.configOrSetting(ctx, cfg, "smtp_server", "")
	port := n.configOrSetting(ctx, cfg, "smtp_port", "587")
	user := n.configOrSetting(ctx, cfg, "smtp_user", "")
	pass := n.configOrSetting(ctx, cfg, "smtp_password", "")
	from := n.configOrSetting(ctx, cfg, "smtp_from", "")
	to := strOr(cfg, "to_email", strOr(cfg, "email", ""))
	if server == "" || user == "" || to == "" {
		return fmt.Errorf("邮件配置不完整：请配置系统 SMTP 或在邮件渠道中覆盖 SMTP，并填写收件邮箱")
	}
	if from == "" {
		from = user
	}
	addr := server + ":" + port
	auth := smtp.PlainAuth("", user, pass, server)
	msg := strings.Join([]string{
		"From: " + from,
		"To: " + to,
		"Subject: =?UTF-8?B?" + base64.StdEncoding.EncodeToString([]byte("闲鱼自动发货通知")) + "?=",
		"MIME-Version: 1.0",
		"Content-Type: text/plain; charset=UTF-8",
		"",
		message,
	}, "\r\n")
	return smtp.SendMail(addr, auth, from, []string{to}, []byte(msg))
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
	resp, err := n.httpc.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, resp.Body); err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("状态码 %d", resp.StatusCode)
	}
	return nil
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
