package engine

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"
)

// fakeClock 是可手动推进的测试时钟，替代 tracker.now 以确定性地模拟窗口过期。
type fakeClock struct {
	// mu 保护 current，允许并发测试读取时钟。
	mu sync.Mutex
	// current 是当前模拟时间。
	current time.Time
}

// now 返回当前模拟时间。
func (c *fakeClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// advance 把模拟时间向前推进 d。
func (c *fakeClock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.current = c.current.Add(d)
}

// newTestManualTakeover 创建使用模拟时钟的跟踪器。
func newTestManualTakeover() (*manualTakeoverTracker, *fakeClock) {
	// clock 是固定起点的模拟时钟。
	clock := &fakeClock{current: time.Date(2026, 11, 11, 10, 0, 0, 0, time.UTC)}
	// tracker 是被测跟踪器。
	tracker := newManualTakeoverTracker()
	tracker.now = clock.now
	return tracker, clock
}

// TestManualTakeoverSystemEchoIsNotManual 验证本系统登记的文本、图片、商品卡片回显被消费，不判为人工；同会话第二份同文回显才判为人工。
func TestManualTakeoverSystemEchoIsNotManual(t *testing.T) {
	// tracker 是被测跟踪器。
	tracker, _ := newTestManualTakeover()
	tracker.recordSystemSend("chat-1@goofish", "text", " 您好 ")
	tracker.recordSystemSend("chat-1", "image", "https://img.example/a.png")
	tracker.recordSystemSend("chat-1", "item", "")
	if tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-1", Text: "您好"}, time.Time{}) {
		t.Fatal("系统文本回显不应判为人工")
	}
	if tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-1", MessageType: "image", Content: "https://cdn.example/other.png"}, time.Time{}) {
		t.Fatal("系统图片回显（CDN 地址不同）不应判为人工")
	}
	// 商品卡片回显解析失败时退化为文本摘要，仍应消费非文本登记。
	if tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-1", Text: "[商品卡片]"}, time.Time{}) {
		t.Fatal("退化为文本摘要的商品卡片回显不应判为人工")
	}
	// ok 表示该会话在保留期内是否存在人工介入记录。
	if _, ok := tracker.manualSince("chat-1"); ok {
		t.Fatal("系统回显后不应存在人工介入记录")
	}
	// 登记已全部消费，同文的再次发送来自人工。
	if !tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-1", Text: "您好"}, time.Time{}) {
		t.Fatal("没有登记的同文回显应判为人工")
	}
	// ok 表示该会话在保留期内是否存在人工介入记录。
	if _, ok := tracker.manualSince("chat-1@goofish"); !ok {
		t.Fatal("人工回显后应记录人工介入")
	}
}

// TestManualTakeoverTruncatedTextAndOtherChat 验证平台截断的文本摘要仍能匹配登记，而其他会话的回显不会消费本会话登记。
func TestManualTakeoverTruncatedTextAndOtherChat(t *testing.T) {
	// tracker 是被测跟踪器。
	tracker, _ := newTestManualTakeover()
	tracker.recordSystemSend("chat-a", "text", "这是一段很长的自动回复内容")
	if !tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-b", Text: "这是一段很长的自动回复内容"}, time.Time{}) {
		t.Fatal("其他会话的回显不能消费本会话登记")
	}
	if tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-a", Text: "这是一段很长"}, time.Time{}) {
		t.Fatal("截断为前缀的系统回显不应判为人工")
	}
	if !tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-a", Text: "完全不同的人工消息"}, time.Time{}) {
		t.Fatal("正文不同的回显应判为人工")
	}
	if tracker.observeOwnEcho(OutgoingChatMessage{ChatID: " ", Text: "x"}, time.Time{}) {
		t.Fatal("缺少会话 ID 的回显不应判为人工")
	}
}

// TestManualTakeoverDuplicateKeyAndStaleEcho 验证同一平台消息 ID 只判定一次，以及超出新鲜窗口的历史同步回显不触发人工介入。
func TestManualTakeoverDuplicateKeyAndStaleEcho(t *testing.T) {
	// tracker、clock 是被测跟踪器及其模拟时钟。
	tracker, clock := newTestManualTakeover()
	tracker.recordSystemSend("chat-1", "text", "自动回复")
	// systemEcho 是同一条系统消息的推送回显，平台可能重复下发。
	systemEcho := OutgoingChatMessage{ChatID: "chat-1", Text: "自动回复", MessageKey: "pnm-1"}
	if tracker.observeOwnEcho(systemEcho, clock.now()) {
		t.Fatal("系统消息回显不应判为人工")
	}
	// 平台重复下发同一条回显时，第二份因消息 ID 已判定过而直接跳过。
	if tracker.observeOwnEcho(systemEcho, clock.now()) {
		t.Fatal("同一系统消息的重复回显不应判为人工")
	}
	// 重连同步的历史人工消息创建时间早于新鲜窗口，不能触发暂停。
	if tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-2", Text: "昨天的人工回复", MessageKey: "pnm-2"}, clock.now().Add(-time.Hour)) {
		t.Fatal("历史同步回显不应判为人工")
	}
	// 创建时间略晚于本地时钟（时钟偏差）仍在窗口内。
	if !tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-3", Text: "刚刚的人工回复", MessageKey: "pnm-3"}, clock.now().Add(30*time.Second)) {
		t.Fatal("新鲜窗口内的人工回显应判为人工")
	}
}

// TestManualTakeoverWindowAndRetention 验证暂停窗口按配置时长过期、窗口非正时关闭，以及保留期后记录被清理。
func TestManualTakeoverWindowAndRetention(t *testing.T) {
	// tracker、clock 是被测跟踪器及其模拟时钟。
	tracker, clock := newTestManualTakeover()
	tracker.markManual("chat-1", clock.now())
	if !tracker.isPaused("chat-1", 10*time.Minute) {
		t.Fatal("人工介入后应处于暂停窗口")
	}
	if tracker.isPaused("chat-1", 0) {
		t.Fatal("窗口为 0 时功能关闭")
	}
	if tracker.isPaused("chat-other", 10*time.Minute) {
		t.Fatal("其他会话不受影响")
	}
	clock.advance(10 * time.Minute)
	if tracker.isPaused("chat-1", 10*time.Minute) {
		t.Fatal("窗口到期后应恢复自动回复")
	}
	clock.advance(manualMarkRetention)
	// ok 表示该会话在保留期内是否存在人工介入记录。
	if _, ok := tracker.manualSince("chat-1"); ok {
		t.Fatal("超过保留期的人工记录应被清理")
	}
	tracker.markManual("", clock.now())
	// ok 表示该会话在保留期内是否存在人工介入记录。
	if _, ok := tracker.manualSince(""); ok {
		t.Fatal("空会话 ID 不应被记录")
	}
}

// TestManualTakeoverNotificationDedup 验证同一会话去重窗口内只领取一次通知，窗口后可再次领取，不同会话互不影响。
func TestManualTakeoverNotificationDedup(t *testing.T) {
	// tracker、clock 是被测跟踪器及其模拟时钟。
	tracker, clock := newTestManualTakeover()
	if !tracker.claimManualNotification("chat-1") {
		t.Fatal("首次通知应可领取")
	}
	if tracker.claimManualNotification("chat-1@goofish") {
		t.Fatal("去重窗口内不应重复领取")
	}
	if !tracker.claimManualNotification("chat-2") {
		t.Fatal("其他会话应可独立领取")
	}
	clock.advance(manualNotifyDedupWindow)
	if !tracker.claimManualNotification("chat-1") {
		t.Fatal("去重窗口后应可再次领取")
	}
	if tracker.claimManualNotification("") {
		t.Fatal("空会话 ID 不应领取通知")
	}
}

// TestManualTakeoverSystemSentRetentionAndBounds 验证登记过期后不再消费回显，以及单会话、会话数和消息 ID 缓存的有界淘汰。
func TestManualTakeoverSystemSentRetentionAndBounds(t *testing.T) {
	// tracker、clock 是被测跟踪器及其模拟时钟。
	tracker, clock := newTestManualTakeover()
	tracker.recordSystemSend("", "text", "缺少会话")
	if len(tracker.systemSent) != 0 {
		t.Fatal("缺少会话 ID 的出站不应登记")
	}
	tracker.recordSystemSend("chat-old", "text", "过期回复")
	tracker.recordSystemSend("chat-mixed", "text", "较早回复")
	clock.advance(systemSentRetention - time.Second)
	tracker.recordSystemSend("chat-mixed", "text", "较新回复")
	clock.advance(time.Second)
	// 同一会话只清理过期的较早登记，较新登记仍可消费回显。
	if tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-mixed", Text: "较新回复"}, time.Time{}) {
		t.Fatal("未过期登记应消费回显")
	}
	if !tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "chat-old", Text: "过期回复"}, time.Time{}) {
		t.Fatal("过期登记不能再消费回显")
	}
	// index 生成超过单会话上限的登记。
	for index := 0; index < maxSystemSentPerChat+5; index++ {
		tracker.recordSystemSend("chat-many", "text", fmt.Sprintf("m-%d", index))
	}
	// got 是实际保留的条目数量，用于断言容量上限。
	if got := len(tracker.systemSent["chat-many"]); got != maxSystemSentPerChat {
		t.Fatalf("单会话登记应有界: %d", got)
	}
	// index 生成超过会话数上限的登记。
	for index := 0; index < maxSystemSentChats+3; index++ {
		clock.advance(time.Millisecond)
		tracker.recordSystemSend(fmt.Sprintf("c-%d", index), "text", "x")
	}
	// got 是实际保留的条目数量，用于断言容量上限。
	if got := len(tracker.systemSent); got > maxSystemSentChats {
		t.Fatalf("登记会话数应有界: %d", got)
	}
	// index 生成超过人工记录上限的会话。
	for index := 0; index < maxManualMarkedChats+3; index++ {
		clock.advance(time.Millisecond)
		tracker.markManual(fmt.Sprintf("m-%d", index), clock.now())
	}
	// got 是实际保留的条目数量，用于断言容量上限。
	if got := len(tracker.lastManualAt); got > maxManualMarkedChats {
		t.Fatalf("人工记录应有界: %d", got)
	}
	// index 生成超过消息 ID 缓存上限的回显。
	for index := 0; index < maxSeenManualEchoKeys+3; index++ {
		tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "k", Text: "x", MessageKey: fmt.Sprintf("key-%d", index)}, time.Time{})
	}
	if len(tracker.seenEchoKeys) > maxSeenManualEchoKeys || len(tracker.seenEchoOrder) > maxSeenManualEchoKeys {
		t.Fatalf("消息 ID 缓存应有界: %d/%d", len(tracker.seenEchoKeys), len(tracker.seenEchoOrder))
	}
}

// TestManualTakeoverNilTrackerIsSafe 验证未装配跟踪器时所有方法安全返回零值。
func TestManualTakeoverNilTrackerIsSafe(t *testing.T) {
	// tracker 是未装配的空跟踪器。
	var tracker *manualTakeoverTracker
	tracker.recordSystemSend("c", "text", "x")
	tracker.markManual("c", time.Now())
	if tracker.observeOwnEcho(OutgoingChatMessage{ChatID: "c"}, time.Time{}) || tracker.isPaused("c", time.Minute) || tracker.claimManualNotification("c") {
		t.Fatal("空跟踪器应返回零值")
	}
	// ok 表示该会话在保留期内是否存在人工介入记录。
	if _, ok := tracker.manualSince("c"); ok {
		t.Fatal("空跟踪器不应有记录")
	}
}

// TestManualTakeoverConcurrentAccess 在 -race 下并发登记、判定与读取，验证 mu 覆盖全部共享状态。
func TestManualTakeoverConcurrentAccess(t *testing.T) {
	// tracker 是并发共享的跟踪器。
	tracker, _ := newTestManualTakeover()
	// wg 等待全部并发 goroutine 结束。
	var wg sync.WaitGroup
	// worker 是并发 goroutine 编号。
	for worker := 0; worker < 8; worker++ {
		wg.Add(1)
		go func(worker int) {
			defer wg.Done()
			// chatID 是当前 goroutine 使用的会话。
			chatID := fmt.Sprintf("chat-%d", worker%3)
			// index 是单个 goroutine 内的操作序号。
			for index := 0; index < 200; index++ {
				tracker.recordSystemSend(chatID, "text", "x")
				tracker.observeOwnEcho(OutgoingChatMessage{ChatID: chatID, Text: "x"}, time.Time{})
				tracker.isPaused(chatID, time.Minute)
				tracker.claimManualNotification(chatID)
			}
		}(worker)
	}
	wg.Wait()
}

// TestEchoCreatedAt 覆盖毫秒、秒、字符串和缺失四种创建时间格式。
func TestEchoCreatedAt(t *testing.T) {
	// got 是从回显帧解析出的创建时间。
	if got := echoCreatedAt(map[string]any{"1": map[string]any{"5": float64(1786945729928)}}); !got.Equal(time.UnixMilli(1786945729928)) {
		t.Fatalf("毫秒时间解析错误: %v", got)
	}
	// got 是从回显帧解析出的创建时间。
	if got := echoCreatedAt(map[string]any{"1": map[string]any{"5": "1786945729"}}); !got.Equal(time.Unix(1786945729, 0)) {
		t.Fatalf("秒级字符串时间解析错误: %v", got)
	}
	if !echoCreatedAt(map[string]any{"1": map[string]any{}}).IsZero() || !echoCreatedAt(map[string]any{"1": "x"}).IsZero() || !echoCreatedAt(map[string]any{"1": map[string]any{"5": "abc"}}).IsZero() {
		t.Fatal("缺失或非法时间应返回零值")
	}
}

// TestIsAutomaticSend 验证自动回复标记与自动化回显确认标记都视为程序发送，未标记视为网页聊天人工发送。
func TestIsAutomaticSend(t *testing.T) {
	if isAutomaticSend(context.Background()) {
		t.Fatal("未标记的发送应视为人工")
	}
	if !isAutomaticSend(WithAutomaticReply(context.Background())) || !isAutomaticSend(WithOutgoingEchoConfirmation(context.Background())) {
		t.Fatal("自动回复与自动化发送应视为程序发送")
	}
}
