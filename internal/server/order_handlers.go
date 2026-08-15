package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// refreshOrderChunkSize 保存refresh订单Chunk数量，供当前处理流程使用
const refreshOrderChunkSize = 100

// maxSoldOrderPages 保存maxSold订单Pages，供当前处理流程使用
const maxSoldOrderPages = 100

// refreshTarget 保存refreshTarget，供当前处理流程使用
type refreshTarget struct {
	OrderID       string
	CurrentStatus string
}

// orderDetailMTop 保存订单DetailMTop，供当前处理流程使用
type orderDetailMTop interface {
	FetchOrderDetail(ctx context.Context, cookiesStr, orderID string) (*mtop.OrderDetailResult, error)
}

// mountOrders 订单端点（真实实现）。
func (s *Server) mountOrdersReal(r chi.Router) {
	r.Get("/api/orders", s.listOrders)
	r.Get("/api/orders/{order_id}", s.getOrder)
	s.mountOrderRefreshJobRoutes(r, "/api")
	r.Post("/api/orders/{order_id}/refresh", s.refreshSingleOrder)
	r.Post("/api/orders/manual-ship", s.manualShipOrders)
	r.Post("/api/orders/import", s.importOrders)
	r.Delete("/api/orders/{order_id}", s.deleteOrder)
	r.Put("/api/orders/{order_id}", s.updateOrder)
}

// listOrders 分页查询当前用户订单。
func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// result、err 保存result、err，供当前处理流程使用
	result, err := s.orders().List(r.Context(), orderListQuery{
		UserID: sess.UserID, CookieID: r.URL.Query().Get("cookie_id"),
		Status: r.URL.Query().Get("status"), Search: r.URL.Query().Get("search"),
		Page: atoiDefault(r.URL.Query().Get("page"), 1), PageSize: atoiDefault(r.URL.Query().Get("page_size"), 20),
	})
	if errors.Is(err, db.ErrForbidden) {
		writeErr(w, http.StatusForbidden, "无权限操作该账号")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	writeJSON(w, http.StatusOK, orderListResponse{
		Success:    true,
		Data:       result.Orders,
		Total:      result.Total,
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
	})
}

// getOrder 订单详情。
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	// orderID 保存订单ID，供当前处理流程使用
	orderID := chi.URLParam(r, "order_id")
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// result、err 保存result、err，供当前处理流程使用
	result, err := s.orders().GetView(r.Context(), sess.UserID, orderID)
	if err != nil {
		if errors.Is(err, db.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权操作此订单")
		} else {
			writeErr(w, http.StatusNotFound, "订单不存在")
		}
		return
	}
	writeJSON(w, http.StatusOK, orderDetailResponse{
		orderDTO: result.Order, Success: true, Data: result.Order,
	})
}

// 订单发现阶段将远端订单索引与本地软删除状态保持一致。
func (s *Server) discoverSoldOrders(ctx context.Context, fetcher mtop.SoldOrderFetcher, cookieID, cookies string) (int, int, map[string]struct{}, map[string]struct{}, error) { // discoverSoldOrders 拉取远端订单列表并同步本地订单索引。
	discovered, updated := 0, 0
	// newOrderIDs 保存new订单IDs，供当前处理流程使用
	newOrderIDs := make(map[string]struct{})
	// remoteOrderIDs 保存remote订单IDs，供当前处理流程使用
	remoteOrderIDs := make(map[string]struct{})
	for // pageNumber 保存页码Number，供当前处理流程使用
	pageNumber := 1; pageNumber <= maxSoldOrderPages; pageNumber++ {
		// page、err 保存page、err，供当前处理流程使用
		page, err := fetcher.FetchSoldOrdersPage(ctx, cookies, pageNumber, 30)
		if err != nil {
			return discovered, updated, newOrderIDs, remoteOrderIDs, err
		}
		// remote 表示当前遍历过程中的remote
		for _, remote := range page.Items {
			remoteOrderIDs[remote.OrderID] = struct{}{}
			if // normalizedAmount、ok 保存normalizedAmount、ok，供当前处理流程使用
			normalizedAmount, ok := db.NormalizeOrderAmount(remote.Amount); ok {
				remote.Amount = normalizedAmount
			}
			// existing、getErr 保存existing、getErr，供当前处理流程使用
			existing, getErr := s.Store.Orders.Get(ctx, remote.OrderID)
			// isNew 保存isNew，供当前处理流程使用
			isNew := errors.Is(getErr, db.ErrNotFound)
			if getErr != nil && !isNew {
				return discovered, updated, newOrderIDs, remoteOrderIDs, fmt.Errorf("读取订单 %s 失败: %w", remote.OrderID, getErr)
			}
			// changed 保存changed，供当前处理流程使用
			changed := isNew || soldOrderChanged(existing, remote)
			// status 保存状态，供当前处理流程使用
			status := remote.OrderStatus
			if !isNew && status == "unknown" {
				status = ""
			}
			// isBargain 保存isBargain，供当前处理流程使用
			var isBargain *bool
			if remote.IsBargain {
				// value 保存值，供当前处理流程使用
				value := true
				isBargain = &value
			}
			if // err 保存err，供当前处理流程使用
			err := s.Store.Orders.Upsert(ctx, remote.OrderID, db.OrderUpsertOpts{
				ItemID: remote.ItemID, BuyerID: remote.BuyerID, CookieID: cookieID,
				OrderStatus: status, Quantity: remote.Quantity, Amount: remote.Amount,
				ReceiverName: remote.ReceiverName, ReceiverPhone: remote.ReceiverPhone,
				ReceiverAddr: remote.ReceiverAddr, ReceiverCity: remote.ReceiverCity,
				IsBargain: isBargain,
			}); err != nil {
				return discovered, updated, newOrderIDs, remoteOrderIDs, fmt.Errorf("保存订单 %s 失败: %w", remote.OrderID, err)
			}
			if isNew {
				discovered++
				newOrderIDs[remote.OrderID] = struct{}{}
			} else if changed {
				updated++
			}
		}
		if !page.NextPage || len(page.Items) == 0 {
			return discovered, updated, newOrderIDs, remoteOrderIDs, nil
		}
	}
	return discovered, updated, newOrderIDs, remoteOrderIDs, fmt.Errorf("订单列表超过 %d 页，已停止继续同步", maxSoldOrderPages)
}

// soldOrderChanged 负责sold订单Changed相关处理。
func soldOrderChanged(existing *db.Order, remote mtop.SoldOrder) bool {
	if existing == nil {
		return true
	}
	// statusChanged 保存状态Changed，供当前处理流程使用
	statusChanged := remote.OrderStatus != "" && remote.OrderStatus != "unknown" &&
		db.NormalizeOrderStatus(existing.OrderStatus) != remote.OrderStatus
	return statusChanged ||
		(remote.ItemID != "" && existing.ItemID != remote.ItemID) ||
		(remote.BuyerID != "" && existing.BuyerID != remote.BuyerID) ||
		(remote.Quantity != "" && existing.Quantity != remote.Quantity) ||
		(remote.Amount != "" && existing.Amount != remote.Amount) ||
		(remote.ReceiverName != "" && existing.ReceiverName != remote.ReceiverName) ||
		(remote.ReceiverPhone != "" && existing.ReceiverPhone != remote.ReceiverPhone) ||
		(remote.ReceiverAddr != "" && existing.ReceiverAddr != remote.ReceiverAddr) ||
		(remote.ReceiverCity != "" && existing.ReceiverCity != remote.ReceiverCity) ||
		(remote.IsBargain && existing.IsBargain == 0)
}

// chunkRefreshTargets 负责chunkRefreshTargets相关处理。
func chunkRefreshTargets(targets []refreshTarget, size int) [][]refreshTarget {
	if size <= 0 {
		size = refreshOrderChunkSize
	}
	// chunks 保存chunks，供当前处理流程使用
	chunks := make([][]refreshTarget, 0, (len(targets)+size-1)/size)
	for // start 保存开始，供当前处理流程使用
	start := 0; start < len(targets); start += size {
		// end 保存结束，供当前处理流程使用
		end := start + size
		if end > len(targets) {
			end = len(targets)
		}
		chunks = append(chunks, targets[start:end])
	}
	return chunks
}

// missingRefreshTargetIDs 负责missingRefreshTargetIDs相关处理。
func missingRefreshTargetIDs(targets []refreshTarget, seen map[string]struct{}) []string {
	// missing 保存missing，供当前处理流程使用
	missing := make([]string, 0)
	// target 表示当前遍历过程中的target
	for _, target := range targets {
		if // ok 保存ok，供当前处理流程使用
		_, ok := seen[target.OrderID]; !ok {
			missing = append(missing, target.OrderID)
		}
	}
	return missing
}

func (s *Server) refreshSingleOrder(w http.ResponseWriter, r *http.Request) { // refreshSingleOrder 保持单订单刷新与批量刷新使用相同的详情 DTO。
	orderID := chi.URLParam(r, "order_id")
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// result、err 保存result、err，供当前处理流程使用
	result, err := s.orders().RefreshSingle(r.Context(), sess.UserID, orderID)
	if errors.Is(err, db.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "订单不存在")
		return
	}
	if errors.Is(err, db.ErrForbidden) {
		writeErr(w, http.StatusForbidden, "无权操作此订单")
		return
	}
	if errors.Is(err, errOrderDetailUnsupported) {
		writeErr(w, http.StatusServiceUnavailable, err.Error())
		return
	}
	if errors.Is(err, errOrderCredentialChanged) {
		writeErr(w, http.StatusConflict, err.Error())
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if !result.Success {
		writeErr(w, http.StatusInternalServerError, "更新订单失败")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// deleteOrder 逻辑删除订单，保留订单历史，避免破坏自动化审计数据。
func (s *Server) deleteOrder(w http.ResponseWriter, r *http.Request) {
	// orderID 保存订单ID，供当前处理流程使用
	orderID := chi.URLParam(r, "order_id")
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if // err 保存err，供当前处理流程使用
	err := s.orders().Delete(r.Context(), sess.UserID, orderID); err != nil {
		if errors.Is(err, db.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权操作此订单")
		} else {
			writeErr(w, http.StatusNotFound, "订单不存在")
		}
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// updateOrder 更新订单（手动发货等）。
func (s *Server) updateOrder(w http.ResponseWriter, r *http.Request) {
	// orderID 保存订单ID，供当前处理流程使用
	orderID := chi.URLParam(r, "order_id")
	// req 保存req，供当前处理流程使用
	var req struct {
		OrderStatus     *string `json:"order_status"`
		Status          *string `json:"status"`
		ItemID          *string `json:"item_id"`
		BuyerID         *string `json:"buyer_id"`
		SpecName        *string `json:"spec_name"`
		SpecValue       *string `json:"spec_value"`
		Quantity        *any    `json:"quantity"`
		Amount          *any    `json:"amount"`
		ReceiverName    *string `json:"receiver_name"`
		ReceiverPhone   *string `json:"receiver_phone"`
		ReceiverAddress *string `json:"receiver_address"`
		ReceiverCity    *string `json:"receiver_city"`
		ChatID          *string `json:"chat_id"`
		SystemShipped   *bool   `json:"system_shipped"`
		ItemTitle       *string `json:"item_title"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// status 保存状态，供当前处理流程使用
	status := req.OrderStatus
	if status == nil {
		status = req.Status
	}
	if status != nil {
		// normalized 保存normalized，供当前处理流程使用
		normalized := db.NormalizeOrderStatus(strings.TrimSpace(*status))
		if !validEditableOrderStatus(normalized) {
			writeErr(w, http.StatusBadRequest, "不支持的订单状态")
			return
		}
		status = &normalized
	}
	// stringPtrFromAny 保存stringPtrFromAny，供当前处理流程使用
	stringPtrFromAny := func(value *any) *string {
		if value == nil {
			return nil
		}
		// v 保存v，供当前处理流程使用
		v := stringFromAny(*value)
		return &v
	}
	// amount 保存amount，供当前处理流程使用
	amount := stringPtrFromAny(req.Amount)
	if amount != nil {
		// normalized、ok 保存normalized、ok，供当前处理流程使用
		normalized, ok := normalizeOrderAmount(*amount)
		if !ok {
			writeErr(w, http.StatusBadRequest, "订单金额必须是普通格式的非负有限数字")
			return
		}
		amount = &normalized
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	if // err 保存err，供当前处理流程使用
	err := s.orders().Update(r.Context(), sess.UserID, orderID, orderUpdateRequest{
		OrderStatus: status, ItemID: req.ItemID, BuyerID: req.BuyerID, SpecName: req.SpecName,
		SpecValue: req.SpecValue, Quantity: stringPtrFromAny(req.Quantity), Amount: amount,
		ReceiverName: req.ReceiverName, ReceiverPhone: req.ReceiverPhone, ReceiverAddress: req.ReceiverAddress,
		ReceiverCity: req.ReceiverCity, ChatID: req.ChatID, SystemShipped: req.SystemShipped, ItemTitle: req.ItemTitle,
	}); err != nil {
		if // kind、classified 保存kind、classified，供当前处理流程使用
		kind, classified := orderErrorKindOf(err); classified && kind == orderErrorBadRequest {
			writeErr(w, http.StatusBadRequest, err.Error())
		} else if errors.Is(err, db.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权操作此订单")
		} else if errors.Is(err, db.ErrNotFound) {
			writeErr(w, http.StatusNotFound, "订单不存在")
		} else {
			writeErr(w, http.StatusInternalServerError, "更新失败")
		}
		return
	}
	writeJSON(w, http.StatusOK, operationResponse{Success: true})
}

// validOrderAmount 负责有效订单Amount相关处理。
func validOrderAmount(raw string) bool {
	// ok 保存ok，供当前处理流程使用
	_, ok := normalizeOrderAmount(raw)
	return ok
}

// normalizeOrderAmount 负责normalize订单Amount相关处理。
func normalizeOrderAmount(raw string) (string, bool) {
	return db.NormalizeOrderAmount(raw)
}

// validEditableOrderStatus 负责有效Editable订单状态相关处理。
func validEditableOrderStatus(status string) bool {
	switch status {
	case "processing", "pending_ship", "shipped", "completed", "cancelled", "refunding":
		return true
	default:
		return false
	}
}

// manualShipOrders 负责manualShip订单列表相关处理。
func (s *Server) manualShipOrders(w http.ResponseWriter, r *http.Request) {
	// req 保存req，供当前处理流程使用
	var req struct {
		OrderIDs []string `json:"order_ids"`
		ShipMode string   `json:"ship_mode"`
	}
	if // err 保存err，供当前处理流程使用
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	if len(req.OrderIDs) == 0 {
		writeErr(w, http.StatusBadRequest, "缺少订单ID")
		return
	}
	if req.ShipMode == "" {
		req.ShipMode = "status_only"
	}
	if req.ShipMode != "status_only" && req.ShipMode != "full_delivery" {
		writeErr(w, http.StatusBadRequest, "发货模式必须是 status_only 或 full_delivery")
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// result、err 保存result、err，供当前处理流程使用
	result, err := s.orders().ManualShip(r.Context(), manualShipRequest{
		UserID: sess.UserID, OrderIDs: req.OrderIDs, ShipMode: req.ShipMode,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询账号失败")
		return
	}
	writeJSON(w, http.StatusOK, manualShipResponse{
		PartialFailure: result.FailedCount > 0,
		Message:        fmt.Sprintf("手动发货完成: 成功%d个, 失败%d个", result.SuccessCount, result.FailedCount),
		// Results 保留逐订单兼容字段，便于旧客户端展示失败原因。
		SuccessCount: result.SuccessCount, FailedCount: result.FailedCount, Results: result.Results,
	})
}

// consignWithCurrentCookie 负责consignWithCurrent登录凭证相关处理。
func (s *Server) consignWithCurrentCookie(ctx context.Context, cookieID, orderID string, userID int64) (bool, []string, string, bool, error) {
	// credentialUnlock 保存credentialUnlock，供当前处理流程使用
	credentialUnlock := s.Store.LockAccountCredentials(cookieID)
	defer credentialUnlock()
	// detail、err 保存detail、err，供当前处理流程使用
	detail, err := s.loadCookiePlatformDetail(ctx, cookieID)
	if err != nil {
		return false, nil, "", false, err
	}
	if detail == nil || detail.UserID != userID {
		return false, nil, "", false, db.ErrForbidden
	}
	if !hasStoredCookieCredential(detail) {
		return false, nil, "", false, errors.New("账号 Cookie 为空")
	}
	// mtopCtx、cookieSession 保存mtopCtx、cookie会话，供当前处理流程使用
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, detail)
	// ok、ret、updatedCookies、callErr 保存ok、ret、updatedCookies、callErr，供当前处理流程使用
	ok, ret, updatedCookies, callErr := s.MTop.ConsignContext(mtopCtx, detail.Value, orderID)
	// value、valueChanged、handled、persistErr 保存value、valueChanged、handled、persistErr，供当前处理流程使用
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(ctx, detail, cookieSession)
	if persistErr != nil {
		persistErr = fmt.Errorf("保存发货响应 Cookie Jar: %w", persistErr)
		if callErr != nil {
			return ok, ret, "", false, errors.Join(callErr, persistErr)
		}
		return ok, ret, "", false, persistErr
	}
	if handled {
		// runtimeCookie 保存runtime登录凭证，供当前处理流程使用
		runtimeCookie := ""
		if valueChanged {
			runtimeCookie = value
		}
		return ok, ret, runtimeCookie, valueChanged, callErr
	}
	if callErr != nil {
		return false, ret, "", false, callErr
	}
	if updatedCookies == "" || updatedCookies == detail.Value {
		return ok, ret, "", false, nil
	}
	if // err 保存err，供当前处理流程使用
	err := s.Store.Cookies.UpdateValueOwned(ctx, cookieID, updatedCookies, userID); err != nil {
		return ok, ret, "", false, fmt.Errorf("保存发货响应 Cookie: %w", err)
	}
	return ok, ret, updatedCookies, true, nil
}

// importOrders 负责import订单列表相关处理。
func (s *Server) importOrders(w http.ResponseWriter, r *http.Request) {
	// orders、err 保存orders、err，供当前处理流程使用
	orders, err := parseImportedOrders(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// sess 保存sess，供当前处理流程使用
	sess := auth.SessionFromContext(r.Context())
	// result、err 保存result、err，供当前处理流程使用
	result, err := s.orders().Import(r.Context(), sess.UserID, orders)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询账号失败")
		return
	}
	writeJSON(w, http.StatusOK, importOrdersResponse{
		PartialFailure: result.FailedCount > 0,
		Message:        fmt.Sprintf("导入完成: 成功%d个, 失败%d个", result.SuccessCount, result.FailedCount),
		// Total 和 Results 共同保留导入批次的统计及逐单结果。
		// 兼容客户端继续使用 partial_failure 判断批次是否需要复核。
		Total: result.Total, SuccessCount: result.SuccessCount, FailedCount: result.FailedCount, Results: result.Results,
	})
}

// atoiDefault 负责atoiDefault相关处理。
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	// n、err 保存n、err，供当前处理流程使用
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// isStableOrderStatus 负责isStable订单状态相关处理。
func isStableOrderStatus(status string) bool {
	switch status {
	case "shipped", "completed", "cancelled":
		return true
	default:
		return false
	}
}

// notifyDelivery 在 Notifier 已注入时发送发货结果通知，未注入则跳过。
func (s *Server) notifyDelivery(cookieID, buyerID, itemID, chatID, message string) {
	if s.notifier == nil {
		return
	}
	s.notifier.NotifyDelivery(cookieID, "", buyerID, itemID, message, chatID)
}

// containsCookieID 判断账号 ID 列表中是否包含目标账号，不触碰 Cookie 明文。
func containsCookieID(cookieIDs []string, target string) bool {
	// cookieID 是当前遍历到的账号 ID。
	for _, cookieID := range cookieIDs {
		if cookieID == target {
			return true
		}
	}
	return false
}
