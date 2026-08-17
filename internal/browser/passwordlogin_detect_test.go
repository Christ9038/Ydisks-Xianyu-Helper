package browser

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestDetectPasswordBaxiaPunishHTML 负责TestDetect密码BaxiaPunishHTML相关处理。
func TestDetectPasswordBaxiaPunishHTML(t *testing.T) {
	// html 保存html，供当前处理流程使用
	html := `<div id="baxia-punish"><div class="captcha-question">请找两个松鼠</div></div>`
	// event、ok 保存event、ok，供当前处理流程使用
	event, ok := detectPasswordBaxiaPunishHTML(html)
	if !ok {
		t.Fatal("应识别 baxia 图形验证")
	}
	if event.Status != PasswordLoginStatusFailed || event.Reason != "baxia_punish_captcha" || event.CooldownHours != 5 {
		t.Fatalf("baxia 事件异常: %+v", event)
	}
}

// TestPasswordEventFromMessageDoesNotTreatFaceRiskAsBaxia 负责Test密码EventFrom消息DoesNotTreatFaceRiskAsBaxia相关处理。
func TestPasswordEventFromMessageDoesNotTreatFaceRiskAsBaxia(t *testing.T) {
	// event 保存event，供当前处理流程使用
	event := PasswordLoginEventFromMessage("账号触发风控，需要人脸验证")
	if event.Reason == "baxia_punish_captcha" {
		t.Fatalf("普通人脸验证不应按 baxia 冷却: %+v", event)
	}
	if event.Status != PasswordLoginStatusVerificationRequired {
		t.Fatalf("人脸验证应标记 verification_required: %+v", event)
	}
}

// TestDetectPasswordLoginErrorHTML 负责TestDetect密码登录错误HTML相关处理。
func TestDetectPasswordLoginErrorHTML(t *testing.T) {
	// msg 保存msg，供当前处理流程使用
	msg := detectPasswordLoginErrorHTML(`<div class="login-error-msg">账号或密码错误</div>`)
	if msg != "账号或密码错误" {
		t.Fatalf("登录错误识别=%q", msg)
	}
	msg = detectPasswordLoginErrorHTML(`<span>账号已被冻结，请联系平台</span>`)
	if msg != "账号已被冻结" {
		t.Fatalf("冻结错误识别=%q", msg)
	}
}

// TestDetectPasswordVerificationHTML 负责TestDetect密码VerificationHTML相关处理。
func TestDetectPasswordVerificationHTML(t *testing.T) {
	// html 保存html，供当前处理流程使用
	html := `<iframe id="alibaba-login-box" src="https:\/\/passport.goofish.com\/iv\/photoVerify\/index.htm?token=abc"></iframe><div>需要人脸验证，请使用手机扫码</div>`
	// event、ok 保存event、ok，供当前处理流程使用
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

// TestQuickEnterCookiesUsableRequiresUNB 负责TestQuickEnterCookiesUsableRequiresUNB相关处理。
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

// TestPasswordLoginReferenceProfileAndTiming 负责Test密码登录ReferenceProfileAndTiming相关处理。
func TestPasswordLoginReferenceProfileAndTiming(t *testing.T) {
	if passwordLoginPageLoadWait != 2*time.Second || passwordLoginTabWait != 1500*time.Millisecond ||
		passwordLoginAfterSubmitWait != 3*time.Second || passwordLoginCompletionWait != 5*time.Second {
		t.Fatalf("密码登录等待节奏未与参考实现一致: page=%s tab=%s submit=%s completion=%s",
			passwordLoginPageLoadWait, passwordLoginTabWait, passwordLoginAfterSubmitWait, passwordLoginCompletionWait)
	}
	if passwordVerificationWaitInterval != 10*time.Second || passwordVerificationMaxWait != 5*time.Minute {
		t.Fatalf("人工验证轮询节奏未与参考实现一致: interval=%s max=%s",
			passwordVerificationWaitInterval, passwordVerificationMaxWait)
	}
}

// TestPasswordPersistentContextOptionsMatchReference 负责Test密码Persistent上下文OptionsMatchReference相关处理。
func TestPasswordPersistentContextOptionsMatchReference(t *testing.T) {
	t.Setenv("PLAYWRIGHT_CHROMIUM_EXECUTABLE_PATH", "/opt/chromium")
	// userAgent 模拟从同版本 Chromium 实测后规范化出的无头身份。
	userAgent := "Mozilla/5.0 Chrome/149.0.7827.55 Safari/537.36"
	// opts 验证无头持久化密码登录会把该身份交给 Playwright context。
	opts := passwordPersistentContextOptions(true, &userAgent)
	if opts.Headless == nil || !*opts.Headless {
		t.Fatal("密码登录应按调用参数使用无头模式")
	}
	if opts.UserAgent == nil || *opts.UserAgent != userAgent {
		t.Fatalf("无头密码登录应使用去除 HeadlessChrome 的运行时 UA: %v", opts.UserAgent)
	}
	if opts.Viewport == nil || opts.Viewport.Width != 1980 || opts.Viewport.Height != 1024 {
		t.Fatalf("密码登录 viewport=%+v", opts.Viewport)
	}
	if opts.Locale == nil || *opts.Locale != "zh-CN" || opts.TimezoneId == nil || *opts.TimezoneId != "Asia/Shanghai" {
		t.Fatalf("密码登录区域参数异常: locale=%v timezone=%v", opts.Locale, opts.TimezoneId)
	}
	if opts.AcceptDownloads == nil || !*opts.AcceptDownloads || opts.IgnoreHttpsErrors == nil || !*opts.IgnoreHttpsErrors {
		t.Fatal("密码登录下载/HTTPS 参数未与参考实现一致")
	}
	if opts.ExtraHttpHeaders["Accept-Language"] != "zh-CN,zh;q=0.9,en;q=0.8" {
		t.Fatalf("Accept-Language=%q", opts.ExtraHttpHeaders["Accept-Language"])
	}
	if opts.Timeout == nil || *opts.Timeout != 30000 {
		t.Fatalf("密码登录启动超时=%v", opts.Timeout)
	}
	if opts.ExecutablePath == nil || *opts.ExecutablePath != "/opt/chromium" {
		t.Fatalf("Chromium 路径=%v", opts.ExecutablePath)
	}
	// headed 是有头密码登录配置，必须保留 Chromium 的原生 UA 而不使用无头覆盖。
	headed := passwordPersistentContextOptions(false, &userAgent)
	if headed.UserAgent != nil {
		t.Fatalf("有头密码登录应保留 Chromium 原生 UA: %v", headed.UserAgent)
	}
}

// TestPasswordLoginRejectsBlankCredentialsBeforeBrowserInit 负责Test密码登录RejectsBlankCredentialsBefore浏览器Init相关处理。
func TestPasswordLoginRejectsBlankCredentialsBeforeBrowserInit(t *testing.T) {
	// m 保存m，供当前处理流程使用
	m := &Manager{}
	// tc 表示当前遍历过程中的tc
	for _, tc := range []struct {
		account  string
		password string
	}{
		{account: "", password: "secret"},
		{account: "account", password: ""},
		{account: "  ", password: "secret"},
	} {
		// err 保存err，供当前处理流程使用
		_, err := m.PasswordLogin(context.Background(), tc.account, tc.password, "cookie-id", "", true)
		if err == nil || !strings.Contains(err.Error(), "账号或密码不能为空") {
			t.Fatalf("account=%q password=%q: err=%v", tc.account, tc.password, err)
		}
	}
}
