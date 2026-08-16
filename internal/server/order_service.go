package server

import (
	"context"
	"errors"
	"log/slog"

	"xianyu-go/internal/adapter"
	orderapp "xianyu-go/internal/application/orders"
)

// serverOrderRuntimeAdapter 将 Server 的运行时能力适配为订单应用 Port。
// 适配器只存在于装配边界，订单应用服务本身不再依赖 *Server。
type serverOrderRuntimeAdapter struct {
	// runtime 保存已下沉到 adapter 包的订单运行时端口。
	runtime *adapter.OrderRuntime
}

// newServerOrderRuntime 创建订单运行时 Port 的 Server 适配器。
func newServerOrderRuntime(server *Server, reconciliation orderapp.ReconciliationRecorder) serverOrderRuntimeAdapter {
	// hooks 保存 Server 运行时回调，避免适配器反向依赖 Server 包。
	hooks := adapter.OrderRuntimeHooks{}
	if server != nil {
		hooks = adapter.OrderRuntimeHooks{
			Client:          server.mtopClient,
			ClientAvailable: func() bool { return server.MTop != nil },
			AccountRunning: func(cookieID string) bool {
				if server.Manager == nil {
					return false
				}
				// _, running 保存账号运行实例是否存在。
				_, running := server.Manager.GetInstance(cookieID)
				return running
			},
			AutomationReady: func() bool { return server.Manager != nil && server.automation != nil },
			ManualFullDelivery: func(ctx context.Context, order *orderapp.Order) (int, error) {
				if server.automation == nil {
					return 0, errors.New("自动化中心未初始化")
				}
				return server.automation.ManualFullDelivery(ctx, adapter.OrderForAutomation(order))
			},
			UpdateRunningCookie:   server.updateRunningCookie,
			NotifyDelivery:        server.notifyDelivery,
			RecoverExpiredSession: server.recoverExpiredMTOPSession,
		}
	}
	// logger 保存 Server 使用的日志器；适配器会在缺失时使用默认日志器。
	var logger = (*slog.Logger)(nil)
	if server != nil {
		logger = server.Logger
	}
	// runtime 保存已完成数据库与 Server 回调装配的订单运行时；零值 Server 只用于测试缺失依赖分支。
	var runtime *adapter.OrderRuntime
	if server == nil {
		runtime = adapter.NewOrderRuntime(nil, hooks, reconciliation, logger)
	} else {
		runtime = server.dependencies.NewOrderRuntime(hooks, reconciliation, logger)
	}
	return serverOrderRuntimeAdapter{runtime: runtime}
}

// AccountRunning 判断指定账号是否在线运行。
func (a serverOrderRuntimeAdapter) AccountRunning(cookieID string) bool {
	return a.runtime != nil && a.runtime.AccountRunning(cookieID)
}

// AutomationReady 判断完整发货自动化依赖是否已装配。
func (a serverOrderRuntimeAdapter) AutomationReady() bool {
	return a.runtime != nil && a.runtime.AutomationReady()
}

// ManualFullDelivery 执行完整自动化发货。
func (a serverOrderRuntimeAdapter) ManualFullDelivery(ctx context.Context, order *orderapp.Order) (int, error) {
	if a.runtime == nil {
		return 0, errors.New("订单运行时未初始化")
	}
	return a.runtime.ManualFullDelivery(ctx, order)
}

// MTopAvailable 判断平台客户端是否已注入。
func (a serverOrderRuntimeAdapter) MTopAvailable() bool {
	return a.runtime != nil && a.runtime.MTopAvailable()
}

// ConfirmShipment 将 Server 的确认发货能力适配为应用层结果模型。
func (a serverOrderRuntimeAdapter) ConfirmShipment(ctx context.Context, cookieID, orderID string, userID int64) orderapp.ConsignResult {
	if a.runtime == nil {
		return orderapp.ConsignResult{Err: errors.New("订单运行时未初始化")}
	}
	return a.runtime.ConfirmShipment(ctx, cookieID, orderID, userID)
}

// updateRunningCookie 同步运行时账号 Cookie。
func (a serverOrderRuntimeAdapter) updateRunningCookie(ctx context.Context, cookieID, value string) {
	if a.runtime != nil {
		a.runtime.UpdateRunningCookie(ctx, cookieID, value)
	}
}

// UpdateRunningCookie 将运行时账号 Cookie 能力暴露给手动发货应用 Port。
func (a serverOrderRuntimeAdapter) UpdateRunningCookie(ctx context.Context, cookieID, value string) {
	a.updateRunningCookie(ctx, cookieID, value)
}

// notifyDelivery 发送发货结果通知。
func (a serverOrderRuntimeAdapter) notifyDelivery(cookieID, buyerID, itemID, chatID, message string) {
	if a.runtime != nil {
		a.runtime.NotifyDelivery(cookieID, buyerID, itemID, chatID, message)
	}
}

// NotifyDelivery 将发货通知能力暴露给手动发货应用 Port。
func (a serverOrderRuntimeAdapter) NotifyDelivery(cookieID, buyerID, itemID, chatID, message string) {
	a.notifyDelivery(cookieID, buyerID, itemID, chatID, message)
}

// RecordOrderReconciliation 将外部动作成功后的本地状态异常写入补偿记录。
func (a serverOrderRuntimeAdapter) RecordOrderReconciliation(ctx context.Context, orderID, cookieID, kind, message string) (string, error) {
	if a.runtime == nil {
		return "", errors.New("订单运行时未初始化")
	}
	return a.runtime.RecordOrderReconciliation(ctx, orderID, cookieID, kind, message)
}

// RecordReconciliation 将补偿记录能力暴露给手动发货应用 Port。
func (a serverOrderRuntimeAdapter) RecordReconciliation(ctx context.Context, orderID, cookieID, kind, message string) (string, error) {
	return a.RecordOrderReconciliation(ctx, orderID, cookieID, kind, message)
}

// ReportPersistenceFailure 记录手动发货本地订单状态持久化失败。
func (a serverOrderRuntimeAdapter) ReportPersistenceFailure(orderID string, err error) {
	if a.runtime != nil {
		a.runtime.ReportPersistenceFailure(orderID, err)
	}
}

// RecoverExpiredSession 处理订单刷新应用服务报告的平台会话过期。
func (a serverOrderRuntimeAdapter) RecoverExpiredSession(ctx context.Context, cookieID string, err error) bool {
	return a.runtime != nil && a.runtime.RecoverExpiredSession(ctx, cookieID, err)
}

// DetailAvailable 判断订单详情接口是否可用。
func (a serverOrderRuntimeAdapter) DetailAvailable() bool {
	return a.runtime != nil && a.runtime.DetailAvailable()
}

// SoldAvailable 判断已售订单列表接口是否可用。
func (a serverOrderRuntimeAdapter) SoldAvailable() bool {
	return a.runtime != nil && a.runtime.SoldAvailable()
}

// CredentialAvailable 判断平台请求视图是否包含可用 Cookie。
func (a serverOrderRuntimeAdapter) CredentialAvailable(detail *orderapp.PlatformRuntimeData) bool {
	return a.runtime != nil && a.runtime.CredentialAvailable(detail)
}

// FetchOrderDetail 调用平台详情接口并收集 Cookie 会话变化。
func (a serverOrderRuntimeAdapter) FetchOrderDetail(ctx context.Context, detail *orderapp.PlatformRuntimeData, orderID string) (orderapp.RefreshDetailFetchResult, error) {
	if a.runtime == nil {
		return orderapp.RefreshDetailFetchResult{}, errors.New("订单运行时未初始化")
	}
	return a.runtime.FetchOrderDetail(ctx, detail, orderID)
}

// FetchSoldOrders 调用平台已售订单接口并收集 Cookie 会话变化。
func (a serverOrderRuntimeAdapter) FetchSoldOrders(ctx context.Context, detail *orderapp.PlatformRuntimeData) (orderapp.RefreshSoldFetchResult, error) {
	if a.runtime == nil {
		return orderapp.RefreshSoldFetchResult{}, errors.New("订单运行时未初始化")
	}
	return a.runtime.FetchSoldOrders(ctx, detail)
}

// PersistCookieSession 在凭证锁内保存应用层 Cookie 更新。
func (a serverOrderRuntimeAdapter) PersistCookieSession(ctx context.Context, detail *orderapp.PlatformRuntimeData, update orderapp.RefreshCookieUpdate) (string, bool, bool, error) {
	if a.runtime == nil {
		if detail == nil {
			return update.Value, false, update.Handled, errors.New("订单运行时未初始化")
		}
		return update.Value, update.Value != detail.Value, update.Handled, errors.New("订单运行时未初始化")
	}
	return a.runtime.PersistCookieSession(ctx, detail, update)
}

// IsSessionExpired 判断平台错误是否为会话过期。
func (a serverOrderRuntimeAdapter) IsSessionExpired(err error) bool {
	return a.runtime != nil && a.runtime.IsSessionExpired(err)
}

// orderHTTPAdapter 将 HTTP 请求模型和兼容响应模型适配到应用层订单服务。
// 订单业务编排由 internal/application/orders.ServiceSet 负责。
type orderHTTPAdapter struct {
	// services 保存应用层统一构造的订单业务服务集合。
	services *orderapp.ServiceSet
	// repository 保存 HTTP 适配器需要的账号归属查询 Port。
	repository orderapp.Repository
}

// RefreshSingle 刷新单个订单详情并转换为兼容 HTTP 响应模型。
func (a *orderHTTPAdapter) RefreshSingle(ctx context.Context, userID int64, orderID string) (orderSingleRefreshResponse, error) {
	// result、err 保存应用层单订单刷新结果和错误。
	result, err := a.services.Refresh.RefreshSingle(ctx, userID, orderID)
	err = adapter.NormalizeOrderError(err)
	if errors.Is(err, orderapp.ErrNotFound) {
		return orderSingleRefreshResponse{}, orderapp.ErrNotFound
	}
	if errors.Is(err, orderapp.ErrForbidden) {
		return orderSingleRefreshResponse{}, orderapp.ErrForbidden
	}
	if errors.Is(err, orderapp.ErrRefreshDetailUnsupported) {
		return orderSingleRefreshResponse{}, errOrderDetailUnsupported
	}
	if errors.Is(err, orderapp.ErrRefreshCredentialChanged) {
		return orderSingleRefreshResponse{}, errOrderCredentialChanged
	}
	if err != nil {
		return orderSingleRefreshResponse{}, err
	}
	return orderSingleRefreshResponse{Success: result.Success, Message: result.Message, Order: orderRefreshDetailResponse{Quantity: result.Detail.Quantity, SpecName: result.Detail.SpecName, SpecValue: result.Detail.SpecValue, OrderStatus: orderapp.NormalizeOrderStatus(result.Detail.OrderStatus), Amount: result.Detail.Amount}}, nil
}

// Refresh 刷新当前用户订单并转换为兼容 HTTP 响应模型。
func (a *orderHTTPAdapter) Refresh(ctx context.Context, userID int64, cookieID, status string) (orderRefreshResponse, error) {
	// result、err 保存应用层批量刷新结果和错误。
	result, err := a.services.Refresh.Refresh(ctx, userID, cookieID, status)
	err = adapter.NormalizeOrderError(err)
	if errors.Is(err, orderapp.ErrForbidden) {
		return orderRefreshResponse{}, orderapp.ErrForbidden
	}
	if err != nil {
		return orderRefreshResponse{}, err
	}
	return orderRefreshResponse{PartialFailure: result.PartialFailure, Message: result.Message, Summary: orderRefreshSummary{Discovered: result.Summary.Discovered, ListUpdated: result.Summary.ListUpdated, SoftDeleted: result.Summary.SoftDeleted, DetailTotal: result.Summary.DetailTotal, Total: result.Summary.Total, Updated: result.Summary.Updated, NoChange: result.Summary.NoChange, Failed: result.Summary.Failed}, Results: refreshResultsFromApplication(result.Results)}, nil
}

// refreshResultsFromApplication 将应用层刷新结果转换为兼容动态结果行。
func refreshResultsFromApplication(items []orderapp.RefreshOrderResult) []map[string]any {
	// results 保存兼容客户端使用的结果行。
	results := make([]map[string]any, 0, len(items))
	// item 是当前应用层刷新结果。
	for _, item := range items {
		// row 保存当前兼容响应行。
		row := map[string]any{"success": item.Success}
		if item.CookieID != "" {
			row["cookie_id"], row["discovered"], row["updated"] = item.CookieID, item.Discovered, item.Updated
			if item.Success {
				row["soft_deleted"] = item.SoftDeleted
			}
		}
		if item.OrderID != "" {
			row["order_id"] = item.OrderID
		}
		if item.Stage != "" {
			row["stage"] = item.Stage
		}
		if item.Message != "" {
			row["message"] = item.Message
		}
		if item.Error != "" {
			row["error"] = item.Error
		}
		if item.OldStatus != "" || item.NewStatus != "" {
			row["old_status"], row["new_status"] = item.OldStatus, item.NewStatus
		}
		results = append(results, row)
	}
	return results
}

// errOrderDetailUnsupported 保存err订单DetailUnsupported，供当前处理流程使用
var errOrderDetailUnsupported = errors.New("当前 Go MTOP 客户端不支持订单详情接口")

// errOrderCredentialChanged 保存err订单CredentialChanged，供当前处理流程使用
var errOrderCredentialChanged = errors.New("账号凭证已变化，请重试")

// orderErrorKind 标识应用服务错误的业务分类，避免 HTTP 层依赖错误文本判断状态码。
type orderErrorKind uint8

const (
	// orderErrorBadRequest 表示请求字段不满足订单业务约束。
	orderErrorBadRequest orderErrorKind = iota + 1
)

// orderApplicationError 是带业务分类的订单应用服务错误。
type orderApplicationError struct {
	// kind 是错误所属的业务分类。
	kind orderErrorKind
	// err 是底层可读错误。
	err error
}

// Error 返回订单应用服务错误文本。
func (e *orderApplicationError) Error() string { return e.err.Error() }

// Unwrap 暴露底层错误，保留 errors.Is/As 的兼容能力。
func (e *orderApplicationError) Unwrap() error { return e.err }

// newOrderBadRequest 创建订单字段校验错误。
func newOrderBadRequest(message string) error {
	return &orderApplicationError{kind: orderErrorBadRequest, err: errors.New(message)}
}

// orderErrorKindOf 读取订单应用服务错误分类。
func orderErrorKindOf(err error) (orderErrorKind, bool) {
	// applicationErr 保存applicationErr，供当前处理流程使用
	var applicationErr *orderApplicationError
	if !errors.As(err, &applicationErr) {
		return 0, false
	}
	return applicationErr.kind, true
}

// orders 返回当前 Server 绑定的订单应用服务。
func (s *Server) orders() *orderHTTPAdapter {
	return s.applicationServiceSet().orders
}

// orderOwnedByUser 判断订单服务使用的账号是否归属于当前用户。
func (a *orderHTTPAdapter) orderOwnedByUser(ctx context.Context, userID int64, cookieID string) bool {
	// owned 和 err 保存账号归属检查结果。
	owned, err := a.repository.ExistsOwned(ctx, userID, cookieID)
	return err == nil && owned
}

// orderListQuery 描述订单列表的业务查询条件。
type orderListQuery struct {
	// UserID 是当前登录用户标识。
	UserID int64
	// CookieID 是可选的账号筛选条件。
	CookieID string
	// Status 是可选的订单状态筛选条件。
	Status string
	// Search 是订单号、商品或买家搜索词。
	Search string
	// Page 是请求页码。
	Page int
	// PageSize 是请求页大小。
	PageSize int
}

// orderListResult 返回订单列表及分页统计。
type orderListResult struct {
	// Orders 是已经完成状态归一和商品图片映射的订单视图。
	Orders []orderDTO
	// Total 是符合条件的订单总数。
	Total int
	// Page 是规范化后的页码。
	Page int
	// PageSize 是规范化后的每页数量。
	PageSize int
	// TotalPages 是总页数。
	TotalPages int
}

// orderDTOFromRow 把数据库订单列表行转换为稳定的订单响应视图。
func orderDTOFromRow(row orderapp.OrderRow) orderDTO {
	// status 保存状态，供当前处理流程使用
	status := orderapp.NormalizeOrderStatus(row.OrderStatus)
	return orderDTO{
		OrderID: row.OrderID, ItemID: row.ItemID, ItemTitle: row.ItemTitle,
		ItemImage: itemImageFromDetail(row.ItemDetail), BuyerID: row.BuyerID,
		SpecName: row.SpecName, SpecValue: row.SpecValue, Quantity: row.Quantity,
		Amount: row.Amount, OrderStatus: status, Status: status, CookieID: row.CookieID,
		IsBargain: row.IsBargain, SystemShipped: row.SystemShipped,
		ReceiverName: row.ReceiverName, ReceiverPhone: row.ReceiverPhone,
		ReceiverAddress: row.ReceiverAddr, ReceiverCity: row.ReceiverCity,
		CreatedAt: row.CreatedAt, UpdatedAt: row.UpdatedAt,
	}
}

// orderDTOFromOrder 把订单实体和关联商品信息转换为详情响应视图。
func orderDTOFromOrder(order *orderapp.Order, item *orderapp.ItemInfo) orderDTO {
	// itemTitle、itemImage 保存商品Title、item图片，供当前处理流程使用
	itemTitle, itemImage := "", ""
	if item != nil {
		itemTitle = item.ItemTitle
		itemImage = itemImageFromDetail(item.ItemDetail)
	}
	// status 保存状态，供当前处理流程使用
	status := orderapp.NormalizeOrderStatus(order.OrderStatus)
	return orderDTO{
		OrderID: order.OrderID, ItemID: order.ItemID, ItemTitle: itemTitle, ItemImage: itemImage,
		BuyerID: order.BuyerID, SpecName: order.SpecName, SpecValue: order.SpecValue,
		Quantity: order.Quantity, Amount: order.Amount, OrderStatus: status, Status: status,
		CookieID: order.CookieID, IsBargain: order.IsBargain, SystemShipped: order.SystemShipped,
		ReceiverName: order.ReceiverName, ReceiverPhone: order.ReceiverPhone,
		ReceiverAddress: order.ReceiverAddress, ReceiverCity: order.ReceiverCity,
		CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt,
	}
}

// orderDetailResult 返回订单详情的统一响应视图。
type orderDetailResult struct {
	// Order 是已经完成商品信息补全的订单视图。
	Order orderDTO
}

// List 查询当前用户可见的订单，并集中处理分页和账号所有权规则。
func (a *orderHTTPAdapter) List(ctx context.Context, query orderListQuery) (orderListResult, error) {
	// result、err 保存应用层订单列表结果及错误。
	result, err := a.services.List.List(ctx, orderapp.ListQuery{
		UserID: query.UserID, CookieID: query.CookieID, Status: query.Status,
		Search: query.Search, Page: query.Page, PageSize: query.PageSize,
	})
	err = adapter.NormalizeOrderError(err)
	if errors.Is(err, orderapp.ErrForbidden) {
		return orderListResult{}, orderapp.ErrForbidden
	}
	if err != nil {
		return orderListResult{}, err
	}
	// orders 保存订单列表，供当前处理流程使用
	orders := make([]orderDTO, 0, len(result.Rows))
	// row 表示当前遍历过程中的row
	for _, row := range result.Rows {
		orders = append(orders, orderDTOFromRow(row))
	}
	return orderListResult{
		Orders: orders, Total: result.Total, Page: result.Page, PageSize: result.PageSize,
		TotalPages: result.TotalPages,
	}, nil
}

// Get 查询订单并校验订单绑定账号属于当前用户。
func (a *orderHTTPAdapter) Get(ctx context.Context, userID int64, orderID string) (*orderapp.Order, error) {
	// order、err 保存应用层订单详情结果及错误。
	order, err := a.services.Detail.Get(ctx, userID, orderID)
	err = adapter.NormalizeOrderError(err)
	if errors.Is(err, orderapp.ErrNotFound) {
		return nil, orderapp.ErrNotFound
	}
	if errors.Is(err, orderapp.ErrForbidden) {
		return nil, orderapp.ErrForbidden
	}
	return order, err
}

// GetView 查询订单并补全商品标题和主图，供详情 handler 直接编码。
func (a *orderHTTPAdapter) GetView(ctx context.Context, userID int64, orderID string) (orderDetailResult, error) {
	// result、err 保存应用层详情结果及错误。
	result, err := a.services.Detail.GetView(ctx, userID, orderID)
	err = adapter.NormalizeOrderError(err)
	if errors.Is(err, orderapp.ErrNotFound) {
		return orderDetailResult{}, orderapp.ErrNotFound
	}
	if errors.Is(err, orderapp.ErrForbidden) {
		return orderDetailResult{}, orderapp.ErrForbidden
	}
	if err != nil {
		return orderDetailResult{}, err
	}
	return orderDetailResult{Order: orderDTOFromOrder(result.Order, result.Item)}, nil
}

// Delete 逻辑删除订单，保留历史记录供审计使用。
func (a *orderHTTPAdapter) Delete(ctx context.Context, userID int64, orderID string) error {
	// err 保存应用层订单删除错误。
	err := a.services.Delete.Delete(ctx, userID, orderID)
	err = adapter.NormalizeOrderError(err)
	if errors.Is(err, orderapp.ErrForbidden) {
		return orderapp.ErrForbidden
	}
	if errors.Is(err, orderapp.ErrNotFound) {
		return orderapp.ErrNotFound
	}
	return err
}

// orderUpdateRequest 描述订单更新中允许修改的字段。
type orderUpdateRequest struct {
	// OrderStatus 是订单状态补丁。
	OrderStatus *string
	// ItemID 是商品标识补丁。
	ItemID *string
	// BuyerID 是买家标识补丁。
	BuyerID *string
	// SpecName 是规格名称补丁。
	SpecName *string
	// SpecValue 是规格值补丁。
	SpecValue *string
	// Quantity 是购买数量补丁。
	Quantity *string
	// Amount 是订单金额补丁。
	Amount *string
	// ReceiverName 是收货人补丁。
	ReceiverName *string
	// ReceiverPhone 是收货电话补丁。
	ReceiverPhone *string
	// ReceiverAddress 是收货地址补丁。
	ReceiverAddress *string
	// ReceiverCity 是收货城市补丁。
	ReceiverCity *string
	// ChatID 是聊天会话补丁。
	ChatID *string
	// SystemShipped 表示是否由系统完成发货。
	SystemShipped *bool
	// ItemTitle 是关联商品标题补丁。
	ItemTitle *string
}

// Update 在单事务内更新订单及可选的商品标题。
func (a *orderHTTPAdapter) Update(ctx context.Context, userID int64, orderID string, request orderUpdateRequest) error {
	// err 保存应用层订单更新错误。
	err := a.services.Update.Update(ctx, userID, orderID, orderapp.UpdateRequest{
		OrderStatus: request.OrderStatus, ItemID: request.ItemID, BuyerID: request.BuyerID,
		SpecName: request.SpecName, SpecValue: request.SpecValue, Quantity: request.Quantity,
		Amount: request.Amount, ReceiverName: request.ReceiverName, ReceiverPhone: request.ReceiverPhone,
		ReceiverAddress: request.ReceiverAddress, ReceiverCity: request.ReceiverCity, ChatID: request.ChatID,
		SystemShipped: request.SystemShipped, ItemTitle: request.ItemTitle,
	})
	if err == nil {
		return nil
	}
	// validationErr 保存应用层字段校验错误。
	var validationErr *orderapp.ValidationError
	if errors.As(err, &validationErr) {
		return newOrderBadRequest(validationErr.Error())
	}
	err = adapter.NormalizeOrderError(err)
	if errors.Is(err, orderapp.ErrForbidden) {
		return orderapp.ErrForbidden
	}
	if errors.Is(err, orderapp.ErrNotFound) {
		return orderapp.ErrNotFound
	}
	return err
}

// orderImportResult 描述批量导入的逐单结果和统计。
type orderImportResult struct {
	// Total 是本次导入的订单数。
	Total int
	// SuccessCount 是成功导入数。
	SuccessCount int
	// FailedCount 是失败导入数。
	FailedCount int
	// Results 是逐单结果。
	Results []map[string]any
}

// Import 按当前用户账号所有权逐单导入订单，并为订单关联商品补全基础信息。
func (a *orderHTTPAdapter) Import(ctx context.Context, userID int64, rawOrders []map[string]any) (orderImportResult, error) {
	// inputs 保存文件/HTTP 原始数据转换后的应用导入行。
	inputs := make([]orderapp.ImportOrder, 0, len(rawOrders))
	// raw 保存当前待转换的原始导入行。
	for _, raw := range rawOrders {
		inputs = append(inputs, importOrderFromRaw(raw))
	}
	// result、err 保存应用层导入结果和错误。
	result, err := a.services.Import.Import(ctx, userID, inputs)
	if err != nil {
		return orderImportResult{}, err
	}
	return orderImportResultFromApplication(result), nil
}

// importOrderFromRaw 将文件/HTTP 适配层的动态字段转换为应用层订单导入命令。
func importOrderFromRaw(raw map[string]any) orderapp.ImportOrder {
	return orderapp.ImportOrder{
		OrderID: firstImportString(raw, "order_id"), CookieID: firstImportString(raw, "cookie_id"),
		ItemID: firstImportString(raw, "item_id"), ItemTitle: firstImportString(raw, "item_title"),
		ItemPrice: firstImportString(raw, "item_price"), ItemDetail: firstImportString(raw, "item_detail", "item_description"),
		BuyerID: firstImportString(raw, "buyer_id"), OrderStatus: firstImportString(raw, "order_status", "status", "status_text"),
		SpecName: firstImportString(raw, "spec_name"), SpecValue: firstImportString(raw, "spec_value"),
		Quantity: firstImportString(raw, "quantity"), Amount: firstImportString(raw, "amount"),
		ReceiverName: firstImportString(raw, "receiver_name"), ReceiverPhone: firstImportString(raw, "receiver_phone"),
		ReceiverAddress: firstImportString(raw, "receiver_address"), ReceiverCity: firstImportString(raw, "receiver_city"),
		ChatID: firstImportString(raw, "chat_id"),
	}
}

// orderImportResultFromApplication 将应用层导入结果转换为旧 HTTP 响应兼容模型。
func orderImportResultFromApplication(result orderapp.ImportResult) orderImportResult {
	// results 保存兼容客户端使用的逐条动态结果。
	results := make([]map[string]any, 0, len(result.Results))
	// item 保存当前应用层导入结果行。
	for _, item := range result.Results {
		results = append(results, map[string]any{"order_id": item.OrderID, "success": item.Success, "message": item.Message})
	}
	return orderImportResult{Total: result.Total, SuccessCount: result.SuccessCount, FailedCount: result.FailedCount, Results: results}
}

// manualShipRequest 描述批量手动发货请求。
type manualShipRequest struct {
	// UserID 是当前登录用户标识。
	UserID int64
	// OrderIDs 是待处理订单标识列表。
	OrderIDs []string
	// ShipMode 是发货模式。
	ShipMode string
}

// manualShipResult 描述批量手动发货结果。
type manualShipResult struct {
	// SuccessCount 是成功发货数。
	SuccessCount int
	// FailedCount 是失败发货数。
	FailedCount int
	// Results 是逐单结果。
	Results []map[string]any
}

// ManualShip 执行状态确认或完整自动化发货，并集中处理逐单失败而不中断批次的规则。
func (a *orderHTTPAdapter) ManualShip(ctx context.Context, request manualShipRequest) (manualShipResult, error) {
	// result 保存应用层手动发货结果。
	result, err := a.services.ManualShip.ManualShip(ctx, orderapp.ManualShipRequest{UserID: request.UserID, OrderIDs: request.OrderIDs, ShipMode: request.ShipMode})
	if err != nil {
		return manualShipResult{}, err
	}
	return manualShipResultFromApplication(result), nil
}

// manualShipResultFromApplication 将应用层手动发货结果转换为旧 HTTP 响应兼容模型。
func manualShipResultFromApplication(result orderapp.ManualShipResult) manualShipResult {
	// results 保存兼容客户端使用的逐条动态结果。
	results := make([]map[string]any, 0, len(result.Results))
	// item 保存当前应用层手动发货结果行。
	for _, item := range result.Results {
		// row 保存兼容客户端使用的单条结果。
		row := map[string]any{"order_id": item.OrderID, "status": item.Status, "success": item.Success, "message": item.Message}
		if item.ReconciliationFieldsPresent {
			row["reconciliation_id"] = item.ReconciliationID
			row["reconciliation_warning"] = item.ReconciliationWarning
		}
		results = append(results, row)
	}
	return manualShipResult{SuccessCount: result.SuccessCount, FailedCount: result.FailedCount, Results: results}
}
