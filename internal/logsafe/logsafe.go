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
func ID(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(s))
	return hex.EncodeToString(sum[:])[:12]
}

// URL returns origin + path for URLs that may contain session tokens.
func URL(raw string) string {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "<redacted>"
	}
	return u.Scheme + "://" + u.Host + u.EscapedPath()
}
