package engine

import "testing"

// TestExtractMessageReadEventCoversCompactBatchAndNestedJSON 验证已读回执解析的批量紧凑格式、字段回退和 JSON 字符串信封。
func TestExtractMessageReadEventCoversCompactBatchAndNestedJSON(t *testing.T) {
	// compactEvent 保存平台批量紧凑格式的已读回执。
	compactEvent := map[string]any{"1": []any{"message.PNM"}, "2": "2", "3": "chat@goofish"}
	// compactRead、compactOK 保存紧凑回执解析结果。
	compactRead, compactOK := extractMessageReadEvent(compactEvent)
	if !compactOK || compactRead.MessageID != "message.PNM" || compactRead.ChatID != "chat" {
		t.Fatalf("紧凑回执解析异常：event=%+v ok=%v", compactRead, compactOK)
	}

	// nestedEvent 保存被 JSON 字符串包裹的兼容回执信封。
	nestedEvent := map[string]any{"payload": `{"bizType":40103,"id":"nested-message","chat_id":"nested-chat@goofish"}`}
	// nestedRead、nestedOK 保存嵌套回执解析结果。
	nestedRead, nestedOK := extractMessageReadEvent(nestedEvent)
	if !nestedOK || nestedRead.MessageID != "nested-message" || nestedRead.ChatID != "nested-chat@goofish" {
		t.Fatalf("嵌套回执解析异常：event=%+v ok=%v", nestedRead, nestedOK)
	}
	// missingRead、missingOK 保存没有有效消息 ID 的回执结果。
	missingRead, missingOK := extractMessageReadEvent(map[string]any{"payload": map[string]any{"bizType": 40103, "cid": "chat"}})
	if missingOK || missingRead.MessageID != "" {
		t.Fatalf("缺少消息 ID 的回执不应命中：event=%+v ok=%v", missingRead, missingOK)
	}
	// ordinaryRead、ordinaryOK 保存普通非回执消息的结果。
	ordinaryRead, ordinaryOK := extractMessageReadEvent(map[string]any{"type": 10001})
	if ordinaryOK || ordinaryRead.MessageID != "" {
		t.Fatalf("普通消息不应被识别为已读回执：event=%+v ok=%v", ordinaryRead, ordinaryOK)
	}
}

// TestMessageContentTypeCoversExtensionAndNestedFallbacks 验证聊天内容类型从扩展字段、紧凑字段和 JSON 内容回退读取。
func TestMessageContentTypeCoversExtensionAndNestedFallbacks(t *testing.T) {
	// extensionType 表示扩展 JSON 直接提供的内容类型。
	extensionType := messageContentType(map[string]any{}, map[string]any{"extJson": `{"contentType":14}`})
	if extensionType != "14" {
		t.Fatalf("扩展内容类型=%q", extensionType)
	}
	// compactType 表示紧凑消息字段直接提供的内容类型。
	compactType := messageContentType(map[string]any{"6": map[string]any{"3": map[string]any{"4": 26}}}, map[string]any{})
	if compactType != "26" {
		t.Fatalf("紧凑内容类型=%q", compactType)
	}
	// nestedType 表示紧凑消息字段中的 JSON 内容提供的内容类型。
	nestedType := messageContentType(map[string]any{"6": map[string]any{"3": map[string]any{"5": `{"contentType":"7"}`}}}, map[string]any{})
	if nestedType != "7" {
		t.Fatalf("嵌套内容类型=%q", nestedType)
	}
	if messageContentType(map[string]any{}, map[string]any{"extJson": "not-json"}) != "" {
		t.Fatal("非法扩展 JSON 不应产生内容类型")
	}
}
