package server

import (
	"testing"
	"time"
)

// TestLoginFailureLimiterScopesExpiresAndResets 封装Test登录FailureLimiterScopesExpiresAndResets业务协调。
func TestLoginFailureLimiterScopesExpiresAndResets(t *testing.T) {
	// limiter 用于本次流程后续判断的limiter
	limiter := newLoginFailureLimiter()
	limiter.perIP = 3
	limiter.perPrincipal = 2
	limiter.window = time.Minute
	// now 用于本次流程后续判断的now
	now := time.Unix(100, 0)

	limiter.failure("10.0.0.1", "admin", now)
	limiter.failure("10.0.0.1", "admin", now)
	if // allowed、retry 用于本次流程后续判断的allowed、retry
	allowed, retry := limiter.allow("10.0.0.2", "admin", now); allowed || retry != time.Minute {
		t.Fatalf("principal limit: allowed=%v retry=%v", allowed, retry)
	}
	if // allowed 用于本次流程后续判断的allowed
	allowed, _ := limiter.allow("10.0.0.1", "other", now); !allowed {
		t.Fatal("IP should remain below its independent limit")
	}
	limiter.failure("10.0.0.1", "other", now)
	if // allowed 用于本次流程后续判断的allowed
	allowed, _ := limiter.allow("10.0.0.1", "third", now); allowed {
		t.Fatal("IP limit should apply across principals")
	}

	limiter.success("10.0.0.1", "admin")
	if // allowed 用于本次流程后续判断的allowed
	allowed, _ := limiter.allow("10.0.0.1", "admin", now); !allowed {
		t.Fatal("successful authentication should reset matching buckets")
	}
	limiter.failure("10.0.0.3", "blocked", now)
	limiter.failure("10.0.0.3", "blocked", now)
	if // allowed 用于本次流程后续判断的allowed
	allowed, _ := limiter.allow("10.0.0.4", "blocked", now.Add(time.Minute)); !allowed {
		t.Fatal("expired failure window should allow a new attempt")
	}
}
