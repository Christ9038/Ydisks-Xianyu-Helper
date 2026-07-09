package browser

import "strings"

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

type PasswordLoginEventHandler func(PasswordLoginEvent)

func PasswordLoginEventFromError(err error) PasswordLoginEvent {
	if err == nil {
		return PasswordLoginEvent{}
	}
	return PasswordLoginEventFromMessage(err.Error())
}

func PasswordLoginEventFromMessage(msg string) PasswordLoginEvent {
	lower := strings.ToLower(msg)
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

func IsBaxiaPunishMessage(msg string) bool {
	lower := strings.ToLower(msg)
	return strings.Contains(lower, "baxia") ||
		strings.Contains(lower, "punish") ||
		strings.Contains(lower, "scratch-captcha") ||
		strings.Contains(lower, "captcha-question") ||
		strings.Contains(lower, "scratch-captcha-container") ||
		strings.Contains(msg, "找两个") ||
		strings.Contains(msg, "松鼠")
}
