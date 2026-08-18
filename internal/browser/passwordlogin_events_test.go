package browser

import (
	"errors"
	"testing"
)

// TestPasswordLoginEventFromErrorVerification 封装Test密码登录EventFrom错误Verification业务协调。
func TestPasswordLoginEventFromErrorVerification(t *testing.T) {
	// event 用于本次流程后续判断的event
	event := PasswordLoginEventFromError(errors.New("需要人脸验证"))
	if event.Status != PasswordLoginStatusVerificationRequired {
		t.Fatalf("status=%q want verification_required", event.Status)
	}
}

// TestPasswordLoginEventFromErrorBaxia 封装Test密码登录EventFrom错误Baxia业务协调。
func TestPasswordLoginEventFromErrorBaxia(t *testing.T) {
	// event 用于本次流程后续判断的event
	event := PasswordLoginEventFromError(errors.New("baxia-punish verification 风控图形验证"))
	if event.Status != PasswordLoginStatusFailed || event.Reason != "baxia_punish_captcha" || event.CooldownHours != 5 {
		t.Fatalf("event=%+v", event)
	}
}

// TestIsBaxiaPunishMessage 封装TestIsBaxiaPunish消息业务协调。
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
