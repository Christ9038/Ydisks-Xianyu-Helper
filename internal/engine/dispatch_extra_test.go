package engine

import (
	"context"
	"encoding/json"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/automation"
)

// TestHandleMessage_SystemEventRoutesToAutomation 付款待发货系统卡片进入自动化中心，不进入回复链。
func TestHandleMessage_SystemEventRoutesToAutomation(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	decrypted := mustPaidDeliveryCard(t)
	acc.handleMessage(decrypted)

	// 系统事件应交给 handler.HandleSystemEvent（recordingHandler 目前不记录系统事件，
	// 但自动化 ExtractTaskFromWS 应识别为 TriggerOrderPaid——这里通过 handler 间接断言）。
	// 直接验证：handleMessage 不应触发任何防抖回复投递。
	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	chats := len(h.chats)
	h.mu.Unlock()
	if chats != 0 {
		t.Errorf("系统卡片不应进入回复链，got %d 条聊天投递", chats)
	}
}

// systemCapturingHandler 捕获系统事件，用于断言 handleMessage 把系统卡片交给自动化。
type systemCapturingHandler struct {
	recordingHandler
	mu    sync.Mutex
	tasks []automation.Task
}

func (s *systemCapturingHandler) HandleSystemEvent(_ context.Context, task automation.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, task)
	return nil
}

// TestHandleMessage_SystemEventDispatchedToHandler 系统卡片经 handleMessage 进入 handler.HandleSystemEvent。
func TestHandleMessage_SystemEventDispatchedToHandler(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	h := &systemCapturingHandler{}
	acc.handler = h
	acc.handleMessage(mustPaidDeliveryCard(t))

	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.tasks) != 1 {
		t.Fatalf("应投递 1 个系统事件，got %d", len(h.tasks))
	}
	if h.tasks[0].TriggerType != automation.TriggerOrderPaid {
		t.Errorf("TriggerType=%q want %q", h.tasks[0].TriggerType, automation.TriggerOrderPaid)
	}
}

// TestHandleMessage_PlainChatRoutesToDebounce 普通聊天消息进入防抖回复链。
func TestHandleMessage_PlainChatRoutesToDebounce(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// 构造一条普通聊天消息（非系统卡片、非 contentType 14/26）。
	decrypted := plainChatMessage(t, "你好老板", "buyer1", "chat-plain")
	acc.handleMessage(decrypted)

	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 1 {
		t.Fatalf("应投递 1 条聊天消息，got %d", len(h.chats))
	}
	if h.chats[0].Text != "你好老板" {
		t.Errorf("Text=%q want 你好老板", h.chats[0].Text)
	}
}

// TestHandleMessage_ContentType14Filtered contentType=14 系统提示不进入回复链。
func TestHandleMessage_ContentType14Filtered(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	acc.handleMessage(mustContentType14Notice(t))
	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 0 {
		t.Errorf("contentType=14 不应进入回复链，got %d", len(h.chats))
	}
}

// TestHandleMessage_RefundCardFiltered contentType=26 退款卡片不进入回复链。
func TestHandleMessage_RefundCardFiltered(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	acc.handleMessage(mustRefundTradeCard(t))
	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 0 {
		t.Errorf("退款卡片不应进入回复链，got %d", len(h.chats))
	}
}

// TestHandleMessage_DedupSkipsRepeat 同一 msgID 的重复消息只处理一次。
func TestHandleMessage_DedupSkipsRepeat(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	decrypted := plainChatMessage(t, "重复消息", "buyer1", "chat-dup")
	acc.handleMessage(decrypted)
	acc.handleMessage(decrypted) // 重复
	acc.handleMessage(decrypted) // 再次重复

	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 1 {
		t.Errorf("去重后应只投递 1 条，got %d", len(h.chats))
	}
}

// TestHandleMessage_NoReminderNoOp 无 reminderContent 的消息不进入回复链也不报错。
func TestHandleMessage_NoReminderNoOp(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// 缺少 reminderContent 的消息。
	decrypted := map[string]any{
		"1": map[string]any{
			"2": "123@goofish",
			"10": map[string]any{
				"senderUserId": "b1",
			},
		},
	}
	acc.handleMessage(decrypted)
	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 0 {
		t.Errorf("无 reminder 不应投递，got %d", len(h.chats))
	}
}

// TestDispatch_UpdatesLastMsgReceived dispatch 记录消息接收时间。
func TestDispatch_UpdatesLastMsgReceived(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	acc.mu.Lock()
	acc.lastMsgReceived = time.Time{}
	acc.mu.Unlock()

	before := time.Now()
	acc.dispatch(plainChatMessage(t, "hi", "b1", "c1"))
	// dispatch 内部起 goroutine，等其更新 lastMsgReceived。
	time.Sleep(50 * time.Millisecond)

	acc.mu.Lock()
	last := acc.lastMsgReceived
	acc.mu.Unlock()
	if last.IsZero() || last.Before(before) {
		t.Errorf("dispatch 应更新 lastMsgReceived，got %v", last)
	}
}

// TestDispatch_SemaphoreOverflowDrops 消息并发达上限时丢弃多余消息（不阻塞）。
func TestDispatch_SemaphoreOverflowDrops(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// 把信号量填满，再投递一条：应走 default 分支丢弃，不阻塞。
	for i := 0; i < MessageSemaphoreSize; i++ {
		acc.sem <- struct{}{}
	}
	// 这条应被丢弃（dispatch 立即返回，不进入 handleMessage）。
	done := make(chan struct{})
	go func() {
		acc.dispatch(plainChatMessage(t, "overflow", "b1", "c1"))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("dispatch 在信号量满时应立即返回，不应阻塞")
	}

	// 释放信号量，避免 Stop 时泄漏 goroutine。
	for i := 0; i < MessageSemaphoreSize; i++ {
		<-acc.sem
	}
}

func TestDispatch_SystemEventWaitsForCapacityInsteadOfDropping(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()
	h := &systemCapturingHandler{}
	acc.handler = h
	for i := 0; i < MessageSemaphoreSize; i++ {
		acc.sem <- struct{}{}
	}
	event := mustPaidDeliveryCard(t)
	done := make(chan struct{})
	go func() {
		acc.dispatch(event)
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("system event was dropped while semaphore was full")
	case <-time.After(100 * time.Millisecond):
	}
	<-acc.sem
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("system event did not continue after capacity became available")
	}
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		count := len(h.tasks)
		h.mu.Unlock()
		if count == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.Lock()
	count := len(h.tasks)
	h.mu.Unlock()
	if count != 1 {
		t.Fatalf("system event count=%d want 1", count)
	}
	for len(acc.sem) > 0 {
		<-acc.sem
	}
}

// TestScheduleDebouncedReply_DifferentChatIDsNotCoalesced 不同 chat_id 的消息各自独立防抖，不合并。
func TestScheduleDebouncedReply_DifferentChatIDsNotCoalesced(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	chat1 := ChatMessage{AccountID: "cid", ChatID: "chat-a", Text: "A1", SenderUserID: "b1"}
	chat2 := ChatMessage{AccountID: "cid", ChatID: "chat-b", Text: "B1", SenderUserID: "b2"}
	chat1.CookieStr = "cookie"
	chat2.CookieStr = "cookie"

	acc.scheduleDebouncedReply(chat1)
	acc.scheduleDebouncedReply(chat2)

	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 2 {
		t.Fatalf("不同 chat_id 应各自投递，got %d 条: %+v", len(h.chats), h.chats)
	}
	texts := map[string]bool{}
	for _, c := range h.chats {
		texts[c.Text] = true
	}
	if !texts["A1"] || !texts["B1"] {
		t.Errorf("应投递 A1 和 B1，got %+v", texts)
	}
}

// TestScheduleDebouncedReply_TimerReset 同一 chat_id 连续消息重置定时器，旧消息被跳过。
func TestScheduleDebouncedReply_TimerReset(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// 第一条立即调度。
	first := ChatMessage{AccountID: "cid", ChatID: "chat-reset", Text: "first", SenderUserID: "b1"}
	first.CookieStr = "cookie"
	acc.scheduleDebouncedReply(first)

	// 在防抖延迟内再投递第二条，应取消第一条的定时器。
	time.Sleep(MessageDebounceDelay / 2)
	second := ChatMessage{AccountID: "cid", ChatID: "chat-reset", Text: "second", SenderUserID: "b1"}
	second.CookieStr = "cookie"
	acc.scheduleDebouncedReply(second)

	time.Sleep(MessageDebounceDelay + 300*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 1 {
		t.Fatalf("应只投递最后一条，got %d: %+v", len(h.chats), h.chats)
	}
	if h.chats[0].Text != "second" {
		t.Errorf("应投递 second，got %q", h.chats[0].Text)
	}
}

// TestScheduleDebouncedReply_StopClearsTimers Stop 后未触发的定时器被取消，不再投递。
func TestScheduleDebouncedReply_StopClearsTimers(t *testing.T) {
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()

	chat := ChatMessage{AccountID: "cid", ChatID: "chat-stop", Text: "won't deliver", SenderUserID: "b1"}
	chat.CookieStr = "cookie"
	acc.scheduleDebouncedReply(chat)
	// 立即 Stop，定时器应被取消。
	acc.Stop()

	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 0 {
		t.Errorf("Stop 后定时器应被取消，不应投递，got %d", len(h.chats))
	}
}

type cancelAwareHandler struct {
	started  chan struct{}
	canceled chan struct{}
}

func (h *cancelAwareHandler) HandleChatMessage(ctx context.Context, _ ChatMessage) error {
	close(h.started)
	<-ctx.Done()
	close(h.canceled)
	return ctx.Err()
}

func (h *cancelAwareHandler) HandleSystemEvent(context.Context, automation.Task) error       { return nil }
func (h *cancelAwareHandler) OnPasswordLoginRefresh(context.Context, string) bool            { return false }
func (h *cancelAwareHandler) OnAccountAlert(context.Context, string, string, string, string) {}

func TestStopCancelsInFlightReplyHandler(t *testing.T) {
	handler := &cancelAwareHandler{started: make(chan struct{}), canceled: make(chan struct{})}
	acc := New(Config{CookieID: "cid", CookieStr: "unb=1", Handler: handler})
	acc.reply = nil
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acc.mu.Lock()
	acc.stopFn = cancel
	acc.mu.Unlock()
	acc.taskMu.Lock()
	acc.runtimeCtx = ctx
	acc.taskMu.Unlock()
	acc.scheduleDebouncedReply(ChatMessage{ChatID: "cancel-chat", SenderUserID: "buyer", Text: "hi"})
	select {
	case <-handler.started:
	case <-time.After(MessageDebounceDelay + time.Second):
		t.Fatal("handler did not start")
	}
	done := make(chan struct{})
	go func() {
		acc.Stop()
		close(done)
	}()
	select {
	case <-handler.canceled:
	case <-time.After(time.Second):
		t.Fatal("in-flight handler context was not canceled")
	}
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop did not wait for handler exit")
	}
}

// TestCleanupDedupLocked_ExpiredRemoved 过期消息 ID 被清理。
func TestCleanupDedupLocked_ExpiredRemoved(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	now := time.Now()
	acc.dedupMu.Lock()
	// 一条过期（>1h）、一条新鲜。
	acc.processed["old-msg"] = now.Add(-2 * time.Hour)
	acc.processed["fresh-msg"] = now
	acc.dedupMu.Unlock()

	acc.dedupMu.Lock()
	acc.cleanupDedupLocked(now)
	acc.dedupMu.Unlock()

	acc.dedupMu.Lock()
	defer acc.dedupMu.Unlock()
	if _, ok := acc.processed["old-msg"]; ok {
		t.Error("过期消息应被清理")
	}
	if _, ok := acc.processed["fresh-msg"]; !ok {
		t.Error("新鲜消息不应被清理")
	}
}

// TestCleanupDedupLocked_OverLimitDropsOldestHalf 超上限且都未过期时删最旧一半。
func TestCleanupDedupLocked_OverLimitDropsOldestHalf(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	now := time.Now()
	acc.dedupMu.Lock()
	// 填入 ProcessedIDsMaxSize + 10 条新鲜消息，时间递增。
	total := ProcessedIDsMaxSize + 10
	for i := 0; i < total; i++ {
		// 时间按 i 递增，确保有明确的最旧一半。
		acc.processed["msg-"+itoa(i)] = now.Add(time.Duration(i) * time.Millisecond)
	}
	acc.cleanupDedupLocked(now)
	remaining := len(acc.processed)
	acc.dedupMu.Unlock()

	// 删最旧一半后：total - total/2 = (ProcessedIDsMaxSize+10) - (ProcessedIDsMaxSize+10)/2。
	want := total - total/2
	if remaining != want {
		t.Errorf("超上限删半后剩余 %d，want %d", remaining, want)
	}
}

// itoa 简单整数转字符串，避免引入 strconv。
func itoa(n int) string {
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

// TestCleanupDedupLocked_TriggeredByMarkAndCheck markAndCheckDedup 在超上限时触发清理。
func TestCleanupDedupLocked_TriggeredByMarkAndCheck(t *testing.T) {
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	now := time.Now()
	acc.dedupMu.Lock()
	// 预填 ProcessedIDsMaxSize 条过期消息，使下一次 markAndCheckDedup 触发清理。
	for i := 0; i < ProcessedIDsMaxSize; i++ {
		acc.processed["old-"+itoa(i)] = now.Add(-2 * time.Hour)
	}
	acc.dedupMu.Unlock()

	// 构造一条新消息触发清理。msgID 由 extractMessageID 提取，这里直接用现成样本。
	decrypted := plainChatMessage(t, "trigger", "b1", "c1")
	chat := extractChatMessage(decrypted, "cid", "cookie")
	if !acc.markAndCheckDedup(decrypted, chat) {
		t.Fatal("新消息应允许处理")
	}

	acc.dedupMu.Lock()
	remaining := len(acc.processed)
	acc.dedupMu.Unlock()
	// 过期消息应被清理，仅剩新消息 1 条（或新消息+残留，但应远小于 ProcessedIDsMaxSize）。
	if remaining >= ProcessedIDsMaxSize {
		t.Errorf("清理应大幅缩减 processed，剩 %d", remaining)
	}
}

// TestScheduleDebouncedReply_HandlerErrorLogged reply.Handle 报错仅记录日志，不影响 handler 投递。
func TestScheduleDebouncedReply_HandlerErrorLogged(t *testing.T) {
	acc, h, store, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// 让 reply.Handle 走默认回复路径并成功发送（sender 是 acc 自身，无 conn 会失败但仅记录日志）。
	// 这里主要验证防抖回调路径在 reply 报错时仍会调用 handler.HandleChatMessage。
	_ = store
	chat := ChatMessage{
		AccountID: "cid", ChatID: "chat-err", Text: "hi",
		SenderUserID: "b1", CookieStr: "unb=123;",
	}
	acc.scheduleDebouncedReply(chat)
	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 1 {
		t.Errorf("应投递到 handler，got %d", len(h.chats))
	}
}

// plainChatMessage 构造一条普通聊天消息（非系统卡片、contentType 非 14/26），
// 含 bizTag.messageId 以便去重提取。
func plainChatMessage(t *testing.T, text, senderUserID, chatID string) map[string]any {
	t.Helper()
	s := `{"1":{"2":"` + chatID + `@goofish","10":{"bizTag":"{\"messageId\":\"msg-` + chatID + `\"}","reminderContent":"` + text + `","senderUserId":"` + senderUserID + `","senderNick":"买家","reminderUrl":"fleamarket://message_chat?itemId=item-` + chatID + `&peerUserId=` + senderUserID + `"}}}`
	var m map[string]any
	if err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}
