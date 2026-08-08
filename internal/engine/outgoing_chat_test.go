package engine

import (
	"context"
	"testing"

	"xianyu-go/internal/automation"
)

type outgoingObserverHandler struct {
	messages []OutgoingChatMessage
}

func (h *outgoingObserverHandler) HandleChatMessage(context.Context, ChatMessage) error { return nil }
func (h *outgoingObserverHandler) HandleSystemEvent(context.Context, automation.Task) error {
	return nil
}
func (h *outgoingObserverHandler) OnPasswordLoginRefresh(context.Context, string) bool            { return false }
func (h *outgoingObserverHandler) OnAccountAlert(context.Context, string, string, string, string) {}
func (h *outgoingObserverHandler) HandleOutgoingChatMessage(_ context.Context, message OutgoingChatMessage) error {
	h.messages = append(h.messages, message)
	return nil
}

func TestSendTextEmitsCorrelatedOutgoingObservation(t *testing.T) {
	handler := &outgoingObserverHandler{}
	account := New(Config{CookieID: "account-1", CookieStr: "unb=me", Handler: handler})
	conn := &fakeWSConn{}
	account.mu.Lock()
	account.conn = conn
	account.mu.Unlock()
	ctx := WithOutgoingMessageKey(context.Background(), "local-1")
	if err := account.SendText(ctx, "chat-1", "buyer-1", "您好"); err != nil {
		t.Fatal(err)
	}
	if len(handler.messages) != 1 {
		t.Fatalf("messages=%+v", handler.messages)
	}
	got := handler.messages[0]
	if got.AccountID != "account-1" || got.ChatID != "chat-1" || got.BuyerID != "buyer-1" || got.Text != "您好" || got.MessageKey != "local-1" {
		t.Fatalf("observation=%+v", got)
	}
}
