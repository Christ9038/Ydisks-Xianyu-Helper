package adapter

import (
	"context"
	"testing"

	"xianyu-go/internal/chat"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/cookierefresh"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// legacyEventNotifier 仅实现旧版告警接口，用于验证 Adapter 的兼容回退路径。
type legacyEventNotifier struct {
	// calls 保存收到的旧版告警参数数量。
	calls int
}

// NotifyAccountAlert 记录旧版告警接口调用。
func (n *legacyEventNotifier) NotifyAccountAlert(string, string, string, string) { n.calls++ }

// credentialWakeRecorder 记录适配器转发的凭证阻塞任务唤醒请求。
type credentialWakeRecorder struct {
	// accountIDs 保存收到唤醒请求的账号标识顺序。
	accountIDs []string
}

// TestAdapterEventHooksHandleNilChatAndLegacyNotifier 验证未装配聊天服务和旧版通知器时的兼容分支。
func TestAdapterEventHooksHandleNilChatAndLegacyNotifier(t *testing.T) {
	// store、cleanup 保存隔离的适配器测试数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// adapter 保存未装配聊天服务的适配器。
	adapter := New(store, nil, nil)
	// ctx 是本测试事件调用共用的非取消上下文。
	ctx := context.Background()
	// chatErr 保存未装配聊天服务时入站事件的兼容结果。
	chatErr := adapter.HandleChatMessage(ctx, engine.ChatMessage{})
	if chatErr != nil {
		t.Fatalf("未装配聊天服务的入站事件错误=%v", chatErr)
	}
	// outgoingErr 保存未装配聊天服务时出站事件的兼容结果。
	outgoingErr := adapter.HandleOutgoingChatMessage(ctx, engine.OutgoingChatMessage{})
	if outgoingErr != nil {
		t.Fatalf("未装配聊天服务的出站事件错误=%v", outgoingErr)
	}
	// readErr 保存未装配聊天服务时已读事件的兼容结果。
	readErr := adapter.HandleMessageRead(ctx, engine.MessageReadEvent{})
	if readErr != nil {
		t.Fatalf("未装配聊天服务的已读事件错误=%v", readErr)
	}
	// activeAdapter 保存已装配聊天服务、用于验证持久化错误传播的适配器。
	activeAdapter := New(store, nil, nil)
	activeAdapter.SetChatService(chat.New(store))
	// canceledContext、cancel 保存触发聊天持久化取消分支的上下文。
	canceledContext, cancel := context.WithCancel(ctx)
	cancel()
	// canceledChatErr 保存入站消息持久化的取消错误。
	canceledChatErr := activeAdapter.HandleChatMessage(canceledContext, engine.ChatMessage{AccountID: "cid", ChatID: "chat", SenderUserID: "buyer", MessageID: "cancel-incoming"})
	// canceledOutgoingErr 保存出站消息观察写入的取消错误。
	canceledOutgoingErr := activeAdapter.HandleOutgoingChatMessage(canceledContext, engine.OutgoingChatMessage{AccountID: "cid", ChatID: "chat", BuyerID: "buyer", MessageKey: "cancel-outgoing"})
	// canceledReadErr 保存已读状态写入的取消错误。
	canceledReadErr := activeAdapter.HandleMessageRead(canceledContext, engine.MessageReadEvent{AccountID: "cid", ChatID: "chat", MessageID: "cancel-read", ReadAt: 1})
	if canceledChatErr == nil || canceledOutgoingErr == nil || canceledReadErr == nil {
		t.Fatalf("取消聊天持久化错误未传播 incoming=%v outgoing=%v read=%v", canceledChatErr, canceledOutgoingErr, canceledReadErr)
	}
	// legacyNotifier 保存只实现旧告警接口的通知器替身。
	legacyNotifier := &legacyEventNotifier{}
	adapter.SetNotifier(legacyNotifier)
	adapter.OnAccountEvent(ctx, "cid", engine.EventSystemError, engine.AlertLevelWarn, "标题", "正文")
	if legacyNotifier.calls != 1 {
		t.Fatalf("旧版告警通知调用次数=%d", legacyNotifier.calls)
	}
}

// TestClassifyAccountAlertEventCoversEventFamilies 验证账号告警标题和正文的全部分类族。
func TestClassifyAccountAlertEventCoversEventFamilies(t *testing.T) {
	// cases 保存告警文本到事件类型的确定性映射样例。
	cases := []struct {
		// title、body 保存参与分类的告警标题和正文。
		title, body string
		// want 保存期望的账号事件类型。
		want string
	}{
		{title: "账号被禁用", want: engine.EventAccountDisabled},
		{title: "offline", want: engine.EventAccountOffline},
		{title: "续期失败", want: engine.EventTokenRenewal},
		{title: "平台通知", body: "系统异常", want: engine.EventSystemError},
		{title: "risk challenge", want: engine.EventSecurityVerification},
	}
	// item 表示当前待验证的告警分类样例。
	for _, item := range cases {
		// got 保存当前告警样例的分类结果。
		got := classifyAccountAlertEvent(item.title, item.body)
		if got != item.want {
			t.Fatalf("告警分类 title=%q body=%q got=%q want=%q", item.title, item.body, got, item.want)
		}
	}
}

// WakeCredentialBlocked 记录凭证恢复后的自动化唤醒请求。
func (r *credentialWakeRecorder) WakeCredentialBlocked(_ context.Context, accountID string) error {
	r.accountIDs = append(r.accountIDs, accountID)
	return nil
}

// TestAdapterChatEventHooksPersistNonSelfMessages 验证聊天事件适配器落库真实入站消息并过滤账号自身回显。
func TestAdapterChatEventHooksPersistNonSelfMessages(t *testing.T) {
	// store、cleanup 保存隔离的 SQLite 存储和关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试聊天事件处理共用的上下文。
	ctx := context.Background()
	// adapter 保存注入聊天服务后的事件适配器。
	adapter := New(store, nil, nil)
	adapter.SetChatService(chat.New(store))
	// selfErr 保存账号自身 WebSocket 回显的处理结果。
	selfErr := adapter.HandleChatMessage(ctx, engine.ChatMessage{AccountID: "cid", CookieStr: "unb=me", ChatID: "chat-events", SenderUserID: "me", Text: "自己发的"})
	if selfErr != nil {
		t.Fatal(selfErr)
	}
	// incomingErr 保存真实买家消息的落库结果。
	incomingErr := adapter.HandleChatMessage(ctx, engine.ChatMessage{AccountID: "cid", CookieStr: "unb=me", ChatID: "chat-events", SenderUserID: "buyer", SenderName: "买家", MessageID: "incoming-events", Text: "你好", Raw: map[string]any{"kind": "text"}})
	if incomingErr != nil {
		t.Fatal(incomingErr)
	}
	// outgoingErr 保存出站消息观察回调的落库结果。
	outgoingErr := adapter.HandleOutgoingChatMessage(ctx, engine.OutgoingChatMessage{AccountID: "cid", ChatID: "chat-events", BuyerID: "buyer", MessageKey: "outgoing-events", Text: "稍等"})
	if outgoingErr != nil {
		t.Fatal(outgoingErr)
	}
	// owner 保存测试账号所属用户，用于读取适配器写入的消息。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// messages、listErr 保存入站和出站观察消息的持久化结果。
	messages, listErr := store.Chats.ListMessages(ctx, owner.ID, "cid", "chat-events", 0, 20)
	if listErr != nil || len(messages) != 2 {
		t.Fatalf("聊天事件落库异常 messages=%+v err=%v", messages, listErr)
	}
}

// TestAdapterSettersAndCookieSnapshotHelpers 验证兼容 setter 和凭证快照纯判断均可安全执行。
func TestAdapterSettersAndCookieSnapshotHelpers(t *testing.T) {
	// store、cleanup 保存测试适配器依赖及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// adapter 保存可被兼容入口更新的测试适配器。
	adapter := New(store, nil, nil)
	adapter.SetAutomation(nil)
	adapter.SetNotifier(nil)
	adapter.SetCredentialWakeService(nil)
	adapter.SetBrowser(nil)
	adapter.SetRenewService(xrenew.Service{})
	adapter.SetTokenCaptchaRequester(nil)
	adapter.SetOrderDetailClient(nil)
	adapter.SetChatService(nil)
	if adapter.automation != nil || adapter.notifier != nil || adapter.credentialWake != nil || adapter.browser != nil || adapter.chat != nil {
		t.Fatal("兼容 setter 未清理可选依赖")
	}
	// completeMetadata 和 incompleteMetadata 保存快照完整性判断的两种边界输入。
	completeMetadata := cookierefresh.MetadataWithSnapshot(`{"other":true}`, []cookierefresh.BrowserCookie{{Name: "unb", Value: "me", Domain: ".goofish.com", Path: "/"}})
	// incompleteMetadata 表示没有权威浏览器快照键的旧账号元数据。
	incompleteMetadata := `{"other":true}`
	if !hasCompleteCookieSnapshot(completeMetadata) || hasCompleteCookieSnapshot(incompleteMetadata) || hasCompleteCookieSnapshot("") {
		t.Fatal("Cookie 快照完整性判断异常")
	}
}

// TestAdapterCredentialWakeCallbacks 验证凭证更新和传输就绪事件均转发到唤醒端口。
func TestAdapterCredentialWakeCallbacks(t *testing.T) {
	// store、cleanup 保存测试适配器依赖及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// adapter 保存待触发凭证唤醒事件的适配器。
	adapter := New(store, nil, nil)
	// recorder 保存凭证唤醒端口的调用记录。
	recorder := &credentialWakeRecorder{}
	adapter.SetCredentialWakeService(recorder)
	// ctx 是本测试回调调用共用的上下文。
	ctx := context.Background()
	adapter.OnCredentialUpdated(ctx, "cid")
	adapter.OnTransportReady(ctx, "cid")
	if len(recorder.accountIDs) != 2 || recorder.accountIDs[0] != "cid" || recorder.accountIDs[1] != "cid" {
		t.Fatalf("凭证唤醒回调记录异常=%v", recorder.accountIDs)
	}
	// canceledContext 保存用于协议续期 wrapper 的取消上下文，避免触发外部平台请求。
	canceledContext, cancel := context.WithCancel(ctx)
	cancel()
	if adapter.RecoverExpiredCredential(canceledContext, "cid") {
		t.Fatal("取消上下文不应报告凭证恢复成功")
	}
	adapter.SetCredentialWakeService(nil)
	adapter.OnCredentialUpdated(ctx, "cid")
	adapter.OnTransportReady(ctx, "cid")
}
