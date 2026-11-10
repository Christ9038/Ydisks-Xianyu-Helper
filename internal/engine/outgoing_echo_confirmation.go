package engine

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
)

// outgoingEchoConfirmationTimeout 限制自动化发送等待自身 WebSocket 回显的最长时间；超时结果必须人工核对。
const outgoingEchoConfirmationTimeout = 5 * time.Second

// errOutgoingEchoUnconfirmed 表示平台发送请求结束后，在确认窗口内没有观察到匹配的自身回显。
var errOutgoingEchoUnconfirmed = errors.New("闲鱼出站消息回显未确认")

// outgoingEchoKey 按会话、消息类型和实际正文索引待确认的出站消息，避免不同会话的回显互相消费。
type outgoingEchoKey struct {
	// chatID 是平台会话标识，已去除闲鱼协议后缀。
	chatID string
	// messageType 是 text 或 image；未提供类型时按 text 兼容。
	messageType string
	// content 是文本正文或图片 URL，不保存到日志。
	content string
}

// outgoingEchoWaiter 表示一条已经登记、等待账号自身 WebSocket 回显的消息。
type outgoingEchoWaiter struct {
	// tracker 是拥有待确认集合的账号级状态；所有字段只在 tracker.mu 保护下修改。
	tracker *outgoingEchoTracker
	// key 是本等待项在 tracker.pending 中的索引。
	key outgoingEchoKey
	// buyerID 是预期接收人；平台回显缺失该字段时允许通过会话和正文继续确认。
	buyerID string
	// requestID 是本次发送请求的 mid；存在时优先精确匹配，防止同文并发响应按队列错配。
	requestID string
	// done 在匹配回显或等待项被取消时关闭，等待方只读该 channel。
	done chan struct{}
	// registeredAt 是等待项登记时刻，登记后不再修改，用于计算被忽略推送回显相对发送的到达延迟。
	registeredAt time.Time
	// ignoredPushAt 是首个正文匹配但因本等待项绑定请求 mid 而被忽略的无 mid 推送回显到达时刻；零值表示未出现。
	// 仅用于诊断日志，不参与确认判定；由 tracker.mu 保护。
	ignoredPushAt time.Time
}

// outgoingEchoDiagnostic 是一次回显观察产生的诊断事件，在释放 tracker.mu 之后再写日志。
type outgoingEchoDiagnostic struct {
	// latePush 表示一条无 mid 推送回显匹配到了已因超时或不确定而取消的带 mid 发送。
	latePush bool
	// requestID 是被取消发送的请求 mid，用于与超时日志和 WS 层诊断日志关联。
	requestID string
	// chatID 是推送回显所属会话，已去除协议后缀。
	chatID string
	// sinceCancel 是推送回显到达时距对应发送被取消的时长。
	sinceCancel time.Duration
}

// canceledEchoRecord 保存一条取消发送的迟到回显保护记录；requestID 为空表示历史正文兼容路径。
type canceledEchoRecord struct {
	// at 是取消发生时间，用于限定迟到回显保护窗口。
	at time.Time
	// requestID 是被取消发送的请求 mid；非空时只能拦截同一 mid 的迟到响应。
	requestID string
}

// outgoingEchoTracker 由单个账号运行时拥有，串行管理并发发送对应的自身回显等待项和迟到保护记录。
// tracker.mu 保护 pending、canceledPending 与平台消息去重缓存；不在锁内执行 Context 等待、日志或外部 I/O。
type outgoingEchoTracker struct {
	// mu 保护同一账号所有待确认消息的登记、匹配和移除。
	mu sync.Mutex
	// pending 按会话和正文保存尚未确认的自动化出站消息；切片保留相同正文的发送顺序。
	pending map[outgoingEchoKey][]*outgoingEchoWaiter
	// seenPlatformKeys 保存已经用于确认的闲鱼消息 ID，防止同一回显再次确认后续同文发送。
	seenPlatformKeys map[string]struct{}
	// seenPlatformOrder 保存消息 ID 的登记顺序，用于限制去重缓存的内存占用。
	seenPlatformOrder []string
	// canceledPending 保存已取消发送的匹配键及其请求身份；窗口内的迟到平台回显只能被丢弃，不能确认后续同文发送。
	canceledPending map[outgoingEchoKey][]canceledEchoRecord
	// logger 是可选的诊断日志器，在账号构造期间一次性设置、之后只读；为空时不输出诊断。日志只在释放 mu 后写出。
	logger *slog.Logger
}

// maxSeenPlatformKeys 限制单账号去重缓存规模；超过后只淘汰最早的已确认 ID。
const maxSeenPlatformKeys = 4096

// canceledEchoRetentionWindow 限制取消发送的迟到平台回显保护窗口，避免旧回显长期阻塞同文发送确认。
const canceledEchoRetentionWindow = 30 * time.Second

// maxCanceledEchoKeys 限制取消回显保护键的数量，避免唯一正文持续失败时状态无界增长。
const maxCanceledEchoKeys = 4096

// maxCanceledEchoRecordsPerKey 限制同一正文键的取消保护记录数量，避免重复失败把单个切片无限扩大。
const maxCanceledEchoRecordsPerKey = 64

// newOutgoingEchoTracker 创建账号级出站回显确认器。
func newOutgoingEchoTracker() *outgoingEchoTracker {
	return &outgoingEchoTracker{pending: make(map[outgoingEchoKey][]*outgoingEchoWaiter), seenPlatformKeys: make(map[string]struct{}), seenPlatformOrder: make([]string, 0, maxSeenPlatformKeys), canceledPending: make(map[outgoingEchoKey][]canceledEchoRecord)}
}

// observePlatform 接收带平台消息 ID 的发送响应；同一 ID 只允许消费一个等待项。诊断日志在释放锁后输出。
func (t *outgoingEchoTracker) observePlatform(message OutgoingChatMessage) {
	if t == nil {
		return
	}
	t.logDiagnostic(t.observePlatformLocked(message))
}

// observePlatformLocked 在 tracker.mu 内完成带平台消息 ID 回显的匹配与去重，返回需要在锁外写出的诊断事件。
func (t *outgoingEchoTracker) observePlatformLocked(message OutgoingChatMessage) outgoingEchoDiagnostic {
	// platformKey 保存平台消息唯一 ID，用于跨响应和推送去重。
	platformKey := strings.TrimSpace(message.MessageKey)
	t.mu.Lock()
	defer t.mu.Unlock()
	if platformKey != "" && strings.TrimSpace(message.RequestID) == "" && t.hasCorrelatedWaiterLocked(message) {
		// 无请求 mid 的推送不能提前占用 PNM；随后到达的带 mid 发送响应仍需有机会确认对应等待器。
		t.markIgnoredPushLocked(message)
		return outgoingEchoDiagnostic{}
	}
	if platformKey != "" {
		// exists 表示该平台消息 ID 是否已经消费过一个等待项。
		if _, exists := t.seenPlatformKeys[platformKey]; exists {
			return outgoingEchoDiagnostic{}
		}
	}
	// matched、diagnostic 表示当前消息是否实际确认了等待器，以及未确认时的迟到推送诊断；只有确实归属当前事件时才登记 PNM 去重。
	matched, diagnostic := t.observeLocked(message)
	if !matched || platformKey == "" {
		return diagnostic
	}
	if len(t.seenPlatformOrder) >= maxSeenPlatformKeys {
		// oldestKey 保存最早登记的消息 ID，淘汰它以避免长期运行的账号无限增长内存。
		oldestKey := t.seenPlatformOrder[0]
		delete(t.seenPlatformKeys, oldestKey)
		t.seenPlatformOrder = t.seenPlatformOrder[1:]
	}
	t.seenPlatformKeys[platformKey] = struct{}{}
	t.seenPlatformOrder = append(t.seenPlatformOrder, platformKey)
	return diagnostic
}

// markIgnoredPushLocked 为与无 mid 推送正文匹配、但绑定了请求 mid 的等待项记录首次被忽略的推送时刻；调用方必须持有 tracker.mu。
// 该记录只供超时日志判断“推送回显其实已到达”，不改变确认结果。
func (t *outgoingEchoTracker) markIgnoredPushLocked(message OutgoingChatMessage) {
	// key 是当前推送按会话、类型和正文构造的索引。
	key := outgoingEchoKey{chatID: normalizeOutgoingIdentity(message.ChatID), messageType: normalizeOutgoingEchoType(message.MessageType), content: outgoingEchoContent(message)}
	// now 是本次推送的到达时刻。
	now := time.Now()
	// waiter 是同键下的单个等待项；只标记绑定了 mid 且尚未记录过的等待项。
	for _, waiter := range t.pending[key] {
		if waiter.requestID != "" && waiter.ignoredPushAt.IsZero() {
			waiter.ignoredPushAt = now
		}
	}
}

// ignoredPushDelay 返回首个被忽略的推送回显相对等待项登记的到达延迟；ok 为 false 表示确认窗口内没有出现这类推送。
func (w *outgoingEchoWaiter) ignoredPushDelay() (delay time.Duration, ok bool) {
	if w == nil || w.tracker == nil {
		return 0, false
	}
	w.tracker.mu.Lock()
	defer w.tracker.mu.Unlock()
	if w.ignoredPushAt.IsZero() {
		return 0, false
	}
	return w.ignoredPushAt.Sub(w.registeredAt), true
}

// logDiagnostic 在锁外输出迟到推送回显诊断；未配置日志器或没有诊断事件时直接返回。日志不含消息正文。
func (t *outgoingEchoTracker) logDiagnostic(diagnostic outgoingEchoDiagnostic) {
	if t == nil || t.logger == nil || !diagnostic.latePush {
		return
	}
	t.logger.Info("已取消的自动化发送收到迟到推送回显", "log_source", "echo_diag", "request_mid", diagnostic.requestID,
		"chat_id", diagnostic.chatID, "since_cancel_ms", diagnostic.sinceCancel.Milliseconds())
}

// hasCorrelatedWaiterLocked 判断同一正文键下是否存在已经绑定请求 mid 的等待器；调用方必须持有 tracker.mu。
func (t *outgoingEchoTracker) hasCorrelatedWaiterLocked(message OutgoingChatMessage) bool {
	// key 是当前回显按会话、类型和正文构造的索引。
	key := outgoingEchoKey{chatID: normalizeOutgoingIdentity(message.ChatID), messageType: normalizeOutgoingEchoType(message.MessageType), content: outgoingEchoContent(message)}
	// waiter 表示当前正文键下的单个回显等待项。
	for _, waiter := range t.pending[key] {
		if waiter.requestID != "" {
			return true
		}
	}
	return false
}

// observeMessage 统一处理 WebSocket 自身回显；带平台消息 ID 的回显必须走去重路径。
func (t *outgoingEchoTracker) observeMessage(message OutgoingChatMessage) {
	if strings.TrimSpace(message.MessageKey) != "" {
		t.observePlatform(message)
		return
	}
	t.observe(message)
}

// register 登记一条待确认消息；调用方必须在写入 WebSocket 前调用，避免回显先到造成竞态。
func (t *outgoingEchoTracker) register(chatID, buyerID, messageType, content string, requestIDs ...string) *outgoingEchoWaiter {
	if t == nil {
		return nil
	}
	// normalizedType、normalizedContent 保存与回显解析器一致的非空比较字段。
	normalizedType, normalizedContent := normalizeOutgoingEchoType(messageType), strings.TrimSpace(content)
	// requestID 保存可选的本次发送请求 mid；历史调用未提供时继续使用正文匹配兼容路径。
	requestID := ""
	if len(requestIDs) > 0 {
		requestID = strings.TrimSpace(requestIDs[0])
	}
	// waiter 保存本次发送的匹配键和完成信号；它的生命周期不超过一次发送调用。
	waiter := &outgoingEchoWaiter{tracker: t, key: outgoingEchoKey{chatID: normalizeOutgoingIdentity(chatID), messageType: normalizedType, content: normalizedContent}, buyerID: normalizeOutgoingIdentity(buyerID), requestID: requestID, done: make(chan struct{}), registeredAt: time.Now()}
	t.mu.Lock()
	t.pruneCanceledLocked(time.Now())
	t.pending[waiter.key] = append(t.pending[waiter.key], waiter)
	t.mu.Unlock()
	return waiter
}

// wait 在有限确认窗口内等待匹配回显；Context 取消同样返回未确认，禁止调用方安全重放。
func (w *outgoingEchoWaiter) wait(ctx context.Context, timeout time.Duration) error {
	if w == nil {
		return nil
	}
	if timeout <= 0 {
		timeout = outgoingEchoConfirmationTimeout
	}
	// timer 是本次回显确认的有限等待计时器；返回时必须释放底层计时资源。
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case <-w.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return errOutgoingEchoUnconfirmed
	}
}

// cancel 移除尚未匹配的等待项；发送失败或确认超时后必须调用，避免账号级状态无界增长。
func (w *outgoingEchoWaiter) cancel() {
	w.cancelWithLateProtection(true)
}

// cancelWithoutLateProtection 移除确定未发送的等待项，不为后续同文消息登记迟到回显保护。
func (w *outgoingEchoWaiter) cancelWithoutLateProtection() {
	w.cancelWithLateProtection(false)
}

// cancelWithLateProtection 按发送结果决定是否保留迟到回显保护记录。
func (w *outgoingEchoWaiter) cancelWithLateProtection(protectLateEcho bool) {
	if w == nil || w.tracker == nil {
		return
	}
	// t 是当前等待项所属的账号级跟踪器；其锁保护待确认列表。
	t := w.tracker
	t.mu.Lock()
	defer t.mu.Unlock()
	// waiters 是同一会话、消息类型和正文下仍未完成的等待项。
	waiters := t.pending[w.key]
	// index 标识当前候选在待确认列表中的位置；candidate 是待比较的等待项。
	for index, candidate := range waiters {
		if candidate != w {
			continue
		}
		waiters = append(waiters[:index], waiters[index+1:]...)
		if len(waiters) == 0 {
			delete(t.pending, w.key)
		} else {
			t.pending[w.key] = waiters
		}
		if protectLateEcho {
			// canceledAt 记录不确定发送的移除时间；后续同键迟到回显在保护窗口内必须先被消费掉。
			canceledAt := t.canceledPending[w.key]
			if len(canceledAt) >= maxCanceledEchoRecordsPerKey {
				// 丢弃最早记录以保持单键有界；更近的取消更可能对应仍在途的迟到回显。
				canceledAt = canceledAt[1:]
			}
			t.canceledPending[w.key] = append(canceledAt, canceledEchoRecord{at: time.Now(), requestID: w.requestID})
			t.pruneCanceledLocked(time.Now())
		}
		return
	}
}

// observe 接收消息分发器识别出的自身回显，并唤醒第一个匹配的自动化发送等待项。
// message 只包含已脱敏的会话、接收人和消息正文摘要；诊断日志只在释放锁后输出且不含正文。
func (t *outgoingEchoTracker) observe(message OutgoingChatMessage) {
	if t == nil {
		return
	}
	t.mu.Lock()
	t.pruneCanceledLocked(time.Now())
	// diagnostic 是锁内匹配产生的迟到推送诊断，释放锁后再写日志。
	_, diagnostic := t.observeLocked(message)
	t.mu.Unlock()
	t.logDiagnostic(diagnostic)
}

// observeLocked 在 tracker.mu 保护下处理一条自身回显；第一个返回值表示事件是否实际消费了等待器，
// 第二个返回值是未消费时需要在锁外输出的迟到推送诊断。调用方必须持有 tracker.mu，返回 false 时不得把平台消息 ID 登记为已消费。
func (t *outgoingEchoTracker) observeLocked(message OutgoingChatMessage) (bool, outgoingEchoDiagnostic) {
	// key 是当前回显按同一会话、类型和正文构造的索引。
	key := outgoingEchoKey{chatID: normalizeOutgoingIdentity(message.ChatID), messageType: normalizeOutgoingEchoType(message.MessageType), content: outgoingEchoContent(message)}
	// now、canceledAt 保存迟到回显保护窗口的清理基准和当前键的取消记录。
	now := time.Now()
	// canceledAt 保存当前匹配键尚未消费的取消发送记录。
	canceledAt := t.canceledPending[key]
	// requestID 是平台发送响应携带的请求级 mid；精确响应只检查同一请求的取消记录。
	requestID := strings.TrimSpace(message.RequestID)
	// firstValid 指向第一个仍处于迟到回显保护窗口内的取消记录。
	firstValid := 0
	for firstValid < len(canceledAt) && now.Sub(canceledAt[firstValid].at) >= canceledEchoRetentionWindow {
		firstValid++
	}
	if firstValid > 0 {
		canceledAt = canceledAt[firstValid:]
		if len(canceledAt) == 0 {
			delete(t.canceledPending, key)
		} else {
			t.canceledPending[key] = canceledAt
		}
	}
	// canceledIndex 指向与当前事件身份相符的取消记录；精确 mid 不得消费其他请求，正文推送也不得消费带 mid 的记录。
	canceledIndex := -1
	// index、record 分别表示取消保护记录的位置和当前记录内容。
	for index, record := range canceledAt {
		if record.requestID == requestID {
			canceledIndex = index
			break
		}
	}
	if canceledIndex >= 0 {
		// 先消费同一请求的取消记录，防止迟到回显确认新登记的同文发送。
		canceledAt = append(canceledAt[:canceledIndex], canceledAt[canceledIndex+1:]...)
		if len(canceledAt) == 0 {
			delete(t.canceledPending, key)
		} else {
			t.canceledPending[key] = canceledAt
		}
		return false, outgoingEchoDiagnostic{}
	}
	// waiters 是当前回显键对应的等待项；只消费一个与请求标识或历史正文规则匹配的等待项。
	waiters := t.pending[key]
	// 有值的 requestID 禁止退回正文 FIFO，避免同文并发错配。
	if requestID == "" {
		// hasCorrelatedWaiter 表示当前键下存在已经绑定请求 mid 的等待项；无 mid 的异步推送无法安全判断归属，不能消费它。
		hasCorrelatedWaiter := false
		// waiter 表示当前正文键下用于判断是否已绑定请求 mid 的等待项。
		for _, waiter := range waiters {
			if waiter.requestID != "" {
				hasCorrelatedWaiter = true
				break
			}
		}
		if hasCorrelatedWaiter {
			t.markIgnoredPushLocked(message)
			return false, outgoingEchoDiagnostic{}
		}
	}
	// index 标识候选等待项的位置；waiter 是将被本次回显唤醒的对象。
	for index, waiter := range waiters {
		if requestID != "" && waiter.requestID != requestID {
			continue
		}
		if waiter.buyerID != "" && normalizeOutgoingIdentity(message.BuyerID) != "" && waiter.buyerID != normalizeOutgoingIdentity(message.BuyerID) {
			continue
		}
		waiters = append(waiters[:index], waiters[index+1:]...)
		if len(waiters) == 0 {
			delete(t.pending, key)
		} else {
			t.pending[key] = waiters
		}
		close(waiter.done)
		return true, outgoingEchoDiagnostic{}
	}
	if requestID != "" {
		// 带请求标识但没有对应等待器的响应不能消费其他同文请求，避免迟到或跨代响应错配。
		return false, outgoingEchoDiagnostic{}
	}
	// record 是同键下仍在保护窗口内的取消记录；无 mid 推送命中带 mid 的取消记录说明该发送很可能已经送达，只记录诊断，不消费记录。
	for _, record := range canceledAt {
		if record.requestID != "" {
			return false, outgoingEchoDiagnostic{latePush: true, requestID: record.requestID, chatID: key.chatID, sinceCancel: now.Sub(record.at)}
		}
	}
	return false, outgoingEchoDiagnostic{}
}

// pruneCanceledLocked 清理所有已过期的取消保护，并在键数量超限时淘汰最早记录。
// 调用方必须持有 tracker.mu；清理只修改账号级内存状态，不执行外部 I/O。
func (t *outgoingEchoTracker) pruneCanceledLocked(now time.Time) {
	if t == nil {
		return
	}
	// key、canceledAt 分别表示当前取消保护键及其时间序列。
	for key, canceledAt := range t.canceledPending {
		// firstValid 表示当前键第一个仍在保护窗口内的时间索引。
		firstValid := 0
		for firstValid < len(canceledAt) && now.Sub(canceledAt[firstValid].at) >= canceledEchoRetentionWindow {
			firstValid++
		}
		if firstValid == len(canceledAt) {
			delete(t.canceledPending, key)
			continue
		}
		if firstValid > 0 {
			t.canceledPending[key] = canceledAt[firstValid:]
		}
	}
	for len(t.canceledPending) > maxCanceledEchoKeys {
		// oldestKey 保存需要优先淘汰的最早取消记录键。
		var oldestKey outgoingEchoKey
		// oldest 保存需要优先淘汰的最早取消记录时间。
		var oldest time.Time
		// key、canceledAt 分别表示候选淘汰键及其时间序列。
		for key, canceledAt := range t.canceledPending {
			if len(canceledAt) == 0 {
				delete(t.canceledPending, key)
				continue
			}
			if oldest.IsZero() || canceledAt[0].at.Before(oldest) {
				// oldestKey、oldest 保存需要优先淘汰的最早取消记录。
				oldestKey, oldest = key, canceledAt[0].at
			}
		}
		if oldest.IsZero() {
			break
		}
		delete(t.canceledPending, oldestKey)
	}
}

// normalizeOutgoingIdentity 统一会话和用户标识的闲鱼协议后缀，保证发送参数与回显字段可比较。
func normalizeOutgoingIdentity(value string) string {
	return strings.TrimSuffix(strings.TrimSpace(value), "@goofish")
}

// normalizeOutgoingEchoType 统一回显消息类型；历史文本观察缺少类型时按 text 处理。
func normalizeOutgoingEchoType(messageType string) string {
	messageType = strings.TrimSpace(messageType)
	if messageType == "" {
		return "text"
	}
	return messageType
}

// outgoingEchoContent 返回回显的实际比较正文；文本优先使用 Text，图片和其他媒体使用 Content。
func outgoingEchoContent(message OutgoingChatMessage) string {
	if normalizeOutgoingEchoType(message.MessageType) == "text" {
		return strings.TrimSpace(message.Text)
	}
	return strings.TrimSpace(message.Content)
}
