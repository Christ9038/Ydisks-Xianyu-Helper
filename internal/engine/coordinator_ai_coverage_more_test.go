package engine

import (
	"context"
	"testing"
)

// historyWSConn 是出站协调器历史查询使用的可编程 WebSocket 替身。
type historyWSConn struct {
	// fakeWSConn 提供历史测试不涉及的基础发送和生命周期方法。
	fakeWSConn
	// history、conversations 保存历史查询成功结果。
	history       map[string]any
	conversations map[string]any
	// historyErr、conversationsErr 保存两类历史查询的预置错误。
	historyErr       error
	conversationsErr error
}

// readReceiptHandler 是消息已读回执处理使用的可编程 Handler 替身。
type readReceiptHandler struct {
	recordingHandler
	// receipt 保存最近收到的已读回执。
	receipt MessageReadEvent
	// receiptErr 保存已读回执处理的预置错误。
	receiptErr error
}

// HandleMessageRead 保存已读回执并返回预置错误。
func (handler *readReceiptHandler) HandleMessageRead(_ context.Context, receipt MessageReadEvent) error {
	handler.receipt = receipt
	return handler.receiptErr
}

// ListUserMessages 返回预置的聊天历史结果。
func (conn *historyWSConn) ListUserMessages(context.Context, string, int64, int) (map[string]any, error) {
	return conn.history, conn.historyErr
}

// ListConversations 返回预置的历史会话结果。
func (conn *historyWSConn) ListConversations(context.Context, int64, int) (map[string]any, error) {
	return conn.conversations, conn.conversationsErr
}

// TestOutgoingCoordinatorCoversHistoryQueries 覆盖出站协调器历史查询的缺少能力、成功和失败分支。
func TestOutgoingCoordinatorCoversHistoryQueries(t *testing.T) {
	// nilCoordinator 表示未装配账号的出站协调器。
	nilCoordinator := &outgoingMessageCoordinator{}
	// err 表示未装配账号时的历史查询错误。
	if _, _, err := nilCoordinator.fetchChatHistory(context.Background(), "chat", 0, 10); err == nil {
		t.Fatal("未装配账号时历史查询应失败")
	}
	// unsupportedAccount 是连接存在但不支持历史查询接口的账号运行时。
	unsupportedAccount := New(Config{CookieID: "unsupported-history", CookieStr: "unb=buyer"})
	unsupportedAccount.runtimeMu.Lock()
	unsupportedAccount.conn = &fakeWSConn{}
	unsupportedAccount.runtimeMu.Unlock()
	// unsupportedCoordinator 保存不支持历史查询接口的协调器。
	unsupportedCoordinator := &outgoingMessageCoordinator{account: unsupportedAccount}
	// err 表示连接缺少聊天历史查询能力时的错误。
	if _, _, err := unsupportedCoordinator.fetchChatHistory(context.Background(), "chat", 0, 10); err == nil {
		t.Fatal("不支持历史查询的连接应返回错误")
	}
	// err 表示连接缺少历史会话查询能力时的错误。
	if _, _, err := unsupportedCoordinator.fetchChatConversations(context.Background(), 0, 10); err == nil {
		t.Fatal("不支持会话查询的连接应返回错误")
	}
	// historyConn 是同时支持聊天历史和会话查询的连接替身。
	historyConn := &historyWSConn{history: map[string]any{"messages": 1}, conversations: map[string]any{"sessions": 2}}
	// account 是绑定历史查询连接且拥有 unb 身份的账号运行时。
	account := New(Config{CookieID: "supported-history", CookieStr: "unb=buyer"})
	account.runtimeMu.Lock()
	account.conn = historyConn
	account.runtimeMu.Unlock()
	// coordinator 保存支持历史查询接口的协调器。
	coordinator := &outgoingMessageCoordinator{account: account}
	// history、historyUserID、historyErr 保存聊天历史查询结果。
	history, historyUserID, historyErr := coordinator.fetchChatHistory(context.Background(), "chat", 1, 10)
	if historyErr != nil || history["messages"] != 1 || historyUserID != "buyer" {
		t.Fatalf("history=%v user=%q err=%v", history, historyUserID, historyErr)
	}
	// conversations、conversationUserID、conversationErr 保存会话查询结果。
	conversations, conversationUserID, conversationErr := coordinator.fetchChatConversations(context.Background(), 1, 10)
	if conversationErr != nil || conversations["sessions"] != 2 || conversationUserID != "buyer" {
		t.Fatalf("conversations=%v user=%q err=%v", conversations, conversationUserID, conversationErr)
	}
	// historyConn.historyErr、historyConn.conversationsErr 模拟两个历史查询的远端失败。
	historyConn.historyErr = context.DeadlineExceeded
	historyConn.conversationsErr = context.Canceled
	// err 表示聊天历史远端查询返回的错误。
	if _, _, err := coordinator.fetchChatHistory(context.Background(), "chat", 0, 1); err == nil {
		t.Fatal("聊天历史远端错误应向上传播")
	}
	// err 表示历史会话远端查询返回的错误。
	if _, _, err := coordinator.fetchChatConversations(context.Background(), 0, 1); err == nil {
		t.Fatal("会话历史远端错误应向上传播")
	}
}

// TestGlobalAIConfigCoversDatabaseFailure 覆盖 AI 全局配置读取首个敏感设置失败分支。
func TestGlobalAIConfigCoversDatabaseFailure(t *testing.T) {
	// store、cleanup 保存随后关闭数据库连接的 AI 测试存储。
	store, cleanup := newAIStore(t)
	defer cleanup()
	// closeErr 保存关闭 AI 测试数据库连接的结果。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// replier 是绑定关闭数据库的 AI 回复适配器。
	replier := NewAIReplier("cid", store, nil)
	// config、configErr 保存配置读取失败结果。
	config, configErr := replier.globalAIConfig(context.Background())
	if config != nil || configErr == nil {
		t.Fatalf("closed AI config=%+v err=%v", config, configErr)
	}
}

// TestMessageDispatcherCoversReadReceiptHandlerBranches 覆盖消息分发器已读回执的可选接口和错误分支。
func TestMessageDispatcherCoversReadReceiptHandlerBranches(t *testing.T) {
	// plainHandler 是只支持基础 Handler 的兼容处理器。
	plainHandler := &recordingHandler{}
	// plainDispatcher 保存不支持已读回执扩展接口的消息分发器。
	plainDispatcher := newMessageDispatcher(messageDispatcherConfig{CookieID: "cid", CurrentHandler: func() Handler { return plainHandler }})
	plainDispatcher.handleMessageContext(context.Background(), map[string]any{"1": "message.PNM", "2": 2, "3": 0, "4": "chat@goofish", "5": 1, "6": 1})
	// reader 是记录回执的扩展处理器。
	reader := &readReceiptHandler{}
	// readerDispatcher 保存支持已读回执扩展接口的消息分发器。
	readerDispatcher := newMessageDispatcher(messageDispatcherConfig{CookieID: "cid", CurrentHandler: func() Handler { return reader }})
	readerDispatcher.handleMessageContext(context.Background(), map[string]any{"1": "message.PNM", "2": 2, "3": 0, "4": "chat@goofish", "5": 1, "6": 1})
	if reader.receipt.AccountID != "cid" || reader.receipt.MessageID != "message.PNM" || reader.receipt.ChatID != "chat" {
		t.Fatalf("已读回执=%+v", reader.receipt)
	}
	// reader.receiptErr 模拟已读回执持久化失败，分发器仍应安全返回。
	reader.receiptErr = context.DeadlineExceeded
	readerDispatcher.handleMessageContext(context.Background(), map[string]any{"1": "message-2.PNM", "2": 2, "3": 0, "4": "chat@goofish", "5": 1, "6": 2})
}
