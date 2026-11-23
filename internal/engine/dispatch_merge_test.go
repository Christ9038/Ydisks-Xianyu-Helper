package engine

import (
	"context"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// shortenMergeWindow 把测试账号的合并等待缩短到最小的 1 秒；账号行存在时默认是 3 秒，会让依赖固定等待的测试变慢。
func shortenMergeWindow(store *db.Store) {
	_, _ = store.DB.ExecContext(context.Background(), `UPDATE cookies SET reply_merge_seconds=1 WHERE id='cid'`)
}

// attachReplyService 给测试账号装配回复服务与记录型投递端口，并同步到消息分发器；返回投递记录。
// 通用账号夹具不带回复服务，而合并等待计时器只在存在回复服务时才有意义。
func attachReplyService(acc *Account, store *db.Store) *recordingDelivery {
	// delivery 记录回复服务发送的全部消息；service 是装配的回复服务。
	delivery := &recordingDelivery{}
	shortenMergeWindow(store)
	// service 是装配到账号上的回复服务。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)
	acc.reply = service
	acc.messageDispatcher.reply = service
	return delivery
}

// batchRecordingAI 是记录每次收到的合并消息的 AI 回复夹具，可按需阻塞以模拟生成耗时。
type batchRecordingAI struct {
	// mu 保护 seen 与 calls。
	mu sync.Mutex
	// seen 保存每次调用收到的合并文字，按调用顺序。
	seen []string
	// parts 保存每次调用收到的原始消息条数。
	parts []int
	// block 非空时调用会等到它关闭或 ctx 取消；started 在每次调用进入阻塞前收到信号。
	block   chan struct{}
	started chan struct{}
}

// Reply 记录合并文字；阻塞期间 ctx 被取消（新消息顶替）时返回取消错误，与真实模型调用一致。
func (a *batchRecordingAI) Reply(ctx context.Context, m ChatMessage) (*ReplyResult, error) {
	// turnCtx 是随新消息取消的生成上下文。
	turnCtx := m.Turn.Context(ctx)
	a.mu.Lock()
	a.seen = append(a.seen, m.Text)
	a.parts = append(a.parts, max(len(m.Parts), 1))
	a.mu.Unlock()
	if a.started != nil {
		a.started <- struct{}{}
	}
	if a.block != nil {
		select {
		case <-a.block:
		case <-turnCtx.Done():
			return nil, turnCtx.Err()
		}
	}
	return &ReplyResult{Text: "回复:" + m.Text}, nil
}

// calls 返回已记录的调用次数。
func (a *batchRecordingAI) calls() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return len(a.seen)
}

// newMergeFixture 构造带回复服务（可注入 AI）的分发器夹具：本地商品归属成立，合并等待缩短为 1 秒。
// 返回分发器、投递记录、业务处理器夹具和清理函数。
func newMergeFixture(t *testing.T, ai AIReplier) (*messageDispatcher, *recordingDelivery, *localOwnershipHandlerFixture, func()) {
	t.Helper()
	// store、cleanup 提供独立数据库；账号行的合并等待缩短为 1 秒。
	store, cleanup := newReplyStore(t)
	shortenMergeWindow(store)
	// delivery 记录发送；reply 是被测回复服务。
	delivery := &recordingDelivery{}
	// reply 是被测回复服务，使用注入的 AI 夹具。
	reply := NewReplyService("cid", store, delivery, nil, ai, nil)
	// observer 提供会话角色与本地商品归属，使 canAutoReply 通过。
	observer := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}, owned: true}
	// dispatcher 是被测消息分发器，合并等待取自账号设置。
	dispatcher := newMessageDispatcher(messageDispatcherConfig{
		CookieID: "cid", Reply: reply,
		CurrentCookie:  func() string { return "unb=123" },
		CurrentHandler: func() Handler { return observer },
	})
	return &dispatcher, delivery, observer, func() { dispatcher.stop(); cleanup() }
}

// waitFor 轮询条件直到成立或超时。
func waitFor(t *testing.T, what string, timeout time.Duration, condition func() bool) {
	t.Helper()
	// deadline 是等待截止时刻。
	deadline := time.Now().Add(timeout)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatalf("等待超时: %s", what)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// buyerMsg 构造同一会话、同一商品的买家消息。
func buyerMsg(text string) ChatMessage {
	return ChatMessage{AccountID: "cid", ChatID: "merge-chat", ItemID: "item", SenderUserID: "buyer", Text: text, MessageID: "id-" + text}
}

// TestDispatcherMergesSplitMessagesIntoOneReply 验证买家把一句话拆成多条发送：窗口内的消息合并成一次回复请求，只回复一次。
func TestDispatcherMergesSplitMessagesIntoOneReply(t *testing.T) {
	// ai 记录收到的合并文字。
	ai := &batchRecordingAI{}
	// dispatcher、delivery、observer、cleanup 是分发器、投递记录、业务处理器夹具与清理函数。
	dispatcher, delivery, observer, cleanup := newMergeFixture(t, ai)
	defer cleanup()
	// text 是买家依次发送的分段消息。
	for _, text := range []string{"你好", "这个还在吗", "能便宜点吗"} {
		dispatcher.scheduleDebouncedReply(buyerMsg(text))
		time.Sleep(100 * time.Millisecond)
	}
	waitFor(t, "合并回复发出", 5*time.Second, func() bool { return delivery.count() == 1 })
	time.Sleep(MessageDebounceDelay + 200*time.Millisecond)
	if ai.calls() != 1 || ai.seen[0] != "你好\n这个还在吗\n能便宜点吗" || ai.parts[0] != 3 {
		t.Fatalf("应只请求一次且带合并文字: calls=%d seen=%q parts=%v", ai.calls(), ai.seen, ai.parts)
	}
	if delivery.count() != 1 || delivery.messages[0].Text != "回复:你好\n这个还在吗\n能便宜点吗" {
		t.Fatalf("应只发送一条回复: %+v", delivery.messages)
	}
	observer.mu.Lock()
	defer observer.mu.Unlock()
	if len(observer.chats) != 3 {
		t.Fatalf("三条消息都应落库: %d", len(observer.chats))
	}
}

// TestDispatcherInterruptsGenerationAndMergesOnNewMessage 验证生成中（未发送）买家又发新消息：旧生成被取消且不发送，
// 新请求包含旧消息与新消息合并后的内容。
func TestDispatcherInterruptsGenerationAndMergesOnNewMessage(t *testing.T) {
	// ai 的第一次调用会阻塞直到被取消；之后不再阻塞。
	ai := &batchRecordingAI{block: make(chan struct{}), started: make(chan struct{}, 4)}
	// dispatcher、delivery、cleanup 是分发器、投递记录与清理函数。
	dispatcher, delivery, _, cleanup := newMergeFixture(t, ai)
	defer cleanup()
	dispatcher.scheduleDebouncedReply(buyerMsg("我想问个问题"))
	select {
	case <-ai.started:
	case <-time.After(5 * time.Second):
		t.Fatal("第一次生成未开始")
	}
	// 解除后续调用的阻塞：第二次生成不应再等待。
	go func() {
		<-ai.started
		close(ai.block)
	}()
	dispatcher.scheduleDebouncedReply(buyerMsg("就是价格"))
	waitFor(t, "合并后重新生成并发送", 8*time.Second, func() bool { return delivery.count() == 1 })
	time.Sleep(300 * time.Millisecond)
	if delivery.count() != 1 {
		t.Fatalf("被顶替的旧生成不应发送: %+v", delivery.messages)
	}
	if ai.calls() != 2 || ai.seen[0] != "我想问个问题" || ai.seen[1] != "我想问个问题\n就是价格" || ai.parts[1] != 2 {
		t.Fatalf("第二次请求应合并旧消息与新消息: seen=%q parts=%v", ai.seen, ai.parts)
	}
	if delivery.messages[0].Text != "回复:我想问个问题\n就是价格" {
		t.Fatalf("发送的应是合并后的回复: %q", delivery.messages[0].Text)
	}
}

// TestDispatcherHardMaxWaitStillReplies 验证买家持续连发时，从第一条算起超过硬上限也必定开始回复。
func TestDispatcherHardMaxWaitStillReplies(t *testing.T) {
	// dispatcher、cleanup 是分发器与清理函数。
	dispatcher, _, _, cleanup := newMergeFixture(t, &batchRecordingAI{})
	defer cleanup()
	// 第一条建立批次起点；把起点回拨到接近硬上限，再来一条消息，计时器应缩短到剩余时间而不是整个窗口。
	dispatcher.scheduleDebouncedReply(buyerMsg("a"))
	dispatcher.debounceMu.Lock()
	dispatcher.debounceTimers["merge-chat"].firstAt = time.Now().Add(-MessageDebounceMaxWait + 150*time.Millisecond)
	dispatcher.debounceMu.Unlock()
	// started 是第二条消息到达时刻；到期应远早于完整窗口（MessageDebounceDelay）。
	started := time.Now()
	dispatcher.scheduleDebouncedReply(buyerMsg("b"))
	waitFor(t, "硬上限触发回复", 3*time.Second, func() bool {
		dispatcher.debounceMu.Lock()
		defer dispatcher.debounceMu.Unlock()
		return len(dispatcher.debounceTimers) == 0
	})
	// elapsed 是第二条消息到达到计时器到期的耗时。
	if elapsed := time.Since(started); elapsed >= MessageDebounceDelay {
		t.Fatalf("硬上限应使计时器早于完整窗口到期: %v", elapsed)
	}
}

// TestDispatcherManualEchoCancelsPendingReply 验证人工已回复该会话：尚未开始的合并等待被取消，助手不再紧接着自动回复。
func TestDispatcherManualEchoCancelsPendingReply(t *testing.T) {
	// ai 记录收到的合并文字。
	ai := &batchRecordingAI{}
	// dispatcher、delivery、cleanup 是分发器、投递记录与清理函数。
	dispatcher, delivery, _, cleanup := newMergeFixture(t, ai)
	defer cleanup()
	dispatcher.scheduleDebouncedReply(buyerMsg("在吗"))
	dispatcher.cancelPendingReply("merge-chat")
	dispatcher.debounceMu.Lock()
	// pending 是取消后残留的合并等待计时器数。
	pending := len(dispatcher.debounceTimers)
	dispatcher.debounceMu.Unlock()
	time.Sleep(MessageDebounceDelay + 300*time.Millisecond)
	if pending != 0 || ai.calls() != 0 || delivery.count() != 0 {
		t.Fatalf("人工回复后不应再自动回复: timers=%d ai=%d sent=%d", pending, ai.calls(), delivery.count())
	}
	// 没有回复服务时取消是空操作，不应 panic。
	empty := newMessageDispatcher(messageDispatcherConfig{})
	empty.cancelPendingReply("whatever")
}

// TestDispatcherDiscardsPendingWhenAutoReplyNotAllowed 验证不允许自动回复的会话不会让等待消息滞留在协调器里。
func TestDispatcherDiscardsPendingWhenAutoReplyNotAllowed(t *testing.T) {
	// ai 记录收到的合并文字。
	ai := &batchRecordingAI{}
	// dispatcher、delivery、observer、cleanup 是分发器、投递记录、业务处理器夹具与清理函数。
	dispatcher, delivery, observer, cleanup := newMergeFixture(t, ai)
	defer cleanup()
	observer.owned = false
	dispatcher.scheduleDebouncedReply(buyerMsg("你好"))
	time.Sleep(MessageDebounceDelay + 300*time.Millisecond)
	dispatcher.reply.turns.mu.Lock()
	// tracked 是协调器中残留的会话条目数，应为 0。
	tracked := len(dispatcher.reply.turns.chats)
	dispatcher.reply.turns.mu.Unlock()
	if tracked != 0 || ai.calls() != 0 || delivery.count() != 0 {
		t.Fatalf("不可自动回复时应丢弃等待消息且不回复: tracked=%d ai=%d sent=%d", tracked, ai.calls(), delivery.count())
	}
}
