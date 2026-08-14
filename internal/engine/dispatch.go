package engine

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// dispatch 是 ws.ReceiveLoop 的回调，对每条解密后的消息做：
// 标记消息接收时间 → 提取消息 ID 去重 → 信号量限并发 → 分类（聊天/系统）→ 防抖投递。
func (a *Account) dispatch(decrypted map[string]any) {
	a.messageDispatcher.dispatch(decrypted)
}

// handleMessage 分类并投递消息。
func (a *Account) handleMessage(decrypted map[string]any) {
	a.messageDispatcher.handleMessage(decrypted)
}

// markAndCheckDedup 提取消息 ID，检查 1 小时内是否已处理；未处理则标记。
// 返回 true 表示应继续处理。移植自 _schedule_debounced_reply 的去重段。
func (a *Account) markAndCheckDedup(decrypted map[string]any, chat *ChatMessage) bool {
	return a.messageDispatcher.markAndCheckDedup(decrypted, chat)
}

func (a *Account) cleanupDedupLocked(now time.Time) {
	a.messageDispatcher.cleanupDedupLocked(now)
}

// scheduleDebouncedReply 为 chat_id 调度防抖回复：
// 同一 chat_id 连续来消息时取消旧定时器、用最新消息重新计时，1s 后投递最后一条。
func (a *Account) scheduleDebouncedReply(chat ChatMessage) {
	a.messageDispatcher.scheduleDebouncedReply(chat)
}

// extractMessageID 从 message["1"]["10"]["bizTag"] 或 extJson 中提取 messageId。
// 移植自 _extract_message_id。
func extractMessageID(decrypted map[string]any) string {
	m1, ok := decrypted["1"].(map[string]any)
	if !ok {
		return ""
	}
	m10, ok := m1["10"].(map[string]any)
	if !ok {
		return ""
	}
	// bizTag 是 JSON 字符串：{"sourceId":"...","messageId":"..."}
	if biz, _ := m10["bizTag"].(string); biz != "" {
		if id := parseMessageIDFromJSON(biz); id != "" {
			return id
		}
	}
	if ext, _ := m10["extJson"].(string); ext != "" {
		if id := parseMessageIDFromJSON(ext); id != "" {
			return id
		}
	}
	return ""
}

func parseMessageIDFromJSON(s string) string {
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		return ""
	}
	if id, ok := m["messageId"].(string); ok {
		return id
	}
	return ""
}

// extractChatMessage 从解密消息中提取聊天消息字段。
func extractChatMessage(decrypted map[string]any, accountID, cookieStr string) *ChatMessage {
	m1, ok := decrypted["1"].(map[string]any)
	if !ok {
		return nil
	}
	m10, _ := m1["10"].(map[string]any)
	if m10 == nil {
		return nil
	}
	reminder, _ := m10["reminderContent"].(string)
	if reminder == "" {
		return nil
	}
	if isNonUserChatNotice(m1, m10, reminder) {
		return nil
	}
	chatID := toString(m1["2"])
	// chat_id 形如 "47983389096@goofish"，去掉后缀。
	if i := strings.Index(chatID, "@"); i >= 0 {
		chatID = chatID[:i]
	}
	senderUserID, _ := m10["senderUserId"].(string)
	senderName, _ := m10["senderNick"].(string)
	if strings.TrimSpace(senderName) == "" {
		senderName, _ = m10["reminderTitle"].(string)
	}
	reminderURL, _ := m10["reminderUrl"].(string)
	itemID := extractItemID(reminderURL)
	return &ChatMessage{
		AccountID:    accountID,
		CookieStr:    cookieStr,
		ChatID:       chatID,
		SenderUserID: senderUserID,
		SenderName:   senderName,
		Text:         reminder,
		ItemID:       itemID,
		Raw:          decrypted,
	}
}

// isNonUserChatNotice 判断闲鱼 IM 中不应进入自动回复的系统提示或交易卡片。
// 典型样本：
// - contentType=14：“有蚂蚁森林能量可领”“不想宝贝被砍价?设置不砍价回复”“退款成功”
// - contentType=26：交易卡片，如“我已拍下，待付款”“我发起了退款申请”
// 付款待发货卡片已经在 handleMessage 前半段进入 automation.Center，这里不能再进入聊天回复链。
func isNonUserChatNotice(m1, m10 map[string]any, reminder string) bool {
	if strings.TrimSuffix(strings.TrimSpace(toString(m10["senderUserId"])), "@goofish") == "1400" {
		return true
	}
	if strings.TrimSpace(reminder) == "发来一条新消息" {
		return true
	}
	if sessionType := strings.TrimSpace(toString(m10["sessionType"])); sessionType != "" && sessionType != "1" {
		return true
	}
	contentType := messageContentType(m1, m10)
	switch contentType {
	case "14":
		return true
	case "26":
		return true
	}
	return false
}

func messageContentType(m1, m10 map[string]any) string {
	if ext, _ := m10["extJson"].(string); ext != "" {
		var extJSON map[string]any
		if json.Unmarshal([]byte(ext), &extJSON) == nil {
			if v := toString(extJSON["contentType"]); v != "" {
				return v
			}
		}
	}
	m6, _ := m1["6"].(map[string]any)
	if m6 == nil {
		return ""
	}
	m63, _ := m6["3"].(map[string]any)
	if m63 == nil {
		return ""
	}
	if v := toString(m63["4"]); v != "" {
		return v
	}
	if contentJSON, _ := m63["5"].(string); contentJSON != "" {
		var content map[string]any
		if json.Unmarshal([]byte(contentJSON), &content) == nil {
			return toString(content["contentType"])
		}
	}
	return ""
}

// extractItemID 从 reminderUrl 中正则提取 itemId=xxx。
func extractItemID(url string) string {
	const key = "itemId="
	i := strings.Index(url, key)
	if i < 0 {
		return ""
	}
	s := url[i+len(key):]
	if j := strings.IndexAny(s, "&\n\r"); j >= 0 {
		s = s[:j]
	}
	return s
}

func (a *Account) currentCookieStr() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.CookieStr
}

// CurrentCookieStr 线程安全地返回账号当前使用的 Cookie。
func (a *Account) CurrentCookieStr() string {
	return a.currentCookieStr()
}

// ---- 小工具 ----

func contains(s, sub string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(sub)) }

func toString(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		// JSON 数字 → 整数字符串。
		return trimFloatInt(x)
	default:
		b, _ := json.Marshal(v)
		return string(b)
	}
}

func trimFloatInt(f float64) string {
	if f == float64(int64(f)) {
		return int64ToString(int64(f))
	}
	return ftoa(f)
}

func int64ToString(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}

func ftoa(f float64) string { return jsonNumber(f) }

func jsonNumber(f float64) string {
	b, _ := json.Marshal(f)
	return string(b)
}

func truncID(id string) string {
	if len(id) > 50 {
		return id[:50] + "..."
	}
	return id
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
