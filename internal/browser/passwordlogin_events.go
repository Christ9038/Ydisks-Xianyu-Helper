package browser

import "strings"

// PasswordLoginStatusProcessing 用于本次流程后续判断的密码登录状态Processing
const (
	PasswordLoginStatusProcessing           = "processing"
	PasswordLoginStatusVerificationRequired = "verification_required"
	PasswordLoginStatusFailed               = "failed"
)

// PasswordLoginEvent 描述密码登录过程中的中间状态，供 HTTP 会话轮询接口展示。
type PasswordLoginEvent struct {
	Status          string
	Message         string
	Error           string
	Reason          string
	VerificationURL string
	ScreenshotPath  string
	QRCodeURL       string
	CooldownHours   int
}

// PasswordLoginEventHandler 用于本次流程后续判断的密码登录EventHandler
type PasswordLoginEventHandler func(PasswordLoginEvent)

// PasswordLoginEventFromError 封装密码登录EventFrom错误业务协调。
func PasswordLoginEventFromError(err error) PasswordLoginEvent {
	if err == nil {
		return PasswordLoginEvent{}
	}
	return PasswordLoginEventFromMessage(err.Error())
}

// PasswordLoginEventFromMessage 封装密码登录EventFrom消息业务协调。
func PasswordLoginEventFromMessage(msg string) PasswordLoginEvent {
	// lower 用于本次流程后续判断的lower
	lower := strings.ToLower(msg)
	// event 用于本次流程后续判断的event
	event := PasswordLoginEvent{Status: PasswordLoginStatusFailed, Message: msg, Error: msg}
	switch {
	case IsBaxiaPunishMessage(msg) || (strings.Contains(msg, "风控") && strings.Contains(msg, "图形验证")):
		event.Reason = "baxia_punish_captcha"
		event.CooldownHours = 5
	case strings.Contains(msg, "人脸") || strings.Contains(lower, "verification") || strings.Contains(lower, "captcha"):
		event.Status = PasswordLoginStatusVerificationRequired
	}
	return event
}

// IsBaxiaPunishMessage 封装IsBaxiaPunish消息业务协调。
func IsBaxiaPunishMessage(msg string) bool {
	// lower 用于本次流程后续判断的lower
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "baxia") ||
		strings.Contains(lower, "punish") ||
		strings.Contains(lower, "scratch-captcha") ||
		strings.Contains(lower, "captcha-question") ||
		strings.Contains(lower, "scratch-captcha-container") ||
		strings.Contains(msg, "找两个") ||
		strings.Contains(msg, "松鼠")
}
