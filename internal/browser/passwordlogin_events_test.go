package browser

import (
	"errors"
	"testing"
)

// TestPasswordLoginEventFromErrorVerification 负责Test密码登录EventFrom错误Verification相关处理。
func TestPasswordLoginEventFromErrorVerification(t *testing.T) {
	// event 保存event，供当前处理流程使用
	event := PasswordLoginEventFromError(errors.New("需要人脸验证"))
	if event.Status != PasswordLoginStatusVerificationRequired {
		t.Fatalf("status=%q want verification_required", event.Status)
	}
}

// TestPasswordLoginEventFromErrorBaxia 负责Test密码登录EventFrom错误Baxia相关处理。
func TestPasswordLoginEventFromErrorBaxia(t *testing.T) {
	// event 保存event，供当前处理流程使用
	event := PasswordLoginEventFromError(errors.New("baxia-punish verification 风控图形验证"))
	if event.Status != PasswordLoginStatusFailed || event.Reason != "baxia_punish_captcha" || event.CooldownHours != 5 {
		t.Fatalf("event=%+v", event)
	}
}

// TestIsBaxiaPunishMessage 负责TestIsBaxiaPunish消息相关处理。
func TestIsBaxiaPunishMessage(t *testing.T) {
	// msg 表示当前遍历过程中的msg
	for _, msg := range []string{"baxia-punish", "scratch-captcha-container", "找两个松鼠"} {
		if !IsBaxiaPunishMessage(msg) {
			t.Fatalf("%q should be recognized as baxia punish", msg)
		}
	}
	if PasswordLoginEventFromMessage("触发风控图形验证").Reason != "baxia_punish_captcha" {
		t.Fatal("明确的风控图形验证应按 baxia 冷却")
	}
	if IsBaxiaPunishMessage("用户名或密码错误") {
		t.Fatal("ordinary password error should not be recognized as baxia")
	}
	if IsBaxiaPunishMessage("触发风控，需要人脸验证") {
		t.Fatal("ordinary face verification should not be recognized as baxia")
	}
}
