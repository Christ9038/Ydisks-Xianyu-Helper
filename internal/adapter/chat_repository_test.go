package adapter

import (
	"context"
	"errors"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// fakeChatUploadClient 只实现聊天图片上传能力，用于隔离平台网络请求。
type fakeChatUploadClient struct {
	// mtop.Client 保留其余平台能力占位，测试只关注图片上传方法。
	mtop.Client
	// upload 保存模拟平台返回的图片结果。
	upload *mtop.ChatImageUpload
	// err 保存模拟平台上传错误。
	err error
}

// UploadChatImage 返回预设图片结果或错误，不记录传入 Cookie。
func (c fakeChatUploadClient) UploadChatImage(context.Context, string, string, string, []byte) (*mtop.ChatImageUpload, error) {
	return c.upload, c.err
}

// fakeChatIdentityClient 只实现聊天身份查询，其余 MTOP 能力由嵌入接口占位。
type fakeChatIdentityClient struct {
	// mtop.Client 保留未涉及本切片的 MTOP 能力占位。
	mtop.Client
	// info 保存模拟平台返回的买家展示身份。
	info *mtop.ChatUserInfo
}

// FetchChatUserInfo 返回预设聊天身份，并记录适配器已能调用动态能力。
func (c fakeChatIdentityClient) FetchChatUserInfo(context.Context, string, string) (*mtop.ChatUserInfo, error) {
	return c.info, nil
}

// TestChatRepositoryMapsSessionMaintenance 验证聊天数据库适配器覆盖列表、清理、身份和归属端口。
func TestChatRepositoryMapsSessionMaintenance(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库操作使用的非取消上下文。
	ctx := context.Background()
	// repository 是聊天应用层会话端口的数据库实现。
	repository := NewChatRepository(store)
	// owner 是模板账号的所有者。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// session 是待写入数据库并转换回应用模型的非敏感会话。
	session := db.ChatSession{CookieID: "cid", ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家", LastMessage: "你好", LastMessageAt: 10}
	// saveErr 表示写入测试会话时的数据库错误。
	if saveErr := store.Chats.UpsertSession(ctx, session); saveErr != nil {
		t.Fatal(saveErr)
	}
	// port 是经类型断言确认的完整聊天维护端口。
	port, ok := repository.(chatapp.SessionRepository)
	if !ok {
		t.Fatal("聊天适配器未覆盖 SessionRepository")
	}
	// listed 和 listErr 保存应用层会话列表及转换错误。
	listed, listErr := port.ListSessions(ctx, owner.ID, "cid", 20)
	if listErr != nil || len(listed) != 1 || listed[0].BuyerName != "买家" {
		t.Fatalf("会话列表映射异常 listed=%+v err=%v", listed, listErr)
	}
	// owned 和 ownershipErr 保存账号归属查询结果。
	owned, ownershipErr := port.ExistsOwned(ctx, owner.ID, "cid")
	if ownershipErr != nil || !owned {
		t.Fatalf("账号归属映射异常 owned=%v err=%v", owned, ownershipErr)
	}
	// updateErr 保存会话身份缓存写入错误。
	if updateErr := port.UpdateSessionIdentity(ctx, "cid", "chat-1", "buyer-1", "新名称", "avatar"); updateErr != nil {
		t.Fatal(updateErr)
	}
	// refreshed 和 refreshedErr 保存身份缓存写入后的会话列表。
	refreshed, refreshedErr := port.ListSessions(ctx, owner.ID, "cid", 20)
	if refreshedErr != nil || refreshed[0].BuyerName != "新名称" || refreshed[0].BuyerAvatar != "avatar" {
		t.Fatalf("身份缓存未更新 refreshed=%+v err=%v", refreshed, refreshedErr)
	}
	// emptyErr 保存删除无消息会话壳的结果。
	if emptyErr := port.DeleteEmptySessions(ctx, "cid"); emptyErr != nil {
		t.Fatal(emptyErr)
	}
}

// TestChatIdentityResolverKeepsCredentialsInsideAdapter 验证身份适配器只向应用层返回展示字段。
func TestChatIdentityResolverKeepsCredentialsInsideAdapter(t *testing.T) {
	// store 是包含测试账号 Cookie 的临时数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 是返回非敏感身份的测试 MTOP 客户端。
	client := fakeChatIdentityClient{info: &mtop.ChatUserInfo{Nickname: "买家新名", AvatarURL: "https://example.invalid/avatar"}}
	// resolver 是读取 Cookie 并调用测试平台客户端的聊天身份适配器。
	resolver := NewChatIdentityResolver(store, func() mtop.Client { return client })
	// identity 和 resolveErr 保存适配器转换后的身份及查询错误。
	identity, resolveErr := resolver.Resolve(context.Background(), "cid", "chat-1")
	if resolveErr != nil || identity.BuyerName != "买家新名" || identity.BuyerAvatar == "" {
		t.Fatalf("身份映射异常 identity=%+v err=%v", identity, resolveErr)
	}
}

// TestChatSendingAdaptersRejectUnavailableDependencies 验证实时聊天适配器的未装配错误分支。
func TestChatSendingAdaptersRejectUnavailableDependencies(t *testing.T) {
	// outgoingErr 保存未装配聊天领域服务时的稳定应用错误。
	_, outgoingErr := NewChatOutgoingRepository(nil).CreateOutgoing(context.Background(), chatapp.Session{}, "文本")
	if !errors.Is(outgoingErr, chatapp.ErrUnavailable) {
		t.Fatalf("nil outgoing service error=%v", outgoingErr)
	}
	// senderProvider 是未装配账号管理器的在线发送适配器。
	senderProvider := NewChatSenderProvider(nil)
	// sender、ok 保存未装配管理器时返回的发送器和存在性标记。
	if sender, ok := senderProvider.Sender("account-1"); ok || sender != nil {
		t.Fatalf("nil sender provider returned sender=%v ok=%v", sender, ok)
	}
	// uploadErr 保存未提供平台客户端时的稳定应用错误。
	_, uploadErr := NewChatImageUploader(nil, nil, nil).UploadChatImage(context.Background(), "account-1", "a.jpg", "image/jpeg", []byte("image"))
	if !errors.Is(uploadErr, chatapp.ErrUnavailable) {
		t.Fatalf("nil image client error=%v", uploadErr)
	}
}

// TestChatImageUploaderRejectsUnsupportedAndEmptyPlatformResults 验证图片上传适配器不会吞掉平台能力缺失或空结果。
func TestChatImageUploaderRejectsUnsupportedAndEmptyPlatformResults(t *testing.T) {
	// store 是包含测试 Cookie 的临时数据库，凭证只在适配器调用期间读取。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// unsupportedErr 保存客户端缺少图片上传能力时的稳定错误。
	_, unsupportedErr := NewChatImageUploader(store, func() mtop.Client { return fakeChatIdentityClient{} }, nil).UploadChatImage(context.Background(), "cid", "a.jpg", "image/jpeg", []byte("image"))
	if !errors.Is(unsupportedErr, chatapp.ErrUnavailable) {
		t.Fatalf("unsupported image client error=%v", unsupportedErr)
	}
	// emptyErr 保存平台返回空图片结果时的发送错误。
	_, emptyErr := NewChatImageUploader(store, func() mtop.Client { return fakeChatUploadClient{} }, nil).UploadChatImage(context.Background(), "cid", "a.jpg", "image/jpeg", []byte("image"))
	if !errors.Is(emptyErr, chatapp.ErrSend) {
		t.Fatalf("empty image result error=%v", emptyErr)
	}
}

var _ chatapp.SessionRepository = chatRepository{}
var _ chatapp.IdentityResolver = chatIdentityResolver{}
