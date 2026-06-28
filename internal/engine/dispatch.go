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
	a.mu.Lock()
	a.lastMsgReceived = time.Now()
	a.mu.Unlock()

	// 信号量限并发（非阻塞获取，超限则丢弃并告警——避免阻塞 WS 读取）。
	select {
	case a.sem <- struct{}{}:
	default:
		a.logger.Warn("消息处理并发达上限，丢弃消息", "limit", MessageSemaphoreSize)
		return
	}
	go func() {
		defer func() { <-a.sem }()
		a.handleMessage(decrypted)
	}()
}

// handleMessage 分类并投递消息。
func (a *Account) handleMessage(decrypted map[string]any) {
	chat := extractChatMessage(decrypted, a.CookieID, a.currentCookieStr())
	if chat != nil && chat.Text != "" {
		// 去重。
		if !a.markAndCheckDedup(decrypted, chat) {
			return
		}
		// 自动发货触发消息必须立即处理，不能进入普通聊天防抖。
		// 否则闲鱼紧随其后的系统提示（如“不想宝贝被砍价”）可能覆盖付款卡片，
		// 造成未发货且进入 AI 回复。
		if IsAutoDeliveryTrigger(chat.Text) {
			a.cancelDebouncedReply(chat.ChatID)
			if a.delivery != nil {
				if err := a.delivery.Handle(context.Background(), *chat); err != nil {
					a.logger.Error("处理自动发货失败", "err", err, "chat_id", chat.ChatID)
				}
			}
			if a.handler != nil {
				if err := a.handler.HandleChatMessage(context.Background(), *chat); err != nil {
					a.logger.Error("处理聊天消息失败", "err", err, "chat_id", chat.ChatID)
				}
			}
			return
		}

		// 普通聊天消息走防抖，合并连续文本后再自动回复。
		a.scheduleDebouncedReply(*chat)
		return
	}
	sys := extractSystemMessage(decrypted, a.CookieID, a.currentCookieStr())
	if sys != nil && sys.RedReminder != "" && a.handler != nil {
		if err := a.handler.HandleSystemMessage(context.Background(), *sys); err != nil {
			a.logger.Error("处理系统消息失败", "err", err)
		}
	}
}

func (a *Account) cancelDebouncedReply(chatID string) {
	if chatID == "" {
		return
	}
	a.debounceMu.Lock()
	defer a.debounceMu.Unlock()
	if old, ok := a.debounceTimers[chatID]; ok {
		if old.timer != nil {
			old.timer.Stop()
		}
		delete(a.debounceTimers, chatID)
	}
}

// markAndCheckDedup 提取消息 ID，检查 1 小时内是否已处理；未处理则标记。
// 返回 true 表示应继续处理。移植自 _schedule_debounced_reply 的去重段。
func (a *Account) markAndCheckDedup(decrypted map[string]any, chat *ChatMessage) bool {
	msgID := extractMessageID(decrypted)
	if msgID == "" {
		// 备用标识：chat_id + text + create_time。
		createTime := "0"
		if m1, ok := decrypted["1"].(map[string]any); ok {
			if t, ok := m1["5"]; ok {
				createTime = toString(t)
			}
		}
		msgID = chat.ChatID + "_" + chat.Text + "_" + createTime
	}

	a.dedupMu.Lock()
	defer a.dedupMu.Unlock()
	now := time.Now()
	if last, ok := a.processed[msgID]; ok {
		if now.Sub(last) < MessageExpireTime {
			a.logger.Info("消息已处理过，跳过", "msg_id", truncID(msgID))
			return false
		}
	}
	a.processed[msgID] = now

	// 清理：超上限时删过期记录，仍超则删最旧一半。
	if len(a.processed) > ProcessedIDsMaxSize {
		a.cleanupDedupLocked(now)
	}
	return true
}

func (a *Account) cleanupDedupLocked(now time.Time) {
	for id, t := range a.processed {
		if now.Sub(t) > MessageExpireTime {
			delete(a.processed, id)
		}
	}
	if len(a.processed) > ProcessedIDsMaxSize {
		// 删最旧一半。
		type kv struct {
			id string
			t  time.Time
		}
		all := make([]kv, 0, len(a.processed))
		for id, t := range a.processed {
			all = append(all, kv{id, t})
		}
		// 简单选择：按时间升序，删前一半。
		for i := 0; i < len(all); i++ {
			for j := i + 1; j < len(all); j++ {
				if all[j].t.Before(all[i].t) {
					all[i], all[j] = all[j], all[i]
				}
			}
		}
		remove := len(all) / 2
		for i := 0; i < remove; i++ {
			delete(a.processed, all[i].id)
		}
	}
}

// scheduleDebouncedReply 为 chat_id 调度防抖回复：
// 同一 chat_id 连续来消息时取消旧定时器、用最新消息重新计时，1s 后投递最后一条。
func (a *Account) scheduleDebouncedReply(chat ChatMessage) {
	a.debounceMu.Lock()
	defer a.debounceMu.Unlock()

	deadline := time.Now()
	// 取消旧定时器。
	if old, ok := a.debounceTimers[chat.ChatID]; ok && old.timer != nil {
		old.timer.Stop()
	}
	entry := &debounceEntry{lastMsg: chat, deadline: deadline}
	a.debounceTimers[chat.ChatID] = entry

	entry.timer = time.AfterFunc(MessageDebounceDelay, func() {
		a.debounceMu.Lock()
		cur, ok := a.debounceTimers[chat.ChatID]
		if !ok || cur.deadline != deadline {
			// 期间有新消息，跳过旧消息处理。
			a.debounceMu.Unlock()
			return
		}
		delete(a.debounceTimers, chat.ChatID)
		lastMsg := cur.lastMsg
		a.debounceMu.Unlock()

		if a.delivery != nil {
			if err := a.delivery.Handle(context.Background(), lastMsg); err != nil {
				a.logger.Error("处理自动发货失败", "err", err, "chat_id", chat.ChatID)
			}
			if IsAutoDeliveryTrigger(lastMsg.Text) {
				return
			}
		}
		if a.reply != nil {
			if err := a.reply.Handle(context.Background(), lastMsg); err != nil {
				a.logger.Error("处理自动回复失败", "err", err, "chat_id", chat.ChatID)
			}
		}
		if a.handler != nil {
			if err := a.handler.HandleChatMessage(context.Background(), lastMsg); err != nil {
				a.logger.Error("处理聊天消息失败", "err", err, "chat_id", chat.ChatID)
			}
		}
	})
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

// isNonUserChatNotice 判断闲鱼 IM 中不应进入自动回复/自动发货的系统提示或交易卡片。
// 典型样本：
// - contentType=14：“有蚂蚁森林能量可领”“不想宝贝被砍价?设置不砍价回复”“退款成功”
// - contentType=26：交易卡片，如“我已拍下，待付款”“我发起了退款申请”
// 例外：付款待发货卡片要进入自动发货，因此保留。
func isNonUserChatNotice(m1, m10 map[string]any, reminder string) bool {
	contentType := messageContentType(m1, m10)
	switch contentType {
	case "14":
		return true
	case "26":
		return !IsAutoDeliveryTrigger(reminder)
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

// extractSystemMessage 提取订单状态等系统消息（message["3"]）。
func extractSystemMessage(decrypted map[string]any, accountID, cookieStr string) *SystemMessage {
	m3, ok := decrypted["3"].(map[string]any)
	if !ok {
		return nil
	}
	red, _ := m3["redReminder"].(string)
	uid, _ := m3["userId"].(string)
	if red == "" && uid == "" {
		return nil
	}
	return &SystemMessage{
		AccountID:   accountID,
		CookieStr:   cookieStr,
		RedReminder: red,
		UserID:      uid,
		Raw:         decrypted,
	}
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

// ---- 小工具 ----

func contains(s, sub string) bool { return strings.Contains(strings.ToLower(s), strings.ToLower(sub)) }

func toString(v any) string {
	switch x := v.(type) {
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

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
