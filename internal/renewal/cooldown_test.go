package renewal

import (
	"testing"
	"time"
)

func TestPasswordLoginAllowedDoesNotStartCooldown(t *testing.T) {
	m := NewCooldownManager()
	for i := 0; i < 2; i++ {
		ok, remain, reason := m.PasswordLoginAllowed("cid", 60*time.Second)
		if !ok || remain != 0 || reason != "" {
			t.Fatalf("check %d: ok=%v remain=%s reason=%q", i, ok, remain, reason)
		}
	}
}

func TestPasswordLoginCooldownStartsOnlyWhenMarked(t *testing.T) {
	m := NewCooldownManager()
	m.MarkPasswordLogin("cid")
	ok, remain, reason := m.PasswordLoginAllowed("cid", 60*time.Second)
	if ok || remain <= 0 || remain > 60*time.Second || reason != "login_cooldown" {
		t.Fatalf("ok=%v remain=%s reason=%q", ok, remain, reason)
	}
}

func TestPasswordErrorCooldownReason(t *testing.T) {
	m := NewCooldownManager()
	m.MarkPasswordError("cid")
	ok, remain, reason := m.PasswordLoginAllowed("cid", 60*time.Second)
	if ok || remain <= 0 || reason != "password_error_cooldown" {
		t.Fatalf("ok=%v remain=%s reason=%q", ok, remain, reason)
	}
	m.Reset("cid")
	if ok, _, _ := m.PasswordLoginAllowed("cid", 60*time.Second); !ok {
		t.Fatal("Reset 后应解除冷却")
	}
}
