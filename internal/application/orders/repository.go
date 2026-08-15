package orders

import "context"

// Repository 定义订单用例所需的持久化、凭证锁和平台运行视图能力。
// 实现由基础设施适配器提供，本包不依赖数据库、HTTP 或 Server。
type Repository interface {
	// ExistsOwned 判断账号是否归属于用户。
	ExistsOwned(ctx context.Context, userID int64, cookieID string) (bool, error)
	// ListOwnedIDs 返回用户拥有的账号 ID。
	ListOwnedIDs(ctx context.Context, userID int64) ([]string, error)
	// ListOrdersForUser 查询用户范围内的订单列表。
	ListOrdersForUser(ctx context.Context, filter ListFilter) ([]OrderRow, int, error)
	// GetOrder 查询单个订单。
	GetOrder(ctx context.Context, orderID string) (*Order, error)
	// GetItem 查询账号下的商品信息。
	GetItem(ctx context.Context, cookieID, itemID string) (*ItemInfo, error)
	// SoftDeleteOrder 逻辑删除订单。
	SoftDeleteOrder(ctx context.Context, orderID string) (bool, error)
	// WithTransaction 在订单写入用例中执行单个事务。
	WithTransaction(ctx context.Context, work func(Writer) error) error
	// UpsertOrder 写入订单。
	UpsertOrder(ctx context.Context, orderID string, options UpsertOptions) error
	// LockCredentials 串行化账号凭证状态变更。
	LockCredentials(cookieID string) func()
	// LoadCookiePlatformDetail 读取订单平台请求需要的最小账号视图。
	LoadCookiePlatformDetail(ctx context.Context, cookieID string) (*PlatformRuntimeData, error)
	// UpdateRenewalCookie 更新账号续期 Cookie 和 metadata。
	UpdateRenewalCookie(ctx context.Context, cookieID, value, metadata string, at int64) error
	// SoftDeleteMissingOrders 删除账号下远端已不存在的订单。
	SoftDeleteMissingOrders(ctx context.Context, cookieID string, activeIDs map[string]struct{}) (int, error)
	// ListOrdersByCookiePage 分页读取账号订单。
	ListOrdersByCookiePage(ctx context.Context, cookieID string, limit, offset int) ([]OrderRow, error)
}
