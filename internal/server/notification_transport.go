package server

import notificationsapp "xianyu-go/internal/application/notifications"

// notificationChannelEditorResponse 是通知渠道编辑接口的脱敏响应 DTO。
type notificationChannelEditorResponse struct {
	// ID 是通知渠道稳定标识。
	ID int64 `json:"id"`
	// Name 是渠道名称。
	Name string `json:"name"`
	// Type 是通知渠道类型。
	Type string `json:"type"`
	// EventTypes 是订阅事件类型 JSON 或兼容分隔文本。
	EventTypes string `json:"event_types,omitempty"`
	// Enabled 表示通知渠道是否启用。
	Enabled bool `json:"enabled"`
	// ToEmail 是邮件渠道的收件地址，不是发送凭据。
	ToEmail string `json:"to_email,omitempty"`
	// UseCustomSMTP 表示邮件渠道是否使用独立 SMTP，不包含 SMTP 密码。
	UseCustomSMTP bool `json:"use_custom_smtp,omitempty"`
}

// newNotificationChannelEditorResponse 将应用层编辑器视图转换为脱敏 HTTP DTO。
func newNotificationChannelEditorResponse(channel notificationsapp.ChannelEditor) notificationChannelEditorResponse {
	return notificationChannelEditorResponse{ID: channel.ID, Name: channel.Name, Type: channel.Type, EventTypes: channel.EventTypes, Enabled: channel.Enabled, ToEmail: channel.ToEmail, UseCustomSMTP: channel.UseCustomSMTP}
}
