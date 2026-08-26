package db

import (
	"context"
	"errors"
	"testing"
)

// TestChatSessionAndMessageLifecycle 覆盖聊天会话、消息幂等、未读计数和已读回执路径。
func TestChatSessionAndMessageLifecycle(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存聊天账号归属。
	userID, cookieID := seedAccount(t, store)
	// session 保存本测试消息共同所属的会话。
	session := ChatSession{CookieID: cookieID, ChatID: "chat", BuyerID: "buyer", BuyerName: "买家", ItemID: "item", ItemTitle: "商品", LastMessageAt: 1}
	// err 表示初次创建聊天会话的数据库错误。
	if err := store.Chats.UpsertSession(ctx, session); err != nil {
		t.Fatal(err)
	}
	// err 表示会话身份增量更新的数据库错误。
	if err := store.Chats.UpdateSessionIdentity(ctx, cookieID, "chat", "buyer-2", "买家2", "avatar"); err != nil {
		t.Fatal(err)
	}
	// incoming 保存一条真实用户入站消息，用于未读与昵称回退测试。
	incoming := ChatMessage{MessageKey: "in-1", Direction: "incoming", SenderID: "buyer-2", SenderName: "真实昵称", MessageType: "text", Content: "你好", Status: "received", SentAt: 100}
	// savedIncoming、inserted、saveErr 保存入站消息首次写入结果。
	savedIncoming, inserted, saveErr := store.Chats.SaveMessage(ctx, session, incoming, true)
	if saveErr != nil || !inserted || savedIncoming == nil {
		t.Fatalf("incoming=%+v inserted=%v err=%v", savedIncoming, inserted, saveErr)
	}
	// duplicate、duplicateInserted 验证同一消息键不会重复增加未读数。
	duplicate, duplicateInserted, duplicateErr := store.Chats.SaveMessage(ctx, session, incoming, true)
	if duplicateErr != nil || duplicateInserted || duplicate == nil {
		t.Fatalf("duplicate=%+v inserted=%v err=%v", duplicate, duplicateInserted, duplicateErr)
	}
	// outgoing 保存一条待回执的出站消息。
	outgoing := ChatMessage{MessageKey: "out-1", Direction: "outgoing", SenderName: "我", MessageType: "text", Content: "稍等", Status: "sent", SentAt: 90}
	// err 表示出站消息写入错误。
	if _, outgoingInserted, err := store.Chats.SaveMessage(ctx, session, outgoing, false); err != nil || !outgoingInserted {
		t.Fatalf("outgoing inserted=%v err=%v", outgoingInserted, err)
	}
	// system 保存系统入站消息，验证系统消息不增加用户未读数。
	system := ChatMessage{MessageKey: "system-1", Direction: "incoming", SenderName: "交易消息", MessageType: "system", Content: "系统", Status: "received", SentAt: 110}
	// err 表示系统消息写入错误。
	if _, _, err := store.Chats.SaveMessage(ctx, session, system, true); err != nil {
		t.Fatal(err)
	}
	// count、countErr 保存真实用户未读数。
	count, countErr := store.Chats.CountUnreadUserMessages(ctx, cookieID, "chat")
	if countErr != nil || count != 1 {
		t.Fatalf("unread count=%d err=%v", count, countErr)
	}
	// sessions、sessionsErr 保存会话列表结果。
	sessions, sessionsErr := store.Chats.ListSessions(ctx, userID, cookieID, 0)
	if sessionsErr != nil || len(sessions) != 1 || sessions[0].ChatID != "chat" {
		t.Fatalf("sessions=%+v err=%v", sessions, sessionsErr)
	}
	// messages、messagesErr 保存按时间正序排列的消息列表。
	messages, messagesErr := store.Chats.ListMessages(ctx, userID, cookieID, "chat", 0, 0)
	if messagesErr != nil || len(messages) != 3 || messages[0].MessageKey != "out-1" || messages[2].MessageKey != "system-1" {
		t.Fatalf("messages=%+v err=%v", messages, messagesErr)
	}
	// paged、pagedErr 覆盖带 beforeID 的消息分页条件。
	paged, pagedErr := store.Chats.ListMessages(ctx, userID, cookieID, "chat", messages[1].ID, 10)
	if pagedErr != nil || len(paged) == 0 {
		t.Fatalf("paged=%+v err=%v", paged, pagedErr)
	}
	// err 表示把会话摘要改为平台遮罩昵称时的数据库错误。
	if err := store.Chats.UpdateSessionIdentity(ctx, cookieID, "chat", "", "x***3", ""); err != nil {
		t.Fatal(err)
	}
	// nickname、nicknameErr 保存未遮罩历史昵称回退结果。
	nickname, nicknameErr := store.Chats.BuyerNicknameForAutomation(ctx, cookieID, "chat")
	if nicknameErr != nil || nickname != "真实昵称" {
		t.Fatalf("nickname=%q err=%v", nickname, nicknameErr)
	}
	// err 表示将会话摘要更新为较新平台事实的数据库错误。
	if err := store.Chats.SyncSessionSummary(ctx, cookieID, "chat", "最新", 200, 200, 4); err != nil {
		t.Fatal(err)
	}
	// err 表示消息正文分类更新的数据库错误。
	if err := store.Chats.UpdateMessageContent(ctx, cookieID, "in-1", "image", "https://example.test/image"); err != nil {
		t.Fatal(err)
	}
	// err 表示媒体时长更新的数据库错误。
	if err := store.Chats.UpdateMessageMediaDuration(ctx, cookieID, "in-1", 8); err != nil {
		t.Fatal(err)
	}
	// updated、updatedErr 保存消息状态更新后的完整消息。
	updated, updatedErr := store.Chats.UpdateMessageStatus(ctx, cookieID, "in-1", "processed")
	if updatedErr != nil || updated == nil || updated.Status != "processed" || updated.MediaDuration != 8 {
		t.Fatalf("updated=%+v err=%v", updated, updatedErr)
	}
	// incomingRead、incomingReadErr 验证入站回执不改变消息方向状态。
	incomingRead, incomingReadErr := store.Chats.MarkMessageRead(ctx, cookieID, "in-1", 123)
	if incomingReadErr != nil || incomingRead == nil || incomingRead.ReadStatus != 0 {
		t.Fatalf("incoming read=%+v err=%v", incomingRead, incomingReadErr)
	}
	// readOutgoing、readOutgoingErr 保存目标出站消息的已读回执结果。
	readOutgoing, readOutgoingErr := store.Chats.MarkLatestOutgoingRead(ctx, cookieID, "chat", 456)
	if readOutgoingErr != nil || readOutgoing == nil || readOutgoing.ReadStatus != 2 || readOutgoing.ReadAt != 456 {
		t.Fatalf("outgoing read=%+v err=%v", readOutgoing, readOutgoingErr)
	}
	// err 表示按用户归属清零聊天红点的数据库错误。
	if err := store.Chats.MarkRead(ctx, userID, cookieID, "chat"); err != nil {
		t.Fatal(err)
	}
	// countAfterRead、countAfterReadErr 保存已读后的用户未读数。
	countAfterRead, countAfterReadErr := store.Chats.CountUnreadUserMessages(ctx, cookieID, "chat")
	if countAfterReadErr != nil || countAfterRead != 0 {
		t.Fatalf("after read count=%d err=%v", countAfterRead, countAfterReadErr)
	}
	// err 表示删除聊天会话的数据库错误。
	if err := store.Chats.DeleteSession(ctx, cookieID, "chat"); err != nil {
		t.Fatal(err)
	}
	// missingMessage、missingMessageErr 验证删除后消息随会话级联消失。
	missingMessage, missingMessageErr := store.Chats.GetMessageByKey(ctx, cookieID, "in-1")
	if missingMessage != nil || !errors.Is(missingMessageErr, ErrNotFound) {
		t.Fatalf("missing message=%+v err=%v", missingMessage, missingMessageErr)
	}
}
