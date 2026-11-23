package engine

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// localOwnershipHandlerFixture 在角色仓储夹具上补充本地商品归属结果，验证生产自动回复不会回退到商品详情接口。
type localOwnershipHandlerFixture struct {
	*roleHandlerFixture
	// owned 表示当前测试商品是否存在于账号的有效本地商品集合。
	owned bool
	// ownershipErr 是本地归属查询按场景返回的数据库错误。
	ownershipErr error
}

// ItemBelongsToAccount 返回本地商品归属夹具，不读取真实数据库或账号凭证。
func (fixture *localOwnershipHandlerFixture) ItemBelongsToAccount(_ context.Context, _, _ string) (bool, error) {
	return fixture.owned, fixture.ownershipErr
}

// roleHandlerFixture 在内存中保存会话角色，并复用 recordingHandler 观察消息业务旁路。
type roleHandlerFixture struct {
	*recordingHandler
	// roleMu 保护角色快照及读写次数。
	roleMu sync.Mutex
	// role 保存当前会话与商品绑定的角色结论。
	role db.ChatSession
	// roleReads 和 roleWrites 分别记录本地角色读取与持久化次数。
	roleReads, roleWrites int
	// roleWriteErr 是角色持久化夹具按场景返回的错误。
	roleWriteErr error
	// handled 在消息业务处理完成后接收通知，供时序测试建立屏障。
	handled chan struct{}
	// handledOnce 保证多条消息只发送一次完成通知。
	handledOnce sync.Once
}

// HandleChatMessage 先记录业务消息，再通知时序测试本地处理已经完成。
func (fixture *roleHandlerFixture) HandleChatMessage(ctx context.Context, message ChatMessage) error {
	// err 保存基础记录处理器的执行结果。
	err := fixture.recordingHandler.HandleChatMessage(ctx, message)
	if fixture.handled != nil {
		fixture.handledOnce.Do(func() { fixture.handled <- struct{}{} })
	}
	return err
}

// ChatSessionRole 返回内存角色；角色绑定商品不一致时返回 unknown。
func (fixture *roleHandlerFixture) ChatSessionRole(_ context.Context, accountID, chatID, itemID string) (db.ChatSession, error) {
	fixture.roleMu.Lock()
	defer fixture.roleMu.Unlock()
	fixture.roleReads++
	if fixture.role.RoleItemID != itemID {
		return db.ChatSession{CookieID: accountID, ChatID: chatID, ItemID: itemID, AccountRole: "unknown"}, nil
	}
	return fixture.role, nil
}

// SaveChatSessionRole 固定首次本地商品归属结论，供后续消息只读本地缓存。
func (fixture *roleHandlerFixture) SaveChatSessionRole(_ context.Context, accountID, chatID, itemID, accountRole, buyerUserID, sellerUserID, roleSource string) error {
	fixture.roleMu.Lock()
	defer fixture.roleMu.Unlock()
	fixture.roleWrites++
	if fixture.roleWriteErr != nil {
		return fixture.roleWriteErr
	}
	fixture.role = db.ChatSession{CookieID: accountID, ChatID: chatID, ItemID: itemID, AccountRole: accountRole, BuyerUserID: buyerUserID, SellerUserID: sellerUserID, RoleItemID: itemID, RoleSource: roleSource}
	return nil
}

// TestBuyerMessagesRemainVisibleWithoutAutoReply 验证真实防抖分发链保留买家侧聊天观察，并在 API 和默认回复之前检查身份；t 管理夹具。
func TestBuyerMessagesRemainVisibleWithoutAutoReply(t *testing.T) {
	// seller 表示当前账号是否是会话商品发布人。
	for _, seller := range []bool{false, true} {
		// apiReply 表示本次使用 API 回复或默认回复，验证身份门禁位于整个回复链之前。
		for _, apiReply := range []bool{false, true} {
			// store、cleanup 提供独立数据库，防止默认回复发送记录跨场景污染。
			store, cleanup := newReplyStore(t)
			// fixtureErr 是默认回复配置写入结果。
			if _, fixtureErr := store.DB.ExecContext(context.Background(), `INSERT INTO default_replies (cookie_id,enabled,reply_content) VALUES ('cid',1,'默认回复')`); fixtureErr != nil {
				cleanup()
				t.Fatal(fixtureErr)
			}
			// sender 记录真实回复链发送的文本和图片；主测试仅在完成通道关闭后读取。
			sender := &recordingSender{}
			// api 提供可计数的 API 回复，禁止买家侧执行外部回复查询。
			api := &fakeAPIReplier{result: &ReplyResult{Text: "API 回复"}}
			// reply 是待测回复链，默认场景不装配 API 优先层。
			reply := NewReplyService("cid", store, sender, nil, nil, nil)
			if apiReply {
				reply.api = api
			}
			// observer 记录发给业务旁路的聊天，并提供本地商品归属事实。
			observer := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}, owned: seller}
			// done 在每个生命周期任务结束时收到一个信号：一条消息对应两个任务（接收落库、合并等待到期后的回复），
			// 测试等待两个都结束后再断言，建立异步写入与断言的同步关系。
			done := make(chan struct{}, 4)
			// dispatcher 注入本地商品归属和完成通知，不启动账号网络连接。
			dispatcher := newMessageDispatcher(messageDispatcherConfig{
				CookieID: "cid", Reply: reply,
				CurrentCookie:  func() string { return "unb=123" },
				CurrentHandler: func() Handler { return observer },
				BeginTask: func() (context.Context, func(), bool) {
					return context.Background(), func() { done <- struct{}{} }, true
				},
			})
			dispatcher.scheduleDebouncedReply(chatMsg("你好", "conversation-item", "conversation"))
			// finished 统计已结束的生命周期任务数。
			for finished := 0; finished < 2; finished++ {
				select {
				case <-done:
				case <-time.After(5 * time.Second):
					t.Fatal("防抖消息未完成")
				}
			}
			dispatcher.stop()
			cleanup()
			if len(observer.chats) != 1 {
				t.Fatal("身份门禁不应丢弃买家侧聊天消息")
			}
			if seller && len(sender.texts) != 1 {
				t.Fatal("卖家没有收到预期的自动回复")
			}
			if !seller && (len(sender.texts) != 0 || len(sender.images) != 0 || api.called != 0) {
				t.Fatal("买家侧错误进入自动回复或发送消息")
			}
			if observer.roleWrites != 1 {
				t.Fatalf("本地商品归属应只写入一次角色事实: save=%d", observer.roleWrites)
			}
		}
	}
}

// TestReplyIdentityUsesPersistedRole 验证本地商品归属只在首次未知角色时写入卖家角色，后续消息复用本地角色。
func TestReplyIdentityUsesPersistedRole(t *testing.T) {
	// observer 保存首次本地商品归属写入的卖家角色。
	observer := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}, owned: true}
	// dispatcher 使用固定账号身份和本地商品归属事实。
	dispatcher := newMessageDispatcher(messageDispatcherConfig{
		CurrentCookie:  func() string { return "unb=self" },
		CurrentHandler: func() Handler { return observer },
	})
	// message 是绑定固定账号、会话和商品的入站消息。
	message := ChatMessage{AccountID: "cid", ChatID: "chat", ItemID: "item", SenderUserID: "buyer"}
	// firstAllowed 是首次未知角色经本地商品归属后的自动回复结论。
	firstAllowed := dispatcher.canAutoReply(context.Background(), message)
	// secondAllowed 是同一会话后续只读本地角色的自动回复结论。
	secondAllowed := dispatcher.canAutoReply(context.Background(), message)
	if !firstAllowed || !secondAllowed {
		t.Fatal("持久卖家角色应允许后续自动回复")
	}
	if observer.roleWrites != 1 || observer.roleReads != 2 {
		t.Fatalf("本地角色缓存写入次数异常: read=%d write=%d", observer.roleReads, observer.roleWrites)
	}
}

// TestReplyIdentityUnresolvedLocalItemSkipsRepeatedLookup 验证商品不在本地库时固定为不可回复，后续消息不会再尝试外部核验。
func TestReplyIdentityUnresolvedLocalItemSkipsRepeatedLookup(t *testing.T) {
	// observer 保存首次本地归属失败后的待同步标记。
	observer := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}, owned: false}
	// dispatcher 只注入本地商品归属能力，不存在商品详情回退。
	dispatcher := newMessageDispatcher(messageDispatcherConfig{
		CurrentCookie:  func() string { return "unb=self" },
		CurrentHandler: func() Handler { return observer },
	})
	// message 是连续到达两次的同一会话商品消息。
	message := ChatMessage{AccountID: "cid", ChatID: "chat", ItemID: "item", SenderUserID: "peer"}
	// firstAllowed、secondAllowed 分别保存首次及重复消息的本地门禁结果。
	firstAllowed := dispatcher.canAutoReply(context.Background(), message)
	// secondAllowed 保存重复消息再次经过本地门禁后的结果。
	secondAllowed := dispatcher.canAutoReply(context.Background(), message)
	if firstAllowed || secondAllowed {
		t.Fatal("商品不在本地库时不应允许自动回复")
	}
	if observer.roleWrites != 1 || observer.role.RoleSource != "platform_unresolved" {
		t.Fatalf("本地缺失结果没有固定待同步状态: save=%d source=%q", observer.roleWrites, observer.role.RoleSource)
	}
}

// TestReplyIdentityOwnershipWriteFailureSkipsReply 验证本地商品归属成立但角色写入失败时仍拒绝自动回复。
func TestReplyIdentityOwnershipWriteFailureSkipsReply(t *testing.T) {
	// observer 模拟本地角色事实写入失败。
	observer := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}, roleWriteErr: errors.New("本地模拟写入失败")}, owned: true}
	// dispatcher 只注入本地商品归属能力，写入失败不得进入任何外部身份查询。
	dispatcher := newMessageDispatcher(messageDispatcherConfig{
		CurrentCookie:  func() string { return "unb=self" },
		CurrentHandler: func() Handler { return observer },
	})
	// allowed 是本地卖家角色写入失败时的自动回复结论。
	allowed := dispatcher.canAutoReply(context.Background(), ChatMessage{AccountID: "cid", ChatID: "chat", ItemID: "item", SenderUserID: "peer"})
	if allowed || observer.roleWrites != 1 {
		t.Fatalf("本地角色写入失败边界不符: allow=%v writes=%d", allowed, observer.roleWrites)
	}
}

// TestReplyIdentityOwnershipReadFailureSkipsReply 验证本地商品库读取失败时不猜测商品归属，也不写入角色结论。
func TestReplyIdentityOwnershipReadFailureSkipsReply(t *testing.T) {
	// observer 模拟商品归属仓储读取失败；角色仓储仍可记录调用次数。
	observer := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}, ownershipErr: errors.New("本地商品查询失败")}
	// dispatcher 只依赖本地商品归属端口，查询错误必须直接关闭自动回复。
	dispatcher := newMessageDispatcher(messageDispatcherConfig{
		CurrentCookie:  func() string { return "unb=self" },
		CurrentHandler: func() Handler { return observer },
	})
	// allowed 是本地商品库不可读时的保守结果。
	allowed := dispatcher.canAutoReply(context.Background(), ChatMessage{AccountID: "cid", ChatID: "chat", ItemID: "item", SenderUserID: "peer"})
	if allowed || observer.roleWrites != 0 {
		t.Fatalf("本地商品查询失败仍产生自动回复或角色写入: allow=%v writes=%d", allowed, observer.roleWrites)
	}
}

// TestReplyIdentityKnownRolesRequireLocalItem 验证已确认角色也必须保持商品在本地库中，才允许卖家自动回复。
func TestReplyIdentityKnownRolesSkipPlatform(t *testing.T) {
	// cases 覆盖卖家/买家角色及商品是否仍在本地库中的组合。
	cases := []struct {
		// name 是测试场景名称。
		name string
		// accountRole 是已持久化的会话角色。
		accountRole string
		// owned 表示本地商品库是否仍有该商品。
		owned bool
		// allow 是自动回复预期结果。
		allow bool
	}{
		{name: "卖家且本地存在", accountRole: "seller", owned: true, allow: true},
		{name: "卖家但本地缺失", accountRole: "seller", owned: false, allow: false},
		{name: "买家且本地存在", accountRole: "buyer", owned: true, allow: false},
		{name: "买家且本地缺失", accountRole: "buyer", owned: false, allow: false},
	}
	// testCase 表示一组已知角色与本地商品存在性的门禁输入。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// role 保存与当前商品绑定的完整参与方结论。
			role := db.ChatSession{CookieID: "cid", ChatID: "chat", ItemID: "item", AccountRole: testCase.accountRole, BuyerUserID: "buyer", SellerUserID: "self", RoleItemID: "item", RoleSource: "local_item"}
			// observer 返回已持久化角色，并提供本地商品存在性结论。
			observer := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}, role: role}, owned: testCase.owned}
			// dispatcher 不再装配商品详情接口，证明已知角色也不具备外部回退。
			dispatcher := newMessageDispatcher(messageDispatcherConfig{
				CurrentCookie:  func() string { return "unb=self" },
				CurrentHandler: func() Handler { return observer },
			})
			// message 是与角色缓存完全一致的会话商品消息。
			message := ChatMessage{AccountID: "cid", ChatID: "chat", ItemID: "item", SenderUserID: "peer"}
			if dispatcher.canAutoReply(context.Background(), message) != testCase.allow {
				t.Fatalf("known role %s owned=%t allow mismatch", testCase.accountRole, testCase.owned)
			}
			if observer.roleWrites != 0 {
				t.Fatalf("已确认角色不应被本地缺失结果覆盖: writes=%d", observer.roleWrites)
			}
		})
	}
}

// TestReplyIdentityPrefersLocalOwnership 验证本地商品归属能直接恢复旧会话卖家角色，不触发商品详情风控请求。
func TestReplyIdentityPrefersLocalOwnership(t *testing.T) {
	// observer 保存旧会话的 unknown 角色，并提供当前商品属于账号的本地事实。
	observer := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}, owned: true}
	// dispatcher 只注入本地商品归属能力；身份判断没有商品详情回退。
	dispatcher := newMessageDispatcher(messageDispatcherConfig{
		CurrentCookie:  func() string { return "unb=self" },
		CurrentHandler: func() Handler { return observer },
	})
	// message 是旧会话收到的当前商品买家消息；商品本地归属应允许卖家回复。
	message := ChatMessage{AccountID: "cid", ChatID: "chat", ItemID: "item", SenderUserID: "buyer"}
	if !dispatcher.canAutoReply(context.Background(), message) {
		t.Fatal("本地商品归属确认后应允许自动回复")
	}
	if observer.role.AccountRole != "seller" || observer.role.RoleSource != "local_item" || observer.role.SellerUserID != "self" {
		t.Fatalf("本地卖家角色未正确持久化: %+v", observer.role)
	}
}

// TestReplyIdentityDoesNotUseDetailForUnknownLocalItem 验证本地商品不存在时直接拒绝自动回复并固定待同步状态。
func TestReplyIdentityDoesNotUseDetailForUnknownLocalItem(t *testing.T) {
	// observer 返回商品不在当前账号本地集合中的确定结论。
	observer := &localOwnershipHandlerFixture{roleHandlerFixture: &roleHandlerFixture{recordingHandler: &recordingHandler{}}, owned: false}
	// dispatcher 只注入本地归属能力，未知商品没有任何平台详情回退。
	dispatcher := newMessageDispatcher(messageDispatcherConfig{
		CurrentCookie:  func() string { return "unb=self" },
		CurrentHandler: func() Handler { return observer },
	})
	// message 是尚未同步到本地商品表的旧会话商品。
	message := ChatMessage{AccountID: "cid", ChatID: "chat", ItemID: "remote-item", SenderUserID: "buyer"}
	if dispatcher.canAutoReply(context.Background(), message) {
		t.Fatal("本地商品不存在时不得猜测卖家并自动回复")
	}
	if observer.role.RoleSource != "platform_unresolved" {
		t.Fatalf("未知商品未固定待同步状态: role=%+v", observer.role)
	}
}

// TestReplyIdentityMissingInputs 验证缺少商品、查询能力或当前账号身份时不得猜测卖家身份；t 管理断言。
func TestReplyIdentityMissingInputs(t *testing.T) {
	// dispatcher 故意没有角色仓储和本地商品归属能力。
	dispatcher := newMessageDispatcher(messageDispatcherConfig{})
	if dispatcher.canAutoReply(context.Background(), ChatMessage{ItemID: "item"}) {
		t.Fatal("缺少查询能力仍允许回复")
	}
	if dispatcher.canAutoReply(context.Background(), ChatMessage{}) || dispatcher.canAutoReply(context.Background(), ChatMessage{ItemID: "item"}) {
		t.Fatal("缺少身份或商品仍允许回复")
	}
}
