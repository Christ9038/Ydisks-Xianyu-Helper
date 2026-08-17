// Package chat 提供聊天历史等 HTTP 用例所需的应用层编排，不依赖 HTTP 或数据库模型。
package chat

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"
)

// ErrInvalidInput 表示聊天历史查询缺少必要的非敏感标识。
var ErrInvalidInput = errors.New("聊天历史查询参数无效")

// ErrSessionUnavailable 表示会话清理、归属或身份持久化端口未装配。
var ErrSessionUnavailable = errors.New("聊天会话服务未启用")

// ErrSubscriptionUnavailable 表示实时聊天事件订阅端口尚未装配。
var ErrSubscriptionUnavailable = errors.New("聊天实时订阅服务未启用")

// ErrRefreshUnavailable 表示聊天平台刷新端口尚未装配。
var ErrRefreshUnavailable = errors.New("聊天刷新服务未启用")

// ErrRefreshPersist 表示平台刷新成功但本地聊天历史持久化失败。
var ErrRefreshPersist = errors.New("聊天刷新结果保存失败")

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

// Identity 是平台身份查询返回的非敏感展示信息。
type Identity struct {
	// BuyerName 是平台返回的买家展示名称。
	BuyerName string
	// BuyerAvatar 是平台返回的买家头像地址。
	BuyerAvatar string
}

// IdentityResolver 定义聊天应用获取平台会话展示身份的最小能力。
// 凭证读取、平台请求和凭证刷新均由适配器内部完成，应用层只接收展示字段。
type IdentityResolver interface {
	// Resolve 根据账号和聊天会话查询非敏感的买家展示身份。
	Resolve(ctx context.Context, accountID, chatID string) (Identity, error)
}

// Event 是实时聊天推送使用的非敏感应用层事件模型。
type Event struct {
	// Type 表示事件类别，例如 message.created。
	Type string `json:"type"`
	// Message 保存事件关联的聊天消息；非消息事件可以为空。
	Message *Message `json:"message,omitempty"`
	// Session 保存事件关联的会话摘要；无会话信息时为空。
	Session *Session `json:"session,omitempty"`
}

// SubscriptionProvider 定义实时聊天订阅所需的最小能力。
type SubscriptionProvider interface {
	// Subscribe 按用户归属订阅实时事件，并返回幂等取消函数。
	Subscribe(ctx context.Context, userID int64) (<-chan Event, func(), error)
}

// ConversationPage 是平台联系人刷新后返回的非敏感分页结果。
type ConversationPage struct {
	// HasMore 表示平台是否还存在更早的联系人页。
	HasMore bool
	// NextCursor 保存下一次联系人刷新使用的平台游标。
	NextCursor int64
}

// HistoryPage 是平台聊天历史刷新后返回的非敏感分页结果。
type HistoryPage struct {
	// Messages 保存已写入本地仓储的聊天消息。
	Messages []Message
	// Session 保存当前会话的非敏感摘要。
	Session Session
	// HasMore 表示平台是否还存在更早的消息页。
	HasMore bool
	// NextCursor 保存下一次历史刷新使用的平台游标。
	NextCursor int64
}

// RefreshProvider 定义聊天平台刷新和本地落库所需的最小适配能力。
// 原始平台响应、运行时实例和数据库模型均由适配器持有，不进入 Server。
type RefreshProvider interface {
	// RefreshConversations 拉取并保存指定账号的联系人页。
	RefreshConversations(context.Context, string, int64, int) (ConversationPage, error)
	// RefreshHistory 拉取并保存指定会话的消息页。
	RefreshHistory(context.Context, string, string, int64, int, Session) (HistoryPage, error)
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

// SessionRepository 定义会话列表之外的清理、身份写入和账号归属能力。
type SessionRepository interface {
	// Repository 提供聊天消息和会话列表查询能力。
	Repository
	// DeleteEmptySessions 删除指定账号中没有有效消息的空会话壳。
	DeleteEmptySessions(ctx context.Context, accountID string) error
	// UpdateSessionIdentity 更新会话的买家展示名称和头像。
	UpdateSessionIdentity(ctx context.Context, accountID, chatID, buyerID, buyerName, buyerAvatar string) error
	// ExistsOwned 判断账号是否归属于指定用户，只返回存在性，不返回敏感字段。
	ExistsOwned(ctx context.Context, userID int64, accountID string) (bool, error)
	// MarkRead 将指定用户拥有的会话未读数归零。
	MarkRead(ctx context.Context, userID int64, accountID, chatID string) error
}

// ReadMessageIDResolver 定义旧版聊天关联标识解析需要的最小诊断查询能力。
type ReadMessageIDResolver interface {
	// FindInboundParsedJSONContaining 返回可能包含旧关联标识的有限已解密入站诊断帧。
	FindInboundParsedJSONContaining(ctx context.Context, accountID, fragment string, limit int) ([]string, error)
}

// Service 编排聊天历史查询，不持有 HTTP 请求或数据库连接。
type Service struct {
	// repository 保存由调用方注入的最小持久化端口。
	repository Repository
	// outgoing 保存实时发送用例的本地消息写入端口。
	outgoing OutgoingRepository
	// senders 保存按账号解析在线发送实例的端口。
	senders SenderProvider
	// uploader 保存图片上传的平台适配端口。
	uploader ImageUploader
	// identityResolver 保存平台身份查询端口，凭证只在适配器内部短暂存在。
	identityResolver IdentityResolver
	// subscription 保存实时事件订阅端口；平台和领域实现不会泄露到 HTTP 层。
	subscription SubscriptionProvider
	// refresh 保存平台聊天刷新端口；原始响应只在适配器内部解析和持久化。
	refresh RefreshProvider
}

// New 创建聊天历史应用服务；空端口会导致构造结果不可用。
func New(repository Repository) *Service {
	return &Service{repository: repository}
}

// NewWithIdentity 创建支持平台会话身份补全的聊天应用服务。
func NewWithIdentity(repository Repository, resolver IdentityResolver) *Service {
	return &Service{repository: repository, identityResolver: resolver}
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

// ListSessions 查询当前用户有权访问的账号会话摘要。
func (s *Service) ListSessions(ctx context.Context, userID int64, accountID string, limit int) ([]Session, error) {
	accountID = strings.TrimSpace(accountID)
	if s == nil || s.repository == nil || userID <= 0 || accountID == "" {
		return nil, ErrInvalidInput
	}
	// sessions 和 err 保存带用户归属的会话摘要及查询错误。
	sessions, err := s.repository.ListSessions(ctx, userID, accountID, limit)
	if err != nil {
		return nil, err
	}
	return sessions, nil
}

// FindSession 查询指定账号下的单个会话；找不到时返回零值且不视为错误。
func (s *Service) FindSession(ctx context.Context, userID int64, accountID, chatID string) (Session, error) {
	chatID = strings.TrimSpace(chatID)
	if chatID == "" {
		return Session{}, ErrInvalidInput
	}
	// sessions 和 err 保存会话摘要列表及查询错误。
	sessions, err := s.ListSessions(ctx, userID, accountID, 500)
	if err != nil {
		return Session{}, err
	}
	// session 保存匹配到的会话摘要。
	var session Session
	// candidate 表示当前遍历到的会话摘要。
	for _, candidate := range sessions {
		if candidate.ChatID == chatID {
			session = candidate
			break
		}
	}
	return session, nil
}

// ResolveReadMessageID 将旧版实时消息关联标识转换为闲鱼已读接口要求的 PNM 标识。
func (s *Service) ResolveReadMessageID(ctx context.Context, accountID, chatID, messageID string) string {
	// legacyID 保存去除空白后的待迁移消息关联标识。
	legacyID := strings.TrimSpace(messageID)
	if legacyID == "" || strings.HasSuffix(legacyID, ".PNM") || s == nil || s.repository == nil {
		return legacyID
	}
	// resolver 保存仓储实现的可选诊断帧查询能力。
	resolver, ok := s.repository.(ReadMessageIDResolver)
	if !ok {
		return ""
	}
	// values、err 保存可能匹配旧标识的诊断 JSON 及读取错误。
	values, err := resolver.FindInboundParsedJSONContaining(ctx, accountID, legacyID, 20)
	if err != nil {
		return ""
	}
	// value 保存当前待解析的诊断 JSON。
	for _, value := range values {
		// decoded 保存当前诊断帧反序列化后的动态结构。
		var decoded any
		if json.Unmarshal([]byte(value), &decoded) != nil {
			continue
		}
		// platformID 保存当前诊断帧中解析出的平台 PNM 标识。
		if platformID := FindPlatformMessageID(decoded, chatID, legacyID); platformID != "" {
			return platformID
		}
	}
	return ""
}

// FindPlatformMessageID 在已解密的实时消息结构中定位与旧关联标识对应的 PNM 标识。
func FindPlatformMessageID(value any, chatID, legacyID string) string {
	// walk 递归检查嵌套 map、数组和嵌入 JSON 文本。
	var walk func(any) string
	walk = func(current any) string {
		// typed 保存当前动态节点按容器类别断言后的值。
		switch typed := current.(type) {
		case map[string]any:
			// payload、ok 保存协议第 10 段及其存在标识。
			if payload, ok := typed["10"]; ok && readValueContainsID(payload, legacyID) {
				// candidate 保存当前协议节点提供的平台消息标识。
				candidate := strings.TrimSpace(fmt.Sprint(typed["3"]))
				if strings.HasSuffix(candidate, ".PNM") {
					// currentChatID 保存协议节点所属的聊天会话标识。
					currentChatID := strings.TrimSuffix(strings.TrimSpace(fmt.Sprint(typed["2"])), "@goofish")
					if currentChatID == "" || currentChatID == chatID {
						return candidate
					}
				}
			}
			// child 保存当前 map 的嵌套协议节点。
			for _, child := range typed {
				// candidate 保存递归解析得到的候选 PNM 标识。
				if candidate := walk(child); candidate != "" {
					return candidate
				}
			}
		case []any:
			// child 保存当前数组中的嵌套协议节点。
			for _, child := range typed {
				// candidate 保存递归解析得到的候选 PNM 标识。
				if candidate := walk(child); candidate != "" {
					return candidate
				}
			}
		case string:
			// nested 保存嵌入 JSON 文本反序列化后的结构。
			var nested any
			if json.Unmarshal([]byte(typed), &nested) == nil {
				return walk(nested)
			}
		}
		return ""
	}
	return walk(value)
}

// readValueContainsID 判断嵌套诊断字段是否保存了指定旧关联标识。
func readValueContainsID(value any, legacyID string) bool {
	// typed 保存当前动态节点按容器类别断言后的值。
	switch typed := value.(type) {
	case map[string]any:
		// key、child 保存当前字段名及其嵌套值。
		for key, child := range typed {
			if (strings.EqualFold(key, "messageId") || strings.EqualFold(key, "message_id")) && strings.TrimSpace(fmt.Sprint(child)) == legacyID {
				return true
			}
			if readValueContainsID(child, legacyID) {
				return true
			}
		}
	case []any:
		// child 保存当前数组中的嵌套值。
		for _, child := range typed {
			if readValueContainsID(child, legacyID) {
				return true
			}
		}
	case string:
		// nested 保存嵌入 JSON 文本反序列化后的结构。
		var nested any
		if json.Unmarshal([]byte(typed), &nested) == nil {
			return readValueContainsID(nested, legacyID)
		}
	}
	return false
}

// CleanupEmptySessions 清理平台分页产生的无效空会话壳。
func (s *Service) CleanupEmptySessions(ctx context.Context, accountID string) error {
	accountID = strings.TrimSpace(accountID)
	if s == nil || s.repository == nil || accountID == "" {
		return ErrInvalidInput
	}
	// repository 保存支持会话维护操作的窄端口。
	repository, ok := s.repository.(SessionRepository)
	if !ok {
		return ErrSessionUnavailable
	}
	return repository.DeleteEmptySessions(ctx, accountID)
}

// OwnsAccount 查询当前用户是否拥有指定账号，不读取或解密账号凭证。
func (s *Service) OwnsAccount(ctx context.Context, userID int64, accountID string) (bool, error) {
	accountID = strings.TrimSpace(accountID)
	if s == nil || s.repository == nil || userID <= 0 || accountID == "" {
		return false, ErrInvalidInput
	}
	// repository 保存支持账号归属查询的窄端口。
	repository, ok := s.repository.(SessionRepository)
	if !ok {
		return false, ErrSessionUnavailable
	}
	return repository.ExistsOwned(ctx, userID, accountID)
}

// MarkRead 将当前用户拥有的指定会话标记为已读；底层端口只接收非敏感标识。
func (s *Service) MarkRead(ctx context.Context, userID int64, accountID, chatID string) error {
	accountID = strings.TrimSpace(accountID)
	chatID = strings.TrimSpace(chatID)
	if s == nil || s.repository == nil || userID <= 0 || accountID == "" || chatID == "" {
		return ErrInvalidInput
	}
	// repository 保存支持会话已读更新的窄端口。
	repository, ok := s.repository.(SessionRepository)
	if !ok {
		return ErrSessionUnavailable
	}
	return repository.MarkRead(ctx, userID, accountID, chatID)
}

// ResolveSessionIdentity 补全单个会话展示身份并尽力保存到本地。
// 平台查询错误会原样返回，但已获得的会话摘要仍会返回给调用方。
func (s *Service) ResolveSessionIdentity(ctx context.Context, session Session) (Session, error) {
	if s == nil || s.repository == nil || strings.TrimSpace(session.AccountID) == "" || strings.TrimSpace(session.ChatID) == "" {
		return session, ErrInvalidInput
	}
	// resolveErr 保存平台身份查询失败，供 HTTP 层决定是否触发会话恢复。
	var resolveErr error
	if session.BuyerID != "1400" && s.identityResolver != nil {
		// identity 和 err 保存平台适配器返回的非敏感身份及调用错误。
		identity, err := s.identityResolver.Resolve(ctx, session.AccountID, session.ChatID)
		if err != nil {
			resolveErr = err
		} else {
			// name 是去除空白后的平台买家名称。
			if name := strings.TrimSpace(identity.BuyerName); name != "" {
				session.BuyerName = name
			}
			// avatar 是去除空白后的平台买家头像地址。
			if avatar := strings.TrimSpace(identity.BuyerAvatar); avatar != "" {
				session.BuyerAvatar = avatar
			}
		}
	}
	// repository 保存会话身份更新所需的窄端口。
	if repository, ok := s.repository.(SessionRepository); ok {
		// _ 表示身份缓存更新失败不应覆盖旧 handler 的展示容错语义。
		_ = repository.UpdateSessionIdentity(ctx, session.AccountID, session.ChatID, session.BuyerID, session.BuyerName, session.BuyerAvatar)
	}
	return session, resolveErr
}

// RefreshSessionIdentities 并发补全会话列表身份，保留首个失败以供调用方处理过期会话。
func (s *Service) RefreshSessionIdentities(ctx context.Context, accountID string, sessions []Session) ([]Session, error) {
	accountID = strings.TrimSpace(accountID)
	if s == nil || s.repository == nil || accountID == "" {
		return sessions, ErrInvalidInput
	}
	if s.identityResolver == nil {
		return sessions, nil
	}
	// result 复制输入列表，避免异步身份补全修改 handler 持有的外部切片。
	result := append([]Session(nil), sessions...)
	// jobs 保存待补全的会话下标。
	jobs := make(chan int)
	// workers 保存身份补全工作器的完成状态。
	var workers sync.WaitGroup
	// once 保证只记录第一个平台查询错误。
	var once sync.Once
	// firstErr 保存第一个平台查询错误。
	var firstErr error
	// workerCount 是固定的并发度，避免单个账号的联系人数量放大 goroutine 数量。
	workerCount := 8
	// worker 表示当前启动的身份补全工作器序号。
	for worker := 0; worker < workerCount; worker++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			// index 表示当前待处理会话在结果切片中的下标。
			for index := range jobs {
				// identityCtx 和 cancel 限制单个联系人平台查询的最长时间。
				identityCtx, cancel := context.WithTimeout(ctx, 3*time.Second)
				// updated 和 err 保存身份补全后的会话及平台查询错误。
				updated, err := s.ResolveSessionIdentity(identityCtx, result[index])
				cancel()
				result[index] = updated
				if err != nil {
					once.Do(func() { firstErr = err })
				}
			}
		}()
	}
	// queueDone 表示是否因为父上下文取消而提前停止投递。
	queueDone := false
	// index 表示当前排队会话在结果切片中的下标。
	for index := range result {
		if result[index].BuyerID == "1400" {
			continue
		}
		select {
		case jobs <- index:
		case <-ctx.Done():
			queueDone = true
		}
		if queueDone {
			break
		}
	}
	close(jobs)
	workers.Wait()
	return result, firstErr
}
