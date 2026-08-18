package renewal

import (
	"testing"
	"time"
)

// TestPasswordLoginAllowedDoesNotStartCooldown 封装Test密码登录AllowedDoesNot开始Cooldown业务协调。
func TestPasswordLoginAllowedDoesNotStartCooldown(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := NewCooldownManager()
	for // i 用于本次流程后续判断的i
	i := 0; i < 2; i++ {
		// ok、remain、reason 用于本次流程后续判断的ok、remain、reason
		ok, remain, reason := m.PasswordLoginAllowed("cid", 60*time.Second)
		if !ok || remain != 0 || reason != "" {
			t.Fatalf("check %d: ok=%v remain=%s reason=%q", i, ok, remain, reason)
		}
	}
}

// TestPasswordLoginCooldownStartsOnlyWhenMarked 封装Test密码登录CooldownStartsOnlyWhenMarked业务协调。
func TestPasswordLoginCooldownStartsOnlyWhenMarked(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := NewCooldownManager()
	m.MarkPasswordLogin("cid")
	// ok、remain、reason 用于本次流程后续判断的ok、remain、reason
	ok, remain, reason := m.PasswordLoginAllowed("cid", 60*time.Second)
	if ok || remain <= 0 || remain > 60*time.Second || reason != "login_cooldown" {
		t.Fatalf("ok=%v remain=%s reason=%q", ok, remain, reason)
	}
}

// TestPasswordErrorCooldownReason 封装Test密码错误Cooldown原因业务协调。
func TestPasswordErrorCooldownReason(t *testing.T) {
	// m 用于本次流程后续判断的m
	m := NewCooldownManager()
	m.MarkPasswordError("cid")
	// ok、remain、reason 用于本次流程后续判断的ok、remain、reason
	ok, remain, reason := m.PasswordLoginAllowed("cid", 60*time.Second)
	if ok || remain <= 0 || reason != "password_error_cooldown" {
		t.Fatalf("ok=%v remain=%s reason=%q", ok, remain, reason)
	}
	m.Reset("cid")
	if // ok 用于本次流程后续判断的ok
	ok, _, _ := m.PasswordLoginAllowed("cid", 60*time.Second); !ok {
		t.Fatal("Reset 后应解除冷却")
	}
}
