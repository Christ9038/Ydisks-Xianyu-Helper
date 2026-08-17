package adapter

import (
	"xianyu-go/internal/account"
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/chat"
	"xianyu-go/internal/db"
)

// ChatDependencies 封装聊天应用服务所需的数据库适配器工厂，避免 Server 依赖通用设施容器。
type ChatDependencies struct {
	// store 保存聊天仓储共享的数据库入口，仅在 adapter 内部使用。
	store *db.Store
}

// NewChatDependencies 从数据库 Store 构造聊天专用依赖组。
func NewChatDependencies(store *db.Store) *ChatDependencies {
	if store == nil {
		return nil
	}
	return &ChatDependencies{store: store}
}

// NewChatSendingApplication 创建聊天应用服务及其平台、运行时和持久化适配器。
func (d *ChatDependencies) NewChatSendingApplication(service *chat.Service, manager *account.Manager, client func() MTOPClient) *chatapp.Service {
	if d == nil {
		return NewChatSendingApplication(service, nil, manager, client)
	}
	return NewChatSendingApplication(service, d.store, manager, client)
}
