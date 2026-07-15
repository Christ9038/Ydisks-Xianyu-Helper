package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/protocol"
)

const tokenExpirySafetyMargin = time.Minute

func effectiveTokenExpireAt(serverExpireAt int64, now time.Time) int64 {
	ttl := time.Unix(serverExpireAt, 0).Sub(now)
	if ttl <= 0 {
		return 0
	}
	margin := tokenExpirySafetyMargin
	if ttl <= 2*margin {
		margin = ttl / 10
	}
	return now.Add(ttl - margin).Unix()
}

func credentialCookieFingerprint(cookieStr string) string {
	cookies := protocol.TransCookies(cookieStr)
	keys := make([]string, 0, len(cookies))
	for key := range cookies {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var canonical strings.Builder
	for _, key := range keys {
		canonical.WriteString(key)
		canonical.WriteByte(0)
		canonical.WriteString(cookies[key])
		canonical.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}
