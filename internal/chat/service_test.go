package chat

import (
	"context"
	"encoding/base64"
	"path/filepath"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestRecordHistoryPageParsesDirectionMediaAndDeduplicates 封装TestRecordHistory页码ParsesDirectionMediaAndDeduplicates业务协调。
func TestRecordHistoryPageParsesDirectionMediaAndDeduplicates(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// service 用于本次流程后续判断的service
	service := New(store)
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// encoded 用于本次流程后续判断的encoded
	encoded := func(value string) string { return base64.StdEncoding.EncodeToString([]byte(value)) }
	// body 用于本次流程后续判断的请求体
	body := map[string]any{
		"hasMore": float64(1), "nextCursor": float64(12345),
		"userMessageModels": []any{
			map[string]any{"message": map[string]any{"messageId": "m2", "createAt": float64(2000), "extension": `{"senderUserId":"self@goofish","reminderTitle":"我"}`, "content": map[string]any{"custom": map[string]any{"data": encoded(`{"contentType":2,"image":{"pics":[{"url":"https://img.example/2.jpg"}]}}`)}}}},
			map[string]any{"message": map[string]any{"messageId": "m1", "createAt": float64(1000), "extension": map[string]any{"senderUserId": "peer@goofish", "reminderTitle": "对方"}, "content": map[string]any{"custom": map[string]any{"data": encoded(`{"contentType":1,"text":{"text":"较早的消息"}}`)}}}},
		},
	}
	// session 用于本次流程后续判断的会话
	session := db.ChatSession{CookieID: "account-1", ChatID: "cid", BuyerID: "peer", BuyerName: "对方"}
	// page、err 用于本次流程后续判断的page、err
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
	if // err 用于本次流程后续判断的err
	_, err := service.RecordHistoryPage(ctx, "account-1", "cid", "self", session, body); err != nil {
		t.Fatal(err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "owner")
	// rows、err 用于本次流程后续判断的rows、err
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
	// name、err 用于本次流程后续判断的name、err
	name, err := store.Chats.LatestUnmaskedPeerName(ctx, "account-1", "cid")
	if err != nil || name != "对方" {
		t.Fatalf("historical nickname=%q err=%v", name, err)
	}
}

// TestRecordHistoryPageRepairsStoredAudioPlaceholder 验证历史刷新能把旧版已落库的“[语音]”占位行升级为可播放 AMR 地址。
func TestRecordHistoryPageRepairsStoredAudioPlaceholder(t *testing.T) {
	// store 和 cleanup 分别是 SQLite 测试存储及资源释放函数。
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// ctx 是本次占位消息写入和历史修复共享的测试上下文。
	ctx := context.Background()
	// service 是执行历史消息归一和富媒体纠正的聊天服务。
	service := New(store)
	// session 保存语音消息所属的非敏感会话信息。
	session := db.ChatSession{CookieID: "account-1", ChatID: "audio-chat", BuyerID: "peer", BuyerName: "对方"}
	if // saveErr 表示模拟旧版保存语音占位行时的数据库错误。
	_, _, saveErr := store.Chats.SaveMessage(ctx, session, db.ChatMessage{
		MessageKey: "audio-message", Direction: "incoming", SenderID: "peer", SenderName: "对方",
		MessageType: "text", Content: "[语音]", Status: "received", SentAt: 3000,
	}, false); saveErr != nil {
		t.Fatal(saveErr)
	}
	// encodedAudio 是历史接口 custom.data 中 Base64 编码的真实语音载荷。
	encodedAudio := base64.StdEncoding.EncodeToString([]byte(`{"contentType":3,"audio":{"duration":3,"url":"https://media.example/voice.amr"}}`))
	// body 模拟同一平台消息稍后从历史接口返回完整 AMR 地址。
	body := map[string]any{"userMessageModels": []any{
		map[string]any{"message": map[string]any{
			"messageId": "audio-message", "createAt": float64(3000),
			"extension": map[string]any{"senderUserId": "peer@goofish", "reminderTitle": "对方"},
			"content":   map[string]any{"custom": map[string]any{"data": encodedAudio, "summary": "[语音]"}},
		}},
	}}
	// page 和 historyErr 分别是修复后的历史页及处理错误。
	page, historyErr := service.RecordHistoryPage(ctx, "account-1", "audio-chat", "self", session, body)
	if historyErr != nil {
		t.Fatal(historyErr)
	}
	if len(page.Messages) != 1 || page.Messages[0].MessageType != "audio" || page.Messages[0].Content != "https://media.example/voice.amr" || page.Messages[0].MediaDuration != 3 {
		t.Fatalf("history audio was not repaired: %+v", page.Messages)
	}
	// stored 和 readErr 分别是数据库中修复后的消息及读取错误。
	stored, readErr := store.Chats.GetMessageByKey(ctx, "account-1", "audio-message")
	if readErr != nil || stored.MessageType != "audio" || stored.Content != "https://media.example/voice.amr" || stored.MediaDuration != 3 {
		t.Fatalf("stored audio placeholder was not repaired: message=%+v err=%v", stored, readErr)
	}
}

// TestRecordHistoryPageClassifiesOfficialCardsAsSystem 封装TestRecordHistory页码ClassifiesOfficial卡密列表As系统业务协调。
func TestRecordHistoryPageClassifiesOfficialCardsAsSystem(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// service 用于本次流程后续判断的service
	service := New(store)
	// encoded 用于本次流程后续判断的encoded
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"contentType":26,"dxCard":{"item":{"main":{"exContent":{"title":"我已拍下，待付款"}}}}}`))
	// body 用于本次流程后续判断的请求体
	body := map[string]any{"userMessageModels": []any{
		map[string]any{"message": map[string]any{
			"messageId": "official-card", "createAt": float64(3000),
			"extension": map[string]any{"senderUserId": "peer@goofish", "reminderTitle": "买家已拍下，待付款"},
			"content":   map[string]any{"custom": map[string]any{"data": encoded, "summary": "[我已拍下，待付款]"}},
		}},
	}}
	// session 用于本次流程后续判断的会话
	session := db.ChatSession{CookieID: "account-1", ChatID: "official", BuyerID: "peer", BuyerName: "真实昵称"}
	if // err 用于本次流程后续判断的err
	_, _, err := store.Chats.SaveMessage(context.Background(), session, db.ChatMessage{
		MessageKey: "official-card", Direction: "incoming", SenderID: "peer", SenderName: "真实昵称",
		MessageType: "text", Content: "[我已拍下，待付款]", Status: "received", SentAt: 3000,
	}, false); err != nil {
		t.Fatal(err)
	}
	// page、err 用于本次流程后续判断的page、err
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

// TestRecordIncomingClassifiesXianxiaomiAndPlaceholder 封装TestRecordIncomingClassifiesXianxiaomiAndPlaceholder业务协调。
func TestRecordIncomingClassifiesXianxiaomiAndPlaceholder(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// service 用于本次流程后续判断的service
	service := New(store)
	// message、inserted、err 用于本次流程后续判断的message、inserted、err
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

// TestRecordIncomingExtractsMessageIDFromEncodedExtension 验证嵌套扩展中的平台消息键优先进入实时落库。
func TestRecordIncomingExtractsMessageIDFromEncodedExtension(t *testing.T) {
	// store、cleanup 保存隔离聊天数据库及清理函数，确保消息键提取不依赖其他用例数据。
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// service 是待测聊天服务，使用上述存储验证实时消息落库。
	service := New(store)
	// message、err 保存落库后的消息和处理错误，消息键必须来自编码扩展字段。
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

// TestRecordConversationPageImportsHistoricalContacts 验证联系人历史页不会覆盖较新的会话摘要。
func TestRecordConversationPageImportsHistoricalContacts(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// service 用于本次流程后续判断的service
	service := New(store)
	// encoded 用于本次流程后续判断的encoded
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"contentType":1,"text":{"text":"历史消息"}}`))
	if // err 用于本次流程后续判断的err
	err := store.Chats.UpsertSession(context.Background(), db.ChatSession{CookieID: "account-1", ChatID: "history-cid", BuyerID: "peer-9", LastMessage: "错误的新摘要", LastMessageAt: 987654}); err != nil {
		t.Fatal(err)
	}
	// body 用于本次流程后续判断的请求体
	body := map[string]any{"hasMore": true, "nextCursor": float64(888), "userConvs": []any{
		map[string]any{"singleChatUserConversation": map[string]any{
			"singleChatConversation": map[string]any{"cid": "history-cid@goofish", "pairFirst": "self@goofish", "pairSecond": "peer-9@goofish", "extension": `{"itemTitle":"旧商品","itemMainPic":"https://img.example/history-item.jpg"}`},
			"lastMessage":            map[string]any{"message": map[string]any{"createAt": float64(123456), "extension": map[string]any{"senderUserId": "peer-9@goofish", "reminderTitle": "历史用户"}, "content": map[string]any{"custom": map[string]any{"data": encoded}}}},
			"modifyTime":             float64(987654), "redPoint": float64(2),
		}},
	}}
	// page、err 用于本次流程后续判断的page、err
	page, err := service.RecordConversationPage(context.Background(), "account-1", "self", body)
	if err != nil {
		t.Fatal(err)
	}
	if !page.HasMore || page.NextCursor != 888 {
		t.Fatalf("unexpected page: %+v", page)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(context.Background(), "owner")
	// rows、err 用于本次流程后续判断的rows、err
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
	if rows[0].ItemImageURL != "https://img.example/history-item.jpg" {
		t.Fatalf("conversation item image=%q", rows[0].ItemImageURL)
	}
}

// TestRecordConversationPageHandlesXianxiaomiAndRemovesInvisibleSessions 封装TestRecordConversation页码HandlesXianxiaomiAndRemovesInvisibleSessions业务协调。
func TestRecordConversationPageHandlesXianxiaomiAndRemovesInvisibleSessions(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// service 用于本次流程后续判断的service
	service := New(store)
	if // err 用于本次流程后续判断的err
	err := store.Chats.UpsertSession(ctx, db.ChatSession{CookieID: "account-1", ChatID: "hidden", BuyerID: "peer", LastMessage: "暂无消息"}); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Chats.UpsertSession(ctx, db.ChatSession{CookieID: "account-1", ChatID: "platform", BuyerID: "900", LastMessage: "暂无消息"}); err != nil {
		t.Fatal(err)
	}
	// body 用于本次流程后续判断的请求体
	body := map[string]any{"userConvs": []any{
		map[string]any{"singleChatUserConversation": map[string]any{"visible": float64(0), "singleChatConversation": map[string]any{"cid": "hidden@goofish"}}},
		map[string]any{"singleChatUserConversation": map[string]any{"visible": float64(1), "singleChatConversation": map[string]any{"cid": "platform@goofish", "pairFirst": "self@goofish", "pairSecond": "0@goofish", "extension": map[string]any{"extUserId": "900"}}}},
		map[string]any{"singleChatUserConversation": map[string]any{"visible": float64(1), "modifyTime": float64(123),
			"singleChatConversation": map[string]any{"cid": "xiaomi@goofish", "pairFirst": "self@goofish", "pairSecond": "0@goofish", "extension": map[string]any{"extUserId": "1400"}},
			"redPoint":               float64(3),
			"lastMessage":            map[string]any{"message": map[string]any{"extension": map[string]any{"senderUserId": "1400@goofish", "reminderTitle": "闲小蜜发来一条新消息"}, "content": map[string]any{"custom": map[string]any{"summary": "邀您填写售后问卷"}}}}}},
	}}
	if // err 用于本次流程后续判断的err
	_, err := service.RecordConversationPage(ctx, "account-1", "self", body); err != nil {
		t.Fatal(err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "owner")
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := store.Chats.ListSessions(ctx, owner.ID, "account-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].BuyerID != "1400" || rows[0].BuyerName != "闲小蜜" || rows[0].BuyerAvatar != xianxiaomiAvatar || rows[0].UnreadCount != 0 {
		t.Fatalf("unexpected sessions: %+v", rows)
	}
}

// TestConversationUnreadCountUsesRedPointButFiltersSystemMessages 验证官方红点不会把系统卡片计为用户未读。
func TestConversationUnreadCountUsesRedPointButFiltersSystemMessages(t *testing.T) {
	// store、cleanup 保存隔离聊天数据库及清理函数，供红点与本地未读数交叉验证。
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// service 是待测聊天服务，负责按系统消息规则折算会话红点。
	service := New(store)

	// systemCard 保存模拟交易通知的 Base64 卡片载荷，使末条消息被归类为系统消息。
	systemCard := base64.StdEncoding.EncodeToString([]byte(`{"contentType":26}`))
	// systemLast 保存带官方红点和系统未读状态的末条消息协议对象。
	systemLast := map[string]any{
		"extension":   map[string]any{"senderUserId": "peer@goofish"},
		"content":     map[string]any{"custom": map[string]any{"summary": "[交易通知]", "data": systemCard}},
		"unreadCount": float64(1), "readStatus": float64(1),
	}
	// got 保存扣除系统消息后的用户未读数，系统部分不得显示为用户红点。
	if got := service.conversationUnreadCount(context.Background(), "account-1", "system-last", "peer", map[string]any{"redPoint": float64(3)}, systemLast, "[交易通知]"); got != 2 {
		t.Fatalf("系统未读未从 redPoint 扣除: got=%d", got)
	}
	// got 保存闲小蜜会话的折算未读数；该官方系统账号永远不产生用户红点。
	if got := service.conversationUnreadCount(context.Background(), "account-1", "xiaomi", "1400", map[string]any{"redPoint": float64(3)}, systemLast, "[交易通知]"); got != 0 {
		t.Fatalf("闲小蜜全是系统消息时仍显示红点: got=%d", got)
	}

	// err 保存真实用户消息持久化错误；成功后本地消息级未读数应优先于官方红点。
	if _, _, err := service.RecordIncoming(context.Background(), Incoming{
		AccountID: "account-1", ChatID: "real", BuyerID: "peer", BuyerName: "真实用户", Text: "未读消息",
		MessageID: "real-unread", Raw: map[string]any{"messageId": "real-unread"},
	}); err != nil {
		t.Fatal(err)
	}
	// userLast 保存真实用户末条消息协议对象，不含系统卡片字段。
	userLast := map[string]any{
		"extension": map[string]any{"senderUserId": "peer@goofish"},
		"content":   map[string]any{"custom": map[string]any{"summary": "未读消息"}},
	}
	// got 保存本地记录的真实用户未读数，必须防止较慢官方刷新复活已读红点。
	if got := service.conversationUnreadCount(context.Background(), "account-1", "real", "peer", map[string]any{"redPoint": float64(3)}, userLast, "未读消息"); got != 1 {
		t.Fatalf("未使用消息级真实未读数: got=%d", got)
	}
}

// TestHistoryMessageIsSystem 验证历史卡片载荷和普通用户文本被正确区分，避免误算未读。
func TestHistoryMessageIsSystem(t *testing.T) {
	// encoded 保存模拟交易卡片的 Base64 载荷，触发内容类型的系统消息识别。
	encoded := base64.StdEncoding.EncodeToString([]byte(`{"contentType":26,"dxCard":{}}`))
	// last 保存待识别的历史末条消息协议对象。
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

// TestRecordConversationPageSkipsEmptyConversationShells 验证空会话壳不会被错误展示为联系人。
func TestRecordConversationPageSkipsEmptyConversationShells(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// service 用于本次流程后续判断的service
	service := New(store)
	// body 用于本次流程后续判断的请求体
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
	if // err 用于本次流程后续判断的err
	_, err := service.RecordConversationPage(context.Background(), "account-1", "self", body); err != nil {
		t.Fatal(err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(context.Background(), "owner")
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := store.Chats.ListSessions(context.Background(), owner.ID, "account-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ChatID != "system" {
		t.Fatalf("empty conversation shell was imported: %+v", rows)
	}
}

// TestDeleteEmptySessionsRemovesGhostsButKeepsRealConversation 封装TestDeleteEmptySessionsRemovesGhostsButKeepsRealConversation业务协调。
func TestDeleteEmptySessionsRemovesGhostsButKeepsRealConversation(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// ghost 用于本次流程后续判断的ghost
	ghost := db.ChatSession{CookieID: "account-1", ChatID: "ghost", BuyerID: "peer-ghost", LastMessage: "暂无消息", LastMessageAt: 100}
	if // err 用于本次流程后续判断的err
	err := store.Chats.UpsertSession(ctx, ghost); err != nil {
		t.Fatal(err)
	}
	// real 用于本次流程后续判断的real
	real := db.ChatSession{CookieID: "account-1", ChatID: "real", BuyerID: "peer-real", LastMessage: "暂无消息", LastMessageAt: 200}
	if // err 用于本次流程后续判断的err
	_, _, err := store.Chats.SaveMessage(ctx, real, db.ChatMessage{MessageKey: "real-1", Direction: "incoming", SenderID: "peer-real", SenderName: "真实用户", MessageType: "text", Content: "真实消息", Status: "received", SentAt: 200}, false); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Chats.DeleteEmptySessions(ctx, "account-1"); err != nil {
		t.Fatal(err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "owner")
	// rows、err 用于本次流程后续判断的rows、err
	rows, err := store.Chats.ListSessions(ctx, owner.ID, "account-1", 20)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].ChatID != "real" {
		t.Fatalf("unexpected sessions after pruning: %+v", rows)
	}
}

// TestValidNicknameRejectsSystemReminderTitles 封装Test有效NicknameRejects系统ReminderTitles业务协调。
func TestValidNicknameRejectsSystemReminderTitles(t *testing.T) {
	// value 表示当前遍历过程中的值
	for _, value := range []string{"", "203591535", "x***3", "快给ta一个评价吧～", "[卖家已发货]", "闲小蜜发来一条新消息"} {
		if ValidNickname(value) {
			t.Fatalf("system reminder accepted as nickname: %q", value)
		}
	}
	if !ValidNickname("纽约做手工的石斑") {
		t.Fatal("real nickname rejected")
	}
}

// TestIncomingMessagePersistsDeduplicatesAndPublishesByOwner 封装TestIncoming消息PersistsDeduplicatesAndPublishesBy所有者业务协调。
func TestIncomingMessagePersistsDeduplicatesAndPublishesByOwner(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := chatTestStore(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "owner")
	// other 用于本次流程后续判断的other
	other, _ := store.Users.GetByUsername(ctx, "other")
	// service 用于本次流程后续判断的service
	service := New(store)
	// ownerEvents、cancelOwner、err 用于本次流程后续判断的所有者Events、cancelOwner、err
	ownerEvents, cancelOwner, err := service.Subscribe(ctx, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelOwner()
	// otherEvents、cancelOther、err 用于本次流程后续判断的otherEvents、cancelOther、err
	otherEvents, cancelOther, err := service.Subscribe(ctx, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	defer cancelOther()

	// incoming 用于本次流程后续判断的incoming
	incoming := Incoming{AccountID: "account-1", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家甲",
		Text: "你好", ItemID: "item-1", Raw: map[string]any{"messageId": "platform-1", "sendTime": int64(1234567890000)}}
	// message、inserted、err 用于本次流程后续判断的message、inserted、err
	message, inserted, err := service.RecordIncoming(ctx, incoming)
	if err != nil || !inserted || message.MessageKey != "platform-1" {
		t.Fatalf("message=%+v inserted=%v err=%v", message, inserted, err)
	}
	if // inserted、err 用于本次流程后续判断的inserted、err
	_, inserted, err := service.RecordIncoming(ctx, incoming); err != nil || inserted {
		t.Fatalf("duplicate inserted=%v err=%v", inserted, err)
	}
	select {
	case // event 用于本次流程后续判断的event
	event := <-ownerEvents:
		if event.Type != "message.created" || event.Message.MessageKey != "platform-1" {
			t.Fatalf("event=%+v", event)
		}
	case <-time.After(time.Second):
		t.Fatal("owner did not receive event")
	}
	select {
	case // event 用于本次流程后续判断的event
	event := <-otherEvents:
		t.Fatalf("other owner leaked event: %+v", event)
	case <-time.After(30 * time.Millisecond):
	}
}

// TestExtractMessageContentSupportsMedia 验证图片、视频和 AMR 语音载荷均归一为可展示的媒体地址。
func TestExtractMessageContentSupportsMedia(t *testing.T) {
	// imageRaw 用于本次流程后续判断的图片原始
	imageRaw := map[string]any{"payload": `{"contentType":2,"image":{"pics":[{"url":"https://cdn/image.jpg"}]}}`}
	if // kind、content 用于本次流程后续判断的kind、content
	kind, content := extractMessageContent(imageRaw, "[图片]"); kind != "image" || content != "https://cdn/image.jpg" {
		t.Fatalf("image kind=%q content=%q", kind, content)
	}
	// videoRaw 用于本次流程后续判断的video原始
	videoRaw := map[string]any{"content": map[string]any{"video": map[string]any{"playUrl": "https://cdn/video.mp4"}}}
	if // kind、content 用于本次流程后续判断的kind、content
	kind, content := extractMessageContent(videoRaw, "[视频]"); kind != "video" || content != "https://cdn/video.mp4" {
		t.Fatalf("video kind=%q content=%q", kind, content)
	}
	// audioRaw 模拟实时 WebSocket 中 1.6.3.5 携带的语音 JSON 字符串。
	audioRaw := map[string]any{"1": map[string]any{"6": map[string]any{"3": map[string]any{"5": `{"contentType":3,"audio":{"duration":3,"url":"http://cdn.example/voice.amr"}}`}}}}
	if // kind、content 分别是语音分类结果和供前端解码的 AMR 地址。
	kind, content := extractMessageContent(audioRaw, "[语音]"); kind != "audio" || content != "http://cdn.example/voice.amr" {
		t.Fatalf("audio kind=%q content=%q", kind, content)
	}
	// duration 保存从同一嵌套语音载荷提取出的秒级长度，必须在播放前就可供 UI 使用。
	duration := extractMediaDuration(audioRaw, "audio")
	if duration != 3 {
		t.Fatalf("audio duration=%d, want 3", duration)
	}
	// nonAudioDuration 验证视频等媒体的 duration 不会误写入语音展示字段。
	nonAudioDuration := extractMediaDuration(videoRaw, "video")
	if nonAudioDuration != 0 {
		t.Fatalf("non-audio duration=%d, want 0", nonAudioDuration)
	}
	if // kind、content 用于本次流程后续判断的kind、content
	kind, content := extractMessageContent(nil, " 你好 "); kind != "text" || content != "你好" {
		t.Fatalf("text kind=%q content=%q", kind, content)
	}
}

// chatTestStore 封装聊天TestStore业务协调。
func chatTestStore(t *testing.T) (*db.Store, func()) {
	t.Helper()
	// database、dialect、err 用于本次流程后续判断的database、dialect、err
	database, dialect, err := db.Open(context.Background(), filepath.Join(t.TempDir(), "chat.db"))
	if err != nil {
		t.Fatal(err)
	}
	// store 用于本次流程后续判断的store
	store := db.NewStore(database, dialect)
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(context.Background(), "owner", "owner@example.com", "pw"); err != nil || !ok {
		t.Fatal(err)
	}
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(context.Background(), "other", "other@example.com", "pw"); err != nil || !ok {
		t.Fatal(err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(context.Background(), "owner")
	// other 用于本次流程后续判断的other
	other, _ := store.Users.GetByUsername(context.Background(), "other")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(context.Background(), "account-1", "unb=1", owner.ID); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(context.Background(), "account-2", "unb=2", other.ID); err != nil {
		t.Fatal(err)
	}
	return store, func() { _ = database.Close() }
}
