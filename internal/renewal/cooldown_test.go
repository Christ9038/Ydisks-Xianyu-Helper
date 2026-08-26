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

// TestCooldownManagerSessionAndPasswordBranches 覆盖会话过期冷却、密码登录尝试和空接收者路径。
func TestCooldownManagerSessionAndPasswordBranches(t *testing.T) {
	// manager 保存本次冷却状态测试使用的管理器。
	manager := NewCooldownManager()
	// uncool、uncoolRemain 保存未标记账号的会话冷却结果。
	uncool, uncoolRemain := manager.IsSessionCooled("cid")
	if uncool || uncoolRemain != 0 {
		t.Fatalf("uncool=%v remain=%s", uncool, uncoolRemain)
	}
	manager.MarkSessionExpired("cid")
	// cooled、cooledRemain 保存会话过期后的冷却结果。
	cooled, cooledRemain := manager.IsSessionCooled("cid")
	if !cooled || cooledRemain <= 0 {
		t.Fatalf("cooled=%v remain=%s", cooled, cooledRemain)
	}
	// loginAllowed、loginRemain 保存首次密码登录尝试结果。
	loginAllowed, loginRemain := manager.TryPasswordLogin("new-cid")
	if !loginAllowed || loginRemain != 0 {
		t.Fatalf("loginAllowed=%v remain=%s", loginAllowed, loginRemain)
	}
	// loginBlocked、loginBlockedRemain 保存冷却期间的重复密码登录结果。
	loginBlocked, loginBlockedRemain := manager.TryPasswordLogin("new-cid")
	if loginBlocked || loginBlockedRemain <= 0 {
		t.Fatalf("loginBlocked=%v remain=%s", loginBlocked, loginBlockedRemain)
	}
	// nilManager 覆盖所有公开冷却方法对空接收者的安全语义。
	var nilManager *CooldownManager
	// nilCooled、nilRemain 保存空管理器会话冷却结果。
	nilCooled, nilRemain := nilManager.IsSessionCooled("cid")
	if nilCooled || nilRemain != 0 {
		t.Fatalf("nil session=%v remain=%s", nilCooled, nilRemain)
	}
	// nilAllowed、nilLoginRemain、nilReason 保存空管理器密码登录检查结果。
	nilAllowed, nilLoginRemain, nilReason := nilManager.PasswordLoginAllowed("cid", time.Second)
	if !nilAllowed || nilLoginRemain != 0 || nilReason != "" {
		t.Fatalf("nil password=%v remain=%s reason=%q", nilAllowed, nilLoginRemain, nilReason)
	}
	nilManager.MarkSessionExpired("cid")
	nilManager.MarkPasswordLogin("cid")
	nilManager.MarkPasswordError("cid")
	nilManager.Reset("cid")
}
