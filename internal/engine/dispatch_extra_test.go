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
	// acc、h、cleanup 保存acc、h、cleanup，供当前处理流程使用
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// decrypted 保存decrypted，供当前处理流程使用
	decrypted := mustPaidDeliveryCard(t)
	acc.handleMessage(decrypted)

	// 系统事件应交给 handler.HandleSystemEvent（recordingHandler 目前不记录系统事件，
	// 但自动化 ExtractTaskFromWS 应识别为 TriggerOrderPaid——这里通过 handler 间接断言）。
	// 直接验证：handleMessage 不应触发任何防抖回复投递。
	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	// chats 保存chats，供当前处理流程使用
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

// HandleSystemEvent 处理系统Event。
func (s *systemCapturingHandler) HandleSystemEvent(_ context.Context, task automation.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tasks = append(s.tasks, task)
	return nil
}

// TestHandleMessage_SystemEventDispatchedToHandler 系统卡片经 handleMessage 进入 handler.HandleSystemEvent。
func TestHandleMessage_SystemEventDispatchedToHandler(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// h 保存h，供当前处理流程使用
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
	// acc、h、cleanup 保存acc、h、cleanup，供当前处理流程使用
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
	// acc、h、cleanup 保存acc、h、cleanup，供当前处理流程使用
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
	// acc、h、cleanup 保存acc、h、cleanup，供当前处理流程使用
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
	// acc、h、cleanup 保存acc、h、cleanup，供当前处理流程使用
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// decrypted 保存decrypted，供当前处理流程使用
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

// TestHandleMessage_IgnoresOwnWebSocketEcho 验证当前账号的 WS 回显不会触发自动回复。
func TestHandleMessage_IgnoresOwnWebSocketEcho(t *testing.T) {
	// acc 是待测账号；h 记录业务处理调用；cleanup 释放测试数据库和运行时资源。
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// newAccountForTest 使用 unb=123；模拟同一账号在闲鱼官方客户端发出的
	// 消息回显，不能进入自动回复或业务消息处理链。
	acc.handleMessage(plainChatMessage(t, "官方客户端发送", "123", "chat-own"))
	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.chats) != 0 {
		t.Fatalf("账号自身消息回显不应触发自动回复，got %d", len(h.chats))
	}
}

// TestHandleMessage_NoReminderNoOp 无 reminderContent 的消息不进入回复链也不报错。
func TestHandleMessage_NoReminderNoOp(t *testing.T) {
	// acc、h、cleanup 保存acc、h、cleanup，供当前处理流程使用
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
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	acc.mu.Lock()
	acc.lastMsgReceived = time.Time{}
	acc.mu.Unlock()

	// before 保存before，供当前处理流程使用
	before := time.Now()
	acc.dispatch(plainChatMessage(t, "hi", "b1", "c1"))
	// dispatch 内部起 goroutine，等其更新 lastMsgReceived。
	time.Sleep(50 * time.Millisecond)

	acc.mu.Lock()
	// last 保存last，供当前处理流程使用
	last := acc.lastMsgReceived
	acc.mu.Unlock()
	if last.IsZero() || last.Before(before) {
		t.Errorf("dispatch 应更新 lastMsgReceived，got %v", last)
	}
}

// TestDispatch_SemaphoreOverflowDrops 消息并发达上限时丢弃多余消息（不阻塞）。
func TestDispatch_SemaphoreOverflowDrops(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
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

// TestDispatch_SystemEventWaitsForCapacityInsteadOfDropping 负责TestDispatch系统EventWaitsForCapacityInsteadOfDropping相关处理。
func TestDispatch_SystemEventWaitsForCapacityInsteadOfDropping(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()
	// h 保存h，供当前处理流程使用
	h := &systemCapturingHandler{}
	acc.handler = h
	for // i 保存i，供当前处理流程使用
	i := 0; i < MessageSemaphoreSize; i++ {
		acc.sem <- struct{}{}
	}
	// event 保存event，供当前处理流程使用
	event := mustPaidDeliveryCard(t)
	// done 保存done，供当前处理流程使用
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
	// deadline 保存deadline，供当前处理流程使用
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		h.mu.Lock()
		// count 保存数量，供当前处理流程使用
		count := len(h.tasks)
		h.mu.Unlock()
		if count == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	h.mu.Lock()
	// count 保存数量，供当前处理流程使用
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
	// acc、h、cleanup 保存acc、h、cleanup，供当前处理流程使用
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// chat1 保存chat1，供当前处理流程使用
	chat1 := ChatMessage{AccountID: "cid", ChatID: "chat-a", Text: "A1", SenderUserID: "b1"}
	// chat2 保存chat2，供当前处理流程使用
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
	// texts 保存texts，供当前处理流程使用
	texts := map[string]bool{}
	// c 表示当前遍历过程中的c
	for _, c := range h.chats {
		texts[c.Text] = true
	}
	if !texts["A1"] || !texts["B1"] {
		t.Errorf("应投递 A1 和 B1，got %+v", texts)
	}
}

// TestScheduleDebouncedReply_TimerReset 同一 chat_id 连续消息重置定时器，旧消息被跳过。
func TestScheduleDebouncedReply_TimerReset(t *testing.T) {
	// acc、h、cleanup 保存acc、h、cleanup，供当前处理流程使用
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// 第一条立即调度。
	first := ChatMessage{AccountID: "cid", ChatID: "chat-reset", Text: "first", SenderUserID: "b1"}
	first.CookieStr = "cookie"
	acc.scheduleDebouncedReply(first)

	// 在防抖延迟内再投递第二条，应取消第一条的定时器。
	time.Sleep(MessageDebounceDelay / 2)
	// second 保存second，供当前处理流程使用
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
	// acc、h、cleanup 保存acc、h、cleanup，供当前处理流程使用
	acc, h, _, cleanup := newAccountForTest(t)
	defer cleanup()

	// chat 保存聊天，供当前处理流程使用
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

// cancelAwareHandler 保存取消AwareHandler，供当前处理流程使用
type cancelAwareHandler struct {
	started  chan struct{}
	canceled chan struct{}
}

// HandleChatMessage 处理聊天消息。
func (h *cancelAwareHandler) HandleChatMessage(ctx context.Context, _ ChatMessage) error {
	close(h.started)
	<-ctx.Done()
	close(h.canceled)
	return ctx.Err()
}

// HandleSystemEvent 处理系统Event。
func (h *cancelAwareHandler) HandleSystemEvent(context.Context, automation.Task) error { return nil }

// OnPasswordLoginRefresh 负责On密码登录Refresh相关处理。
func (h *cancelAwareHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }

// OnAccountAlert 负责On账号Alert相关处理。
func (h *cancelAwareHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// TestStopCancelsInFlightReplyHandler 负责TestStopCancelsInFlight回复Handler相关处理。
func TestStopCancelsInFlightReplyHandler(t *testing.T) {
	// handler 保存handler，供当前处理流程使用
	handler := &cancelAwareHandler{started: make(chan struct{}), canceled: make(chan struct{})}
	// acc 保存acc，供当前处理流程使用
	acc := New(Config{CookieID: "cid", CookieStr: "unb=1", Handler: handler})
	acc.reply = nil
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	acc.lifecycle.start(ctx, cancel)
	acc.scheduleDebouncedReply(ChatMessage{ChatID: "cancel-chat", SenderUserID: "buyer", Text: "hi"})
	select {
	case <-handler.started:
	case <-time.After(MessageDebounceDelay + time.Second):
		t.Fatal("handler did not start")
	}
	// done 保存done，供当前处理流程使用
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
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// now 保存now，供当前处理流程使用
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
	if // ok 保存ok，供当前处理流程使用
	_, ok := acc.processed["old-msg"]; ok {
		t.Error("过期消息应被清理")
	}
	if // ok 保存ok，供当前处理流程使用
	_, ok := acc.processed["fresh-msg"]; !ok {
		t.Error("新鲜消息不应被清理")
	}
}

// TestCleanupDedupLocked_OverLimitDropsOldestHalf 超上限且都未过期时删最旧一半。
func TestCleanupDedupLocked_OverLimitDropsOldestHalf(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// now 保存now，供当前处理流程使用
	now := time.Now()
	acc.dedupMu.Lock()
	// 填入 ProcessedIDsMaxSize + 10 条新鲜消息，时间递增。
	total := ProcessedIDsMaxSize + 10
	for // i 保存i，供当前处理流程使用
	i := 0; i < total; i++ {
		// 时间按 i 递增，确保有明确的最旧一半。
		acc.processed["msg-"+itoa(i)] = now.Add(time.Duration(i) * time.Millisecond)
	}
	acc.cleanupDedupLocked(now)
	// remaining 保存remaining，供当前处理流程使用
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

// TestCleanupDedupLocked_TriggeredByMarkAndCheck markAndCheckDedup 在超上限时触发清理。
func TestCleanupDedupLocked_TriggeredByMarkAndCheck(t *testing.T) {
	// acc、cleanup 保存acc、cleanup，供当前处理流程使用
	acc, _, _, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// now 保存now，供当前处理流程使用
	now := time.Now()
	acc.dedupMu.Lock()
	// 预填 ProcessedIDsMaxSize 条过期消息，使下一次 markAndCheckDedup 触发清理。
	for i := 0; i < ProcessedIDsMaxSize; i++ {
		acc.processed["old-"+itoa(i)] = now.Add(-2 * time.Hour)
	}
	acc.dedupMu.Unlock()

	// 构造一条新消息触发清理。msgID 由 extractMessageID 提取，这里直接用现成样本。
	decrypted := plainChatMessage(t, "trigger", "b1", "c1")
	// chat 保存聊天，供当前处理流程使用
	chat := extractChatMessage(decrypted, "cid", "cookie")
	if !acc.markAndCheckDedup(decrypted, chat) {
		t.Fatal("新消息应允许处理")
	}

	acc.dedupMu.Lock()
	// remaining 保存remaining，供当前处理流程使用
	remaining := len(acc.processed)
	acc.dedupMu.Unlock()
	// 过期消息应被清理，仅剩新消息 1 条（或新消息+残留，但应远小于 ProcessedIDsMaxSize）。
	if remaining >= ProcessedIDsMaxSize {
		t.Errorf("清理应大幅缩减 processed，剩 %d", remaining)
	}
}

// TestScheduleDebouncedReply_HandlerErrorLogged reply.Handle 报错仅记录日志，不影响 handler 投递。
func TestScheduleDebouncedReply_HandlerErrorLogged(t *testing.T) {
	// acc、h、store、cleanup 保存acc、h、store、cleanup，供当前处理流程使用
	acc, h, store, cleanup := newAccountForTest(t)
	defer cleanup()
	defer acc.Stop()

	// 让 reply.Handle 走默认回复路径并成功发送（sender 是 acc 自身，无 conn 会失败但仅记录日志）。
	// 这里主要验证防抖回调路径在 reply 报错时仍会调用 handler.HandleChatMessage。
	_ = store
	// chat 保存聊天，供当前处理流程使用
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
// plainChatMessage 负责plain聊天消息相关处理。
func plainChatMessage(t *testing.T, text, senderUserID, chatID string) map[string]any {
	t.Helper()
	// s 保存s，供当前处理流程使用
	s := `{"1":{"2":"` + chatID + `@goofish","10":{"bizTag":"{\"messageId\":\"msg-` + chatID + `\"}","reminderContent":"` + text + `","senderUserId":"` + senderUserID + `","senderNick":"买家","reminderUrl":"fleamarket://message_chat?itemId=item-` + chatID + `&peerUserId=` + senderUserID + `"}}}`
	// m 保存m，供当前处理流程使用
	var m map[string]any
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal([]byte(s), &m); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	return m
}

// TestExtractMessageReadEvent40103 验证紧凑和批量 40103 已读事件均能生成规范回执。
func TestExtractMessageReadEvent40103(t *testing.T) {
	// event 是单条紧凑回执的解析结果；ok 表示该输入已被识别为已读事件。
	event, ok := extractMessageReadEvent(map[string]any{
		"1": "4263141580162.PNM", "2": 2, "3": 0,
		"4": "64725235816@goofish", "5": 1, "6": 1786945729928,
	})
	if !ok || event.MessageID != "4263141580162.PNM" || event.ChatID != "64725235816" || event.ReadAt <= 0 {
		t.Fatalf("40103 event=%+v ok=%v", event, ok)
	}
	// batch 是批量编码回执的解析结果；ok 复用为该回执的事件识别结果。
	batch, ok := extractMessageReadEvent(map[string]any{
		"1": []any{"4263107993838.PNM"}, "2": 2, "3": "64725235816@goofish", "4": 1,
	})
	if !ok || batch.MessageID != "4263107993838.PNM" || batch.ChatID != "64725235816" {
		t.Fatalf("40103 batch event=%+v ok=%v", batch, ok)
	}
}
