// Package adapter 是账号运行时与外部能力（浏览器自动化、通知、自动化中心）的装配层。
//
// 它实现 engine.Handler 与 automation.OrderDetailFetcher，把系统事件转发到自动化中心、
// 把订单详情抓取/密码登录刷新接到浏览器、把账号告警推到通知器。业务逻辑集中在此，
// cmd/server 只负责构造与接线。
package adapter

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"xianyu-go/internal/automation"
	"xianyu-go/internal/browser"
	"xianyu-go/internal/db"
	"xianyu-go/internal/engine"
)

// browserManager 是 adapter 所需的浏览器能力最小契约。*browser.Manager 实现该接口；
// 测试可注入桩实现，避免依赖 Chromium。
type browserManager interface {
	FetchOrderDetail(ctx context.Context, orderID, cookieID, cookieValue string, requireSpec ...bool) (*browser.OrderDetail, error)
	CookieRenew(ctx context.Context, cookieID, cookieStr string, headless bool) (map[string]string, error)
	PasswordLogin(ctx context.Context, account, password, cookieID, userDataDir string, headless bool) (map[string]string, error)
}

// Adapter 实现 engine.Handler 与 automation.OrderDetailFetcher，
// 把系统消息、订单详情抓取和密码登录刷新接到浏览器与自动化中心。
//
// 自动发货只走 automation.Center；用户聊天消息由 Account 内部 ReplyService 处理，
// 故 HandleChatMessage 为空实现。
type Adapter struct {
	store      *db.Store
	browser    browserManager
	logger     *slog.Logger
	automation *automation.Center
	notifier   notifyNotifier

	mu             sync.Mutex
	lastLoginByCID map[string]time.Time
	orderFetchMu   sync.Mutex
	lastOrderFetch time.Time
}

// notifyNotifier 是 *notify.Notifier 的最小接口，避免 adapter 直接依赖 notify 包
// （notify 包未来若反向引用 adapter 也不会形成循环）。
type notifyNotifier interface {
	NotifyAccountAlert(cookieID, level, title, body string)
}

// New 构造 Adapter。automation 与 notifier 通过 Set* 后期注入（因创建顺序存在循环：
// mgr 依赖 adapter，automation 依赖 mgr，adapter 又依赖 automation）。
func New(store *db.Store, bm *browser.Manager, logger *slog.Logger) *Adapter {
	if logger == nil {
		logger = slog.Default()
	}
	return &Adapter{store: store, browser: browserManagerOrNil(bm), logger: logger}
}

// browserManagerOrNil 把 *browser.Manager 转为接口；nil 时返回 nil 接口。
func browserManagerOrNil(bm *browser.Manager) browserManager {
	if bm == nil {
		return nil
	}
	return bm
}

// SetAutomation 注入自动化中心（系统事件转发目标）。
func (a *Adapter) SetAutomation(c *automation.Center) { a.automation = c }

// SetNotifier 注入通知器（账号告警推送目标）。
func (a *Adapter) SetNotifier(n notifyNotifier) { a.notifier = n }

// SetBrowser 覆盖浏览器实现，便于测试注入桩。
func (a *Adapter) SetBrowser(b browserManager) { a.browser = b }

// HandleChatMessage 用户聊天消息由 Account 内部 ReplyService 处理，此处空实现满足接口。
func (a *Adapter) HandleChatMessage(_ context.Context, _ engine.ChatMessage) error {
	return nil
}

// OnAccountAlert 把账号告警（token 失效/自动恢复失败/风控验证等）转发给通知器，
// 推送到该账号已绑定的通知渠道。
func (a *Adapter) OnAccountAlert(_ context.Context, cookieID, level, title, body string) {
	if a.notifier == nil {
		a.logger.Warn("告警通知未发送：通知器未注入", "account", cookieID, "level", level, "title", title)
		return
	}
	a.notifier.NotifyAccountAlert(cookieID, level, title, body)
}

// HandleSystemEvent 把系统卡片事件转发到自动化中心，由自动化规则决定是否执行。
func (a *Adapter) HandleSystemEvent(ctx context.Context, task automation.Task) error {
	if a.automation == nil {
		return nil
	}
	a.logger.Info("系统自动化事件", "account", task.AccountID, "trigger", task.TriggerType, "order_id", task.OrderID)
	return a.automation.HandleTask(ctx, task)
}

// FetchOrderDetail 实现 automation.OrderDetailFetcher。只在本地订单缺少关键字段时启动浏览器，
// 并将所有账号的详情请求串行化、至少间隔 3 秒，避免短时间高频访问闲鱼。
func (a *Adapter) FetchOrderDetail(ctx context.Context, cookieID, orderID, itemID, buyerID, cookieStr string) (*automation.OrderDetail, error) {
	if detail, ok := a.localOrderDetail(ctx, orderID); ok {
		return detail, nil
	}
	if a.browser == nil {
		return nil, fmt.Errorf("订单缺少规格/数量信息且浏览器自动化未启用")
	}

	a.orderFetchMu.Lock()
	defer a.orderFetchMu.Unlock()
	// 等锁期间其他流程可能已经补齐订单，再检查一次。
	if detail, ok := a.localOrderDetail(ctx, orderID); ok {
		return detail, nil
	}
	if remain := 3*time.Second - time.Since(a.lastOrderFetch); !a.lastOrderFetch.IsZero() && remain > 0 {
		timer := time.NewTimer(remain)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	a.lastOrderFetch = time.Now()
	detail, err := a.browser.FetchOrderDetail(ctx, orderID, cookieID, cookieStr, true)
	if err != nil {
		return nil, err
	}
	if detail.UpdatedCookies != "" && detail.UpdatedCookies != cookieStr {
		_ = a.store.Cookies.Save(ctx, cookieID, detail.UpdatedCookies, 0)
	}
	return &automation.OrderDetail{
		Quantity: detail.Quantity, SpecName: detail.SpecName, SpecValue: detail.SpecValue,
		Amount: detail.Amount, OrderStatus: detail.OrderStatus,
	}, nil
}

// localOrderDetail 命中本地完整订单时直接返回，避免不必要的浏览器抓取。
func (a *Adapter) localOrderDetail(ctx context.Context, orderID string) (*automation.OrderDetail, bool) {
	order, err := a.store.Orders.Get(ctx, orderID)
	if err != nil || order == nil {
		return nil, false
	}
	if order.Amount == "" || order.Quantity == "" || order.SpecName == "" || order.SpecValue == "" {
		return nil, false
	}
	return &automation.OrderDetail{
		Quantity: order.Quantity, SpecName: order.SpecName, SpecValue: order.SpecValue,
		Amount: order.Amount, OrderStatus: order.OrderStatus,
	}, true
}

// OnPasswordLoginRefresh 连续失败时恢复 Cookie。
//
// 恢复顺序是“浏览器快速续期 -> 密码登录”。快速续期复用旧 Cookie 打开闲鱼，
// 尝试点“快速进入”刷新浏览器登录态；这比账号密码登录更轻，风控压力也更小。
// 同一账号冷却 engine.PasswordLoginMinGap，避免短时间反复触发浏览器恢复。
func (a *Adapter) OnPasswordLoginRefresh(ctx context.Context, cookieID string) bool {
	if a.browser == nil {
		a.logger.Warn("密码登录刷新失败：浏览器自动化已禁用")
		return false
	}
	a.mu.Lock()
	if a.lastLoginByCID == nil {
		a.lastLoginByCID = make(map[string]time.Time)
	}
	last := a.lastLoginByCID[cookieID]
	if !last.IsZero() && time.Since(last) < engine.PasswordLoginMinGap {
		remain := engine.PasswordLoginMinGap - time.Since(last)
		a.mu.Unlock()
		a.logger.Warn("密码登录刷新冷却中", "account", cookieID, "remain", remain.Round(time.Second))
		return false
	}
	a.mu.Unlock()

	d, err := a.store.Cookies.GetDetails(ctx, cookieID)
	if err != nil {
		a.logger.Warn("密码登录刷新失败：读取账号详情失败", "account", cookieID, "err", err)
		return false
	}
	a.mu.Lock()
	a.lastLoginByCID[cookieID] = time.Now()
	a.mu.Unlock()

	// 先走 Cookie 快速续期。只要旧 Cookie 还能触发快速进入，就不暴露账号密码登录，
	// 也减少滑块/人脸/设备校验等更重风控的概率。
	if a.saveRecoveredBrowserCookies(ctx, cookieID, d.UserID, func() (map[string]string, error) {
		return a.browser.CookieRenew(ctx, cookieID, d.Value, !d.ShowBrowser)
	}, "浏览器快速续期") {
		return true
	}

	if strings.TrimSpace(d.Username) == "" || strings.TrimSpace(d.Password) == "" {
		a.logger.Warn("密码登录刷新失败：账号未配置登录用户名或密码", "account", cookieID)
		return false
	}
	return a.saveRecoveredBrowserCookies(ctx, cookieID, d.UserID, func() (map[string]string, error) {
		return a.browser.PasswordLogin(ctx, d.Username, d.Password, cookieID, "", !d.ShowBrowser)
	}, "密码登录刷新")
}

// saveRecoveredBrowserCookies 执行浏览器恢复动作，统一校验、保存 Cookie 并清理 token 缓存。
// Cookie 更新后，旧 accessToken 与旧 session 绑定，继续复用会造成“新 Cookie + 旧 token”
// 的逻辑冲突，所以这里必须清除缓存，让 engine 下一轮重新派生 token。
func (a *Adapter) saveRecoveredBrowserCookies(ctx context.Context, cookieID string, userID int64, recover func() (map[string]string, error), action string) bool {
	cookies, err := recover()
	if err != nil {
		a.logger.Warn(action+"失败", "account", cookieID, "err", err)
		return false
	}
	cookieStr := browser.MarshalCookies(cookies)
	if strings.TrimSpace(cookieStr) == "" {
		a.logger.Warn(action+"失败：浏览器未返回 cookie", "account", cookieID)
		return false
	}
	if err := a.store.Cookies.Save(ctx, cookieID, cookieStr, userID); err != nil {
		a.logger.Warn(action+"失败：保存 cookie 失败", "account", cookieID, "err", err)
		return false
	}
	if a.store.Tokens != nil {
		if err := a.store.Tokens.Clear(ctx, cookieID); err != nil {
			a.logger.Warn(action+"后清除 token 缓存失败", "account", cookieID, "err", err)
		}
	}
	a.logger.Info(action+" cookie 成功", "account", cookieID)
	return true
}

// 编译期保证 *Adapter 同时实现 engine.Handler 与 automation.OrderDetailFetcher。
var (
	_ engine.Handler                = (*Adapter)(nil)
	_ automation.OrderDetailFetcher = (*Adapter)(nil)
)
