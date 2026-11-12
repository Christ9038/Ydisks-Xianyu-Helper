package engine

import (
	"context"
	"testing"
	"time"
)

// ownTextEchoFrame 构造账号 unb=me 在会话 chatID 发出 text 的自身回显帧；messageID 为平台消息 ID，createdAt 为创建时间。
func ownTextEchoFrame(chatID, text, messageID string, createdAt time.Time) map[string]any {
	return map[string]any{"1": map[string]any{
		"2":  chatID + "@goofish",
		"3":  map[string]any{"messageId": messageID},
		"5":  float64(createdAt.UnixMilli()),
		"10": map[string]any{"reminderContent": text, "senderUserId": "me", "reminderUrl": "fleamarket://message_chat?peerUserId=buyer"},
	}}
}

// TestManualTakeoverAutomaticReplyEchoDoesNotPause 是 issue #109 的核心回归：自动回复与自动化发送的自身回显不能把会话判为人工介入。
// 只按“回显确认器未消费”判断会把 AI 自己的回复当成人工，导致 AI 每回一句就暂停自己。
func TestManualTakeoverAutomaticReplyEchoDoesNotPause(t *testing.T) {
	// conn 是总是发送成功的连接替身。
	conn := &hookWSConn{fakeWSConn: &fakeWSConn{}}
	// account 是挂接连接、身份为 me 的在线账号。
	account := newOutgoingGapAccount(conn, &recordingHandler{})
	// tracker 是账号构造时装配的人工介入跟踪器。
	tracker := account.outgoing.manualTakeover
	if tracker == nil || account.manualTakeover != tracker {
		t.Fatal("账号构造应把同一跟踪器装配到出站协调器与分发器")
	}
	// ctx 是自动回复链的发送上下文。
	ctx := WithAutomaticReply(context.Background())
	// err 保存 SendText 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := account.SendText(ctx, "chat-1", "buyer", "AI 自动回复"); err != nil {
		t.Fatalf("自动回复发送失败: %v", err)
	}
	account.handleMessage(ownTextEchoFrame("chat-1", "AI 自动回复", "pnm-auto", time.Now()))
	// ok 表示该会话在保留期内是否存在人工介入记录。
	if _, ok := tracker.manualSince("chat-1"); ok {
		t.Fatal("自动回复的自身回显不应判为人工介入")
	}
	// 卡片与图片同样属于系统发送。
	if err := account.SendItemCard(ctx, "chat-1", "buyer", "item", "标题", "https://img", "1"); err == nil {
		t.Fatal("基础连接不支持商品卡片，应返回未发送错误")
	}
	// 官方客户端发出的人工消息没有系统登记，必须进入人工介入窗口。
	account.handleMessage(ownTextEchoFrame("chat-1", "我是真人客服", "pnm-manual", time.Now()))
	if !tracker.isPaused("chat-1", time.Minute) {
		t.Fatal("官方客户端的人工回复应让会话进入暂停窗口")
	}
	// 重连同步下发的历史人工消息不能触发其他会话暂停。
	account.handleMessage(ownTextEchoFrame("chat-2", "很久以前的人工消息", "pnm-old", time.Now().Add(-time.Hour)))
	// ok 表示该会话在保留期内是否存在人工介入记录。
	if _, ok := tracker.manualSince("chat-2"); ok {
		t.Fatal("历史同步回显不应判为人工介入")
	}
}

// TestManualTakeoverWebChatSendMarksManual 验证网页聊天的人工发送（无自动标记）直接标记人工介入，自动化发送不标记。
func TestManualTakeoverWebChatSendMarksManual(t *testing.T) {
	// conn 是总是发送成功的连接替身。
	conn := &hookWSConn{fakeWSConn: &fakeWSConn{}}
	// account 是挂接连接、身份为 me 的在线账号。
	account := newOutgoingGapAccount(conn, &recordingHandler{})
	// tracker 是账号构造时装配的人工介入跟踪器。
	tracker := account.outgoing.manualTakeover
	// err 保存 SendText 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := account.SendText(WithOutgoingMessageKey(context.Background(), "ui-key"), "chat-web", "buyer", "网页人工回复"); err != nil {
		t.Fatalf("网页聊天发送失败: %v", err)
	}
	if !tracker.isPaused("chat-web", time.Minute) {
		t.Fatal("网页聊天的人工发送应让会话进入暂停窗口")
	}
	// 该消息随后的自身回显被登记消费，不会重复计算。
	account.handleMessage(ownTextEchoFrame("chat-web", "网页人工回复", "pnm-web", time.Now()))
	// err 保存 SendImage 调用的返回错误，非空即说明该步骤未按预期完成。
	if err := account.SendImage(WithAutomaticReply(context.Background()), "chat-img", "buyer", "https://img.example/a.png", 0, 1, 1); err != nil {
		t.Fatalf("自动回复图片发送失败: %v", err)
	}
	// ok 表示该会话在保留期内是否存在人工介入记录。
	if _, ok := tracker.manualSince("chat-img"); ok {
		t.Fatal("自动回复图片不应标记人工介入")
	}
	// 自动化中心发送要求回显确认；这里只验证不标记人工，回显确认超时由已有测试覆盖。
	account.outgoing.echoWaitTimeout = 10 * time.Millisecond
	_ = account.SendText(WithOutgoingEchoConfirmation(context.Background()), "chat-auto", "buyer", "卡密内容")
	// ok 表示该会话在保留期内是否存在人工介入记录。
	if _, ok := tracker.manualSince("chat-auto"); ok {
		t.Fatal("自动化中心发送不应标记人工介入")
	}
}
