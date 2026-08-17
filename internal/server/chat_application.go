package server

import (
	"xianyu-go/internal/adapter"
	chatapp "xianyu-go/internal/application/chat"
)

// newChatSendingApplication 创建实时聊天应用服务，Server 仅负责注入运行时依赖。
func newChatSendingApplication(server *Server) *chatapp.Service {
	if server == nil {
		return adapter.NewChatSendingApplication(nil, nil, nil, nil)
	}
	if server.chatDependencies == nil {
		return adapter.NewChatSendingApplication(server.chat, nil, server.Manager, server.mtopClient)
	}
	return server.chatDependencies.NewChatSendingApplication(server.chat, server.Manager, server.mtopClient)
}
