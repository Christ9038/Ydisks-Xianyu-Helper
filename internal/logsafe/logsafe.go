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

// ID returns a short stable fingerprint for a sensitive identifier.
// ID 负责标识相关处理。
func ID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	// sum 保存sum，供当前处理流程使用
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

// URL returns origin + path for URLs that may contain session tokens.
// URL 负责URL相关处理。
func URL(raw string) string {
	// u、err 保存u、err，供当前处理流程使用
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

// Text 清理诊断文本中的 URL 查询参数和常见敏感键值；普通业务文字保持原样。
func Text(raw string) string {
	// sanitized 保存已移除 URL 查询和凭证值的诊断文本。
	sanitized := embeddedURLPattern.ReplaceAllStringFunc(raw, URL)
	return sensitiveValuePattern.ReplaceAllString(sanitized, `${1}<redacted>`)
}
