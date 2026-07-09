package engine

import (
	"crypto/rand"
	"math/big"
	"os"
	"strconv"
	"strings"
	"time"
)

const tokenCacheTTLGranularity = time.Second

func tokenCacheTTL() time.Duration {
	minTTL, minOK := tokenCacheTTLEnv("TOKEN_CACHE_TTL_MIN_HOURS", TokenCacheTTLMin)
	maxTTL, maxOK := tokenCacheTTLEnv("TOKEN_CACHE_TTL_MAX_HOURS", TokenCacheTTLMax)
	if !minOK || !maxOK || minTTL > maxTTL {
		minTTL = TokenCacheTTLMin
		maxTTL = TokenCacheTTLMax
	}
	if maxTTL == minTTL {
		return minTTL
	}
	steps := int64((maxTTL - minTTL) / tokenCacheTTLGranularity)
	if steps <= 0 {
		return minTTL
	}
	n, err := rand.Int(rand.Reader, big.NewInt(steps+1))
	if err != nil {
		return minTTL
	}
	return minTTL + time.Duration(n.Int64())*tokenCacheTTLGranularity
}

func tokenCacheTTLEnv(key string, fallback time.Duration) (time.Duration, bool) {
	raw := strings.TrimSpace(os.Getenv(key))
	if raw == "" {
		return fallback, true
	}
	hours, err := strconv.ParseFloat(raw, 64)
	if err != nil || hours <= 0 {
		return fallback, false
	}
	return time.Duration(hours * float64(time.Hour)), true
}
