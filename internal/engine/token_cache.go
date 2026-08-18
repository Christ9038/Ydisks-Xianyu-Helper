package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// tokenExpirySafetyMargin 用于本次流程后续判断的令牌ExpirySafetyMargin
const tokenExpirySafetyMargin = time.Minute

// tokenFallbackLifetime 用于本次流程后续判断的令牌FallbackLifetime
const (
	tokenFallbackLifetime = 30 * time.Minute
	tokenRefreshLeadTime  = 10 * time.Minute
)

// effectiveTokenExpireAt 封装effective令牌ExpireAt业务协调。
func effectiveTokenExpireAt(serverExpireAt int64, now time.Time) int64 {
	// ttl 用于本次流程后续判断的ttl
	ttl := time.Unix(serverExpireAt, 0).Sub(now)
	if ttl <= 0 {
		return 0
	}
	// margin 用于本次流程后续判断的margin
	margin := tokenExpirySafetyMargin
	if ttl <= 2*margin {
		margin = ttl / 10
	}
	return now.Add(ttl - margin).Unix()
}

// tokenRotationSchedule 封装令牌RotationSchedule业务协调。
func tokenRotationSchedule(serverExpireAt int64, now time.Time) (expiresAt, refreshAt time.Time) {
	expiresAt = time.Unix(serverExpireAt, 0)
	if serverExpireAt <= now.Unix() {
		expiresAt = now.Add(tokenFallbackLifetime)
	}
	// ttl 用于本次流程后续判断的ttl
	ttl := expiresAt.Sub(now)
	// lead 用于本次流程后续判断的lead
	lead := ttl / 10
	if lead < tokenRefreshLeadTime {
		lead = tokenRefreshLeadTime
	}
	if lead >= ttl {
		lead = ttl / 2
	}
	refreshAt = expiresAt.Add(-lead)
	return expiresAt, refreshAt
}

// credentialCookieFingerprint 封装credential登录凭证Fingerprint业务协调。
func credentialCookieFingerprint(cookieStr string) string {
	// canonical 用于本次流程后续判断的canonical
	var canonical strings.Builder
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(cookieStr, ";") {
		// key、value、ok 用于本次流程后续判断的key、value、ok
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
	// sum 用于本次流程后续判断的sum
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}

// credentialStateFingerprint binds a connection token to both the exact flat
// Cookie header and the authoritative browser Jar that produced it. The flat
// fingerprint deliberately retains duplicate names and their order because
// lib-mtop reads the first path-ordered _m_h5_tk entry. A present empty Jar is
// distinct from legacy metadata that has no complete snapshot.
// credentialStateFingerprint 封装credential状态Fingerprint业务协调。
func credentialStateFingerprint(cookieStr, metadataJSON string) string {
	// canonical 用于本次流程后续判断的canonical
	var canonical strings.Builder
	canonical.WriteString("flat\x00")
	canonical.WriteString(credentialCookieFingerprint(cookieStr))
	canonical.WriteString("\x00snapshot\x00")
	if // snapshot、complete 用于本次流程后续判断的snapshot、complete
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(metadataJSON); complete {
		canonical.WriteByte('1')
		// raw 用于本次流程后续判断的原始
		raw, _ := json.Marshal(cookierefresh.NormalizeSnapshot(snapshot))
		canonical.Write(raw)
	} else {
		canonical.WriteByte('0')
	}
	// sum 用于本次流程后续判断的sum
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}
