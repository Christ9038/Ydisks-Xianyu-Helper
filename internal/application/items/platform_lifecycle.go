package items

import (
	"context"
	"errors"
	"strings"
)

var (
	// ErrDownShelfInvalidInput 表示商品下架请求缺少有效的用户、账号或商品标识。
	ErrDownShelfInvalidInput = errors.New("商品下架参数无效")
	// ErrDownShelfUnsupported 表示当前平台客户端没有实现已验证的商品下架能力。
	ErrDownShelfUnsupported = errors.New("当前平台客户端不支持商品下架")
	// ErrDownShelfCredentialChanged 表示调用期间账号凭证或归属发生变化，未继续执行下架请求。
	ErrDownShelfCredentialChanged = errors.New("账号凭证已变化，请刷新后重试")
	// ErrDownShelfSessionPersistence 表示平台已确认下架，但响应 Cookie 未能可靠写回本地。
	ErrDownShelfSessionPersistence = errors.New("平台已下架商品，但账号会话写回失败")
)

// DownShelfOutcome 是商品平台下架的非敏感结果。
type DownShelfOutcome struct {
	// ItemID 是本次下架的闲鱼商品标识。
	ItemID string
	// Status 是平台确认后的商品状态；成功下架固定为 off_sale。
	Status string
	// Message 是平台返回的安全摘要，不包含 Cookie 或签名。
	Message string
	// PlatformSuccess 表示平台响应明确确认下架成功。
	PlatformSuccess bool
	// CookiePersisted 表示平台响应中的会话变化已可靠写回本地。
	CookiePersisted bool
}

// PlatformLifecyclePort 定义商品平台生命周期写操作的最小应用端口。
type PlatformLifecyclePort interface {
	// DownShelf 调用平台下架操作并持久化响应会话变化；不得执行平台删除或本地商品删除。
	DownShelf(context.Context, int64, string, string) (DownShelfOutcome, error)
}

// PlatformLifecycleService 编排平台商品下架用例并校验用户输入。
type PlatformLifecycleService struct {
	// port 是负责账号归属、MTOP 调用和会话写回的基础设施端口。
	port PlatformLifecyclePort
}

// NewPlatformLifecycleService 创建平台商品生命周期应用服务。
func NewPlatformLifecycleService(port PlatformLifecyclePort) (*PlatformLifecycleService, error) {
	if port == nil {
		return nil, errors.New("商品平台生命周期端口不能为空")
	}
	return &PlatformLifecycleService{port: port}, nil
}

// DownShelf 将指定账号下的商品在闲鱼平台下架；不会删除平台商品或本地商品记录。
func (service *PlatformLifecycleService) DownShelf(ctx context.Context, userID int64, cookieID, itemID string) (DownShelfOutcome, error) {
	if service == nil || service.port == nil || userID <= 0 || strings.TrimSpace(cookieID) == "" || strings.TrimSpace(itemID) == "" {
		return DownShelfOutcome{}, ErrDownShelfInvalidInput
	}
	return service.port.DownShelf(ctx, userID, strings.TrimSpace(cookieID), strings.TrimSpace(itemID))
}
