package notify

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/tls"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/mail"
	"net/smtp"
	"net/url"
	"strconv"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/logsafe"
)

// send 根据渠道类型将已格式化通知发送到外部服务；渠道配置错误直接返回供 outbox 重试分类。
func (n *Notifier) send(ch db.NotificationChannel, message string) error {
	// cfg 用于本次流程后续判断的cfg
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
	// m 用于本次流程后续判断的m
	var m map[string]any
	if // err 用于本次流程后续判断的err
	err := json.Unmarshal([]byte(config), &m); err != nil {
		return map[string]any{"config": config}
	}
	return m
}

// ---- 钉钉 ----
func (n *Notifier) sendDingTalk(cfg map[string]any, message string) error {
	// webhook 用于本次流程后续判断的webhook
	webhook := strOr(cfg, "webhook_url", strOr(cfg, "config", ""))
	// secret 用于本次流程后续判断的secret
	secret := strOr(cfg, "secret", "")
	if webhook == "" {
		return fmt.Errorf("钉钉 webhook_url 为空")
	}
	if secret != "" {
		// ts 用于本次流程后续判断的ts
		ts := strconv.FormatInt(time.Now().UnixMilli(), 10)
		// stringToSign 用于本次流程后续判断的stringToSign
		stringToSign := ts + "\n" + secret
		// h 用于本次流程后续判断的h
		h := hmac.New(sha256.New, []byte(secret))
		h.Write([]byte(stringToSign))
		// sign 用于本次流程后续判断的sign
		sign := base64.StdEncoding.EncodeToString(h.Sum(nil))
		// parsed、err 用于本次流程后续判断的parsed、err
		parsed, err := url.Parse(webhook)
		if err != nil {
			return notificationRequestError(err)
		}
		// query 用于本次流程后续判断的查询
		query := parsed.Query()
		query.Set("timestamp", ts)
		query.Set("sign", sign)
		parsed.RawQuery = query.Encode()
		webhook = parsed.String()
	}
	// payload 用于本次流程后续判断的请求载荷
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
	// webhook 用于本次流程后续判断的webhook
	webhook := strOr(cfg, "webhook_url", "")
	// secret 用于本次流程后续判断的secret
	secret := strOr(cfg, "secret", "")
	if webhook == "" {
		return fmt.Errorf("飞书 webhook_url 为空")
	}
	// ts 用于本次流程后续判断的ts
	ts := strconv.FormatInt(time.Now().Unix(), 10)
	// data 用于本次流程后续判断的数据
	data := map[string]any{
		"msg_type":  "text",
		"content":   map[string]any{"text": message},
		"timestamp": ts,
	}
	if secret != "" {
		// stringToSign 用于本次流程后续判断的stringToSign
		stringToSign := ts + "\n" + secret
		// h 用于本次流程后续判断的h
		h := hmac.New(sha256.New, []byte(stringToSign))
		h.Write([]byte(""))
		data["sign"] = base64.StdEncoding.EncodeToString(h.Sum(nil))
	}
	return n.postJSON(webhook, data)
}

// ---- Bark ----
// sendBark 封装sendBark业务协调。
func (n *Notifier) sendBark(cfg map[string]any, message string) error {
	// server 用于本次流程后续判断的server
	server := strings.TrimRight(strOr(cfg, "server_url", "https://api.day.app"), "/")
	// deviceKey 用于本次流程后续判断的deviceKey
	deviceKey := strOr(cfg, "device_key", "")
	if deviceKey == "" {
		return fmt.Errorf("bark device_key 为空")
	}
	// data 用于本次流程后续判断的数据
	data := map[string]any{
		"device_key": deviceKey,
		"title":      strOr(cfg, "title", "闲鱼自动回复通知"),
		"body":       message,
		"sound":      strOr(cfg, "sound", "default"),
		"group":      strOr(cfg, "group", "xianyu"),
	}
	if // icon 用于本次流程后续判断的icon
	icon := strOr(cfg, "icon", ""); icon != "" {
		data["icon"] = icon
	}
	if // u 用于本次流程后续判断的u
	u := strOr(cfg, "url", ""); u != "" {
		data["url"] = u
	}
	return n.postJSON(server+"/push", data)
}

// ---- Webhook ----
// sendWebhook 封装sendWebhook业务协调。
func (n *Notifier) sendWebhook(cfg map[string]any, message string) error {
	// webhook 用于本次流程后续判断的webhook
	webhook := strOr(cfg, "webhook_url", "")
	if webhook == "" {
		return fmt.Errorf("webhook_url 为空")
	}
	// method 用于本次流程后续判断的方法
	method := strings.ToUpper(strOr(cfg, "http_method", "POST"))
	// headers 用于本次流程后续判断的headers
	headers := map[string]any{}
	if // h 用于本次流程后续判断的h
	h := strOr(cfg, "headers", ""); h != "" {
		_ = json.Unmarshal([]byte(h), &headers)
	}
	// data 用于本次流程后续判断的数据
	data := map[string]any{
		"message":   message,
		"timestamp": time.Now().Format("2006-01-02 15:04:05"),
		"source":    "xianyu-auto-reply",
	}
	// body 用于本次流程后续判断的请求体
	body, _ := json.Marshal(data)
	// req、err 用于本次流程后续判断的req、err
	req, err := http.NewRequest(method, webhook, bytes.NewReader(body))
	if err != nil {
		return notificationRequestError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	// k、v 表示当前遍历过程中的k、v
	for k, v := range headers {
		req.Header.Set(k, fmt.Sprintf("%v", v))
	}
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := n.httpc.Do(req)
	if err != nil {
		return notificationRequestError(err)
	}
	defer resp.Body.Close()
	if // err 用于本次流程后续判断的err
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
	// webhook 用于本次流程后续判断的webhook
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
// sendTelegram 封装sendTelegram业务协调。
func (n *Notifier) sendTelegram(cfg map[string]any, message string) error {
	// botToken 用于本次流程后续判断的bot令牌
	botToken := strOr(cfg, "bot_token", "")
	// chatID 用于本次流程后续判断的聊天ID
	chatID := strOr(cfg, "chat_id", "")
	if botToken == "" || chatID == "" {
		return fmt.Errorf("telegram bot_token/chat_id 不完整")
	}
	// endpoint 用于本次流程后续判断的endpoint
	endpoint := fmt.Sprintf("https://api.telegram.org/bot%s/sendMessage", botToken)
	return n.postJSON(endpoint, map[string]any{
		"chat_id": chatID,
		"text":    message,
	})
}

// ---- 邮件 ----
func (n *Notifier) sendEmail(cfg map[string]any, message string) error {
	// ctx、cancel 用于本次流程后续判断的ctx、cancel
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	// server 用于本次流程后续判断的server
	server := n.smtpConfigValue(ctx, cfg, "smtp_server", "")
	// port 用于本次流程后续判断的port
	port := n.smtpConfigValue(ctx, cfg, "smtp_port", "587")
	// user 用于本次流程后续判断的用户
	user := n.smtpConfigValue(ctx, cfg, "smtp_user", "")
	// pass 用于本次流程后续判断的pass
	pass := n.smtpConfigValue(ctx, cfg, "smtp_password", "")
	// useTLS 用于本次流程后续判断的useTLS
	useTLS := parseConfigBool(n.smtpConfigValue(ctx, cfg, "smtp_use_tls", "true"), true)
	// useSSL 用于本次流程后续判断的useSSL
	useSSL := parseConfigBool(n.smtpConfigValue(ctx, cfg, "smtp_use_ssl", "false"), false)
	// fromAddress 用于本次流程后续判断的fromAddress
	fromAddress := n.smtpConfigValue(ctx, cfg, "smtp_from_address", "")
	// fromName 用于本次流程后续判断的from名称
	fromName := n.smtpConfigValue(ctx, cfg, "smtp_from_name", "")
	// legacyFrom 用于本次流程后续判断的legacyFrom
	legacyFrom := n.smtpConfigValue(ctx, cfg, "smtp_from", "")
	// to 用于本次流程后续判断的to
	to := strOr(cfg, "to_email", strOr(cfg, "email", ""))
	if server == "" || user == "" || to == "" {
		return fmt.Errorf("邮件配置不完整：请配置系统 SMTP 或在邮件渠道中覆盖 SMTP，并填写收件邮箱")
	}
	if legacyFrom != "" {
		if // parsed、err 用于本次流程后续判断的parsed、err
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
	// from、err 用于本次流程后续判断的from、err
	from, err := mail.ParseAddress(fromAddress)
	if err != nil || !strings.Contains(from.Address, "@") {
		return fmt.Errorf("发件邮箱地址无效")
	}
	// recipient、err 用于本次流程后续判断的recipient、err
	recipient, err := mail.ParseAddress(to)
	if err != nil || !strings.Contains(recipient.Address, "@") {
		return fmt.Errorf("收件邮箱地址无效")
	}
	from.Name = fromName
	// fromHeader 用于本次流程后续判断的fromHeader
	fromHeader := from.Address
	if from.Name != "" {
		fromHeader = from.String()
	}
	// toHeader 用于本次流程后续判断的toHeader
	toHeader := recipient.Address
	if recipient.Name != "" {
		toHeader = recipient.String()
	}
	// addr 用于本次流程后续判断的addr
	addr := server + ":" + port
	// auth 用于本次流程后续判断的auth
	auth := smtp.PlainAuth("", user, pass, server)
	// msg 用于本次流程后续判断的msg
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

// smtpTransportOptions 用于本次流程后续判断的smtpTransportOptions
type smtpTransportOptions struct {
	UseSTARTTLS    bool
	UseImplicitTLS bool
}

// sendPublicSMTP 封装sendPublicSMTP业务协调。
func sendPublicSMTP(ctx context.Context, addr, server string, auth smtp.Auth, from, to string, message []byte, options ...smtpTransportOptions) error {
	// rawPort、err 用于本次流程后续判断的原始Port、err
	_, rawPort, err := net.SplitHostPort(addr)
	if err != nil {
		return fmt.Errorf("SMTP 地址无效")
	}
	// port、err 用于本次流程后续判断的port、err
	port, err := strconv.Atoi(strings.TrimSpace(rawPort))
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("SMTP 端口无效")
	}
	// conn、err 用于本次流程后续判断的conn、err
	conn, err := dialPublicSMTP(ctx, "tcp", addr, 10*time.Second)
	if err != nil {
		return fmt.Errorf("连接 SMTP 服务器失败: %w", err)
	}
	defer func() { _ = conn.Close() }()
	_ = conn.SetDeadline(time.Now().Add(20 * time.Second))
	// transport 用于本次流程后续判断的transport
	transport := smtpTransportOptions{UseSTARTTLS: true}
	if len(options) > 0 {
		transport = options[0]
	}
	if transport.UseImplicitTLS {
		// tlsConn 用于本次流程后续判断的tlsConn
		tlsConn := tls.Client(conn, &tls.Config{MinVersion: tls.VersionTLS12, ServerName: server})
		if // err 用于本次流程后续判断的err
		err := tlsConn.HandshakeContext(ctx); err != nil {
			return fmt.Errorf("SMTP SSL 握手失败: %w", err)
		}
		conn = tlsConn
	}
	// client、err 用于本次流程后续判断的client、err
	client, err := smtp.NewClient(conn, server)
	if err != nil {
		return err
	}
	defer func() { _ = client.Close() }()
	if transport.UseSTARTTLS {
		if // ok 用于本次流程后续判断的ok
		ok, _ := client.Extension("STARTTLS"); !ok {
			return fmt.Errorf("SMTP 服务器不支持要求的 STARTTLS")
		}
		if // err 用于本次流程后续判断的err
		err := client.StartTLS(&tls.Config{MinVersion: tls.VersionTLS12, ServerName: server}); err != nil {
			return err
		}
	}
	if auth != nil {
		if // err 用于本次流程后续判断的err
		err := client.Auth(auth); err != nil {
			return err
		}
	}
	if // err 用于本次流程后续判断的err
	err := client.Mail(from); err != nil {
		return err
	}
	if // err 用于本次流程后续判断的err
	err := client.Rcpt(to); err != nil {
		return err
	}
	// w、err 用于本次流程后续判断的w、err
	w, err := client.Data()
	if err != nil {
		return err
	}
	if // err 用于本次流程后续判断的err
	_, err := w.Write(message); err != nil {
		_ = w.Close()
		return err
	}
	if // err 用于本次流程后续判断的err
	err := w.Close(); err != nil {
		return err
	}
	return client.Quit()
}

// parseConfigBool 封装parse配置Bool业务协调。
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

// configOrSetting 封装配置Or设置业务协调。
func (n *Notifier) configOrSetting(ctx context.Context, cfg map[string]any, key, fallbackValue string) string {
	if // v 用于本次流程后续判断的v
	v := strings.TrimSpace(strOr(cfg, key, "")); v != "" {
		return v
	}
	if n.repository != nil {
		if // v、err 用于本次流程后续判断的v、err
		v, err := n.repository.GetSetting(ctx, key); err == nil && strings.TrimSpace(v) != "" {
			return strings.TrimSpace(v)
		}
	}
	return fallbackValue
}

// smtpConfigValue keeps legacy per-field fallback behavior for existing rows,
// while new rows use an explicit all-system or all-channel SMTP mode.
// smtpConfigValue 封装smtp配置值业务协调。
func (n *Notifier) smtpConfigValue(ctx context.Context, cfg map[string]any, key, fallbackValue string) string {
	// modeValue、hasExplicitMode 用于本次流程后续判断的模式Value、hasExplicit模式
	modeValue, hasExplicitMode := cfg["use_custom_smtp"]
	if !hasExplicitMode {
		return n.configOrSetting(ctx, cfg, key, fallbackValue)
	}
	if parseConfigBool(fmt.Sprintf("%v", modeValue), false) {
		if // value 用于本次流程后续判断的值
		value := strings.TrimSpace(strOr(cfg, key, "")); value != "" {
			return value
		}
		return fallbackValue
	}
	if n.repository != nil {
		if // value、err 用于本次流程后续判断的value、err
		value, err := n.repository.GetSetting(ctx, key); err == nil && strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return fallbackValue
}

// postJSON 通用 JSON POST。
func (n *Notifier) postJSON(url string, payload any) error {
	// body、err 用于本次流程后续判断的body、err
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	// requestCtx、requestCancel 为单次渠道 HTTP 请求创建有界取消路径。
	requestCtx, requestCancel := context.WithTimeout(context.Background(), legacyNotifierOperationTimeout)
	defer requestCancel()
	// req、err 分别是携带取消语义的渠道 HTTP 请求及其构造错误。
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return notificationRequestError(err)
	}
	req.Header.Set("Content-Type", "application/json")
	// resp、err 用于本次流程后续判断的resp、err
	resp, err := n.httpc.Do(req)
	if err != nil {
		return notificationRequestError(err)
	}
	defer resp.Body.Close()
	// responseBody、err 用于本次流程后续判断的响应Body、err
	responseBody, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("状态码 %d", resp.StatusCode)
	}
	if // err 用于本次流程后续判断的err
	err := notificationBusinessError(responseBody); err != nil {
		return err
	}
	return nil
}

// notificationRequestError 将外部通知请求错误转换为不含 URL 路径凭证的内部诊断错误。
// err 是 net/http、URL 解析或传输层返回的原始错误；返回值不得再暴露其完整请求地址。
func notificationRequestError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("通知渠道请求失败: %s", logsafe.ExternalError(err))
}

// notificationBusinessError 封装通知Business错误业务协调。
func notificationBusinessError(body []byte) error {
	if len(bytes.TrimSpace(body)) == 0 {
		return nil
	}
	// payload 用于本次流程后续判断的请求载荷
	var payload map[string]any
	if json.Unmarshal(body, &payload) != nil {
		return nil
	}
	// message 用于本次流程后续判断的消息
	message := strings.TrimSpace(firstMapString(payload, "errmsg", "msg", "message", "description"))
	if // code、ok 用于本次流程后续判断的code、ok
	code, ok := mapNumber(payload, "errcode"); ok && code != 0 {
		return fmt.Errorf("通知渠道返回错误 %.0f: %s", code, message)
	}
	if // code、ok 用于本次流程后续判断的code、ok
	code, ok := mapNumber(payload, "StatusCode"); ok && code != 0 {
		return fmt.Errorf("通知渠道返回错误 %.0f: %s", code, message)
	}
	if // code、ok 用于本次流程后续判断的code、ok
	code, ok := mapNumber(payload, "code"); ok && code != 0 && code != 200 {
		return fmt.Errorf("通知渠道返回错误 %.0f: %s", code, message)
	}
	if // okValue、exists 用于本次流程后续判断的okValue、exists
	okValue, exists := payload["ok"].(bool); exists && !okValue {
		return fmt.Errorf("通知渠道返回失败: %s", message)
	}
	return nil
}

// mapNumber 封装mapNumber业务协调。
func mapNumber(payload map[string]any, key string) (float64, bool) {
	// value、ok 用于本次流程后续判断的value、ok
	value, ok := payload[key]
	if !ok {
		return 0, false
	}
	switch // typed 用于本次流程后续判断的typed
	typed := value.(type) {
	case float64:
		return typed, true
	case string:
		// number、err 用于本次流程后续判断的number、err
		number, err := strconv.ParseFloat(typed, 64)
		return number, err == nil
	default:
		return 0, false
	}
}

// firstMapString 封装firstMapString业务协调。
func firstMapString(payload map[string]any, keys ...string) string {
	// key 表示当前遍历过程中的key
	for _, key := range keys {
		if // value、ok 用于本次流程后续判断的value、ok
		value, ok := payload[key].(string); ok && value != "" {
			return value
		}
	}
	return "未知错误"
}

// strOr 从 map 取字符串，缺失返回 fallback。
func strOr(m map[string]any, key, fallback string) string {
	if // v、ok 用于本次流程后续判断的v、ok
	v, ok := m[key]; ok {
		switch // x 用于本次流程后续判断的x
		x := v.(type) {
		case string:
			return x
		default:
			return fmt.Sprintf("%v", x)
		}
	}
	return fallback
}

// fallback 封装fallback业务协调。
func fallback(s, def string) string {
	if s == "" {
		return def
	}
	return s
}
