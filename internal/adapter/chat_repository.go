package adapter

import (
	"context"
	"errors"

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
			MessageType: row.MessageType, Content: row.Content, Status: row.Status,
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
			AccountID: row.CookieID, ChatID: row.ChatID, BuyerID: row.BuyerID,
			BuyerName: row.BuyerName, BuyerAvatar: row.BuyerAvatar, ItemID: row.ItemID,
			ItemTitle: row.ItemTitle, LastMessage: row.LastMessage, LastMessageAt: row.LastMessageAt,
			UnreadCount: row.UnreadCount,
		})
	}
	return sessions, nil
}

// DeleteEmptySessions 删除没有有效消息的聊天会话壳。
func (r chatRepository) DeleteEmptySessions(ctx context.Context, accountID string) error {
	return r.store.Chats.DeleteEmptySessions(ctx, accountID)
}

// UpdateSessionIdentity 更新会话的买家身份缓存。
func (r chatRepository) UpdateSessionIdentity(ctx context.Context, accountID, chatID, buyerID, buyerName, buyerAvatar string) error {
	return r.store.Chats.UpdateSessionIdentity(ctx, accountID, chatID, buyerID, buyerName, buyerAvatar)
}

// ExistsOwned 判断账号是否归属于指定用户，只返回非敏感存在性。
func (r chatRepository) ExistsOwned(ctx context.Context, userID int64, accountID string) (bool, error) {
	return r.store.Cookies.ExistsOwned(ctx, userID, accountID)
}

// MarkRead 将用户拥有的聊天会话未读数归零，不读取或解密账号凭证。
func (r chatRepository) MarkRead(ctx context.Context, userID int64, accountID, chatID string) error {
	return r.store.Chats.MarkRead(ctx, userID, accountID, chatID)
}

// FindInboundParsedJSONContaining 提供旧版聊天消息标识迁移所需的受限诊断帧查询。
func (r chatRepository) FindInboundParsedJSONContaining(ctx context.Context, accountID, fragment string, limit int) ([]string, error) {
	if r.store == nil || r.store.WSMessages == nil {
		return nil, errors.New("聊天诊断存储未初始化")
	}
	return r.store.WSMessages.FindInboundParsedJSONContaining(ctx, accountID, fragment, limit)
}

// chatIdentityResolver 在适配器内读取 Cookie 并调用平台身份查询接口。
type chatIdentityResolver struct {
	// store 提供账号凭证读取能力，明文只在本次平台调用期间存在。
	store *db.Store
	// clientProvider 返回当前可注入的 MTOP 客户端，便于运行时替换和测试。
	clientProvider func() mtop.Client
}

// NewChatIdentityResolver 创建聊天身份查询适配器。
func NewChatIdentityResolver(store *db.Store, clientProvider func() mtop.Client) chatapp.IdentityResolver {
	if store == nil || store.Cookies == nil || clientProvider == nil {
		return nil
	}
	return chatIdentityResolver{store: store, clientProvider: clientProvider}
}

// Resolve 查询聊天买家展示身份；Cookie 和平台客户端均不会离开适配器。
func (r chatIdentityResolver) Resolve(ctx context.Context, accountID, chatID string) (chatapp.Identity, error) {
	// cookies 和 err 保存平台调用需要的短暂凭证及读取错误，不得写入日志或响应。
	cookies, err := r.store.Cookies.GetValue(ctx, accountID)
	if err != nil {
		return chatapp.Identity{}, err
	}
	// client 保存当前 MTOP 客户端。
	client := r.clientProvider()
	// fetcher 和 supported 保存身份查询能力及接口支持情况。
	fetcher, supported := client.(interface {
		FetchChatUserInfo(context.Context, string, string) (*mtop.ChatUserInfo, error)
	})
	if !supported {
		return chatapp.Identity{}, errors.New("当前 MTOP 客户端不支持聊天身份查询")
	}
	// info 和 err 保存平台返回的非敏感身份及查询错误。
	info, err := fetcher.FetchChatUserInfo(ctx, cookies, chatID)
	if err != nil {
		return chatapp.Identity{}, err
	}
	if info == nil {
		return chatapp.Identity{}, nil
	}
	return chatapp.Identity{BuyerName: info.Nickname, BuyerAvatar: info.AvatarURL}, nil
}

// 确保数据库聊天适配器覆盖应用层会话端口的全部能力。
var _ chatapp.SessionRepository = chatRepository{}

// 确保聊天身份适配器覆盖应用层平台身份端口。
var _ chatapp.IdentityResolver = chatIdentityResolver{}
