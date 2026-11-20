package server

import (
	"context"

	notificationsapp "xianyu-go/internal/application/notifications"
)

// FeishuAppNotificationPort 定义飞书扫码绑定与应用通知设置能力。
type FeishuAppNotificationPort interface {
	// StartFeishuAppBinding 发起飞书应用扫码绑定并返回一次性的会话与二维码地址。
	StartFeishuAppBinding(context.Context, int64, notificationsapp.FeishuBindingOptions) (notificationsapp.FeishuBindingStart, error)
	// GetFeishuAppBindingStatus 查询扫码状态，并在到达轮询时间时执行一次平台轮询。
	GetFeishuAppBindingStatus(context.Context, int64, string) (notificationsapp.FeishuBindingStatus, error)
	// GetFeishuChannelCategories 读取飞书应用渠道的警告与普通提示开关。
	GetFeishuChannelCategories(context.Context, int64, int64) (notificationsapp.FeishuChannelCategories, error)
	// SetFeishuChannelCategories 更新飞书应用渠道的警告与普通提示开关。
	SetFeishuChannelCategories(context.Context, int64, int64, notificationsapp.FeishuChannelCategories) error
}
