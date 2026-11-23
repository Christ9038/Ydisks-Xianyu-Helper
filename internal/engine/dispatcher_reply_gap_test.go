package engine

import (
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/ws"
)

// systemErrHandler 的系统事件处理总是失败，用于验证分发器只记录错误而不中断接收循环。
type systemErrHandler struct {
	// recordingHandler 提供其余 Handler 方法。
	*recordingHandler
}

// HandleSystemEvent 总是返回处理失败。
func (h *systemErrHandler) HandleSystemEvent(context.Context, automation.Task) error {
	return errors.New("system event failed")
}

// readErrHandler 同时实现已读回执端口且总是失败。
type readErrHandler struct {
	// recordingHandler 提供其余 Handler 方法。
	*recordingHandler
	// calls 统计回执处理次数。
	calls atomic.Int32
}

// HandleMessageRead 记录调用并返回失败。
func (h *readErrHandler) HandleMessageRead(context.Context, MessageReadEvent) error {
	h.calls.Add(1)
	return errors.New("read failed")
}

// chatErrHandler 的聊天处理总是失败，用于验证失败后不再进入自动回复。
type chatErrHandler struct {
	// recordingHandler 提供其余 Handler 方法。
	*recordingHandler
}

// HandleChatMessage 总是返回处理失败。
func (h *chatErrHandler) HandleChatMessage(context.Context, ChatMessage) error {
	return errors.New("chat failed")
}

// roleErrHandler 在本地归属夹具上让会话角色读取失败。
type roleErrHandler struct {
	// localOwnershipHandlerFixture 提供角色与归属夹具。
	*localOwnershipHandlerFixture
}

// ChatSessionRole 总是返回读取失败。
func (h *roleErrHandler) ChatSessionRole(context.Context, string, string, string) (db.ChatSession, error) {
	return db.ChatSession{}, errors.New("role failed")
}

// ownEchoFrame 构造当前账号 unb=123 自己发出的普通文本回显帧。
func ownEchoFrame() map[string]any {
	return map[string]any{"1": map[string]any{"2": "chat-1@goofish", "10": map[string]any{"reminderContent": "hi", "senderUserId": "123"}}}
}

// waitSemaphoreDrained 在有限时间内等待分发器处理协程释放全部并发槽位。
func waitSemaphoreDrained(t *testing.T, d *messageDispatcher) {
	t.Helper()
	// deadline 是等待处理协程退出的预算。
	deadline := time.Now().Add(5 * time.Second)
	for len(d.sem) > 0 {
		if time.Now().After(deadline) {
			t.Fatal("分发处理协程未释放并发槽位")
		}
		time.Sleep(time.Millisecond)
	}
}

// TestMessageDispatcherDefaultsAndRejectedTasks 验证默认依赖可用，且生命周期拒绝接纳时不处理消息。
func TestMessageDispatcherDefaultsAndRejectedTasks(t *testing.T) {
	// dispatcher 使用全部默认依赖。
	dispatcher := newMessageDispatcher(messageDispatcherConfig{CookieID: "cid"})
	dispatcher.dispatch(map[string]any{})
	waitSemaphoreDrained(t, &dispatcher)
	dispatcher.handleMessage(ownEchoFrame())
	// rejected 的 BeginTask 总是拒绝。
	rejected := newMessageDispatcher(messageDispatcherConfig{CookieID: "cid", BeginTask: func() (context.Context, func(), bool) { return nil, nil, false }})
	rejected.dispatch(map[string]any{})
	rejected.handleMessage(map[string]any{})
}

// TestMessageDispatcherSystemEventBackpressureCancel 验证并发槽位耗尽时系统事件等待被取消会释放任务。
func TestMessageDispatcherSystemEventBackpressureCancel(t *testing.T) {
	// ctx、cancel 是已取消的任务上下文。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// finished 在任务释放函数被调用时关闭。
	finished := make(chan struct{})
	// dispatcher 是本用例构造的消息分发器，不启动账号网络连接
	dispatcher := newMessageDispatcher(messageDispatcherConfig{
		CookieID: "cid", CurrentCookie: func() string { return "unb=123" },
		BeginTask: func() (context.Context, func(), bool) { return ctx, func() { close(finished) }, true },
	})
	for range cap(dispatcher.sem) {
		dispatcher.sem <- struct{}{}
	}
	dispatcher.dispatch(mustPaidDeliveryCard(t))
	select {
	case <-finished:
	case <-time.After(5 * time.Second):
		t.Fatal("取消后应释放任务")
	}
}

// TestMessageDispatcherHandlerErrors 验证系统事件、已读回执、出站回显与聊天处理失败都只记录错误。
func TestMessageDispatcherHandlerErrors(t *testing.T) {
	// ctx 是测试共用上下文。
	ctx := context.Background()
	// system 的系统事件处理失败。
	system := &systemErrHandler{recordingHandler: &recordingHandler{}}
	// d1 是对应场景使用的消息分发器
	d1 := newMessageDispatcher(messageDispatcherConfig{CookieID: "cid", CurrentCookie: func() string { return "unb=123" }, CurrentHandler: func() Handler { return system }})
	d1.handleMessageContext(ctx, mustPaidDeliveryCard(t))
	// read 的已读回执处理失败。
	read := &readErrHandler{recordingHandler: &recordingHandler{}}
	// d2 是对应场景使用的消息分发器
	d2 := newMessageDispatcher(messageDispatcherConfig{CookieID: "cid", CurrentHandler: func() Handler { return read }})
	d2.handleMessageContext(ctx, map[string]any{"1": "9.PNM", "2": "2", "4": "chat@goofish"})
	if read.calls.Load() != 1 {
		t.Fatal("已读回执应交给处理器")
	}
	// echo 的出站观察失败。
	echo := &failingObserverHandler{outgoingObserverHandler: &outgoingObserverHandler{}}
	// d3 是对应场景使用的消息分发器
	d3 := newMessageDispatcher(messageDispatcherConfig{CookieID: "cid", CurrentCookie: func() string { return "unb=123" }, CurrentHandler: func() Handler { return echo }})
	d3.handleMessageContext(ctx, ownEchoFrame())
}

// TestMessageDispatcherDedupBackupKeyAndDebounceFailures 验证无消息 ID 时的备用去重键，以及防抖回调的拒绝与处理失败。
func TestMessageDispatcherDedupBackupKeyAndDebounceFailures(t *testing.T) {
	// dispatcher 是本用例构造的消息分发器，不启动账号网络连接
	dispatcher := newMessageDispatcher(messageDispatcherConfig{CookieID: "cid"})
	// frame 没有可用消息 ID，但带有创建时间。
	frame := map[string]any{"1": map[string]any{"5": 123}}
	// chat 是被检查的聊天消息。
	chat := &ChatMessage{ChatID: "c", Text: "hello"}
	if !dispatcher.markAndCheckDedup(frame, chat) || dispatcher.markAndCheckDedup(frame, chat) {
		t.Fatal("首次应通过，重复应被去重")
	}

	// 到期回复任务：生命周期拒绝接纳。第一个任务（接收落库）被接纳，合并等待到期后的回复任务被拒绝，不得开始回复。
	// asked 在回复任务请求生命周期时关闭；calls 统计 BeginTask 调用次数。
	asked := make(chan struct{})
	// calls 统计生命周期接纳回调被调用的次数，区分接收任务与到期回复任务。
	var calls atomic.Int32
	// store、cleanup 提供独立数据库；回复服务只需存在即可让计时器登记。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	shortenMergeWindow(store)
	// rejected 是回复任务被生命周期拒绝接纳的分发器
	rejected := newMessageDispatcher(messageDispatcherConfig{CookieID: "cid", Reply: NewReplyService("cid", store, &recordingDelivery{}, nil, nil, nil),
		BeginTask: func() (context.Context, func(), bool) {
			if calls.Add(1) == 1 {
				return context.Background(), func() {}, true
			}
			close(asked)
			return nil, nil, false
		}})
	rejected.scheduleDebouncedReply(chatMsg("x", "i", "c1"))
	select {
	case <-asked:
	case <-time.After(5 * time.Second):
		t.Fatal("合并等待到期后未请求回复任务")
	}
	rejected.stop()

	// 接收落库：业务处理器失败时不进入自动回复，不登记合并等待，也不留下计时器。
	// done 在每个接收任务释放时收到一个信号。
	done := make(chan struct{}, 2)
	// failing 是预置失败行为的测试替身，用于驱动错误分支
	failing := &chatErrHandler{recordingHandler: &recordingHandler{}}
	// failed 是聊天处理总是失败的分发器
	failed := newMessageDispatcher(messageDispatcherConfig{CookieID: "cid", CurrentHandler: func() Handler { return failing },
		Reply: NewReplyService("cid", store, &recordingDelivery{}, nil, nil, nil),
		BeginTask: func() (context.Context, func(), bool) {
			return context.Background(), func() { done <- struct{}{} }, true
		}})
	failed.scheduleDebouncedReply(chatMsg("first", "i", "c2"))
	failed.scheduleDebouncedReply(chatMsg("second", "i", "c2"))
	// finished 是已确认完成的接收任务数。
	for finished := 0; finished < 2; finished++ {
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Fatal("接收任务未完成")
		}
	}
	failed.debounceMu.Lock()
	// timers 是落库失败后残留的合并等待计时器数，应为 0。
	timers := len(failed.debounceTimers)
	failed.debounceMu.Unlock()
	if timers != 0 {
		t.Fatalf("落库失败不应登记合并等待计时器: %d", timers)
	}
	failed.stop()
}

// TestCanAutoReplyGuards 验证自动回复门禁在缺商品、角色读取失败、归属能力缺失、写标记失败与账号切换下拒绝。
func TestCanAutoReplyGuards(t *testing.T) {
	// ctx 是测试共用上下文。
	ctx := context.Background()
	// message 是携带商品的买家消息。
	message := chatMsg("hi", "item-1", "chat-1")
	// base 是本地归属夹具，默认商品不属于本账号。
	base := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}}
	// newDispatcher 创建使用指定处理器和 Cookie 来源的分发器。
	newDispatcher := func(handler Handler, cookie func() string) messageDispatcher {
		return newMessageDispatcher(messageDispatcherConfig{CookieID: "cid", CurrentCookie: cookie, CurrentHandler: func() Handler { return handler }})
	}
	// fixedCookie 是返回固定账号 Cookie 的测试函数
	fixedCookie := func() string { return "unb=123" }

	// noItem 是缺少商品标识场景使用的分发器
	noItem := newDispatcher(base, fixedCookie)
	if noItem.canAutoReply(ctx, chatMsg("hi", "", "chat-1")) {
		t.Fatal("缺少商品不应自动回复")
	}
	// roleFail 是角色读取总是失败的分发器
	roleFail := newDispatcher(&roleErrHandler{localOwnershipHandlerFixture: base}, fixedCookie)
	if roleFail.canAutoReply(ctx, message) {
		t.Fatal("角色读取失败不应自动回复")
	}
	// roleOnly 只实现角色仓储而不实现本地归属。
	roleOnly := newDispatcher(base.roleHandlerFixture, fixedCookie)
	if roleOnly.canAutoReply(ctx, message) {
		t.Fatal("缺少归属能力不应自动回复")
	}
	// markFail 的归属为否，且保存未解析标记失败。
	markFail := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}, roleWriteErr: errors.New("write failed")}}
	// d 保存 newDispatcher 的返回值，供随后的断言使用
	if d := newDispatcher(markFail, fixedCookie); d.canAutoReply(ctx, message) {
		t.Fatal("标记写入失败不应自动回复")
	}
	// switching 在第二次读取 Cookie 时返回另一个账号，模拟凭证切换。
	var cookieReads atomic.Int32
	// switching 是归属为真且用于模拟账号切换的本地归属夹具
	switching := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}, owned: true}
	// d 保存 newDispatcher 的返回值，供随后的断言使用
	d := newDispatcher(switching, func() string {
		if cookieReads.Add(1) > 1 {
			return "unb=999"
		}
		return "unb=123"
	})
	if d.canAutoReply(ctx, message) {
		t.Fatal("账号切换后不应自动回复")
	}
}

// fixedDelivery 返回固定投递结果，用于驱动回复状态机。
type fixedDelivery struct {
	// result 与 err 是 SendReply 的固定返回值。
	result ReplySendResult
	err    error
	// sent 记录最后一次收到的消息。
	sent ReplyMessage
}

// SendReply 记录消息并返回固定结果。
func (f *fixedDelivery) SendReply(_ context.Context, message ReplyMessage) (ReplySendResult, error) {
	f.sent = message
	return f.result, f.err
}

// replyOnceService 创建带一次性默认回复的服务；image 为空时只有文字回复。
func replyOnceService(t *testing.T, delivery ReplyDelivery, image string) (*ReplyService, *db.Store) {
	t.Helper()
	// cleanup 是释放测试数据库的清理函数 ；store 是该账号使用的隔离测试数据库存储，用于构造和破坏持久化状态
	store, cleanup := newReplyStore(t)
	t.Cleanup(cleanup)
	// err 保存 ExecContext 调用返回的错误，随后断言它是否符合该场景的预期
	if _, err := store.DB.ExecContext(context.Background(), `INSERT INTO default_replies (cookie_id,enabled,reply_content,reply_image_url,reply_once) VALUES ('cid',1,'默认回复',?,1)`, image); err != nil {
		t.Fatal(err)
	}
	return NewReplyService("cid", store, delivery, nil, nil, nil), store
}

// blockReplyRecordUpdates 阻断默认回复记录的更新，使分段状态写入失败。
func blockReplyRecordUpdates(t *testing.T, store *db.Store) {
	t.Helper()
	// err 保存 ExecContext 调用返回的错误，随后断言它是否符合该场景的预期
	if _, err := store.DB.ExecContext(context.Background(), `CREATE TRIGGER block_reply_update BEFORE UPDATE ON default_reply_records BEGIN SELECT RAISE(ABORT, 'blocked'); END`); err != nil {
		t.Fatal(err)
	}
}

// TestReplyHandleStateFailures 验证一次性回复在领取、分段确认、发送失败与收口失败时的返回。
func TestReplyHandleStateFailures(t *testing.T) {
	// ctx 是测试共用上下文。
	ctx := context.Background()
	// message 是需要默认回复的买家消息。
	message := chatMsg("你好", "", "chat-1")
	t.Run("领取失败", func(t *testing.T) {
		// service 是本用例构造的回复服务，绑定隔离测试存储与固定投递端口 ；store 是该账号使用的隔离测试数据库存储，用于构造和破坏持久化状态
		service, store := replyOnceService(t, &fixedDelivery{}, "")
		// err 保存 ExecContext 调用返回的错误，随后断言它是否符合该场景的预期
		if _, err := store.DB.ExecContext(ctx, `DROP TABLE default_reply_records`); err != nil {
			t.Fatal(err)
		}
		// err 保存 Handle 调用返回的错误，随后断言它是否符合该场景的预期
		if err := service.Handle(ctx, message); err == nil || !strings.Contains(err.Error(), "领取默认回复发送任务") {
			t.Fatalf("应报领取失败: %v", err)
		}
	})
	t.Run("图片分段写入失败", func(t *testing.T) {
		// service 是本用例构造的回复服务，绑定隔离测试存储与固定投递端口 ；store 是该账号使用的隔离测试数据库存储，用于构造和破坏持久化状态
		service, store := replyOnceService(t, &fixedDelivery{result: ReplySendResult{ImageSent: true, TextSent: true}}, "https://img.example/a.png")
		blockReplyRecordUpdates(t, store)
		// err 保存 Handle 调用返回的错误，随后断言它是否符合该场景的预期
		if err := service.Handle(ctx, message); err == nil {
			t.Fatal("图片分段写入失败应返回错误")
		}
	})
	t.Run("文字分段写入失败", func(t *testing.T) {
		// service 是本用例构造的回复服务，绑定隔离测试存储与固定投递端口 ；store 是该账号使用的隔离测试数据库存储，用于构造和破坏持久化状态
		service, store := replyOnceService(t, &fixedDelivery{result: ReplySendResult{TextSent: true}}, "")
		blockReplyRecordUpdates(t, store)
		// err 保存 Handle 调用返回的错误，随后断言它是否符合该场景的预期
		if err := service.Handle(ctx, message); err == nil {
			t.Fatal("文字分段写入失败应返回错误")
		}
	})
	t.Run("发送失败且状态写入失败", func(t *testing.T) {
		// sendErr 保存 New 调用返回的错误，随后断言它是否符合该场景的预期
		sendErr := errors.New("send boom")
		// service 是本用例构造的回复服务，绑定隔离测试存储与固定投递端口 ；store 是该账号使用的隔离测试数据库存储，用于构造和破坏持久化状态
		service, store := replyOnceService(t, &fixedDelivery{err: sendErr}, "")
		blockReplyRecordUpdates(t, store)
		// err 保存 Handle 调用返回的错误，随后断言它是否符合该场景的预期
		if err := service.Handle(ctx, message); !errors.Is(err, sendErr) || !strings.Contains(err.Error(), "保存默认回复失败状态") {
			t.Fatalf("应同时返回发送与状态写入错误: %v", err)
		}
	})
	t.Run("传输未知结果", func(t *testing.T) {
		// sendErr 保存 New 调用返回的错误，随后断言它是否符合该场景的预期
		sendErr := &ws.SendError{Kind: ws.SendUncertain, Err: errors.New("timeout")}
		// service 是本用例构造的回复服务，绑定隔离测试存储与固定投递端口
		service, _ := replyOnceService(t, &fixedDelivery{err: sendErr}, "")
		// err 保存 Handle 调用返回的错误，随后断言它是否符合该场景的预期
		if err := service.Handle(ctx, message); !errors.Is(err, sendErr) {
			t.Fatalf("应返回原发送错误: %v", err)
		}
	})
	t.Run("记录收口失败", func(t *testing.T) {
		// service 是本用例构造的回复服务，绑定隔离测试存储与固定投递端口 ；store 是该账号使用的隔离测试数据库存储，用于构造和破坏持久化状态
		service, store := replyOnceService(t, &fixedDelivery{}, "")
		blockReplyRecordUpdates(t, store)
		// err 保存 Handle 调用返回的错误，随后断言它是否符合该场景的预期
		if err := service.Handle(ctx, message); err == nil {
			t.Fatal("收口失败应返回错误")
		}
	})
}

// TestReplyHandleWithDeliveryDirectBranches 验证已发分段的剔除、全部已发的跳过，以及 AI 报价保存失败。
func TestReplyHandleWithDeliveryDirectBranches(t *testing.T) {
	// ctx 是测试共用上下文。
	ctx := context.Background()
	// delivery 记录实际发送的消息。
	delivery := &fixedDelivery{result: ReplySendResult{ImageSent: true}}
	// service 是本用例构造的回复服务，绑定隔离测试存储与固定投递端口 ；store 是该账号使用的隔离测试数据库存储，用于构造和破坏持久化状态
	service, store := replyOnceService(t, delivery, "")
	// message 是带会话和商品标识的买家消息。
	message := chatMsg("hi", "item-1", "chat-1")
	// res 同时包含文字与图片。
	res := &ReplyResult{Text: "文字", ImageURL: "https://img.example/a.png"}
	// err 保存 handleWithDelivery 调用返回的错误，随后断言它是否符合该场景的预期
	if err := service.handleWithDelivery(ctx, message, res, db.DefaultReplyRecord{TextSent: true}); err != nil {
		t.Fatal(err)
	}
	if delivery.sent.Text != "" || delivery.sent.ImageURL == "" {
		t.Fatalf("已发文字应被剔除: %+v", delivery.sent)
	}
	delivery.sent = ReplyMessage{}
	// err 保存 handleWithDelivery 调用返回的错误，随后断言它是否符合该场景的预期
	if err := service.handleWithDelivery(ctx, message, res, db.DefaultReplyRecord{TextSent: true, ImageSent: true}); err != nil || delivery.sent.ChatID != "" {
		t.Fatalf("全部已发应直接跳过: %v %+v", err, delivery.sent)
	}
	// quoteRes 是已确认文字后需要保存的 AI 报价。
	quoteRes := &ReplyResult{Text: "可以", Source: "AI", AutoPriceQuote: &AIPriceQuoteProposal{PriceCents: 100}}
	delivery.result = ReplySendResult{TextSent: true}
	// err 保存 ExecContext 调用返回的错误，随后断言它是否符合该场景的预期
	if _, err := store.DB.ExecContext(ctx, `DROP TABLE ai_bargain_quotes`); err != nil {
		t.Fatal(err)
	}
	// err 保存 handleWithDelivery 调用返回的错误，随后断言它是否符合该场景的预期
	if err := service.handleWithDelivery(ctx, message, quoteRes, db.DefaultReplyRecord{}); err == nil || !strings.Contains(err.Error(), "保存已发送的 AI 自动改价报价") {
		t.Fatalf("报价保存失败应报错: %v", err)
	}
}
