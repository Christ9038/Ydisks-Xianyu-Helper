package server

import (
	"context"
	"errors"

	orderapp "xianyu-go/internal/application/orders"
)

// orderHTTPAdapter 将 HTTP 请求模型和兼容响应模型适配到应用层订单服务。
// 订单业务编排由 internal/application/orders.ServiceSet 负责。
type orderHTTPAdapter struct {
	// services 保存应用层统一构造的订单业务服务集合。
	services OrdersPort
}

// RefreshSingle 刷新单个订单详情并转换为兼容 HTTP 响应模型。
func (a *orderHTTPAdapter) RefreshSingle(ctx context.Context, userID int64, orderID string) (orderSingleRefreshResponse, error) {
	// result、err 保存应用层单订单刷新结果和错误。
	result, err := a.services.RefreshSingle(ctx, userID, orderID)
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

// Refresh 由 a 调用订单用例，ctx 传递请求取消，userID 限定用户，cookieID、status 限定筛选；
// 返回兼容 HTTP 结果并原样透传已去重的恢复修正统计，应用错误保持原有分类。
func (a *orderHTTPAdapter) Refresh(ctx context.Context, userID int64, cookieID, status string) (orderRefreshResponse, error) {
	// result、err 保存应用层批量刷新结果和错误。
	result, err := a.services.Refresh(ctx, userID, cookieID, status)
	if errors.Is(err, orderapp.ErrForbidden) {
		return orderRefreshResponse{}, orderapp.ErrForbidden
	}
	if err != nil {
		return orderRefreshResponse{}, err
	}
	return orderRefreshResponse{PartialFailure: result.PartialFailure, Message: result.Message, Summary: orderRefreshSummary{Restored: result.Summary.Restored, Reassigned: result.Summary.Reassigned, Discovered: result.Summary.Discovered, ListUpdated: result.Summary.ListUpdated, SoftDeleted: result.Summary.SoftDeleted, DetailTotal: result.Summary.DetailTotal, Total: result.Summary.Total, Updated: result.Summary.Updated, NoChange: result.Summary.NoChange, Failed: result.Summary.Failed}, Results: refreshResultsFromApplication(result.Results)}, nil
}

// intPointer 为结果 DTO 创建整数指针，使零值也能按兼容契约显式返回。
func intPointer(value int) *int {
	return &value
}

// boolPointer 为结果 DTO 创建布尔指针，使 false 也能按兼容契约显式返回。
func boolPointer(value bool) *bool {
	return &value
}

// refreshResultsFromApplication 将应用层刷新结果转换为具名结果行。
func refreshResultsFromApplication(items []orderapp.RefreshOrderResult) []orderRefreshResultDTO {
	// results 保存兼容客户端使用的具名结果行，避免响应契约依赖动态 map。
	results := make([]orderRefreshResultDTO, 0, len(items))
	// item 是当前应用层刷新结果。
	for _, item := range items {
		// row 保存当前兼容响应行。
		row := orderRefreshResultDTO{Success: item.Success}
		if item.CookieID != "" {
			row.CookieID = item.CookieID
			row.Discovered = intPointer(item.Discovered)
			row.Updated = intPointer(item.Updated)
			if item.Success {
				// softDeleted 表示本次账号刷新是否标记了失效订单。
				softDeleted := item.SoftDeleted != 0
				row.SoftDeleted = boolPointer(softDeleted)
			}
		}
		if item.OrderID != "" {
			row.OrderID = item.OrderID
		}
		if item.Stage != "" {
			row.Stage = item.Stage
		}
		if item.Message != "" {
			row.Message = item.Message
		}
		if item.Error != "" {
			row.Error = item.Error
		}
		if item.OldStatus != "" || item.NewStatus != "" {
			row.OldStatus, row.NewStatus = item.OldStatus, item.NewStatus
		}
		results = append(results, row)
	}
	return results
}

// errOrderDetailUnsupported 用于本次流程后续判断的err订单DetailUnsupported
var errOrderDetailUnsupported = errors.New("当前 Go MTOP 客户端不支持订单详情接口")

// errOrderCredentialChanged 用于本次流程后续判断的err订单CredentialChanged
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
	// applicationErr 用于本次流程后续判断的applicationErr
	var applicationErr *orderApplicationError
	if !errors.As(err, &applicationErr) {
		return 0, false
	}
	return applicationErr.kind, true
}

// orders 返回当前 Server 绑定的订单应用服务。
func (s *Server) orders() *orderHTTPAdapter {
	return &orderHTTPAdapter{services: s.applicationServiceSet().orders}
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
	// status 用于本次流程后续判断的状态
	status := orderapp.NormalizeOrderStatus(row.OrderStatus)
	return orderDTO{
		OrderID: row.OrderID, ItemID: row.ItemID, ItemTitle: row.ItemTitle,
		ItemImage: itemImageFromDetail(row.ItemDetail), BuyerID: row.BuyerID,
		SpecName: row.SpecName, SpecValue: row.SpecValue, Quantity: row.Quantity,
		Amount: row.Amount, OrderStatus: status, Status: status, CookieID: row.CookieID,
		IsBargain: row.IsBargain, SystemShipped: row.SystemShipped,
		ReceiverName: row.ReceiverName, ReceiverPhone: row.ReceiverPhone,
		ReceiverAddress: row.ReceiverAddr, ReceiverCity: row.ReceiverCity,
		CreatedAt: normalizeOrderTimestamp(row.CreatedAt), UpdatedAt: normalizeOrderTimestamp(row.UpdatedAt),
	}
}

// orderDTOFromOrder 把订单实体和关联商品信息转换为详情响应视图。
func orderDTOFromOrder(order *orderapp.Order, item *orderapp.ItemInfo) orderDTO {
	// itemTitle、itemImage 用于本次流程后续判断的商品Title、item图片
	itemTitle, itemImage := "", ""
	if item != nil {
		itemTitle = item.ItemTitle
		itemImage = itemImageFromDetail(item.ItemDetail)
	}
	// status 用于本次流程后续判断的状态
	status := orderapp.NormalizeOrderStatus(order.OrderStatus)
	return orderDTO{
		OrderID: order.OrderID, ItemID: order.ItemID, ItemTitle: itemTitle, ItemImage: itemImage,
		BuyerID: order.BuyerID, SpecName: order.SpecName, SpecValue: order.SpecValue,
		Quantity: order.Quantity, Amount: order.Amount, OrderStatus: status, Status: status,
		CookieID: order.CookieID, IsBargain: order.IsBargain, SystemShipped: order.SystemShipped,
		ReceiverName: order.ReceiverName, ReceiverPhone: order.ReceiverPhone,
		ReceiverAddress: order.ReceiverAddress, ReceiverCity: order.ReceiverCity,
		CreatedAt: normalizeOrderTimestamp(order.CreatedAt), UpdatedAt: normalizeOrderTimestamp(order.UpdatedAt),
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
	result, err := a.services.List(ctx, orderapp.ListQuery{
		UserID: query.UserID, CookieID: query.CookieID, Status: query.Status,
		Search: query.Search, Page: query.Page, PageSize: query.PageSize,
	})
	if errors.Is(err, orderapp.ErrForbidden) {
		return orderListResult{}, orderapp.ErrForbidden
	}
	if err != nil {
		return orderListResult{}, err
	}
	// orders 用于本次流程后续判断的订单列表
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
	order, err := a.services.Get(ctx, userID, orderID)
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
	result, err := a.services.GetView(ctx, userID, orderID)
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
	err := a.services.Delete(ctx, userID, orderID)
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
	err := a.services.Update(ctx, userID, orderID, orderapp.UpdateRequest{
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
	// Results 是逐单具名结果。
	Results []orderImportResultDTO
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
	result, err := a.services.Import(ctx, userID, inputs)
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
	// results 保存兼容客户端使用的逐条具名结果。
	results := make([]orderImportResultDTO, 0, len(result.Results))
	// item 保存当前应用层导入结果行。
	for _, item := range result.Results {
		results = append(results, orderImportResultDTO{OrderID: item.OrderID, Success: item.Success, Message: item.Message})
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
	// Results 是逐单具名结果。
	Results []orderMutationResultDTO
}

// ManualShip 执行状态确认或完整自动化发货，并集中处理逐单失败而不中断批次的规则。
func (a *orderHTTPAdapter) ManualShip(ctx context.Context, request manualShipRequest) (manualShipResult, error) {
	// result 保存应用层手动发货结果。
	result, err := a.services.ManualShip(ctx, orderapp.ManualShipRequest{UserID: request.UserID, OrderIDs: request.OrderIDs, ShipMode: request.ShipMode})
	if err != nil {
		return manualShipResult{}, err
	}
	return manualShipResultFromApplication(result), nil
}

// manualShipResultFromApplication 将应用层手动发货结果转换为旧 HTTP 响应兼容模型。
func manualShipResultFromApplication(result orderapp.ManualShipResult) manualShipResult {
	// results 保存兼容客户端使用的逐条具名结果。
	results := make([]orderMutationResultDTO, 0, len(result.Results))
	// item 保存当前应用层手动发货结果行。
	for _, item := range result.Results {
		// row 保存兼容客户端使用的单条结果。
		row := orderMutationResultDTO{OrderID: item.OrderID, Status: item.Status, Success: item.Success, Message: item.Message}
		if item.ReconciliationFieldsPresent {
			row.ReconciliationID = item.ReconciliationID
			row.ReconciliationWarning = item.ReconciliationWarning
		}
		results = append(results, row)
	}
	return manualShipResult{SuccessCount: result.SuccessCount, FailedCount: result.FailedCount, Results: results}
}
