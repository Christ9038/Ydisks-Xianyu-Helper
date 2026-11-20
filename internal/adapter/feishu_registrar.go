package adapter

import (
	"context"
	"errors"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/feishu"
)

// FeishuRegistrar 将飞书平台协议客户端适配为通知应用层定义的扫码注册端口。
type FeishuRegistrar struct {
	// client 是执行设备码注册与轮询的飞书协议客户端；只由本适配器持有，不暴露给 transport。
	client *feishu.Client
}

// NewFeishuRegistrar 构造生产飞书扫码注册适配器；HTTP 客户端使用协议包提供的有界默认值。
func NewFeishuRegistrar() notificationsapp.FeishuRegistrar {
	return FeishuRegistrar{client: feishu.NewClient(nil)}
}

// newFeishuRegistrarWithClient 构造使用显式协议客户端的适配器，供测试注入本地 httptest 地址。
func newFeishuRegistrarWithClient(client *feishu.Client) notificationsapp.FeishuRegistrar {
	return FeishuRegistrar{client: client}
}

// BeginRegistration 发起飞书设备码注册并把平台结果转换为应用层 plain 类型。
// ctx 控制单次平台请求；错误保持协议包的脱敏诊断，不包含应用凭据。
func (r FeishuRegistrar) BeginRegistration(ctx context.Context) (notificationsapp.FeishuDeviceCode, error) {
	if r.client == nil {
		return notificationsapp.FeishuDeviceCode{}, errors.New("飞书扫码注册客户端未初始化")
	}
	// result、beginErr 保存协议层注册结果及错误。
	result, beginErr := r.client.BeginRegistration(ctx)
	if beginErr != nil {
		return notificationsapp.FeishuDeviceCode{}, beginErr
	}
	return notificationsapp.FeishuDeviceCode{
		DeviceCode: result.DeviceCode,
		QRCodeURL:  result.VerificationURIComplete,
		ExpiresIn:  result.ExpiresIn,
		Interval:   result.Interval,
	}, nil
}

// PollRegistration 执行一次设备码轮询并把协议分类与凭据转换为应用层 plain 类型。
// ctx 控制单次平台请求；返回结果中的 AppSecret 只允许进入加密渠道配置。
func (r FeishuRegistrar) PollRegistration(ctx context.Context, request notificationsapp.FeishuPollRequest) (notificationsapp.FeishuPollResult, error) {
	if r.client == nil {
		return notificationsapp.FeishuPollResult{}, errors.New("飞书扫码注册客户端未初始化")
	}
	// result、pollErr 保存协议层轮询结果及错误。
	result, pollErr := r.client.Poll(ctx, feishu.PollRequest{Domain: request.Domain, DeviceCode: request.DeviceCode})
	if pollErr != nil {
		return notificationsapp.FeishuPollResult{}, pollErr
	}
	return notificationsapp.FeishuPollResult{
		Status:    notificationsapp.FeishuPollStatus(result.Status),
		Domain:    result.Domain,
		AppID:     result.AppID,
		AppSecret: result.AppSecret,
		OpenID:    result.OpenID,
		Message:   result.Message,
		Interval:  result.Interval,
	}, nil
}

// 编译期确认飞书扫码注册适配器覆盖应用层定义的全部能力。
var _ notificationsapp.FeishuRegistrar = FeishuRegistrar{}
