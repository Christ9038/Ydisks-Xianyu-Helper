package renew

import (
	"bytes"
	"encoding/json"
	"strings"
)

// renewVerificationURL 判断 body 是否为“平台要求继续安全验证”的中间响应。
// 只有 iframeRedirect 为严格布尔 true 时才返回 true，字符串 "true" 不算；
// 返回的 URL 带一次性验证令牌，调用方只能放进内存登记，不得写日志或通知。
// 该响应在官方插件中同样不会 resolve，因此它永远不等同于续期成功。
func renewVerificationURL(body []byte) (string, bool) {
	// data 为响应中的业务结果对象。
	data := renewResponseData(body)
	if data == nil {
		return "", false
	}
	// required 表示平台是否声明需要额外验证。
	required, _ := data["iframeRedirect"].(bool)
	if !required {
		return "", false
	}
	// verificationURL 为平台给出的验证跳转地址，缺失时为空串但仍视为需要验证。
	verificationURL, _ := data["iframeRedirectUrl"].(string)
	return strings.TrimSpace(verificationURL), true
}

// redactVerificationBody 返回 body 的副本，其中所有 iframeRedirectUrl 字段被替换为占位符。
// 响应正文会进入续期日志，而验证地址带一次性令牌，不能落库；无法解析时返回占位提示而不是原文。
func redactVerificationBody(body []byte) []byte {
	// payload 保存解码后的任意 JSON 值，递归脱敏后重新编码。
	var payload any
	// err 是 json.Unmarshal 返回的失败原因，用于断言成功或被拒绝的路径。
	if err := json.Unmarshal(body, &payload); err != nil {
		return []byte(`{"redacted":"响应含安全验证地址，已隐藏"}`)
	}
	redactVerificationValue(payload)
	// buffer 接收编码结果；关闭 HTML 转义，使占位符在运行日志里保持可读而不是 <。
	var buffer bytes.Buffer
	// encoder 保存 json.NewEncoder 的结果，供后续分支和断言使用。
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	// err 是 encoder.Encode 返回的失败原因，用于断言成功或被拒绝的路径。
	if err := encoder.Encode(payload); err != nil {
		return []byte(`{"redacted":"响应含安全验证地址，已隐藏"}`)
	}
	return bytes.TrimRight(buffer.Bytes(), "\n")
}

// redactVerificationValue 原地遍历 value，把名为 iframeRedirectUrl 的非空字符串字段改写为占位符。
func redactVerificationValue(value any) {
	// typed 保存 value. 的结果，供后续分支和断言使用。
	switch typed := value.(type) {
	case map[string]any:
		// key、child 是当前对象的字段名与字段值。
		for key, child := range typed {
			if key == "iframeRedirectUrl" {
				// ok 标记 child. 取得的条目是否存在或调用是否成功；text 保存 child. 的结果，供后续分支和断言使用。
				if text, ok := child.(string); ok && text != "" {
					typed[key] = "<redacted>"
					continue
				}
			}
			redactVerificationValue(child)
		}
	case []any:
		// item 是当前数组元素。
		for _, item := range typed {
			redactVerificationValue(item)
		}
	}
}
