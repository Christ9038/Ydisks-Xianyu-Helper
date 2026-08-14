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

const refreshOrderChunkSize = 100
const maxSoldOrderPages = 100

type refreshTarget struct {
	OrderID       string
	CurrentStatus string
}

type orderDetailMTop interface {
	FetchOrderDetail(ctx context.Context, cookiesStr, orderID string) (*mtop.OrderDetailResult, error)
}

// mountOrders 订单端点（真实实现）。
func (s *Server) mountOrdersReal(r chi.Router) {
	r.Get("/api/orders", s.listOrders)
	r.Get("/api/orders/{order_id}", s.getOrder)
	r.Post("/api/orders/refresh", s.refreshOrders)
	r.Post("/api/orders/{order_id}/refresh", s.refreshSingleOrder)
	r.Post("/api/orders/manual-ship", s.manualShipOrders)
	r.Post("/api/orders/import", s.importOrders)
	r.Delete("/api/orders/{order_id}", s.deleteOrder)
	r.Put("/api/orders/{order_id}", s.updateOrder)
}

// listOrders 分页查询当前用户订单。
func (s *Server) listOrders(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
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
	orderID := chi.URLParam(r, "order_id")
	sess := auth.SessionFromContext(r.Context())
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

// refreshOrders 将 HTTP 请求转换为订单刷新应用服务调用并编码兼容响应。
func (s *Server) refreshOrders(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	result, err := s.orders().Refresh(r.Context(), sess.UserID, r.FormValue("cookie_id"), r.FormValue("status"))
	if errors.Is(err, db.ErrForbidden) {
		writeErr(w, http.StatusForbidden, "Cookie不存在或无权访问")
		return
	}
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询账号失败")
		return
	}
	writeJSON(w, http.StatusOK, result)
}

// 订单发现阶段将远端订单索引与本地软删除状态保持一致。
func (s *Server) discoverSoldOrders(ctx context.Context, fetcher mtop.SoldOrderFetcher, cookieID, cookies string) (int, int, map[string]struct{}, map[string]struct{}, error) { // discoverSoldOrders 拉取远端订单列表并同步本地订单索引。
	discovered, updated := 0, 0
	newOrderIDs := make(map[string]struct{})
	remoteOrderIDs := make(map[string]struct{})
	for pageNumber := 1; pageNumber <= maxSoldOrderPages; pageNumber++ {
		page, err := fetcher.FetchSoldOrdersPage(ctx, cookies, pageNumber, 30)
		if err != nil {
			return discovered, updated, newOrderIDs, remoteOrderIDs, err
		}
		for _, remote := range page.Items {
			remoteOrderIDs[remote.OrderID] = struct{}{}
			if normalizedAmount, ok := db.NormalizeOrderAmount(remote.Amount); ok {
				remote.Amount = normalizedAmount
			}
			existing, getErr := s.Store.Orders.Get(ctx, remote.OrderID)
			isNew := errors.Is(getErr, db.ErrNotFound)
			if getErr != nil && !isNew {
				return discovered, updated, newOrderIDs, remoteOrderIDs, fmt.Errorf("读取订单 %s 失败: %w", remote.OrderID, getErr)
			}
			changed := isNew || soldOrderChanged(existing, remote)
			status := remote.OrderStatus
			if !isNew && status == "unknown" {
				status = ""
			}
			var isBargain *bool
			if remote.IsBargain {
				value := true
				isBargain = &value
			}
			if err := s.Store.Orders.Upsert(ctx, remote.OrderID, db.OrderUpsertOpts{
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

func soldOrderChanged(existing *db.Order, remote mtop.SoldOrder) bool {
	if existing == nil {
		return true
	}
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

func chunkRefreshTargets(targets []refreshTarget, size int) [][]refreshTarget {
	if size <= 0 {
		size = refreshOrderChunkSize
	}
	chunks := make([][]refreshTarget, 0, (len(targets)+size-1)/size)
	for start := 0; start < len(targets); start += size {
		end := start + size
		if end > len(targets) {
			end = len(targets)
		}
		chunks = append(chunks, targets[start:end])
	}
	return chunks
}

func missingRefreshTargetIDs(targets []refreshTarget, seen map[string]struct{}) []string {
	missing := make([]string, 0)
	for _, target := range targets {
		if _, ok := seen[target.OrderID]; !ok {
			missing = append(missing, target.OrderID)
		}
	}
	return missing
}

func (s *Server) refreshSingleOrder(w http.ResponseWriter, r *http.Request) { // refreshSingleOrder 保持单订单刷新与批量刷新使用相同的详情 DTO。
	orderID := chi.URLParam(r, "order_id")
	sess := auth.SessionFromContext(r.Context())
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
	orderID := chi.URLParam(r, "order_id")
	sess := auth.SessionFromContext(r.Context())
	if err := s.orders().Delete(r.Context(), sess.UserID, orderID); err != nil {
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
	orderID := chi.URLParam(r, "order_id")
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
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	status := req.OrderStatus
	if status == nil {
		status = req.Status
	}
	if status != nil {
		normalized := db.NormalizeOrderStatus(strings.TrimSpace(*status))
		if !validEditableOrderStatus(normalized) {
			writeErr(w, http.StatusBadRequest, "不支持的订单状态")
			return
		}
		status = &normalized
	}
	stringPtrFromAny := func(value *any) *string {
		if value == nil {
			return nil
		}
		v := stringFromAny(*value)
		return &v
	}
	amount := stringPtrFromAny(req.Amount)
	if amount != nil {
		normalized, ok := normalizeOrderAmount(*amount)
		if !ok {
			writeErr(w, http.StatusBadRequest, "订单金额必须是普通格式的非负有限数字")
			return
		}
		amount = &normalized
	}
	sess := auth.SessionFromContext(r.Context())
	if err := s.orders().Update(r.Context(), sess.UserID, orderID, orderUpdateRequest{
		OrderStatus: status, ItemID: req.ItemID, BuyerID: req.BuyerID, SpecName: req.SpecName,
		SpecValue: req.SpecValue, Quantity: stringPtrFromAny(req.Quantity), Amount: amount,
		ReceiverName: req.ReceiverName, ReceiverPhone: req.ReceiverPhone, ReceiverAddress: req.ReceiverAddress,
		ReceiverCity: req.ReceiverCity, ChatID: req.ChatID, SystemShipped: req.SystemShipped, ItemTitle: req.ItemTitle,
	}); err != nil {
		if kind, classified := orderErrorKindOf(err); classified && kind == orderErrorBadRequest {
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

func validOrderAmount(raw string) bool {
	_, ok := normalizeOrderAmount(raw)
	return ok
}

func normalizeOrderAmount(raw string) (string, bool) {
	return db.NormalizeOrderAmount(raw)
}

func validEditableOrderStatus(status string) bool {
	switch status {
	case "processing", "pending_ship", "shipped", "completed", "cancelled", "refunding":
		return true
	default:
		return false
	}
}

func (s *Server) manualShipOrders(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrderIDs []string `json:"order_ids"`
		ShipMode string   `json:"ship_mode"`
	}
	if err := decodeJSON(r, &req); err != nil {
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
	sess := auth.SessionFromContext(r.Context())
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

func (s *Server) consignWithCurrentCookie(ctx context.Context, cookieID, orderID string, userID int64) (bool, []string, string, bool, error) {
	credentialUnlock := s.Store.LockAccountCredentials(cookieID)
	defer credentialUnlock()
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
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, detail)
	ok, ret, updatedCookies, callErr := s.MTop.ConsignContext(mtopCtx, detail.Value, orderID)
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(ctx, detail, cookieSession)
	if persistErr != nil {
		persistErr = fmt.Errorf("保存发货响应 Cookie Jar: %w", persistErr)
		if callErr != nil {
			return ok, ret, "", false, errors.Join(callErr, persistErr)
		}
		return ok, ret, "", false, persistErr
	}
	if handled {
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
	if err := s.Store.Cookies.UpdateValueOwned(ctx, cookieID, updatedCookies, userID); err != nil {
		return ok, ret, "", false, fmt.Errorf("保存发货响应 Cookie: %w", err)
	}
	return ok, ret, updatedCookies, true, nil
}

func (s *Server) importOrders(w http.ResponseWriter, r *http.Request) {
	orders, err := parseImportedOrders(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	sess := auth.SessionFromContext(r.Context())
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

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

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
