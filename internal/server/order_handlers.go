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
	"xianyu-go/internal/xianyu/cookierefresh"
	"xianyu-go/internal/xianyu/mtop"
)

const refreshOrderChunkSize = 100
const maxSoldOrderPages = 100

type refreshTarget struct {
	OrderID       string
	CurrentStatus string
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
	page := atoiDefault(r.URL.Query().Get("page"), 1)
	pageSize := atoiDefault(r.URL.Query().Get("page_size"), 20)
	cookieID := r.URL.Query().Get("cookie_id")
	status := r.URL.Query().Get("status")
	search := r.URL.Query().Get("search")
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	if cookieID != "" {
		if _, ok := s.cookieForUser(r, sess.UserID, cookieID); !ok {
			writeErr(w, http.StatusForbidden, "无权限操作该账号")
			return
		}
	}
	if pageSize > 200 {
		pageSize = 200
	}
	offset := (page - 1) * pageSize
	rows, total, err := s.Store.Orders.ListForUser(r.Context(), db.OrderListFilter{
		UserID: sess.UserID, CookieID: cookieID, Status: status, Search: search, Limit: pageSize, Offset: offset,
	})
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	orders := make([]map[string]any, 0, len(rows))
	for _, o := range rows {
		st := db.NormalizeOrderStatus(o.OrderStatus)
		orders = append(orders, map[string]any{
			"order_id":         o.OrderID,
			"item_id":          o.ItemID,
			"item_title":       o.ItemTitle,
			"item_image":       itemImageFromDetail(o.ItemDetail),
			"buyer_id":         o.BuyerID,
			"spec_name":        o.SpecName,
			"spec_value":       o.SpecValue,
			"quantity":         o.Quantity,
			"amount":           o.Amount,
			"order_status":     st,
			"status":           st,
			"cookie_id":        o.CookieID,
			"is_bargain":       o.IsBargain,
			"system_shipped":   o.SystemShipped,
			"receiver_name":    o.ReceiverName,
			"receiver_phone":   o.ReceiverPhone,
			"receiver_address": o.ReceiverAddr,
			"receiver_city":    o.ReceiverCity,
			"created_at":       o.CreatedAt,
			"updated_at":       o.UpdatedAt,
		})
	}
	totalPages := (total + pageSize - 1) / pageSize
	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"data":        orders,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// getOrder 订单详情。
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	o, ok := s.requireOrderOwner(w, r, orderID)
	if !ok {
		return
	}
	itemTitle, itemImage := "", ""
	if item, itemErr := s.Store.Items.Get(r.Context(), o.CookieID, o.ItemID); itemErr == nil {
		itemTitle = item.ItemTitle
		itemImage = itemImageFromDetail(item.ItemDetail)
	}
	payload := map[string]any{
		"order_id":         o.OrderID,
		"item_id":          o.ItemID,
		"item_title":       itemTitle,
		"item_image":       itemImage,
		"buyer_id":         o.BuyerID,
		"spec_name":        o.SpecName,
		"spec_value":       o.SpecValue,
		"quantity":         o.Quantity,
		"amount":           o.Amount,
		"order_status":     db.NormalizeOrderStatus(o.OrderStatus),
		"status":           db.NormalizeOrderStatus(o.OrderStatus),
		"cookie_id":        o.CookieID,
		"is_bargain":       o.IsBargain,
		"system_shipped":   o.SystemShipped,
		"receiver_name":    o.ReceiverName,
		"receiver_phone":   o.ReceiverPhone,
		"receiver_address": o.ReceiverAddr,
		"receiver_city":    o.ReceiverCity,
		"created_at":       o.CreatedAt,
		"updated_at":       o.UpdatedAt,
	}
	payload["success"] = true
	payload["data"] = map[string]any{
		"order_id":         o.OrderID,
		"item_id":          o.ItemID,
		"item_title":       itemTitle,
		"item_image":       itemImage,
		"buyer_id":         o.BuyerID,
		"spec_name":        o.SpecName,
		"spec_value":       o.SpecValue,
		"quantity":         o.Quantity,
		"amount":           o.Amount,
		"order_status":     db.NormalizeOrderStatus(o.OrderStatus),
		"status":           db.NormalizeOrderStatus(o.OrderStatus),
		"cookie_id":        o.CookieID,
		"is_bargain":       o.IsBargain,
		"system_shipped":   o.SystemShipped,
		"receiver_name":    o.ReceiverName,
		"receiver_phone":   o.ReceiverPhone,
		"receiver_address": o.ReceiverAddr,
		"receiver_city":    o.ReceiverCity,
		"created_at":       o.CreatedAt,
		"updated_at":       o.UpdatedAt,
	}
	writeJSON(w, http.StatusOK, payload)
}

func (s *Server) refreshOrders(w http.ResponseWriter, r *http.Request) {
	sess := auth.SessionFromContext(r.Context())
	all, err := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询账号失败")
		return
	}
	cookieID := r.FormValue("cookie_id")
	status := r.FormValue("status")
	if cookieID != "" {
		value, ok := all[cookieID]
		if !ok {
			writeErr(w, http.StatusForbidden, "Cookie不存在或无权访问")
			return
		}
		all = map[string]string{cookieID: value}
	}

	discovered, listUpdated, failed := 0, 0, 0
	results := []map[string]any{}
	newOrderIDs := make(map[string]struct{})
	if fetcher, ok := s.mtopClient().(mtop.SoldOrderFetcher); ok {
		for cid := range all {
			credentialUnlock := s.Store.LockAccountCredentials(cid)
			latest, latestErr := s.Store.Cookies.GetDetails(r.Context(), cid)
			if latestErr != nil || latest == nil || latest.UserID != sess.UserID || !hasStoredCookieCredential(latest) {
				credentialUnlock()
				if latestErr == nil {
					latestErr = errors.New("账号凭证已变化")
				}
				failed++
				results = append(results, map[string]any{
					"cookie_id": cid, "stage": "discover", "success": false, "error": latestErr.Error(),
				})
				continue
			}
			all[cid] = latest.Value
			mtopCtx, cookieSession := withMTopCookieSnapshot(r.Context(), latest)
			accountDiscovered, accountUpdated, accountNewIDs, discoveryErr := s.discoverSoldOrders(mtopCtx, fetcher, cid, latest.Value)
			value, valueChanged, _, persistErr := s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
			if persistErr != nil {
				discoveryErr = errors.Join(discoveryErr, fmt.Errorf("保存订单列表响应 Cookie Jar: %w", persistErr))
			} else if valueChanged {
				all[cid] = value
			}
			credentialUnlock()
			if persistErr == nil && valueChanged {
				s.updateRunningCookie(r.Context(), cid, value)
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
			if discoveryErr != nil {
				failed++
				result["error"] = discoveryErr.Error()
			}
			results = append(results, result)
		}
	} else {
		failed++
		results = append(results, map[string]any{"stage": "discover", "success": false, "error": "当前 MTop 客户端不支持订单列表发现"})
	}

	ordersByCookie := map[string][]refreshTarget{}
	for cid := range all {
		for offset := 0; ; offset += 500 {
			rows, err := s.Store.Orders.ByCookiePage(r.Context(), cid, 500, offset)
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
	if s.Browser == nil {
		message := "订单列表同步完成"
		if discovered > 0 {
			message = fmt.Sprintf("订单列表同步完成，发现并导入 %d 个新订单", discovered)
		}
		if total > 0 {
			message += fmt.Sprintf("；浏览器自动化未启用，已跳过 %d 个订单的详情补全", total)
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"success": failed == 0,
			"message": message,
			"summary": map[string]int{
				"discovered": discovered, "list_updated": listUpdated, "detail_total": total,
				"total": total, "updated": 0, "no_change": 0, "failed": failed,
			},
			"results": results,
		})
		return
	}
	if total == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": failed == 0,
			"message": fmt.Sprintf("订单列表同步完成，发现 %d 个新订单；没有需要补全详情的订单", discovered),
			"summary": map[string]int{
				"discovered": discovered, "list_updated": listUpdated, "detail_total": 0,
				"total": 0, "updated": 0, "no_change": 0, "failed": failed,
			},
			"results": results,
		})
		return
	}

	updated, noChange := 0, 0
	for cid, targets := range ordersByCookie {
		for _, chunk := range chunkRefreshTargets(targets, refreshOrderChunkSize) {
			orderIDs := make([]string, 0, len(chunk))
			currentStatus := make(map[string]string, len(chunk))
			for _, target := range chunk {
				orderIDs = append(orderIDs, target.OrderID)
				currentStatus[target.OrderID] = target.CurrentStatus
			}
			credentialUnlock := s.Store.LockAccountCredentials(cid)
			latest, latestErr := s.Store.Cookies.GetDetails(r.Context(), cid)
			if latestErr != nil || latest == nil || latest.UserID != sess.UserID || !hasStoredCookieCredential(latest) {
				credentialUnlock()
				failed += len(chunk)
				results = append(results, map[string]any{"cookie_id": cid, "success": false, "error": "账号凭证已变化"})
				continue
			}
			ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
			mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
			batch, err := s.Browser.BatchRefreshOrders(mtopCtx, orderIDs, cid, latest.Value)
			cancel()
			value, valueChanged, _, persistErr := s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
			if persistErr != nil {
				err = errors.Join(err, fmt.Errorf("保存订单详情响应 Cookie Jar: %w", persistErr))
			}
			credentialUnlock()
			if persistErr == nil && valueChanged {
				s.updateRunningCookie(r.Context(), cid, value)
			}
			if err != nil {
				failed += len(chunk)
				results = append(results, map[string]any{"cookie_id": cid, "success": false, "error": err.Error()})
				continue
			}
			rawOrders, _ := batch["orders"].([]map[string]any)
			seen := make(map[string]struct{}, len(rawOrders))
			for _, raw := range rawOrders {
				orderID := stringFromAny(raw["order_id"])
				if _, expected := currentStatus[orderID]; !expected {
					continue
				}
				seen[orderID] = struct{}{}
				if raw["success"] == false {
					failed++
					results = append(results, map[string]any{
						"order_id": orderID,
						"success":  false,
						"error":    stringFromAny(raw["error"]),
					})
					continue
				}
				newStatus := db.NormalizeOrderStatus(stringFromAny(raw["order_status"]))
				if !validEditableOrderStatus(newStatus) {
					newStatus = currentStatus[orderID]
				}
				err := s.Store.Orders.Upsert(r.Context(), orderID, db.OrderUpsertOpts{
					CookieID:    cid,
					OrderStatus: newStatus,
					SpecName:    stringFromAny(raw["spec_name"]),
					SpecValue:   stringFromAny(raw["spec_value"]),
					Quantity:    stringFromAny(raw["quantity"]),
					Amount:      stringFromAny(raw["amount"]),
				})
				if err != nil {
					failed++
					results = append(results, map[string]any{"order_id": orderID, "success": false, "error": "更新数据库失败"})
					continue
				}
				changed := newStatus != "" && newStatus != currentStatus[orderID]
				if changed {
					updated++
				} else {
					noChange++
				}
				results = append(results, map[string]any{
					"order_id":   orderID,
					"success":    true,
					"old_status": currentStatus[orderID],
					"new_status": newStatus,
				})
			}
			for _, orderID := range missingRefreshTargetIDs(chunk, seen) {
				failed++
				results = append(results, map[string]any{"order_id": orderID, "success": false, "error": "浏览器未返回该订单的刷新结果"})
			}
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": failed == 0,
		"message": fmt.Sprintf("订单同步完成，发现 %d 个新订单", discovered),
		"summary": map[string]int{
			"discovered": discovered, "list_updated": listUpdated, "detail_total": total,
			"total": total, "updated": updated, "no_change": noChange, "failed": failed,
		},
		"results": results,
	})
}

func (s *Server) discoverSoldOrders(ctx context.Context, fetcher mtop.SoldOrderFetcher, cookieID, cookies string) (int, int, map[string]struct{}, error) {
	discovered, updated := 0, 0
	newOrderIDs := make(map[string]struct{})
	for pageNumber := 1; pageNumber <= maxSoldOrderPages; pageNumber++ {
		page, err := fetcher.FetchSoldOrdersPage(ctx, cookies, pageNumber, 30)
		if err != nil {
			return discovered, updated, newOrderIDs, err
		}
		pageChanged := false
		for _, remote := range page.Items {
			if normalizedAmount, ok := db.NormalizeOrderAmount(remote.Amount); ok {
				remote.Amount = normalizedAmount
			}
			existing, getErr := s.Store.Orders.Get(ctx, remote.OrderID)
			isNew := errors.Is(getErr, db.ErrNotFound)
			if getErr != nil && !isNew {
				return discovered, updated, newOrderIDs, fmt.Errorf("读取订单 %s 失败: %w", remote.OrderID, getErr)
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
				return discovered, updated, newOrderIDs, fmt.Errorf("保存订单 %s 失败: %w", remote.OrderID, err)
			}
			if isNew {
				discovered++
				newOrderIDs[remote.OrderID] = struct{}{}
			} else if changed {
				updated++
			}
			pageChanged = pageChanged || changed
		}
		if !page.NextPage || len(page.Items) == 0 {
			return discovered, updated, newOrderIDs, nil
		}
		// 卖家列表按新到旧排列。一整页都已存在且字段未变化时，更早页也无需重复扫描。
		if !pageChanged {
			return discovered, updated, newOrderIDs, nil
		}
	}
	return discovered, updated, newOrderIDs, fmt.Errorf("订单列表超过 %d 页，已停止继续同步", maxSoldOrderPages)
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

func (s *Server) refreshSingleOrder(w http.ResponseWriter, r *http.Request) {
	if s.Browser == nil {
		writeErr(w, http.StatusServiceUnavailable, "浏览器自动化未启用，无法刷新订单")
		return
	}
	orderID := chi.URLParam(r, "order_id")
	order, ok := s.requireOrderOwner(w, r, orderID)
	if !ok {
		return
	}
	cookieID := order.CookieID
	credentialUnlock := s.Store.LockAccountCredentials(cookieID)
	credentialLocked := true
	defer func() {
		if credentialLocked {
			credentialUnlock()
		}
	}()
	sess := auth.SessionFromContext(r.Context())
	latest, err := s.Store.Cookies.GetDetails(r.Context(), cookieID)
	if err != nil || latest == nil || latest.UserID != sess.UserID || !hasStoredCookieCredential(latest) {
		writeErr(w, http.StatusConflict, "账号凭证已变化，请重试")
		return
	}
	cookieValue := latest.Value
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	mtopCtx, cookieSession := withMTopCookieSnapshot(ctx, latest)
	detail, callErr := s.Browser.FetchOrderDetail(mtopCtx, orderID, cookieID, cookieValue, s.Store.Items.IsMultiSpec(mtopCtx, cookieID, order.ItemID))
	if callErr == nil && detail == nil {
		callErr = errors.New("订单详情接口未返回结果")
	}
	runtimeCookie := ""
	runtimeCookieChanged := false
	value, valueChanged, handled, persistErr := s.persistMTopCookieSessionLocked(r.Context(), latest, cookieSession)
	if !handled && detail != nil && detail.CookieSnapshotComplete {
		// 没有历史 snapshot 时不会创建 CookieSession；浏览器 fallback 返回的
		// 完整 Jar 仍是权威结果，必须原子落库（包括空 Jar/空 flat）。
		value = detail.UpdatedCookies
		valueChanged = value != latest.Value
		handled = true
		metadata := cookierefresh.MetadataWithSnapshot(latest.MetadataJSON, detail.CookieSnapshot)
		persistErr = s.Store.Cookies.UpdateRenewalCookie(r.Context(), cookieID, value, metadata, time.Now().Unix())
	}
	if persistErr != nil {
		s.Logger.Error("保存订单详情响应 Cookie Jar 失败", "cookie_id", cookieID, "err", persistErr)
		callErr = errors.Join(callErr, fmt.Errorf("保存订单详情响应 Cookie Jar: %w", persistErr))
	} else if handled {
		if valueChanged {
			runtimeCookie = value
			runtimeCookieChanged = true
		}
	} else if callErr == nil && detail.UpdatedCookies != "" && detail.UpdatedCookies != cookieValue {
		if saveErr := s.Store.Cookies.UpdateValueExisting(r.Context(), cookieID, detail.UpdatedCookies); saveErr != nil {
			s.Logger.Error("保存订单详情刷新后的 cookie 失败", "cookie_id", cookieID, "err", saveErr)
		} else {
			runtimeCookie = detail.UpdatedCookies
			runtimeCookieChanged = true
		}
	}
	credentialUnlock()
	credentialLocked = false
	if runtimeCookieChanged && s.Manager != nil {
		if account, running := s.Manager.GetInstance(cookieID); running {
			account.UpdateCookie(runtimeCookie)
		}
	}
	if callErr != nil {
		writeErr(w, http.StatusBadGateway, callErr.Error())
		return
	}
	status := db.NormalizeOrderStatus(detail.OrderStatus)
	if !validEditableOrderStatus(status) {
		status = db.NormalizeOrderStatus(order.OrderStatus)
	}
	if err := s.Store.Orders.Upsert(r.Context(), orderID, db.OrderUpsertOpts{
		CookieID:    cookieID,
		OrderStatus: status,
		SpecName:    detail.SpecName,
		SpecValue:   detail.SpecValue,
		Quantity:    detail.Quantity,
		Amount:      detail.Amount,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新订单失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true, "message": "订单刷新完成", "order": detail})
}

// deleteOrder 删除订单。
func (s *Server) deleteOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	if _, ok := s.requireOrderOwner(w, r, orderID); !ok {
		return
	}
	_, err := s.Store.DB.ExecContext(r.Context(), `DELETE FROM orders WHERE order_id=?`, orderID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "删除失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

// updateOrder 更新订单（手动发货等）。
func (s *Server) updateOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	order, ok := s.requireOrderOwner(w, r, orderID)
	if !ok {
		return
	}
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
	finalItemID := strings.TrimSpace(order.ItemID)
	itemIDPatch := req.ItemID
	if req.ItemID != nil {
		finalItemID = strings.TrimSpace(*req.ItemID)
		itemIDPatch = &finalItemID
	}
	itemTitle := ""
	if req.ItemTitle != nil {
		itemTitle = strings.TrimSpace(*req.ItemTitle)
		if itemTitle == "" || finalItemID == "" {
			writeErr(w, http.StatusBadRequest, "商品标题不能为空且订单必须关联商品")
			return
		}
	}
	tx, err := s.Store.DB.BeginTx(r.Context(), nil)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	defer tx.Rollback()
	if err := s.Store.Orders.PatchTx(r.Context(), tx, orderID, db.OrderPatch{
		OrderStatus: status, ItemID: itemIDPatch, BuyerID: req.BuyerID,
		SpecName: req.SpecName, SpecValue: req.SpecValue,
		Quantity: stringPtrFromAny(req.Quantity), Amount: amount,
		ReceiverName: req.ReceiverName, ReceiverPhone: req.ReceiverPhone,
		ReceiverAddr: req.ReceiverAddress, ReceiverCity: req.ReceiverCity,
		ChatID: req.ChatID, SystemShipped: req.SystemShipped,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	if req.ItemTitle != nil {
		if err := s.Store.Items.UpsertBasicTx(r.Context(), tx, &db.ItemInfoRow{CookieID: order.CookieID, ItemID: finalItemID, ItemTitle: itemTitle}); err != nil {
			writeErr(w, http.StatusInternalServerError, "更新商品标题失败")
			return
		}
	}
	if err := tx.Commit(); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
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
	userCookies, err := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询账号失败")
		return
	}

	successCount, failedCount := 0, 0
	results := make([]map[string]any, 0, len(req.OrderIDs))
	for _, orderID := range req.OrderIDs {
		orderID = strings.TrimSpace(orderID)
		if orderID == "" {
			continue
		}
		order, err := s.Store.Orders.Get(r.Context(), orderID)
		if err != nil || order == nil {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "订单不存在"})
			continue
		}
		_, ok := userCookies[order.CookieID]
		if !ok {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "无权操作此订单"})
			continue
		}
		if db.NormalizeOrderStatus(strings.TrimSpace(order.OrderStatus)) != "pending_ship" {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "仅待发货订单可以执行手动发货"})
			continue
		}
		if req.ShipMode == "full_delivery" {
			if s.Manager == nil || s.automation == nil {
				failedCount++
				results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "自动化中心未初始化"})
				continue
			}
			if _, running := s.Manager.GetInstance(order.CookieID); !running {
				failedCount++
				results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "该账号未在线运行，无法执行完整发货"})
				continue
			}
			sent, err := s.automation.ManualFullDelivery(r.Context(), order)
			if err != nil {
				failedCount++
				results = append(results, map[string]any{"order_id": orderID, "success": false, "message": err.Error()})
				s.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, "手动完整发货失败: "+err.Error())
				continue
			}
			successCount++
			results = append(results, map[string]any{
				"order_id": orderID,
				"success":  true,
				"message":  fmt.Sprintf("完整发货成功，已发送%d条卡券信息给买家", sent),
			})
			s.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID,
				fmt.Sprintf("手动完整发货成功（订单 %s，已发送 %d 条）", orderID, sent))
			continue
		}
		if s.MTop == nil {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "mtop 客户端未初始化"})
			continue
		}
		ok, ret, runtimeCookie, runtimeCookieChanged, err := s.consignWithCurrentCookie(r.Context(), order.CookieID, orderID, sess.UserID)
		if runtimeCookieChanged && s.Manager != nil {
			if acc, running := s.Manager.GetInstance(order.CookieID); running {
				acc.UpdateCookie(runtimeCookie)
			}
		}
		if err != nil && !ok {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "确认发货异常: " + err.Error()})
			s.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, "手动确认发货异常: "+err.Error())
			continue
		}
		if !ok {
			failedCount++
			msg := "确认发货失败"
			if len(ret) > 0 {
				msg += ": " + strings.Join(ret, "; ")
			}
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": msg})
			s.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, "手动确认发货失败: "+msg)
			continue
		}
		sysShip := true
		if err := s.Store.Orders.Upsert(r.Context(), orderID, db.OrderUpsertOpts{
			CookieID:      order.CookieID,
			OrderStatus:   "shipped",
			SystemShipped: &sysShip,
			ItemID:        order.ItemID,
			BuyerID:       order.BuyerID,
			ReceiverName:  order.ReceiverName,
			ReceiverPhone: order.ReceiverPhone,
			ReceiverAddr:  order.ReceiverAddr,
			ReceiverCity:  order.ReceiverCity,
			ChatID:        order.ChatID,
			SpecName:      order.SpecName,
			SpecValue:     order.SpecValue,
			Quantity:      order.Quantity,
			Amount:        order.Amount,
		}); err != nil {
			s.Logger.Error("更新订单为系统已发货失败", "order_id", orderID, "err", err)
		}
		successCount++
		message := "已成功修改闲鱼发货状态"
		credentialWarning := ""
		if err != nil {
			message += "；但登录凭证更新保存失败，请尽快重新登录（请勿重复确认发货）"
			credentialWarning = err.Error()
		}
		results = append(results, map[string]any{"order_id": orderID, "success": true, "message": message, "credential_warning": credentialWarning})
		s.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID,
			fmt.Sprintf("手动确认发货成功（订单 %s）", orderID))
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       failedCount == 0,
		"message":       fmt.Sprintf("手动发货完成: 成功%d个, 失败%d个", successCount, failedCount),
		"success_count": successCount,
		"failed_count":  failedCount,
		"results":       results,
	})
}

func (s *Server) consignWithCurrentCookie(ctx context.Context, cookieID, orderID string, userID int64) (bool, []string, string, bool, error) {
	credentialUnlock := s.Store.LockAccountCredentials(cookieID)
	defer credentialUnlock()
	detail, err := s.Store.Cookies.GetDetails(ctx, cookieID)
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
	userCookies, err := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询账号失败")
		return
	}
	defaultCookieID := ""
	if len(userCookies) == 1 {
		for cid := range userCookies {
			defaultCookieID = cid
		}
	}

	successCount, failedCount := 0, 0
	results := make([]map[string]any, 0, len(orders))
	for _, raw := range orders {
		orderID := firstImportString(raw, "order_id")
		if orderID == "" {
			failedCount++
			results = append(results, map[string]any{"order_id": "unknown", "success": false, "message": "缺少必需字段: order_id"})
			continue
		}
		cookieID := firstImportString(raw, "cookie_id")
		if cookieID == "" {
			cookieID = defaultCookieID
		}
		if cookieID == "" {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "缺少必需字段: cookie_id"})
			continue
		}
		if _, ok := userCookies[cookieID]; !ok {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "无权操作此账号的订单"})
			continue
		}
		status := firstImportString(raw, "order_status", "status", "status_text")
		if status != "" {
			status = db.NormalizeOrderStatus(status)
			if !validEditableOrderStatus(status) {
				failedCount++
				results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "不支持的订单状态"})
				continue
			}
		}
		itemID := firstImportString(raw, "item_id")
		amount, amountOK := normalizeOrderAmount(firstImportString(raw, "amount"))
		if !amountOK {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "订单金额必须是普通格式的非负有限数字"})
			continue
		}
		tx, err := s.Store.DB.BeginTx(r.Context(), nil)
		if err != nil {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "开始导入事务失败"})
			continue
		}
		if err := s.Store.Orders.UpsertTx(r.Context(), tx, orderID, db.OrderUpsertOpts{
			CookieID:      cookieID,
			ItemID:        itemID,
			BuyerID:       firstImportString(raw, "buyer_id"),
			OrderStatus:   status,
			SpecName:      firstImportString(raw, "spec_name"),
			SpecValue:     firstImportString(raw, "spec_value"),
			Quantity:      firstImportString(raw, "quantity"),
			Amount:        amount,
			ReceiverName:  firstImportString(raw, "receiver_name"),
			ReceiverPhone: firstImportString(raw, "receiver_phone"),
			ReceiverAddr:  firstImportString(raw, "receiver_address"),
			ReceiverCity:  firstImportString(raw, "receiver_city"),
			ChatID:        firstImportString(raw, "chat_id"),
		}); err != nil {
			_ = tx.Rollback()
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": err.Error()})
			continue
		}
		if itemID != "" {
			if err := s.Store.Items.UpsertBasicTx(r.Context(), tx, &db.ItemInfoRow{
				CookieID:   cookieID,
				ItemID:     itemID,
				ItemTitle:  firstImportString(raw, "item_title"),
				ItemPrice:  firstImportString(raw, "item_price"),
				ItemDetail: firstImportString(raw, "item_detail", "item_description"),
			}); err != nil {
				_ = tx.Rollback()
				failedCount++
				results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "补全商品信息失败: " + err.Error()})
				continue
			}
		}
		if err := tx.Commit(); err != nil {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "提交导入事务失败"})
			continue
		}
		successCount++
		results = append(results, map[string]any{"order_id": orderID, "success": true, "message": "订单已导入"})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       failedCount == 0,
		"message":       fmt.Sprintf("导入完成: 成功%d个, 失败%d个", successCount, failedCount),
		"total":         len(orders),
		"success_count": successCount,
		"failed_count":  failedCount,
		"results":       results,
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
