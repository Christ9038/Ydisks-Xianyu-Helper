package items

import (
	"context"
	"errors"
	"strings"
)

// StockMax 是单个库存数的上限，与闲鱼发布页和 MTOP 层保持一致。
const StockMax = 999999

var (
	// ErrStockInvalidInput 表示库存请求缺少有效标识，或数量、规格行不合法。
	ErrStockInvalidInput = errors.New("商品库存参数无效")
	// ErrStockUnsupported 表示当前平台客户端没有实现商品库存读取与修改能力。
	ErrStockUnsupported = errors.New("当前平台客户端不支持商品库存修改")
	// ErrStockCredentialChanged 表示调用期间账号凭证或归属发生变化，未继续请求平台。
	ErrStockCredentialChanged = errors.New("账号凭证已变化，请刷新后重试")
	// ErrStockChanged 表示平台当前库存与用户看到的库存不一致，未提交修改。
	ErrStockChanged = errors.New("商品库存已在平台侧变化，请刷新后重试")
	// ErrStockEditUnsupported 表示商品类目不支持网页端编辑，未修改任何数据，用户应改用闲鱼 App。
	ErrStockEditUnsupported = errors.New("该商品类目暂不支持网页端编辑，请在闲鱼 App 中修改库存")
	// ErrStockRiskVerification 表示平台要求安全验证，本次请求已终止，用户应先在闲鱼 App 完成验证。
	ErrStockRiskVerification = errors.New("闲鱼要求安全验证，请先在闲鱼 App 完成验证后再重试")
	// ErrStockSessionExpired 表示账号登录会话已失效，用户需要重新登录该账号。
	ErrStockSessionExpired = errors.New("账号登录已失效，请重新登录该账号后再试")
	// ErrStockSessionPersistence 表示平台已修改库存，但响应 Cookie 未能可靠写回本地。
	ErrStockSessionPersistence = errors.New("平台已修改库存，但账号会话写回失败")
)

// StockSKU 是多规格商品中一行规格组合的库存视图。
type StockSKU struct {
	// Key 是该规格行在同一商品内的稳定标识，修改时原样回传。
	Key string
	// Label 是展示给用户的规格组合文案。
	Label string
	// PriceCents 是该规格行售价（分），仅用于展示。
	PriceCents int64
	// Quantity 是该规格行当前库存。
	Quantity int
}

// StockSnapshot 是商品当前库存的非敏感视图。
type StockSnapshot struct {
	// ItemID 是闲鱼商品标识。
	ItemID string
	// MultiSpec 表示库存由规格行提供。
	MultiSpec bool
	// Quantity 是单规格商品库存；多规格商品为各规格库存之和。
	Quantity int
	// SKUs 是多规格商品的规格行；单规格商品为空。
	SKUs []StockSKU
}

// StockSKUChange 是多规格库存修改中的一行。
type StockSKUChange struct {
	// Key 对应 StockSKU.Key。
	Key string
	// Quantity 是新库存，范围 0..StockMax。
	Quantity int
	// ExpectedQuantity 是用户看到的原库存，用于并发校验。
	ExpectedQuantity int
}

// StockUpdate 描述一次库存修改：单规格填 Quantity/ExpectedQuantity，多规格填全部规格行 SKUs。
type StockUpdate struct {
	// Quantity 是单规格商品的新库存，范围 1..StockMax。
	Quantity int
	// ExpectedQuantity 是单规格商品用户看到的原库存。
	ExpectedQuantity int
	// SKUs 是多规格商品的全部规格行修改；非空表示按多规格处理。
	SKUs []StockSKUChange
}

// StockUpdateOutcome 是库存修改成功后的非敏感结果。
type StockUpdateOutcome struct {
	// ItemID 是被修改的商品标识。
	ItemID string
	// Message 是平台返回的安全摘要。
	Message string
	// CookiePersisted 表示平台响应中的会话变化已可靠写回本地。
	CookiePersisted bool
}

// ItemStockPort 定义商品库存读取与修改的最小应用端口。
type ItemStockPort interface {
	// GetStock 读取商品当前库存，不修改任何平台数据。
	GetStock(ctx context.Context, userID int64, cookieID, itemID string) (StockSnapshot, error)
	// UpdateStock 以最新平台数据为底修改库存并持久化响应会话变化。
	UpdateStock(ctx context.Context, userID int64, cookieID, itemID string, update StockUpdate) (StockUpdateOutcome, error)
}

// ItemStockService 校验库存用例的输入并委托基础设施端口。
type ItemStockService struct {
	// port 是负责账号归属、MTOP 调用和会话写回的基础设施端口。
	port ItemStockPort
}

// NewItemStockService 创建商品库存应用服务。
func NewItemStockService(port ItemStockPort) (*ItemStockService, error) {
	if port == nil {
		return nil, errors.New("商品库存端口不能为空")
	}
	return &ItemStockService{port: port}, nil
}

// GetStock 读取指定账号下商品的当前库存。
func (service *ItemStockService) GetStock(ctx context.Context, userID int64, cookieID, itemID string) (StockSnapshot, error) {
	// cookie、item 是去掉首尾空白后的账号与商品标识。
	cookie, item := strings.TrimSpace(cookieID), strings.TrimSpace(itemID)
	if service == nil || service.port == nil || userID <= 0 || cookie == "" || item == "" {
		return StockSnapshot{}, ErrStockInvalidInput
	}
	return service.port.GetStock(ctx, userID, cookie, item)
}

// UpdateStock 修改指定账号下商品的库存；数量范围和规格行在请求平台之前校验。
func (service *ItemStockService) UpdateStock(ctx context.Context, userID int64, cookieID, itemID string, update StockUpdate) (StockUpdateOutcome, error) {
	// cookie、item 是去掉首尾空白后的账号与商品标识。
	cookie, item := strings.TrimSpace(cookieID), strings.TrimSpace(itemID)
	if service == nil || service.port == nil || userID <= 0 || cookie == "" || item == "" || !validStockUpdate(update) {
		return StockUpdateOutcome{}, ErrStockInvalidInput
	}
	return service.port.UpdateStock(ctx, userID, cookie, item, update)
}

// validStockUpdate 校验库存修改请求自身：单规格 1..StockMax；多规格行不重复、0..StockMax 且至少一行大于 0。
func validStockUpdate(update StockUpdate) bool {
	if len(update.SKUs) == 0 {
		return update.Quantity >= 1 && update.Quantity <= StockMax && update.ExpectedQuantity >= 0
	}
	// seen 保存已出现的规格行标识，用于拒绝重复行。
	seen := make(map[string]struct{}, len(update.SKUs))
	// positive 统计新库存大于 0 的规格行数。
	positive := 0
	// row 是当前待校验的规格行。
	for _, row := range update.SKUs {
		// key 是规格行标识。
		key := strings.TrimSpace(row.Key)
		if key == "" || row.Quantity < 0 || row.Quantity > StockMax || row.ExpectedQuantity < 0 {
			return false
		}
		// duplicated 表示该规格行标识此前是否已出现，用于拒绝重复行。
		if _, duplicated := seen[key]; duplicated {
			return false
		}
		seen[key] = struct{}{}
		if row.Quantity > 0 {
			positive++
		}
	}
	return positive > 0
}
