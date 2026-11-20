package notify

import (
	"encoding/json"
	"strings"

	"xianyu-go/internal/db"
)

// feishuAppNotificationTiers 解析飞书应用渠道的警告类与普通提示类推送开关。
// config 是已解密的渠道配置 JSON；本函数只提取两个布尔字段，不读取、不返回、也不记录 app_secret 等其他配置。
// 字段缺失时按 true 处理以兼容旧渠道；整个 JSON 无法解析或字段类型错误时同样返回 true/true，
// 避免分类开关配置异常导致通知被静默屏蔽。
func feishuAppNotificationTiers(config string) (warning, info bool) {
	// warning、info 是分类开关的返回值，默认均为 true。
	warning, info = true, true
	// options 只承载两个分类开关，避免把 app_secret 等敏感配置复制到额外结构。
	var options struct {
		// NotifyWarning 为 nil 表示旧渠道未配置警告类开关，按 true 处理。
		NotifyWarning *bool `json:"notify_warning"`
		// NotifyInfo 为 nil 表示旧渠道未配置普通提示类开关，按 true 处理。
		NotifyInfo *bool `json:"notify_info"`
	}
	if // err 是渠道配置 JSON 解析失败的原因；错误文本不包含配置内容。
	err := json.Unmarshal([]byte(strings.TrimSpace(config)), &options); err != nil {
		return true, true
	}
	if options.NotifyWarning != nil {
		warning = *options.NotifyWarning
	}
	if options.NotifyInfo != nil {
		info = *options.NotifyInfo
	}
	return warning, info
}

// isWarningNotificationEvent 判断一条通知是否属于警告类。
// 警告类包括 warn/critical 级别、结果为“失败”的事件，以及账号异常、风控、需要人工处理、
// 系统错误和人工客服请求等固定事件类型；其余事件均为普通提示类。
func isWarningNotificationEvent(ev NotificationEvent) bool {
	// level 是去除空白并转为小写的通知级别，用于匹配 warn/critical。
	level := strings.ToLower(strings.TrimSpace(ev.Level))
	if level == "warn" || level == "critical" {
		return true
	}
	if strings.TrimSpace(ev.Outcome) == "失败" {
		return true
	}
	switch ev.Type {
	case EventAccountOffline, EventAccountDisabled, EventSecurityVerification,
		EventManualInterventionRequired, EventSystemError, EventChatManualRequested:
		return true
	default:
		return false
	}
}

// feishuAppAllowsNotification 在事件订阅过滤之后应用飞书应用渠道的分类开关。
// 非 feishu_app 渠道不受分类开关影响；feishu_app 渠道按事件类别读取 warning/info，
// 两个开关均为 false 时该渠道不会接收任何事件。
func feishuAppAllowsNotification(ch db.NotificationChannel, ev NotificationEvent) bool {
	if strings.ToLower(strings.TrimSpace(ch.Type)) != "feishu_app" {
		return true
	}
	// warning、info 是当前飞书应用渠道已解析的分类开关。
	warning, info := feishuAppNotificationTiers(ch.Config)
	if isWarningNotificationEvent(ev) {
		return warning
	}
	return info
}
