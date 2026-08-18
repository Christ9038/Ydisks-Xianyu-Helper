package engine

import (
	"context"
	"testing"

	"xianyu-go/internal/automation"
)

// outgoingObserverHandler 用于本次流程后续判断的outgoingObserverHandler
type outgoingObserverHandler struct {
	messages []OutgoingChatMessage
}

// HandleChatMessage 处理聊天消息。
func (h *outgoingObserverHandler) HandleChatMessage(context.Context, ChatMessage) error { return nil }

// HandleSystemEvent 处理系统Event。
func (h *outgoingObserverHandler) HandleSystemEvent(context.Context, automation.Task) error {
	return nil
}

// OnPasswordLoginRefresh 封装On密码登录Refresh业务协调。
func (h *outgoingObserverHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }

// OnAccountAlert 封装On账号Alert业务协调。
func (h *outgoingObserverHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// HandleOutgoingChatMessage 处理Outgoing聊天消息。
func (h *outgoingObserverHandler) HandleOutgoingChatMessage(_ context.Context, message OutgoingChatMessage) error {
	h.messages = append(h.messages, message)
	return nil
}

// TestSendTextEmitsCorrelatedOutgoingObservation 封装TestSend文本EmitsCorrelatedOutgoingObservation业务协调。
func TestSendTextEmitsCorrelatedOutgoingObservation(t *testing.T) {
	// handler 用于本次流程后续判断的handler
	handler := &outgoingObserverHandler{}
	// account 用于本次流程后续判断的账号
	account := New(Config{CookieID: "account-1", CookieStr: "unb=me", Handler: handler})
	// conn 用于本次流程后续判断的conn
	conn := &fakeWSConn{}
	account.mu.Lock()
	account.conn = conn
	account.mu.Unlock()
	// ctx 用于本次流程后续判断的ctx
	ctx := WithOutgoingMessageKey(context.Background(), "local-1")
	if // err 用于本次流程后续判断的err
	err := account.SendText(ctx, "chat-1", "buyer-1", "您好"); err != nil {
		t.Fatal(err)
	}
	if len(handler.messages) != 1 {
		t.Fatalf("messages=%+v", handler.messages)
	}
	// got 用于本次流程后续判断的got
	got := handler.messages[0]
	if got.AccountID != "account-1" || got.ChatID != "chat-1" || got.BuyerID != "buyer-1" || got.Text != "您好" || got.MessageKey != "local-1" {
		t.Fatalf("observation=%+v", got)
	}
}
