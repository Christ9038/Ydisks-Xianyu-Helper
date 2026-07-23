package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
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
	var canonical strings.Builder
	for _, part := range strings.Split(cookieStr, ";") {
		key, value, ok := strings.Cut(strings.TrimSpace(part), "=")
		key = strings.TrimSpace(key)
		if !ok || key == "" {
			continue
		}
		canonical.WriteString(key)
		canonical.WriteByte(0)
		canonical.WriteString(strings.TrimSpace(value))
		canonical.WriteByte(0)
	}
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}

// credentialStateFingerprint binds a connection token to both the exact flat
// Cookie header and the authoritative browser Jar that produced it. The flat
// fingerprint deliberately retains duplicate names and their order because
// lib-mtop reads the first path-ordered _m_h5_tk entry. A present empty Jar is
// distinct from legacy metadata that has no complete snapshot.
func credentialStateFingerprint(cookieStr, metadataJSON string) string {
	var canonical strings.Builder
	canonical.WriteString("flat\x00")
	canonical.WriteString(credentialCookieFingerprint(cookieStr))
	canonical.WriteString("\x00snapshot\x00")
	if snapshot, complete := cookierefresh.SnapshotFromMetadataOK(metadataJSON); complete {
		canonical.WriteByte('1')
		raw, _ := json.Marshal(cookierefresh.NormalizeSnapshot(snapshot))
		canonical.Write(raw)
	} else {
		canonical.WriteByte('0')
	}
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}
