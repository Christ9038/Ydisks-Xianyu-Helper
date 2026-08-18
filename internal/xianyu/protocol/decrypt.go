package protocol

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Decrypt 解密闲鱼 WS 同步包载荷：base64 解码 → MessagePack 解码 → 归一化为 JSON 字符串。
//   - map 的键统一转为字符串（整数键 → 数字字符串）
//   - []byte（msgpack bin）→ UTF-8 字符串（忽略无效字节）
//   - 其余类型原样保留
//
// Decrypt 封装Decrypt业务协调。
func Decrypt(data string) (string, error) {
	// 清理非 ASCII。
	data = stripNonASCII(data)

	// base64 解码，必要时补 padding。
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		if // pad 用于本次流程后续判断的pad
		pad := len(data) % 4; pad != 0 {
			decoded, err = base64.StdEncoding.DecodeString(data + strings.Repeat("=", 4-pad))
		}
		if err != nil {
			return "", fmt.Errorf("解密失败: base64 解码: %w", err)
		}
	}

	// d 用于本次流程后续判断的d
	d := &msgpackDecoder{data: decoded}
	// val、err 用于本次流程后续判断的val、err
	val, err := d.decodeValue()
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}

	// normalized 用于本次流程后续判断的normalized
	normalized := normalizeForJSON(val)
	// b、err 用于本次流程后续判断的b、err
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("解密失败: JSON 序列化: %w", err)
	}
	return string(b), nil
}

// stripNonASCII 删除非 ASCII 字符。
func stripNonASCII(s string) string {
	// b 用于本次流程后续判断的b
	var b strings.Builder
	b.Grow(len(s))
	// r 表示当前遍历过程中的r
	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeForJSON 把 msgpack 解码出的任意结构转为 json.Marshal 友好的结构。
func normalizeForJSON(v any) any {
	switch // x 用于本次流程后续判断的x
	x := v.(type) {
	case map[any]any:
		// m 用于本次流程后续判断的m
		m := make(map[string]any, len(x))
		// k、val 表示当前遍历过程中的k、val
		for k, val := range x {
			m[keyToString(k)] = normalizeForJSON(val)
		}
		return m
	case []any:
		// out 用于本次流程后续判断的out
		out := make([]any, len(x))
		// i、e 表示当前遍历过程中的i、e
		for i, e := range x {
			out[i] = normalizeForJSON(e)
		}
		return out
	case []byte:
		// 忽略无效 UTF-8 字节。
		return strings.ToValidUTF8(string(x), "")
	default:
		return v
	}
}

// keyToString 将非字符串 map 键转换为 JSON 对象键。
func keyToString(k any) string {
	switch // x 用于本次流程后续判断的x
	x := k.(type) {
	case string:
		return x
	case int64:
		return strconv.FormatInt(x, 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	case bool:
		if x {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	case float64:
		return strconv.FormatFloat(x, 'g', -1, 64)
	case []byte:
		return strings.ToValidUTF8(string(x), "")
	default:
		return fmt.Sprintf("%v", k)
	}
}
