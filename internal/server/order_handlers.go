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

// refreshOrderChunkSize 用于本次流程后续判断的refresh订单Chunk数量
const refreshOrderChunkSize = 100

// refreshTarget 用于本次流程后续判断的refreshTarget
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
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// result、err 用于本次流程后续判断的result、err
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
	// orderID 用于本次流程后续判断的订单ID
	orderID := chi.URLParam(r, "order_id")
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// result、err 用于本次流程后续判断的result、err
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

// chunkRefreshTargets 封装chunkRefreshTargets业务协调。
func chunkRefreshTargets(targets []refreshTarget, size int) [][]refreshTarget {
	if size <= 0 {
		size = refreshOrderChunkSize
	}
	// chunks 用于本次流程后续判断的chunks
	chunks := make([][]refreshTarget, 0, (len(targets)+size-1)/size)
	for // start 用于本次流程后续判断的开始
	start := 0; start < len(targets); start += size {
		// end 用于本次流程后续判断的结束
		end := start + size
		if end > len(targets) {
			end = len(targets)
		}
		chunks = append(chunks, targets[start:end])
	}
	return chunks
}

// missingRefreshTargetIDs 封装missingRefreshTargetIDs业务协调。
func missingRefreshTargetIDs(targets []refreshTarget, seen map[string]struct{}) []string {
	// missing 用于本次流程后续判断的missing
	missing := make([]string, 0)
	// target 表示当前遍历过程中的target
	for _, target := range targets {
		if // ok 用于本次流程后续判断的ok
		_, ok := seen[target.OrderID]; !ok {
			missing = append(missing, target.OrderID)
		}
	}
	return missing
}

func (s *Server) refreshSingleOrder(w http.ResponseWriter, r *http.Request) { // refreshSingleOrder 保持单订单刷新与批量刷新使用相同的详情 DTO。
	orderID := chi.URLParam(r, "order_id")
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// result、err 用于本次流程后续判断的result、err
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
	// orderID 用于本次流程后续判断的订单ID
	orderID := chi.URLParam(r, "order_id")
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if // err 用于本次流程后续判断的err
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
	// orderID 用于本次流程后续判断的订单ID
	orderID := chi.URLParam(r, "order_id")
	// req 保存具名订单更新请求，兼容旧客户端的状态别名和数值字段。
	var req orderUpdateRequestDTO
	if // err 用于本次流程后续判断的err
	err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	// status 保存兼容字段 order_status/status 合并后的状态值。
	status := req.OrderStatus
	if status == nil {
		status = req.Status
	}
	// stringPtrFromAny 用于本次流程后续判断的stringPtrFromAny
	stringPtrFromAny := func(value *any) *string {
		if value == nil {
			return nil
		}
		// v 用于本次流程后续判断的v
		v := stringFromAny(*value)
		return &v
	}
	// amount 保存兼容 JSON 数值转换后的订单金额。
	amount := stringPtrFromAny(req.Amount)
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	if // err 用于本次流程后续判断的err
	err := s.orders().Update(r.Context(), sess.UserID, orderID, orderUpdateRequest{
		OrderStatus: status, ItemID: req.ItemID, BuyerID: req.BuyerID, SpecName: req.SpecName,
		SpecValue: req.SpecValue, Quantity: stringPtrFromAny(req.Quantity), Amount: amount,
		ReceiverName: req.ReceiverName, ReceiverPhone: req.ReceiverPhone, ReceiverAddress: req.ReceiverAddress,
		ReceiverCity: req.ReceiverCity, ChatID: req.ChatID, SystemShipped: req.SystemShipped, ItemTitle: req.ItemTitle,
	}); err != nil {
		if // kind、classified 用于本次流程后续判断的kind、classified
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

// validOrderAmount 封装有效订单Amount业务协调。
func validOrderAmount(raw string) bool {
	// ok 用于本次流程后续判断的ok
	_, ok := normalizeOrderAmount(raw)
	return ok
}

// normalizeOrderAmount 封装normalize订单Amount业务协调。
func normalizeOrderAmount(raw string) (string, bool) {
	return orderapp.NormalizeOrderAmount(raw)
}

// manualShipOrders 封装manualShip订单列表业务协调。
func (s *Server) manualShipOrders(w http.ResponseWriter, r *http.Request) {
	// req 保存具名批量发货请求。
	var req manualShipRequestDTO
	if // err 用于本次流程后续判断的err
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
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// result、err 用于本次流程后续判断的result、err
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

// importOrders 封装import订单列表业务协调。
func (s *Server) importOrders(w http.ResponseWriter, r *http.Request) {
	// orders、err 用于本次流程后续判断的orders、err
	orders, err := parseImportedOrders(w, r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	// sess 用于本次流程后续判断的sess
	sess := auth.SessionFromContext(r.Context())
	// result、err 用于本次流程后续判断的result、err
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

// atoiDefault 封装atoiDefault业务协调。
func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	// n、err 用于本次流程后续判断的n、err
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}

// isStableOrderStatus 封装isStable订单状态业务协调。
func isStableOrderStatus(status string) bool {
	switch status {
	case "shipped", "completed", "cancelled":
		return true
	default:
		return false
	}
}
