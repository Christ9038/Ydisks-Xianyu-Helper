// Package logsafe contains helpers for logging identifiers without leaking
// account tokens, verification URLs, or full platform IDs.
package logsafe

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"regexp"
	"strings"
)

// sensitiveValuePattern 匹配诊断文本中常见的凭证键值对，避免错误信息把明文秘密带入日志。
var sensitiveValuePattern = regexp.MustCompile(`(?i)(\b(?:cookie|set-cookie|x5sec|token|access[_-]?token|refresh[_-]?token|password|passwd|secret|api[_-]?key|authorization)\b\s*[:=]\s*)(?:"[^"]*"|'[^']*'|[^\s,;]+)`)

// embeddedURLPattern 匹配错误文本中可能包含查询参数的 URL。
var embeddedURLPattern = regexp.MustCompile(`(?i)\b(?:https?|wss?|mysql|postgres(?:ql)?):\/\/[^\s"'<>]+`)

// quotedRequestTargetPattern 匹配 URL 解析器和 HTTP 客户端错误中可能不具备合法协议的原始请求地址。
var quotedRequestTargetPattern = regexp.MustCompile(`(?i)\b(?:parse|connect|request|get|post|put|patch|delete|head|options)\s+"[^"\r\n]*"`)

// ID returns a short stable fingerprint for a sensitive identifier.
// ID 封装标识业务协调。
func ID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// sum 用于本次流程后续判断的sum
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

// URL returns origin + path for URLs that may contain session tokens.
// URL 封装URL业务协调。
func URL(raw string) string {
	// u、err 用于本次流程后续判断的u、err
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "<redacted>"
	}
	return u.Scheme + "://" + u.Host + u.EscapedPath()
}

// Error 返回适合写入日志的错误文本；其中的 URL 查询、用户信息和常见凭证键值会被移除。
// 调用方仍可保留错误的业务上下文，但不得把返回值当作用户可见的原始错误。
func Error(err error) string {
	if err == nil {
		return ""
	}
	return Text(err.Error())
}

// ExternalError 返回适合记录外部网络失败的诊断文本；所有嵌入 URL 仅保留源站，防止路径型 Token 或 Webhook 密钥泄漏。
// err 是可能由 HTTP 客户端包装完整请求地址的底层错误；返回值可用于日志、持久化诊断和内部错误包装。
func ExternalError(err error) string {
	if err == nil {
		return ""
	}
	// sanitizedTarget 保存已清理 HTTP 方法或 URL 解析器后方引用请求地址的错误文本。
	sanitizedTarget := quotedRequestTargetPattern.ReplaceAllStringFunc(err.Error(), externalQuotedTarget)
	// sanitized 保存已移除其余外部 URL 用户信息、路径、查询参数和片段的错误文本。
	sanitized := embeddedURLPattern.ReplaceAllStringFunc(sanitizedTarget, externalURLOrigin)
	return sensitiveValuePattern.ReplaceAllString(sanitized, `${1}<redacted>`)
}

// externalQuotedTarget 清理外部错误中由动作前缀和双引号包裹的请求地址。
// raw 是完整的“动作 + 地址”片段；返回值保留动作与安全源站，无效地址只保留统一占位符。
func externalQuotedTarget(raw string) string {
	// separator 保存动作前缀与引号地址之间最后一个空格的位置。
	separator := strings.IndexByte(raw, ' ')
	if separator < 0 {
		return "<redacted>"
	}
	// action 保存 parse 或 HTTP 方法，便于保留错误发生阶段。
	action := raw[:separator]
	// target 保存移除双引号后的原始请求地址，仅交给安全源站转换函数处理。
	target := strings.Trim(raw[separator+1:], `"`)
	return action + ` "` + externalURLOrigin(target) + `"`
}

// externalURLOrigin 将外部错误里的完整 URL 收敛为不含凭证路径的源站。
// raw 是正则匹配出的完整 URL；返回值只包含协议和主机，解析失败时返回统一占位符。
func externalURLOrigin(raw string) string {
	// parsed、parseErr 保存待清理 URL 的结构化结果和解析错误。
	parsed, parseErr := url.Parse(strings.TrimSpace(raw))
	if parseErr != nil || parsed.Scheme == "" || parsed.Host == "" {
		return "<redacted>"
	}
	return parsed.Scheme + "://" + parsed.Host + "/<redacted>"
}

// Text 清理诊断文本中的 URL 查询参数和常见敏感键值；普通业务文字保持原样。
func Text(raw string) string {
	// sanitized 保存已移除 URL 查询和凭证值的诊断文本。
	sanitized := embeddedURLPattern.ReplaceAllStringFunc(raw, URL)
	return sensitiveValuePattern.ReplaceAllString(sanitized, `${1}<redacted>`)
}
