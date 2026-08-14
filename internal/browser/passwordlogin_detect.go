package browser

import (
	"html"
	"regexp"
	"strings"
)

// htmlTagPattern 保存htmlTagPattern，供当前处理流程使用
var (
	htmlTagPattern      = regexp.MustCompile(`(?is)<[^>]+>`)
	scriptStylePattern  = regexp.MustCompile(`(?is)<script[^>]*>.*?</script>|<style[^>]*>.*?</style>`)
	verificationURLRe   = regexp.MustCompile(`https?://[^\s"'<>\\]+`)
	dataImageURLRe      = regexp.MustCompile(`data:image/[^;"'\s<>]+;base64,[A-Za-z0-9+/=]+`)
	escapedSlashPattern = strings.NewReplacer(`\/`, `/`, `\u0026`, `&`)
)

// detectPasswordBaxiaPunishHTML 负责detect密码BaxiaPunishHTML相关处理。
func detectPasswordBaxiaPunishHTML(htmlText string) (PasswordLoginEvent, bool) {
	if !IsBaxiaPunishMessage(htmlText) {
		return PasswordLoginEvent{}, false
	}
	// msg 保存msg，供当前处理流程使用
	msg := "触发闲鱼风控图形验证（baxia-punish），账号正常但暂时无法自动登录"
	return PasswordLoginEvent{
		Status:        PasswordLoginStatusFailed,
		Message:       msg,
		Error:         msg,
		Reason:        "baxia_punish_captcha",
		CooldownHours: 5,
	}, true
}

// detectPasswordLoginErrorHTML 负责detect密码登录错误HTML相关处理。
func detectPasswordLoginErrorHTML(htmlText string) string {
	// text 保存文本，供当前处理流程使用
	text := normalizeHTMLText(htmlText)
	// keyword 表示当前遍历过程中的关键词
	for _, keyword := range []string{
		"账密错误", "账号密码错误", "用户名或密码错误", "账号或密码错误", "密码错误",
		"账号不存在", "账号已被冻结", "账号被冻结", "账户被冻结", "账号已锁定", "账户已锁定",
		"操作过于频繁", "登录过于频繁", "暂时无法登录",
	} {
		if strings.Contains(text, keyword) {
			return keyword
		}
	}
	return ""
}

// detectPasswordVerificationHTML 负责detect密码VerificationHTML相关处理。
func detectPasswordVerificationHTML(htmlText string) (PasswordLoginEvent, bool) {
	// normalized 保存normalized，供当前处理流程使用
	normalized := normalizeHTMLText(htmlText)
	// lower 保存lower，供当前处理流程使用
	lower := strings.ToLower(htmlText)
	if !containsAny(normalized,
		"人脸验证", "人脸识别", "身份验证", "安全验证", "扫码验证", "验证二维码",
		"请使用手机", "请在手机", "请在浏览器中完成验证",
	) && !containsAny(lower,
		"alibaba-login-box", "iframe_redirect", "iframeredirect", "photoverify", "iv/photoverify",
		"verifyurl", "identity_verify", "normal_validate", "verifycode", "qrcode",
	) {
		return PasswordLoginEvent{}, false
	}
	// msg 保存msg，供当前处理流程使用
	msg := "密码登录需要人工验证，请在浏览器中完成验证后等待自动继续"
	// event 保存event，供当前处理流程使用
	event := PasswordLoginEvent{
		Status:          PasswordLoginStatusVerificationRequired,
		Message:         msg,
		Error:           msg,
		VerificationURL: extractPasswordVerificationURL(htmlText),
		QRCodeURL:       extractPasswordQRURL(htmlText),
	}
	return event, true
}

// extractPasswordVerificationURL 负责extract密码VerificationURL相关处理。
func extractPasswordVerificationURL(htmlText string) string {
	// cleaned 保存cleaned，供当前处理流程使用
	cleaned := escapedSlashPattern.Replace(html.UnescapeString(htmlText))
	// matches 保存matches，供当前处理流程使用
	matches := verificationURLRe.FindAllString(cleaned, -1)
	// raw 表示当前遍历过程中的原始
	for _, raw := range matches {
		// url 保存地址，供当前处理流程使用
		url := strings.TrimRight(raw, ".,);]")
		// lower 保存lower，供当前处理流程使用
		lower := strings.ToLower(url)
		if containsAny(lower, "passport", "verify", "photo", "iv/", "identity", "login", "qrcode") {
			return url
		}
	}
	return ""
}

// extractPasswordQRURL 负责extract密码QRURL相关处理。
func extractPasswordQRURL(htmlText string) string {
	// cleaned 保存cleaned，供当前处理流程使用
	cleaned := escapedSlashPattern.Replace(html.UnescapeString(htmlText))
	if // match 保存match，供当前处理流程使用
	match := dataImageURLRe.FindString(cleaned); match != "" {
		return match
	}
	return ""
}

// normalizeHTMLText 负责normalizeHTML文本相关处理。
func normalizeHTMLText(htmlText string) string {
	// cleaned 保存cleaned，供当前处理流程使用
	cleaned := scriptStylePattern.ReplaceAllString(htmlText, " ")
	cleaned = htmlTagPattern.ReplaceAllString(cleaned, " ")
	cleaned = escapedSlashPattern.Replace(html.UnescapeString(cleaned))
	return strings.Join(strings.Fields(cleaned), " ")
}

// containsAny 负责containsAny相关处理。
func containsAny(s string, keywords ...string) bool {
	// keyword 表示当前遍历过程中的关键词
	for _, keyword := range keywords {
		if strings.Contains(s, keyword) {
			return true
		}
	}
	return false
}
