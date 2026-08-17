package server

import (
	"encoding/json"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
)

// TestChatMessageDTOKeepsReadReceipt 验证聊天传输 DTO 不会丢弃平台出站已读回执。
func TestChatMessageDTOKeepsReadReceipt(t *testing.T) {
	// message 保存带已读确认的应用层出站消息。
	message := chatapp.Message{ID: 7, AccountID: "account-1", ChatID: "chat-1", MessageKey: "message-1", Direction: "outgoing", Status: "sent", ReadStatus: 2, ReadAt: 88, SentAt: 99}
	// single 保存单消息接口使用的传输 DTO。
	single := newChatMessageDTOFromApplication(&message)
	if single.ReadStatus != 2 || single.ReadAt != 88 {
		t.Fatalf("单消息响应丢失已读回执: got=%+v", single)
	}
	// page 保存历史消息接口使用的传输 DTO 列表。
	page := newChatMessageDTOsFromApplication([]chatapp.Message{message})
	if len(page) != 1 || page[0].ReadStatus != 2 || page[0].ReadAt != 88 {
		t.Fatalf("消息分页响应丢失已读回执: got=%+v", page)
	}
}

// TestChatEventDTOUsesFrontendContract 验证 WebSocket 实时事件沿用聊天 HTTP 接口的 snake_case 字段契约。
func TestChatEventDTOUsesFrontendContract(t *testing.T) {
	// event 保存带消息与会话的应用层实时事件。
	event := chatapp.Event{
		Type:    "message.created",
		Message: &chatapp.Message{AccountID: "account-1", ChatID: "chat-1", MessageKey: "message-1", Direction: "incoming", Status: "received", SentAt: 9},
		Session: &chatapp.Session{AccountID: "account-1", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家"},
	}
	// encoded、marshalErr 分别保存 DTO 编码结果和编码错误。
	encoded, marshalErr := json.Marshal(newChatEventDTOFromApplication(event))
	if marshalErr != nil {
		t.Fatalf("编码聊天实时事件: %v", marshalErr)
	}
	// payload 保存浏览器实际接收的 JSON 对象。
	var payload map[string]any
	if unmarshalErr := json.Unmarshal(encoded, &payload); unmarshalErr != nil {
		t.Fatalf("解码聊天实时事件: %v", unmarshalErr)
	}
	// message 保存事件中的消息对象，必须保留前端读取的 snake_case 账号和会话字段。
	message, messageOK := payload["message"].(map[string]any)
	if !messageOK || message["account_id"] != "account-1" || message["chat_id"] != "chat-1" || message["AccountID"] != nil {
		t.Fatalf("消息 WebSocket 契约错误: %#v", payload["message"])
	}
	// session 保存事件中的会话对象，也必须使用同一命名约定。
	session, sessionOK := payload["session"].(map[string]any)
	if !sessionOK || session["account_id"] != "account-1" || session["chat_id"] != "chat-1" || session["AccountID"] != nil {
		t.Fatalf("会话 WebSocket 契约错误: %#v", payload["session"])
	}
}
