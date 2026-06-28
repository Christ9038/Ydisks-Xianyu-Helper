// Package notify 多渠道通知：dingtalk/feishu/lark/bark/webhook/wechat/telegram/email。
// 移植自 Python _send_*_notification 系列。每个渠道解析 config JSON 后 POST。
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

// Notifier 通知发送器（实现 engine.Notifier 接口）。
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

// NotifyDelivery 实现 engine.Notifier.NotifyDelivery。
// accountID 为 cookie_id。向该账号所有已启用渠道发送发货通知。
func (n *Notifier) NotifyDelivery(accountID, buyerName, buyerID, itemID, message, chatID string) {
	channels, err := n.store.Notifications.AccountChannels(context.Background(), accountID)
	if err != nil || len(channels) == 0 {
		return
	}
	// 复刻 Python 通知文案。
	body := fmt.Sprintf("🚨 自动发货通知\n\n账号: %s\n买家: %s (ID: %s)\n商品ID: %s\n聊天ID: %s\n结果: %s\n时间: %s\n\n请及时处理！",
		accountID, buyerName, buyerID, itemID, fallback(chatID, "未知"), message, time.Now().Format("2006-01-02 15:04:05"))
	for _, ch := range channels {
		if err := n.send(ch, body); err != nil {
			n.logger.Error("发送通知失败", "channel", ch.Type, "err", err)
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
		"msg_type": "text",
		"content":  map[string]any{"text": message},
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
	io.Copy(io.Discard, resp.Body)
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
	server := strOr(cfg, "smtp_server", "")
	port := strOr(cfg, "smtp_port", "587")
	user := strOr(cfg, "smtp_user", "")
	pass := strOr(cfg, "smtp_password", "")
	from := strOr(cfg, "smtp_from", "")
	to := strOr(cfg, "to_email", strOr(cfg, "email", ""))
	if server == "" || user == "" || to == "" {
		return fmt.Errorf("邮件配置不完整")
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
	io.Copy(io.Discard, resp.Body)
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
