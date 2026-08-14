package engine

import (
	"context"
	"encoding/json"
	"strings"
	"time"
)

// dispatch 是 ws.ReceiveLoop 的回调，对每条解密后的消息做：
// 标记消息接收时间 → 提取消息 ID 去重 → 信号量限并发 → 分类（聊天/系统）→ 防抖投递。
// dispatch 负责dispatch相关处理。
func (a *Account) dispatch(decrypted map[string]any) {
	a.messageDispatcher.dispatch(decrypted)
}

// handleMessage 分类并投递消息。
func (a *Account) handleMessage(decrypted map[string]any) {
	a.messageDispatcher.handleMessage(decrypted)
}

// markAndCheckDedup 提取消息 ID，检查 1 小时内是否已处理；未处理则标记。
// 返回 true 表示应继续处理。移植自 _schedule_debounced_reply 的去重段。
// markAndCheckDedup 负责markAndCheckDedup相关处理。
func (a *Account) markAndCheckDedup(decrypted map[string]any, chat *ChatMessage) bool {
	return a.messageDispatcher.markAndCheckDedup(decrypted, chat)
}

// cleanupDedupLocked 负责cleanupDedupLocked相关处理。
func (a *Account) cleanupDedupLocked(now time.Time) {
	a.messageDispatcher.cleanupDedupLocked(now)
}

// scheduleDebouncedReply 为 chat_id 调度防抖回复：
// 同一 chat_id 连续来消息时取消旧定时器、用最新消息重新计时，1s 后投递最后一条。
// scheduleDebouncedReply 负责scheduleDebounced回复相关处理。
func (a *Account) scheduleDebouncedReply(chat ChatMessage) {
	a.messageDispatcher.scheduleDebouncedReply(chat)
}

// extractMessageID 从 message["1"]["10"]["bizTag"] 或 extJson 中提取 messageId。
// 移植自 _extract_message_id。
// extractMessageID 负责extract消息ID相关处理。
func extractMessageID(decrypted map[string]any) string {
	// m1、ok 保存m1、ok，供当前处理流程使用
	m1, ok := decrypted["1"].(map[string]any)
	if !ok {
		return ""
	}
	// m10、ok 保存m10、ok，供当前处理流程使用
	m10, ok := m1["10"].(map[string]any)
	if !ok {
		return ""
	}
	// bizTag 是 JSON 字符串：{"sourceId":"...","messageId":"..."}
	if biz, _ := m10["bizTag"].(string); biz != "" {
		if // id 保存标识，供当前处理流程使用
		id := parseMessageIDFromJSON(biz); id != "" {
			return id
		}
	}
	if // ext 保存ext，供当前处理流程使用
	ext, _ := m10["extJson"].(string); ext != "" {
		if // id 保存标识，供当前处理流程使用
		id := parseMessageIDFromJSON(ext); id != "" {
			return id
		}
	}
	return ""
}

// parseMessageIDFromJSON 负责parse消息IDFromJSON相关处理。
func parseMessageIDFromJSON(s string) string {
	// m 保存m，供当前处理流程使用
	var m map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal([]byte(s), &m); err != nil {
		return ""
	}
	if // id、ok 保存id、ok，供当前处理流程使用
	id, ok := m["messageId"].(string); ok {
		return id
	}
	return ""
}

// extractChatMessage 从解密消息中提取聊天消息字段。
func extractChatMessage(decrypted map[string]any, accountID, cookieStr string) *ChatMessage {
	// m1、ok 保存m1、ok，供当前处理流程使用
	m1, ok := decrypted["1"].(map[string]any)
	if !ok {
		return nil
	}
	// m10 保存m10，供当前处理流程使用
	m10, _ := m1["10"].(map[string]any)
	if m10 == nil {
		return nil
	}
	// reminder 保存reminder，供当前处理流程使用
	reminder, _ := m10["reminderContent"].(string)
	if reminder == "" {
		return nil
	}
	if isNonUserChatNotice(m1, m10, reminder) {
		return nil
	}
	// chatID 保存聊天ID，供当前处理流程使用
	chatID := toString(m1["2"])
	// chat_id 形如 "47983389096@goofish"，去掉后缀。
	if i := strings.Index(chatID, "@"); i >= 0 {
		chatID = chatID[:i]
	}
	// senderUserID 保存sender用户ID，供当前处理流程使用
	senderUserID, _ := m10["senderUserId"].(string)
	// senderName 保存sender名称，供当前处理流程使用
	senderName, _ := m10["senderNick"].(string)
	if strings.TrimSpace(senderName) == "" {
		senderName, _ = m10["reminderTitle"].(string)
	}
	// reminderURL 保存reminderURL，供当前处理流程使用
	reminderURL, _ := m10["reminderUrl"].(string)
	// itemID 保存商品ID，供当前处理流程使用
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
// isNonUserChatNotice 负责isNon用户聊天Notice相关处理。
func isNonUserChatNotice(m1, m10 map[string]any, reminder string) bool {
	if strings.TrimSuffix(strings.TrimSpace(toString(m10["senderUserId"])), "@goofish") == "1400" {
		return true
	}
	if strings.TrimSpace(reminder) == "发来一条新消息" {
		return true
	}
	if // sessionType 保存会话类型，供当前处理流程使用
	sessionType := strings.TrimSpace(toString(m10["sessionType"])); sessionType != "" && sessionType != "1" {
		return true
	}
	// contentType 保存内容类型，供当前处理流程使用
	contentType := messageContentType(m1, m10)
	switch contentType {
	case "14":
		return true
	case "26":
		return true
	}
	return false
}

// messageContentType 负责消息内容类型相关处理。
func messageContentType(m1, m10 map[string]any) string {
	if // ext 保存ext，供当前处理流程使用
	ext, _ := m10["extJson"].(string); ext != "" {
		// extJSON 保存extJSON，供当前处理流程使用
		var extJSON map[string]any
		if json.Unmarshal([]byte(ext), &extJSON) == nil {
			if // v 保存v，供当前处理流程使用
			v := toString(extJSON["contentType"]); v != "" {
				return v
			}
		}
	}
	// m6 保存m6，供当前处理流程使用
	m6, _ := m1["6"].(map[string]any)
	if m6 == nil {
		return ""
	}
	// m63 保存m63，供当前处理流程使用
	m63, _ := m6["3"].(map[string]any)
	if m63 == nil {
		return ""
	}
	if // v 保存v，供当前处理流程使用
	v := toString(m63["4"]); v != "" {
		return v
	}
	if // contentJSON 保存内容JSON，供当前处理流程使用
	contentJSON, _ := m63["5"].(string); contentJSON != "" {
		// content 保存内容，供当前处理流程使用
		var content map[string]any
		if json.Unmarshal([]byte(contentJSON), &content) == nil {
			return toString(content["contentType"])
		}
	}
	return ""
}

// extractItemID 从 reminderUrl 中正则提取 itemId=xxx。
func extractItemID(url string) string {
	// key 保存key，供当前处理流程使用
	const key = "itemId="
	// i 保存i，供当前处理流程使用
	i := strings.Index(url, key)
	if i < 0 {
		return ""
	}
	// s 保存s，供当前处理流程使用
	s := url[i+len(key):]
	if // j 保存j，供当前处理流程使用
	j := strings.IndexAny(s, "&\n\r"); j >= 0 {
		s = s[:j]
	}
	return s
}

// currentCookieStr 负责current登录凭证Str相关处理。
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

// contains 负责contains相关处理。
func contains(s, sub string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(sub)) }

// toString 负责toString相关处理。
func toString(v any) string {
	switch // x 保存x，供当前处理流程使用
	x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case float64:
		// JSON 数字 → 整数字符串。
		return trimFloatInt(x)
	default:
		// b 保存b，供当前处理流程使用
		b, _ := json.Marshal(v)
		return string(b)
	}
}

// trimFloatInt 负责trimFloatInt相关处理。
func trimFloatInt(f float64) string {
	if f == float64(int64(f)) {
		return int64ToString(int64(f))
	}
	return ftoa(f)
}

// int64ToString 负责int64ToString相关处理。
func int64ToString(n int64) string {
	if n == 0 {
		return "0"
	}
	// neg 保存neg，供当前处理流程使用
	neg := n < 0
	if neg {
		n = -n
	}
	// b 保存b，供当前处理流程使用
	var b [20]byte
	// i 保存i，供当前处理流程使用
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

// ftoa 负责ftoa相关处理。
func ftoa(f float64) string { return jsonNumber(f) }

// jsonNumber 负责jsonNumber相关处理。
func jsonNumber(f float64) string {
	// b 保存b，供当前处理流程使用
	b, _ := json.Marshal(f)
	return string(b)
}

// truncID 负责truncID相关处理。
func truncID(id string) string {
	if len(id) > 50 {
		return id[:50] + "..."
	}
	return id
}

// errString 负责errString相关处理。
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// sleepCtx 负责sleepCtx相关处理。
func sleepCtx(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return nil
	}
	// t 保存t，供当前处理流程使用
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
