package engine

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"xianyu-go/internal/xianyu/ws"
)

// extendedEngineWSConn 为聊天历史、会话和已读回执能力提供可观察测试实现。
type extendedEngineWSConn struct {
	// fakeWSConn 提供基础 WebSocket 生命周期和发送能力。
	*fakeWSConn
	// readChatID 保存最后一次已读上报的聊天标识。
	readChatID string
	// readMessages 保存最后一次已读上报的消息列表。
	readMessages []map[string]any
	// history 保存历史消息测试返回正文。
	history map[string]any
	// conversations 保存会话测试返回正文。
	conversations map[string]any
	// readErr、historyErr、conversationErr 保存可注入的能力错误。
	readErr         error
	historyErr      error
	conversationErr error
}

// MarkChatRead 记录已读回执参数并返回预设结果。
func (c *extendedEngineWSConn) MarkChatRead(_ context.Context, chatID string, messages []map[string]any) error {
	c.readChatID = chatID
	c.readMessages = messages
	return c.readErr
}

// ListUserMessages 返回聊天历史测试正文。
func (c *extendedEngineWSConn) ListUserMessages(context.Context, string, int64, int) (map[string]any, error) {
	return c.history, c.historyErr
}

// ListConversations 返回历史会话测试正文。
func (c *extendedEngineWSConn) ListConversations(context.Context, int64, int) (map[string]any, error) {
	return c.conversations, c.conversationErr
}

// TestAccountForwardsOptionalChatCapabilities 验证账号 facade 转发已读、历史和会话能力。
func TestAccountForwardsOptionalChatCapabilities(t *testing.T) {
	// conn 是同时支持已读、历史和会话查询的 WebSocket 测试替身。
	conn := &extendedEngineWSConn{fakeWSConn: &fakeWSConn{}, history: map[string]any{"history": true}, conversations: map[string]any{"conversations": true}}
	// account 是带有当前在线连接和 unb 身份的账号运行时。
	account := New(Config{CookieID: "capability-account", CookieStr: "unb=me"})
	account.runtimeMu.Lock()
	account.conn = conn
	account.runtimeState = RuntimeOnline
	account.runtimeMu.Unlock()
	// readErr 保存已读回执转发结果。
	readErr := account.MarkChatRead(context.Background(), "chat-1", []map[string]any{{"id": "message-1"}})
	if readErr != nil || conn.readChatID != "chat-1" || len(conn.readMessages) != 1 {
		t.Fatalf("已读回执转发错误: err=%v chat=%q messages=%v", readErr, conn.readChatID, conn.readMessages)
	}
	// history、historyUser、historyErr 保存聊天历史转发结果。
	history, historyUser, historyErr := account.FetchChatHistory(context.Background(), "chat-1", 3, 20)
	if historyErr != nil || historyUser != "me" || history["history"] != true {
		t.Fatalf("聊天历史转发错误: body=%v user=%q err=%v", history, historyUser, historyErr)
	}
	// conversations、conversationUser、conversationErr 保存会话列表转发结果。
	conversations, conversationUser, conversationErr := account.FetchChatConversations(context.Background(), 4, 20)
	if conversationErr != nil || conversationUser != "me" || conversations["conversations"] != true {
		t.Fatalf("会话列表转发错误: body=%v user=%q err=%v", conversations, conversationUser, conversationErr)
	}
	if !account.AutomationReady() {
		t.Fatal("在线连接应允许自动化发送")
	}
}

// TestAccountRejectsUnsupportedChatCapabilities 验证连接不支持可选能力时返回明确错误。
func TestAccountRejectsUnsupportedChatCapabilities(t *testing.T) {
	// account 是只有基础 WebSocket 能力的账号运行时。
	account := New(Config{CookieID: "unsupported-account", CookieStr: "unb=me"})
	account.runtimeMu.Lock()
	account.conn = &fakeWSConn{}
	account.runtimeMu.Unlock()
	// readErr 保存不支持已读上报时的错误。
	readErr := account.MarkChatRead(context.Background(), "chat", nil)
	if readErr == nil {
		t.Fatal("不支持已读上报时应返回错误")
	}
	// historyErr 保存不支持历史查询时的错误。
	_, _, historyErr := account.FetchChatHistory(context.Background(), "chat", 0, 10)
	if historyErr == nil {
		t.Fatal("不支持聊天历史时应返回错误")
	}
	// conversationErr 保存不支持会话查询时的错误。
	_, _, conversationErr := account.FetchChatConversations(context.Background(), 0, 10)
	if conversationErr == nil {
		t.Fatal("不支持历史会话时应返回错误")
	}
	if account.AutomationReady() {
		t.Fatal("未进入 online 的连接不应允许自动化发送")
	}
}

// TestAccountMapsOptionalCapabilityErrors 验证可选连接能力的底层错误保持透传。
func TestAccountMapsOptionalCapabilityErrors(t *testing.T) {
	// capabilityErr 是可选连接能力模拟的底层错误。
	capabilityErr := errors.New("capability failed")
	// conn 是返回可选能力错误的 WebSocket 测试替身。
	conn := &extendedEngineWSConn{fakeWSConn: &fakeWSConn{}, readErr: capabilityErr, historyErr: capabilityErr, conversationErr: capabilityErr}
	// account 是带有错误连接的账号运行时。
	account := New(Config{CookieID: "error-account", CookieStr: "unb=me"})
	account.runtimeMu.Lock()
	account.conn = conn
	account.runtimeMu.Unlock()
	// readErr 保存已读上报错误。
	readErr := account.MarkChatRead(context.Background(), "chat", nil)
	if !errors.Is(readErr, capabilityErr) {
		t.Fatalf("已读错误未透传: %v", readErr)
	}
	// _, _, historyErr 保存历史查询错误。
	_, _, historyErr := account.FetchChatHistory(context.Background(), "chat", 0, 10)
	if !errors.Is(historyErr, capabilityErr) {
		t.Fatalf("历史错误未透传: %v", historyErr)
	}
	// _, _, conversationErr 保存会话查询错误。
	_, _, conversationErr := account.FetchChatConversations(context.Background(), 0, 10)
	if !errors.Is(conversationErr, capabilityErr) {
		t.Fatalf("会话错误未透传: %v", conversationErr)
	}
}

// TestDefaultDialerHonorsCanceledContext 验证默认拨号器在取消上下文后不会建立外部连接。
func TestDefaultDialerHonorsCanceledContext(t *testing.T) {
	// ctx 是已经取消的拨号上下文。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// err 保存默认拨号器对取消上下文的响应。
	_, err := (defaultDialer{}).Dial(ctx, ws.Config{}, slog.Default())
	if err == nil {
		t.Fatal("取消上下文的默认拨号应失败")
	}
}
