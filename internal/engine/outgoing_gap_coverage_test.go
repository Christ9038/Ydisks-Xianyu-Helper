package engine

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/xianyu/ws"
)

// hookWSConn 在基础 WebSocket 替身上注入文本、图片和商品卡片发送钩子，用于模拟回显与平台错误。
type hookWSConn struct {
	// fakeWSConn 提供注册、心跳和接收循环等基础生命周期能力。
	*fakeWSConn
	// onText 在文本发送时被调用，返回值作为平台发送结果；为空时委托基础替身。
	onText func(ctx context.Context, text string) error
	// onImage 在图片发送时被调用，返回值作为平台发送结果；为空时委托基础替身。
	onImage func(ctx context.Context, url string) error
}

// SendText 调用文本钩子，缺省时委托基础替身。
func (c *hookWSConn) SendText(ctx context.Context, myID, chatID, toID, text string) error {
	if c.onText != nil {
		return c.onText(ctx, text)
	}
	return c.fakeWSConn.SendText(ctx, myID, chatID, toID, text)
}

// SendImage 调用图片钩子，缺省时委托基础替身。
func (c *hookWSConn) SendImage(ctx context.Context, myID, chatID, toID, url string, width, height int) error {
	if c.onImage != nil {
		return c.onImage(ctx, url)
	}
	return c.fakeWSConn.SendImage(ctx, myID, chatID, toID, url, width, height)
}

// itemCardWSConn 在基础替身上增加可选的商品卡片发送能力，并记录入参。
type itemCardWSConn struct {
	// fakeWSConn 提供基础 WebSocket 生命周期能力。
	*fakeWSConn
	// err 是商品卡片发送返回的预设平台结果。
	err error
	// args 记录最后一次商品卡片发送的聊天、买家、商品、标题、图片和价格参数。
	args []string
}

// SendItemCard 记录商品卡片参数并返回预设结果。
func (c *itemCardWSConn) SendItemCard(_ context.Context, myID, chatID, toID, itemID, title, imageURL, price string) error {
	c.args = []string{myID, chatID, toID, itemID, title, imageURL, price}
	return c.err
}

// failingObserverHandler 是出站旁路观察必定失败的处理器，用于验证旁路错误不影响发送结果。
type failingObserverHandler struct {
	// outgoingObserverHandler 提供其余 Handler 方法的空实现。
	*outgoingObserverHandler
}

// HandleOutgoingChatMessage 总是返回旁路失败。
func (h *failingObserverHandler) HandleOutgoingChatMessage(context.Context, OutgoingChatMessage) error {
	return errors.New("observer failed")
}

// pendingRequestID 返回 tracker 中第一个待确认等待器绑定的请求 mid，模拟平台按 mid 回显。
func pendingRequestID(tracker *outgoingEchoTracker) string {
	tracker.mu.Lock()
	defer tracker.mu.Unlock()
	// waiters 是某个正文键下的待确认等待器列表。
	for _, waiters := range tracker.pending {
		return waiters[0].requestID
	}
	return ""
}

// newOutgoingGapAccount 构造带指定连接和处理器的在线账号，供出站协调器测试使用。
func newOutgoingGapAccount(conn WSConn, handler Handler) *Account {
	// account 是具备 unb 身份且已挂接连接的账号运行时。
	account := New(Config{CookieID: "gap-account", CookieStr: "unb=me", Handler: handler})
	account.runtimeMu.Lock()
	account.conn = conn
	account.runtimeMu.Unlock()
	return account
}

// TestOutgoingCoordinatorNilAccountGuards 验证未初始化协调器的所有出站入口都返回明确错误。
func TestOutgoingCoordinatorNilAccountGuards(t *testing.T) {
	// coordinator 是没有绑定账号的协调器。
	coordinator := &outgoingMessageCoordinator{}
	// ctx 是测试使用的背景上下文。
	ctx := context.Background()
	// err 保存 sendText 调用返回的错误，随后断言它是否符合该场景的预期
	if err := coordinator.sendText(ctx, "c", "b", "hi"); err == nil {
		t.Fatal("sendText 未初始化应报错")
	}
	// err 保存 sendImage 调用返回的错误，随后断言它是否符合该场景的预期
	if err := coordinator.sendImage(ctx, "c", "b", "https://x/i.jpg", 0, 1, 1); err == nil {
		t.Fatal("sendImage 未初始化应报错")
	}
	// err 保存 sendItemCard 调用返回的错误，随后断言它是否符合该场景的预期
	if err := coordinator.sendItemCard(ctx, "c", "b", "1", "t", "u", "1"); err == nil {
		t.Fatal("sendItemCard 未初始化应报错")
	}
	// err 保存 currentSenderState 调用返回的错误，随后断言它是否符合该场景的预期
	if _, _, err := coordinator.currentSenderState(); err == nil {
		t.Fatal("currentSenderState 未初始化应报错")
	}
	if coordinator.automationReady() {
		t.Fatal("未初始化协调器不应就绪")
	}
	// nilCoordinator 是空指针协调器，用于验证旁路响应观察的空保护。
	var nilCoordinator *outgoingMessageCoordinator
	nilCoordinator.observePlatformSendResponse(ws.OutgoingEcho{})
	coordinator.observePlatformSendResponse(ws.OutgoingEcho{})
	// registered 保存被测登记函数返回的等待器，为空表示未登记
	if registered := nilCoordinator.registerOutgoingEcho(ctx, "c", "b", "text", "x", "m"); registered != nil {
		t.Fatal("空协调器不应登记回显")
	}
}

// TestSendItemCardPaths 验证商品卡片发送的成功、旁路失败、平台错误和能力缺失分支。
func TestSendItemCardPaths(t *testing.T) {
	// handler 记录商品卡片出站旁路消息。
	handler := &outgoingObserverHandler{}
	// conn 是支持商品卡片的连接替身。
	conn := &itemCardWSConn{fakeWSConn: &fakeWSConn{}}
	// account 是带观察器的在线账号。
	account := newOutgoingGapAccount(conn, handler)
	// ctx 携带 UI 待发送消息关联键。
	ctx := WithOutgoingMessageKey(context.Background(), "key-1")
	// err 保存 SendItemCard 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendItemCard(ctx, "chat", "buyer", " item-1 ", " 标题 ", " https://img ", " ¥12.50 "); err != nil {
		t.Fatalf("SendItemCard() err=%v", err)
	}
	if len(conn.args) != 7 || conn.args[0] != "me" || conn.args[3] != " item-1 " {
		t.Fatalf("卡片参数未透传: %v", conn.args)
	}
	if len(handler.messages) != 1 || handler.messages[0].MessageType != "item" || handler.messages[0].MessageKey != "key-1" {
		t.Fatalf("旁路消息异常: %+v", handler.messages)
	}
	if !strings.Contains(handler.messages[0].Content, `"price":"12.50"`) || !strings.Contains(handler.messages[0].Content, `"item_id":"item-1"`) {
		t.Fatalf("旁路正文未规范化: %s", handler.messages[0].Content)
	}

	// failing 是旁路必定失败的账号，发送结果仍应成功。
	failing := newOutgoingGapAccount(&itemCardWSConn{fakeWSConn: &fakeWSConn{}}, &failingObserverHandler{outgoingObserverHandler: &outgoingObserverHandler{}})
	// err 保存 SendItemCard 调用返回的错误，随后断言它是否符合该场景的预期
	if err := failing.SendItemCard(context.Background(), "chat", "buyer", "1", "t", "u", "1"); err != nil {
		t.Fatalf("旁路失败不应影响发送: %v", err)
	}

	// notSent 是协议层确定未发送的错误。
	notSent := &ws.SendError{Kind: ws.SendNotSent, Err: errors.New("closed")}
	// rejectedAccount 是商品卡片发送被协议层拒绝的账号
	rejectedAccount := newOutgoingGapAccount(&itemCardWSConn{fakeWSConn: &fakeWSConn{}, err: notSent}, nil)
	// err 保存 SendItemCard 调用返回的错误，随后断言它是否符合该场景的预期
	if err := rejectedAccount.SendItemCard(context.Background(), "chat", "buyer", "1", "t", "u", "1"); !errors.Is(err, automation.ErrMessageNotSent) {
		t.Fatalf("确定未发送应归类为 ErrMessageNotSent: %v", err)
	}

	// unsupported 是不支持商品卡片的基础连接。
	unsupported := newOutgoingGapAccount(&fakeWSConn{}, nil)
	// err 保存 SendItemCard 调用返回的错误，随后断言它是否符合该场景的预期
	if err := unsupported.SendItemCard(context.Background(), "chat", "buyer", "1", "t", "u", "1"); !errors.Is(err, automation.ErrMessageNotSent) {
		t.Fatalf("不支持卡片应返回 ErrMessageNotSent: %v", err)
	}

	// offline 是没有连接的账号。
	offline := newOutgoingGapAccount(nil, nil)
	// err 保存 SendItemCard 调用返回的错误，随后断言它是否符合该场景的预期
	if err := offline.SendItemCard(context.Background(), "chat", "buyer", "1", "t", "u", "1"); !errors.Is(err, automation.ErrMessageNotSent) {
		t.Fatalf("无连接应返回 ErrMessageNotSent: %v", err)
	}
}

// TestSendTextConfirmedAndFailurePaths 验证文本发送的回显确认、平台错误分类、超时与旁路失败。
func TestSendTextConfirmedAndFailurePaths(t *testing.T) {
	// ctx 要求等待自身回显的自动化发送上下文。
	ctx := WithOutgoingEchoConfirmation(context.Background())
	// conn 在发送时通过账号跟踪器模拟平台按请求 mid 回显。
	conn := &hookWSConn{fakeWSConn: &fakeWSConn{}}
	// account 是带旁路失败处理器的账号，用于同时覆盖旁路告警分支。
	account := newOutgoingGapAccount(conn, &failingObserverHandler{outgoingObserverHandler: &outgoingObserverHandler{}})
	conn.onText = func(_ context.Context, text string) error {
		// requestID 是已登记等待器绑定的平台请求 mid。
		requestID := pendingRequestID(account.outgoing.echoTracker)
		account.outgoing.echoTracker.observe(OutgoingChatMessage{ChatID: "chat", BuyerID: "buyer", MessageType: "text", Text: text, RequestID: requestID})
		return nil
	}
	// err 保存 SendText 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendText(ctx, "chat", "buyer", " 你好 "); err != nil {
		t.Fatalf("带回显确认的文本发送失败: %v", err)
	}
	// err 保存 SendText 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendText(ctx, "chat", "buyer", "   "); err != nil {
		t.Fatalf("空文本应直接成功: %v", err)
	}

	// notSent 与 unknown 分别是确定未发送和结果未知的平台错误。
	notSent, unknown := &ws.SendError{Kind: ws.SendNotSent, Err: errors.New("closed")}, errors.New("unknown")
	conn.onText = func(context.Context, string) error { return notSent }
	// err 保存 SendText 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendText(ctx, "chat", "buyer", "a"); !errors.Is(err, automation.ErrMessageNotSent) {
		t.Fatalf("未发送错误分类异常: %v", err)
	}
	conn.onText = func(context.Context, string) error { return unknown }
	// err 保存 SendText 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendText(ctx, "chat", "buyer", "b"); !errors.Is(err, unknown) || errors.Is(err, automation.ErrMessageNotSent) {
		t.Fatalf("未知错误应保留原错误链: %v", err)
	}

	// 回显始终不到达且预算极短：必须返回回显未确认。
	account.outgoing.echoWaitTimeout = time.Millisecond
	conn.onText = func(context.Context, string) error { return nil }
	// err 保存 SendText 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendText(ctx, "chat", "buyer", "c"); !errors.Is(err, errOutgoingEchoUnconfirmed) {
		t.Fatalf("回显超时应返回未确认: %v", err)
	}
}

// TestSendImageConfirmedAndFailurePaths 验证图片发送的本地路径拒绝、回显确认和平台错误分类。
func TestSendImageConfirmedAndFailurePaths(t *testing.T) {
	// ctx 要求等待自身回显的自动化发送上下文。
	ctx := WithOutgoingEchoConfirmation(context.Background())
	// conn 是可注入图片发送结果的连接替身。
	conn := &hookWSConn{fakeWSConn: &fakeWSConn{}}
	// account 是挂接该连接的在线账号。
	account := newOutgoingGapAccount(conn, nil)
	// err 保存 SendImage 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendImage(ctx, "chat", "buyer", "  ", 0, 1, 1); err != nil {
		t.Fatalf("空图片地址应直接成功: %v", err)
	}
	// err 保存 SendImage 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendImage(ctx, "chat", "buyer", "/static/a.png", 0, 1, 1); err == nil {
		t.Fatal("本地静态图片应被拒绝")
	}
	conn.onImage = func(_ context.Context, url string) error {
		// requestID 是已登记等待器绑定的平台请求 mid。
		requestID := pendingRequestID(account.outgoing.echoTracker)
		account.outgoing.echoTracker.observe(OutgoingChatMessage{ChatID: "chat", BuyerID: "buyer", MessageType: "image", Content: url, RequestID: requestID})
		return nil
	}
	// err 保存 SendImage 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendImage(ctx, "chat", "buyer", "https://cdn/x.jpg", 0, 1, 1); err != nil {
		t.Fatalf("带回显确认的图片发送失败: %v", err)
	}
	// notSent 与 unknown 分别是确定未发送和结果未知的平台错误。
	notSent, unknown := &ws.SendError{Kind: ws.SendRejected, Code: 403}, errors.New("unknown")
	conn.onImage = func(context.Context, string) error { return notSent }
	// err 保存 SendImage 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendImage(ctx, "chat", "buyer", "https://cdn/y.jpg", 0, 1, 1); !errors.Is(err, automation.ErrMessageNotSent) {
		t.Fatalf("拒绝错误应归类为未发送: %v", err)
	}
	conn.onImage = func(context.Context, string) error { return unknown }
	// err 保存 SendImage 调用返回的错误，随后断言它是否符合该场景的预期
	if err := account.SendImage(ctx, "chat", "buyer", "https://cdn/z.jpg", 0, 1, 1); !errors.Is(err, unknown) {
		t.Fatalf("未知错误应保留: %v", err)
	}
	// offline 是无连接账号，图片发送应返回状态错误。
	offline := newOutgoingGapAccount(nil, nil)
	// err 保存 SendImage 调用返回的错误，随后断言它是否符合该场景的预期
	if err := offline.SendImage(ctx, "chat", "buyer", "https://cdn/x.jpg", 0, 1, 1); err == nil {
		t.Fatal("无连接图片发送应失败")
	}
}

// TestOutgoingCoordinatorStateAndQueryBoundaries 验证缺少 unb、未连接和查询能力缺失的错误分支。
func TestOutgoingCoordinatorStateAndQueryBoundaries(t *testing.T) {
	// noUnb 是有连接但没有 unb 身份的账号。
	noUnb := New(Config{CookieID: "no-unb", CookieStr: "x=1"})
	noUnb.runtimeMu.Lock()
	noUnb.conn = &fakeWSConn{}
	noUnb.runtimeMu.Unlock()
	// err 保存 currentSenderState 调用返回的错误，随后断言它是否符合该场景的预期
	if _, _, err := noUnb.outgoing.currentSenderState(); !errors.Is(err, automation.ErrMessageNotSent) {
		t.Fatalf("缺少 unb 应返回未发送错误: %v", err)
	}
	// offline 是无连接账号，查询入口应返回状态错误。
	offline := newOutgoingGapAccount(nil, nil)
	// err 保存 FetchChatHistory 调用返回的错误，随后断言它是否符合该场景的预期
	if _, _, err := offline.FetchChatHistory(context.Background(), "c", 0, 1); err == nil {
		t.Fatal("无连接历史查询应失败")
	}
	// err 保存 FetchChatConversations 调用返回的错误，随后断言它是否符合该场景的预期
	if _, _, err := offline.FetchChatConversations(context.Background(), 0, 1); err == nil {
		t.Fatal("无连接会话查询应失败")
	}
	// err 保存 MarkChatRead 调用返回的错误，随后断言它是否符合该场景的预期
	if err := offline.MarkChatRead(context.Background(), "c", nil); err == nil {
		t.Fatal("无连接已读上报应失败")
	}
}

// TestOutgoingEchoTrackerNilGuards 验证空 tracker、空 waiter 和无 tracker waiter 的安全空操作。
func TestOutgoingEchoTrackerNilGuards(t *testing.T) {
	// tracker 是空指针 tracker。
	var tracker *outgoingEchoTracker
	tracker.observePlatform(OutgoingChatMessage{MessageKey: "k"})
	tracker.observe(OutgoingChatMessage{})
	tracker.pruneCanceledLocked(time.Now())
	if tracker.register("c", "b", "text", "x") != nil {
		t.Fatal("空 tracker 不应登记等待器")
	}
	// waiter 是空指针等待器。
	var waiter *outgoingEchoWaiter
	// err 保存 wait 调用返回的错误，随后断言它是否符合该场景的预期
	if err := waiter.wait(context.Background(), time.Second); err != nil {
		t.Fatalf("空等待器应视为已确认: %v", err)
	}
	waiter.cancel()
	(&outgoingEchoWaiter{}).cancel()
}

// TestOutgoingEchoWaitBranches 验证等待器的默认超时、完成、取消和计时器过期分支。
func TestOutgoingEchoWaitBranches(t *testing.T) {
	// tracker 是账号级回显跟踪器。
	tracker := newOutgoingEchoTracker()
	// done 是已被回显确认的等待器；非正超时会使用默认预算但立即返回。
	done := tracker.register("c", "b", "text", "x")
	close(done.done)
	// err 保存 wait 调用返回的错误，随后断言它是否符合该场景的预期
	if err := done.wait(context.Background(), 0); err != nil {
		t.Fatalf("已完成等待器应返回 nil: %v", err)
	}
	// pending 是尚未确认的等待器。
	pending := tracker.register("c", "b", "text", "y")
	// err 保存 wait 调用返回的错误，随后断言它是否符合该场景的预期
	if err := pending.wait(context.Background(), time.Nanosecond); !errors.Is(err, errOutgoingEchoUnconfirmed) {
		t.Fatalf("计时器到期应返回未确认: %v", err)
	}
	// ctx 是已取消的调用方上下文。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// err 保存 wait 调用返回的错误，随后断言它是否符合该场景的预期
	if err := pending.wait(ctx, time.Minute); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消应返回 context.Canceled: %v", err)
	}
}

// TestOutgoingEchoObserveMessageRouting 验证带与不带平台消息 ID 的回显路由，以及类型/内容规范化。
func TestOutgoingEchoObserveMessageRouting(t *testing.T) {
	// tracker 是账号级回显跟踪器。
	tracker := newOutgoingEchoTracker()
	// plain 与 keyed 分别等待无 ID 回显和带平台 ID 回显。
	plain, keyed := tracker.register("c", "b", "text", "plain"), tracker.register("c", "b", "image", "https://i")
	tracker.observeMessage(OutgoingChatMessage{ChatID: "c@goofish", MessageType: "", Text: "plain"})
	tracker.observeMessage(OutgoingChatMessage{ChatID: "c", MessageType: "image", Content: " https://i ", MessageKey: "pnm"})
	// waiter 是当前遍历的回显等待器
	for _, waiter := range []*outgoingEchoWaiter{plain, keyed} {
		select {
		case <-waiter.done:
		default:
			t.Fatal("回显未唤醒对应等待器")
		}
	}
	if normalizeOutgoingEchoType("  ") != "text" || normalizeOutgoingEchoType(" image ") != "image" {
		t.Fatal("消息类型规范化异常")
	}
	if outgoingEchoContent(OutgoingChatMessage{MessageType: "image", Text: "t", Content: " c "}) != "c" {
		t.Fatal("非文本应使用 Content")
	}
}

// TestOutgoingEchoEvictsOldestPlatformKey 验证已消费平台消息 ID 达到上限时淘汰最早记录。
func TestOutgoingEchoEvictsOldestPlatformKey(t *testing.T) {
	// tracker 是账号级回显跟踪器，预先填满去重缓存。
	tracker := newOutgoingEchoTracker()
	// i 是填充或循环序号
	for i := 0; i < maxSeenPlatformKeys; i++ {
		// key 是预置的已消费平台消息 ID。
		key := fmt.Sprintf("old-%d", i)
		tracker.seenPlatformKeys[key] = struct{}{}
		tracker.seenPlatformOrder = append(tracker.seenPlatformOrder, key)
	}
	// waiter 等待将触发淘汰的新回显。
	waiter := tracker.register("c", "b", "text", "x")
	tracker.observePlatform(OutgoingChatMessage{ChatID: "c", BuyerID: "b", Text: "x", MessageKey: "fresh"})
	select {
	case <-waiter.done:
	default:
		t.Fatal("新回显应确认等待器")
	}
	// exists 表示目标键是否仍保存在被测集合中
	if _, exists := tracker.seenPlatformKeys["old-0"]; exists {
		t.Fatal("最早的平台消息 ID 应被淘汰")
	}
	// exists 表示目标键是否仍保存在被测集合中
	if _, exists := tracker.seenPlatformKeys["fresh"]; !exists || len(tracker.seenPlatformOrder) != maxSeenPlatformKeys {
		t.Fatalf("新平台消息 ID 未登记或容量失控: %d", len(tracker.seenPlatformOrder))
	}
}

// TestOutgoingEchoCancelKeepsSiblingsAndCapsRecords 验证取消只移除自身，并限制同键取消保护记录数量。
func TestOutgoingEchoCancelKeepsSiblingsAndCapsRecords(t *testing.T) {
	// tracker 是账号级回显跟踪器。
	tracker := newOutgoingEchoTracker()
	// first、second 是同一正文键下的两个等待器。
	first, second := tracker.register("c", "b", "text", "x"), tracker.register("c", "b", "text", "x")
	first.cancelWithoutLateProtection()
	if len(tracker.pending[second.key]) != 1 || len(tracker.canceledPending) != 0 {
		t.Fatal("无保护取消只应移除自身且不登记保护记录")
	}
	second.cancel()
	// i 是填充或循环序号
	for i := 0; i < maxCanceledEchoRecordsPerKey+5; i++ {
		tracker.register("c", "b", "text", "x").cancel()
	}
	// got 保存被测函数的实际输出，随后与期望值比较
	if got := len(tracker.canceledPending[second.key]); got != maxCanceledEchoRecordsPerKey {
		t.Fatalf("取消保护记录应被限制为上限, got=%d", got)
	}
}

// TestOutgoingEchoObserveLockedBranches 验证 observeLocked 的过期记录清理、记录消费、关联等待器与身份不匹配分支。
func TestOutgoingEchoObserveLockedBranches(t *testing.T) {
	// key 是被测正文键。
	key := outgoingEchoKey{chatID: "c", messageType: "text", content: "x"}
	// stale 与 fresh 分别是已过保护窗口和仍在窗口内的取消记录时间。
	stale, fresh := time.Now().Add(-2*canceledEchoRetentionWindow), time.Now()

	// 仅有过期记录：键被删除。
	tracker := newOutgoingEchoTracker()
	tracker.canceledPending[key] = []canceledEchoRecord{{at: stale}}
	tracker.observeLocked(OutgoingChatMessage{ChatID: "c", Text: "x"})
	if len(tracker.canceledPending) != 0 {
		t.Fatal("全部过期的记录应删除")
	}

	// 过期加两条有效记录：过期被裁剪，消费一条后保留另一条。
	tracker = newOutgoingEchoTracker()
	tracker.canceledPending[key] = []canceledEchoRecord{{at: stale}, {at: fresh}, {at: fresh}}
	if observeLockedMatched(tracker, OutgoingChatMessage{ChatID: "c", Text: "x"}) {
		t.Fatal("消费取消记录不应确认等待器")
	}
	if len(tracker.canceledPending[key]) != 1 {
		t.Fatalf("应裁剪过期并消费一条, 剩余=%d", len(tracker.canceledPending[key]))
	}

	// 仅过期裁剪后仍有有效记录且 mid 不同：记录被保留。
	tracker = newOutgoingEchoTracker()
	tracker.canceledPending[key] = []canceledEchoRecord{{at: stale}, {at: fresh, requestID: "other"}}
	tracker.observeLocked(OutgoingChatMessage{ChatID: "c", Text: "x", RequestID: "mine"})
	if len(tracker.canceledPending[key]) != 1 {
		t.Fatal("不同 mid 的有效记录应保留")
	}

	// 关联等待器阻止无 mid 推送；mid 不匹配、买家不匹配均不确认。
	tracker = newOutgoingEchoTracker()
	// correlated 是已绑定请求 mid 的等待器。
	correlated := tracker.register("c", "buyer-a", "text", "x", "mid-1")
	if observeLockedMatched(tracker, OutgoingChatMessage{ChatID: "c", Text: "x"}) {
		t.Fatal("无 mid 推送不应消费已关联等待器")
	}
	if observeLockedMatched(tracker, OutgoingChatMessage{ChatID: "c", Text: "x", RequestID: "mid-2"}) {
		t.Fatal("mid 不匹配不应确认")
	}
	if observeLockedMatched(tracker, OutgoingChatMessage{ChatID: "c", BuyerID: "buyer-b", Text: "x", RequestID: "mid-1"}) {
		t.Fatal("买家不匹配不应确认")
	}
	if !observeLockedMatched(tracker, OutgoingChatMessage{ChatID: "c", BuyerID: "buyer-a", Text: "x", RequestID: "mid-1"}) {
		t.Fatal("完全匹配应确认")
	}
	select {
	case <-correlated.done:
	default:
		t.Fatal("等待器应被唤醒")
	}
}

// TestOutgoingEchoPruneCanceled 验证过期裁剪、全过期删除和键数量超限时淘汰最早键。
func TestOutgoingEchoPruneCanceled(t *testing.T) {
	// tracker 是账号级回显跟踪器。
	tracker := newOutgoingEchoTracker()
	// now 是清理基准时间。
	now := time.Now()
	// partial 与 expired 分别是部分过期键和全过期键。
	partial, expired := outgoingEchoKey{content: "partial"}, outgoingEchoKey{content: "expired"}
	tracker.canceledPending[partial] = []canceledEchoRecord{{at: now.Add(-time.Hour)}, {at: now}}
	tracker.canceledPending[expired] = []canceledEchoRecord{{at: now.Add(-time.Hour)}}
	tracker.pruneCanceledLocked(now)
	if len(tracker.canceledPending[partial]) != 1 {
		t.Fatal("部分过期键应裁剪为一条")
	}
	// exists 表示目标键是否仍保存在被测集合中
	if _, exists := tracker.canceledPending[expired]; exists {
		t.Fatal("全过期键应删除")
	}
	// 填充超过键上限的未过期键，其中 oldest 的时间最早。
	oldest := outgoingEchoKey{content: "oldest"}
	tracker.canceledPending[oldest] = []canceledEchoRecord{{at: now.Add(-time.Second)}}
	// i 是填充或循环序号
	for i := 0; i < maxCanceledEchoKeys+1; i++ {
		tracker.canceledPending[outgoingEchoKey{content: fmt.Sprintf("k-%d", i)}] = []canceledEchoRecord{{at: now}}
	}
	tracker.pruneCanceledLocked(now)
	if len(tracker.canceledPending) > maxCanceledEchoKeys {
		t.Fatalf("键数量应回落到上限, got=%d", len(tracker.canceledPending))
	}
	// exists 表示目标键是否仍保存在被测集合中
	if _, exists := tracker.canceledPending[oldest]; exists {
		t.Fatal("最早的键应被淘汰")
	}
}

// TestOutgoingEchoCancelSkipsOtherWaiters 验证取消排在后面的等待器时会跳过同键的前序等待器。
func TestOutgoingEchoCancelSkipsOtherWaiters(t *testing.T) {
	// tracker 是账号级回显跟踪器。
	tracker := newOutgoingEchoTracker()
	// first、second 是同一正文键下按登记顺序排列的两个等待器。
	first, second := tracker.register("c", "b", "text", "x"), tracker.register("c", "b", "text", "x")
	second.cancelWithoutLateProtection()
	// remaining 保存冷却剩余时长，已过冷却窗口时应为零
	if remaining := tracker.pending[first.key]; len(remaining) != 1 || remaining[0] != first {
		t.Fatal("只应移除被取消的后序等待器")
	}
}

// TestSendTextWithoutConnectionAndNilClassification 验证文本发送缺少连接时的状态错误以及空错误分类。
func TestSendTextWithoutConnectionAndNilClassification(t *testing.T) {
	// offline 是没有可用连接的账号。
	offline := newOutgoingGapAccount(nil, nil)
	// err 保存 SendText 调用返回的错误，随后断言它是否符合该场景的预期
	if err := offline.SendText(context.Background(), "c", "b", "hi"); !errors.Is(err, automation.ErrMessageNotSent) {
		t.Fatalf("无连接文本发送应返回未发送错误: %v", err)
	}
	if classifyPlatformSendError(nil) != nil {
		t.Fatal("空错误分类结果应为空")
	}
}
