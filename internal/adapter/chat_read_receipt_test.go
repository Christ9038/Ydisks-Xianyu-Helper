package adapter

import (
	"context"
	"log/slog"
	"testing"

	domainchat "xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// TestHandleMessageReadIgnoresIncomingReceipt 验证跨端同步的入站消息已读回执不会误标同会话的本地出站消息。
func TestHandleMessageReadIgnoresIncomingReceipt(t *testing.T) {
	// store、cleanup 保存隔离测试数据库及其关闭函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是消息写入和回执处理共用的测试上下文。
	ctx := context.Background()
	// service 是使用真实数据库聊天仓储的领域服务。
	service := domainchat.New(store)
	// bridge 是只装配聊天回执能力的适配器。
	bridge := &Adapter{chat: service, logger: slog.Default()}
	// session 是入站和出站消息共用的聊天会话范围。
	session := db.ChatSession{CookieID: "cid", ChatID: "receipt-chat", BuyerID: "buyer"}
	// incoming 是平台同步到本账号的对方消息，其 PNM 回执不得用于更新本账号出站状态。
	incoming := db.ChatMessage{MessageKey: "incoming-receipt.PNM", Direction: "incoming", MessageType: "text", Content: "你好", Status: "received", SentAt: 100}
	// stored、inserted、saveErr 分别是入站消息的保存结果、新增标记和不应出现的存储错误。
	stored, inserted, saveErr := store.Chats.SaveMessage(ctx, session, incoming, true)
	if saveErr != nil || !inserted || stored == nil {
		t.Fatalf("保存入站消息失败: stored=%+v inserted=%v err=%v", stored, inserted, saveErr)
	}
	// outgoing 是同一会话内仍待确认已读的本地出站消息，用于验证入站回执不会触发错误回退。
	outgoing := db.ChatMessage{MessageKey: "outgoing-pending.PNM", Direction: "outgoing", MessageType: "text", Content: "请问需要什么帮助", Status: "sent", SentAt: 200}
	// stored、inserted、saveErr 分别是出站消息的保存结果、新增标记和不应出现的存储错误。
	stored, inserted, saveErr = store.Chats.SaveMessage(ctx, session, outgoing, false)
	if saveErr != nil || !inserted || stored == nil {
		t.Fatalf("保存出站消息失败: stored=%+v inserted=%v err=%v", stored, inserted, saveErr)
	}
	// readErr 是入站消息回执处理错误；该跨端同步场景必须静默成功。
	readErr := bridge.HandleMessageRead(ctx, engine.MessageReadEvent{AccountID: "cid", ChatID: session.ChatID, MessageID: incoming.MessageKey, ReadAt: 300})
	if readErr != nil {
		t.Fatalf("入站消息回执不应报错: %v", readErr)
	}
	// pending、pendingErr 分别是待确认出站消息的最新状态和读取错误；它不能被入站回执误标已读。
	pending, pendingErr := store.Chats.GetMessageByKey(ctx, "cid", outgoing.MessageKey)
	if pendingErr != nil || pending.ReadStatus == 2 {
		t.Fatalf("入站回执误标出站消息: message=%+v err=%v", pending, pendingErr)
	}
	// fallbackErr 是找不到原始 PNM 时的回退处理错误；存在待确认出站消息时应按会话安全收口。
	fallbackErr := bridge.HandleMessageRead(ctx, engine.MessageReadEvent{AccountID: "cid", ChatID: session.ChatID, MessageID: "missing-receipt.PNM", ReadAt: 400})
	if fallbackErr != nil {
		t.Fatalf("缺失 PNM 的会话级回退不应报错: %v", fallbackErr)
	}
	// updated、updatedErr 分别是回退后出站消息状态和读取错误；它必须被标记为已读。
	updated, updatedErr := store.Chats.GetMessageByKey(ctx, "cid", outgoing.MessageKey)
	if updatedErr != nil || updated.ReadStatus != 2 || updated.ReadAt != 400 {
		t.Fatalf("会话级回退未更新出站消息: message=%+v err=%v", updated, updatedErr)
	}
	// noCandidateErr 是没有待确认出站消息时的历史回执处理错误，必须静默忽略而不是返回 WARN。
	noCandidateErr := bridge.HandleMessageRead(ctx, engine.MessageReadEvent{AccountID: "cid", ChatID: session.ChatID, MessageID: "historic-receipt.PNM", ReadAt: 500})
	if noCandidateErr != nil {
		t.Fatalf("无本地候选消息的历史回执不应报错: %v", noCandidateErr)
	}
}
