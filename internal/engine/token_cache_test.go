package engine

import (
	"testing"
	"time"

	"xianyu-go/internal/xianyu/cookierefresh"
)

func TestEffectiveTokenExpireAtUsesServerDeadlineWithSafetyMargin(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	got := effectiveTokenExpireAt(now.Add(2*time.Hour).Unix(), now)
	want := now.Add(2*time.Hour - tokenExpirySafetyMargin).Unix()
	if got != want {
		t.Fatalf("effective expiry=%d want=%d", got, want)
	}
}

func TestEffectiveTokenExpireAtRejectsMissingOrExpiredDeadline(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	for _, expireAt := range []int64{0, now.Add(-time.Second).Unix()} {
		if got := effectiveTokenExpireAt(expireAt, now); got != 0 {
			t.Fatalf("effective expiry=%d want=0 for server expiry %d", got, expireAt)
		}
	}
}

func TestTokenRotationScheduleStartsBeforeServerExpiry(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	expiresAt, refreshAt := tokenRotationSchedule(now.Add(time.Hour).Unix(), now)
	if !refreshAt.Before(expiresAt) || refreshAt != expiresAt.Add(-tokenRefreshLeadTime) {
		t.Fatalf("refresh_at=%s expires_at=%s", refreshAt, expiresAt)
	}
}

func TestTokenRotationScheduleUsesFallbackWhenExpiryMissing(t *testing.T) {
	now := time.Unix(1_700_000_000, 0)
	expiresAt, refreshAt := tokenRotationSchedule(0, now)
	if expiresAt != now.Add(tokenFallbackLifetime) || !refreshAt.Before(expiresAt) || !refreshAt.After(now) {
		t.Fatalf("fallback refresh_at=%s expires_at=%s", refreshAt, expiresAt)
	}
}

func TestCredentialCookieFingerprintPreservesHeaderOrderAndDuplicates(t *testing.T) {
	left := credentialCookieFingerprint("unb=1; cookie2=abc; _m_h5_tk=tk_1")
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
	firstPathToken := credentialCookieFingerprint("_m_h5_tk=first; _m_h5_tk=second; unb=1")
	secondPathToken := credentialCookieFingerprint("_m_h5_tk=second; _m_h5_tk=first; unb=1")
	if firstPathToken == secondPathToken {
		t.Fatal("同名 Cookie 的路径顺序会影响首值签名，不能折叠")
	}
}

func TestCredentialStateFingerprintIncludesAuthoritativeSnapshot(t *testing.T) {
	flat := "unb=1; _m_h5_tk=tk_1"
	metadataA := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "path-a", Domain: ".goofish.com", Path: "/"},
	})
	metadataB := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{
		{Name: "_m_h5_tk", Value: "path-b", Domain: ".goofish.com", Path: "/"},
	})
	if credentialStateFingerprint(flat, metadataA) == credentialStateFingerprint(flat, metadataB) {
		t.Fatal("权威 Jar 变化必须改变完整凭证指纹")
	}
	emptyJar := cookierefresh.MetadataWithSnapshot("", []cookierefresh.BrowserCookie{})
	if credentialStateFingerprint("", emptyJar) == credentialStateFingerprint("", "") {
		t.Fatal("权威空 Jar 必须与没有快照的历史状态区分")
	}
}
