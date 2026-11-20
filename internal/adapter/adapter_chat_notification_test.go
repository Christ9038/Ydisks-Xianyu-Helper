package adapter

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"testing"

	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// chatNotificationCall 保存一条新消息通知的实参快照，便于断言适配器只透传非敏感展示字段。
type chatNotificationCall struct {
	// accountID、accountName 是通知归属账号及展示名称。
	accountID, accountName string
	// chatID、peerName、peerID 是会话标识与买家名称、平台用户标识。
	chatID, peerName, peerID string
	// itemID、messageType、preview、messageKey 是商品、消息摘要和平台消息键。
	itemID, messageType, preview, messageKey string
}

// recordingChatNotifier 同时实现账号告警和可选新消息提醒接口，用于验证适配器能力探测。
type recordingChatNotifier struct {
	// calls 保存收到的每条新消息通知实参。
	calls []chatNotificationCall
}

// NotifyAccountAlert 满足适配器必需的账号告警接口，本测试不关心其调用。
func (n *recordingChatNotifier) NotifyAccountAlert(string, string, string, string) {}

// NotifyChatMessage 记录一条新消息通知的全部实参；ctx 仅用于满足可选通知接口签名。
func (n *recordingChatNotifier) NotifyChatMessage(_ context.Context, accountID, accountName, chatID, peerName, peerID, itemID, messageType, preview, messageKey string) {
	n.calls = append(n.calls, chatNotificationCall{
		accountID: accountID, accountName: accountName, chatID: chatID,
		peerName: peerName, peerID: peerID, itemID: itemID,
		messageType: messageType, preview: preview, messageKey: messageKey,
	})
}

// TestAdapterChatMessageNotifiesOnlyFirstInsertedIncoming 验证只有首次落库的买家消息触发通知，回显、重复和系统消息均不触发。
func TestAdapterChatMessageNotifiesOnlyFirstInsertedIncoming(t *testing.T) {
	// store、cleanup 保存隔离数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试聊天事件处理共用的上下文。
	ctx := context.Background()
	// remarkErr 保存账号备注夹具写入结果，备注用于验证通知使用可读账号名称。
	if _, remarkErr := store.DB.ExecContext(ctx, `UPDATE cookies SET remark=? WHERE id=?`, "店铺备注", "cid"); remarkErr != nil {
		t.Fatalf("写入账号备注: %v", remarkErr)
	}
	// adapter 保存已装配聊天服务和可记录通知器的适配器。
	adapter := New(store, nil, nil)
	adapter.SetChatService(chat.New(store))
	// recorder 保存新消息通知实参，后续逐条断言调用次数和字段。
	recorder := &recordingChatNotifier{}
	adapter.SetNotifier(recorder)
	// selfErr 保存账号自身回显的处理结果，该消息不得产生通知。
	selfErr := adapter.HandleChatMessage(ctx, engine.ChatMessage{AccountID: "cid", CookieStr: "unb=me", ChatID: "chat-1", SenderUserID: "me", Text: "自己发的"})
	if selfErr != nil {
		t.Fatal(selfErr)
	}
	if len(recorder.calls) != 0 {
		t.Fatalf("自身回显不应通知: %+v", recorder.calls)
	}
	// incoming 是首次落库的真实买家消息，应触发一条通知。
	incoming := engine.ChatMessage{AccountID: "cid", CookieStr: "unb=me", ChatID: "chat-1", SenderUserID: "buyer-1", SenderName: "买家", MessageID: "msg-1", ItemID: "item-1", Text: "你好"}
	// incomingErr 保存首次入站消息落库与通知结果。
	if incomingErr := adapter.HandleChatMessage(ctx, incoming); incomingErr != nil {
		t.Fatal(incomingErr)
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("首次入站消息通知次数=%d", len(recorder.calls))
	}
	// call 保存首次通知实参，用于逐字段核对账号、买家、商品和消息摘要。
	call := recorder.calls[0]
	if call.accountID != "cid" || call.accountName != "店铺备注" || call.chatID != "chat-1" {
		t.Fatalf("账号或会话字段错误: %+v", call)
	}
	if call.peerName != "买家" || call.peerID != "buyer-1" || call.itemID != "item-1" {
		t.Fatalf("买家或商品字段错误: %+v", call)
	}
	if call.messageType != "text" || call.preview != "你好" || call.messageKey != "msg-1" {
		t.Fatalf("消息字段错误: %+v", call)
	}
	// duplicateErr 保存重复平台消息的处理结果，重复消息不应产生新通知。
	if duplicateErr := adapter.HandleChatMessage(ctx, incoming); duplicateErr != nil {
		t.Fatal(duplicateErr)
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("重复消息不应再次通知，调用次数=%d", len(recorder.calls))
	}
	// systemErr 保存平台系统消息的处理结果，系统消息不得作为买家新消息通知。
	systemErr := adapter.HandleChatMessage(ctx, engine.ChatMessage{AccountID: "cid", CookieStr: "unb=me", ChatID: "chat-1", SenderUserID: "buyer-2", MessageID: "sys-1", Text: "系统提示", Raw: map[string]any{"contentType": "14"}})
	if systemErr != nil {
		t.Fatal(systemErr)
	}
	if len(recorder.calls) != 1 {
		t.Fatalf("系统消息不应通知，调用次数=%d", len(recorder.calls))
	}
}

// TestAdapterChatMessageNotificationToleratesMissingNotifierAndDisplayFailure 验证缺失通知器和账号名称读取失败都不阻断消息落库。
func TestAdapterChatMessageNotificationToleratesMissingNotifierAndDisplayFailure(t *testing.T) {
	// store、cleanup 保存隔离数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试聊天事件处理共用的上下文。
	ctx := context.Background()
	// logger 丢弃预期内的降级告警，避免测试输出噪声。
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	// adapter 保存未注入通知器但已装配聊天服务的适配器。
	adapter := New(store, nil, logger)
	adapter.SetChatService(chat.New(store))
	// plainErr 保存无通知器时入站消息的持久化结果，通知能力缺失不得影响落库。
	plainErr := adapter.HandleChatMessage(ctx, engine.ChatMessage{AccountID: "cid", CookieStr: "unb=me", ChatID: "chat-missing", SenderUserID: "buyer", MessageID: "missing-notifier", Text: "你好"})
	if plainErr != nil {
		t.Fatal(plainErr)
	}
	// closedDB、_, openErr 保存已主动关闭的数据库连接，用于制造账号名称读取失败。
	closedDB, _, openErr := db.Open(ctx, filepath.Join(t.TempDir(), "closed.db"))
	if openErr != nil {
		t.Fatalf("打开名称查询故障数据库: %v", openErr)
	}
	_ = closedDB.Close()
	// failingAdapter 保存已装配通知器、但名称查询所依赖的数据库连接不可用的适配器。
	failingAdapter := New(store, nil, logger)
	failingAdapter.SetChatService(chat.New(store))
	// recorder 保存名称读取失败时仍应发送的通知实参。
	recorder := &recordingChatNotifier{}
	failingAdapter.SetNotifier(recorder)
	failingAdapter.store = &db.Store{Cookies: &db.Cookies{DB: closedDB, Dialect: db.DialectSQLite}}
	// lookupErr 保存名称查询失败场景的入站消息处理结果；聊天持久化仍应成功。
	lookupErr := failingAdapter.HandleChatMessage(ctx, engine.ChatMessage{AccountID: "cid", CookieStr: "unb=me", ChatID: "chat-lookup", SenderUserID: "buyer", SenderName: "买家", MessageID: "lookup-failure", Text: "你好"})
	if lookupErr != nil {
		t.Fatalf("名称读取失败不应阻断消息落库: %v", lookupErr)
	}
	if len(recorder.calls) != 1 || recorder.calls[0].accountName != "" {
		t.Fatalf("名称读取失败时应回退空名称并继续通知: %+v", recorder.calls)
	}
}
