package engine

import (
	"context"
	"testing"

	"xianyu-go/internal/automation"
)

// outgoingObserverHandler 保存outgoingObserverHandler，供当前处理流程使用
type outgoingObserverHandler struct {
	messages []OutgoingChatMessage
}

// HandleChatMessage 处理聊天消息。
func (h *outgoingObserverHandler) HandleChatMessage(context.Context, ChatMessage) error { return nil }

// HandleSystemEvent 处理系统Event。
func (h *outgoingObserverHandler) HandleSystemEvent(context.Context, automation.Task) error {
	return nil
}

// OnPasswordLoginRefresh 负责On密码登录Refresh相关处理。
func (h *outgoingObserverHandler) OnPasswordLoginRefresh(context.Context, string) bool { return false }

// OnAccountAlert 负责On账号Alert相关处理。
func (h *outgoingObserverHandler) OnAccountAlert(context.Context, string, string, string, string) {}

// HandleOutgoingChatMessage 处理Outgoing聊天消息。
func (h *outgoingObserverHandler) HandleOutgoingChatMessage(_ context.Context, message OutgoingChatMessage) error {
	h.messages = append(h.messages, message)
	return nil
}

// TestSendTextEmitsCorrelatedOutgoingObservation 负责TestSend文本EmitsCorrelatedOutgoingObservation相关处理。
func TestSendTextEmitsCorrelatedOutgoingObservation(t *testing.T) {
	// handler 保存handler，供当前处理流程使用
	handler := &outgoingObserverHandler{}
	// account 保存账号，供当前处理流程使用
	account := New(Config{CookieID: "account-1", CookieStr: "unb=me", Handler: handler})
	// conn 保存conn，供当前处理流程使用
	conn := &fakeWSConn{}
	account.mu.Lock()
	account.conn = conn
	account.mu.Unlock()
	// ctx 保存ctx，供当前处理流程使用
	ctx := WithOutgoingMessageKey(context.Background(), "local-1")
	if // err 保存err，供当前处理流程使用
	err := account.SendText(ctx, "chat-1", "buyer-1", "您好"); err != nil {
		t.Fatal(err)
	}
	if len(handler.messages) != 1 {
		t.Fatalf("messages=%+v", handler.messages)
	}
	// got 保存got，供当前处理流程使用
	got := handler.messages[0]
	if got.AccountID != "account-1" || got.ChatID != "chat-1" || got.BuyerID != "buyer-1" || got.Text != "您好" || got.MessageKey != "local-1" {
		t.Fatalf("observation=%+v", got)
	}
}
