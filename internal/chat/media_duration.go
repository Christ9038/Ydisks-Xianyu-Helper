// Package chat 提供闲鱼实时与历史聊天载荷的非敏感展示字段归一能力。
package chat

import "encoding/json"

// extractMediaDuration 递归读取语音载荷的 duration 字段，避免将视频等其他媒体时长误用于语音展示。
// raw 是实时或历史协议的解码对象；messageType 已由媒体地址分类确定；返回值为正整数秒，缺失时为零。
func extractMediaDuration(raw map[string]any, messageType string) int64 {
	if messageType != "audio" {
		return 0
	}
	// inspect 从任意嵌套 JSON 容器中定位语音对象，遇到第一个正数秒值即结束。
	var inspect func(any) int64
	inspect = func(value any) int64 {
		switch // typed 是当前递归节点的协议原始类型。
		typed := value.(type) {
		case string:
			// nested 保存成功反序列化的内层 JSON，实时 WebSocket 经常将其作为字符串封装。
			var nested any
			if json.Unmarshal([]byte(typed), &nested) == nil {
				return inspect(nested)
			}
		case map[string]any:
			// audio 保存当前消息的语音元数据对象；只有其 duration 具备媒体语义。
			audio := mapValue(typed["audio"])
			// duration 保存当前语音对象声明的秒级时长，正数表示可以直接用于播放前展示。
			if duration := int64Value(audio["duration"]); duration > 0 {
				return duration
			}
			// child 是当前对象的子载荷，继续兼容协议多层包装。
			for _, child := range typed {
				// duration 保存当前子载荷递归解析出的秒级语音时长。
				if duration := inspect(child); duration > 0 {
					return duration
				}
			}
		case []any:
			// child 是当前数组中的一项协议载荷。
			for _, child := range typed {
				// duration 保存当前数组项递归解析出的秒级语音时长。
				if duration := inspect(child); duration > 0 {
					return duration
				}
			}
		}
		return 0
	}
	return inspect(raw)
}
