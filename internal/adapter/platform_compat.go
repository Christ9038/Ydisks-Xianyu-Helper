package adapter

import (
	"context"
	"log/slog"

	"xianyu-go/internal/xianyu/mtop"
	"xianyu-go/internal/xianyu/qrlogin"
	xrenew "xianyu-go/internal/xianyu/renew"
)

// MTOPClient 是平台装配边界使用的客户端别名，避免上层依赖具体 MTOP 包。
type MTOPClient = mtop.Client

// QRLoginService 定义 HTTP 扫码端点所需的最小二维码会话协议。
type QRLoginService interface {
	// GenerateQRCode 创建待轮询的二维码会话并返回会话标识和二维码地址。
	GenerateQRCode(context.Context) (string, string, error)
	// GetSessionStatus 返回会话状态；HTTP 适配器负责历史字段转换。
	GetSessionStatus(string) map[string]any
	// CompleteVerification 完成风控验证并返回当前请求范围内的凭证值。
	CompleteVerification(context.Context, string) (string, string, error)
}

// NewMTOPClient 创建默认平台客户端，供生产装配和测试替换使用。
func NewMTOPClient() MTOPClient {
	return mtop.NewClient()
}

// NewQRLoginService 创建默认二维码会话服务。
func NewQRLoginService(logger *slog.Logger) QRLoginService {
	return qrlogin.NewManager(logger)
}

// NewLongLoginClient 创建默认长登录平台客户端。
func NewLongLoginClient() LongLoginClient {
	return xrenew.Service{}
}
