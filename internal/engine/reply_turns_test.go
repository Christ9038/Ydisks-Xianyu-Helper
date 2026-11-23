package engine

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// turnMsg 构造一条指定会话与文字的买家消息，供协调器测试使用。
func turnMsg(chatID, text string) ChatMessage {
	return ChatMessage{AccountID: "cid", ChatID: chatID, Text: text, SenderUserID: "buyer", MessageID: "m-" + text}
}

// mustBegin 开始轮次并断言一定有等待消息；返回轮次与拍下的消息。
func mustBegin(t *testing.T, c *replyTurnCoordinator, ctx context.Context, chatID string) (*ReplyTurn, []ChatMessage) {
	t.Helper()
	// turn、parts、err 是新轮次、拍下的消息及开始错误。
	turn, parts, err := c.begin(ctx, chatID)
	if err != nil || turn == nil {
		t.Fatalf("应开始新轮次: turn=%v err=%v", turn, err)
	}
	return turn, parts
}

// TestCoordinatorBeginMergesAllPendingAndFinishConsumes 验证 begin 拍下全部等待消息，未提交就正常结束会消费它们，避免无限重发。
func TestCoordinatorBeginMergesAllPendingAndFinishConsumes(t *testing.T) {
	// c 是被测协调器，零值可用；ctx 是账号生命周期上下文。
	var c replyTurnCoordinator
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	// turn、parts、err 是无等待消息时 begin 的结果，应全为空。
	if turn, parts, err := c.begin(ctx, "chat"); turn != nil || parts != nil || err != nil {
		t.Fatalf("没有等待消息时不应开始轮次: %v %v %v", turn, parts, err)
	}
	c.enqueue(turnMsg("chat", "a"))
	c.enqueue(turnMsg("chat", "b"))
	c.enqueue(turnMsg("chat", "c"))
	// turn、parts 是第一轮及其消费的等待消息。
	turn, parts := mustBegin(t, &c, ctx, "chat")
	if len(parts) != 3 || parts[0].Text != "a" || parts[2].Text != "c" {
		t.Fatalf("应按到达顺序拍下 3 条: %+v", parts)
	}
	turn.finish()
	turn.finish() // 幂等
	if next, _, _ := c.begin(ctx, "chat"); next != nil {
		t.Fatal("未提交的正常结束应消费消息，不应再次开始轮次")
	}
	if len(c.chats) != 0 {
		t.Fatalf("空闲会话应被清理: %d", len(c.chats))
	}
}

// TestCoordinatorSupersedesUncommittedTurnAndMergesForRetry 验证生成中（未提交）来新消息：旧轮次被取消、提交失败，
// 旧消息留给下一轮合并，并且下一轮带 replaces_turn_id。
func TestCoordinatorSupersedesUncommittedTurnAndMergesForRetry(t *testing.T) {
	// c 是被测协调器。
	var c replyTurnCoordinator
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	c.enqueue(turnMsg("chat", "第一段"))
	// old 是第一轮。
	old, _ := mustBegin(t, &c, ctx, "chat")
	if old.Superseded() || old.Context(ctx).Err() != nil {
		t.Fatal("新轮次不应已被顶替")
	}
	c.enqueue(turnMsg("chat", "第二段"))
	if !old.Superseded() || old.Context(ctx).Err() == nil {
		t.Fatal("新消息应立即顶替并取消未提交的轮次")
	}
	// ok 表示被顶替的旧轮次是否仍可提交，应为 false。
	if _, ok := old.Commit(ctx, false); ok {
		t.Fatal("被顶替的轮次不得提交")
	}
	old.finish()
	// next、parts 是顶替后的新一轮及其消费的合并消息。
	next, parts := mustBegin(t, &c, ctx, "chat")
	defer next.finish()
	if len(parts) != 2 || parts[0].Text != "第一段" || parts[1].Text != "第二段" {
		t.Fatalf("旧消息应与新消息一起合并重发: %+v", parts)
	}
	if next.ReplacesID() != old.ID() || old.ID() == "" {
		t.Fatalf("新轮次应记录它顶替的轮次: %q vs %q", next.ReplacesID(), old.ID())
	}
}

// TestCoordinatorCommittedNonInterruptibleIsNotInterrupted 验证已提交且不可中断的发送不被新消息打断，
// 新消息单独成为下一轮，begin 会等到旧轮次结束。
func TestCoordinatorCommittedNonInterruptibleIsNotInterrupted(t *testing.T) {
	// c 是被测协调器。
	var c replyTurnCoordinator
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	c.enqueue(turnMsg("chat", "问题"))
	// old 是第一轮。
	old, _ := mustBegin(t, &c, ctx, "chat")
	// sendCtx、ok 是提交后的发送上下文及提交结果。
	sendCtx, ok := old.Commit(ctx, false)
	if !ok || sendCtx != ctx {
		t.Fatalf("不可中断提交应返回脱离取消的生命周期上下文: ok=%v", ok)
	}
	// again、ok 是重复提交的返回，应沿用同一个上下文。
	if again, ok := old.Commit(ctx, true); !ok || again != ctx {
		t.Fatal("重复提交应返回首次提交的结果")
	}
	c.enqueue(turnMsg("chat", "追问"))
	if old.Superseded() || old.Context(ctx).Err() != nil || sendCtx.Err() != nil {
		t.Fatal("已提交的不可中断发送不应被新消息取消")
	}
	// started 在新轮次 begin 返回后关闭；它必须等旧轮次 finish。
	started := make(chan []ChatMessage, 1)
	go func() {
		// turn、parts 是新一轮及其消费的消息。
		turn, parts, _ := c.begin(ctx, "chat")
		started <- parts
		turn.finish()
	}()
	select {
	case <-started:
		t.Fatal("旧轮次未结束前不应开始新轮次")
	case <-time.After(100 * time.Millisecond):
	}
	old.finish()
	select {
	// parts 是新轮次拍下的消息，只应包含提交之后到达的追问。
	case parts := <-started:
		if len(parts) != 1 || parts[0].Text != "追问" {
			t.Fatalf("新轮次只应带提交之后的消息: %+v", parts)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("旧轮次结束后新轮次应开始")
	}
}

// TestCoordinatorInterruptibleCommitStillSuperseded 验证可中断提交（Agent 多段轮询）后新消息仍会顶替它，
// 并且已提交消息不会被下一轮重复合并。
func TestCoordinatorInterruptibleCommitStillSuperseded(t *testing.T) {
	// c 是被测协调器。
	var c replyTurnCoordinator
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	c.enqueue(turnMsg("chat", "旧问题"))
	// old 是第一轮。
	old, _ := mustBegin(t, &c, ctx, "chat")
	// sendCtx、ok 是提交后脱离顶替取消的发送上下文及提交结果。
	sendCtx, ok := old.Commit(ctx, true)
	if !ok || sendCtx != old.Context(ctx) {
		t.Fatal("可中断提交应返回随新消息取消的上下文")
	}
	c.enqueue(turnMsg("chat", "新问题"))
	if !old.Superseded() || sendCtx.Err() == nil {
		t.Fatal("可中断的已提交轮次应被新消息顶替并取消")
	}
	old.finish()
	// next、parts 是顶替后的新一轮及其消费的消息。
	next, parts := mustBegin(t, &c, ctx, "chat")
	defer next.finish()
	if len(parts) != 1 || parts[0].Text != "新问题" || next.ReplacesID() != old.ID() {
		t.Fatalf("已提交的旧消息不应重复，且应告知顶替了旧轮次: %+v replaces=%q", parts, next.ReplacesID())
	}
}

// TestCoordinatorDiscardClearsPendingAndCancelsUncommitted 验证人工介入丢弃：清空等待消息、取消未提交轮次，且不留下 replaces 标识。
func TestCoordinatorDiscardClearsPendingAndCancelsUncommitted(t *testing.T) {
	// c 是被测协调器。
	var c replyTurnCoordinator
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	c.discard("missing") // 不存在的会话是空操作
	c.enqueue(turnMsg("chat", "x"))
	// turn 是被丢弃前正在运行的轮次。
	turn, _ := mustBegin(t, &c, ctx, "chat")
	c.enqueue(turnMsg("chat", "y"))
	c.discard("chat")
	if !turn.Superseded() {
		t.Fatal("丢弃应顶替未提交的轮次")
	}
	turn.finish()
	// next 是丢弃后再次 begin 的结果，应为空。
	if next, _, _ := c.begin(ctx, "chat"); next != nil {
		t.Fatal("丢弃后不应再有等待消息")
	}
	c.enqueue(turnMsg("chat", "z"))
	// fresh 是丢弃后新消息开启的新一轮。
	fresh, _ := mustBegin(t, &c, ctx, "chat")
	defer fresh.finish()
	if fresh.ReplacesID() != "" {
		t.Fatalf("丢弃后的新轮次不应声称顶替了旧轮次: %q", fresh.ReplacesID())
	}
	// 没有运行轮次时丢弃会删除会话条目。
	fresh.finish()
	c.enqueue(turnMsg("other", "q"))
	c.discard("other")
	// exists 表示其他会话条目是否仍存在。
	if _, exists := c.chats["other"]; exists {
		t.Fatal("空闲会话丢弃后应被清理")
	}
}

// TestCoordinatorBeginRespectsParentCancel 验证等待旧轮次退出期间账号停止（parent 取消）会返回其错误而不是挂住。
func TestCoordinatorBeginRespectsParentCancel(t *testing.T) {
	// c 是被测协调器。
	var c replyTurnCoordinator
	c.enqueue(turnMsg("chat", "a"))
	// old 是已提交的轮次。
	old, _ := mustBegin(t, &c, context.Background(), "chat")
	defer old.finish()
	// ok 是提交结果。
	if _, ok := old.Commit(context.Background(), false); !ok {
		t.Fatal("提交应成功")
	}
	c.enqueue(turnMsg("chat", "b"))
	// parent、cancel 是会很快超时的父上下文及其取消函数。
	parent, cancel := context.WithTimeout(context.Background(), 80*time.Millisecond)
	defer cancel()
	// err 是等待旧轮次退出时上下文到期的错误。
	if _, _, err := c.begin(parent, "chat"); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("等待旧轮次时 parent 超时应返回其错误: %v", err)
	}
}

// TestCoordinatorBoundsPendingMessages 验证等待消息条数上限、TTL 过期与会话数容量淘汰。
func TestCoordinatorBoundsPendingMessages(t *testing.T) {
	// c 是被测协调器。
	var c replyTurnCoordinator
	// now 是可调的注入时钟。
	now := time.Now()
	c.now = func() time.Time { return now }
	// index 是投递的消息序号。
	for index := 0; index < replyPendingMaxMessages+5; index++ {
		c.enqueue(turnMsg("chat", string(rune('a'+index))))
	}
	// got 是实际保留的等待消息条数。
	if got := len(c.chats["chat"].pending); got != replyPendingMaxMessages {
		t.Fatalf("等待消息应有上限: %d", got)
	}
	// first 是保留下来的最旧一条消息文字。
	if first := c.chats["chat"].pending[0].msg.Text; first != "f" {
		t.Fatalf("超限应丢弃最早的消息: %q", first)
	}
	// TTL：过期的等待消息在下一次登记时被清理。
	now = now.Add(replyPendingTTL + time.Minute)
	c.enqueue(turnMsg("fresh", "x"))
	// exists 表示空闲会话条目是否仍存在。
	if _, exists := c.chats["chat"]; exists {
		t.Fatal("过期的等待消息应被清理")
	}
	// 容量：超过会话数上限时淘汰最久未活动的空闲会话，运行中的会话保留。
	for index := 0; index < replyMaxTrackedChats+10; index++ {
		now = now.Add(time.Millisecond)
		c.enqueue(turnMsg("c"+strings.Repeat("x", index%7)+string(rune(0x4e00+index)), "t"))
	}
	if len(c.chats) > replyMaxTrackedChats+1 {
		t.Fatalf("会话数应受容量上限约束: %d", len(c.chats))
	}
}

// TestNilReplyTurnIsTransparent 验证 nil 轮次表现为没有协调器的直接调用。
func TestNilReplyTurnIsTransparent(t *testing.T) {
	// turn 是 nil 轮次；ctx 是任意上下文。
	var turn *ReplyTurn
	// ctx 是本用例共用的上下文。
	ctx := context.Background()
	if turn.ID() != "" || turn.ReplacesID() != "" || turn.Superseded() || turn.Context(ctx) != ctx {
		t.Fatal("nil 轮次的只读方法应返回零值并原样返回上下文")
	}
	// got、ok 是提交结果与返回的发送上下文。
	if got, ok := turn.Commit(ctx, true); !ok || got != ctx {
		t.Fatal("nil 轮次提交应总是成功并返回原上下文")
	}
	turn.finish()
}

// TestMergeChatMessages 验证合并文字、图片占位符、图片数量上限与文字长度上限。
func TestMergeChatMessages(t *testing.T) {
	// plain 是普通文字消息；image 构造携带公网图片地址的图片消息（占位符文字加 Raw 中的地址）。
	plain := func(text string) ChatMessage { return turnMsg("chat", text) }
	// merged 是最基础的两条文字合并结果。
	merged := mergeChatMessages([]ChatMessage{plain("你好"), plain(" 这个能便宜吗 ")}, nil)
	if merged.Text != "你好\n这个能便宜吗" || len(merged.Parts) != 2 || merged.MessageID != "m- 这个能便宜吗 " {
		t.Fatalf("文字应按行合并并取最新消息的身份: %+v", merged)
	}
	// 全是占位符且取不到地址：保留占位符，保持与单条图片消息一致。
	onlyImage := mergeChatMessages([]ChatMessage{plain(agentImagePlaceholder)}, nil)
	if onlyImage.Text != agentImagePlaceholder || len(onlyImage.Images) != 0 {
		t.Fatalf("纯图片批次应保留占位符: %+v", onlyImage)
	}
	// 文字加图片占位符：占位符不进入文字。
	mixed := mergeChatMessages([]ChatMessage{plain("看图"), plain(agentImagePlaceholder)}, nil)
	if mixed.Text != "看图" {
		t.Fatalf("占位符不应计入合并文字: %q", mixed.Text)
	}
	// 超长：从最早的行开始丢弃，最新一行必留。
	long := strings.Repeat("长", 1500)
	// capped 是超过字数上限后合并出的消息。
	capped := mergeChatMessages([]ChatMessage{plain(long), plain(long), plain("最新")}, nil)
	if !strings.HasSuffix(capped.Text, "最新") || len([]rune(capped.Text)) > replyMergedMaxRunes {
		t.Fatalf("超长应丢弃最早的行并保留最新: %d", len([]rune(capped.Text)))
	}
	// 最新一行单独超长也要保留。
	huge := mergeChatMessages([]ChatMessage{plain("早"), plain(strings.Repeat("长", replyMergedMaxRunes+50))}, nil)
	if len([]rune(huge.Text)) != replyMergedMaxRunes+50 {
		t.Fatalf("最新一行即使超长也应完整保留: %d", len([]rune(huge.Text)))
	}
}

// TestMergeChatMessagesImages 验证合并批次提取买家图片：按时间顺序、最多保留最近 3 张，并能供 AI 与 Agent 共用的 buyerImageURLs 读取。
func TestMergeChatMessagesImages(t *testing.T) {
	// image 构造带公网图片地址的买家图片消息（占位符文字加 Raw 中的地址）。
	image := func(url string) ChatMessage {
		// m 是图片占位消息。
		m := turnMsg("chat", agentImagePlaceholder)
		m.Raw = map[string]any{"1": map[string]any{"10": map[string]any{"image": map[string]any{"pics": []any{map[string]any{"url": url}}}}}}
		return m
	}
	// merged 是 1 条文字加 5 张图片的合并结果。
	parts := []ChatMessage{turnMsg("chat", "看图"), image("https://i/1.png"), image("https://i/2.png"), image("https://i/3.png"), image("https://i/4.png"), image("https://i/5.png")}
	// merged 是合并结果。
	merged := mergeChatMessages(parts, nil)
	// got 是保留下来的图片地址拼接串。
	if got := strings.Join(merged.Images, ","); got != "https://i/3.png,https://i/4.png,https://i/5.png" {
		t.Fatalf("应保留最近 3 张图片并保持顺序: %s", got)
	}
	if merged.Text != "看图" || strings.Join(buyerImageURLs(merged), ",") != strings.Join(merged.Images, ",") {
		t.Fatalf("文字不含占位符，buyerImageURLs 应读取合并图片: %q", merged.Text)
	}
	// 图文混合时 modelUserInput 在文字后追加统一提示语；纯图片时只用提示语。
	if text, images := modelUserInput(merged); len(images) != 3 || !strings.HasPrefix(text, "看图\n") {
		t.Fatalf("图文混合输入异常: %q %v", text, images)
	}
	// text、images 是图片占位消息经 modelUserInput 得到的文字与图片。
	if text, images := modelUserInput(mergeChatMessages(parts[1:2], nil)); len(images) != 1 || strings.Contains(text, agentImagePlaceholder) {
		t.Fatalf("纯图片输入应使用提示语而不是占位符: %q %v", text, images)
	}
	// 没有合并信息的单条消息退回 buyerImageURL；普通文字没有图片。
	if single := image("https://i/solo.png"); len(buyerImageURLs(single)) != 1 {
		t.Fatal("单条图片消息应提取到图片")
	}
	// text、images 是纯文字消息经 modelUserInput 得到的文字与图片。
	if text, images := modelUserInput(turnMsg("chat", "你好")); text != "你好" || images != nil {
		t.Fatalf("普通文字不应有图片: %q %v", text, images)
	}
}
