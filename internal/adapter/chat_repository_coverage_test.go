package adapter

import (
	"context"
	"errors"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// TestChatRepositoryMapsMessagesAndMetadata 验证聊天适配器把本地非敏感记录转换为应用模型。
func TestChatRepositoryMapsMessagesAndMetadata(t *testing.T) {
	// store、cleanup 保存隔离的 SQLite 存储和关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库操作共用的上下文。
	ctx := context.Background()
	// owner 保存测试账号所属用户，用于消息查询的归属过滤。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// repository 是面向聊天应用端口的数据库适配器。
	repository := NewChatRepository(store)
	// session 保存消息和会话共同使用的非敏感聊天摘要。
	session := db.ChatSession{CookieID: "cid", ChatID: "chat-cover", BuyerID: "buyer-cover", BuyerName: "买家"}
	// message 保存需要被转换为应用模型的入站消息。
	message := db.ChatMessage{MessageKey: "message-cover", Direction: "incoming", SenderID: "buyer-cover", SenderName: "买家", MessageType: "text", Content: "你好", MediaDuration: 3, Status: "received", SentAt: 100}
	// saved、inserted、saveErr 保存消息首次写入结果。
	if saved, inserted, saveErr := store.Chats.SaveMessage(ctx, session, message, true); saveErr != nil || !inserted || saved.Content != "你好" {
		t.Fatalf("保存消息失败 saved=%+v inserted=%v err=%v", saved, inserted, saveErr)
	}
	// port 保存会话适配器的完整接口视图。
	port, ok := repository.(chatapp.SessionRepository)
	if !ok {
		t.Fatal("聊天适配器未实现 SessionRepository")
	}
	// messages、listErr 保存消息分页转换结果。
	messages, listErr := port.ListMessages(ctx, owner.ID, "cid", "chat-cover", 0, 20)
	if listErr != nil || len(messages) != 1 || messages[0].Content != "你好" || messages[0].MediaDuration != 3 {
		t.Fatalf("消息转换异常 messages=%+v err=%v", messages, listErr)
	}
	// readErr 保存本地会话标记已读的结果。
	if readErr := port.MarkRead(ctx, owner.ID, "cid", "chat-cover"); readErr != nil {
		t.Fatal(readErr)
	}
	// readBack 保存已读操作后重新读取的消息。
	readBack, readBackErr := port.ListMessages(ctx, owner.ID, "cid", "chat-cover", 0, 20)
	if readBackErr != nil || len(readBack) != 1 || readBack[0].ReadStatus != 2 {
		t.Fatalf("消息已读状态异常 messages=%+v err=%v", readBack, readBackErr)
	}
	// diagnostics 保存旧版消息关联迁移所需的入站解密帧。
	if diagnosticsErr := store.WSMessages.AddBatch(ctx, []db.WSMessage{{CookieID: "cid", Direction: "in", ParseStatus: "decrypted", ParsedJSON: `{"messageId":"message-cover"}`}}); diagnosticsErr != nil {
		t.Fatal(diagnosticsErr)
	}
	// frames、frameErr 保存诊断帧查询适配结果。
	frames, frameErr := repository.(chatapp.ReadMessageIDResolver).FindInboundParsedJSONContaining(ctx, "cid", "message-cover", 10)
	if frameErr != nil || len(frames) != 1 || frames[0] == "" {
		t.Fatalf("诊断帧查询异常 frames=%v err=%v", frames, frameErr)
	}
	// metadata 保存快捷回复和备注适配器的接口视图。
	metadata, metadataOK := repository.(chatapp.MetadataRepository)
	if !metadataOK {
		t.Fatal("聊天适配器未实现 MetadataRepository")
	}
	// reply、replyErr 保存快捷回复创建结果。
	reply, replyErr := metadata.CreateQuickReply(ctx, "cid", "请稍等，我马上为您确认")
	if replyErr != nil || reply.Content == "" || reply.AccountID != "cid" {
		t.Fatalf("快捷回复创建异常 reply=%+v err=%v", reply, replyErr)
	}
	// replies、repliesErr 保存快捷回复列表转换结果。
	replies, repliesErr := metadata.ListQuickReplies(ctx, "cid")
	if repliesErr != nil || len(replies) != 1 || replies[0].ID != reply.ID {
		t.Fatalf("快捷回复列表异常 replies=%+v err=%v", replies, repliesErr)
	}
	// deleted、deleteErr 保存快捷回复删除结果。
	deleted, deleteErr := metadata.DeleteQuickReply(ctx, "cid", reply.ID)
	if deleteErr != nil || !deleted {
		t.Fatalf("快捷回复删除异常 deleted=%v err=%v", deleted, deleteErr)
	}
	// note、noteErr 保存买家备注写入后的应用模型。
	note, noteErr := metadata.SaveBuyerNote(ctx, chatapp.BuyerNote{AccountID: "cid", BuyerID: "buyer-cover", Content: "偏好顺丰"})
	if noteErr != nil || note.Content != "偏好顺丰" || note.BuyerID != "buyer-cover" {
		t.Fatalf("买家备注保存异常 note=%+v err=%v", note, noteErr)
	}
	// loaded、found、loadErr 保存买家备注读取结果。
	loaded, found, loadErr := metadata.GetBuyerNote(ctx, "cid", "buyer-cover")
	if loadErr != nil || !found || loaded.Content != "偏好顺丰" {
		t.Fatalf("买家备注读取异常 note=%+v found=%v err=%v", loaded, found, loadErr)
	}
	// cleared、clearErr 保存空备注触发删除语义后的结果。
	cleared, clearErr := metadata.SaveBuyerNote(ctx, chatapp.BuyerNote{AccountID: "cid", BuyerID: "buyer-cover"})
	if clearErr != nil || cleared.Content != "" {
		t.Fatalf("买家备注清除异常 note=%+v err=%v", cleared, clearErr)
	}
	// _, stillFound、missingErr 保存清除后的备注查询状态。
	_, stillFound, missingErr := metadata.GetBuyerNote(ctx, "cid", "buyer-cover")
	if missingErr != nil || stillFound {
		t.Fatalf("买家备注仍存在 found=%v err=%v", stillFound, missingErr)
	}
}

// TestChatRepositoryRejectsUnavailableDependencies 验证聊天适配器不会接受不完整的数据库依赖。
func TestChatRepositoryRejectsUnavailableDependencies(t *testing.T) {
	// nilRepository 保存缺少数据库存储时的构造结果。
	nilRepository := NewChatRepository(nil)
	if nilRepository != nil {
		t.Fatal("空数据库存储不应构造聊天仓储")
	}
	// store、cleanup 保存正常测试数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// brokenStore 保存缺少聊天聚合入口的独立数据库存储。
	brokenStore := db.NewStore(store.DB, store.Dialect)
	brokenStore.Chats = nil
	// repository 保存缺少聊天存储时的构造结果，预期保持为空。
	if repository := NewChatRepository(brokenStore); repository != nil {
		t.Fatal("缺少聊天存储时不应构造聊天仓储")
	}
	// resolver 保存缺少平台客户端提供函数时的身份适配器构造结果。
	if resolver := NewChatIdentityResolver(store, nil); resolver != nil {
		t.Fatal("缺少客户端提供函数时不应构造身份适配器")
	}
	// unsupportedResolver 保存客户端不具备聊天身份能力时的适配器。
	unsupportedResolver := NewChatIdentityResolver(store, func() mtop.Client { return nil })
	// err 保存平台能力缺失时的稳定错误。
	_, err := unsupportedResolver.Resolve(context.Background(), "cid", "chat-cover")
	if err == nil || errors.Is(err, context.Canceled) {
		t.Fatalf("未支持聊天身份查询应返回业务错误 err=%v", err)
	}
}

// TestChatRepositoryCoversClosedDatabaseOperations 验证聊天适配器各数据库端点传播基础设施故障。
func TestChatRepositoryCoversClosedDatabaseOperations(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定已关闭数据库的聊天适配器。
	repository := NewChatRepository(store)
	// sessionRepository 是会话端口视图，用于调用消息和会话方法。
	sessionRepository := repository.(chatapp.SessionRepository)
	// metadataRepository 是元数据端口视图，用于调用快捷回复和备注方法。
	metadataRepository := repository.(chatapp.MetadataRepository)
	// diagnosticsRepository 是受限诊断端口视图，用于查询历史入站帧。
	diagnosticsRepository := repository.(chatapp.ReadMessageIDResolver)
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// ctx 是本测试全部数据库操作使用的非取消上下文。
	ctx := context.Background()
	// operations 保存需要统一验证底层错误传播的聊天操作结果。
	operations := []struct {
		name string
		err  error
	}{
		{name: "消息列表", err: func() error {
			// err 表示消息列表在关闭数据库后的底层错误。
			_, err := sessionRepository.ListMessages(ctx, 1, "cid", "chat", 0, 10)
			return err
		}()},
		{name: "会话列表", err: func() error {
			// err 表示会话列表在关闭数据库后的底层错误。
			_, err := sessionRepository.ListSessions(ctx, 1, "cid", 10)
			return err
		}()},
		{name: "删除空会话", err: sessionRepository.DeleteEmptySessions(ctx, "cid")},
		{name: "更新会话身份", err: sessionRepository.UpdateSessionIdentity(ctx, "cid", "chat", "buyer", "买家", "avatar")},
		{name: "账号归属", err: func() error {
			// err 表示账号归属查询在关闭数据库后的底层错误。
			_, err := sessionRepository.ExistsOwned(ctx, 1, "cid")
			return err
		}()},
		{name: "标记已读", err: sessionRepository.MarkRead(ctx, 1, "cid", "chat")},
		{name: "快捷回复列表", err: func() error {
			// err 表示快捷回复列表在关闭数据库后的底层错误。
			_, err := metadataRepository.ListQuickReplies(ctx, "cid")
			return err
		}()},
		{name: "创建快捷回复", err: func() error {
			// err 表示快捷回复创建在关闭数据库后的底层错误。
			_, err := metadataRepository.CreateQuickReply(ctx, "cid", "reply")
			return err
		}()},
		{name: "删除快捷回复", err: func() error {
			// err 表示快捷回复删除在关闭数据库后的底层错误。
			_, err := metadataRepository.DeleteQuickReply(ctx, "cid", 1)
			return err
		}()},
		{name: "读取备注", err: func() error {
			// err 表示买家备注读取在关闭数据库后的底层错误。
			_, _, err := metadataRepository.GetBuyerNote(ctx, "cid", "buyer")
			return err
		}()},
		{name: "保存备注", err: func() error {
			// err 表示买家备注保存在关闭数据库后的底层错误。
			_, err := metadataRepository.SaveBuyerNote(ctx, chatapp.BuyerNote{AccountID: "cid", BuyerID: "buyer", Content: "note"})
			return err
		}()},
	}
	// operation 表示当前待验证的聊天操作及其底层结果。
	for _, operation := range operations {
		if operation.err == nil {
			t.Errorf("%s 未传播数据库故障", operation.name)
		}
	}
	// diagnosticErr 表示诊断仓储在关闭数据库后的查询错误。
	if _, diagnosticErr := diagnosticsRepository.FindInboundParsedJSONContaining(ctx, "cid", "fragment", 1); diagnosticErr == nil {
		t.Fatal("诊断帧查询未传播数据库故障")
	}
}
