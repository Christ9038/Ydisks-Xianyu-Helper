package chat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// readRandomBytes 是可替换的系统随机字节读取函数，便于确定性验证本地消息键的降级路径。
var readRandomBytes = rand.Read

// randomID 生成本地出站消息幂等键的随机后缀；随机源失败时使用时间回退避免阻断发送。
func randomID() string {
	// value 保存随机读取的 128 位本地消息键熵。
	var value [16]byte
	// _, err 分别是随机字节读取数量和系统随机源错误。
	if _, err := readRandomBytes(value[:]); err == nil {
		return hex.EncodeToString(value[:])
	}
	return fmt.Sprintf("%d", time.Now().UTC().UnixNano())
}

// extractString 在嵌套聊天载荷中按键名递归提取首个非空文本。
func extractString(value any, keys ...string) string {
	// wanted 保存大小写无关的目标字段名。
	wanted := make(map[string]struct{}, len(keys))
	// key 是当前待规范化登记的目标字段名。
	for _, key := range keys {
		wanted[strings.ToLower(key)] = struct{}{}
	}
	// walk 递归处理对象、数组和可能嵌套 JSON 的字符串。
	var walk func(any) string
	walk = func(current any) string {
		// typed 是当前递归节点断言后的载荷容器类型。
		switch typed := current.(type) {
		case map[string]any:
			// key、child 分别是当前对象的字段名和字段值。
			for key, child := range typed {
				// ok 表示当前字段是否是调用方指定的候选字段。
				if _, ok := wanted[strings.ToLower(key)]; ok {
					// text 是转换并裁剪后的候选字段文本。
					if text := strings.TrimSpace(fmt.Sprint(child)); text != "" && text != "<nil>" {
						return text
					}
				}
			}
			// child 是当前对象中待递归查找的非目标字段值。
			for _, child := range typed {
				// text 是子树中返回的首个非空文本。
				if text := walk(child); text != "" {
					return text
				}
			}
		case []any:
			// child 是当前数组中待递归查找的元素。
			for _, child := range typed {
				// text 是子元素中返回的首个非空文本。
				if text := walk(child); text != "" {
					return text
				}
			}
		case string:
			// decoded 是字符串形式 JSON 解码后的嵌套载荷。
			var decoded any
			if json.Unmarshal([]byte(typed), &decoded) == nil {
				return walk(decoded)
			}
		}
		return ""
	}
	return walk(value)
}

// extractUnixMilli 提取聊天载荷的毫秒时间戳，并兼容秒级值。
func extractUnixMilli(raw map[string]any) int64 {
	// text 是按平台候选字段读取的时间文本。
	text := extractString(raw, "sendTime", "timestamp", "time", "createdAt")
	// value 是解析后的秒或毫秒 Unix 时间戳。
	var value int64
	_, _ = fmt.Sscan(text, &value)
	if value > 0 && value < 10_000_000_000 {
		value *= 1000
	}
	return value
}
