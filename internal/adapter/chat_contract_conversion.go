package adapter

import (
	chatapp "xianyu-go/internal/application/chat"
	"xianyu-go/internal/db"
)

// ChatSessionFromApplication 将聊天应用会话转换为 legacy 聊天领域写入模型。
// 转换只包含会话展示字段，不携带 Cookie、Token 或其他账号秘密。
func ChatSessionFromApplication(session chatapp.Session) db.ChatSession {
	return dbChatSession(session)
}

// ChatMessagesFromDB 将 legacy 聊天领域消息转换为应用层非敏感消息模型。
// 数据库模型只在 adapter 内部出现，调用方获得的结果不暴露存储层类型。
func ChatMessagesFromDB(messages []db.ChatMessage) []chatapp.Message {
	// result 保存转换后的应用层消息列表。
	result := make([]chatapp.Message, 0, len(messages))
	// message 表示当前待转换的 legacy 聊天消息。
	for _, message := range messages {
		result = append(result, chatApplicationMessage(&message))
	}
	return result
}
