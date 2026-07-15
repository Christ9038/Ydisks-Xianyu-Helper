package engine

import (
	"testing"
	"time"
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

func TestCredentialCookieFingerprintIsOrderIndependent(t *testing.T) {
	left := credentialCookieFingerprint("unb=1; cookie2=abc; _m_h5_tk=tk_1")
	right := credentialCookieFingerprint("_m_h5_tk=tk_1; unb=1; cookie2=abc")
	if left == "" || left != right {
		t.Fatalf("fingerprints differ: %q != %q", left, right)
	}
	if left == credentialCookieFingerprint("unb=1; cookie2=changed; _m_h5_tk=tk_1") {
		t.Fatal("credential change must alter fingerprint")
	}
}
