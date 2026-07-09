package engine

import (
	"testing"
	"time"
)

func TestTokenCacheTTLFixedEnv(t *testing.T) {
	t.Setenv("TOKEN_CACHE_TTL_MIN_HOURS", "2")
	t.Setenv("TOKEN_CACHE_TTL_MAX_HOURS", "2")
	if got := tokenCacheTTL(); got != 2*time.Hour {
		t.Fatalf("tokenCacheTTL=%s want 2h", got)
	}
}

func TestTokenCacheTTLMaxBelowMinFallsBackToDefaultRange(t *testing.T) {
	t.Setenv("TOKEN_CACHE_TTL_MIN_HOURS", "4")
	t.Setenv("TOKEN_CACHE_TTL_MAX_HOURS", "2")
	got := tokenCacheTTL()
	if got < TokenCacheTTLMin || got > TokenCacheTTLMax {
		t.Fatalf("tokenCacheTTL=%s want within [%s,%s]", got, TokenCacheTTLMin, TokenCacheTTLMax)
	}
}

func TestTokenCacheTTLInvalidEnvFallsBackToDefaultRange(t *testing.T) {
	t.Setenv("TOKEN_CACHE_TTL_MIN_HOURS", "bad")
	t.Setenv("TOKEN_CACHE_TTL_MAX_HOURS", "also-bad")
	got := tokenCacheTTL()
	if got < TokenCacheTTLMin || got > TokenCacheTTLMax {
		t.Fatalf("tokenCacheTTL=%s want within [%s,%s]", got, TokenCacheTTLMin, TokenCacheTTLMax)
	}
}

func TestTokenCacheTTLPartiallyInvalidEnvFallsBackToDefaultRange(t *testing.T) {
	t.Setenv("TOKEN_CACHE_TTL_MIN_HOURS", "2")
	t.Setenv("TOKEN_CACHE_TTL_MAX_HOURS", "bad")
	got := tokenCacheTTL()
	if got < TokenCacheTTLMin || got > TokenCacheTTLMax {
		t.Fatalf("tokenCacheTTL=%s want within [%s,%s]", got, TokenCacheTTLMin, TokenCacheTTLMax)
	}
}
