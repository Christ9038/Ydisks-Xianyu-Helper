package adapter

import (
	"context"
	"errors"
	"fmt"

	"xianyu-go/internal/account"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/automation"
	domainchat "xianyu-go/internal/chat"
)

// chatRefreshProvider 将账号运行时的平台分页能力与聊天领域落库服务收敛到应用端口。
type chatRefreshProvider struct {
	// service 保存联系人和消息页的领域解析、幂等写入能力。
	service *domainchat.Service
	// lookup 按账号查找在线运行实例；运行时管理器只在适配器装配边界出现。
	lookup func(string) (automation.MessageSender, bool)
}

// NewChatRefreshProvider 创建聊天平台刷新适配器。
func NewChatRefreshProvider(service *domainchat.Service, manager *account.Manager) chatapp.RefreshProvider {
	if service == nil || manager == nil {
		return nil
	}
	return chatRefreshProvider{service: service, lookup: func(accountID string) (automation.MessageSender, bool) {
		return manager.GetInstance(accountID)
	}}
}

// RefreshConversations 拉取联系人页并在领域服务中完成解析和持久化。
func (p chatRefreshProvider) RefreshConversations(ctx context.Context, accountID string, cursor int64, limit int) (chatapp.ConversationPage, error) {
	if p.service == nil || p.lookup == nil {
		return chatapp.ConversationPage{}, chatapp.ErrRefreshUnavailable
	}
	// sender 和 ok 保存指定账号的在线运行实例及其存在性。
	sender, ok := p.lookup(accountID)
	if !ok || sender == nil {
		return chatapp.ConversationPage{}, chatapp.ErrOffline
	}
	// fetcher 和 supported 保存联系人平台分页能力及接口支持状态。
	fetcher, supported := sender.(interface {
		FetchChatConversations(context.Context, int64, int) (map[string]any, string, error)
	})
	if !supported {
		return chatapp.ConversationPage{}, errors.New("当前账号不支持聊天联系人刷新")
	}
	// body、myID 和 fetchErr 保存平台原始响应、当前账号身份及请求错误。
	body, myID, fetchErr := fetcher.FetchChatConversations(ctx, cursor, limit)
	if fetchErr != nil {
		return chatapp.ConversationPage{}, fetchErr
	}
	// page 和 saveErr 保存领域解析后的联系人分页结果及持久化错误。
	page, saveErr := p.service.RecordConversationPage(ctx, accountID, myID, body)
	if saveErr != nil {
		return chatapp.ConversationPage{}, fmt.Errorf("%w: %v", chatapp.ErrRefreshPersist, saveErr)
	}
	return chatapp.ConversationPage{HasMore: page.HasMore, NextCursor: page.NextCursor}, nil
}

// RefreshHistory 拉取聊天历史页并在领域服务中完成解析和持久化。
func (p chatRefreshProvider) RefreshHistory(ctx context.Context, accountID, chatID string, cursor int64, limit int, session chatapp.Session) (chatapp.HistoryPage, error) {
	if p.service == nil || p.lookup == nil {
		return chatapp.HistoryPage{}, chatapp.ErrRefreshUnavailable
	}
	// sender 和 ok 保存指定账号的在线运行实例及其存在性。
	sender, ok := p.lookup(accountID)
	if !ok || sender == nil {
		return chatapp.HistoryPage{}, chatapp.ErrOffline
	}
	// fetcher 和 supported 保存历史平台分页能力及接口支持状态。
	fetcher, supported := sender.(interface {
		FetchChatHistory(context.Context, string, int64, int) (map[string]any, string, error)
	})
	if !supported {
		return chatapp.HistoryPage{}, errors.New("当前账号不支持聊天历史刷新")
	}
	// body、myID 和 fetchErr 保存平台原始响应、当前账号身份及请求错误。
	body, myID, fetchErr := fetcher.FetchChatHistory(ctx, chatID, cursor, limit)
	if fetchErr != nil {
		return chatapp.HistoryPage{}, fetchErr
	}
	// page 和 saveErr 保存领域解析后的消息分页结果及持久化错误。
	page, saveErr := p.service.RecordHistoryPage(ctx, accountID, chatID, myID, ChatSessionFromApplication(session), body)
	if saveErr != nil {
		return chatapp.HistoryPage{}, fmt.Errorf("%w: %v", chatapp.ErrRefreshPersist, saveErr)
	}
	return chatapp.HistoryPage{
		Messages: ChatMessagesFromDB(page.Messages), Session: session,
		HasMore: page.HasMore, NextCursor: page.NextCursor,
	}, nil
}

// 确保聊天刷新适配器覆盖应用层定义的全部能力。
var _ chatapp.RefreshProvider = chatRefreshProvider{}
