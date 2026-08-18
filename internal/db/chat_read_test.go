package db

import (
	"context"
	"testing"
)

// TestMarkMessageReadMarksPriorOutgoingHistory 验证平台对目标出站消息的已读回执会确认同会话中更早的出站历史。
func TestMarkMessageReadMarksPriorOutgoingHistory(t *testing.T) {
	// store、cleanup 分别保存隔离的 SQLite 存储和关闭函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库调用共用的上下文。
	ctx := context.Background()
	// userID 保存测试账号的数据库主键，用于建立会话所属账号。
	var userID int64
	// err 表示创建已读回执测试用户并读取其数据库主键时的错误。
	if err := store.DB.QueryRowContext(ctx, `INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`, "chat-read-owner", "chat-read-owner@example.com", "test-hash").Scan(&userID); err != nil {
		t.Fatalf("创建测试用户: %v", err)
	}
	// cookieID 是测试聊天所属的非敏感账号标识。
	const cookieID = "chat-read-account"
	// err 表示创建已读回执测试账号时的数据库错误。
	if err := store.Cookies.CreateOwned(ctx, cookieID, "test-cookie", userID); err != nil {
		t.Fatalf("创建测试账号: %v", err)
	}
	// session 保存三条出站消息共同所属的聊天会话。
	session := ChatSession{CookieID: cookieID, ChatID: "chat-read-session", BuyerID: "buyer-1", BuyerName: "买家"}
	// messages 按发送顺序构造两条应被确认的历史消息和一条仍未确认的新消息。
	messages := []ChatMessage{
		{MessageKey: "outgoing-1.PNM", Direction: "outgoing", MessageType: "text", Content: "第一条", Status: "sent", SentAt: 100},
		{MessageKey: "outgoing-2.PNM", Direction: "outgoing", MessageType: "text", Content: "第二条", Status: "sent", SentAt: 200},
		{MessageKey: "outgoing-3.PNM", Direction: "outgoing", MessageType: "text", Content: "第三条", Status: "sent", SentAt: 300},
	}
	// message 表示当前待写入并验证已读状态的测试出站消息。
	for _, message := range messages {
		// inserted、saveErr 分别表示本条测试消息是否首次保存及保存失败原因。
		if _, inserted, saveErr := store.Chats.SaveMessage(ctx, session, message, false); saveErr != nil || !inserted {
			t.Fatalf("保存测试消息 key=%s inserted=%v err=%v", message.MessageKey, inserted, saveErr)
		}
	}
	// updated、readErr 分别保存回执目标消息及处理回执失败原因。
	updated, readErr := store.Chats.MarkMessageRead(ctx, cookieID, "outgoing-2.PNM", 999)
	if readErr != nil || updated.ReadStatus != 2 || updated.ReadAt != 999 {
		t.Fatalf("处理已读回执 message=%+v err=%v", updated, readErr)
	}
	// saved 保存从数据库重新读取的消息，用来验证批量已读水位的实际持久化结果。
	saved, listErr := store.Chats.ListMessages(ctx, userID, cookieID, session.ChatID, 0, 10)
	if listErr != nil || len(saved) != 3 {
		t.Fatalf("读取确认结果 messages=%+v err=%v", saved, listErr)
	}
	if saved[0].ReadStatus != 2 || saved[1].ReadStatus != 2 || saved[2].ReadStatus != 0 {
		t.Fatalf("已读水位错误 statuses=%d,%d,%d", saved[0].ReadStatus, saved[1].ReadStatus, saved[2].ReadStatus)
	}
	if saved[0].ReadAt != 999 || saved[1].ReadAt != 999 || saved[2].ReadAt != 0 {
		t.Fatalf("已读时间错误 timestamps=%d,%d,%d", saved[0].ReadAt, saved[1].ReadAt, saved[2].ReadAt)
	}
}

// TestSaveIncomingMessageMarksPriorOutgoingHistoryRead 验证买家后续发言会确认此前出站历史已读，且不会确认其后的出站消息。
func TestSaveIncomingMessageMarksPriorOutgoingHistoryRead(t *testing.T) {
	// store、cleanup 分别保存隔离的 SQLite 存储和关闭函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试数据库调用共用的上下文。
	ctx := context.Background()
	// userID 保存测试账号的数据库主键，用于建立会话所属账号。
	var userID int64
	// err 表示创建聊天历史测试用户并读取其数据库主键时的错误。
	if err := store.DB.QueryRowContext(ctx, `INSERT INTO users (username,email,password_hash) VALUES (?,?,?) RETURNING id`, "chat-history-owner", "chat-history-owner@example.com", "test-hash").Scan(&userID); err != nil {
		t.Fatalf("创建测试用户: %v", err)
	}
	// cookieID 是测试聊天所属的非敏感账号标识。
	const cookieID = "chat-history-account"
	// err 表示创建聊天历史测试账号时的数据库错误。
	if err := store.Cookies.CreateOwned(ctx, cookieID, "test-cookie", userID); err != nil {
		t.Fatalf("创建测试账号: %v", err)
	}
	// session 保存测试消息共同所属的聊天会话。
	session := ChatSession{CookieID: cookieID, ChatID: "chat-history-session", BuyerID: "buyer-1", BuyerName: "买家"}
	// first 保存买家回复之前的出站消息，应被后续回复确认已读。
	first := ChatMessage{MessageKey: "history-first.PNM", Direction: "outgoing", MessageType: "text", Content: "第一条", Status: "sent", SentAt: 100}
	// inserted、saveErr 分别表示首条测试消息是否新增及保存失败原因。
	if _, inserted, saveErr := store.Chats.SaveMessage(ctx, session, first, false); saveErr != nil || !inserted {
		t.Fatalf("保存第一条出站消息 inserted=%v err=%v", inserted, saveErr)
	}
	// reply 保存买家后续消息，其到达表明此前出站内容已被阅读。
	reply := ChatMessage{MessageKey: "history-reply.PNM", Direction: "incoming", MessageType: "text", Content: "收到", Status: "received", SentAt: 200}
	// inserted、saveErr 分别表示买家回复是否新增及保存失败原因。
	if _, inserted, saveErr := store.Chats.SaveMessage(ctx, session, reply, true); saveErr != nil || !inserted {
		t.Fatalf("保存买家回复 inserted=%v err=%v", inserted, saveErr)
	}
	// last 保存买家回复之后的出站消息，不能被此前回复错误确认。
	last := ChatMessage{MessageKey: "history-last.PNM", Direction: "outgoing", MessageType: "text", Content: "第三条", Status: "sent", SentAt: 300}
	// inserted、saveErr 分别表示末条测试消息是否新增及保存失败原因。
	if _, inserted, saveErr := store.Chats.SaveMessage(ctx, session, last, false); saveErr != nil || !inserted {
		t.Fatalf("保存最后一条出站消息 inserted=%v err=%v", inserted, saveErr)
	}
	// saved 保存从数据库读取的最终已读状态。
	saved, listErr := store.Chats.ListMessages(ctx, userID, cookieID, session.ChatID, 0, 10)
	if listErr != nil || len(saved) != 3 {
		t.Fatalf("读取历史确认结果 messages=%+v err=%v", saved, listErr)
	}
	if saved[0].ReadStatus != 2 || saved[0].ReadAt != 200 || saved[2].ReadStatus != 0 || saved[2].ReadAt != 0 {
		t.Fatalf("历史已读确认错误 first=%+v last=%+v", saved[0], saved[2])
	}
}
