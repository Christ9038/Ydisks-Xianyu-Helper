package browser

import "testing"

func TestDetectPasswordBaxiaPunishHTML(t *testing.T) {
	html := `<div id="baxia-punish"><div class="captcha-question">请找两个松鼠</div></div>`
	event, ok := detectPasswordBaxiaPunishHTML(html)
	if !ok {
		t.Fatal("应识别 baxia 图形验证")
	}
	if event.Status != PasswordLoginStatusFailed || event.Reason != "baxia_punish_captcha" || event.CooldownHours != 5 {
		t.Fatalf("baxia 事件异常: %+v", event)
	}
}

func TestPasswordEventFromMessageDoesNotTreatFaceRiskAsBaxia(t *testing.T) {
	event := PasswordLoginEventFromMessage("账号触发风控，需要人脸验证")
	if event.Reason == "baxia_punish_captcha" {
		t.Fatalf("普通人脸验证不应按 baxia 冷却: %+v", event)
	}
	if event.Status != PasswordLoginStatusVerificationRequired {
		t.Fatalf("人脸验证应标记 verification_required: %+v", event)
	}
}

func TestDetectPasswordLoginErrorHTML(t *testing.T) {
	msg := detectPasswordLoginErrorHTML(`<div class="login-error-msg">账号或密码错误</div>`)
	if msg != "账号或密码错误" {
		t.Fatalf("登录错误识别=%q", msg)
	}
	msg = detectPasswordLoginErrorHTML(`<span>账号已被冻结，请联系平台</span>`)
	if msg != "账号已被冻结" {
		t.Fatalf("冻结错误识别=%q", msg)
	}
}

func TestDetectPasswordVerificationHTML(t *testing.T) {
	html := `<iframe id="alibaba-login-box" src="https:\/\/passport.goofish.com\/iv\/photoVerify\/index.htm?token=abc"></iframe><div>需要人脸验证，请使用手机扫码</div>`
	event, ok := detectPasswordVerificationHTML(html)
	if !ok {
		t.Fatal("应识别人脸验证")
	}
	if event.Status != PasswordLoginStatusVerificationRequired {
		t.Fatalf("验证状态异常: %+v", event)
	}
	if event.VerificationURL != "https://passport.goofish.com/iv/photoVerify/index.htm?token=abc" {
		t.Fatalf("验证 URL 提取异常: %q", event.VerificationURL)
	}
}

func TestQuickEnterCookiesUsableRequiresUNB(t *testing.T) {
	if quickEnterCookiesUsable(map[string]string{"_m_h5_tk": "tk"}) {
		t.Fatal("快速进入未拿到 unb 不应视为成功")
	}
	if !quickEnterCookiesUsable(map[string]string{"unb": " 123 "}) {
		t.Fatal("快速进入拿到 unb 应视为成功")
	}
	if quickEnterCookiesUsable(nil) {
		t.Fatal("空 Cookie 不应视为成功")
	}
}
