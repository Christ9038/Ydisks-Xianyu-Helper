package qrlogin

import (
	"context"
	"strings"
	"testing"
)

// nilQRContext 返回用于覆盖二维码管理器兼容 nil Context 分支的空上下文接口。
func nilQRContext() context.Context { return nil }

// TestQRLoginPureHelpersCoversNavigatorAndCookieFallback 覆盖平台标识映射及扁平 Cookie 回退路径。
func TestQRLoginPureHelpersCoversNavigatorAndCookieFallback(t *testing.T) {
	// platformCases 保存浏览器 sec-ch-ua-platform 到 navigator.platform 的映射。
	platformCases := map[string]string{"windows": "Win32", "macOS": "MacIntel", "linux": "Linux x86_64", "android": "Linux armv8l", "ios": "iPhone", "other": "other"}
	// platform、want 保存当前平台映射样本及预期值。
	for platform, want := range platformCases {
		// got 保存当前平台标识的规范化结果。
		if got := navigatorPlatform(platform); got != want {
			t.Fatalf("navigatorPlatform(%q)=%q want=%q", platform, got, want)
		}
	}
	// nilSessionHeader 验证 nil 会话不会产生凭证头。
	if sessionCookieHeader(nil, apiGenerateQR) != "" {
		t.Fatal("nil session should have no cookie header")
	}
	// session 保存带扁平 Cookie 的历史会话。
	session := &Session{cookies: map[string]string{"a": "1", "b": "2"}}
	// cookieHeader 保存扁平 Cookie 回退结果。
	cookieHeader := sessionCookieHeader(session, apiGenerateQR)
	if cookieHeader == "" || len(cookieHeader) < 3 {
		t.Fatalf("cookie header=%q", cookieHeader)
	}
}

// TestQRLoginFaceHelpersCoversURLAndQRCodeBranches 覆盖人脸验证 URL、二维码和内容提取分支。
func TestQRLoginFaceHelpersCoversURLAndQRCodeBranches(t *testing.T) {
	// validURL 保存合法验证地址解析结果。
	validURL := mustParseURL("https://passport.goofish.com/iv/verify")
	if validURL == nil || validURL.Host != "passport.goofish.com" {
		t.Fatalf("valid URL=%v", validURL)
	}
	if mustParseURL("://bad") != nil {
		t.Fatal("invalid URL should return nil")
	}
	// qrDataURL、qrErr 保存二维码渲染结果。
	qrDataURL, qrErr := renderQRDataURL("https://example.com/verify")
	if qrErr != nil || len(qrDataURL) < len("data:image/png;base64,") || qrDataURL[:len("data:image/png;base64,")] != "data:image/png;base64," {
		t.Fatalf("qr data URL len=%d err=%v", len(qrDataURL), qrErr)
	}
	// htoken、htokenErr 保存人脸令牌提取结果。
	htoken, htokenErr := extractFaceHToken("?htoken=abc_123-")
	if htokenErr != nil || htoken != "abc_123-" {
		t.Fatalf("htoken=%q err=%v", htoken, htokenErr)
	}
	// verifyURL、verifyErr 保存带协议相对地址的验证模式 URL。
	verifyURL, verifyErr := extractVerifyModesURL(`window.location.href = "//passport.goofish.com/iv/mini/verify_modes.htm?x=1_umidfg="`)
	if verifyErr != nil || verifyURL != "https://passport.goofish.com/iv/mini/verify_modes.htm?x=1_umidfg=1" {
		t.Fatalf("verify URL=%q err=%v", verifyURL, verifyErr)
	}
	// qrContent、contentErr 保存人脸二维码内容提取结果。
	qrContent, contentErr := extractFaceQRCodeContent(`new Qrcode({ text: "https:\/\/example.com\/face?x=1&amp;y=2" })`)
	if contentErr != nil || qrContent != "https://example.com/face?x=1&y=2" {
		t.Fatalf("qr content=%q err=%v", qrContent, contentErr)
	}
	// invalidContentErr 保存空二维码内容错误。
	if _, invalidContentErr := extractFaceQRCodeContent(`new Qrcode({ text: "" })`); invalidContentErr == nil {
		t.Fatal("empty QR content should fail")
	}
}

// TestQRLoginFaceHelpersCoversExtractorErrors 覆盖人脸页面提取器的格式错误与转义回退分支。
func TestQRLoginFaceHelpersCoversExtractorErrors(t *testing.T) {
	// htokenErr 保存缺少人脸令牌时的协议错误。
	if _, htokenErr := extractFaceHToken("没有令牌"); htokenErr == nil {
		t.Fatal("missing htoken should fail")
	}
	// verifyErr 保存缺少验证模式跳转时的协议错误。
	if _, verifyErr := extractVerifyModesURL("没有跳转"); verifyErr == nil {
		t.Fatal("missing verify modes URL should fail")
	}
	// qrPatternErr 保存缺少二维码构造代码时的协议错误。
	if _, qrPatternErr := extractFaceQRCodeContent("没有二维码"); qrPatternErr == nil {
		t.Fatal("missing QR code should fail")
	}
	// fallbackContent 保存无法按 JavaScript 字符串解码时仍可保留的原始内容。
	fallbackContent, fallbackErr := extractFaceQRCodeContent(`new Qrcode({ text: "https://example.com/face?x=\\q" })`)
	if fallbackErr != nil || !strings.Contains(fallbackContent, "\\q") {
		t.Fatalf("fallback QR content=%q err=%v", fallbackContent, fallbackErr)
	}
}

// TestQRLoginPureHelpersCoversBodyAndTaskGuards 覆盖响应体读取错误、任务登记保护和无效验证地址分支。
func TestQRLoginPureHelpersCoversBodyAndTaskGuards(t *testing.T) {
	// bodyErr 保存底层响应体读取失败结果。
	if _, bodyErr := readQRBody(failingReader{}); bodyErr == nil {
		t.Fatal("readQRBody should return reader errors")
	}
	// manager 保存用于验证后台任务登记保护的二维码管理器。
	manager := NewManager(nil)
	// missingTask 表示不存在会话不能启动后台任务。
	missingTask := manager.startSessionTask("missing", 0, func(context.Context) {})
	if missingTask {
		t.Fatal("missing session should not start a task")
	}
	// manager.mu 保护关闭状态，关闭后任何新任务都必须被拒绝。
	manager.mu.Lock()
	manager.closing = true
	manager.mu.Unlock()
	// closedTask 表示关闭状态下的任务登记结果。
	closedTask := manager.startSessionTask("missing", 0, func(context.Context) {})
	if closedTask {
		t.Fatal("closing manager should not start a task")
	}
	// manager.runGoVerification 对不支持的人脸地址只记录状态，不发起外部请求。
	manager.runGoVerification(context.Background(), "missing", "https://example.com/not-face")
}

// TestQRLoginManagerStartCoversLifecycleGuards 覆盖二维码管理器启动的 nil、取消和关闭状态保护。
func TestQRLoginManagerStartCoversLifecycleGuards(t *testing.T) {
	// nilManager 验证 nil 管理器接收者的错误保护。
	var nilManager *Manager
	// startErr 保存 nil 管理器启动错误。
	if startErr := nilManager.Start(context.Background()); startErr == nil {
		t.Fatal("nil manager should reject Start")
	}
	// manager 保存正常生命周期测试管理器。
	manager := NewManager(nil)
	// startErr 保存 nil 生命周期上下文错误。
	if startErr := manager.Start(nilQRContext()); startErr == nil {
		t.Fatal("nil lifecycle context should reject Start")
	}
	// canceledContext、cancelContext 保存已经取消的生命周期上下文。
	canceledContext, cancelContext := context.WithCancel(context.Background())
	cancelContext()
	// startErr 保存已取消生命周期上下文错误。
	if startErr := manager.Start(canceledContext); startErr == nil {
		t.Fatal("canceled lifecycle context should reject Start")
	}
	// startErr 保存正常生命周期启动结果。
	if startErr := manager.Start(context.Background()); startErr != nil {
		t.Fatalf("normal Start=%v", startErr)
	}
	manager.mu.Lock()
	manager.closing = true
	manager.mu.Unlock()
	// startErr 保存关闭管理器启动错误。
	if startErr := manager.Start(context.Background()); startErr == nil {
		t.Fatal("closing manager should reject Start")
	}
}
