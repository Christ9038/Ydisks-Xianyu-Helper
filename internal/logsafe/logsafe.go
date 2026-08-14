// Package logsafe contains helpers for logging identifiers without leaking
// account tokens, verification URLs, or full platform IDs.
package logsafe

import (
	"crypto/sha256"
	"encoding/hex"
	"net/url"
	"strings"
)

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
