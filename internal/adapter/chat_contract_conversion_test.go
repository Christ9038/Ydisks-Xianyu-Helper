package adapter

import (
	"reflect"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/db"
)

// TestChatSessionFromApplicationKeepsNonSensitiveFields 验证会话转换只复制非敏感展示字段。
func TestChatSessionFromApplicationKeepsNonSensitiveFields(t *testing.T) {
	// session 保存应用层聊天会话摘要。
	session := chatapp.Session{AccountID: "account-1", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家", BuyerAvatar: "avatar", ItemID: "item-1", ItemTitle: "商品", LastMessage: "你好", LastMessageAt: 42, UnreadCount: 3}
	// converted 保存转换后的 legacy 聊天会话模型。
	converted := ChatSessionFromApplication(session)
	// expected 保存应由领域仓储接收的非敏感会话字段。
	expected := db.ChatSession{CookieID: "account-1", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家", BuyerAvatar: "avatar", ItemID: "item-1", ItemTitle: "商品", LastMessage: "你好", LastMessageAt: 42, UnreadCount: 3}
	if !reflect.DeepEqual(converted, expected) {
		t.Fatalf("聊天会话转换异常: got=%+v want=%+v", converted, expected)
	}
}

// TestChatMessagesFromDBKeepsMessageContract 验证数据库消息转换为应用消息时完整保留 API 所需字段。
func TestChatMessagesFromDBKeepsMessageContract(t *testing.T) {
	// messages 保存 legacy 聊天仓储返回的消息模型。
	messages := []db.ChatMessage{{ID: 7, CookieID: "account-1", ChatID: "chat-1", MessageKey: "key-1", Direction: "incoming", SenderID: "buyer-1", SenderName: "买家", MessageType: "text", Content: "你好", Status: "received", SentAt: 99}}
	// converted 保存不再暴露数据库模型的应用层消息。
	converted := ChatMessagesFromDB(messages)
	if len(converted) != 1 {
		t.Fatalf("消息数量异常: got=%d", len(converted))
	}
	// expected 保存应用层消息字段。
	expected := chatapp.Message{ID: 7, AccountID: "account-1", ChatID: "chat-1", MessageKey: "key-1", Direction: "incoming", SenderID: "buyer-1", SenderName: "买家", MessageType: "text", Content: "你好", Status: "received", SentAt: 99}
	if !reflect.DeepEqual(converted[0], expected) {
		t.Fatalf("聊天消息转换异常: got=%+v want=%+v", converted[0], expected)
	}
}

// TestChatMessagesFromDBHandlesEmptyInput 验证空消息页转换后保持可安全遍历的空切片。
func TestChatMessagesFromDBHandlesEmptyInput(t *testing.T) {
	// converted 保存空 legacy 消息列表转换结果。
	converted := ChatMessagesFromDB(nil)
	if converted == nil || len(converted) != 0 {
		t.Fatalf("空消息转换应返回非 nil 空切片: %#v", converted)
	}
}
