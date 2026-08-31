package chat

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
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

// extractString 在嵌套聊天载荷中按候选键优先级递归提取首个非空文本。
// 同一对象先检查调用方给出的键顺序，再按稳定字段名顺序进入子树，避免 Go map 随机遍历改变消息标识或时间。
func extractString(value any, keys ...string) string {
	// wanted 保存去重后且保持调用方优先级的目标字段名。
	wanted := make([]string, 0, len(keys))
	// registered 保存已登记的小写字段名，避免大小写重复候选改变查找顺序。
	registered := make(map[string]struct{}, len(keys))
	// key 是当前待规范化登记的候选字段名。
	for _, key := range keys {
		// normalized 保存大小写无关匹配使用的字段名。
		normalized := strings.ToLower(strings.TrimSpace(key))
		if normalized == "" {
			continue
		}
		// exists 表示规范化后的候选字段是否已经登记。
		if _, exists := registered[normalized]; exists {
			continue
		}
		registered[normalized] = struct{}{}
		wanted = append(wanted, normalized)
	}
	// walk 递归处理对象、数组和可能嵌套 JSON 的字符串。
	var walk func(any) string
	walk = func(current any) string {
		// typed 是当前递归节点断言后的载荷容器类型。
		switch typed := current.(type) {
		case map[string]any:
			// fieldNames 保存当前对象稳定排序后的字段名，兼顾大小写别名和子树遍历确定性。
			fieldNames := make([]string, 0, len(typed))
			// fieldName 表示当前登记到稳定顺序中的对象字段名。
			for fieldName := range typed {
				fieldNames = append(fieldNames, fieldName)
			}
			sort.Strings(fieldNames)
			// wantedKey 表示当前按调用方优先级查找的候选字段名。
			for _, wantedKey := range wanted {
				// fieldName 表示当前与候选字段执行大小写无关比较的实际字段名。
				for _, fieldName := range fieldNames {
					if strings.ToLower(fieldName) != wantedKey {
						continue
					}
					// text 是转换并裁剪后的候选字段文本。
					if text := strings.TrimSpace(fmt.Sprint(typed[fieldName])); text != "" && text != "<nil>" {
						return text
					}
				}
			}
			// fieldName 表示当前按稳定字段名顺序进入的子树。
			for _, fieldName := range fieldNames {
				// text 是子树中返回的首个非空文本。
				if text := walk(typed[fieldName]); text != "" {
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
