package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

// startAPIResponseServer 启动用于请求 API 方法的本地 WebSocket 响应端点。
func startAPIResponseServer(t *testing.T, responseBody map[string]any, responseCode int) (*httptest.Server, chan map[string]any) {
	t.Helper()
	// requests 收集客户端发送的业务帧，供测试校验参数归一化结果。
	requests := make(chan map[string]any, 8)
	// server 保存模拟闲鱼 WebSocket API 的本地服务。
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		// connection、acceptErr 保存 WebSocket 升级结果。
		connection, acceptErr := websocket.Accept(writer, request, nil)
		if acceptErr != nil {
			return
		}
		defer connection.Close(websocket.StatusNormalClosure, "")
		// readContext、cancelRead 保存服务端读取生命周期。
		readContext, cancelRead := context.WithCancel(request.Context())
		defer cancelRead()
		for {
			// messageType、raw、readErr 保存客户端业务帧读取结果。
			messageType, raw, readErr := connection.Read(readContext)
			if readErr != nil {
				return
			}
			if messageType != websocket.MessageText {
				continue
			}
			// message 保存客户端请求帧。
			var message map[string]any
			if json.Unmarshal(raw, &message) != nil {
				continue
			}
			select {
			case requests <- message:
			default:
			}
			// headers 保存请求中的 mid，使响应能够匹配 pending 请求。
			headers, _ := message["headers"].(map[string]any)
			// response 保存模拟平台响应。
			response := map[string]any{"code": responseCode, "headers": map[string]any{"mid": headers["mid"]}, "body": responseBody}
			// encodedResponse 保存响应帧 JSON。
			encodedResponse, marshalErr := json.Marshal(response)
			if marshalErr != nil {
				return
			}
			if connection.Write(readContext, websocket.MessageText, encodedResponse) != nil {
				return
			}
		}
	}))
	t.Cleanup(server.Close)
	return server, requests
}

// newAPIResponseConn 连接本地 WebSocket API 模拟端点并返回业务连接。
func newAPIResponseConn(t *testing.T, responseBody map[string]any, responseCode int) (*Conn, chan map[string]any) {
	t.Helper()
	// server、requests 保存本地端点及收到的请求帧。
	server, requests := startAPIResponseServer(t, responseBody, responseCode)
	// dialContext、cancelDial 保存本地连接建立上下文。
	dialContext, cancelDial := context.WithTimeout(context.Background(), time.Second)
	defer cancelDial()
	// dialed、_, dialErr 保存 WebSocket 拨号结果。
	dialed, _, dialErr := websocket.Dial(dialContext, wsURL(server), nil)
	if dialErr != nil {
		t.Fatalf("dial local API server: %v", dialErr)
	}
	dialed.SetReadLimit(8 << 20)
	t.Cleanup(func() { _ = dialed.CloseNow() })
	return newConn(dialed, Config{}, nilLogger()), requests
}

// TestListUserMessagesAndConversationsCoversResponseBranches 覆盖官方消息历史和会话列表 API 的响应分支。
func TestListUserMessagesAndConversationsCoversResponseBranches(t *testing.T) {
	// connection、requests 保存成功响应场景的本地连接和请求帧。
	connection, requests := newAPIResponseConn(t, map[string]any{"items": []any{"m1"}}, http.StatusOK)
	// messages、messagesErr 保存消息历史查询结果。
	messages, messagesErr := connection.ListUserMessages(context.Background(), "cid", 0, 0)
	if messagesErr != nil || messages["items"] == nil {
		t.Fatalf("messages=%v err=%v", messages, messagesErr)
	}
	// conversations、conversationsErr 保存会话列表查询结果。
	conversations, conversationsErr := connection.ListConversations(context.Background(), 0, 0)
	if conversationsErr != nil || conversations["items"] == nil {
		t.Fatalf("conversations=%v err=%v", conversations, conversationsErr)
	}
	// requestReceived 表示服务端已经收到首个请求帧。
	select {
	case <-requests:
	case <-time.After(time.Second):
		t.Fatal("request frame not received")
	}
	// emptyConnection 保存空会话 ID 校验场景的连接。
	emptyConnection, _ := newAPIResponseConn(t, nil, http.StatusOK)
	// emptyErr 保存空会话 ID 校验错误。
	if _, emptyErr := emptyConnection.ListUserMessages(context.Background(), " ", 1, 1); emptyErr == nil {
		t.Fatal("empty conversation ID should fail")
	}
	// errorConnection 保存平台失败响应场景的连接。
	errorConnection, _ := newAPIResponseConn(t, map[string]any{"reason": "denied"}, http.StatusBadRequest)
	// responseErr 保存平台非成功状态错误。
	if _, responseErr := errorConnection.ListConversations(context.Background(), 1, 1); responseErr == nil {
		t.Fatal("non-200 conversation response should fail")
	}
}

// TestListAPICoversMissingBodyReasonAndCanceledRequest 覆盖消息 API 缺少 body、业务 reason 和取消请求分支。
func TestListAPICoversMissingBodyReasonAndCanceledRequest(t *testing.T) {
	// missingBodyConnection 保存缺少响应 body 的连接。
	missingBodyConnection, _ := newAPIResponseConn(t, nil, http.StatusOK)
	// missingBodyErr 保存缺少响应 body 的解析错误。
	if _, missingBodyErr := missingBodyConnection.ListUserMessages(context.Background(), "cid@goofish", 1, 101); missingBodyErr == nil {
		t.Fatal("missing body should fail")
	}
	// reasonConnection 保存带业务失败 reason 的连接。
	reasonConnection, _ := newAPIResponseConn(t, map[string]any{"reason": "denied"}, http.StatusOK)
	// reasonErr 保存平台业务 reason 错误。
	if _, reasonErr := reasonConnection.ListUserMessages(context.Background(), "cid", 1, 1); reasonErr == nil {
		t.Fatal("response reason should fail")
	}
	// canceledConnection 保存请求取消场景的连接。
	canceledConnection, _ := newAPIResponseConn(t, map[string]any{"items": []any{}}, http.StatusOK)
	// canceledContext、cancelContext 保存主动取消的请求上下文。
	canceledContext, cancelContext := context.WithCancel(context.Background())
	cancelContext()
	// canceledErr 保存主动取消请求的错误。
	if _, canceledErr := canceledConnection.ListConversations(canceledContext, 1, 1); canceledErr == nil {
		t.Fatal("canceled request should fail")
	}
	// readConnection 保存已读上报场景的连接。
	readConnection, _ := newAPIResponseConn(t, map[string]any{}, http.StatusBadRequest)
	// readErr 保存已读上报请求错误。
	if readErr := readConnection.MarkChatRead(context.Background(), "cid", []map[string]any{{"messageId": "m1"}, {"messageId": " "}, {"messageId": nil}}); readErr != nil {
		t.Fatalf("mark chat read=%v", readErr)
	}
}
