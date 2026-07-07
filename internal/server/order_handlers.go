package server

import (
	"context"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"xianyu-go/internal/auth"
	"xianyu-go/internal/db"
)

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
		UserID: sess.UserID, CookieID: cookieID, Status: status, Limit: pageSize, Offset: offset,
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
	if s.Browser == nil {
		writeErr(w, http.StatusServiceUnavailable, "浏览器自动化未启用，无法刷新订单")
		return
	}
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

	type refreshTarget struct {
		OrderID       string
		CurrentStatus string
	}
	ordersByCookie := map[string][]refreshTarget{}
	for cid := range all {
		rows, err := s.Store.Orders.ByCookie(r.Context(), cid, 1000)
		if err != nil {
			continue
		}
		for _, row := range rows {
			currentStatus := db.NormalizeOrderStatus(row.OrderStatus)
			if status != "" && status != "all" && currentStatus != status {
				continue
			}
			// 稳定状态无需反复抓取；但历史订单若缺少实付金额，仍需补全详情。
			if isStableOrderStatus(currentStatus) && strings.TrimSpace(row.Amount) != "" {
				continue
			}
			ordersByCookie[cid] = append(ordersByCookie[cid], refreshTarget{
				OrderID:       row.OrderID,
				CurrentStatus: currentStatus,
			})
		}
	}

	total := 0
	for _, targets := range ordersByCookie {
		total += len(targets)
	}
	if total == 0 {
		writeJSON(w, http.StatusOK, map[string]any{
			"success": true,
			"message": "没有需要刷新的订单",
			"summary": map[string]int{"total": 0, "updated": 0, "no_change": 0, "failed": 0},
			"results": []any{},
		})
		return
	}

	updated, noChange, failed := 0, 0, 0
	results := []map[string]any{}
	for cid, targets := range ordersByCookie {
		cookieValue := all[cid]
		if cookieValue == "" {
			failed += len(targets)
			continue
		}
		orderIDs := make([]string, 0, len(targets))
		currentStatus := make(map[string]string, len(targets))
		for _, target := range targets {
			orderIDs = append(orderIDs, target.OrderID)
			currentStatus[target.OrderID] = target.CurrentStatus
		}
		ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
		batch, err := s.Browser.BatchRefreshOrders(ctx, orderIDs, cid, cookieValue)
		cancel()
		if err != nil {
			failed += len(targets)
			results = append(results, map[string]any{"cookie_id": cid, "success": false, "error": err.Error()})
			continue
		}
		rawOrders, _ := batch["orders"].([]map[string]any)
		for _, raw := range rawOrders {
			orderID := stringFromAny(raw["order_id"])
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
			if newStatus == "unknown" {
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
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success": true,
		"message": "订单刷新完成",
		"summary": map[string]int{"total": total, "updated": updated, "no_change": noChange, "failed": failed},
		"results": results,
	})
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
	cookieValue, _, ok := s.cookieForCurrentUser(w, r, cookieID)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), time.Minute)
	defer cancel()
	detail, err := s.Browser.FetchOrderDetail(ctx, orderID, cookieID, cookieValue, s.Store.Items.IsMultiSpec(ctx, cookieID, order.ItemID))
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	if detail.UpdatedCookies != "" && detail.UpdatedCookies != cookieValue {
		if err := s.Store.Cookies.Save(r.Context(), cookieID, detail.UpdatedCookies, 0); err != nil {
			s.Logger.Error("保存订单详情刷新后的 cookie 失败", "cookie_id", cookieID, "err", err)
		}
		if s.Manager != nil {
			if account, running := s.Manager.GetInstance(cookieID); running {
				account.UpdateCookie(detail.UpdatedCookies)
			}
		}
	}
	status := db.NormalizeOrderStatus(detail.OrderStatus)
	if status == "unknown" {
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
	if _, ok := s.requireOrderOwner(w, r, orderID); !ok {
		return
	}
	var req struct {
		OrderStatus     string `json:"order_status"`
		Status          string `json:"status"`
		ItemID          string `json:"item_id"`
		BuyerID         string `json:"buyer_id"`
		SpecName        string `json:"spec_name"`
		SpecValue       string `json:"spec_value"`
		Quantity        any    `json:"quantity"`
		Amount          any    `json:"amount"`
		ReceiverName    string `json:"receiver_name"`
		ReceiverPhone   string `json:"receiver_phone"`
		ReceiverAddress string `json:"receiver_address"`
		ReceiverCity    string `json:"receiver_city"`
		ChatID          string `json:"chat_id"`
		SystemShipped   *bool  `json:"system_shipped"`
	}
	if err := decodeJSON(r, &req); err != nil {
		writeErr(w, http.StatusBadRequest, "请求格式错误")
		return
	}
	status := req.OrderStatus
	if status == "" {
		status = req.Status
	}
	if err := s.Store.Orders.Upsert(r.Context(), orderID, db.OrderUpsertOpts{
		OrderStatus:   status,
		ItemID:        req.ItemID,
		BuyerID:       req.BuyerID,
		SpecName:      req.SpecName,
		SpecValue:     req.SpecValue,
		Quantity:      stringFromAny(req.Quantity),
		Amount:        stringFromAny(req.Amount),
		ReceiverName:  req.ReceiverName,
		ReceiverPhone: req.ReceiverPhone,
		ReceiverAddr:  req.ReceiverAddress,
		ReceiverCity:  req.ReceiverCity,
		ChatID:        req.ChatID,
		SystemShipped: req.SystemShipped,
	}); err != nil {
		writeErr(w, http.StatusInternalServerError, "更新失败")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"success": true})
}

func (s *Server) manualShipOrders(w http.ResponseWriter, r *http.Request) {
	var req struct {
		OrderIDs      []string `json:"order_ids"`
		ShipMode      string   `json:"ship_mode"`
		CustomContent string   `json:"custom_content"`
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
		cookieValue, ok := userCookies[order.CookieID]
		if !ok {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "无权操作此订单"})
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
		ok, ret, updatedCookies, err := s.MTop.ConsignContext(r.Context(), cookieValue, orderID)
		if err != nil {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "确认发货异常: " + err.Error()})
			s.notifyDelivery(order.CookieID, order.BuyerID, order.ItemID, order.ChatID, "手动确认发货异常: "+err.Error())
			continue
		}
		if updatedCookies != "" && updatedCookies != cookieValue {
			if err := s.Store.Cookies.Save(r.Context(), order.CookieID, updatedCookies, sess.UserID); err != nil {
				s.Logger.Error("保存发货刷新后的 cookie 失败", "cookie_id", order.CookieID, "err", err)
			}
			if s.Manager != nil {
				if acc, running := s.Manager.GetInstance(order.CookieID); running {
					acc.UpdateCookie(updatedCookies)
				}
			}
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
		results = append(results, map[string]any{"order_id": orderID, "success": true, "message": "已成功修改闲鱼发货状态"})
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
		}
		itemID := firstImportString(raw, "item_id")
		if err := s.Store.Orders.Upsert(r.Context(), orderID, db.OrderUpsertOpts{
			CookieID:      cookieID,
			ItemID:        itemID,
			BuyerID:       firstImportString(raw, "buyer_id"),
			OrderStatus:   status,
			SpecName:      firstImportString(raw, "spec_name"),
			SpecValue:     firstImportString(raw, "spec_value"),
			Quantity:      firstImportString(raw, "quantity"),
			Amount:        firstImportString(raw, "amount"),
			ReceiverName:  firstImportString(raw, "receiver_name"),
			ReceiverPhone: firstImportString(raw, "receiver_phone"),
			ReceiverAddr:  firstImportString(raw, "receiver_address"),
			ReceiverCity:  firstImportString(raw, "receiver_city"),
			ChatID:        firstImportString(raw, "chat_id"),
		}); err != nil {
			failedCount++
			results = append(results, map[string]any{"order_id": orderID, "success": false, "message": err.Error()})
			continue
		}
		if itemID != "" {
			if err := s.Store.Items.UpsertBasic(r.Context(), &db.ItemInfoRow{
				CookieID:   cookieID,
				ItemID:     itemID,
				ItemTitle:  firstImportString(raw, "item_title"),
				ItemPrice:  firstImportString(raw, "item_price"),
				ItemDetail: firstImportString(raw, "item_detail", "item_description"),
			}); err != nil {
				s.Logger.Error("导入订单时补全商品信息失败", "cookie_id", cookieID, "item_id", itemID, "err", err)
			}
		}
		successCount++
		results = append(results, map[string]any{"order_id": orderID, "success": true, "message": "订单已导入"})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":       true,
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
