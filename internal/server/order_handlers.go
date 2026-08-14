package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

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
	orders := make([]orderDTO, 0, len(result.Rows))
	for _, o := range result.Rows {
		st := db.NormalizeOrderStatus(o.OrderStatus)
		orders = append(orders, orderDTO{
			OrderID:         o.OrderID,
			ItemID:          o.ItemID,
			ItemTitle:       o.ItemTitle,
			ItemImage:       itemImageFromDetail(o.ItemDetail),
			BuyerID:         o.BuyerID,
			SpecName:        o.SpecName,
			SpecValue:       o.SpecValue,
			Quantity:        o.Quantity,
			Amount:          o.Amount,
			OrderStatus:     st,
			Status:          st,
			CookieID:        o.CookieID,
			IsBargain:       o.IsBargain,
			SystemShipped:   o.SystemShipped,
			ReceiverName:    o.ReceiverName,
			ReceiverPhone:   o.ReceiverPhone,
			ReceiverAddress: o.ReceiverAddr,
			ReceiverCity:    o.ReceiverCity,
			CreatedAt:       o.CreatedAt,
			UpdatedAt:       o.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, orderListResponse{
		Success:    true,
		Data:       orders,
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
	o, err := s.orders().Get(r.Context(), sess.UserID, orderID)
	if err != nil {
		if errors.Is(err, db.ErrForbidden) {
			writeErr(w, http.StatusForbidden, "无权操作此订单")
		} else {
			writeErr(w, http.StatusNotFound, "订单不存在")
		}
		return
	}
	itemTitle, itemImage := "", ""
	if item, itemErr := s.Store.Items.Get(r.Context(), o.CookieID, o.ItemID); itemErr == nil {
		itemTitle = item.ItemTitle
		itemImage = itemImageFromDetail(item.ItemDetail)
	}
	writeJSON(w, http.StatusOK, orderDetailResponse{
		orderDTO: orderDTO{
			OrderID:         o.OrderID,
			ItemID:          o.ItemID,
			ItemTitle:       itemTitle,
			ItemImage:       itemImage,
			BuyerID:         o.BuyerID,
			SpecName:        o.SpecName,
			SpecValue:       o.SpecValue,
			Quantity:        o.Quantity,
			Amount:          o.Amount,
			OrderStatus:     db.NormalizeOrderStatus(o.OrderStatus),
			Status:          db.NormalizeOrderStatus(o.OrderStatus),
			CookieID:        o.CookieID,
			IsBargain:       o.IsBargain,
			SystemShipped:   o.SystemShipped,
			ReceiverName:    o.ReceiverName,
			ReceiverPhone:   o.ReceiverPhone,
			ReceiverAddress: o.ReceiverAddr,
			ReceiverCity:    o.ReceiverCity,
			CreatedAt:       o.CreatedAt,
			UpdatedAt:       o.UpdatedAt,
		}, Success: true,
		Data: orderDTO{
			OrderID:         o.OrderID,
			ItemID:          o.ItemID,
			ItemTitle:       itemTitle,
			ItemImage:       itemImage,
			BuyerID:         o.BuyerID,
			SpecName:        o.SpecName,
			SpecValue:       o.SpecValue,
			Quantity:        o.Quantity,
			Amount:          o.Amount,
			OrderStatus:     db.NormalizeOrderStatus(o.OrderStatus),
			Status:          db.NormalizeOrderStatus(o.OrderStatus),
			CookieID:        o.CookieID,
			IsBargain:       o.IsBargain,
			SystemShipped:   o.SystemShipped,
			ReceiverName:    o.ReceiverName,
			ReceiverPhone:   o.ReceiverPhone,
			ReceiverAddress: o.ReceiverAddr,
			ReceiverCity:    o.ReceiverCity,
			CreatedAt:       o.CreatedAt,
			UpdatedAt:       o.UpdatedAt,
		},
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

// Refresh 同步订单索引与订单详情，返回逐账号和逐订单的兼容结果。
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
		if strings.Contains(err.Error(), "不支持的订单状态") || strings.Contains(err.Error(), "订单金额") || strings.Contains(err.Error(), "商品标题不能为空") {
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
