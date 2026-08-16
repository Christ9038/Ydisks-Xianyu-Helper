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
	return server.dependencies.NewChatSendingApplication(server.chat, server.Manager, server.mtopClient)
}
