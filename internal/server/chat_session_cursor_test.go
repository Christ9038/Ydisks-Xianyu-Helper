package server

import (
	"strings"
	"testing"

	chatapp "xianyu-go/internal/application/chat"
)

// TestChatSessionCursorRoundTrip 验证本地会话游标以不透明编码保留稳定键集排序键。
func TestChatSessionCursorRoundTrip(t *testing.T) {
	// original 保存要编码的同时间会话稳定排序键。
	original := &chatapp.SessionCursor{LastMessageAt: 1710000000000, ChatID: "chat-2"}
	// encoded 和 encodeErr 保存编码后的不透明游标及可能的序列化错误。
	encoded, encodeErr := encodeChatSessionCursor(original)
	if encodeErr != nil {
		t.Fatalf("编码本地会话游标失败: %v", encodeErr)
	}
	if strings.Contains(encoded, original.ChatID) {
		t.Fatalf("游标泄露了可读会话标识: %s", encoded)
	}
	// decoded 和 decodeErr 保存解码后的排序键及可能的格式错误。
	decoded, decodeErr := decodeChatSessionCursor(encoded)
	if decodeErr != nil {
		t.Fatalf("解码本地会话游标失败: %v", decodeErr)
	}
	if decoded == nil || *decoded != *original {
		t.Fatalf("游标往返结果=%+v，期望=%+v", decoded, original)
	}
}

// TestDecodeChatSessionCursorRejectsInvalidPayload 验证空会话标识和非 Base64 游标会返回请求错误。
func TestDecodeChatSessionCursorRejectsInvalidPayload(t *testing.T) {
	// cases 保存需要拒绝的客户端游标输入。
	cases := []string{"%%%", "eyJ2IjoxLCJsYXN0X21lc3NhZ2VfYXQiOjF9"}
	// raw 表示当前待验证的非法游标。
	for _, raw := range cases {
		// decodeErr 保存当前非法游标被解码器拒绝时的格式错误。
		if _, decodeErr := decodeChatSessionCursor(raw); decodeErr == nil {
			t.Fatalf("非法游标 %q 被错误接受", raw)
		}
	}
}
