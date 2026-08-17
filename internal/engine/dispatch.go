package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
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

// extractMessageReadEvent 从解密 WebSocket 事件中提取出站消息已读回执。
func extractMessageReadEvent(v map[string]any) (MessageReadEvent, bool) {
	// 40103 解密后不会保留外层 objectType，只剩下固定的紧凑字段：
	// 1=PNM 消息 ID、2=状态(2=已读)、4=会话 ID、6=事件时间。
	// 不能只在解密后的 map 里继续寻找 "bizType":40103，否则真实回执
	// 会被当成普通消息静默丢弃。
	// messageID 保存紧凑回执或嵌套事件中的平台 PNM 消息 ID。
	messageID := strings.TrimSpace(toString(v["1"]))
	// ids 保存批量回执携带的 PNM ID 列表；ok 表示字段 1 是否采用批量编码。
	if ids, ok := v["1"].([]any); ok && len(ids) > 0 {
		messageID = strings.TrimSpace(toString(ids[0]))
	}
	if strings.HasSuffix(messageID, ".PNM") && toString(v["2"]) == "2" {
		// chatID 保存紧凑回执携带的会话 ID，旧信封缺失时回退字段 3。
		chatID := strings.TrimSpace(toString(v["4"]))
		if !strings.Contains(chatID, "@goofish") {
			chatID = strings.TrimSpace(toString(v["3"]))
		}
		chatID = strings.TrimSuffix(chatID, "@goofish")
		return MessageReadEvent{ChatID: chatID, MessageID: messageID, ReadAt: time.Now().UTC().UnixMilli()}, true
	}
	// found 标记已定位 40103 信封；ev 累积其会话和消息标识；walk 递归读取兼容嵌套结构。
	var found bool
	// ev 保存从嵌套兼容回执中提取的平台会话与消息标识。
	var ev MessageReadEvent
	// walk 解析对象、数组或 JSON 字符串形式的嵌套事件，命中后停止后续遍历。
	var walk func(any)
	walk = func(x any) {
		if found {
			return
		}
		// m 保存当前遍历节点被识别出的具体 JSON 数据类型。
		switch m := x.(type) {
		case map[string]any:
			// k 为当前字段名；val 为其原始 JSON 值，用来定位回执事件类型。
			for k, val := range m {
				// lk 是小写化字段名，兼容平台信封的不同字段命名。
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
			// child 为当前对象中的嵌套值，继续搜索未解包的回执信封。
			for _, child := range m {
				walk(child)
			}
		case []any:
			// child 为当前批量信封中的一个候选事件。
			for _, child := range m {
				walk(child)
			}
		case string:
			// 部分同步信封将事件体编码为转义 JSON 字符串，而非已解码对象。
			// decoded 保存反序列化后的事件体，兼容字符串包装的同步消息。
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

// extractNestedString 在嵌套信封中按大小写无关键名查找第一个非空字符串。
func extractNestedString(m map[string]any, keys ...string) string {
	// k 是当前字段名；v 是字段原值，用于大小写无关地匹配候选键。
	for k, v := range m {
		// want 为调用方允许的目标字段名。
		for _, want := range keys {
			if strings.EqualFold(k, want) && strings.TrimSpace(toString(v)) != "" {
				return strings.TrimSpace(toString(v))
			}
		}
	}
	// v 为当前对象中的嵌套值，继续查询其中的兼容子信封。
	for _, v := range m {
		// child 为可继续递归解析的对象；ok 表示当前值是否是对象结构。
		if child, ok := v.(map[string]any); ok {
			// s 保存子信封中找到的第一个非空目标字段值。
			if s := extractNestedString(child, keys...); s != "" {
				return s
			}
		}
	}
	return ""
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

// extractMessageID 提取闲鱼消息状态接口使用的消息 ID。
//
// 实时 WS 聊天消息同时包含两种 ID：message["1"]["3"] 是消息在 IM
// 服务中的 PNM ID，/r/MessageStatus/read 使用的正是这个 ID；10.bizTag、
// extJson 和 reminderUrl 里的 messageId 是通知/推送侧的关联 ID，不能用来
// 上报会话已读。历史消息接口也返回 PNM ID，因此优先使用字段 3，其他
// 字段只作为兼容旧消息或缺失字段 3 的兜底。
func extractMessageID(decrypted map[string]any) string {
	// m1、ok 保存m1、ok，供当前处理流程使用
	m1, ok := decrypted["1"].(map[string]any)
	if !ok {
		return ""
	}
	// id 保存实时消息最可靠的 PNM ID，存在时不可用推送关联 ID 覆盖。
	if id := strings.TrimSpace(toString(m1["3"])); id != "" && id != "<nil>" {
		return id
	}
	// m10、ok 保存消息展示扩展及其是否存在，用于兼容旧消息关联 ID。
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
	// reminderURL 保存提醒链接，旧消息会将推送关联 ID 编码在其查询参数中。
	if reminderURL, _ := m10["reminderUrl"].(string); reminderURL != "" {
		// parsed 保存已解析的提醒链接；err 表示 URL 格式不合法时的解析失败。
		if parsed, err := url.Parse(reminderURL); err == nil {
			// id 保存 reminderUrl 查询参数中的兼容消息关联 ID。
			if id := strings.TrimSpace(parsed.Query().Get("messageId")); id != "" {
				return id
			}
		}
	}
	return findMessageID(decrypted)
}

// findMessageID 递归解析兼容消息信封中可能存在的关联消息 ID。
func findMessageID(value any) string {
	// x 保存当前待搜索节点的具体值，按对象、数组和 JSON 字符串分别处理。
	switch x := value.(type) {
	case map[string]any:
		// key 是对象字段名；child 是字段值，用于先检查本层关联 ID。
		for key, child := range x {
			if strings.EqualFold(key, "messageId") || strings.EqualFold(key, "message_id") {
				// id 保存当前字段提供的非空关联消息 ID。
				if id := strings.TrimSpace(fmt.Sprint(child)); id != "" && id != "<nil>" {
					return id
				}
			}
		}
		// child 为本层嵌套字段值，递归兼容更深层信封。
		for _, child := range x {
			// id 保存子节点中首先发现的关联消息 ID。
			if id := findMessageID(child); id != "" {
				return id
			}
		}
	case []any:
		// child 为批量消息中的一个候选事件或子信封。
		for _, child := range x {
			// id 保存数组成员中首先发现的关联消息 ID。
			if id := findMessageID(child); id != "" {
				return id
			}
		}
	case string:
		// decoded 保存从 JSON 字符串解码后的兼容嵌套结构。
		var decoded any
		if json.Unmarshal([]byte(x), &decoded) == nil {
			return findMessageID(decoded)
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
	// senderUserID 保存经兼容字段归一后的发送者身份。
	senderUserID := extractChatSenderUserIDFromMaps(m1, m10)
	if isSelfUserID(senderUserID, protocol.TransCookies(cookieStr)["unb"]) {
		// 官方客户端会通过同一 WebSocket 回显本账号发出的消息；它仅表示投递观察，
		// 并非买家新输入，绝不能进入自动回复链。
		return nil
	}
	// senderName 保存发送者展示名称，字段缺失时由提醒标题补齐。
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
		MessageID:    extractMessageID(decrypted),
		ItemID:       itemID,
		Raw:          decrypted,
	}
}

// extractChatSenderUserIDFromMaps 优先读取展示扩展，并兼容紧凑信封中的发送者账号 ID。
func extractChatSenderUserIDFromMaps(m1, m10 map[string]any) string {
	if m10 != nil {
		// sender 保存标准 senderUserId 的去空白结果。
		if sender := strings.TrimSpace(toString(m10["senderUserId"])); sender != "" {
			return sender
		}
	}
	// 部分回显消息缺少 10.senderUserId，但仍在紧凑 1.1 信封中保留发送者身份。
	// sender 保存紧凑信封中的发送者对象；ok 表示该兼容字段是否存在。
	if sender, ok := m1["1"].(map[string]any); ok {
		return strings.TrimSpace(toString(sender["1"]))
	}
	return ""
}

// isSelfUserID 比较发送者与当前账号身份，忽略 IM 协议后缀以过滤官方客户端回显。
func isSelfUserID(senderUserID, selfUserID string) bool {
	// sender 是归一化后的消息发送者身份。
	sender := strings.TrimSuffix(strings.TrimSpace(senderUserID), "@goofish")
	// self 是归一化后的当前账号身份。
	self := strings.TrimSuffix(strings.TrimSpace(selfUserID), "@goofish")
	return sender != "" && self != "" && sender == self
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
