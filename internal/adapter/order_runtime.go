package adapter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	accountmanager "xianyu-go/internal/account"
	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// OrderAutomation 定义订单手动发货所需的最小自动化能力，避免订单适配器依赖具体 Center 类型。
type OrderAutomation interface {
	// ManualFullDelivery 执行一次订单完整发货并返回实际发送数量。
	ManualFullDelivery(context.Context, *db.Order) (int, error)
}

// OrderNotifier 定义订单发货结果所需的最小通知能力，通知实现不进入订单应用层。
type OrderNotifier interface {
	// NotifyDelivery 发送订单发货结果通知。
	NotifyDelivery(string, string, string, string, string, string)
}

// orderDetailMTop 是订单运行时需要的可选详情接口能力。
type orderDetailMTop interface {
	// FetchOrderDetail 请求订单详情并返回平台响应中的订单字段。
	FetchOrderDetail(context.Context, string, string) (*mtop.OrderDetailResult, error)
}

// OrderRuntimeHooks 定义 Server 运行时注入订单适配器的非持久化能力。
type OrderRuntimeHooks struct {
	// Client 返回当前使用的平台客户端。
	Client func() mtop.Client
	// ClientAvailable 判断平台客户端是否已由 Server 显式装配。
	ClientAvailable func() bool
	// AccountRunning 判断账号运行实例是否在线。
	AccountRunning func(string) bool
	// AutomationReady 判断完整发货自动化依赖是否可用。
	AutomationReady func() bool
	// ManualFullDelivery 执行完整自动化发货；订单模型转换由装配回调负责。
	ManualFullDelivery func(context.Context, *orderapp.Order) (int, error)
	// UpdateRunningCookie 同步平台响应中的最新 Cookie 到账号运行实例。
	UpdateRunningCookie func(context.Context, string, string)
	// NotifyDelivery 发送手动发货结果通知。
	NotifyDelivery func(string, string, string, string, string)
	// RecoverExpiredSession 处理平台明确的 Session 过期；MTOP Token 刷新由请求客户端负责。
	RecoverExpiredSession func(context.Context, string, error) bool
	// ReportPersistenceFailure 记录本地订单状态写入失败。
	ReportPersistenceFailure func(string, error)
	// RefreshChatConversations 按需刷新指定账号的聊天联系人缓存；未装配时订单同步保持可用但不补关联。
	RefreshChatConversations func(context.Context, string) error
	// OrderDetails 是订单刷新与自动发货共享的详情限流协调器；构造后不可替换。
	OrderDetails *OrderDetailCoordinator
}

// NewOrderRuntimeHooks 将账号、自动化和通知依赖转换为订单运行时回调；闭包只存在于 adapter 装配边界。
func NewOrderRuntimeHooks(client func() mtop.Client, manager *accountmanager.Manager, automation OrderAutomation, notifier OrderNotifier, updateCookie func(context.Context, string, string), recoverSession func(context.Context, string, error) bool) OrderRuntimeHooks {
	return OrderRuntimeHooks{
		Client:          client,
		ClientAvailable: func() bool { return client != nil && client() != nil },
		AccountRunning:  AccountRunningLookup(manager),
		AutomationReady: func() bool { return manager != nil && automation != nil },
		ManualFullDelivery: func(ctx context.Context, order *orderapp.Order) (int, error) {
			if automation == nil {
				return 0, errors.New("自动化中心未初始化")
			}
			return automation.ManualFullDelivery(ctx, OrderForAutomation(order))
		},
		UpdateRunningCookie: updateCookie,
		NotifyDelivery: func(cookieID, buyerID, itemID, chatID, message string) {
			if notifier != nil {
				notifier.NotifyDelivery(cookieID, "", buyerID, itemID, message, chatID)
			}
		},
		RecoverExpiredSession: recoverSession,
	}
}

// OrderRuntime 将订单应用 Port 适配到数据库凭证、MTOP 和账号运行时。
type OrderRuntime struct {
	// store 提供订单确认发货所需的平台凭证读取和 Cookie 写回能力。
	store *db.Store
	// hooks 保存 Server 注入的运行时回调，不保存 HTTP 或 Server 对象。
	hooks OrderRuntimeHooks
	// reconciliation 保存外部动作成功后的补偿记录端口。
	reconciliation orderapp.ReconciliationRecorder
	// logger 记录不含凭证的订单持久化错误。
	logger *slog.Logger
	// orderDetails 统一限制管理端订单刷新对同一账号发出的详情请求，并与自动发货复用在途请求。
	orderDetails *OrderDetailCoordinator
}

// NewOrderRuntime 构造订单平台与运行时适配器。
func NewOrderRuntime(store *db.Store, hooks OrderRuntimeHooks, reconciliation orderapp.ReconciliationRecorder, logger *slog.Logger) *OrderRuntime {
	// resolvedLogger 保存订单适配器使用的日志器。
	resolvedLogger := logger
	if resolvedLogger == nil {
		resolvedLogger = slog.Default()
	}
	// orderDetails 是构造期固定的详情协调器；隔离测试未注入时保留独立默认实例。
	orderDetails := hooks.OrderDetails
	if orderDetails == nil {
		orderDetails = NewOrderDetailCoordinator(resolvedLogger)
	}
	return &OrderRuntime{store: store, hooks: hooks, reconciliation: reconciliation, logger: resolvedLogger, orderDetails: orderDetails}
}

// AccountRunning 判断指定账号是否在线运行。
func (r *OrderRuntime) AccountRunning(cookieID string) bool {
	return r != nil && r.hooks.AccountRunning != nil && r.hooks.AccountRunning(cookieID)
}

// AutomationReady 判断完整发货自动化依赖是否已装配。
func (r *OrderRuntime) AutomationReady() bool {
	return r != nil && r.hooks.AutomationReady != nil && r.hooks.AutomationReady()
}

// ManualFullDelivery 执行完整自动化发货。
func (r *OrderRuntime) ManualFullDelivery(ctx context.Context, order *orderapp.Order) (int, error) {
	if r == nil || r.hooks.ManualFullDelivery == nil {
		return 0, errors.New("自动化中心未初始化")
	}
	return r.hooks.ManualFullDelivery(ctx, order)
}

// MTopAvailable 判断平台客户端是否已由 Server 显式装配。
func (r *OrderRuntime) MTopAvailable() bool {
	return r != nil && r.hooks.ClientAvailable != nil && r.hooks.ClientAvailable()
}

// mtopClient 返回订单流程使用的平台客户端。
func (r *OrderRuntime) mtopClient() mtop.Client {
	if r == nil || r.hooks.Client == nil {
		return nil
	}
	return r.hooks.Client()
}

// ConfirmShipment 使用当前账号凭证调用平台确认发货接口。
func (r *OrderRuntime) ConfirmShipment(ctx context.Context, cookieID, orderID string, userID int64) orderapp.ConsignResult {
	// success、messages、runtimeCookie、runtimeCookieChanged、callErr 保存确认发货结果及凭证变化。
	success, messages, runtimeCookie, runtimeCookieChanged, callErr := r.consignWithCurrentCookie(ctx, cookieID, orderID, userID)
	return orderapp.ConsignResult{Success: success, Messages: messages, RuntimeCookie: runtimeCookie, RuntimeCookieChanged: runtimeCookieChanged, Err: callErr}
}

// consignWithCurrentCookie 使用账号平台凭证确认发货并保存响应 Cookie。
func (r *OrderRuntime) consignWithCurrentCookie(ctx context.Context, cookieID, orderID string, userID int64) (bool, []string, string, bool, error) {
	if r == nil || r.store == nil || r.store.Cookies == nil {
		return false, nil, "", false, errors.New("订单凭证存储未初始化")
	}
	// unlock 保护当前账号凭证读取；平台确认发货开始前必须释放账号锁。
	unlock := r.store.LockAccountCredentials(cookieID)
	// detail、loadErr 保存按账号读取的平台运行凭证及错误。
	detail, loadErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, cookieID)
	if loadErr != nil {
		unlock()
		return false, nil, "", false, loadErr
	}
	if detail.UserID != userID {
		unlock()
		return false, nil, "", false, orderapp.ErrForbidden
	}
	if !hasStoredOrderCredential(detail) {
		unlock()
		return false, nil, "", false, errors.New("账号 Cookie 为空")
	}
	// requestCtx、session 保存带 Cookie 快照的平台上下文及响应会话。
	requestCtx, session := withOrderCookieSnapshot(ctx, detail)
	unlock()
	// client 保存当前平台调用客户端。
	client := r.mtopClient()
	if client == nil {
		return false, nil, "", false, errors.New("MTop 客户端未初始化")
	}
	// success、messages、updatedCookies、callErr 保存平台确认发货响应。
	success, messages, updatedCookies, callErr := client.ConsignContext(requestCtx, detail.Value, orderID)
	// value、valueChanged、handled、persistErr 保存响应 Cookie 会话写回结果。
	value, valueChanged, handled, persistErr := r.persistCurrentOrderCookieSession(ctx, detail, session, updatedCookies, userID)
	if persistErr != nil {
		// wrappedPersistErr 保存包含订单语义的 Cookie 写回错误。
		wrappedPersistErr := fmt.Errorf("保存发货响应 Cookie Jar: %w", persistErr)
		if callErr != nil {
			return success, messages, "", false, errors.Join(callErr, wrappedPersistErr)
		}
		return success, messages, "", false, wrappedPersistErr
	}
	if handled {
		// runtimeCookie 保存完整 Cookie 会话变化后的平面 Cookie 值。
		runtimeCookie := ""
		if valueChanged {
			runtimeCookie = value
		}
		return success, messages, runtimeCookie, valueChanged, callErr
	}
	if callErr != nil {
		return false, messages, "", false, callErr
	}
	return success, messages, "", false, nil
}

// UpdateRunningCookie 同步运行时账号 Cookie。
func (r *OrderRuntime) UpdateRunningCookie(ctx context.Context, cookieID, value string) {
	if r != nil && r.hooks.UpdateRunningCookie != nil {
		r.hooks.UpdateRunningCookie(ctx, cookieID, value)
	}
}

// NotifyDelivery 发送发货结果通知。
func (r *OrderRuntime) NotifyDelivery(cookieID, buyerID, itemID, chatID, message string) {
	if r != nil && r.hooks.NotifyDelivery != nil {
		r.hooks.NotifyDelivery(cookieID, buyerID, itemID, chatID, message)
	}
}

// RecordOrderReconciliation 创建外部动作成功后的补偿记录。
func (r *OrderRuntime) RecordOrderReconciliation(ctx context.Context, orderID, cookieID, kind, message string) (string, error) {
	if r == nil || r.reconciliation == nil {
		return "", errors.New("订单补偿存储未初始化")
	}
	return r.reconciliation.RecordReconciliation(ctx, orderID, cookieID, kind, message)
}

// RecordReconciliation 将补偿记录能力暴露给手动发货应用 Port。
func (r *OrderRuntime) RecordReconciliation(ctx context.Context, orderID, cookieID, kind, message string) (string, error) {
	return r.RecordOrderReconciliation(ctx, orderID, cookieID, kind, message)
}

// ReportPersistenceFailure 记录手动发货本地订单状态持久化失败。
func (r *OrderRuntime) ReportPersistenceFailure(orderID string, err error) {
	if r == nil || err == nil {
		return
	}
	r.logger.Error("更新订单为系统已发货失败", "order_id", orderID, "err", err)
	if r.hooks.ReportPersistenceFailure != nil {
		r.hooks.ReportPersistenceFailure(orderID, err)
	}
}

// RecoverExpiredSession 处理订单刷新应用服务报告的平台会话过期。
func (r *OrderRuntime) RecoverExpiredSession(ctx context.Context, cookieID string, err error) bool {
	return r != nil && r.hooks.RecoverExpiredSession != nil && r.hooks.RecoverExpiredSession(ctx, cookieID, err)
}

// DetailAvailable 判断平台详情接口是否可用。
func (r *OrderRuntime) DetailAvailable() bool {
	// client 保存当前平台客户端。
	client := r.mtopClient()
	// _, available 丢弃客户端具体类型并记录详情接口能力。
	_, available := client.(orderDetailMTop)
	return available
}

// SoldAvailable 判断平台已售订单列表接口是否可用。
func (r *OrderRuntime) SoldAvailable() bool {
	// client 保存当前平台客户端。
	client := r.mtopClient()
	// _, available 丢弃客户端具体类型并记录订单列表接口能力。
	_, available := client.(mtop.SoldOrderFetcher)
	return available
}

// CredentialAvailable 判断平台请求视图是否包含可用 Cookie。
func (r *OrderRuntime) CredentialAvailable(detail *orderapp.PlatformRuntimeData) bool {
	return detail != nil && strings.TrimSpace(detail.Value) != ""
}

// FetchOrderDetail 调用平台详情接口并收集 Cookie 会话变化。
func (r *OrderRuntime) FetchOrderDetail(ctx context.Context, detail *orderapp.PlatformRuntimeData, orderID string) (orderapp.RefreshDetailFetchResult, error) {
	// fetcher、available 保存详情接口实现及其可用状态。
	fetcher, available := r.mtopClient().(orderDetailMTop)
	if !available {
		return orderapp.RefreshDetailFetchResult{}, orderapp.ErrRefreshDetailUnsupported
	}
	if detail == nil {
		return orderapp.RefreshDetailFetchResult{}, errors.New("订单详情请求缺少账号凭证")
	}
	// requestCtx、session 保存带 Cookie 快照的平台上下文及响应会话。
	requestCtx, session := withOrderCookieSnapshot(ctx, platformRuntimeDataForOrder(detail))
	// result、callErr 保存共享限流和同订单去重后的平台详情响应及错误。
	result, callErr := r.orderDetails.Fetch(requestCtx, detail.ID, orderID, detail.Value, fetcher)
	// cookieUpdate 保存平台详情请求观察到的 Cookie 会话变化。
	cookieUpdate := orderCookieUpdate(detail, session)
	if callErr != nil {
		return orderapp.RefreshDetailFetchResult{CookieUpdate: cookieUpdate}, callErr
	}
	if result == nil {
		return orderapp.RefreshDetailFetchResult{CookieUpdate: cookieUpdate}, errors.New("订单详情接口未返回结果")
	}
	return orderapp.RefreshDetailFetchResult{Detail: &orderapp.RefreshDetail{Quantity: result.Quantity, SpecName: result.SpecName, SpecValue: result.SpecValue, OrderStatus: result.OrderStatus, Amount: result.Amount, UpdatedCookies: result.UpdatedCookies}, CookieUpdate: cookieUpdate}, nil
}

// FetchSoldOrders 用 r 已装配的客户端按 ctx 取消信号读取 detail 对应账号的全部已售订单。
// detail 中的明文凭证只用于平台请求；返回值保留已抓订单和 Cookie 更新，只有分页完整结束才返回 nil 错误。
// SellerID 仅声明每页实际请求共同使用的 unb，缺失时为空；身份冲突及中途失败均不得用于落库或软删除。
func (r *OrderRuntime) FetchSoldOrders(ctx context.Context, detail *orderapp.PlatformRuntimeData) (orderapp.RefreshSoldFetchResult, error) {
	return r.fetchSoldOrders(ctx, detail, nil)
}

// FetchSoldOrdersPaged 与 FetchSoldOrders 使用同一分页与身份校验，但每取得并校验一页就交给 onPage 提交，不累积订单。
// onPage 在平台请求之外被调用，返回错误会立即终止后续分页；已提交页保留，调用方据此禁止缺失订单清理。
// 返回值只携带最终身份与 Cookie 状态；onPage 不能为空。
func (r *OrderRuntime) FetchSoldOrdersPaged(ctx context.Context, detail *orderapp.PlatformRuntimeData, onPage orderapp.RefreshSoldPageHandler) (orderapp.RefreshSoldFetchResult, error) {
	if onPage == nil {
		return orderapp.RefreshSoldFetchResult{}, errors.New("订单列表分页处理器不能为空")
	}
	return r.fetchSoldOrders(ctx, detail, onPage)
}

// fetchSoldOrders 是两种读取方式的共同实现：onPage 为空时累积全部订单返回，否则逐页回调并只返回最终状态。
func (r *OrderRuntime) fetchSoldOrders(ctx context.Context, detail *orderapp.PlatformRuntimeData, onPage orderapp.RefreshSoldPageHandler) (orderapp.RefreshSoldFetchResult, error) {
	// client 固定本次同步的平台客户端，身份检查与分页调用使用同一实例。
	client := r.mtopClient()
	// fetcher、available 保存订单列表接口实现及其可用状态。
	fetcher, available := client.(mtop.SoldOrderFetcher)
	if !available {
		return orderapp.RefreshSoldFetchResult{}, errors.New("当前 MTop 客户端不支持订单列表发现")
	}
	if detail == nil {
		return orderapp.RefreshSoldFetchResult{}, errors.New("订单列表请求缺少账号凭证")
	}
	// requestCtx、session 保存带 Cookie 快照的平台上下文及响应会话。
	requestCtx, session := withOrderCookieSnapshot(ctx, platformRuntimeDataForOrder(detail))
	// orders 保存跨分页累积的平台订单；逐页提交模式下保持为空。
	orders := make([]orderapp.RefreshSoldOrder, 0)
	// sellerID 保存已观察到的请求 UID，identityComplete 要求每一页都具备可验证的相同身份。
	sellerID, identityComplete := "", true
	// pageNumber 是当前请求的订单列表页码。
	for pageNumber := 1; pageNumber <= orderRuntimeMaxSoldOrderPages; pageNumber++ {
		// requestSellerID、identityErr 根据当前请求 URL 的 Cookie 作用域校验身份，响应更新不反推本页身份。
		requestSellerID, identityErr := soldOrderRequestSellerID(client, detail.Value, session)
		if identityErr != nil {
			return orderapp.RefreshSoldFetchResult{Orders: orders, CookieUpdate: orderCookieUpdate(detail, session)}, identityErr
		}
		if requestSellerID == "" {
			identityComplete = false
		} else {
			if sellerID != "" && requestSellerID != sellerID {
				return orderapp.RefreshSoldFetchResult{Orders: orders, CookieUpdate: orderCookieUpdate(detail, session)}, errors.New("订单列表分页请求身份发生变化")
			}
			sellerID = requestSellerID
		}
		// page、callErr 保存当前订单列表页及错误。
		page, callErr := fetcher.FetchSoldOrdersPage(requestCtx, detail.Value, pageNumber, 30)
		if callErr != nil {
			return orderapp.RefreshSoldFetchResult{Orders: orders, CookieUpdate: orderCookieUpdate(detail, session)}, callErr
		}
		if page == nil {
			return orderapp.RefreshSoldFetchResult{Orders: orders, CookieUpdate: orderCookieUpdate(detail, session)}, errors.New("订单列表接口未返回结果")
		}
		// pageOrders 保存本页转换后的订单；整页通过结构校验后才可累积或提交。
		pageOrders := make([]orderapp.RefreshSoldOrder, 0, len(page.Items))
		// remote 是当前平台订单列表项。
		for _, remote := range page.Items {
			if strings.TrimSpace(remote.OrderID) == "" {
				return orderapp.RefreshSoldFetchResult{Orders: orders, CookieUpdate: orderCookieUpdate(detail, session)}, fmt.Errorf("订单列表第 %d 页存在缺失订单号的条目，分页不完整", pageNumber)
			}
			pageOrders = append(pageOrders, orderapp.RefreshSoldOrder{OrderID: remote.OrderID, ItemID: remote.ItemID, BuyerID: remote.BuyerID, CreatedAt: remote.CreatedAt, OrderStatus: orderapp.NormalizeOrderStatus(remote.OrderStatus), Quantity: remote.Quantity, Amount: remote.Amount, ReceiverName: remote.ReceiverName, ReceiverPhone: remote.ReceiverPhone, ReceiverAddr: remote.ReceiverAddr, ReceiverCity: remote.ReceiverCity, IsBargain: remote.IsBargain})
		}
		if page.NextPage && len(page.Items) == 0 {
			return orderapp.RefreshSoldFetchResult{Orders: append(orders, pageOrders...), CookieUpdate: orderCookieUpdate(detail, session)}, fmt.Errorf("订单列表第 %d 页为空页但 nextPage 为真，分页不完整", pageNumber)
		}
		// pageSellerID 只在此前每页身份都完整时才声明，任一页缺失身份后禁止后续页做跨账号归属修复。
		pageSellerID := sellerID
		if !identityComplete {
			pageSellerID = ""
		}
		if onPage != nil {
			// handlerErr 是调用方提交本页的错误；非空时终止后续分页，已提交页由调用方保留。
			if handlerErr := onPage(ctx, orderapp.RefreshSoldPage{SellerID: pageSellerID, Orders: pageOrders, CookieUpdate: orderCookieUpdate(detail, session)}); handlerErr != nil {
				return orderapp.RefreshSoldFetchResult{SellerID: pageSellerID, CookieUpdate: orderCookieUpdate(detail, session)}, handlerErr
			}
		} else {
			orders = append(orders, pageOrders...)
		}
		if !page.NextPage {
			return orderapp.RefreshSoldFetchResult{SellerID: pageSellerID, Orders: orders, CookieUpdate: orderCookieUpdate(detail, session)}, nil
		}
	}
	return orderapp.RefreshSoldFetchResult{Orders: orders, CookieUpdate: orderCookieUpdate(detail, session)}, fmt.Errorf("订单列表达到 %d 页上限但 nextPage 为真，分页不完整", orderRuntimeMaxSoldOrderPages)
}

// RefreshChatConversations 按需刷新订单同步所需的聊天联系人缓存。
func (r *OrderRuntime) RefreshChatConversations(ctx context.Context, cookieID string) error {
	if r == nil || r.hooks.RefreshChatConversations == nil {
		return nil
	}
	return r.hooks.RefreshChatConversations(ctx, cookieID)
}

// soldOrderRequestSellerID 从 client 本次已售请求对应的 session 读取非敏感 unb，fallback 仅用于检查旧扁平凭证冲突。
// 完整 Jar 必须按实际端点筛选，不能以 State 的 /im 视图或 fallback 冒充实际请求身份；错误不含 UID 或凭证。
func soldOrderRequestSellerID(client mtop.Client, fallback string, session *mtop.CookieSession) (string, error) {
	// endpoint 与标准已售客户端采用相同默认值，替身遵循同一平台请求契约。
	endpoint := mtop.SoldOrdersAPI
	// configured、ok 识别实际客户端的可配置端点，确保本地或代理地址使用自身 Cookie 作用域。
	if configured, ok := client.(*mtop.ClientImpl); ok && configured != nil && configured.SoldOrdersURL != "" {
		endpoint = configured.SoldOrdersURL
	}
	// requestCookies、snapshot 保存会话平面值及完整 Jar；凭证只在当前函数内用于身份提取，禁止输出。
	requestCookies, snapshot, _ := session.State()
	if snapshot != nil {
		requestCookies, _ = cookierefresh.ScopedCookieHeaderForRequest(snapshot, endpoint, "https://goofish.com", time.Now())
	}
	// sellerID、requestErr 保存实际请求 UID 和重复身份冲突，不读取买家字段推断卖家。
	sellerID, requestErr := soldOrderCookieUID(requestCookies)
	if requestErr != nil {
		return "", requestErr
	}
	// fallbackID、fallbackErr 保存兼容凭证中的 UID 和歧义错误，不能用于补齐 Jar 缺失的 UID。
	fallbackID, fallbackErr := soldOrderCookieUID(fallback)
	if fallbackErr != nil {
		return "", fallbackErr
	}
	if sellerID != "" && fallbackID != "" && sellerID != fallbackID {
		return "", errors.New("订单列表请求 Cookie 会话与扁平凭证身份冲突")
	}
	return sellerID, nil
}

// soldOrderCookieUID 只从 header 的 unb 提取非敏感平台 UID；重复且不同的值返回脱敏错误，不改变 Cookie 合并规则。
func soldOrderCookieUID(header string) (string, error) {
	// sellerID 保存当前 UID，found 记录是否出现过 unb，防止空值与非空重复项被误当作单一身份。
	sellerID, found := "", false
	// part 是当前 Cookie 键值片段，内容仅用于比较，禁止输出。
	for _, part := range strings.Split(header, ";") {
		// name、value、present 保存 Cookie 名、值及是否有等号；仅关注名称严格为 unb 的片段。
		name, value, present := strings.Cut(strings.TrimSpace(part), "=")
		if !present || strings.TrimSpace(name) != "unb" {
			continue
		}
		value = strings.TrimSpace(value)
		if found && sellerID != value {
			return "", errors.New("订单列表请求 Cookie 存在多个冲突身份")
		}
		sellerID = value
		found = true
	}
	return sellerID, nil
}

// PersistCookieSession 在凭证锁内保存应用层 Cookie 更新。
func (r *OrderRuntime) PersistCookieSession(ctx context.Context, detail *orderapp.PlatformRuntimeData, update orderapp.RefreshCookieUpdate) (string, bool, bool, error) {
	if detail == nil || !update.Handled {
		return "", false, false, nil
	}
	if !update.Changed {
		return detail.Value, false, true, nil
	}
	if r == nil || r.store == nil || r.store.Cookies == nil {
		return update.Value, update.Value != detail.Value, true, errors.New("账号 Cookie 持久化服务未初始化")
	}
	// persistErr 保存账号续期 Cookie 写入错误。
	persistErr := r.store.Cookies.UpdateRenewalCookie(ctx, detail.ID, update.Value, update.MetadataJSON, time.Now().Unix())
	if persistErr != nil {
		return update.Value, update.Value != detail.Value, true, persistErr
	}
	return update.Value, update.Value != detail.Value, true, nil
}

// IsSessionExpired 判断 err 是否明确表示 Session 失效；r 的订单刷新调用不得把 Token 失效升级为账号恢复。
func (r *OrderRuntime) IsSessionExpired(err error) bool {
	return mtop.IsSessionExpiredErr(err)
}

// withOrderCookieSnapshot 为订单平台请求挂载平面 Cookie 或完整 Cookie Jar。
func withOrderCookieSnapshot(ctx context.Context, detail db.CookiePlatformRuntimeData) (context.Context, *mtop.CookieSession) {
	// snapshot、complete 保存账号 metadata 中的 Cookie Jar 及其完整性。
	snapshot, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	if !complete {
		return mtop.WithFlatCookieSession(ctx, detail.Value)
	}
	return mtop.WithCookieSnapshot(ctx, snapshot)
}

// hasStoredOrderCredential 判断订单平台视图是否包含可用凭证。
func hasStoredOrderCredential(detail db.CookiePlatformRuntimeData) bool {
	if strings.TrimSpace(detail.Value) != "" {
		return true
	}
	// _, complete 丢弃 Cookie 快照内容，仅判断是否存在完整凭证。
	_, complete := cookierefresh.SnapshotFromMetadataOK(detail.MetadataJSON)
	return complete
}

// platformRuntimeDataForOrder 将应用平台运行视图转换为数据库适配内部模型。
func platformRuntimeDataForOrder(data *orderapp.PlatformRuntimeData) db.CookiePlatformRuntimeData {
	if data == nil {
		return db.CookiePlatformRuntimeData{}
	}
	return db.CookiePlatformRuntimeData{ID: data.ID, UserID: data.UserID, Value: data.Value, MetadataJSON: data.MetadataJSON, ShowBrowser: data.ShowBrowser}
}

// orderCookieUpdate 将 MTOP 会话状态转换为订单应用层 Cookie 更新模型。
func orderCookieUpdate(detail *orderapp.PlatformRuntimeData, session *mtop.CookieSession) orderapp.RefreshCookieUpdate {
	if detail == nil || session == nil {
		return orderapp.RefreshCookieUpdate{}
	}
	// value、snapshot、changed 保存会话当前 Cookie、快照及变化状态。
	value, snapshot, changed := session.State()
	if snapshot == nil {
		return orderapp.RefreshCookieUpdate{Value: value, Changed: changed, Handled: false}
	}
	// metadata 保存包含最新 Cookie 快照的账号元数据。
	metadata := cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
	return orderapp.RefreshCookieUpdate{Value: value, MetadataJSON: metadata, Changed: changed, Handled: true}
}

// persistOrderCookieSession 保存确认发货响应中的完整或平面 Cookie 会话。
func (r *OrderRuntime) persistOrderCookieSession(ctx context.Context, detail db.CookiePlatformRuntimeData, session *mtop.CookieSession, updatedCookies string) (string, bool, bool, error) {
	// value、snapshot、changed 保存平台 CookieSession 状态。
	value, snapshot, changed := session.State()
	if !changed {
		if snapshot != nil {
			return detail.Value, false, true, nil
		}
		return "", false, false, nil
	}
	// metadata 保存移除或更新 Cookie 快照后的账号元数据。
	metadata := cookierefresh.MetadataWithoutSnapshot(detail.MetadataJSON)
	if snapshot != nil {
		metadata = cookierefresh.MetadataWithSnapshot(detail.MetadataJSON, snapshot)
	}
	if r.store == nil || r.store.Cookies == nil {
		return value, value != detail.Value, true, errors.New("账号 Cookie 持久化 repository 未初始化")
	}
	// persistErr 保存完整 Cookie 会话写回错误。
	persistErr := r.store.Cookies.UpdateRenewalCookie(ctx, detail.ID, value, metadata, time.Now().Unix())
	if persistErr != nil {
		return value, value != detail.Value, true, persistErr
	}
	return value, value != detail.Value, true, nil
}

// persistCurrentOrderCookieSession 在写回响应前重新校验凭证版本，避免旧发货响应覆盖新登录状态。
func (r *OrderRuntime) persistCurrentOrderCookieSession(ctx context.Context, detail db.CookiePlatformRuntimeData, session *mtop.CookieSession, updatedCookies string, userID int64) (string, bool, bool, error) {
	if r == nil || r.store == nil || r.store.Cookies == nil {
		return "", false, false, errors.New("账号 Cookie 持久化 repository 未初始化")
	}
	// unlock 保护平台响应完成后的凭证复核和条件写回，锁内不执行外部 I/O。
	unlock := r.store.LockAccountCredentials(detail.ID)
	defer unlock()
	// latest、loadErr 保存平台调用完成后的当前账号凭证视图及读取错误。
	latest, loadErr := r.store.Cookies.GetCookiePlatformRuntimeData(ctx, detail.ID)
	if loadErr != nil {
		return "", false, false, loadErr
	}
	if latest.UserID != userID || latest.UserID != detail.UserID || latest.Value != detail.Value || latest.MetadataJSON != detail.MetadataJSON {
		return "", false, false, errors.New("账号凭证已变化，请重试")
	}
	// value、valueChanged、handled、persistErr 保存同一凭证版本的完整会话写回结果。
	value, valueChanged, handled, persistErr := r.persistOrderCookieSession(ctx, latest, session, updatedCookies)
	if persistErr != nil {
		return value, valueChanged, handled, persistErr
	}
	if handled || strings.TrimSpace(updatedCookies) == "" || updatedCookies == latest.Value {
		return value, valueChanged, handled, nil
	}
	// persistErr 保存旧版平台只返回扁平 Cookie 时的条件写回错误。
	if persistErr := r.store.Cookies.UpdateValueOwned(ctx, detail.ID, updatedCookies, userID); persistErr != nil {
		return "", false, false, persistErr
	}
	return updatedCookies, true, true, nil
}

// orderRuntimeMaxSoldOrderPages 限制一次订单发现最多请求的平台页数。
const orderRuntimeMaxSoldOrderPages = 100

// 确保订单运行时覆盖手动发货和订单刷新应用层端口。
var _ orderapp.ManualShipRuntime = (*OrderRuntime)(nil)
var _ orderapp.RefreshRuntime = (*OrderRuntime)(nil)
