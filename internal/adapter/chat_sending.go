package adapter

import (
	"context"

	"xianyu-go/internal/account"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
	domainchat "xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/mtop"
)

// chatOutgoingRepository 将聊天领域服务适配为应用层外发消息端口。
type chatOutgoingRepository struct {
	// service 保存聊天消息幂等写入和实时事件发布能力。
	service *domainchat.Service
}

// NewChatSendingApplication 装配聊天历史、实时发送、图片上传和身份补全端口。
func NewChatSendingApplication(domainService *domainchat.Service, store *db.Store, manager *account.Manager, clientProvider func() mtop.Client) *chatapp.Service {
	if domainService == nil {
		// service 保留历史查询应用对象，但不伪造未装配的发送、订阅和刷新端口。
		return chatapp.NewWithSendingSubscriptionAndRefresh(NewChatRepository(store), nil, nil, nil, nil, nil, NewChatIdentityResolver(store, clientProvider))
	}
	return chatapp.NewWithSendingSubscriptionAndRefresh(
		NewChatRepository(store),
		NewChatOutgoingRepository(domainService),
		NewChatSenderProvider(manager),
		NewChatImageUploader(store, clientProvider, manager),
		NewChatSubscriptionProvider(domainService),
		NewChatRefreshProvider(domainService, manager),
		NewChatIdentityResolver(store, clientProvider),
	)
}

// NewChatOutgoingRepository 创建聊天外发消息的领域适配器。
func NewChatOutgoingRepository(service *domainchat.Service) chatapp.OutgoingRepository {
	return chatOutgoingRepository{service: service}
}

// CreateOutgoing 创建文字消息并转换为应用层非敏感模型。
func (r chatOutgoingRepository) CreateOutgoing(ctx context.Context, session chatapp.Session, text string) (chatapp.Message, error) {
	if r.service == nil {
		return chatapp.Message{}, chatapp.ErrUnavailable
	}
	// message、err 保存领域服务返回的消息及写入错误。
	message, err := r.service.CreateOutgoing(ctx, dbChatSession(session), text)
	return chatApplicationMessage(message), err
}

// CreateOutgoingMedia 创建媒体消息并转换为应用层非敏感模型。
func (r chatOutgoingRepository) CreateOutgoingMedia(ctx context.Context, session chatapp.Session, messageType, content string) (chatapp.Message, error) {
	if r.service == nil {
		return chatapp.Message{}, chatapp.ErrUnavailable
	}
	// message、err 保存领域服务返回的媒体消息及写入错误。
	message, err := r.service.CreateOutgoingMedia(ctx, dbChatSession(session), messageType, content)
	return chatApplicationMessage(message), err
}

// SetOutgoingStatus 更新外发消息状态并转换为应用层非敏感模型。
func (r chatOutgoingRepository) SetOutgoingStatus(ctx context.Context, accountID, key, status string) (chatapp.Message, error) {
	if r.service == nil {
		return chatapp.Message{}, chatapp.ErrUnavailable
	}
	// message、err 保存领域服务返回的状态消息及更新错误。
	message, err := r.service.SetOutgoingStatus(ctx, accountID, key, status)
	return chatApplicationMessage(message), err
}

// dbChatSession 将应用层会话摘要转换为领域层写入模型，不携带账号凭证。
func dbChatSession(session chatapp.Session) db.ChatSession {
	return db.ChatSession{
		CookieID: session.AccountID, ChatID: session.ChatID, BuyerID: session.BuyerID,
		BuyerName: session.BuyerName, BuyerAvatar: session.BuyerAvatar, ItemID: session.ItemID,
		ItemTitle: session.ItemTitle, LastMessage: session.LastMessage, LastMessageAt: session.LastMessageAt,
		UnreadCount: session.UnreadCount,
	}
}

// chatApplicationMessage 将领域消息转换为应用层非敏感模型；空指针保持零值。
func chatApplicationMessage(message *db.ChatMessage) chatapp.Message {
	if message == nil {
		return chatapp.Message{}
	}
	return chatapp.Message{
		ID: message.ID, AccountID: message.CookieID, ChatID: message.ChatID, MessageKey: message.MessageKey,
		Direction: message.Direction, SenderID: message.SenderID, SenderName: message.SenderName,
		MessageType: message.MessageType, Content: message.Content, Status: message.Status,
		ReadStatus: message.ReadStatus, ReadAt: message.ReadAt, SentAt: message.SentAt,
	}
}

// chatSenderProvider 将账号管理器适配为聊天应用的在线发送端口。
type chatSenderProvider struct {
	// manager 保存当前进程内的账号运行时管理器。
	manager *account.Manager
}

// NewChatSenderProvider 创建按账号解析在线发送器的适配器。
func NewChatSenderProvider(manager *account.Manager) chatapp.SenderProvider {
	return chatSenderProvider{manager: manager}
}

// Sender 返回指定账号的在线发送能力；账号不存在或运行时未装配时返回 false。
func (p chatSenderProvider) Sender(accountID string) (chatapp.Sender, bool) {
	if p.manager == nil {
		return nil, false
	}
	// sender、ok 保存账号管理器返回的运行时发送器及存在性。
	sender, ok := p.manager.GetInstance(accountID)
	if !ok || sender == nil {
		return nil, false
	}
	return chatSender{sender: sender}, true
}

// chatSender 将自动化消息发送接口收敛为应用聊天端口，并注入幂等键上下文。
type chatSender struct {
	// sender 保存账号运行时提供的最小消息发送能力。
	sender automation.MessageSender
}

// SendText 发送文字并将应用层幂等键传递给运行时旁路观察器。
func (s chatSender) SendText(ctx context.Context, chatID, toUserID, text, messageKey string) error {
	if s.sender == nil {
		return chatapp.ErrUnavailable
	}
	return s.sender.SendText(engine.WithOutgoingMessageKey(ctx, messageKey), chatID, toUserID, text)
}

// SendImage 发送图片并将应用层幂等键传递给运行时接口。
func (s chatSender) SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64, messageKey string) error {
	if s.sender == nil {
		return chatapp.ErrUnavailable
	}
	return s.sender.SendImage(engine.WithOutgoingMessageKey(ctx, messageKey), chatID, toUserID, imageURL, cardID)
}

// chatCredentialRepository 将 Cookie 读取与写回限制在平台适配器内。
type chatCredentialRepository struct {
	// store 保存数据库聚合入口，仅在适配器内读取和更新明文 Cookie。
	store *db.Store
}

// getCookieValue 读取图片上传所需的账号凭证；调用方不得记录返回值。
func (r chatCredentialRepository) getCookieValue(ctx context.Context, accountID string) (string, error) {
	if r.store == nil || r.store.Cookies == nil {
		return "", chatapp.ErrUnavailable
	}
	return r.store.Cookies.GetValue(ctx, accountID)
}

// updateCookieValue 保存图片上传后平台刷新的账号凭证。
func (r chatCredentialRepository) updateCookieValue(ctx context.Context, accountID, cookieValue string) error {
	if r.store == nil || r.store.Cookies == nil {
		return chatapp.ErrUnavailable
	}
	return r.store.Cookies.UpdateValueExisting(ctx, accountID, cookieValue)
}

// chatImageUploader 将 MTOP 图片上传和凭证刷新适配为聊天应用端口。
type chatImageUploader struct {
	// clientProvider 返回当前可注入的 MTOP 客户端，便于运行时替换和测试。
	clientProvider func() mtop.Client
	// credentials 负责在平台刷新后持久化明文凭证，但不向应用层返回。
	credentials chatCredentialRepository
	// manager 负责将刷新后的凭证同步到在线运行时。
	manager *account.Manager
}

// NewChatImageUploader 创建聊天图片上传适配器；平台客户端和数据库依赖由调用方注入。
func NewChatImageUploader(store *db.Store, clientProvider func() mtop.Client, manager *account.Manager) chatapp.ImageUploader {
	return chatImageUploader{clientProvider: clientProvider, credentials: chatCredentialRepository{store: store}, manager: manager}
}

// UploadChatImage 在适配器内部读取和刷新凭证，只向应用层返回图片地址。
func (u chatImageUploader) UploadChatImage(ctx context.Context, accountID, filename, contentType string, data []byte) (chatapp.ImageUpload, error) {
	if u.clientProvider == nil {
		return chatapp.ImageUpload{}, chatapp.ErrUnavailable
	}
	// cookieValue 和 err 保存平台调用所需的短暂明文凭证及读取错误，不得离开适配器。
	cookieValue, err := u.credentials.getCookieValue(ctx, accountID)
	if err != nil {
		return chatapp.ImageUpload{}, err
	}
	// client 保存当前可用的 MTOP 客户端。
	client := u.clientProvider()
	// uploader、ok 保存 MTOP 图片上传能力及接口支持情况。
	uploader, ok := client.(interface {
		UploadChatImage(context.Context, string, string, string, []byte) (*mtop.ChatImageUpload, error)
	})
	if !ok {
		return chatapp.ImageUpload{}, chatapp.ErrUnavailable
	}
	// upload、err 保存图片平台返回结果及调用错误。
	upload, err := uploader.UploadChatImage(ctx, cookieValue, filename, contentType, data)
	if err != nil {
		return chatapp.ImageUpload{}, err
	}
	if upload == nil {
		return chatapp.ImageUpload{}, chatapp.ErrSend
	}
	if upload.UpdatedCookies != "" && upload.UpdatedCookies != cookieValue {
		// persistErr 保存刷新凭证的持久化错误；该错误必须反馈给调用方，避免静默丢失会话状态。
		if persistErr := u.credentials.updateCookieValue(ctx, accountID, upload.UpdatedCookies); persistErr != nil {
			return chatapp.ImageUpload{}, persistErr
		}
		if u.manager != nil {
			// sender、senderOK 保存刷新凭证同步到运行时的结果。
			sender, senderOK := u.manager.GetInstance(accountID)
			if senderOK && sender != nil {
				sender.UpdateCookie(upload.UpdatedCookies)
			}
		}
	}
	return chatapp.ImageUpload{URL: upload.URL}, nil
}

// 编译期确认聊天实时适配器覆盖应用层定义的全部能力。
var (
	_ chatapp.OutgoingRepository = chatOutgoingRepository{}
	_ chatapp.SenderProvider     = chatSenderProvider{}
	_ chatapp.Sender             = chatSender{}
	_ chatapp.ImageUploader      = chatImageUploader{}
)
