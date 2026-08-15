// Package chat 提供聊天历史等 HTTP 用例所需的应用层编排，不依赖 HTTP 或数据库模型。
package chat

import (
	"context"
	"errors"
	"strings"
)

// ErrInvalidInput 表示聊天历史查询缺少必要的非敏感标识。
var ErrInvalidInput = errors.New("聊天历史查询参数无效")

// Message 是聊天历史用例对外暴露的非敏感消息模型。
type Message struct {
	// ID 是本地消息主键。
	ID int64
	// AccountID 是消息所属账号标识，不包含账号凭证。
	AccountID string
	// ChatID 是平台聊天会话标识。
	ChatID string
	// MessageKey 是消息幂等键。
	MessageKey string
	// Direction 是消息方向，例如 incoming 或 outgoing。
	Direction string
	// SenderID 是平台发送者标识。
	SenderID string
	// SenderName 是发送者展示名称。
	SenderName string
	// MessageType 是消息内容类型。
	MessageType string
	// Content 是文本或媒体地址，不包含平台凭证。
	Content string
	// Status 是消息投递状态。
	Status string
	// SentAt 是消息发送时间的 Unix 秒时间戳。
	SentAt int64
}

// Session 是聊天历史用例使用的会话摘要，不包含 Cookie 或其他秘密。
type Session struct {
	// AccountID 是会话所属账号标识。
	AccountID string
	// ChatID 是平台聊天会话标识。
	ChatID string
	// BuyerID 是买家平台标识。
	BuyerID string
	// BuyerName 是买家展示名称。
	BuyerName string
	// BuyerAvatar 是买家头像地址。
	BuyerAvatar string
	// ItemID 是会话关联商品标识。
	ItemID string
	// ItemTitle 是会话关联商品标题。
	ItemTitle string
	// LastMessage 是最近消息摘要。
	LastMessage string
	// LastMessageAt 是最近消息时间的 Unix 秒时间戳。
	LastMessageAt int64
	// UnreadCount 是当前会话未读消息数量。
	UnreadCount int
}

// Page 是聊天历史查询的分页结果。
type Page struct {
	// Messages 是按时间正序排列的当前页消息。
	Messages []Message
	// Session 是当前会话摘要；找不到摘要时保持零值。
	Session Session
	// HasMore 表示是否可能还有更早消息。
	HasMore bool
}

// Repository 定义聊天历史用例需要的最小持久化能力。
type Repository interface {
	// ListMessages 按用户归属查询指定账号和会话的消息。
	ListMessages(ctx context.Context, userID int64, accountID, chatID string, beforeID int64, limit int) ([]Message, error)
	// ListSessions 按用户归属查询账号的会话摘要。
	ListSessions(ctx context.Context, userID int64, accountID string, limit int) ([]Session, error)
}

// Service 编排聊天历史查询，不持有 HTTP 请求或数据库连接。
type Service struct {
	// repository 保存由调用方注入的最小持久化端口。
	repository Repository
}

// New 创建聊天历史应用服务；空端口会导致构造结果不可用。
func New(repository Repository) *Service {
	return &Service{repository: repository}
}

// ListStoredMessages 查询当前用户有权访问的本地聊天历史。
// userID 用于归属过滤，accountID/chatID 定位账号和会话，beforeID 控制向更早消息翻页，limit 控制页大小；
// 返回的 Page 只含非敏感消息和会话摘要，底层端口错误原样返回。
func (s *Service) ListStoredMessages(ctx context.Context, userID int64, accountID, chatID string, beforeID int64, limit int) (Page, error) {
	accountID = strings.TrimSpace(accountID)
	chatID = strings.TrimSpace(chatID)
	if s == nil || s.repository == nil || userID <= 0 || accountID == "" || chatID == "" {
		return Page{}, ErrInvalidInput
	}
	// messages 保存按用户归属过滤后的聊天消息。
	messages, err := s.repository.ListMessages(ctx, userID, accountID, chatID, beforeID, limit)
	if err != nil {
		return Page{}, err
	}
	// session 保存当前会话摘要；查询失败时保持零值。
	var session Session
	// sessions 和 sessionErr 保存会话摘要列表及其查询错误；摘要失败不影响消息结果。
	if sessions, sessionErr := s.repository.ListSessions(ctx, userID, accountID, 500); sessionErr == nil {
		// candidate 保存当前遍历到的会话摘要。
		for _, candidate := range sessions {
			if candidate.ChatID == chatID {
				session = candidate
				break
			}
		}
	}
	return Page{Messages: messages, Session: session, HasMore: len(messages) == limit}, nil
}
