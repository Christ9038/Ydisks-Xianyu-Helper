package adapter

import (
	"context"
	"errors"
	"fmt"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/automation"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// RelistItem 以最新编辑详情为底把商品改回在售。调用固定最多两次平台请求（读取 + 提交），失败不重试。
func (p *ItemPublishPort) RelistItem(ctx context.Context, userID int64, cookieID, itemID string) (itemapp.RelistOutcome, error) {
	// outcome 保存平台调用结果，由调用闭包写入。
	var outcome itemapp.RelistOutcome
	// persistErr、callErr 分别是会话写回错误和平台调用错误。
	persistErr, callErr := p.runItemEditCall(ctx, userID, cookieID, 90*time.Second, itemapp.ErrRelistCredentialChanged, func(callCtx context.Context, client mtop.Client, cookies string) (string, error) {
		// relister、ok 保存当前客户端的可选重新上架能力。
		relister, ok := client.(mtop.ItemRelistClient)
		if !ok {
			return "", itemapp.ErrRelistUnsupported
		}
		// result、err 是平台重新上架结果。
		result, err := relister.RelistItem(callCtx, cookies, itemID)
		if err != nil {
			return "", err
		}
		if result == nil {
			return "", errors.New("闲鱼商品重新上架未返回结果")
		}
		outcome = itemapp.RelistOutcome{AlreadyOnSale: result.AlreadyOnSale, RestoredQuantity: result.RestoredQuantity}
		return result.UpdatedCookies, nil
	}, mapRelistError)
	if callErr != nil {
		return itemapp.RelistOutcome{}, callErr
	}
	if persistErr != nil {
		// 平台已完成上架，会话写回失败只记日志；下一次平台调用会重新取得会话。
		p.logger.WarnContext(ctx, "自动上架后保存账号会话失败", "account", cookieID, "error", persistErr)
	}
	return outcome, nil
}

// mapRelistError 把 MTOP 错误映射为自动上架应用层哨兵错误，其余错误原样返回。
func mapRelistError(err error) error {
	switch {
	case errors.Is(err, mtop.ErrItemEditUnsupported):
		return itemapp.ErrRelistEditUnsupported
	case mtop.IsRiskVerificationErr(err):
		return errors.Join(itemapp.ErrRelistRiskVerification, err)
	case mtop.IsSessionExpiredErr(err):
		return errors.Join(itemapp.ErrRelistSessionExpired, err)
	default:
		return err
	}
}

// AutoRelistRepository 用数据库仓储实现自动上架所需的本地事实读取，不读取任何凭证。
type AutoRelistRepository struct {
	// store 提供账号设置、归属和商品记录的窄读取。
	store *db.Store
}

// NewAutoRelistRepository 构造自动上架本地事实仓储。
func NewAutoRelistRepository(store *db.Store) (*AutoRelistRepository, error) {
	if store == nil || store.Cookies == nil || store.Items == nil {
		return nil, fmt.Errorf("自动上架仓储需要数据库存储")
	}
	return &AutoRelistRepository{store: store}, nil
}

// AutoRelistEnabled 返回账号“售出后自动上架”开关。
func (r *AutoRelistRepository) AutoRelistEnabled(ctx context.Context, cookieID string) (bool, error) {
	return r.store.Cookies.GetAutoRelist(ctx, cookieID)
}

// AccountUserID 返回账号所属用户，只读取非敏感摘要列。
func (r *AutoRelistRepository) AccountUserID(ctx context.Context, cookieID string) (int64, error) {
	return r.store.Cookies.GetOwnerID(ctx, cookieID)
}

// OwnsItem 返回商品是否属于该账号（含软删除记录）。
func (r *AutoRelistRepository) OwnsItem(ctx context.Context, cookieID, itemID string) (bool, error) {
	return r.store.Items.ExistsByCookieItem(ctx, cookieID, itemID)
}

// 确保适配器满足应用层定义的自动上架端口。
var (
	_ itemapp.AutoRelistRepository = (*AutoRelistRepository)(nil)
	_ itemapp.AutoRelistPlatform   = (*ItemPublishPort)(nil)
)

// relistAfterPaid 在付款 WebSocket 事件处理后触发售出后自动上架。只处理 WebSocket 来源的付款事件，
// 买家角色事件直接忽略；简化付款消息缺少商品 ID 时只读本地订单事实补齐，不发起平台请求。
// 开关、商品归属、去重和平台调用都在应用服务内完成，所有跳过与失败只记日志。
func (a *Adapter) relistAfterPaid(ctx context.Context, task automation.Task) {
	if a.soldItemRelist == nil || task.Source != "ws" || task.TriggerType != automation.TriggerOrderPaid || task.OrderRole == automation.OrderRoleBuyer {
		return
	}
	// itemID 是付款事件对应的商品；缺失时从本地订单回填。
	itemID := task.ItemID
	if itemID == "" && task.OrderID != "" && a.store != nil && a.store.Orders != nil {
		// order、orderErr 是本账号的本地订单事实；归属其他账号的订单不能用于上架。
		order, orderErr := a.store.Orders.Get(ctx, task.OrderID)
		if orderErr != nil || order == nil || order.CookieID != task.AccountID {
			return
		}
		itemID = order.ItemID
	}
	a.soldItemRelist(ctx, task.AccountID, itemID, task.OrderID)
}
