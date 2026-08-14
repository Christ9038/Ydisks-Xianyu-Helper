package server

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

// orderApplicationService 承载订单用例的业务编排，不依赖 HTTP 请求或响应对象。
// HTTP handler 只负责把请求转换为这些类型，再把结果编码为兼容 DTO。
type orderApplicationService struct {
	// server 提供订单服务访问数据库、平台客户端和运行时依赖。
	server *Server
}

var errOrderDetailUnsupported = errors.New("当前 Go MTOP 客户端不支持订单详情接口")
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
	var applicationErr *orderApplicationError
	if !errors.As(err, &applicationErr) {
		return 0, false
	}
	return applicationErr.kind, true
}

// orders 返回当前 Server 绑定的订单应用服务。
func (s *Server) orders() *orderApplicationService {
	return &orderApplicationService{server: s}
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
func orderDTOFromRow(row db.OrderRow) orderDTO {
	status := db.NormalizeOrderStatus(row.OrderStatus)
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
func orderDTOFromOrder(order *db.Order, item *db.ItemInfo) orderDTO {
	itemTitle, itemImage := "", ""
	if item != nil {
		itemTitle = item.ItemTitle
		itemImage = itemImageFromDetail(item.ItemDetail)
	}
	status := db.NormalizeOrderStatus(order.OrderStatus)
	return orderDTO{
		OrderID: order.OrderID, ItemID: order.ItemID, ItemTitle: itemTitle, ItemImage: itemImage,
		BuyerID: order.BuyerID, SpecName: order.SpecName, SpecValue: order.SpecValue,
		Quantity: order.Quantity, Amount: order.Amount, OrderStatus: status, Status: status,
		CookieID: order.CookieID, IsBargain: order.IsBargain, SystemShipped: order.SystemShipped,
		ReceiverName: order.ReceiverName, ReceiverPhone: order.ReceiverPhone,
		ReceiverAddress: order.ReceiverAddr, ReceiverCity: order.ReceiverCity,
		CreatedAt: order.CreatedAt, UpdatedAt: order.UpdatedAt,
	}
}

// orderDetailResult 返回订单详情的统一响应视图。
type orderDetailResult struct {
	// Order 是已经完成商品信息补全的订单视图。
	Order orderDTO
}

// List 查询当前用户可见的订单，并集中处理分页和账号所有权规则。
func (a *orderApplicationService) List(ctx context.Context, query orderListQuery) (orderListResult, error) {
	if query.Page < 1 {
		query.Page = 1
	}
	if query.PageSize < 1 {
		query.PageSize = 20
	}
	if query.PageSize > 200 {
		query.PageSize = 200
	}
	if query.CookieID != "" && !a.server.cookieOwnedByUser(ctx, query.UserID, query.CookieID) {
		return orderListResult{}, db.ErrForbidden
	}
	offset := (query.Page - 1) * query.PageSize
	rows, total, err := a.server.Store.Orders.ListForUser(ctx, db.OrderListFilter{
		UserID: query.UserID, CookieID: query.CookieID, Status: query.Status,
		Search: query.Search, Limit: query.PageSize, Offset: offset,
	})
	if err != nil {
		return orderListResult{}, err
	}
	orders := make([]orderDTO, 0, len(rows))
	for _, row := range rows {
		orders = append(orders, orderDTOFromRow(row))
	}
	return orderListResult{
		Orders: orders, Total: total, Page: query.Page, PageSize: query.PageSize,
		TotalPages: (total + query.PageSize - 1) / query.PageSize,
	}, nil
}

// Get 查询订单并校验订单绑定账号属于当前用户。
func (a *orderApplicationService) Get(ctx context.Context, userID int64, orderID string) (*db.Order, error) {
	order, err := a.server.Store.Orders.Get(ctx, orderID)
	if err != nil || order == nil {
		if err != nil {
			return nil, err
		}
		return nil, db.ErrNotFound
	}
	if strings.TrimSpace(order.CookieID) == "" {
		return nil, db.ErrForbidden
	}
	if !a.server.cookieOwnedByUser(ctx, userID, order.CookieID) {
		return nil, db.ErrForbidden
	}
	return order, nil
}

// GetView 查询订单并补全商品标题和主图，供详情 handler 直接编码。
func (a *orderApplicationService) GetView(ctx context.Context, userID int64, orderID string) (orderDetailResult, error) {
	order, err := a.Get(ctx, userID, orderID)
	if err != nil {
		return orderDetailResult{}, err
	}
	item, itemErr := a.server.Store.Items.Get(ctx, order.CookieID, order.ItemID)
	if itemErr != nil {
		item = nil
	}
	return orderDetailResult{Order: orderDTOFromOrder(order, item)}, nil
}

// Delete 逻辑删除订单，保留历史记录供审计使用。
func (a *orderApplicationService) Delete(ctx context.Context, userID int64, orderID string) error {
	if _, err := a.Get(ctx, userID, orderID); err != nil {
		return err
	}
	_, err := a.server.Store.Orders.SoftDelete(ctx, orderID)
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
func (a *orderApplicationService) Update(ctx context.Context, userID int64, orderID string, request orderUpdateRequest) error {
	order, err := a.Get(ctx, userID, orderID)
	if err != nil {
		return err
	}
	if request.OrderStatus != nil {
		normalized := db.NormalizeOrderStatus(strings.TrimSpace(*request.OrderStatus))
		if !validEditableOrderStatus(normalized) {
			return newOrderBadRequest("不支持的订单状态")
		}
		request.OrderStatus = &normalized
	}
	if request.Amount != nil {
		normalized, ok := normalizeOrderAmount(*request.Amount)
		if !ok {
			return newOrderBadRequest("订单金额必须是普通格式的非负有限数字")
		}
		request.Amount = &normalized
	}
	finalItemID := strings.TrimSpace(order.ItemID)
	if request.ItemID != nil {
		finalItemID = strings.TrimSpace(*request.ItemID)
		request.ItemID = &finalItemID
	}
	itemTitle := ""
	if request.ItemTitle != nil {
		itemTitle = strings.TrimSpace(*request.ItemTitle)
		if itemTitle == "" || finalItemID == "" {
			return newOrderBadRequest("商品标题不能为空且订单必须关联商品")
		}
	}
	return a.server.withTransaction(ctx, func(tx *sql.Tx) error {
		if err := a.server.Store.Orders.PatchTx(ctx, tx, orderID, db.OrderPatch{
			OrderStatus: request.OrderStatus, ItemID: request.ItemID, BuyerID: request.BuyerID,
			SpecName: request.SpecName, SpecValue: request.SpecValue, Quantity: request.Quantity,
			Amount: request.Amount, ReceiverName: request.ReceiverName, ReceiverPhone: request.ReceiverPhone,
			ReceiverAddr: request.ReceiverAddress, ReceiverCity: request.ReceiverCity, ChatID: request.ChatID,
			SystemShipped: request.SystemShipped,
		}); err != nil {
			return err
		}
		if request.ItemTitle != nil {
			if err := a.server.Store.Items.UpsertBasicTx(ctx, tx, &db.ItemInfoRow{
				CookieID: order.CookieID, ItemID: finalItemID, ItemTitle: itemTitle,
			}); err != nil {
				return fmt.Errorf("更新商品标题失败: %w", err)
			}
		}
		return nil
	})
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
func (a *orderApplicationService) Import(ctx context.Context, userID int64, rawOrders []map[string]any) (orderImportResult, error) {
	ownedCookieIDs, err := a.server.Store.Cookies.ListOwnedIDs(ctx, userID)
	if err != nil {
		return orderImportResult{}, err
	}
	defaultCookieID := ""
	if len(ownedCookieIDs) == 1 {
		defaultCookieID = ownedCookieIDs[0]
	}
	result := orderImportResult{Total: len(rawOrders), Results: make([]map[string]any, 0, len(rawOrders))}
	for _, raw := range rawOrders {
		if err := a.importOne(ctx, ownedCookieIDs, defaultCookieID, raw, &result); err != nil {
			result.FailedCount++
			result.Results = append(result.Results, errResult(raw, err.Error()))
			continue
		}
		result.SuccessCount++
		result.Results = append(result.Results, map[string]any{"order_id": firstImportString(raw, "order_id"), "success": true, "message": "订单已导入"})
	}
	return result, nil
}

// importOne 在独立事务中写入一条订单及其商品信息。
func (a *orderApplicationService) importOne(ctx context.Context, ownedCookieIDs []string, defaultCookieID string, raw map[string]any, result *orderImportResult) error {
	orderID := firstImportString(raw, "order_id")
	if orderID == "" {
		return errors.New("缺少必需字段: order_id")
	}
	cookieID := firstImportString(raw, "cookie_id")
	if cookieID == "" {
		cookieID = defaultCookieID
	}
	if cookieID == "" {
		return errors.New("缺少必需字段: cookie_id")
	}
	if !containsCookieID(ownedCookieIDs, cookieID) {
		return errors.New("无权操作此账号的订单")
	}
	status := firstImportString(raw, "order_status", "status", "status_text")
	if status != "" {
		status = db.NormalizeOrderStatus(status)
		if !validEditableOrderStatus(status) {
			return errors.New("不支持的订单状态")
		}
	}
	amount, ok := normalizeOrderAmount(firstImportString(raw, "amount"))
	if !ok {
		return errors.New("订单金额必须是普通格式的非负有限数字")
	}
	if err := a.server.withTransaction(ctx, func(tx *sql.Tx) error {
		if err := a.server.Store.Orders.UpsertTx(ctx, tx, orderID, db.OrderUpsertOpts{
			CookieID: cookieID, ItemID: firstImportString(raw, "item_id"), BuyerID: firstImportString(raw, "buyer_id"),
			OrderStatus: status, SpecName: firstImportString(raw, "spec_name"), SpecValue: firstImportString(raw, "spec_value"),
			Quantity: firstImportString(raw, "quantity"), Amount: amount, ReceiverName: firstImportString(raw, "receiver_name"),
			ReceiverPhone: firstImportString(raw, "receiver_phone"), ReceiverAddr: firstImportString(raw, "receiver_address"),
			ReceiverCity: firstImportString(raw, "receiver_city"), ChatID: firstImportString(raw, "chat_id"),
		}); err != nil {
			return err
		}
		// itemID 是导入订单中关联的商品标识。
		itemID := firstImportString(raw, "item_id")
		if itemID != "" {
			if err := a.server.Store.Items.UpsertBasicTx(ctx, tx, &db.ItemInfoRow{
				CookieID: cookieID, ItemID: itemID, ItemTitle: firstImportString(raw, "item_title"),
				ItemPrice: firstImportString(raw, "item_price"), ItemDetail: firstImportString(raw, "item_detail", "item_description"),
			}); err != nil {
				return fmt.Errorf("补全商品信息失败: %w", err)
			}
		}
		return nil
	}); err != nil {
		return errors.New("订单导入事务失败: " + err.Error())
	}
	return nil
}

// errResult 生成导入失败的兼容结果行。
func errResult(raw map[string]any, message string) map[string]any {
	orderID := firstImportString(raw, "order_id")
	if orderID == "" {
		orderID = "unknown"
	}
	return map[string]any{"order_id": orderID, "success": false, "message": message}
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
func (a *orderApplicationService) ManualShip(ctx context.Context, request manualShipRequest) (manualShipResult, error) {
	ownedCookieIDs, err := a.server.Store.Cookies.ListOwnedIDs(ctx, request.UserID)
	if err != nil {
		return manualShipResult{}, err
	}
	result := manualShipResult{Results: make([]map[string]any, 0, len(request.OrderIDs))}
	for _, rawOrderID := range request.OrderIDs {
		orderID := strings.TrimSpace(rawOrderID)
		if orderID == "" {
			continue
		}
		order, getErr := a.server.Store.Orders.Get(ctx, orderID)
		if getErr != nil || order == nil {
			a.appendManualFailure(&result, orderID, "订单不存在")
			continue
		}
		if !containsCookieID(ownedCookieIDs, order.CookieID) {
			a.appendManualFailure(&result, orderID, "无权操作此订单")
			continue
		}
		if db.NormalizeOrderStatus(strings.TrimSpace(order.OrderStatus)) != "pending_ship" {
			a.appendManualFailure(&result, orderID, "仅待发货订单可以执行手动发货")
			continue
		}
		if request.ShipMode == "full_delivery" {
			a.manualFullDelivery(ctx, order, orderID, &result)
			continue
		}
		a.manualStatusShip(ctx, request.UserID, order, orderID, &result)
	}
	return result, nil
}

// appendManualFailure 追加一条手动发货失败记录并更新失败计数。
func (a *orderApplicationService) appendManualFailure(result *manualShipResult, orderID, message string) {
	result.FailedCount++
	result.Results = append(result.Results, map[string]any{"order_id": orderID, "success": false, "message": message})
}

// manualFullDelivery 执行完整自动化发货分支。
func (a *orderApplicationService) manualFullDelivery(ctx context.Context, order *db.Order, orderID string, result *manualShipResult) {
	if a.server.Manager == nil || a.server.automation == nil {
		a.appendManualFailure(result, orderID, "自动化中心未初始化")
		return
	}
	if _, running := a.server.Manager.GetInstance(order.CookieID); !running {
		a.appendManualFailure(result, orderID, "该账号未在线运行，无法执行完整发货")
		return
	}
	sent, err := a.server.automation.ManualFullDelivery(ctx, order)
	if err != nil {
		a.appendManualFailure(result, orderID, err.Error())
		a.server.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, "手动完整发货失败: "+err.Error())
		return
	}
	result.SuccessCount++
	result.Results = append(result.Results, map[string]any{"order_id": orderID, "success": true, "message": fmt.Sprintf("完整发货成功，已发送%d条卡券信息给买家", sent)})
	a.server.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, fmt.Sprintf("手动完整发货成功（订单 %s，已发送 %d 条）", orderID, sent))
}

// manualStatusShip 调用平台确认发货并把成功状态写入本地订单。
func (a *orderApplicationService) manualStatusShip(ctx context.Context, userID int64, order *db.Order, orderID string, result *manualShipResult) {
	if a.server.MTop == nil {
		a.appendManualFailure(result, orderID, "mtop 客户端未初始化")
		return
	}
	ok, ret, runtimeCookie, runtimeCookieChanged, err := a.server.consignWithCurrentCookie(ctx, order.CookieID, orderID, userID)
	if runtimeCookieChanged {
		a.server.updateRunningCookie(ctx, order.CookieID, runtimeCookie)
	}
	if err != nil && !ok {
		a.appendManualFailure(result, orderID, "确认发货异常: "+err.Error())
		a.server.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, "手动确认发货异常: "+err.Error())
		return
	}
	if !ok {
		message := "确认发货失败"
		if len(ret) > 0 {
			message += ": " + strings.Join(ret, "; ")
		}
		a.appendManualFailure(result, orderID, message)
		a.server.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, "手动确认发货失败: "+message)
		return
	}
	sysShip := true
	upsertErr := a.server.Store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{
		CookieID: order.CookieID, OrderStatus: "shipped", SystemShipped: &sysShip, ItemID: order.ItemID,
		BuyerID: order.BuyerID, ReceiverName: order.ReceiverName, ReceiverPhone: order.ReceiverPhone,
		ReceiverAddr: order.ReceiverAddr, ReceiverCity: order.ReceiverCity, ChatID: order.ChatID,
		SpecName: order.SpecName, SpecValue: order.SpecValue, Quantity: order.Quantity, Amount: order.Amount,
	})
	if upsertErr != nil && a.server.Logger != nil {
		a.server.Logger.Error("更新订单为系统已发货失败", "order_id", orderID, "err", upsertErr)
	}
	result.SuccessCount++
	message := "已成功修改闲鱼发货状态"
	warning := ""
	if upsertErr != nil {
		message += "；但登录凭证更新保存失败，请尽快重新登录（请勿重复确认发货）"
		warning = upsertErr.Error()
	}
	result.Results = append(result.Results, map[string]any{"order_id": orderID, "success": true, "message": message, "credential_warning": warning})
	a.server.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, fmt.Sprintf("手动确认发货成功（订单 %s）", orderID))
}

// RefreshSingle 刷新单个订单详情并写回本地订单。
func (a *orderApplicationService) RefreshSingle(ctx context.Context, userID int64, orderID string) (orderSingleRefreshResponse, error) {
	order, err := a.Get(ctx, userID, orderID)
	if err != nil {
		return orderSingleRefreshResponse{}, err
	}
	detailFetcher, ok := a.server.mtopClient().(orderDetailMTop)
	if !ok {
		return orderSingleRefreshResponse{}, errOrderDetailUnsupported
	}
	cookieID := order.CookieID
	credentialUnlock := a.server.Store.LockAccountCredentials(cookieID)
	credentialLocked := true
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
	}()
	latest, err := a.server.loadCookiePlatformDetail(ctx, cookieID)
	if err != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
		return orderSingleRefreshResponse{}, errOrderCredentialChanged
	}
	refreshCtx, cancel := context.WithTimeout(ctx, time.Minute)
	defer cancel()
	mtopCtx, cookieSession := withMTopCookieSnapshot(refreshCtx, latest)
	detail, callErr := detailFetcher.FetchOrderDetail(mtopCtx, latest.Value, orderID)
	if callErr == nil && detail == nil {
		callErr = errors.New("订单详情接口未返回结果")
	}
	runtimeCookie := ""
	runtimeCookieChanged := false
	value, valueChanged, handled, persistErr := a.server.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
	if persistErr != nil {
		callErr = errors.Join(callErr, fmt.Errorf("保存订单详情响应 Cookie Jar: %w", persistErr))
	} else if handled && valueChanged {
		runtimeCookie, runtimeCookieChanged = value, true
	} else if !handled && callErr == nil && detail.UpdatedCookies != "" && detail.UpdatedCookies != latest.Value {
		metadata := cookierefresh.MetadataWithoutSnapshot(latest.MetadataJSON)
		if saveErr := a.server.Store.Cookies.UpdateRenewalCookie(ctx, cookieID, detail.UpdatedCookies, metadata, time.Now().Unix()); saveErr == nil {
			runtimeCookie, runtimeCookieChanged = detail.UpdatedCookies, true
		}
	}
	credentialUnlock()
	credentialLocked = false
	if runtimeCookieChanged {
		a.server.updateRunningCookie(ctx, cookieID, runtimeCookie)
	}
	if callErr != nil {
		a.server.recoverExpiredMTOPSession(ctx, cookieID, callErr)
		return orderSingleRefreshResponse{}, callErr
	}
	status := db.NormalizeOrderStatus(detail.OrderStatus)
	if !validEditableOrderStatus(status) {
		status = db.NormalizeOrderStatus(order.OrderStatus)
	}
	if err := a.server.Store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{CookieID: cookieID, OrderStatus: status, SpecName: detail.SpecName, SpecValue: detail.SpecValue, Quantity: detail.Quantity, Amount: detail.Amount}); err != nil {
		return orderSingleRefreshResponse{}, err
	}
	return orderSingleRefreshResponse{Success: true, Message: "订单刷新完成", Order: orderRefreshDetailResponse{Quantity: detail.Quantity, SpecName: detail.SpecName, SpecValue: detail.SpecValue, OrderStatus: db.NormalizeOrderStatus(detail.OrderStatus), Amount: detail.Amount}}, nil
}
func (a *orderApplicationService) Refresh(ctx context.Context, userID int64, cookieID, status string) (orderRefreshResponse, error) {

	cookieIDs, err := a.server.Store.Cookies.ListOwnedIDs(ctx, userID)
	if err != nil {
		return orderRefreshResponse{}, err
	}
	if cookieID != "" {
		if !a.server.cookieOwnedByUser(ctx, userID, cookieID) {
			return orderRefreshResponse{}, db.ErrForbidden
		}
		cookieIDs = []string{cookieID}
	}

	discovered, listUpdated, softDeleted, failed := 0, 0, 0, 0
	results := []map[string]any{}
	newOrderIDs := make(map[string]struct{})
	sessionExpiredAccounts := make(map[string]struct{})
	if fetcher, ok := a.server.mtopClient().(mtop.SoldOrderFetcher); ok {
		for _, cid := range cookieIDs { // cid 是当前待刷新的账号 ID。
			credentialUnlock := a.server.Store.LockAccountCredentials(cid)
			latest, latestErr := a.server.loadCookiePlatformDetail(ctx, cid)
			if latestErr != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
				credentialUnlock()
				if latestErr == nil {
					latestErr = errors.New("账号凭证已变化")
				}
				failed++
				results = append(results, map[string]any{
					"cookie_id": cid, "stage": "discover", "success": false, "message": latestErr.Error(),
				})
				continue
			}
			// latest.Value 仅用于当前账号的凭证调用，不需要写回账号列表。
			mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
			accountDiscovered, accountUpdated, accountNewIDs, accountRemoteIDs, discoveryErr := a.server.discoverSoldOrders(mtopCtx, fetcher, cid, latest.Value)
			value, valueChanged, _, persistErr := a.server.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
			if persistErr != nil {
				discoveryErr = errors.Join(discoveryErr, fmt.Errorf("保存订单列表响应 Cookie Jar: %w", persistErr))
			}
			// value 通过运行时更新路径继续传递，不需要写回账号列表。
			// 账号筛选列表只保存 ID，不承载刷新后的凭证值。
			credentialUnlock()
			if persistErr == nil && valueChanged {
				a.server.updateRunningCookie(ctx, cid, value)
			}
			if mtop.IsSessionExpiredErr(discoveryErr) {
				sessionExpiredAccounts[cid] = struct{}{}
				a.server.recoverExpiredMTOPSession(ctx, cid, discoveryErr)
			}
			discovered += accountDiscovered
			listUpdated += accountUpdated
			for orderID := range accountNewIDs {
				newOrderIDs[orderID] = struct{}{}
			}
			result := map[string]any{
				"cookie_id": cid, "stage": "discover", "success": discoveryErr == nil,
				"discovered": accountDiscovered, "updated": accountUpdated,
			}
			if discoveryErr == nil {
				deleted, deleteErr := a.server.Store.Orders.SoftDeleteMissingForCookie(ctx, cid, accountRemoteIDs)
				if deleteErr != nil {
					discoveryErr = fmt.Errorf("标记缺失订单失败: %w", deleteErr)
					result["success"] = false
				} else {
					softDeleted += deleted
					result["soft_deleted"] = deleted
				}
			}
			if discoveryErr != nil {
				failed++
				result["error"] = discoveryErr.Error()
			}
			results = append(results, result)
		}
	} else {
		failed++
		results = append(results, map[string]any{"stage": "discover", "success": false, "message": "当前 MTop 客户端不支持订单列表发现"})
	}

	ordersByCookie := map[string][]refreshTarget{}
	for _, cid := range cookieIDs { // cid 是当前需要补充订单的账号 ID。
		if _, blocked := sessionExpiredAccounts[cid]; blocked {
			continue
		}
		for offset := 0; ; offset += 500 {
			rows, err := a.server.Store.Orders.ByCookiePage(ctx, cid, 500, offset)
			if err != nil {
				break
			}
			for _, row := range rows {
				currentStatus := db.NormalizeOrderStatus(row.OrderStatus)
				if status != "" && status != "all" && currentStatus != status {
					continue
				}
				// 稳定状态无需反复抓取；但历史订单若缺少实付金额，仍需补全详情。
				_, isNewOrder := newOrderIDs[row.OrderID]
				if !isNewOrder && isStableOrderStatus(currentStatus) && strings.TrimSpace(row.Amount) != "" {
					continue
				}
				ordersByCookie[cid] = append(ordersByCookie[cid], refreshTarget{OrderID: row.OrderID, CurrentStatus: currentStatus})
			}
			if len(rows) < 500 {
				break
			}
		}
	}

	total := 0
	for _, targets := range ordersByCookie {
		total += len(targets)
	}
	detailFetcher, detailSupported := a.server.mtopClient().(orderDetailMTop)
	if !detailSupported {
		message := "订单列表同步完成"
		if discovered > 0 {
			message = fmt.Sprintf("订单列表同步完成，发现并导入 %d 个新订单", discovered)
		}
		if total > 0 {
			message += fmt.Sprintf("；当前 Go MTOP 客户端不支持详情接口，已跳过 %d 个订单", total)
		}
		return orderRefreshResponse{
			PartialFailure: failed > 0, Message: message,
			Summary: orderRefreshSummary{
				Discovered: discovered, ListUpdated: listUpdated, SoftDeleted: softDeleted, DetailTotal: total,
				Total: total, Updated: 0, NoChange: 0, Failed: failed,
			},
			Results: results,
		}, nil
	}
	if total == 0 {
		return orderRefreshResponse{
			PartialFailure: failed > 0,
			Message:        fmt.Sprintf("订单列表同步完成，发现 %d 个新订单；没有需要补全详情的订单", discovered),
			Summary: orderRefreshSummary{
				Discovered: discovered, ListUpdated: listUpdated, SoftDeleted: softDeleted, DetailTotal: 0,
				Total: 0, Updated: 0, NoChange: 0, Failed: failed,
			},
			Results: results,
		}, nil
	}

	// 订单详情刷新阶段分别统计状态变化和无变化结果。
	updated, noChange := 0, 0
	for cid, targets := range ordersByCookie {
		accountSessionExpired := false
		for _, chunk := range chunkRefreshTargets(targets, refreshOrderChunkSize) {
			credentialUnlock := a.server.Store.LockAccountCredentials(cid)
			latest, latestErr := a.server.loadCookiePlatformDetail(ctx, cid)
			if latestErr != nil || latest == nil || latest.UserID != userID || !hasStoredCookieCredential(latest) {
				credentialUnlock()
				failed += len(chunk)
				results = append(results, map[string]any{"cookie_id": cid, "success": false, "message": "账号凭证已变化"})
				continue
			}
			detailCtx, cancel := context.WithTimeout(ctx, 3*time.Minute)
			mtopCtx, cookieSession := withMTopCookieSnapshot(detailCtx, latest)
			var sessionErr error
			for _, target := range chunk {
				detail, fetchErr := detailFetcher.FetchOrderDetail(mtopCtx, latest.Value, target.OrderID)
				if fetchErr != nil || detail == nil {
					failed++
					message := "订单详情接口未返回结果"
					if fetchErr != nil {
						message = fetchErr.Error()
					}
					results = append(results, map[string]any{
						"order_id": target.OrderID,
						"success":  false,
						"message":  message,
					})
					if mtop.IsSessionExpiredErr(fetchErr) {
						sessionErr = fetchErr
						break
					}
					continue
				}
				newStatus := db.NormalizeOrderStatus(detail.OrderStatus)
				if !validEditableOrderStatus(newStatus) {
					newStatus = target.CurrentStatus
				}
				err := a.server.Store.Orders.Upsert(ctx, target.OrderID, db.OrderUpsertOpts{
					CookieID:    cid,
					OrderStatus: newStatus,
					SpecName:    detail.SpecName,
					SpecValue:   detail.SpecValue,
					Quantity:    detail.Quantity,
					Amount:      detail.Amount,
				})
				if err != nil {
					failed++
					results = append(results, map[string]any{"order_id": target.OrderID, "success": false, "message": "更新数据库失败"})
					continue
				}
				changed := newStatus != "" && newStatus != target.CurrentStatus
				if changed {
					updated++
				} else {
					noChange++
				}
				results = append(results, map[string]any{
					"order_id":   target.OrderID,
					"success":    true,
					"old_status": target.CurrentStatus,
					"new_status": newStatus,
				})
			}
			cancel()
			value, valueChanged, _, persistErr := a.server.persistMTopCookieSessionLocked(ctx, latest, cookieSession)
			credentialUnlock()
			if persistErr != nil {
				failed++
				results = append(results, map[string]any{"cookie_id": cid, "stage": "persist_cookie", "success": false, "message": persistErr.Error()})
			} else if valueChanged {
				a.server.updateRunningCookie(ctx, cid, value)
			}
			if sessionErr != nil {
				a.server.recoverExpiredMTOPSession(ctx, cid, sessionErr)
				accountSessionExpired = true
				break
			}
		}
		if accountSessionExpired {
			continue
		}
	}
	return orderRefreshResponse{
		PartialFailure: failed > 0, Message: fmt.Sprintf("订单同步完成，发现 %d 个新订单", discovered),
		Summary: orderRefreshSummary{
			Discovered: discovered, ListUpdated: listUpdated, SoftDeleted: softDeleted, DetailTotal: total,
			Total: total, Updated: updated, NoChange: noChange, Failed: failed,
		},
		Results: results,
	}, nil

}
