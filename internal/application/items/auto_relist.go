package items

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
)

// autoRelistSeenLimit 是已处理订单去重表的容量上限；超过后整体清空，避免长期运行无界增长。
const autoRelistSeenLimit = 1024

var (
	// ErrRelistUnsupported 表示当前平台客户端没有实现商品重新上架能力。
	ErrRelistUnsupported = errors.New("当前平台客户端不支持商品重新上架")
	// ErrRelistEditUnsupported 表示商品类目不支持网页端编辑，平台未修改任何数据。
	ErrRelistEditUnsupported = errors.New("该商品类目暂不支持网页端编辑，无法自动上架")
	// ErrRelistRiskVerification 表示平台要求安全验证，本次已终止且不会重试。
	ErrRelistRiskVerification = errors.New("闲鱼要求安全验证，已跳过自动上架")
	// ErrRelistSessionExpired 表示账号登录会话已失效，本次已终止且不会重试。
	ErrRelistSessionExpired = errors.New("账号登录已失效，已跳过自动上架")
	// ErrRelistCredentialChanged 表示调用期间账号凭证或归属发生变化，未继续请求平台。
	ErrRelistCredentialChanged = errors.New("账号凭证已变化，已跳过自动上架")
)

// RelistOutcome 是一次自动上架平台调用的非敏感结果。
type RelistOutcome struct {
	// AlreadyOnSale 表示商品已在售且有库存，没有提交修改。
	AlreadyOnSale bool
	// RestoredQuantity 表示售罄商品被补回了最小库存。
	RestoredQuantity bool
}

// AutoRelistRepository 是自动上架用例需要的最小本地事实读取能力，不读取任何凭证。
type AutoRelistRepository interface {
	// AutoRelistEnabled 返回账号是否开启“售出后自动上架”。
	AutoRelistEnabled(ctx context.Context, cookieID string) (bool, error)
	// AccountUserID 返回账号所属本地用户，用于平台端口复核归属。
	AccountUserID(ctx context.Context, cookieID string) (int64, error)
	// OwnsItem 返回商品是否属于该账号（含已被同步标记删除的已售商品）。
	OwnsItem(ctx context.Context, cookieID, itemID string) (bool, error)
}

// AutoRelistPlatform 是自动上架使用的平台端口，一次调用最多两次平台请求且不重试。
type AutoRelistPlatform interface {
	// RelistItem 以最新编辑详情为底把商品改回在售。
	RelistItem(ctx context.Context, userID int64, cookieID, itemID string) (RelistOutcome, error)
}

// AutoRelistService 在 WebSocket 付款事件后按账号开关把已售商品重新上架。
// 并发：HandlePaid 可被多个账号的事件循环并发调用；seen 由 mu 保护，持锁期间不做任何 I/O。
type AutoRelistService struct {
	// repository 读取账号开关与商品归属。
	repository AutoRelistRepository
	// platform 执行平台重新上架。
	platform AutoRelistPlatform
	// logger 记录跳过原因与结果，不含凭证。
	logger *slog.Logger
	// mu 仅保护 seen。
	mu sync.Mutex
	// seen 是已处理订单集合，重复的付款推送不会再次请求平台。
	seen map[string]struct{}
}

// NewAutoRelistService 创建自动上架应用服务，仓储与平台端口必填。
func NewAutoRelistService(repository AutoRelistRepository, platform AutoRelistPlatform, logger *slog.Logger) (*AutoRelistService, error) {
	if repository == nil || platform == nil {
		return nil, errors.New("自动上架依赖不能为空")
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &AutoRelistService{repository: repository, platform: platform, logger: logger, seen: make(map[string]struct{})}, nil
}

// claim 登记订单为已处理；返回 false 表示此前已处理过，应忽略重复事件。
func (service *AutoRelistService) claim(orderID string) bool {
	if orderID == "" {
		return true
	}
	service.mu.Lock()
	defer service.mu.Unlock()
	// done 是该订单此前是否已被处理过。
	if _, done := service.seen[orderID]; done {
		return false
	}
	if len(service.seen) >= autoRelistSeenLimit {
		service.seen = make(map[string]struct{})
	}
	service.seen[orderID] = struct{}{}
	return true
}

// HandlePaid 处理一笔已付款订单：账号开关开启且商品属于该账号时，最多发起一次重新上架（两次平台请求）。
// 所有跳过与失败只记录日志，不返回错误，避免影响付款事件的自动发货链路；失败不重试，等待下一笔订单。
func (service *AutoRelistService) HandlePaid(ctx context.Context, cookieID, itemID, orderID string) {
	// account、item、order 是去掉首尾空白后的账号、商品和订单标识。
	account, item, order := strings.TrimSpace(cookieID), strings.TrimSpace(itemID), strings.TrimSpace(orderID)
	if service == nil || account == "" || item == "" {
		return
	}
	// enabled、err 是账号自动上架开关及读取错误；开关默认关闭，读取失败按关闭处理。
	enabled, err := service.repository.AutoRelistEnabled(ctx, account)
	if err != nil {
		service.logger.WarnContext(ctx, "读取自动上架设置失败，已跳过", "account", account, "item_id", item, "err", err)
		return
	}
	if !enabled {
		return
	}
	// owned 表示商品是否属于该账号；买家侧订单或未同步商品不会触发平台请求。
	owned, err := service.repository.OwnsItem(ctx, account, item)
	if err != nil {
		service.logger.WarnContext(ctx, "核验自动上架商品归属失败，已跳过", "account", account, "item_id", item, "err", err)
		return
	}
	if !owned {
		service.logger.InfoContext(ctx, "自动上架跳过：商品不属于该账号或尚未同步", "account", account, "item_id", item, "order_id", order)
		return
	}
	if !service.claim(order) {
		return
	}
	// userID 是账号所属用户，平台端口据此复核凭证归属。
	userID, err := service.repository.AccountUserID(ctx, account)
	if err != nil || userID <= 0 {
		service.logger.WarnContext(ctx, "读取自动上架账号归属失败，已跳过", "account", account, "item_id", item, "err", err)
		return
	}
	// outcome、err 是平台重新上架结果。
	outcome, err := service.platform.RelistItem(ctx, userID, account, item)
	switch {
	case err == nil && outcome.AlreadyOnSale:
		service.logger.InfoContext(ctx, "自动上架跳过：商品仍在售且有库存", "account", account, "item_id", item, "order_id", order)
	case err == nil:
		service.logger.InfoContext(ctx, "商品售出后已自动重新上架", "account", account, "item_id", item, "order_id", order, "restored_quantity", outcome.RestoredQuantity)
	case errors.Is(err, ErrRelistEditUnsupported):
		service.logger.WarnContext(ctx, "自动上架跳过：该商品类目不支持网页端编辑，请在闲鱼 App 中手动上架", "account", account, "item_id", item, "order_id", order)
	case errors.Is(err, ErrRelistRiskVerification), errors.Is(err, ErrRelistSessionExpired), errors.Is(err, ErrRelistCredentialChanged), errors.Is(err, ErrRelistUnsupported):
		service.logger.WarnContext(ctx, "自动上架已终止："+err.Error(), "account", account, "item_id", item, "order_id", order)
	default:
		service.logger.WarnContext(ctx, "自动上架失败，本次不重试", "account", account, "item_id", item, "order_id", order, "err", err)
	}
}
