package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/auth"
)

// refreshOrderChunkSize 保存refresh订单Chunk数量，供当前处理流程使用
const refreshOrderChunkSize = 100

// refreshTarget 保存refreshTarget，供当前处理流程使用
type refreshTarget struct {
	OrderID       string
	CurrentStatus string
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
	if errors.Is(err, orderapp.ErrForbidden) {
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
		if errors.Is(err, orderapp.ErrForbidden) {
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
	if errors.Is(err, orderapp.ErrNotFound) {
		writeErr(w, http.StatusNotFound, "订单不存在")
		return
	}
	if errors.Is(err, orderapp.ErrForbidden) {
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
		if errors.Is(err, orderapp.ErrForbidden) {
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
	// status 保存兼容字段 order_status/status 合并后的状态值。
	status := req.OrderStatus
	if status == nil {
		status = req.Status
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
	// amount 保存兼容 JSON 数值转换后的订单金额。
	amount := stringPtrFromAny(req.Amount)
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
		} else if errors.Is(err, orderapp.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权操作此订单")
		} else if errors.Is(err, orderapp.ErrNotFound) {
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
	return orderapp.NormalizeOrderAmount(raw)
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
