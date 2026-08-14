package renewal

import (
	"testing"
	"time"
)

// TestPasswordLoginAllowedDoesNotStartCooldown 负责Test密码登录AllowedDoesNot开始Cooldown相关处理。
func TestPasswordLoginAllowedDoesNotStartCooldown(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewCooldownManager()
	for // i 保存i，供当前处理流程使用
	i := 0; i < 2; i++ {
		// ok、remain、reason 保存ok、remain、reason，供当前处理流程使用
		ok, remain, reason := m.PasswordLoginAllowed("cid", 60*time.Second)
		if !ok || remain != 0 || reason != "" {
			t.Fatalf("check %d: ok=%v remain=%s reason=%q", i, ok, remain, reason)
		}
	}
}

// TestPasswordLoginCooldownStartsOnlyWhenMarked 负责Test密码登录CooldownStartsOnlyWhenMarked相关处理。
func TestPasswordLoginCooldownStartsOnlyWhenMarked(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewCooldownManager()
	m.MarkPasswordLogin("cid")
	// ok、remain、reason 保存ok、remain、reason，供当前处理流程使用
	ok, remain, reason := m.PasswordLoginAllowed("cid", 60*time.Second)
	if ok || remain <= 0 || remain > 60*time.Second || reason != "login_cooldown" {
		t.Fatalf("ok=%v remain=%s reason=%q", ok, remain, reason)
	}
}

// TestPasswordErrorCooldownReason 负责Test密码错误Cooldown原因相关处理。
func TestPasswordErrorCooldownReason(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := NewCooldownManager()
	m.MarkPasswordError("cid")
	// ok、remain、reason 保存ok、remain、reason，供当前处理流程使用
	ok, remain, reason := m.PasswordLoginAllowed("cid", 60*time.Second)
	if ok || remain <= 0 || reason != "password_error_cooldown" {
		t.Fatalf("ok=%v remain=%s reason=%q", ok, remain, reason)
	}
	m.Reset("cid")
	if // ok 保存ok，供当前处理流程使用
	ok, _, _ := m.PasswordLoginAllowed("cid", 60*time.Second); !ok {
		t.Fatal("Reset 后应解除冷却")
	}
}
