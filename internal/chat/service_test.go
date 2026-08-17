package chat

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

func TestRecordHistoryPageParsesDirectionMediaAndDeduplicates(t *testing.T) {
	store, cleanup := chatTestStore(t)
	defer cleanup()
	service := New(store)
	ctx := context.Background()
	encoded := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
	body := map[string]any{
		"hasMore": float64(1), "nextCursor": float64(12345),
		"userMessageModels": []any{
			map[string]any{"message": map[string]any{"messageId": "m2", "createAt": float64(2000), "extension": `{"senderUserId":"self@goofish","reminderTitle":"我"}`, "content": map[string]any{"custom": map[string]any{"data": encoded(`{"contentType":2,"image":{"pics":[{"url":"https://img.example/2.jpg"}]}}`)}}}},
			map[string]any{"message": map[string]any{"messageId": "m1", "createAt": float64(1000), "extension": map[string]any{"senderUserId": "peer@goofish", "reminderTitle": "对方"}, "content": map[string]any{"custom": map[string]any{"data": encoded(`{"contentType":1,"text":{"text":"较早的消息"}}`)}}}},
		},
	}
	session := db.ChatSession{CookieID: "account-1", ChatID: "cid", BuyerID: "peer", BuyerName: "对方"}
	page, err := service.RecordHistoryPage(ctx, "account-1", "cid", "self", session, body)
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || page.NextCursor != 12345 || len(page.Messages) != 2 {
		t.Fatalf("unexpected page: %+v", page)
	}
	if page.Messages[0].Direction != "incoming" || page.Messages[0].Content != "较早的消息" {
		t.Fatalf("unexpected incoming: %+v", page.Messages[0])
	}
	if page.Messages[1].Direction != "outgoing" || page.Messages[1].MessageType != "image" || page.Messages[1].Content != "https://img.example/2.jpg" {
		t.Fatalf("unexpected outgoing image: %+v", page.Messages[1])
	}
	if _, err := service.RecordHistoryPage(ctx, "account-1", "cid", "self", session, body); err != nil {
		t.Fatal(err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "owner")
	rows, err := store.Chats.ListMessages(ctx, owner.ID, "account-1", "cid", 0, 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 2 {
		t.Fatalf("history retry inserted duplicates: %d", len(rows))
	}
	_, _, err = store.Chats.SaveMessage(ctx, session, db.ChatMessage{MessageKey: "system-later", Direction: "incoming", SenderID: "peer", SenderName: "快给ta一个评价吧～", MessageType: "text", Content: "快给ta一个评价吧～", Status: "received", SentAt: 3000}, false)
	if err != nil {
		t.Fatal(err)
	}
	name, err := store.Chats.LatestUnmaskedPeerName(ctx, "account-1", "cid")
	if err != nil || name != "对方" {
		t.Fatalf("historical nickname=%q err=%v", name, err)
	}
}

func TestRecordHistoryPageClassifiesOfficialCardsAsSystem(t *testing.T) {
	store, cleanup := chatTestStore(t)
	defer cleanup()
	service := New(store)
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"contentType":26,"dxCard":{"item":{"main":{"exContent":{"title":"我已拍下，待付款"}}}}}`))
	body := map[string]any{"userMessageModels": []any{
		map[string]any{"message": map[string]any{
			"messageId": "official-card", "createAt": float64(3000),
			"extension": map[string]any{"senderUserId": "peer@goofish", "reminderTitle": "买家已拍下，待付款"},
			"content":   map[string]any{"custom": map[string]any{"data": encoded, "summary": "[我已拍下，待付款]"}},
		}},
	}}
	session := db.ChatSession{CookieID: "account-1", ChatID: "official", BuyerID: "peer", BuyerName: "真实昵称"}
	if _, _, err := store.Chats.SaveMessage(context.Background(), session, db.ChatMessage{
		MessageKey: "official-card", Direction: "incoming", SenderID: "peer", SenderName: "真实昵称",
		MessageType: "text", Content: "[我已拍下，待付款]", Status: "received", SentAt: 3000,
	}, false); err != nil {
		t.Fatal(err)
	}
	page, err := service.RecordHistoryPage(context.Background(), "account-1", "official", "self", session, body)
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Messages) != 1 || page.Messages[0].MessageType != "system" || page.Messages[0].Direction != "incoming" {
		t.Fatalf("official card was not classified as system: %+v", page.Messages)
	}
	if page.Messages[0].SenderName != "真实昵称" {
		t.Fatalf("history sender metadata unexpectedly changed: %+v", page.Messages[0])
	}
}

func TestRecordIncomingClassifiesXianxiaomiAndPlaceholder(t *testing.T) {
	store, cleanup := chatTestStore(t)
	defer cleanup()
	service := New(store)
	message, inserted, err := service.RecordIncoming(context.Background(), Incoming{
		AccountID: "account-1", ChatID: "xiaomi", BuyerID: "1400@goofish",
		BuyerName: "闲小蜜发来一条新消息", Text: "邀您填写售后问卷",
		Raw: map[string]any{"messageId": "xiaomi-1"},
	})
	if err != nil || !inserted {
		t.Fatalf("record xianxiaomi message: message=%+v inserted=%v err=%v", message, inserted, err)
	}
	if message.MessageType != "system" || message.SenderName != "闲小蜜" {
		t.Fatalf("xianxiaomi message was not classified: %+v", message)
	}
}

func TestRecordIncomingExtractsMessageIDFromEncodedExtension(t *testing.T) {
	store, cleanup := chatTestStore(t)
	defer cleanup()
	service := New(store)
	message, _, err := service.RecordIncoming(context.Background(), Incoming{
		AccountID: "account-1", ChatID: "live", BuyerID: "peer", BuyerName: "对方", Text: "实时消息",
		Raw: map[string]any{"1": map[string]any{"10": map[string]any{
			"extJson": `{"messageId":"live-123"}`,
		}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if message.MessageKey != "live-123" {
		t.Fatalf("实时消息未提取平台 messageId: %+v", message)
	}
}

func TestRecordConversationPageImportsHistoricalContacts(t *testing.T) {
	store, cleanup := chatTestStore(t)
	defer cleanup()
	service := New(store)
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"contentType":1,"text":{"text":"历史消息"}}`))
	if err := store.Chats.UpsertSession(context.Background(), db.ChatSession{CookieID: "account-1", ChatID: "history-cid", BuyerID: "peer-9", LastMessage: "错误的新摘要", LastMessageAt: 987654}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"hasMore": true, "nextCursor": float64(888), "userConvs": []any{
		map[string]any{"singleChatUserConversation": map[string]any{
			"singleChatConversation": map[string]any{"cid": "history-cid@goofish", "pairFirst": "self@goofish", "pairSecond": "peer-9@goofish", "extension": `{"itemTitle":"旧商品"}`},
			"lastMessage":            map[string]any{"message": map[string]any{"createAt": float64(123456), "extension": map[string]any{"senderUserId": "peer-9@goofish", "reminderTitle": "历史用户"}, "content": map[string]any{"custom": map[string]any{"data": encoded}}}},
			"modifyTime":             float64(987654), "redPoint": float64(2),
		}},
	}}
	page, err := service.RecordConversationPage(context.Background(), "account-1", "self", body)
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || page.NextCursor != 888 {
		t.Fatalf("unexpected page: %+v", page)
	}
	owner, _ := store.Users.GetByUsername(context.Background(), "owner")
	rows, err := store.Chats.ListSessions(context.Background(), owner.ID, "account-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].BuyerID != "peer-9" || rows[0].BuyerName != "" || rows[0].LastMessage != "历史消息" || rows[0].UnreadCount != 2 {
		t.Fatalf("unexpected historical contact: %+v", rows)
	}
	if rows[0].LastMessageAt != 123456 {
		t.Fatalf("used conversation modifyTime instead of last message createAt: %d", rows[0].LastMessageAt)
	}
}

func TestRecordConversationPageHandlesXianxiaomiAndRemovesInvisibleSessions(t *testing.T) {
	store, cleanup := chatTestStore(t)
	defer cleanup()
	ctx := context.Background()
	service := New(store)
	if err := store.Chats.UpsertSession(ctx, db.ChatSession{CookieID: "account-1", ChatID: "hidden", BuyerID: "peer", LastMessage: "暂无消息"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Chats.UpsertSession(ctx, db.ChatSession{CookieID: "account-1", ChatID: "platform", BuyerID: "900", LastMessage: "暂无消息"}); err != nil {
		t.Fatal(err)
	}
	body := map[string]any{"userConvs": []any{
		map[string]any{"singleChatUserConversation": map[string]any{"visible": float64(0), "singleChatConversation": map[string]any{"cid": "hidden@goofish"}}},
		map[string]any{"singleChatUserConversation": map[string]any{"visible": float64(1), "singleChatConversation": map[string]any{"cid": "platform@goofish", "pairFirst": "self@goofish", "pairSecond": "0@goofish", "extension": map[string]any{"extUserId": "900"}}}},
		map[string]any{"singleChatUserConversation": map[string]any{"visible": float64(1), "modifyTime": float64(123),
			"singleChatConversation": map[string]any{"cid": "xiaomi@goofish", "pairFirst": "self@goofish", "pairSecond": "0@goofish", "extension": map[string]any{"extUserId": "1400"}},
			"redPoint":               float64(3),
			"lastMessage":            map[string]any{"message": map[string]any{"extension": map[string]any{"senderUserId": "1400@goofish", "reminderTitle": "闲小蜜发来一条新消息"}, "content": map[string]any{"custom": map[string]any{"summary": "邀您填写售后问卷"}}}}}},
	}}
	if _, err := service.RecordConversationPage(ctx, "account-1", "self", body); err != nil {
		t.Fatal(err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "owner")
	rows, err := store.Chats.ListSessions(ctx, owner.ID, "account-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].BuyerID != "1400" || rows[0].BuyerName != "闲小蜜" || rows[0].BuyerAvatar != xianxiaomiAvatar || rows[0].UnreadCount != 0 {
		t.Fatalf("unexpected sessions: %+v", rows)
	}
}

func TestConversationUnreadCountUsesRedPointButFiltersSystemMessages(t *testing.T) {
	store, cleanup := chatTestStore(t)
	defer cleanup()
	service := New(store)

	systemCard := base64.StdEncoding.EncodeToString([]byte(`{"contentType":26}`))
	systemLast := map[string]any{
		"extension":   map[string]any{"senderUserId": "peer@goofish"},
		"content":     map[string]any{"custom": map[string]any{"summary": "[交易通知]", "data": systemCard}},
		"unreadCount": float64(1), "readStatus": float64(1),
	}
	if got := service.conversationUnreadCount(context.Background(), "account-1", "system-last", "peer", map[string]any{"redPoint": float64(3)}, systemLast, "[交易通知]"); got != 2 {
		t.Fatalf("系统未读未从 redPoint 扣除: got=%d", got)
	}
	if got := service.conversationUnreadCount(context.Background(), "account-1", "xiaomi", "1400", map[string]any{"redPoint": float64(3)}, systemLast, "[交易通知]"); got != 0 {
		t.Fatalf("闲小蜜全是系统消息时仍显示红点: got=%d", got)
	}

	if _, _, err := service.RecordIncoming(context.Background(), Incoming{
		AccountID: "account-1", ChatID: "real", BuyerID: "peer", BuyerName: "真实用户", Text: "未读消息",
		MessageID: "real-unread", Raw: map[string]any{"messageId": "real-unread"},
	}); err != nil {
		t.Fatal(err)
	}
	userLast := map[string]any{
		"extension": map[string]any{"senderUserId": "peer@goofish"},
		"content":   map[string]any{"custom": map[string]any{"summary": "未读消息"}},
	}
	if got := service.conversationUnreadCount(context.Background(), "account-1", "real", "peer", map[string]any{"redPoint": float64(3)}, userLast, "未读消息"); got != 1 {
		t.Fatalf("未使用消息级真实未读数: got=%d", got)
	}
}

func TestHistoryMessageIsSystem(t *testing.T) {
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"contentType":26,"dxCard":{}}`))
	last := map[string]any{
		"extension": map[string]any{"senderUserId": "peer@goofish"},
		"content":   map[string]any{"custom": map[string]any{"data": encoded}},
	}
	if !historyMessageIsSystem(last, "[我已拍下，待付款]") {
		t.Fatal("交易卡片应被识别为系统消息")
	}
	if historyMessageIsSystem(map[string]any{
		"extension": map[string]any{"senderUserId": "peer@goofish"},
		"content":   map[string]any{"custom": map[string]any{"summary": "你好"}},
	}, "你好") {
		t.Fatal("真实用户文本不应被识别为系统消息")
	}
}

func TestRecordConversationPageSkipsEmptyConversationShells(t *testing.T) {
	store, cleanup := chatTestStore(t)
	defer cleanup()
	service := New(store)
	body := map[string]any{"userConvs": []any{
		map[string]any{"singleChatUserConversation": map[string]any{
			"singleChatConversation": map[string]any{"cid": "empty@goofish", "pairFirst": "self@goofish", "pairSecond": "69@goofish"},
		}},
		map[string]any{"singleChatUserConversation": map[string]any{
			"singleChatConversation": map[string]any{"cid": "system@goofish", "pairFirst": "self@goofish", "pairSecond": "1400@goofish"},
			"lastMessage": map[string]any{"message": map[string]any{
				"createAt": float64(100), "reminderContent": "邀您填写售后问卷",
			}},
		}},
	}}
	if _, err := service.RecordConversationPage(context.Background(), "account-1", "self", body); err != nil {
		t.Fatal(err)
	}
	owner, _ := store.Users.GetByUsername(context.Background(), "owner")
	rows, err := store.Chats.ListSessions(context.Background(), owner.ID, "account-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ChatID != "system" {
		t.Fatalf("empty conversation shell was imported: %+v", rows)
	}
}

func TestDeleteEmptySessionsRemovesGhostsButKeepsRealConversation(t *testing.T) {
	store, cleanup := chatTestStore(t)
	defer cleanup()
	ctx := context.Background()
	ghost := db.ChatSession{CookieID: "account-1", ChatID: "ghost", BuyerID: "peer-ghost", LastMessage: "暂无消息", LastMessageAt: 100}
	if err := store.Chats.UpsertSession(ctx, ghost); err != nil {
		t.Fatal(err)
	}
	real := db.ChatSession{CookieID: "account-1", ChatID: "real", BuyerID: "peer-real", LastMessage: "暂无消息", LastMessageAt: 200}
	if _, _, err := store.Chats.SaveMessage(ctx, real, db.ChatMessage{MessageKey: "real-1", Direction: "incoming", SenderID: "peer-real", SenderName: "真实用户", MessageType: "text", Content: "真实消息", Status: "received", SentAt: 200}, false); err != nil {
		t.Fatal(err)
	}
	if err := store.Chats.DeleteEmptySessions(ctx, "account-1"); err != nil {
		t.Fatal(err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "owner")
	rows, err := store.Chats.ListSessions(ctx, owner.ID, "account-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ChatID != "real" {
		t.Fatalf("unexpected sessions after pruning: %+v", rows)
	}
}

func TestValidNicknameRejectsSystemReminderTitles(t *testing.T) {
	for _, value := range []string{"", "203591535", "x***3", "快给ta一个评价吧～", "[卖家已发货]", "闲小蜜发来一条新消息"} {
		if ValidNickname(value) {
			t.Fatalf("system reminder accepted as nickname: %q", value)
		}
	}
	if !ValidNickname("纽约做手工的石斑") {
		t.Fatal("real nickname rejected")
	}
}

func TestIncomingMessagePersistsDeduplicatesAndPublishesByOwner(t *testing.T) {
	store, cleanup := chatTestStore(t)
	defer cleanup()
	ctx := context.Background()
	owner, _ := store.Users.GetByUsername(ctx, "owner")
	other, _ := store.Users.GetByUsername(ctx, "other")
	service := New(store)
	ownerEvents, cancelOwner, err := service.Subscribe(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelOwner()
	otherEvents, cancelOther, err := service.Subscribe(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelOther()

	incoming := Incoming{AccountID: "account-1", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家甲",
		Text: "你好", ItemID: "item-1", Raw: map[string]any{"messageId": "platform-1", "sendTime": int64(1234567890000)}}
	message, inserted, err := service.RecordIncoming(ctx, incoming)
	if err != nil || !inserted || message.MessageKey != "platform-1" {
		t.Fatalf("message=%+v inserted=%v err=%v", message, inserted, err)
	}
	if _, inserted, err := service.RecordIncoming(ctx, incoming); err != nil || inserted {
		t.Fatalf("duplicate inserted=%v err=%v", inserted, err)
	}
	select {
	case event := <-ownerEvents:
		if event.Type != "message.created" || event.Message.MessageKey != "platform-1" {
			t.Fatalf("event=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("owner did not receive event")
	}
	select {
	case event := <-otherEvents:
		t.Fatalf("other owner leaked event: %+v", event)
	case <-time.After(30 * time.Millisecond):
	}
}

func TestExtractMessageContentSupportsImageAndVideo(t *testing.T) {
	imageRaw := map[string]any{"payload": `{"contentType":2,"image":{"pics":[{"url":"https://cdn/image.jpg"}]}}`}
	if kind, content := extractMessageContent(imageRaw, "[图片]"); kind != "image" || content != "https://cdn/image.jpg" {
		t.Fatalf("image kind=%q content=%q", kind, content)
	}
	videoRaw := map[string]any{"content": map[string]any{"video": map[string]any{"playUrl": "https://cdn/video.mp4"}}}
	if kind, content := extractMessageContent(videoRaw, "[视频]"); kind != "video" || content != "https://cdn/video.mp4" {
		t.Fatalf("video kind=%q content=%q", kind, content)
	}
	if kind, content := extractMessageContent(nil, " 你好 "); kind != "text" || content != "你好" {
		t.Fatalf("text kind=%q content=%q", kind, content)
	}
}

func chatTestStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	database, dialect, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	store := db.NewStore(database, dialect)
	if ok, err := store.Users.Create(context.Background(), "owner", "owner@example.com", "pw"); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := store.Users.Create(context.Background(), "other", "other@example.com", "pw"); err != nil || !ok {
		t.Fatal(err)
	}
	owner, _ := store.Users.GetByUsername(context.Background(), "owner")
	other, _ := store.Users.GetByUsername(context.Background(), "other")
	if err := store.Cookies.Save(context.Background(), "account-1", "unb=1", owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Cookies.Save(context.Background(), "account-2", "unb=2", other.ID); err != nil {
		t.Fatal(err)
	}
	return store, func() { _ = database.Close() }
}
