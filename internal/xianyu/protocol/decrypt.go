package protocol

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// Decrypt 解密闲鱼 WS 同步包载荷：base64 解码 → MessagePack 解码 → 归一化为 JSON 字符串。
// 对应 Python decrypt(data)。归一化规则与 Python json.dumps(default=bytes→utf-8) 一致：
//   - map 的键统一转为字符串（整数键 → 数字字符串，与 Python json 行为一致）
//   - []byte（msgpack bin）→ UTF-8 字符串（忽略无效字节）
//   - 其余类型原样保留
func Decrypt(data string) (string, error) {
	// 清理非 ASCII（与 Python 一致：utf-8 编码后按 ascii 忽略解码）。
	data = stripNonASCII(data)

	// base64 解码，必要时补 padding。
	decoded, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		if pad := len(data) % 4; pad != 0 {
			decoded, err = base64.StdEncoding.DecodeString(data + strings.Repeat("=", 4-pad))
		}
		if err != nil {
			return "", fmt.Errorf("解密失败: base64 解码: %w", err)
		}
	}

	d := &msgpackDecoder{data: decoded}
	val, err := d.decodeValue()
	if err != nil {
		return "", fmt.Errorf("解密失败: %w", err)
	}

	normalized := normalizeForJSON(val)
	b, err := json.Marshal(normalized)
	if err != nil {
		return "", fmt.Errorf("解密失败: JSON 序列化: %w", err)
	}
	return string(b), nil
}

// stripNonASCII 模拟 Python data.encode('utf-8','ignore').decode('ascii','ignore')。
func stripNonASCII(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if r < 0x80 {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// normalizeForJSON 把 msgpack 解码出的任意结构转为 json.Marshal 友好的结构，
// 并复刻 Python json.dumps 的键/字节处理。
func normalizeForJSON(v any) any {
	switch x := v.(type) {
	case map[any]any:
		m := make(map[string]any, len(x))
		for k, val := range x {
			m[keyToString(k)] = normalizeForJSON(val)
		}
		return m
	case []any:
		out := make([]any, len(x))
		for i, e := range x {
			out[i] = normalizeForJSON(e)
		}
		return out
	case []byte:
		// Python default 序列化器：bytes.decode('utf-8','ignore')
		return strings.ToValidUTF8(string(x), "")
	default:
		return v
	}
}

// keyToString 复刻 Python json 对非字符串键的转换。
func keyToString(k any) string {
	switch x := k.(type) {
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
