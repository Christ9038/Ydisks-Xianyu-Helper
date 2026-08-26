package chat

import (
	"encoding/json"
	"testing"
)

// TestChatPayloadValueConversions 覆盖聊天载荷数字、布尔和媒体时长的兼容转换。
func TestChatPayloadValueConversions(t *testing.T) {
	// intCases 保存常见协议数值类型及其统一整数结果。
	intCases := []struct {
		input any
		want  int64
	}{{float64(1.5), 1}, {int64(2), 2}, {int(3), 3}, {json.Number("4"), 4}, {" 5 ", 5}, {"bad", 0}, {nil, 0}}
	// item 表示当前待验证的协议数值样例。
	for _, item := range intCases {
		// got 保存当前协议值转换后的整数。
		if got := int64Value(item.input); got != item.want {
			t.Errorf("int64Value(%v)=%d want %d", item.input, got, item.want)
		}
	}
	// boolCases 保存常见协议布尔类型及其统一结果。
	boolCases := []struct {
		input any
		want  bool
	}{{true, true}, {false, false}, {float64(1), true}, {float64(0), false}, {1, true}, {"true", true}, {"1", true}, {"false", false}, {nil, false}}
	// item 表示当前待验证的协议布尔样例。
	for _, item := range boolCases {
		// got 保存当前协议值转换后的布尔值。
		if got := boolValue(item.input); got != item.want {
			t.Errorf("boolValue(%v)=%v want %v", item.input, got, item.want)
		}
	}
	// audioDuration 保存语音载荷中递归读取的秒级时长。
	audioDuration := extractMediaDuration(map[string]any{"wrapper": `{"audio":{"duration":9}}`}, "audio")
	if audioDuration != 9 || extractMediaDuration(map[string]any{"audio": map[string]any{"duration": 9}}, "image") != 0 {
		t.Fatalf("audio durations=%d", audioDuration)
	}
	// nestedDuration 覆盖数组、无效字符串和非正时长的递归兼容路径。
	nestedDuration := extractMediaDuration(map[string]any{"items": []any{"not-json", map[string]any{"audio": map[string]any{"duration": 0}}, map[string]any{"audio": map[string]any{"duration": 11}}}}, "audio")
	if nestedDuration != 11 {
		t.Fatalf("nested audio duration=%d", nestedDuration)
	}
}
