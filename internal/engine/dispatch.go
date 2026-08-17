package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/xianyu/protocol"
)

// dispatch 是 ws.ReceiveLoop 的回调，对每条解密后的消息做：
// 标记消息接收时间 → 提取消息 ID 去重 → 信号量限并发 → 分类（聊天/系统）→ 防抖投递。
func (a *Account) dispatch(decrypted map[string]any) {
	ctx, ok := a.beginTask()
	if !ok {
		return
	}
	a.mu.Lock()
	a.lastMsgReceived = time.Now()
	a.mu.Unlock()

	// 系统业务事件不能丢弃：并发满时让 WS 读取产生背压，等待处理槽位。
	// 普通聊天仍采用非阻塞限流，避免聊天洪峰拖垮连接。
	isSystemEvent := automation.ExtractTaskFromWS(a.CookieID, a.currentCookieStr(), decrypted) != nil
	if isSystemEvent {
		select {
		case a.sem <- struct{}{}:
		case <-ctx.Done():
			a.taskWG.Done()
			return
		}
		go func() {
			defer a.taskWG.Done()
			defer func() { <-a.sem }()
			a.handleMessageContext(ctx, decrypted)
		}()
		return
	}

	select {
	case a.sem <- struct{}{}:
	default:
		a.taskWG.Done()
		a.logger.Warn("消息处理并发达上限，丢弃消息", "limit", MessageSemaphoreSize)
		return
	}
	go func() {
		defer a.taskWG.Done()
		defer func() { <-a.sem }()
		a.handleMessageContext(ctx, decrypted)
	}()
}

// handleMessage 分类并投递消息。
func (a *Account) handleMessage(decrypted map[string]any) {
	a.handleMessageContext(context.Background(), decrypted)
}

func (a *Account) handleMessageContext(ctx context.Context, decrypted map[string]any) {
	if receipt, ok := extractMessageReadEvent(decrypted); ok {
		receipt.AccountID = a.CookieID
		if h, ok := a.handler.(MessageReadHandler); ok {
			if err := h.HandleMessageRead(ctx, receipt); err != nil {
				a.logger.Warn("处理聊天已读回执失败", "err", err, "message_id", receipt.MessageID)
			}
		} else {
			a.logger.Debug("收到聊天已读回执，但未接入处理器", "message_id", receipt.MessageID)
		}
		return
	}
	// 第一优先级：系统卡片和平台通知进入自动化中心。
	// 这里不判断具体业务，只做“平台事件”事实解析；系统消息永远不进入 AI 回复范围。
	if task := automation.ExtractTaskFromWS(a.CookieID, a.currentCookieStr(), decrypted); task != nil {
		if a.handler != nil {
			if err := a.handler.HandleSystemEvent(ctx, *task); err != nil {
				a.logger.Error("处理系统自动化事件失败", "err", err, "trigger", task.TriggerType)
			}
		}
		return
	}
	if senderUserID := extractChatSenderUserID(decrypted); isSelfUserID(senderUserID, protocol.TransCookies(a.currentCookieStr())["unb"]) {
		a.logger.Info("忽略账号自身发送的 WebSocket 聊天回显", "chat_id", extractChatID(decrypted), "sender_id", senderUserID)
		return
	}

	chat := extractChatMessage(decrypted, a.CookieID, a.currentCookieStr())
	if chat != nil && chat.Text != "" {
		// 去重。
		if !a.markAndCheckDedup(decrypted, chat) {
			return
		}
		// 用户消息走防抖，合并连续文本后再进入关键词/AI/默认回复链。
		a.scheduleDebouncedReply(*chat)
		return
	}
}

func extractMessageReadEvent(v map[string]any) (MessageReadEvent, bool) {
	// 40103 解密后不会保留外层 objectType，只剩下固定的紧凑字段：
	// 1=PNM 消息 ID、2=状态(2=已读)、4=会话 ID、6=事件时间。
	// 不能只在解密后的 map 里继续寻找 "bizType":40103，否则真实回执
	// 会被当成普通消息静默丢弃。
	messageID := strings.TrimSpace(toString(v["1"]))
	if ids, ok := v["1"].([]any); ok && len(ids) > 0 {
		messageID = strings.TrimSpace(toString(ids[0]))
	}
	if strings.HasSuffix(messageID, ".PNM") && toString(v["2"]) == "2" {
		chatID := strings.TrimSpace(toString(v["4"]))
		if !strings.Contains(chatID, "@goofish") {
			chatID = strings.TrimSpace(toString(v["3"]))
		}
		chatID = strings.TrimSuffix(chatID, "@goofish")
		return MessageReadEvent{ChatID: chatID, MessageID: messageID, ReadAt: time.Now().UTC().UnixMilli()}, true
	}
	var found bool
	var ev MessageReadEvent
	var walk func(any)
	walk = func(x any) {
		if found {
			return
		}
		switch m := x.(type) {
		case map[string]any:
			for k, val := range m {
				lk := strings.ToLower(k)
				if lk == "biztype" || lk == "biz_type" || lk == "type" {
					if toString(val) == "40103" {
						found = true
					}
				}
			}
			if found {
				ev.MessageID = extractNestedString(m, "messageid", "message_id")
				ev.ChatID = extractNestedString(m, "cid", "chatid", "chat_id")
				if ev.MessageID == "" {
					ev.MessageID = extractNestedString(m, "id")
				}
				return
			}
			for _, child := range m {
				walk(child)
			}
		case []any:
			for _, child := range m {
				walk(child)
			}
		case string:
			// Some sync envelopes carry the event body as an escaped JSON
			// string instead of an already-decoded object.
			var decoded any
			if json.Unmarshal([]byte(m), &decoded) == nil {
				walk(decoded)
			}
		}
	}
	walk(v)
	if !found || ev.MessageID == "" {
		return MessageReadEvent{}, false
	}
	return ev, true
}

func extractNestedString(m map[string]any, keys ...string) string {
	for k, v := range m {
		for _, want := range keys {
			if strings.EqualFold(k, want) && strings.TrimSpace(toString(v)) != "" {
				return strings.TrimSpace(toString(v))
			}
		}
	}
	for _, v := range m {
		if child, ok := v.(map[string]any); ok {
			if s := extractNestedString(child, keys...); s != "" {
				return s
			}
		}
	}
	return ""
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
		// 仍超上限：按时间升序排序后删最旧一半。
		type kv struct {
			id string
			t  time.Time
		}
		all := make([]kv, 0, len(a.processed))
		for id, t := range a.processed {
			all = append(all, kv{id, t})
		}
		sort.Slice(all, func(i, j int) bool { return all[i].t.Before(all[j].t) })
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
		ctx, ok := a.beginTask()
		if !ok {
			return
		}
		defer a.taskWG.Done()

		if a.reply != nil {
			if err := a.reply.Handle(ctx, lastMsg); err != nil {
				a.logger.Error("处理自动回复失败", "err", err, "chat_id", chat.ChatID)
			}
		}
		if a.handler != nil {
			if err := a.handler.HandleChatMessage(ctx, lastMsg); err != nil {
				a.logger.Error("处理聊天消息失败", "err", err, "chat_id", chat.ChatID)
			}
		}
	})
}

// extractMessageID 提取闲鱼消息状态接口使用的消息 ID。
//
// 实时 WS 聊天消息同时包含两种 ID：message["1"]["3"] 是消息在 IM
// 服务中的 PNM ID，/r/MessageStatus/read 使用的正是这个 ID；10.bizTag、
// extJson 和 reminderUrl 里的 messageId 是通知/推送侧的关联 ID，不能用来
// 上报会话已读。历史消息接口也返回 PNM ID，因此优先使用字段 3，其他
// 字段只作为兼容旧消息或缺失字段 3 的兜底。
func extractMessageID(decrypted map[string]any) string {
	m1, ok := decrypted["1"].(map[string]any)
	if !ok {
		return ""
	}
	if id := strings.TrimSpace(toString(m1["3"])); id != "" && id != "<nil>" {
		return id
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
	if reminderURL, _ := m10["reminderUrl"].(string); reminderURL != "" {
		if parsed, err := url.Parse(reminderURL); err == nil {
			if id := strings.TrimSpace(parsed.Query().Get("messageId")); id != "" {
				return id
			}
		}
	}
	return findMessageID(decrypted)
}

func findMessageID(value any) string {
	switch x := value.(type) {
	case map[string]any:
		for key, child := range x {
			if strings.EqualFold(key, "messageId") || strings.EqualFold(key, "message_id") {
				if id := strings.TrimSpace(fmt.Sprint(child)); id != "" && id != "<nil>" {
					return id
				}
			}
		}
		for _, child := range x {
			if id := findMessageID(child); id != "" {
				return id
			}
		}
	case []any:
		for _, child := range x {
			if id := findMessageID(child); id != "" {
				return id
			}
		}
	case string:
		var decoded any
		if json.Unmarshal([]byte(x), &decoded) == nil {
			return findMessageID(decoded)
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
	senderUserID := extractChatSenderUserIDFromMaps(m1, m10)
	if isSelfUserID(senderUserID, protocol.TransCookies(cookieStr)["unb"]) {
		// The official client echoes messages sent by this account over the
		// same WebSocket. They are delivery observations, not new buyer input,
		// and must never enter the automatic-reply pipeline.
		return nil
	}
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
		MessageID:    extractMessageID(decrypted),
		ItemID:       itemID,
		Raw:          decrypted,
	}
}

func extractChatSenderUserID(decrypted map[string]any) string {
	m1, _ := decrypted["1"].(map[string]any)
	if m1 == nil {
		return ""
	}
	m10, _ := m1["10"].(map[string]any)
	return extractChatSenderUserIDFromMaps(m1, m10)
}

func extractChatSenderUserIDFromMaps(m1, m10 map[string]any) string {
	if m10 != nil {
		if sender := strings.TrimSpace(toString(m10["senderUserId"])); sender != "" {
			return sender
		}
	}
	// Some echoed messages omit 10.senderUserId but retain the sender in the
	// compact 1.1 envelope.
	if sender, ok := m1["1"].(map[string]any); ok {
		return strings.TrimSpace(toString(sender["1"]))
	}
	return ""
}

func extractChatID(decrypted map[string]any) string {
	m1, _ := decrypted["1"].(map[string]any)
	chatID := strings.TrimSpace(toString(m1["2"]))
	return strings.TrimSuffix(chatID, "@goofish")
}

func isSelfUserID(senderUserID, selfUserID string) bool {
	sender := strings.TrimSuffix(strings.TrimSpace(senderUserID), "@goofish")
	self := strings.TrimSuffix(strings.TrimSpace(selfUserID), "@goofish")
	return sender != "" && self != "" && sender == self
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
