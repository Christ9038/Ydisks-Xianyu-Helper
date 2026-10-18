package adapter

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/account"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
	domainchat "xianyu-go/internal/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/ws"
)

// chatOutgoingRepository 将聊天领域服务适配为应用层外发消息端口。
type chatOutgoingRepository struct {
	// service 保存聊天消息幂等写入和实时事件发布能力。
	service *domainchat.Service
}

// NewChatSendingApplication 装配聊天历史、实时发送、图片上传和身份补全端口。
func NewChatSendingApplication(domainService *domainchat.Service, store *db.Store, manager *account.Manager, clientProvider func() mtop.Client) *chatapp.Service {
	// readReporter 保存从账号运行时适配出的平台已读上报能力，未运行账号保持可选语义。
	readReporter := NewChatReadReporter(manager)
	if domainService == nil {
		// service 保留历史查询应用对象，但不伪造未装配的发送、订阅和刷新端口。
		// service 是仅保留可用读取能力的聊天应用服务。
		service := chatapp.NewWithSendingSubscriptionAndRefresh(NewChatRepository(store), nil, nil, nil, nil, nil, NewChatIdentityResolver(store, clientProvider, manager))
		return chatapp.WithPlatformReadReporter(chatapp.WithChatItemCatalog(service, NewChatItemCatalog(store, clientProvider, manager)), readReporter)
	}
	// service 是装配历史、发送、订阅、刷新和身份能力的聊天应用服务。
	service := chatapp.NewWithReplyImageSources(
		NewChatRepository(store),
		NewChatOutgoingRepository(domainService),
		NewChatSenderProvider(manager),
		NewChatImageUploader(store, clientProvider, manager),
		NewChatSubscriptionProvider(domainService),
		NewChatRefreshProvider(domainService, manager),
		downloadAutomationImage,
		newLocalReplyImageLoader(defaultReplyImageRoot()),
		NewChatIdentityResolver(store, clientProvider, manager),
	)
	return chatapp.WithPlatformReadReporter(chatapp.WithChatItemCatalog(service, NewChatItemCatalog(store, clientProvider, manager)), readReporter)
}

// chatReadReporter 将账号运行时的可选已读上报能力适配为聊天应用端口。
type chatReadReporter struct {
	// manager 保存当前进程的账号运行时查询入口。
	manager *account.Manager
}

// NewChatReadReporter 创建聊天已读上报端口；nil Manager 表示当前进程不提供运行时上报能力。
func NewChatReadReporter(manager *account.Manager) chatapp.PlatformReadReporter {
	return chatReadReporter{manager: manager}
}

// ReportRead 向在线账号实例尽力上报平台已读状态；不支持该能力时保持无副作用。
func (reporter chatReadReporter) ReportRead(ctx context.Context, accountID, chatID string, messageIDs []map[string]any) error {
	if reporter.manager == nil {
		return nil
	}
	// sender、ok 保存账号运行实例与其在线状态。
	sender, ok := reporter.manager.GetInstance(accountID)
	if !ok || sender == nil {
		return nil
	}
	// reader、ok 保存运行时是否支持平台已读上报。
	reader, ok := sender.(interface {
		MarkChatRead(context.Context, string, []map[string]any) error
	})
	if !ok {
		return nil
	}
	return reader.MarkChatRead(ctx, chatID, messageIDs)
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
		CookieID: session.AccountID, ChatID: session.ChatID, BuyerID: session.PeerUserID,
		BuyerName: session.PeerName, BuyerAvatar: session.PeerAvatar, AccountRole: session.AccountRole, BuyerUserID: session.BuyerUserID, SellerUserID: session.SellerUserID, RoleItemID: session.RoleItemID, RoleSource: session.RoleSource, ItemID: session.ItemID,
		ItemTitle: session.ItemTitle, ItemImageURL: session.ItemImageURL, LastMessage: session.LastMessage, LastMessageAt: session.LastMessageAt,
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
		MessageType: message.MessageType, Content: message.Content, MediaDuration: message.MediaDuration, Status: message.Status,
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
	return classifyChatPlatformError(s.sender.SendText(engine.WithOutgoingMessageKey(ctx, messageKey), chatID, toUserID, text))
}

// SendImage 发送图片并将应用层幂等键传递给运行时接口。
func (s chatSender) SendImage(ctx context.Context, chatID, toUserID, imageURL string, cardID int64, width, height int, messageKey string) error {
	if s.sender == nil {
		return chatapp.ErrUnavailable
	}
	return classifyChatPlatformError(s.sender.SendImage(engine.WithOutgoingMessageKey(ctx, messageKey), chatID, toUserID, imageURL, cardID, width, height))
}

// SendItemCard 将应用层商品快照交给账号运行时的可选卡片发送能力。
func (s chatSender) SendItemCard(ctx context.Context, chatID, toUserID string, item chatapp.ChatItem, messageKey string) error {
	if s.sender == nil {
		return chatapp.ErrUnavailable
	}
	// itemSender 和 ok 是账号运行时是否实现了不扩大 automation.MessageSender 的商品卡片窄能力。
	itemSender, ok := s.sender.(interface {
		SendItemCard(context.Context, string, string, string, string, string, string) error
	})
	if !ok {
		return chatapp.ErrUnavailable
	}
	return classifyChatPlatformError(itemSender.SendItemCard(engine.WithOutgoingMessageKey(ctx, messageKey), chatID, toUserID, item.ItemID, item.Title, item.ImageURL, item.Price))
}

// chatReplyDelivery 将引擎生成的完整回复转交给聊天应用服务，不自行上传图片或拼装协议尺寸。
type chatReplyDelivery struct {
	// service 保存消息页面使用的统一聊天应用服务。
	service *chatapp.Service
}

// NewChatReplyDelivery 创建自动回复使用的聊天应用发送端口。
func NewChatReplyDelivery(service *chatapp.Service) engine.ReplyDelivery {
	return chatReplyDelivery{service: service}
}

// SendReply 通过消息页面的完整回复入口发送图片和文字，并返回一次性状态所需的分段结果。
func (d chatReplyDelivery) SendReply(ctx context.Context, message engine.ReplyMessage) (engine.ReplySendResult, error) {
	if d.service == nil {
		return engine.ReplySendResult{}, chatapp.ErrUnavailable
	}
	// sent、sendErr 保存聊天应用统一发送结果及错误。
	sent, sendErr := d.service.SendReply(ctx, chatapp.ReplyInput{
		Session: chatapp.Session{AccountID: message.AccountID, ChatID: message.ChatID, PeerUserID: message.ToUserID},
		Text:    message.Text, ImageURL: message.ImageURL, ImagePath: message.ImagePath,
	})
	// result 保存引擎只关心的分段确认和未知结果标记，不泄露应用层消息模型。
	result := engine.ReplySendResult{}
	if sent != nil {
		result.ImageSent = sent.ImageSent
		result.TextSent = sent.TextSent
	}
	result.Uncertain = errors.Is(sendErr, chatapp.ErrSendUncertain) || errors.Is(sendErr, chatapp.ErrStatusSave)
	return result, sendErr
}

// classifyChatPlatformError 将平台无法确认的发送结果映射为聊天应用错误；不会触发凭证恢复或重发。
func classifyChatPlatformError(err error) error {
	if err == nil {
		return nil
	}
	// 确定未发送的错误发生在平台动作不可回收之前，必须保留可安全重试语义。
	if errors.Is(err, automation.ErrMessageNotSent) {
		return err
	}
	if ws.SendResultKind(err) == ws.SendUncertain {
		return fmt.Errorf("%w: %v", chatapp.ErrSendUncertain, err)
	}
	return err
}

// chatCredentialRepository 将 Cookie 读取与写回限制在平台适配器内。
type chatCredentialRepository struct {
	// store 保存数据库聚合入口，仅在适配器内读取和更新明文 Cookie。
	store *db.Store
}

// errChatCredentialChanged 表示平台调用期间账号凭证已由其他请求更新，旧请求不得覆盖新状态。
var errChatCredentialChanged = errors.New("账号凭证已变化，请重试")

// chatItemCatalog 将 MTOP 个人会话商品查询适配为应用层非敏感端口。
type chatItemCatalog struct {
	// clientProvider 返回当前可用的 MTOP 客户端。
	clientProvider func() mtop.Client
	// credentials 在本适配器内限定凭证读取和写回。
	credentials chatCredentialRepository
	// manager 用于将平台刷新后的 Cookie 同步到在线运行时。
	manager *account.Manager
}

// NewChatItemCatalog 创建聊天商品查询适配器。
func NewChatItemCatalog(store *db.Store, clientProvider func() mtop.Client, manager *account.Manager) chatapp.ChatItemCatalog {
	if store == nil || clientProvider == nil {
		return nil
	}
	return chatItemCatalog{clientProvider: clientProvider, credentials: chatCredentialRepository{store: store}, manager: manager}
}

// ListChatItems 在适配器内使用凭证调用 MTOP，并只返回非敏感商品字段。
func (catalog chatItemCatalog) ListChatItems(ctx context.Context, accountID, chatID, role, query string, page int) (chatapp.ChatItemPage, error) {
	if catalog.credentials.store == nil || catalog.credentials.store.Cookies == nil {
		return chatapp.ChatItemPage{}, chatapp.ErrUnavailable
	}
	// credentialUnlock 保护请求前凭证快照读取；平台 I/O 开始前必须释放。
	credentialUnlock := catalog.credentials.store.LockAccountCredentials(accountID)
	// initial 和 credentialErr 是本次平台请求使用的凭证与元数据快照，不得写入日志或响应。
	initial, credentialErr := catalog.credentials.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	if credentialErr != nil {
		credentialUnlock()
		return chatapp.ChatItemPage{}, credentialErr
	}
	if !hasStoredCredential(initial) {
		credentialUnlock()
		return chatapp.ChatItemPage{}, errors.New("账号凭证不可用")
	}
	// requestContext 和 cookieSession 保存支持完整 Cookie Jar 更新的请求上下文与会话。
	requestContext, cookieSession := withCookieSnapshot(ctx, initial)
	credentialUnlock()
	// client 是当前注入的 MTOP 客户端实例。
	client := catalog.clientProvider()
	// searcher 和 ok 是客户端是否支持聊天商品查询的可选能力。
	searcher, ok := client.(interface {
		SearchChatItems(context.Context, string, string, string, string, int) (*mtop.ChatItemPage, error)
	})
	if !ok {
		return chatapp.ChatItemPage{}, chatapp.ErrChatItemUnavailable
	}
	// platformRole 是前端语义角色映射后的官网 searchItemRole。
	platformRole := "other"
	if role == "self" {
		platformRole = "own"
	}
	// result 和 searchErr 是 MTOP 查询结果及平台错误。
	result, searchErr := searcher.SearchChatItems(requestContext, initial.Value, chatID, platformRole, query, page)
	// runtimeCookie、syncRuntime 和 persistErr 是通过凭证指纹复核后允许同步到在线实例的 Cookie、同步标记及写回错误。
	runtimeCookie, syncRuntime, persistErr := catalog.persistCookieSession(ctx, initial, cookieSession, result)
	if persistErr != nil {
		if searchErr != nil {
			return chatapp.ChatItemPage{}, errors.Join(searchErr, persistErr)
		}
		return chatapp.ChatItemPage{}, persistErr
	}
	if syncRuntime {
		catalog.updateRuntimeCookie(ctx, accountID, runtimeCookie)
	}
	if searchErr != nil {
		return chatapp.ChatItemPage{}, searchErr
	}
	if result == nil {
		return chatapp.ChatItemPage{}, chatapp.ErrChatItemUnavailable
	}
	// items 是转换后的应用层非敏感商品集合。
	items := make([]chatapp.ChatItem, 0, len(result.Items))
	for _ /* item 是当前待转换为应用层非敏感模型的平台商品。 */, item := range result.Items {
		items = append(items, chatapp.ChatItem{ItemID: item.ItemID, Title: item.Title, ImageURL: item.ImageURL, Price: item.Price, Description: item.Description})
	}
	return chatapp.ChatItemPage{Items: items, Page: result.Page, HasMore: result.HasMore}, nil
}

// persistCookieSession 在平台调用后重新加锁复核凭证指纹，只提交基于同一快照产生的 Cookie 变化。
// 第二个返回值表示完整凭证状态已经写入，调用方即使拿到空或未变化的扁平值也必须让运行时复读数据库元数据。
func (catalog chatItemCatalog) persistCookieSession(ctx context.Context, initial db.CookiePlatformRuntimeData, session *mtop.CookieSession, result *mtop.ChatItemPage) (string, bool, error) {
	// updatedCookies 保存旧版商品查询接口可能单独返回的扁平 Cookie。
	updatedCookies := ""
	if result != nil {
		updatedCookies = result.UpdatedCookies
	}
	return catalog.credentials.persistCookieSession(ctx, initial, session, updatedCookies)
}

// persistCookieSession 在平台调用后重新加锁复核凭证指纹，只提交基于同一快照产生的 Cookie 变化。
// updatedCookies 仅用于兼容不提供完整会话快照的旧平台客户端。
func (repository chatCredentialRepository) persistCookieSession(ctx context.Context, initial db.CookiePlatformRuntimeData, session *mtop.CookieSession, updatedCookies string) (string, bool, error) {
	// unlock 保护最新凭证复核和条件写回，锁内不执行平台 I/O。
	if repository.store == nil || repository.store.Cookies == nil {
		return "", false, chatapp.ErrUnavailable
	}
	// unlock 保护最新凭证复核和条件写回，锁内不执行平台 I/O。
	unlock := repository.store.LockAccountCredentials(initial.ID)
	defer unlock()
	// latest 和 loadErr 是平台调用结束后的当前凭证视图。
	latest, loadErr := repository.store.Cookies.GetCookiePlatformRuntimeData(ctx, initial.ID)
	if loadErr != nil {
		return "", false, fmt.Errorf("读取聊天商品凭证失败: %w", loadErr)
	}
	if latest.UserID != initial.UserID || latest.Value != initial.Value || latest.MetadataJSON != initial.MetadataJSON {
		return "", false, errChatCredentialChanged
	}
	if session != nil {
		// value、snapshot 和 changed 是请求会话吸收全部 Set-Cookie 后的状态。
		value, snapshot, changed := session.State()
		if changed {
			// metadata 保留原有非 Cookie 元数据，并仅在权威会话存在时写回完整快照。
			metadata := cookierefresh.MetadataWithoutSnapshot(latest.MetadataJSON)
			if snapshot != nil {
				metadata = cookierefresh.MetadataWithSnapshot(latest.MetadataJSON, snapshot)
			}
			// persistErr 是完整 Cookie 会话发生变化时的条件持久化结果。
			if persistErr := repository.store.Cookies.UpdateRenewalCookie(ctx, latest.ID, value, metadata, time.Now().Unix()); persistErr != nil {
				return "", false, fmt.Errorf("保存聊天商品查询响应 Cookie: %w", persistErr)
			}
			return value, true, nil
		}
		if snapshot != nil {
			return "", false, nil
		}
	}
	if strings.TrimSpace(updatedCookies) != "" && updatedCookies != latest.Value {
		// metadata 去除可能损坏或不完整的历史快照；平面兼容响应不能冒充完整 Cookie Jar。
		metadata := cookierefresh.MetadataWithoutSnapshot(latest.MetadataJSON)
		// persistErr 是兼容旧 MTOP 客户端平面 Cookie 响应的持久化结果。
		if persistErr := repository.store.Cookies.UpdateRenewalCookie(ctx, latest.ID, updatedCookies, metadata, time.Now().Unix()); persistErr != nil {
			return "", false, fmt.Errorf("保存聊天商品查询响应 Cookie: %w", persistErr)
		}
		return updatedCookies, true, nil
	}
	return "", false, nil
}

// updateRuntimeCookie 将已经持久化且通过指纹复核的 Cookie 同步到可选在线账号实例。
func (catalog chatItemCatalog) updateRuntimeCookie(ctx context.Context, accountID, value string) {
	if catalog.manager == nil {
		return
	}
	// runtime 和 runtimeOK 是当前账号可选的在线运行时。
	runtime, runtimeOK := catalog.manager.GetInstance(accountID)
	if !runtimeOK || runtime == nil {
		return
	}
	// contextualUpdater 优先保留调用方取消边界；旧运行时仍使用有界兼容入口。
	if contextualUpdater, supported := runtime.(interface {
		UpdateCookieContext(context.Context, string) error
	}); supported {
		_ = contextualUpdater.UpdateCookieContext(ctx, value)
		return
	}
	runtime.UpdateCookie(value)
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
	if u.clientProvider == nil || u.credentials.store == nil || u.credentials.store.Cookies == nil {
		return chatapp.ImageUpload{}, chatapp.ErrUnavailable
	}
	// credentialUnlock 保护请求前凭证快照读取；平台 I/O 开始前必须释放。
	credentialUnlock := u.credentials.store.LockAccountCredentials(accountID)
	// initial 和 credentialErr 保存本次平台请求使用的凭证与元数据快照，不得写入日志或响应。
	initial, credentialErr := u.credentials.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	if credentialErr != nil {
		credentialUnlock()
		return chatapp.ImageUpload{}, credentialErr
	}
	if !hasStoredCredential(initial) {
		credentialUnlock()
		return chatapp.ImageUpload{}, errors.New("账号凭证不可用")
	}
	// requestContext 和 cookieSession 保存支持完整 Cookie Jar 更新的请求上下文与会话。
	requestContext, cookieSession := withCookieSnapshot(ctx, initial)
	credentialUnlock()
	// client 保存当前可用的 MTOP 客户端。
	client := u.clientProvider()
	// uploader、ok 保存 MTOP 图片上传能力及接口支持情况。
	uploader, ok := client.(interface {
		UploadChatImage(context.Context, string, string, string, []byte) (*mtop.ChatImageUpload, error)
	})
	if !ok {
		return chatapp.ImageUpload{}, chatapp.ErrUnavailable
	}
	// upload、uploadErr 保存图片平台返回结果及调用错误。
	upload, uploadErr := uploader.UploadChatImage(requestContext, initial.Value, filename, contentType, data)
	if uploadErr != nil {
		return chatapp.ImageUpload{}, uploadErr
	}
	if upload == nil {
		return chatapp.ImageUpload{}, chatapp.ErrSend
	}
	// runtimeCookie、syncRuntime 和 persistErr 是通过凭证指纹复核后允许同步到在线实例的 Cookie、同步标记及写回错误。
	runtimeCookie, syncRuntime, persistErr := u.credentials.persistCookieSession(ctx, initial, cookieSession, upload.UpdatedCookies)
	if persistErr != nil {
		return chatapp.ImageUpload{}, persistErr
	}
	if syncRuntime && u.manager != nil {
		// sender、senderOK 保存刷新凭证同步到运行时的结果。
		sender, senderOK := u.manager.GetInstance(accountID)
		if senderOK && sender != nil {
			sender.UpdateCookie(runtimeCookie)
		}
	}
	return chatapp.ImageUpload{URL: upload.URL, Width: upload.Width, Height: upload.Height}, nil
}

// 编译期确认聊天实时适配器覆盖应用层定义的全部能力。
var (
	_ chatapp.OutgoingRepository = chatOutgoingRepository{}
	_ chatapp.SenderProvider     = chatSenderProvider{}
	_ chatapp.Sender             = chatSender{}
	_ chatapp.ImageUploader      = chatImageUploader{}
	_ chatapp.ChatItemCatalog    = chatItemCatalog{}
	_ engine.ReplyDelivery       = chatReplyDelivery{}
)
