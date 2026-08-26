package adapter

import (
	"context"
	"errors"
	"testing"

	accountmanager "xianyu-go/internal/account"
	chatapp "xianyu-go/internal/application/chat"
	domainchat "xianyu-go/internal/chat"
	"xianyu-go/internal/xianyu/mtop"
)

// TestChatSendingWrappersMapOutgoingAndMediaMessages 验证聊天外发适配器的文本、媒体和状态转换。
func TestChatSendingWrappersMapOutgoingAndMediaMessages(t *testing.T) {
	// store、cleanup 保存隔离的 SQLite 存储和关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试聊天外发共用的上下文。
	ctx := context.Background()
	// repository 保存真实领域聊天服务包装后的应用外发仓储。
	repository := NewChatOutgoingRepository(domainchat.New(store))
	// session 保存外发消息共同使用的应用会话摘要。
	session := chatapp.Session{AccountID: "cid", ChatID: "chat-send", BuyerID: "buyer-send", BuyerName: "买家"}
	// textMessage、textErr 保存文本外发消息的模型转换结果。
	textMessage, textErr := repository.CreateOutgoing(ctx, session, "你好")
	if textErr != nil || textMessage.Content != "你好" || textMessage.AccountID != "cid" {
		t.Fatalf("文本外发转换异常 message=%+v err=%v", textMessage, textErr)
	}
	// mediaMessage、mediaErr 保存媒体外发消息的模型转换结果。
	mediaMessage, mediaErr := repository.CreateOutgoingMedia(ctx, session, "image", "https://cdn.invalid/image.jpg")
	if mediaErr != nil || mediaMessage.MessageType != "image" || mediaMessage.Content == "" {
		t.Fatalf("媒体外发转换异常 message=%+v err=%v", mediaMessage, mediaErr)
	}
	// statusMessage、statusErr 保存消息状态更新后的应用模型。
	statusMessage, statusErr := repository.SetOutgoingStatus(ctx, "cid", textMessage.MessageKey, "sent")
	if statusErr != nil || statusMessage.Status != "sent" {
		t.Fatalf("外发状态转换异常 message=%+v err=%v", statusMessage, statusErr)
	}
	// unavailableMediaErr 保存未装配领域服务时的媒体错误。
	_, unavailableMediaErr := NewChatOutgoingRepository(nil).CreateOutgoingMedia(ctx, session, "image", "url")
	if !errors.Is(unavailableMediaErr, chatapp.ErrUnavailable) {
		t.Fatalf("未装配媒体仓储错误=%v", unavailableMediaErr)
	}
	// unavailableStatusErr 保存未装配领域服务时的状态错误。
	_, unavailableStatusErr := NewChatOutgoingRepository(nil).SetOutgoingStatus(ctx, "cid", "key", "sent")
	if !errors.Is(unavailableStatusErr, chatapp.ErrUnavailable) {
		t.Fatalf("未装配状态仓储错误=%v", unavailableStatusErr)
	}
}

// TestChatSenderForwardsToRuntime 验证应用层发送器把文本与图片请求透传到账号运行时。
func TestChatSenderForwardsToRuntime(t *testing.T) {
	// ctx 是本测试发送器共用的上下文。
	ctx := context.Background()
	// unavailableSender 保存空运行时发送器的应用层包装。
	unavailableSender := chatSender{}
	// textErr 保存空运行时发送文本时返回的应用层错误。
	if err := unavailableSender.SendText(ctx, "chat", "buyer", "text", "key"); !errors.Is(err, chatapp.ErrUnavailable) {
		t.Fatalf("空文本发送错误=%v", err)
	}
	// imageErr 保存空运行时发送图片时返回的应用层错误。
	if err := unavailableSender.SendImage(ctx, "chat", "buyer", "url", 1, 10, 20, "key"); !errors.Is(err, chatapp.ErrUnavailable) {
		t.Fatalf("空图片发送错误=%v", err)
	}
	// runtimeSender 保存可记录透传调用的账号运行时发送器。
	runtimeSender := &coverageAutomationSender{}
	// sender 保存运行时发送器的应用层包装。
	sender := chatSender{sender: runtimeSender}
	// sendErr 保存文本请求透传到账号运行时的结果。
	if sendErr := sender.SendText(ctx, "chat", "buyer", "text", "key"); sendErr != nil {
		t.Fatal(sendErr)
	}
	// imageErr 保存图片请求透传到账号运行时的结果。
	if imageErr := sender.SendImage(ctx, "chat", "buyer", "url", 1, 10, 20, "key"); imageErr != nil {
		t.Fatal(imageErr)
	}
}

// TestChatReadReporterWithoutRuntime 验证没有账号管理器时已读上报保持无副作用。
func TestChatReadReporterWithoutRuntime(t *testing.T) {
	// reporter 保存未装配账号管理器的已读上报适配器。
	reporter := NewChatReadReporter(nil)
	// err 保存无运行时上报时的结果。
	err := reporter.ReportRead(context.Background(), "cid", "chat", []map[string]any{{"messageId": "key"}})
	if err != nil {
		t.Fatal(err)
	}
}

// TestChatReadReporterAndSenderProviderUseStartedRuntime 验证已启动账号会进入已读上报和发送器适配分支。
func TestChatReadReporterAndSenderProviderUseStartedRuntime(t *testing.T) {
	// store、cleanup 保存账号管理器启动所需的隔离数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// manager 保存只用于测试运行时句柄解析的账号管理器。
	manager := accountmanager.NewManager(store, &Adapter{}, nil)
	// ctx、cancel 保存账号运行时的生命周期上下文。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// startErr 保存测试账号运行实例登记结果。
	startErr := manager.Start(ctx, "cid", "unb=1; _m_h5_tk=tk;")
	if startErr != nil {
		t.Fatalf("启动测试账号失败: %v", startErr)
	}
	// reporter 保存绑定已启动账号管理器的已读上报适配器。
	reporter := NewChatReadReporter(manager)
	// reportErr 保存账号尚未建立 WebSocket 时由运行时返回的生命周期错误。
	reportErr := reporter.ReportRead(ctx, "cid", "chat", []map[string]any{{"messageId": "key"}})
	if reportErr == nil {
		t.Fatal("未建立 WebSocket 时已读上报应返回运行时错误")
	}
	// provider 保存绑定已启动账号管理器的发送器解析适配器。
	provider := NewChatSenderProvider(manager)
	// sender、ok 保存已启动账号对应的聊天发送器及解析结果。
	sender, ok := provider.Sender("cid")
	if !ok || sender == nil {
		t.Fatalf("已启动账号发送器解析失败 sender=%v ok=%v", sender, ok)
	}
	// runtimeUpload 保存带刷新凭证的平台图片上传结果，验证运行时 Cookie 同步分支。
	runtimeUpload := fakeChatUploadClient{upload: &mtop.ChatImageUpload{URL: "https://cdn.example/runtime.jpg", UpdatedCookies: "unb=1; _m_h5_tk=runtime;"}}
	// runtimeImage、runtimeImageErr 保存绑定账号管理器的图片上传结果。
	runtimeImage, runtimeImageErr := NewChatImageUploader(store, func() mtop.Client { return runtimeUpload }, manager).UploadChatImage(ctx, "cid", "a.jpg", "image/jpeg", []byte("image"))
	if runtimeImageErr != nil || runtimeImage.URL == "" {
		t.Fatalf("运行时 Cookie 同步上传失败 image=%+v err=%v", runtimeImage, runtimeImageErr)
	}
	manager.Stop("cid")
}

// TestChatReadReporterAndSenderProviderRejectMissingRuntime 验证管理器存在但账号未启动时保持无副作用。
func TestChatReadReporterAndSenderProviderRejectMissingRuntime(t *testing.T) {
	// manager 保存没有任何运行实例的账号管理器。
	manager := accountmanager.NewManager(nil, nil, nil)
	// reporter 保存绑定空运行实例表的已读上报适配器。
	reporter := NewChatReadReporter(manager)
	// reportErr 保存不存在账号的已读上报结果。
	if reportErr := reporter.ReportRead(context.Background(), "missing", "chat", nil); reportErr != nil {
		t.Fatalf("不存在账号的已读上报错误=%v", reportErr)
	}
	// provider 保存绑定空运行实例表的发送器解析适配器。
	provider := NewChatSenderProvider(manager)
	// sender、ok 保存不存在账号时的发送器解析结果。
	sender, ok := provider.Sender("missing")
	if ok || sender != nil {
		t.Fatalf("不存在账号不应返回发送器 sender=%v ok=%v", sender, ok)
	}
}

// TestChatSendingConstructionAndCredentialGuards 验证聊天应用构造和凭证仓储的缺失依赖保护。
func TestChatSendingConstructionAndCredentialGuards(t *testing.T) {
	// ctx 是本测试依赖保护共用的上下文。
	ctx := context.Background()
	// emptyRepository 保存没有数据库入口的聊天凭证仓储。
	emptyRepository := chatCredentialRepository{}
	// readErr 保存缺少 Cookie 子仓储时的读取错误。
	if _, readErr := emptyRepository.getCookieValue(ctx, "cid"); !errors.Is(readErr, chatapp.ErrUnavailable) {
		t.Fatalf("缺失 Cookie 仓储读取错误=%v", readErr)
	}
	// updateErr 保存缺少 Cookie 子仓储时的写入错误。
	if updateErr := emptyRepository.updateCookieValue(ctx, "cid", "cookie"); !errors.Is(updateErr, chatapp.ErrUnavailable) {
		t.Fatalf("缺失 Cookie 仓储写入错误=%v", updateErr)
	}
	// emptyService 保存缺少领域服务时仍可构造的历史聊天应用服务。
	emptyService := NewChatSendingApplication(nil, nil, nil, nil)
	if emptyService == nil {
		t.Fatal("聊天应用服务构造结果为空")
	}
	// store、cleanup 保存完整聊天应用构造使用的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// completeService 保存带领域服务和数据库适配器的聊天应用服务。
	completeService := NewChatSendingApplication(domainchat.New(store), store, nil, nil)
	if completeService == nil {
		t.Fatal("完整聊天应用服务构造结果为空")
	}
	// emptyMessage 保存 nil 数据库消息转换出的零值应用模型。
	emptyMessage := chatApplicationMessage(nil)
	if emptyMessage != (chatapp.Message{}) {
		t.Fatalf("空消息转换异常 message=%+v", emptyMessage)
	}
}
