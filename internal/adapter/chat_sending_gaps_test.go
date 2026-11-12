package adapter

import (
	"context"
	"errors"
	"testing"

	accountmanager "xianyu-go/internal/account"
	chatapp "xianyu-go/internal/application/chat"
	domainchat "xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/ws"
)

// staticChatSenderProvider 固定返回同一个发送器，用于隔离在线账号管理器。
type staticChatSenderProvider struct {
	// sender 是所有账号共用的测试发送器。
	sender chatapp.Sender
}

// Sender 对任意账号返回固定发送器。
func (p staticChatSenderProvider) Sender(string) (chatapp.Sender, bool) {
	return p.sender, p.sender != nil
}

// TestChatReplyDeliverySendReplyWithRealService 覆盖自动回复经真实聊天应用服务发送的成功与不确定结果映射。
func TestChatReplyDeliverySendReplyWithRealService(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// runtimeSender 是可控制发送错误的账号运行时替身。
	runtimeSender := &coverageAutomationSender{}
	// service 使用真实仓储与固定发送器。
	service := chatapp.NewWithSending(NewChatRepository(store), NewChatOutgoingRepository(domainchat.New(store)), staticChatSenderProvider{sender: chatSender{sender: runtimeSender}}, nil)
	// delivery 是被测自动回复发送端口。
	delivery := NewChatReplyDelivery(service)
	// message 是待发送的纯文字回复。
	message := engine.ReplyMessage{AccountID: "cid", ChatID: "chat-reply", ToUserID: "buyer", Text: "你好"}
	// result、err 是成功发送的结果。
	result, err := delivery.SendReply(ctx, message)
	if err != nil || !result.TextSent || result.Uncertain || runtimeSender.text != "你好" {
		t.Fatalf("成功发送: %+v %v", result, err)
	}
	// 自动回复必须携带自动标记到达账号运行时，否则会被误判为网页聊天的人工发送而暂停 AI。
	if !runtimeSender.automaticReply {
		t.Fatal("自动回复发送上下文应带自动回复标记")
	}
	// 平台结果不确定时必须标记 Uncertain 并保留错误。
	runtimeSender.sendErr = &ws.SendError{Kind: ws.SendUncertain}
	if result, err = delivery.SendReply(ctx, message); err == nil || !result.Uncertain {
		t.Fatalf("不确定发送: %+v %v", result, err)
	}
}

// TestChatItemCatalogGuardsAndErrors 覆盖商品目录的依赖缺失、凭证缺失、客户端能力缺失和搜索错误路径。
func TestChatItemCatalogGuardsAndErrors(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// store、cleanup 是隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	if NewChatItemCatalog(nil, func() mtop.Client { return nil }, nil) != nil || NewChatItemCatalog(store, nil, nil) != nil {
		t.Fatal("缺依赖应返回 nil 目录")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := (chatItemCatalog{}).ListChatItems(ctx, "cid", "c", "self", "", 1); !errors.Is(err, chatapp.ErrUnavailable) {
		t.Fatalf("空存储: %v", err)
	}
	// client 是可编排结果的搜索客户端。
	client := &coverageChatItemClient{}
	// catalog 绑定隔离存储与客户端。
	catalog := NewChatItemCatalog(store, func() mtop.Client { return client }, nil)
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := catalog.ListChatItems(ctx, "missing", "c", "self", "", 1); err == nil {
		t.Fatal("账号缺失应失败")
	}
	// 搜索返回空结果且无错误：商品查询不可用。
	if _, err := catalog.ListChatItems(ctx, "cid", "c", "self", "", 1); !errors.Is(err, chatapp.ErrChatItemUnavailable) {
		t.Fatalf("空结果: %v", err)
	}
	// 搜索错误原样返回。
	client.err = errors.New("search failed")
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := catalog.ListChatItems(ctx, "cid", "c", "self", "", 1); !errors.Is(err, client.err) {
		t.Fatalf("搜索错误: %v", err)
	}
	// 搜索期间凭证被改写，持久化指纹复核失败，错误与搜索错误合并。
	client.replacement = nil
	// racing 在搜索前改写 Cookie 以触发指纹冲突。
	racing := &racingChatItemClient{store: store}
	// racingCatalog 测试中的局部对象，承载被测流程的输入、依赖或输出。
	racingCatalog := NewChatItemCatalog(store, func() mtop.Client { return racing }, nil)
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := racingCatalog.ListChatItems(ctx, "cid", "c", "self", "", 1); !errors.Is(err, errChatCredentialChanged) {
		t.Fatalf("并发变更: %v", err)
	}
	racing.err = errors.New("also failed")
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := racingCatalog.ListChatItems(ctx, "cid", "c", "self", "", 1); !errors.Is(err, errChatCredentialChanged) || !errors.Is(err, racing.err) {
		t.Fatalf("合并错误: %v", err)
	}
	// 客户端不支持商品查询。
	unsupported := NewChatItemCatalog(store, func() mtop.Client { return fakeChatUploadClient{} }, nil)
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := unsupported.ListChatItems(ctx, "cid", "c", "self", "", 1); !errors.Is(err, chatapp.ErrChatItemUnavailable) {
		t.Fatalf("不支持: %v", err)
	}
	// 空凭证账号。
	if err := store.Cookies.UpdateValueExisting(ctx, "cid", ""); err != nil {
		t.Fatal(err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := catalog.ListChatItems(ctx, "cid", "c", "self", "", 1); err == nil {
		t.Fatal("空凭证应失败")
	}
}

// racingChatItemClient 在搜索期间改写账号 Cookie，模拟并发登录或续期。
type racingChatItemClient struct {
	// Client 占位其余 MTOP 能力。
	mtop.Client
	// store 用于在搜索期间写入新的凭证。
	store *db.Store
	// err 是搜索返回的错误。
	err error
	// calls 统计调用次数，使每次改写的 Cookie 值都不同。
	calls int
}

// SearchChatItems 先改写账号 Cookie 再返回预设错误。
func (c *racingChatItemClient) SearchChatItems(ctx context.Context, _, _, _, _ string, _ int) (*mtop.ChatItemPage, error) {
	c.calls++
	_ = c.store.Cookies.UpdateValueExisting(ctx, "cid", "unb=1; _m_h5_tk=raced"+string(rune('a'+c.calls)))
	return nil, c.err
}

// TestChatCredentialRepositoryPersistBranches 覆盖凭证仓储的空依赖、读取失败、平面更新与未变化路径。
func TestChatCredentialRepositoryPersistBranches(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, _, err := (chatCredentialRepository{}).persistCookieSession(ctx, db.CookiePlatformRuntimeData{ID: "cid"}, nil, ""); !errors.Is(err, chatapp.ErrUnavailable) {
		t.Fatalf("空存储: %v", err)
	}
	// store、cleanup 是隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 绑定隔离存储。
	repository := chatCredentialRepository{store: store}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, _, err := repository.persistCookieSession(ctx, db.CookiePlatformRuntimeData{ID: "missing"}, nil, ""); err == nil {
		t.Fatal("账号缺失应失败")
	}
	// initial 是平台请求开始前的凭证视图。
	initial, err := store.Cookies.GetCookiePlatformRuntimeData(ctx, "cid")
	if err != nil {
		t.Fatal(err)
	}
	// 无会话且无更新：不同步。
	if _, sync, err := repository.persistCookieSession(ctx, initial, nil, ""); sync || err != nil {
		t.Fatalf("无变化: %v %v", sync, err)
	}
	// 会话未变化且带空快照：同样不同步。
	_, session := withCookieSnapshot(ctx, initial)
	// err、sync 依次为：保存本步骤调用返回的错误，用于断言预期的成功或失败路径；测试中的局部对象，承载被测流程的输入、依赖或输出。
	if _, sync, err := repository.persistCookieSession(ctx, initial, session, ""); sync || err != nil {
		t.Fatalf("会话未变化: %v %v", sync, err)
	}
	// 平面 Cookie 更新被保存并要求同步。
	if value, sync, err := repository.persistCookieSession(ctx, initial, nil, "unb=1; flat=1"); !sync || err != nil || value != "unb=1; flat=1" {
		t.Fatalf("平面更新: %q %v %v", value, sync, err)
	}
	// 凭证读取服务：getCookieValue/updateCookieValue 成功路径。
	if _, err := repository.getCookieValue(ctx, "cid"); err != nil {
		t.Fatal(err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := repository.updateCookieValue(ctx, "cid", "unb=1; z=1"); err != nil {
		t.Fatal(err)
	}
	// 数据库关闭后写回失败。
	cleanup()
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, _, err := repository.persistCookieSession(ctx, initial, nil, "unb=1; other=1"); err == nil {
		t.Fatal("数据库关闭后应失败")
	}
}

// TestChatImageUploaderGuardsAndErrors 覆盖图片上传的依赖缺失、凭证缺失、能力缺失、平台错误和写回冲突。
func TestChatImageUploaderGuardsAndErrors(t *testing.T) {
	// ctx 是调用共用的后台上下文。
	ctx := context.Background()
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if _, err := NewChatImageUploader(nil, nil, nil).UploadChatImage(ctx, "cid", "a.jpg", "image/jpeg", nil); !errors.Is(err, chatapp.ErrUnavailable) {
		t.Fatalf("空依赖: %v", err)
	}
	// store、cleanup 是隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// upload 用给定客户端发起上传。
	upload := func(client mtop.Client, account string) error {
		// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
		_, err := NewChatImageUploader(store, func() mtop.Client { return client }, nil).UploadChatImage(ctx, account, "a.jpg", "image/jpeg", []byte("x"))
		return err
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := upload(fakeChatUploadClient{}, "missing"); err == nil {
		t.Fatal("账号缺失应失败")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := upload(&coverageChatItemClient{}, "cid"); !errors.Is(err, chatapp.ErrUnavailable) {
		t.Fatalf("不支持上传: %v", err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := upload(fakeChatUploadClient{err: errors.New("upload failed")}, "cid"); err == nil {
		t.Fatal("平台错误应返回")
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := upload(fakeChatUploadClient{}, "cid"); !errors.Is(err, chatapp.ErrSend) {
		t.Fatalf("空上传结果: %v", err)
	}
	// 上传期间凭证被改写，复核失败。
	conflict := fakeChatUploadClient{upload: &mtop.ChatImageUpload{URL: "u", UpdatedCookies: "unb=1; new=1"}, beforeReturn: func() {
		_ = store.Cookies.UpdateValueExisting(ctx, "cid", "unb=1; raced=1")
	}}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := upload(conflict, "cid"); !errors.Is(err, errChatCredentialChanged) {
		t.Fatalf("写回冲突: %v", err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := store.Cookies.UpdateValueExisting(ctx, "cid", ""); err != nil {
		t.Fatal(err)
	}
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := upload(fakeChatUploadClient{}, "cid"); err == nil {
		t.Fatal("空凭证应失败")
	}
}

// TestChatItemCatalogUpdateRuntimeCookie 覆盖商品目录把刷新凭证同步到在线运行时的各分支。
func TestChatItemCatalogUpdateRuntimeCookie(t *testing.T) {
	// ctx、cancel 是账号运行时的生命周期上下文。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// store、cleanup 是隔离数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// 无管理器：无副作用。
	(chatItemCatalog{}).updateRuntimeCookie(ctx, "cid", "unb=1")
	// manager 管理一个已登记的账号运行实例。
	manager := accountmanager.NewManager(store, &Adapter{}, nil)
	// catalog 绑定管理器。
	catalog := chatItemCatalog{manager: manager}
	catalog.updateRuntimeCookie(ctx, "missing", "unb=1")
	// err 保存本步骤调用返回的错误，用于断言预期的成功或失败路径。
	if err := manager.Start(ctx, "cid", "unb=1; _m_h5_tk=tk;"); err != nil {
		t.Fatal(err)
	}
	catalog.updateRuntimeCookie(ctx, "cid", "unb=1; _m_h5_tk=updated;")
	manager.Stop("cid")
}
