package server

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"strings"

	chatapp "xianyu-go/internal/application/chat"
)

// chatSessionCursorVersion 标识当前本地会话游标的固定编码版本。
const chatSessionCursorVersion = 1

// chatSessionCursorPayload 是本地会话游标序列化后的稳定公开字段集合。
// 它不携带账号、用户或任何凭证信息，归属隔离始终由服务端查询条件保证。
type chatSessionCursorPayload struct {
	// Version 是游标格式版本，用于拒绝未来不兼容格式。
	Version int `json:"v"`
	// LastMessageAt 是最后一条会话的毫秒排序时间。
	LastMessageAt int64 `json:"last_message_at"`
	// ChatID 是同一时间下继续键集分页的会话标识。
	ChatID string `json:"chat_id"`
}

// decodeChatSessionCursor 解码请求提供的本地会话游标；空字符串表示首页。
func decodeChatSessionCursor(raw string) (*chatapp.SessionCursor, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	// encoded 保存 URL 安全 Base64 解码后的 JSON 字节。
	encoded, decodeErr := base64.RawURLEncoding.DecodeString(raw)
	if decodeErr != nil {
		return nil, errors.New("本地会话游标格式无效")
	}
	// payload 保存游标的已验证 JSON 内容。
	var payload chatSessionCursorPayload
	// unmarshalErr 保存游标 JSON 结构无法解析时的格式错误。
	if unmarshalErr := json.Unmarshal(encoded, &payload); unmarshalErr != nil {
		return nil, errors.New("本地会话游标格式无效")
	}
	if payload.Version != chatSessionCursorVersion || strings.TrimSpace(payload.ChatID) == "" {
		return nil, errors.New("本地会话游标格式无效")
	}
	return &chatapp.SessionCursor{LastMessageAt: payload.LastMessageAt, ChatID: payload.ChatID}, nil
}

// encodeChatSessionCursor 将下一页应用层排序键编码为 URL 安全的不透明游标。
func encodeChatSessionCursor(cursor *chatapp.SessionCursor) (string, error) {
	if cursor == nil {
		return "", nil
	}
	if strings.TrimSpace(cursor.ChatID) == "" {
		return "", errors.New("本地会话游标缺少会话标识")
	}
	// payload 保存需编码的固定游标版本和排序键。
	payload := chatSessionCursorPayload{Version: chatSessionCursorVersion, LastMessageAt: cursor.LastMessageAt, ChatID: cursor.ChatID}
	// encoded 保存 JSON 序列化后的游标内容。
	encoded, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		return "", marshalErr
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}
