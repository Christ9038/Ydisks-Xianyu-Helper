package engine

import (
	"context"
	"strconv"
	"strings"
	"sync"
	"time"
)

// systemSentRetention 是本系统出站登记等待自身回显的最长时间；超过后视为回显丢失并清理，不再用于判定。
const systemSentRetention = 2 * time.Minute

// manualMarkRetention 是人工发送时间的保留时长；与可配置暂停窗口上限（1440 分钟）一致，过期后自然恢复自动回复。
const manualMarkRetention = 24 * time.Hour

// manualEchoFreshWindow 是自身回显的创建时间与本地时间允许的最大偏差；超出视为重连同步的历史消息，不能触发人工暂停。
const manualEchoFreshWindow = 2 * time.Minute

// manualNotifyDedupWindow 是同一会话重复发送“买家请求人工客服”通知的最短间隔，防止买家连发关键词刷屏。
const manualNotifyDedupWindow = 10 * time.Minute

// maxSystemSentChats 限制系统出站登记的会话数量；超过后淘汰最早登记的会话，防止回显长期缺失时内存无界增长。
const maxSystemSentChats = 2048

// maxSystemSentPerChat 限制单会话未被回显消费的出站登记条数。
const maxSystemSentPerChat = 32

// maxManualMarkedChats 限制人工发送时间与通知去重记录的会话数量。
const maxManualMarkedChats = 4096

// maxSeenManualEchoKeys 限制已处理自身回显平台消息 ID 的去重缓存规模。
const maxSeenManualEchoKeys = 4096

// systemSentRecord 是一条由本系统发出、尚未被自身回显消费的出站消息摘要。
type systemSentRecord struct {
	// messageType 是 text、image 或 item；与自身回显解析出的类型可比较。
	messageType string
	// content 是文本正文；非文本消息不参与正文比较，只保留类型。正文不写日志。
	content string
	// at 是登记时刻，用于 TTL 清理与选择最早的匹配项。
	at time.Time
}

// manualTakeoverTracker 由单个账号运行时拥有，判断自身回显是否来自本系统之外的人工发送，
// 并记录每个会话最近一次人工介入时间供回复链门禁读取。状态只存内存，账号重建或服务重启后清空。
// mu 保护全部 map 与切片；锁内只做内存比较和清理，不执行日志、数据库或网络 I/O，也不获取其他锁。
type manualTakeoverTracker struct {
	// mu 串行化出站登记、回显判定、人工标记和门禁读取。
	mu sync.Mutex
	// systemSent 按去除协议后缀的会话 ID 保存本系统已发出、等待回显消费的消息，切片按登记时间升序。
	systemSent map[string][]systemSentRecord
	// lastManualAt 按会话保存最近一次人工发送时间。
	lastManualAt map[string]time.Time
	// lastNotifiedAt 按会话保存最近一次发送“请求人工客服”通知的时间，用于通知去重。
	lastNotifiedAt map[string]time.Time
	// seenEchoKeys 保存已经参与过人工判定的平台消息 ID，防止同一回显重复到达时被二次判定。
	seenEchoKeys map[string]struct{}
	// seenEchoOrder 保存 seenEchoKeys 的登记顺序，用于有界淘汰。
	seenEchoOrder []string
	// now 是可注入时钟，测试用来模拟窗口过期；构造后只读。
	now func() time.Time
}

// newManualTakeoverTracker 创建账号级人工介入跟踪器。
func newManualTakeoverTracker() *manualTakeoverTracker {
	return &manualTakeoverTracker{
		systemSent:     make(map[string][]systemSentRecord),
		lastManualAt:   make(map[string]time.Time),
		lastNotifiedAt: make(map[string]time.Time),
		seenEchoKeys:   make(map[string]struct{}),
		now:            time.Now,
	}
}

// recordSystemSend 在写入 WebSocket 之前登记一条本系统出站消息，使随后到达的自身回显不被误判为人工发送。
// chatID 是平台会话 ID；messageType 是 text/image/item；content 是文本正文（非文本可为空）。
func (t *manualTakeoverTracker) recordSystemSend(chatID, messageType, content string) {
	if t == nil {
		return
	}
	// key 是去除协议后缀后的会话 ID，与回显解析结果可比较。
	key := normalizeOutgoingIdentity(chatID)
	if key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// now 是本次登记时刻。
	now := t.now()
	t.pruneSystemSentLocked(now)
	// records 是该会话尚未被消费的登记，超过单会话上限时丢弃最早一条。
	records := t.systemSent[key]
	if len(records) >= maxSystemSentPerChat {
		records = records[1:]
	}
	t.systemSent[key] = append(records, systemSentRecord{messageType: normalizeOutgoingEchoType(messageType), content: strings.TrimSpace(content), at: now})
	t.evictOldestSystemChatLocked()
}

// observeOwnEcho 判定一条自身 WebSocket 回显是否为人工发送；是则记录人工介入时间并返回 true。
// sentAt 是平台消息创建时间（零值表示未知）；本系统发出的消息会消费一条登记并返回 false。
// 判定原则是宁可漏判人工也不误判：误判会让 AI 被自己的回复暂停。
func (t *manualTakeoverTracker) observeOwnEcho(echo OutgoingChatMessage, sentAt time.Time) bool {
	if t == nil {
		return false
	}
	// chatID 是去除协议后缀后的回显会话 ID。
	chatID := normalizeOutgoingIdentity(echo.ChatID)
	if chatID == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// now 是本次判定时刻。
	now := t.now()
	t.pruneSystemSentLocked(now)
	// messageKey 是平台消息唯一 ID；同一 ID 只判定一次，防止推送重复到达把系统消息的第二份回显误判为人工。
	messageKey := strings.TrimSpace(echo.MessageKey)
	if messageKey != "" {
		// seen 表示该平台消息 ID 是否已经判定过。
		if _, seen := t.seenEchoKeys[messageKey]; seen {
			return false
		}
		t.rememberEchoKeyLocked(messageKey)
	}
	if t.consumeSystemSentLocked(chatID, echo) {
		return false
	}
	if !sentAt.IsZero() && absDuration(now.Sub(sentAt)) > manualEchoFreshWindow {
		return false
	}
	t.lastManualAt[chatID] = now
	t.evictOldestTimeLocked(t.lastManualAt)
	return true
}

// markManual 直接记录会话人工介入，用于网页聊天的人工发送或关键词转人工；at 为介入时刻。
func (t *manualTakeoverTracker) markManual(chatID string, at time.Time) {
	if t == nil {
		return
	}
	// key 是去除协议后缀后的会话 ID。
	key := normalizeOutgoingIdentity(chatID)
	if key == "" {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	t.lastManualAt[key] = at
	t.evictOldestTimeLocked(t.lastManualAt)
}

// manualSince 返回会话最近一次人工介入时间；ok 为 false 表示保留期内没有人工介入。
func (t *manualTakeoverTracker) manualSince(chatID string) (time.Time, bool) {
	if t == nil {
		return time.Time{}, false
	}
	// key 是去除协议后缀后的会话 ID。
	key := normalizeOutgoingIdentity(chatID)
	t.mu.Lock()
	defer t.mu.Unlock()
	// at、ok 是该会话的人工介入记录。
	at, ok := t.lastManualAt[key]
	if !ok {
		return time.Time{}, false
	}
	if t.now().Sub(at) >= manualMarkRetention {
		delete(t.lastManualAt, key)
		return time.Time{}, false
	}
	return at, true
}

// isPaused 判断会话在 window 时长的人工介入暂停窗口内；window 非正表示功能关闭。
func (t *manualTakeoverTracker) isPaused(chatID string, window time.Duration) bool {
	if t == nil || window <= 0 {
		return false
	}
	// at、ok 是最近一次人工介入时间。
	at, ok := t.manualSince(chatID)
	return ok && t.now().Sub(at) < window
}

// claimManualNotification 领取会话的“请求人工客服”通知发送权；去重窗口内重复领取返回 false。
func (t *manualTakeoverTracker) claimManualNotification(chatID string) bool {
	if t == nil {
		return false
	}
	// key 是去除协议后缀后的会话 ID。
	key := normalizeOutgoingIdentity(chatID)
	if key == "" {
		return false
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	// now 是本次领取时刻。
	now := t.now()
	// last、ok 是该会话上次通知时间。
	if last, ok := t.lastNotifiedAt[key]; ok && now.Sub(last) < manualNotifyDedupWindow {
		return false
	}
	t.lastNotifiedAt[key] = now
	t.evictOldestTimeLocked(t.lastNotifiedAt)
	return true
}

// consumeSystemSentLocked 为回显寻找并移除一条匹配的系统出站登记；调用方必须持有 mu。
// 优先按同类型同正文匹配（文本允许平台摘要截断为前缀）；非文本只比较类型；
// 都不命中但会话里仍有非文本登记时也消费它，因为商品卡片等回显可能退化为文本摘要。
func (t *manualTakeoverTracker) consumeSystemSentLocked(chatID string, echo OutgoingChatMessage) bool {
	// records 是该会话等待消费的系统出站登记。
	records := t.systemSent[chatID]
	if len(records) == 0 {
		return false
	}
	// echoType、echoText 是回显的规范类型与文本摘要。
	echoType, echoText := normalizeOutgoingEchoType(echo.MessageType), strings.TrimSpace(echo.Text)
	// matched 是被选中的登记下标；-1 表示尚未找到。
	matched := -1
	// index、record 遍历按时间升序的登记，取最早的精确匹配。
	for index, record := range records {
		if record.messageType != echoType {
			continue
		}
		if echoType != "text" || record.content == echoText || (echoText != "" && strings.HasPrefix(record.content, echoText)) {
			matched = index
			break
		}
	}
	if matched < 0 {
		// index、record 退化为匹配任意非文本登记。
		for index, record := range records {
			if record.messageType != "text" {
				matched = index
				break
			}
		}
	}
	if matched < 0 {
		return false
	}
	records = append(records[:matched], records[matched+1:]...)
	if len(records) == 0 {
		delete(t.systemSent, chatID)
	} else {
		t.systemSent[chatID] = records
	}
	return true
}

// pruneSystemSentLocked 清理超过保留期的系统出站登记；调用方必须持有 mu。
func (t *manualTakeoverTracker) pruneSystemSentLocked(now time.Time) {
	// chatID、records 遍历每个会话的登记。
	for chatID, records := range t.systemSent {
		// firstValid 是第一条仍在保留期内的登记下标。
		firstValid := 0
		for firstValid < len(records) && now.Sub(records[firstValid].at) >= systemSentRetention {
			firstValid++
		}
		if firstValid == len(records) {
			delete(t.systemSent, chatID)
		} else if firstValid > 0 {
			t.systemSent[chatID] = records[firstValid:]
		}
	}
}

// evictOldestSystemChatLocked 在会话数量超限时淘汰最早登记的会话；调用方必须持有 mu。
func (t *manualTakeoverTracker) evictOldestSystemChatLocked() {
	for len(t.systemSent) > maxSystemSentChats {
		// oldestChat、oldestAt 保存首条登记最早的会话。
		oldestChat, oldestAt := "", time.Time{}
		// chatID、records 遍历候选会话。
		for chatID, records := range t.systemSent {
			if len(records) > 0 && (oldestAt.IsZero() || records[0].at.Before(oldestAt)) {
				oldestChat, oldestAt = chatID, records[0].at
			}
		}
		delete(t.systemSent, oldestChat)
	}
}

// evictOldestTimeLocked 在时间记录表超限时淘汰最早的会话；调用方必须持有 mu。
func (t *manualTakeoverTracker) evictOldestTimeLocked(values map[string]time.Time) {
	for len(values) > maxManualMarkedChats {
		// oldestChat、oldestAt 保存时间最早的会话。
		oldestChat, oldestAt := "", time.Time{}
		// chatID、at 遍历候选会话。
		for chatID, at := range values {
			if oldestAt.IsZero() || at.Before(oldestAt) {
				oldestChat, oldestAt = chatID, at
			}
		}
		delete(values, oldestChat)
	}
}

// rememberEchoKeyLocked 登记一个已判定的平台消息 ID，超限时淘汰最早一条；调用方必须持有 mu。
func (t *manualTakeoverTracker) rememberEchoKeyLocked(messageKey string) {
	if len(t.seenEchoOrder) >= maxSeenManualEchoKeys {
		delete(t.seenEchoKeys, t.seenEchoOrder[0])
		t.seenEchoOrder = t.seenEchoOrder[1:]
	}
	t.seenEchoKeys[messageKey] = struct{}{}
	t.seenEchoOrder = append(t.seenEchoOrder, messageKey)
}

// absDuration 返回时长绝对值，用于比较回显创建时间与本地时间的双向偏差。
func absDuration(value time.Duration) time.Duration {
	if value < 0 {
		return -value
	}
	return value
}

// automaticReplyContextKey 标记本次外发来自自动回复链（关键词、默认、商品首次、Agent、AI）。
type automaticReplyContextKey struct{}

// WithAutomaticReply 标记出站消息由自动回复链发出；未带该标记且未要求回显确认的发送视为网页聊天的人工发送。
// ctx 是本次发送的取消边界；返回值只增加内部标记，不携带正文或凭证。
func WithAutomaticReply(ctx context.Context) context.Context {
	return context.WithValue(ctx, automaticReplyContextKey{}, true)
}

// IsAutomaticReply 报告上下文是否带有自动回复链标记；供跨包装配方核对标记确实沿发送链传递。
func IsAutomaticReply(ctx context.Context) bool {
	// automatic 表示调用方是否显式标记为自动回复。
	automatic, _ := ctx.Value(automaticReplyContextKey{}).(bool)
	return automatic
}

// isAutomaticSend 判断发送是否由程序自动发起：自动回复链或要求回显确认的自动化中心发送。
func isAutomaticSend(ctx context.Context) bool {
	return IsAutomaticReply(ctx) || wantsOutgoingEchoConfirmation(ctx)
}

// echoCreatedAt 从已解密平台帧读取消息创建时间；平台使用 Unix 毫秒，小于 1e12 的值按秒兼容，缺失返回零值。
func echoCreatedAt(decrypted map[string]any) time.Time {
	// m1 是平台聊天信封主体。
	m1, ok := decrypted["1"].(map[string]any)
	if !ok {
		return time.Time{}
	}
	// raw、parseErr 是按十进制整数解析的创建时间原值；平台可能下发数字或数字字符串，toString 统一为整数文本。
	raw, parseErr := strconv.ParseInt(strings.TrimSpace(toString(m1["5"])), 10, 64)
	if parseErr != nil || raw <= 0 {
		return time.Time{}
	}
	if raw < 1_000_000_000_000 {
		return time.Unix(raw, 0)
	}
	return time.UnixMilli(raw)
}
