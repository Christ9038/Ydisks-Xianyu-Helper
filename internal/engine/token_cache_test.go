package engine

import (
	"testing"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
)

// TestEffectiveTokenExpireAtUsesServerDeadlineWithSafetyMargin 负责TestEffective令牌ExpireAtUsesServerDeadlineWithSafetyMargin相关处理。
func TestEffectiveTokenExpireAtUsesServerDeadlineWithSafetyMargin(t *testing.T) {
	// now 保存now，供当前处理流程使用
	now := time.Unix(1_700_000_000, 0)
	// got 保存got，供当前处理流程使用
	got := effectiveTokenExpireAt(now.Add(2*time.Hour).Unix(), now)
	// want 保存want，供当前处理流程使用
	want := now.Add(2*time.Hour - tokenExpirySafetyMargin).Unix()
	if got != want {
		t.Fatalf("effective expiry=%d want=%d", got, want)
	}
}

// TestEffectiveTokenExpireAtRejectsMissingOrExpiredDeadline 负责TestEffective令牌ExpireAtRejectsMissingOrExpiredDeadline相关处理。
func TestEffectiveTokenExpireAtRejectsMissingOrExpiredDeadline(t *testing.T) {
	// now 保存now，供当前处理流程使用
	now := time.Unix(1_700_000_000, 0)
	// expireAt 表示当前遍历过程中的expireAt
	for _, expireAt := range []int64{0, now.Add(-time.Second).Unix()} {
		if // got 保存got，供当前处理流程使用
		got := effectiveTokenExpireAt(expireAt, now); got != 0 {
			t.Fatalf("effective expiry=%d want=0 for server expiry %d", got, expireAt)
		}
	}
}

// TestTokenRotationScheduleStartsBeforeServerExpiry 负责Test令牌RotationScheduleStartsBeforeServerExpiry相关处理。
func TestTokenRotationScheduleStartsBeforeServerExpiry(t *testing.T) {
	// now 保存now，供当前处理流程使用
	now := time.Unix(1_700_000_000, 0)
	// expiresAt、refreshAt 保存expiresAt、refreshAt，供当前处理流程使用
	expiresAt, refreshAt := tokenRotationSchedule(now.Add(time.Hour).Unix(), now)
	if !refreshAt.Before(expiresAt) || refreshAt != expiresAt.Add(-tokenRefreshLeadTime) {
		t.Fatalf("refresh_at=%s expires_at=%s", refreshAt, expiresAt)
	}
}

// TestTokenRotationScheduleUsesFallbackWhenExpiryMissing 负责Test令牌RotationScheduleUsesFallbackWhenExpiryMissing相关处理。
func TestTokenRotationScheduleUsesFallbackWhenExpiryMissing(t *testing.T) {
	// now 保存now，供当前处理流程使用
	now := time.Unix(1_700_000_000, 0)
	// expiresAt、refreshAt 保存expiresAt、refreshAt，供当前处理流程使用
	expiresAt, refreshAt := tokenRotationSchedule(0, now)
	if expiresAt != now.Add(tokenFallbackLifetime) || !refreshAt.Before(expiresAt) || !refreshAt.After(now) {
		t.Fatalf("fallback refresh_at=%s expires_at=%s", refreshAt, expiresAt)
	}
}

// TestCredentialCookieFingerprintPreservesHeaderOrderAndDuplicates 负责TestCredential登录凭证FingerprintPreservesHeader订单AndDuplicates相关处理。
func TestCredentialCookieFingerprintPreservesHeaderOrderAndDuplicates(t *testing.T) {
	// left 保存left，供当前处理流程使用
	left := credentialCookieFingerprint("unb=1; cookie2=abc; _m_h5_tk=tk_1")
	// right 保存right，供当前处理流程使用
	right := credentialCookieFingerprint("_m_h5_tk=tk_1; unb=1; cookie2=abc")
	if left == "" || left == right {
		t.Fatalf("Cookie Header 顺序变化必须改变指纹: %q == %q", left, right)
	}
	if left == credentialCookieFingerprint("unb=1; cookie2=changed; _m_h5_tk=tk_1") {
		t.Fatal("credential change must alter fingerprint")
	}
	if left != credentialCookieFingerprint(" unb = 1 ; cookie2=abc;_m_h5_tk = tk_1 ;") {
		t.Fatal("无语义空白不应改变指纹")
	}
	// firstPathToken 保存first路径令牌，供当前处理流程使用
	firstPathToken := credentialCookieFingerprint("_m_h5_tk=first; _m_h5_tk=second; unb=1")
	// secondPathToken 保存second路径令牌，供当前处理流程使用
	secondPathToken := credentialCookieFingerprint("_m_h5_tk=second; _m_h5_tk=first; unb=1")
	if firstPathToken == secondPathToken {
		t.Fatal("同名 Cookie 的路径顺序会影响首值签名，不能折叠")
	}
}

// TestCredentialStateFingerprintIncludesAuthoritativeSnapshot 负责TestCredential状态FingerprintIncludesAuthoritativeSnapshot相关处理。
func TestCredentialStateFingerprintIncludesAuthoritativeSnapshot(t *testing.T) {
	// flat 保存flat，供当前处理流程使用
	flat := "unb=1; _m_h5_tk=tk_1"
	// metadataA 保存metadataA，供当前处理流程使用
	metadataA := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "path-a", Domain: ".goofish.com", Path: "/"},
	})
	// metadataB 保存metadataB，供当前处理流程使用
	metadataB := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "path-b", Domain: ".goofish.com", Path: "/"},
	})
	if credentialStateFingerprint(flat, metadataA) == credentialStateFingerprint(flat, metadataB) {
		t.Fatal("权威 Jar 变化必须改变完整凭证指纹")
	}
	// emptyJar 保存emptyJar，供当前处理流程使用
	emptyJar := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{})
	if credentialStateFingerprint("", emptyJar) == credentialStateFingerprint("", "") {
		t.Fatal("权威空 Jar 必须与没有快照的历史状态区分")
	}
}

// TestTokenFingerprintIsStableNonReversibleDiagnosticID 负责Test令牌FingerprintIsStableNonReversibleDiagnosticID相关处理。
func TestTokenFingerprintIsStableNonReversibleDiagnosticID(t *testing.T) {
	// first 保存first，供当前处理流程使用
	first := tokenFingerprint("access-token-a")
	if first == "" || len(first) != 12 {
		t.Fatalf("token fingerprint=%q, want 12 hex chars", first)
	}
	if first != tokenFingerprint("access-token-a") {
		t.Fatal("same token must produce stable fingerprint")
	}
	if first == tokenFingerprint("access-token-b") {
		t.Fatal("different tokens must produce different fingerprints")
	}
	if first == "access-token-a" {
		t.Fatal("fingerprint must not expose the token")
	}
}
