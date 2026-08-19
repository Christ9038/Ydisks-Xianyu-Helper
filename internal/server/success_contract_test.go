package server

import (
	"encoding/json"
	"path/filepath"
	"testing"

	"github.com/getkin/kin-openapi/openapi3"

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
		Message: &chatapp.Message{AccountID: "account-1", ChatID: "chat-1", MessageKey: "message-1", Direction: "incoming", SenderID: "buyer-1", SenderName: "买家", MessageType: "text", Status: "received", SentAt: 9},
		Session: &chatapp.Session{AccountID: "account-1", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家"},
	}
	// encoded、marshalErr 分别保存 DTO 编码结果和编码错误。
	encoded, marshalErr := json.Marshal(newChatEventDTOFromApplication(event))
	if marshalErr != nil {
		t.Fatalf("编码聊天实时事件: %v", marshalErr)
	}
	// payload 保存浏览器实际接收的 JSON 对象。
	var payload map[string]any
	// unmarshalErr 表示将聊天事件 JSON 解码为契约断言对象时的错误。
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
	// specPath 是测试包相对仓库根目录的唯一 OpenAPI 契约位置。
	specPath := filepath.Join("..", "..", "api", "openapi.yaml")
	// document、loadErr 分别保存加载后的 OpenAPI 文档和读取失败原因。
	document, loadErr := openapi3.NewLoader().LoadFromFile(specPath)
	if loadErr != nil {
		t.Fatalf("加载 OpenAPI WebSocket 契约: %v", loadErr)
	}
	// eventSchemaRef 保存 WebSocket 推送帧的具名 schema 引用。
	eventSchemaRef := document.Components.Schemas["ChatWebSocketEvent"]
	if eventSchemaRef == nil || eventSchemaRef.Value == nil {
		t.Fatal("OpenAPI 缺少 ChatWebSocketEvent schema")
	}
	// validationErr 确保真实聊天 DTO 的字段名与类型由同一 OpenAPI schema 约束。
	if validationErr := eventSchemaRef.Value.VisitJSON(payload, openapi3.EnableJSONSchema2020()); validationErr != nil {
		t.Fatalf("聊天 WebSocket 事件不符合 OpenAPI: %v", validationErr)
	}
	// readyPayload 保存服务端在握手完成后立即发送的就绪帧。
	readyPayload := map[string]any{"type": "ready", "at": int64(9)}
	// readyValidationErr 确保握手后的第一条实时消息也被同一 OpenAPI schema 覆盖。
	if readyValidationErr := eventSchemaRef.Value.VisitJSON(readyPayload, openapi3.EnableJSONSchema2020()); readyValidationErr != nil {
		t.Fatalf("聊天 WebSocket 就绪事件不符合 OpenAPI: %v", readyValidationErr)
	}
}
