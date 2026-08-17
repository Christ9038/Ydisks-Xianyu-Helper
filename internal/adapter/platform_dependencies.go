package adapter

import (
	"errors"
	"log/slog"
)

// PlatformDependencies 保存 HTTP 应用装配所需的平台客户端；Server 只能通过本类型读取这些能力。
// 旧的 Server 字段仅作为阶段性测试兼容入口保留，待所有调用方迁移后删除。
type PlatformDependencies struct {
	// mtopClient 承担 MTOP 请求与订单、商品、账号资料等平台操作。
	mtopClient MTOPClient
	// longLoginClient 承担长登录状态查询、更新及响应 Cookie 返回。
	longLoginClient LongLoginClient
	// qrLoginService 承担二维码生成、状态轮询和风控验证完成。
	qrLoginService QRLoginService
}

// NewPlatformDependencies 构造并校验平台客户端集合，拒绝半初始化依赖进入 Server。
func NewPlatformDependencies(mtopClient MTOPClient, longLoginClient LongLoginClient, qrLoginService QRLoginService) (*PlatformDependencies, error) {
	if mtopClient == nil {
		return nil, errors.New("平台依赖 MTOP 客户端不能为空")
	}
	if longLoginClient == nil {
		return nil, errors.New("平台依赖长登录客户端不能为空")
	}
	if qrLoginService == nil {
		return nil, errors.New("平台依赖二维码服务不能为空")
	}
	return &PlatformDependencies{
		mtopClient:      mtopClient,
		longLoginClient: longLoginClient,
		qrLoginService:  qrLoginService,
	}, nil
}

// NewDefaultPlatformDependencies 创建生产环境使用的完整平台客户端集合。
func NewDefaultPlatformDependencies(logger *slog.Logger) (*PlatformDependencies, error) {
	return NewPlatformDependencies(NewMTOPClient(), NewLongLoginClient(), NewQRLoginService(logger))
}

// MTOPClient 返回已校验的 MTOP 客户端；返回值不含账号 Cookie 或其他敏感数据。
func (d *PlatformDependencies) MTOPClient() MTOPClient {
	if d == nil {
		return nil
	}
	return d.mtopClient
}

// LongLoginClient 返回已校验的长登录客户端；调用方负责控制凭证读取和持久化范围。
func (d *PlatformDependencies) LongLoginClient() LongLoginClient {
	if d == nil {
		return nil
	}
	return d.longLoginClient
}

// QRLoginService 返回已校验的二维码服务；平台会话状态仍由该服务自身负责生命周期。
func (d *PlatformDependencies) QRLoginService() QRLoginService {
	if d == nil {
		return nil
	}
	return d.qrLoginService
}
