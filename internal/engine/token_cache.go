package engine

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"strings"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// tokenExpirySafetyMargin 保存令牌ExpirySafetyMargin，供当前处理流程使用
const tokenExpirySafetyMargin = time.Minute

// tokenFallbackLifetime 保存令牌FallbackLifetime，供当前处理流程使用
const (
	tokenFallbackLifetime = 30 * time.Minute
	tokenRefreshLeadTime  = 10 * time.Minute
)

// effectiveTokenExpireAt 负责effective令牌ExpireAt相关处理。
func effectiveTokenExpireAt(serverExpireAt int64, now time.Time) int64 {
	// ttl 保存ttl，供当前处理流程使用
	ttl := time.Unix(serverExpireAt, 0).Sub(now)
	if ttl <= 0 {
		return 0
	}
	// margin 保存margin，供当前处理流程使用
	margin := tokenExpirySafetyMargin
	if ttl <= 2*margin {
		margin = ttl / 10
	}
	return now.Add(ttl - margin).Unix()
}

// tokenRotationSchedule 负责令牌RotationSchedule相关处理。
func tokenRotationSchedule(serverExpireAt int64, now time.Time) (expiresAt, refreshAt time.Time) {
	expiresAt = time.Unix(serverExpireAt, 0)
	if serverExpireAt <= now.Unix() {
		expiresAt = now.Add(tokenFallbackLifetime)
	}
	// ttl 保存ttl，供当前处理流程使用
	ttl := expiresAt.Sub(now)
	// lead 保存lead，供当前处理流程使用
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

// credentialCookieFingerprint 负责credential登录凭证Fingerprint相关处理。
func credentialCookieFingerprint(cookieStr string) string {
	// canonical 保存canonical，供当前处理流程使用
	var canonical strings.Builder
	// part 表示当前遍历过程中的part
	for _, part := range strings.Split(cookieStr, ";") {
		// key、value、ok 保存key、value、ok，供当前处理流程使用
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
	// sum 保存sum，供当前处理流程使用
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}

// credentialStateFingerprint binds a connection token to both the exact flat
// Cookie header and the authoritative browser Jar that produced it. The flat
// fingerprint deliberately retains duplicate names and their order because
// lib-mtop reads the first path-ordered _m_h5_tk entry. A present empty Jar is
// distinct from legacy metadata that has no complete snapshot.
// credentialStateFingerprint 负责credential状态Fingerprint相关处理。
func credentialStateFingerprint(cookieStr, metadataJSON string) string {
	// canonical 保存canonical，供当前处理流程使用
	var canonical strings.Builder
	canonical.WriteString("flat\x00")
	canonical.WriteString(credentialCookieFingerprint(cookieStr))
	canonical.WriteString("\x00snapshot\x00")
	if // snapshot、complete 保存snapshot、complete，供当前处理流程使用
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(metadataJSON); complete {
		canonical.WriteByte('1')
		// raw 保存原始，供当前处理流程使用
		raw, _ := json.Marshal(cookierefresh.NormalizeSnapshot(snapshot))
		canonical.Write(raw)
	} else {
		canonical.WriteByte('0')
	}
	// sum 保存sum，供当前处理流程使用
	sum := sha256.Sum256([]byte(canonical.String()))
	return hex.EncodeToString(sum[:])
}
