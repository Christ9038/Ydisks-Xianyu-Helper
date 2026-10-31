package adapter

import (
	"context"
	"errors"

	"xianyu-go/internal/account"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// chatRepository 将数据库聊天仓储和账号归属查询适配为聊天应用端口。
type chatRepository struct {
	// store 保存数据库聚合入口，仅在本适配器内执行聊天窄查询。
	store *db.Store
}

// NewChatRepository 创建聊天历史应用服务使用的数据库适配器。
func NewChatRepository(store *db.Store) chatapp.Repository {
	if store == nil || store.Chats == nil || store.Cookies == nil {
		return nil
	}
	return chatRepository{store: store}
}

// ListMessages 查询带用户归属条件的聊天消息，并转换为应用层模型。
func (r chatRepository) ListMessages(ctx context.Context, userID int64, accountID, chatID string, beforeID int64, limit int) ([]chatapp.Message, error) {
	// rows 和 err 保存数据库消息记录及查询错误。
	rows, err := r.store.Chats.ListMessages(ctx, userID, accountID, chatID, beforeID, limit)
	if err != nil {
		return nil, err
	}
	// messages 保存脱离数据库模型的应用层消息。
	messages := make([]chatapp.Message, 0, len(rows))
	// row 表示当前待转换的数据库聊天消息。
	for _, row := range rows {
		messages = append(messages, chatapp.Message{
			ID: row.ID, AccountID: row.CookieID, ChatID: row.ChatID, MessageKey: row.MessageKey,
			Direction: row.Direction, SenderID: row.SenderID, SenderName: row.SenderName,
			MessageType: row.MessageType, Content: row.Content, MediaDuration: row.MediaDuration, Status: row.Status,
			ReadStatus: row.ReadStatus, ReadAt: row.ReadAt, SentAt: row.SentAt,
		})
	}
	return messages, nil
}

// ListSessions 查询带用户归属条件的聊天会话，并转换为应用层模型。
func (r chatRepository) ListSessions(ctx context.Context, userID int64, accountID string, limit int) ([]chatapp.Session, error) {
	// rows 和 err 保存数据库会话记录及查询错误。
	rows, err := r.store.Chats.ListSessions(ctx, userID, accountID, limit)
	if err != nil {
		return nil, err
	}
	// sessions 保存脱离数据库模型的应用层会话摘要。
	sessions := make([]chatapp.Session, 0, len(rows))
	// row 表示当前待转换的数据库聊天会话。
	for _, row := range rows {
		sessions = append(sessions, chatapp.Session{
			AccountID: row.CookieID, ChatID: row.ChatID, PeerUserID: row.BuyerID,
			PeerName: row.BuyerName, PeerAvatar: row.BuyerAvatar, AccountRole: row.AccountRole,
			BuyerUserID: row.BuyerUserID, SellerUserID: row.SellerUserID, RoleItemID: row.RoleItemID, RoleSource: row.RoleSource, ItemID: row.ItemID,
			ItemTitle: row.ItemTitle, ItemImageURL: row.ItemImageURL, LastMessage: row.LastMessage, LastMessageAt: row.LastMessageAt,
			UnreadCount: row.UnreadCount,
		})
	}
	return sessions, nil
}

// FindSession 按用户归属精确读取一条可见会话并转换为应用模型。
func (r chatRepository) FindSession(ctx context.Context, userID int64, accountID, chatID string) (chatapp.Session, error) {
	// row 和 err 是底层按归属精确查询的会话记录及错误。
	row, err := r.store.Chats.FindSession(ctx, userID, accountID, chatID)
	if err != nil {
		if errors.Is(err, db.ErrNotFound) {
			return chatapp.Session{}, chatapp.ErrChatSessionNotFound
		}
		return chatapp.Session{}, err
	}
	return chatapp.Session{AccountID: row.CookieID, ChatID: row.ChatID, PeerUserID: row.BuyerID, PeerName: row.BuyerName,
		PeerAvatar: row.BuyerAvatar, AccountRole: row.AccountRole, BuyerUserID: row.BuyerUserID, SellerUserID: row.SellerUserID,
		RoleItemID: row.RoleItemID, RoleSource: row.RoleSource, ItemID: row.ItemID, ItemTitle: row.ItemTitle, ItemImageURL: row.ItemImageURL,
		LastMessage: row.LastMessage, LastMessageAt: row.LastMessageAt, UnreadCount: row.UnreadCount}, nil
}

// ListSessionPage 查询带用户归属条件的本地会话键集分页，并转换为应用层模型。
func (r chatRepository) ListSessionPage(ctx context.Context, userID int64, accountID string, cursor *chatapp.SessionCursor, limit int) (chatapp.SessionPage, error) {
	// databaseCursor 保存转换为数据库排序键后的分页位置；空指针表示读取首页。
	var databaseCursor *db.ChatSessionCursor
	if cursor != nil {
		databaseCursor = &db.ChatSessionCursor{LastMessageAt: cursor.LastMessageAt, ChatID: cursor.ChatID}
	}
	// databasePage 和 pageErr 保存数据库分页结果及查询错误。
	databasePage, pageErr := r.store.Chats.ListSessionPage(ctx, userID, accountID, databaseCursor, limit)
	if pageErr != nil {
		return chatapp.SessionPage{}, pageErr
	}
	// sessions 保存脱离数据库模型的应用层会话摘要。
	sessions := make([]chatapp.Session, 0, len(databasePage.Sessions))
	// row 表示当前待转换的数据库聊天会话。
	for _, row := range databasePage.Sessions {
		sessions = append(sessions, chatapp.Session{
			AccountID: row.CookieID, ChatID: row.ChatID, PeerUserID: row.BuyerID,
			PeerName: row.BuyerName, PeerAvatar: row.BuyerAvatar, AccountRole: row.AccountRole,
			BuyerUserID: row.BuyerUserID, SellerUserID: row.SellerUserID, RoleItemID: row.RoleItemID, RoleSource: row.RoleSource, ItemID: row.ItemID,
			ItemTitle: row.ItemTitle, ItemImageURL: row.ItemImageURL, LastMessage: row.LastMessage, LastMessageAt: row.LastMessageAt,
			UnreadCount: row.UnreadCount,
		})
	}
	// nextCursor 保存转换后的下一页应用层排序键；末页保持 nil。
	var nextCursor *chatapp.SessionCursor
	if databasePage.NextCursor != nil {
		nextCursor = &chatapp.SessionCursor{LastMessageAt: databasePage.NextCursor.LastMessageAt, ChatID: databasePage.NextCursor.ChatID}
	}
	return chatapp.SessionPage{Sessions: sessions, HasMore: databasePage.HasMore, NextCursor: nextCursor}, nil
}

// DeleteEmptySessions 删除没有有效消息的聊天会话壳。
func (r chatRepository) DeleteEmptySessions(ctx context.Context, accountID string) error {
	return r.store.Chats.DeleteEmptySessions(ctx, accountID)
}

// UpdateSessionIdentity 更新会话对端的展示身份缓存；底层历史列名不进入应用契约。
func (r chatRepository) UpdateSessionIdentity(ctx context.Context, accountID, chatID, peerUserID, peerName, peerAvatar string) error {
	return r.store.Chats.UpdateSessionIdentity(ctx, accountID, chatID, peerUserID, peerName, peerAvatar)
}

// ExistsOwned 判断账号是否归属于指定用户，只返回非敏感存在性。
func (r chatRepository) ExistsOwned(ctx context.Context, userID int64, accountID string) (bool, error) {
	return r.store.Cookies.ExistsOwned(ctx, userID, accountID)
}

// MarkRead 将用户拥有的聊天会话未读数归零，不读取或解密账号凭证。
func (r chatRepository) MarkRead(ctx context.Context, userID int64, accountID, chatID string) error {
	return r.store.Chats.MarkRead(ctx, userID, accountID, chatID)
}

// HideAndClearSession 原子隐藏当前用户的会话并清空聊天页消息，保留自动化仍需使用的会话实体。
func (r chatRepository) HideAndClearSession(ctx context.Context, userID int64, accountID, chatID string, clearedAt int64) (bool, error) {
	return r.store.Chats.HideAndClearSession(ctx, userID, accountID, chatID, clearedAt)
}

// FindInboundParsedJSONContaining 提供旧版聊天消息标识迁移所需的受限诊断帧查询。
func (r chatRepository) FindInboundParsedJSONContaining(ctx context.Context, accountID, fragment string, limit int) ([]string, error) {
	if r.store == nil || r.store.WSMessages == nil {
		return nil, errors.New("聊天诊断存储未初始化")
	}
	return r.store.WSMessages.FindInboundParsedJSONContaining(ctx, accountID, fragment, limit)
}

// ListQuickReplies 查询账号级人工快捷回复并转换为应用层非敏感模型。
func (r chatRepository) ListQuickReplies(ctx context.Context, accountID string) ([]chatapp.QuickReply, error) {
	// rows 和 listErr 保存数据库快捷回复记录及查询错误。
	rows, listErr := r.store.Chats.ListQuickReplies(ctx, accountID)
	if listErr != nil {
		return nil, listErr
	}
	// replies 保存脱离数据库模型的快捷回复列表。
	replies := make([]chatapp.QuickReply, 0, len(rows))
	// row 表示当前待转换的快捷回复持久化记录。
	for _, row := range rows {
		replies = append(replies, chatapp.QuickReply{ID: row.ID, AccountID: row.CookieID, Content: row.Content, LibraryImageID: row.LibraryImageID, CreatedAt: row.CreatedAt})
	}
	return replies, nil
}

// CreateQuickReply 写入账号级快捷回复并转换数据库上限错误。
func (r chatRepository) CreateQuickReply(ctx context.Context, accountID, content string) (chatapp.QuickReply, error) {
	// row 和 createErr 保存数据库写入结果及可能的数量上限错误。
	row, createErr := r.store.Chats.CreateQuickReply(ctx, accountID, content)
	if errors.Is(createErr, db.ErrChatQuickReplyLimitReached) {
		return chatapp.QuickReply{}, chatapp.ErrQuickReplyLimitReached
	}
	if createErr != nil {
		return chatapp.QuickReply{}, createErr
	}
	return chatapp.QuickReply{ID: row.ID, AccountID: row.CookieID, Content: row.Content, CreatedAt: row.CreatedAt}, nil
}

// CreateQuickReplyWithImage 写入文字或图片快捷回复；图片必须属于账号所有者，否则返回 ErrQuickReplyImageNotFound。
func (r chatRepository) CreateQuickReplyWithImage(ctx context.Context, accountID, content string, libraryImageID int64) (chatapp.QuickReply, error) {
	// owned、ownedErr 保存图片归属检查结果。
	owned, ownedErr := libraryImageOwnedByAccount(ctx, r.store, accountID, libraryImageID)
	if ownedErr != nil {
		return chatapp.QuickReply{}, ownedErr
	}
	if !owned {
		return chatapp.QuickReply{}, chatapp.ErrQuickReplyImageNotFound
	}
	// row 和 createErr 保存数据库写入结果及可能的数量上限错误。
	row, createErr := r.store.Chats.CreateQuickReplyWithImage(ctx, accountID, content, libraryImageID)
	if errors.Is(createErr, db.ErrChatQuickReplyLimitReached) {
		return chatapp.QuickReply{}, chatapp.ErrQuickReplyLimitReached
	}
	if createErr != nil {
		return chatapp.QuickReply{}, createErr
	}
	return chatapp.QuickReply{ID: row.ID, AccountID: row.CookieID, Content: row.Content, LibraryImageID: row.LibraryImageID, CreatedAt: row.CreatedAt}, nil
}

// DeleteQuickReply 删除账号下指定快捷回复并返回是否命中记录。
func (r chatRepository) DeleteQuickReply(ctx context.Context, accountID string, quickReplyID int64) (bool, error) {
	return r.store.Chats.DeleteQuickReply(ctx, accountID, quickReplyID)
}

// GetBuyerNote 读取账号和买家 ID 共同隔离的完整备注。
func (r chatRepository) GetBuyerNote(ctx context.Context, accountID, buyerID string) (chatapp.BuyerNote, bool, error) {
	// row、found 和 readErr 保存数据库读取结果、存在状态和错误。
	row, found, readErr := r.store.Chats.GetBuyerNote(ctx, accountID, buyerID)
	if readErr != nil || !found {
		return chatapp.BuyerNote{}, found, readErr
	}
	return chatapp.BuyerNote{AccountID: row.CookieID, BuyerID: row.BuyerID, Content: row.Content, UpdatedAt: row.UpdatedAt}, true, nil
}

// SaveBuyerNote 保存买家备注；空内容由数据库适配器转换为删除语义。
func (r chatRepository) SaveBuyerNote(ctx context.Context, note chatapp.BuyerNote) (chatapp.BuyerNote, error) {
	// row 和 saveErr 保存数据库写入后的备注及错误。
	row, saveErr := r.store.Chats.SaveBuyerNote(ctx, db.ChatBuyerNote{CookieID: note.AccountID, BuyerID: note.BuyerID, Content: note.Content})
	if saveErr != nil {
		return chatapp.BuyerNote{}, saveErr
	}
	return chatapp.BuyerNote{AccountID: row.CookieID, BuyerID: row.BuyerID, Content: row.Content, UpdatedAt: row.UpdatedAt}, nil
}

// chatIdentityResolver 在适配器内读取 Cookie 并调用平台身份查询接口。
type chatIdentityResolver struct {
	// store 提供账号凭证读取能力，明文只在本次平台调用期间存在。
	store *db.Store
	// clientProvider 返回当前可注入的 MTOP 客户端，便于运行时替换和测试。
	clientProvider func() mtop.Client
	// credentials 在适配器内完成响应 Cookie 的版本复核和持久化，不向应用层泄露凭证。
	credentials chatCredentialRepository
	// manager 用于把已经持久化的新 Cookie 同步到当前在线账号实例；为空时仅更新数据库。
	manager *account.Manager
}

// NewChatIdentityResolver 创建聊天身份查询适配器。
func NewChatIdentityResolver(store *db.Store, clientProvider func() mtop.Client, manager *account.Manager) chatapp.IdentityResolver {
	if store == nil || store.Cookies == nil || clientProvider == nil {
		return nil
	}
	return chatIdentityResolver{store: store, clientProvider: clientProvider, credentials: chatCredentialRepository{store: store}, manager: manager}
}

// Resolve 查询聊天对端展示身份；Cookie 和平台客户端均不会离开适配器。
func (r chatIdentityResolver) Resolve(ctx context.Context, accountID, chatID string) (chatapp.Identity, error) {
	if r.store == nil || r.store.Cookies == nil {
		return chatapp.Identity{}, chatapp.ErrUnavailable
	}
	// credentialUnlock 保护本次请求的权威凭证快照读取；平台 I/O 开始前必须释放。
	credentialUnlock := r.store.LockAccountCredentials(accountID)
	// initial 和 credentialErr 保存本次身份查询使用的 Cookie 与 metadata 快照；不得进入日志或应用层返回值。
	initial, credentialErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, accountID)
	if credentialErr != nil {
		credentialUnlock()
		return chatapp.Identity{}, credentialErr
	}
	if !hasStoredCredential(initial) {
		credentialUnlock()
		return chatapp.Identity{}, errors.New("账号凭证不可用")
	}
	// requestContext 和 cookieSession 吸收本次身份查询的全部响应 Cookie，包含 Token 过期响应中的换签 Cookie。
	requestContext, cookieSession := withCookieSnapshot(ctx, initial)
	credentialUnlock()
	// client 保存当前注入的 MTOP 客户端。
	client := r.clientProvider()
	// fetcher 和 supported 保存身份查询能力及接口支持情况。
	fetcher, supported := client.(interface {
		FetchChatUserInfo(context.Context, string, string) (*mtop.ChatUserInfo, error)
	})
	if !supported {
		return chatapp.Identity{}, errors.New("当前 MTOP 客户端不支持聊天身份查询")
	}
	// info 和 fetchErr 保存平台返回的展示身份与调用错误；即使 fetchErr 非空也必须先收口已收到的响应 Cookie。
	info, fetchErr := fetcher.FetchChatUserInfo(requestContext, initial.Value, chatID)
	// updatedCookies 兼容没有 CookieSession 的历史 MTOP 实现；当前实现优先从 cookieSession 读取完整 Cookie Jar。
	updatedCookies := ""
	if info != nil {
		updatedCookies = info.UpdatedCookies
	}
	// runtimeCookie、syncRuntime 和 persistErr 保存凭证版本复核后的写回结果；旧请求不得覆盖并发更新的新凭证。
	runtimeCookie, syncRuntime, persistErr := r.credentials.persistCookieSession(ctx, initial, cookieSession, updatedCookies)
	if errors.Is(persistErr, errChatCredentialChanged) {
		// 身份展示结果不依赖旧 Cookie；并发请求已经写入较新凭证时丢弃本次旧响应即可。
		persistErr = nil
	}
	if persistErr != nil {
		if fetchErr != nil {
			return chatapp.Identity{}, errors.Join(fetchErr, persistErr)
		}
		return chatapp.Identity{}, persistErr
	}
	if syncRuntime {
		r.updateRuntimeCookie(ctx, accountID, runtimeCookie)
	}
	if fetchErr != nil {
		return chatapp.Identity{}, fetchErr
	}
	if info == nil {
		return chatapp.Identity{}, nil
	}
	return chatapp.Identity{PeerName: info.Nickname, PeerAvatar: info.AvatarURL}, nil
}

// updateRuntimeCookie 将已完成版本复核并写入数据库的身份查询 Cookie 同步到在线账号实例。
func (r chatIdentityResolver) updateRuntimeCookie(ctx context.Context, accountID, value string) {
	if r.manager == nil || value == "" {
		return
	}
	// runtime 和 runtimeOK 保存当前账号实例及其存在性；账号未运行时数据库写回已经足够。
	runtime, runtimeOK := r.manager.GetInstance(accountID)
	if !runtimeOK || runtime == nil {
		return
	}
	// contextualUpdater 和 supported 优先让同步操作继承 HTTP 请求的取消边界。
	if contextualUpdater, supported := runtime.(contextualCookieUpdater); supported {
		_ = contextualUpdater.UpdateCookieContext(ctx, value)
		return
	}
	runtime.UpdateCookie(value)
}

// 确保数据库聊天适配器覆盖应用层会话端口的全部能力。
var _ chatapp.SessionRepository = chatRepository{}

// 确保数据库聊天适配器覆盖用户会话删除所需的归属与原子清空端口。
var _ chatapp.SessionDeletionRepository = chatRepository{}

// 确保数据库聊天适配器覆盖快捷回复和买家备注的应用层端口。
var _ chatapp.MetadataRepository = chatRepository{}

// 确保聊天身份适配器覆盖应用层平台身份端口。
var _ chatapp.IdentityResolver = chatIdentityResolver{}
