package ws

import (
	"fmt"
	"sort"
	"strings"

	"xianyu-go/internal/logsafe"
)

// sendReasonMaxRunes 是写入错误文本的平台原因最大字符数，防止异常响应撑爆日志。
const sendReasonMaxRunes = 200

// sendReasonKeys 是平台拒绝响应里可能携带人类可读原因的字段名，按优先级排列。
var sendReasonKeys = []string{"reason", "msg", "message", "errorMsg", "errMsg", "error", "desc", "text"}

// extractSendReason 从聊天发送响应的 headers 与 body 中提取平台拒绝原因，并完成脱敏与截断。
// response 是平台原始应答；返回值只含脱敏后的单行文本，找不到原因字段时退化为各节点的字段名列表（不含字段值），
// 以便运维据此判断平台实际返回结构。绝不返回消息正文、Cookie 或 Token。
func extractSendReason(response map[string]any) string {
	// headers 保存应答头节点；类型不符时按缺失处理。
	headers, _ := response["headers"].(map[string]any)
	// body 保存应答正文节点；类型不符时按缺失处理。
	body, _ := response["body"].(map[string]any)
	// node 是当前检查的节点，按 headers、body 顺序查找。
	for _, node := range []map[string]any{headers, body} {
		// key 是候选原因字段名。
		for _, key := range sendReasonKeys {
			// value 是该字段的字符串值，ok 表示字段存在且为字符串；仅接受非空白值。
			if value, ok := node[key].(string); ok && strings.TrimSpace(value) != "" {
				return sanitizeSendReason(key + "=" + value)
			}
		}
	}
	return sanitizeSendReason("无原因字段 " + describeKeys("headers", headers) + " " + describeKeys("body", body))
}

// describeKeys 返回节点的字段名清单（排序后），用于在平台未给出原因时留下结构线索。
// name 是节点名；node 是待列举的节点，为空时返回 name=[]。
func describeKeys(name string, node map[string]any) string {
	// keys 保存排序后的字段名，保证日志稳定可比对。
	keys := make([]string, 0, len(node))
	// key 是节点中的字段名。
	for key := range node {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return fmt.Sprintf("%s=[%s]", name, strings.Join(keys, ","))
}

// sanitizeSendReason 将原因折叠为单行、移除凭证与 URL 查询，并按字符数截断。
// raw 是未经处理的平台文本；返回值可安全写入日志与错误文本。
func sanitizeSendReason(raw string) string {
	// singleLine 去掉换行与连续空白，避免日志注入和多行污染。
	singleLine := strings.Join(strings.Fields(raw), " ")
	// safe 是移除常见凭证键值和 URL 查询后的文本。
	safe := logsafe.Text(singleLine)
	// runes 用于按字符而非字节截断中文。
	runes := []rune(safe)
	if len(runes) > sendReasonMaxRunes {
		return string(runes[:sendReasonMaxRunes]) + "…"
	}
	return safe
}
