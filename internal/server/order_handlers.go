package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
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

	all, err := s.Store.Cookies.AllForUser(r.Context(), sess.UserID)
	if err != nil {
		writeErr(w, http.StatusInternalServerError, "查询失败")
		return
	}
	if cookieID != "" {
		for k := range all {
			if k != cookieID {
				delete(all, k)
			}
		}
	}
	var orders []map[string]any
	for cid := range all {
		itemRows, _ := s.Store.Items.AllForCookie(r.Context(), cid)
		itemsByID := make(map[string]db.ItemInfoRow, len(itemRows))
		for _, item := range itemRows {
			itemsByID[item.ItemID] = item
		}
		rows, err := s.Store.Orders.ByCookie(r.Context(), cid, 1000)
		if err != nil {
			continue
		}
		for _, o := range rows {
			st := db.NormalizeOrderStatus(o.OrderStatus)
			if status != "" && st != status {
				continue
			}
			item := itemsByID[o.ItemID]
			orders = append(orders, map[string]any{
				"order_id":         o.OrderID,
				"item_id":          o.ItemID,
				"item_title":       item.ItemTitle,
				"item_image":       itemImageFromDetail(item.ItemDetail),
				"buyer_id":         o.BuyerID,
				"spec_name":        o.SpecName,
				"spec_value":       o.SpecValue,
				"quantity":         o.Quantity,
				"amount":           o.Amount,
				"order_status":     st,
				"status":           st,
				"cookie_id":        cid,
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
	}

	total := len(orders)
	totalPages := (total + pageSize - 1) / pageSize
	start := (page - 1) * pageSize
	end := start + pageSize
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	paged := []map[string]any{}
	if start < end {
		paged = orders[start:end]
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"success":     true,
		"data":        paged,
		"total":       total,
		"page":        page,
		"page_size":   pageSize,
		"total_pages": totalPages,
	})
}

// getOrder 订单详情。
func (s *Server) getOrder(w http.ResponseWriter, r *http.Request) {
	orderID := chi.URLParam(r, "order_id")
	o, err := s.Store.Orders.Get(r.Context(), orderID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "订单不存在")
		return
	}
	itemTitle, itemImage := "", ""
	if item, itemErr := s.Store.Items.Get(r.Context(), o.CookieID, o.ItemID); itemErr == nil {
		itemTitle = item.ItemTitle
		itemImage = itemImageFromDetail(item.ItemDetail)
	}
	writeJSON(w, http.StatusOK, map[string]any{
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
	})
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
	order, err := s.Store.Orders.Get(r.Context(), orderID)
	if err != nil {
		writeErr(w, http.StatusNotFound, "订单不存在")
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
		_ = s.Store.Cookies.Save(r.Context(), cookieID, detail.UpdatedCookies, 0)
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
			if s.Manager == nil || s.Automation == nil {
				failedCount++
				results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "自动化中心未初始化"})
				continue
			}
			if _, running := s.Manager.GetInstance(order.CookieID); !running {
				failedCount++
				results = append(results, map[string]any{"order_id": orderID, "success": false, "message": "该账号未在线运行，无法执行完整发货"})
				continue
			}
			sent, err := s.Automation.ManualFullDelivery(r.Context(), order)
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
			_ = s.Store.Cookies.Save(r.Context(), order.CookieID, updatedCookies, sess.UserID)
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
		_ = s.Store.Orders.Upsert(r.Context(), orderID, db.OrderUpsertOpts{
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
		})
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
			_ = s.Store.Items.UpsertBasic(r.Context(), &db.ItemInfoRow{
				CookieID:   cookieID,
				ItemID:     itemID,
				ItemTitle:  firstImportString(raw, "item_title"),
				ItemPrice:  firstImportString(raw, "item_price"),
				ItemDetail: firstImportString(raw, "item_detail", "item_description"),
			})
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
	if s.Notifier == nil {
		return
	}
	s.Notifier.NotifyDelivery(cookieID, "", buyerID, itemID, message, chatID)
}

func stringFromAny(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case fmt.Stringer:
		return x.String()
	case float64:
		if x == float64(int64(x)) {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	case bool:
		return strconv.FormatBool(x)
	default:
		return fmt.Sprint(x)
	}
}

func parseImportedOrders(w http.ResponseWriter, r *http.Request) ([]map[string]any, error) {
	ct := r.Header.Get("Content-Type")
	if strings.HasPrefix(ct, "multipart/form-data") {
		// 订单文件上限 32 MiB；MaxBytesReader 必须在 ParseMultipartForm 前应用。
		r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
		// #nosec G120 -- 请求体已由 MaxBytesReader 限制为 32 MiB。
		if err := r.ParseMultipartForm(32 << 20); err != nil {
			return nil, fmt.Errorf("解析上传文件失败: %w", err)
		}
		file, header, err := r.FormFile("file")
		if err != nil {
			return nil, fmt.Errorf("缺少上传文件")
		}
		defer file.Close()
		raw, err := io.ReadAll(io.LimitReader(file, (32<<20)+1))
		if err != nil {
			return nil, fmt.Errorf("读取上传文件失败: %w", err)
		}
		if len(raw) > 32<<20 {
			return nil, fmt.Errorf("上传文件不能超过 32 MiB")
		}
		return parseImportedOrderBytes(raw, header.Filename)
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, (32<<20)+1))
	if err != nil {
		return nil, fmt.Errorf("读取请求失败: %w", err)
	}
	if len(raw) > 32<<20 {
		return nil, fmt.Errorf("导入内容不能超过 32 MiB")
	}
	return parseImportedOrderBytes(raw, "orders.json")
}

func parseImportedOrderBytes(raw []byte, filename string) ([]map[string]any, error) {
	if len(bytes.TrimSpace(raw)) == 0 {
		return nil, fmt.Errorf("导入内容为空")
	}
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".xlsx":
		return parseXLSXOrders(raw)
	case ".csv":
		return parseDelimitedOrders(raw, ',')
	case ".tsv":
		return parseDelimitedOrders(raw, '\t')
	case ".xls":
		return nil, fmt.Errorf("暂不支持旧版 .xls，请另存为 .xlsx 或 CSV 后导入")
	default:
		if bytes.HasPrefix(bytes.TrimSpace(raw), []byte("[")) || bytes.HasPrefix(bytes.TrimSpace(raw), []byte("{")) {
			return parseJSONOrders(raw)
		}
		return parseDelimitedOrders(raw, ',')
	}
}

func parseJSONOrders(raw []byte) ([]map[string]any, error) {
	var arr []map[string]any
	if err := json.Unmarshal(raw, &arr); err == nil {
		return normalizeImportOrderMaps(arr), nil
	}
	var single map[string]any
	if err := json.Unmarshal(raw, &single); err != nil {
		return nil, fmt.Errorf("解析 JSON 失败: %w", err)
	}
	return normalizeImportOrderMaps([]map[string]any{single}), nil
}

func parseDelimitedOrders(raw []byte, comma rune) ([]map[string]any, error) {
	reader := csv.NewReader(bytes.NewReader(raw))
	reader.Comma = comma
	reader.FieldsPerRecord = -1
	reader.LazyQuotes = true
	records, err := reader.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("解析表格失败: %w", err)
	}
	if len(records) < 2 {
		return nil, fmt.Errorf("表格至少需要表头和一行数据")
	}
	headers := normalizeImportHeaders(records[0])
	out := make([]map[string]any, 0, len(records)-1)
	for _, row := range records[1:] {
		m := make(map[string]any)
		nonEmpty := false
		for i, h := range headers {
			if h == "" || i >= len(row) {
				continue
			}
			v := strings.TrimSpace(row[i])
			if v != "" {
				nonEmpty = true
			}
			m[h] = v
		}
		if nonEmpty {
			out = append(out, m)
		}
	}
	return out, nil
}

func parseXLSXOrders(raw []byte) ([]map[string]any, error) {
	zr, err := zip.NewReader(bytes.NewReader(raw), int64(len(raw)))
	if err != nil {
		return nil, fmt.Errorf("解析 xlsx 失败: %w", err)
	}
	shared := xlsxSharedStrings(zr)
	var sheet *zip.File
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "xl/worksheets/sheet") && strings.HasSuffix(f.Name, ".xml") {
			sheet = f
			break
		}
	}
	if sheet == nil {
		return nil, fmt.Errorf("xlsx 中未找到工作表")
	}
	rc, err := sheet.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	var ws xlsxWorksheet
	if err := xml.NewDecoder(rc).Decode(&ws); err != nil {
		return nil, fmt.Errorf("解析工作表失败: %w", err)
	}
	rows := make([][]string, 0, len(ws.SheetData.Rows))
	for _, row := range ws.SheetData.Rows {
		values := []string{}
		for _, cell := range row.Cells {
			idx := xlsxCellIndex(cell.Ref)
			for len(values) <= idx {
				values = append(values, "")
			}
			values[idx] = xlsxCellValue(cell, shared)
		}
		rows = append(rows, values)
	}
	if len(rows) < 2 {
		return nil, fmt.Errorf("xlsx 至少需要表头和一行数据")
	}
	headers := normalizeImportHeaders(rows[0])
	out := make([]map[string]any, 0, len(rows)-1)
	for _, row := range rows[1:] {
		m := make(map[string]any)
		nonEmpty := false
		for i, h := range headers {
			if h == "" || i >= len(row) {
				continue
			}
			v := strings.TrimSpace(row[i])
			if v != "" {
				nonEmpty = true
			}
			m[h] = v
		}
		if nonEmpty {
			out = append(out, m)
		}
	}
	return out, nil
}

type xlsxWorksheet struct {
	SheetData struct {
		Rows []xlsxRow `xml:"row"`
	} `xml:"sheetData"`
}

type xlsxRow struct {
	Cells []xlsxCell `xml:"c"`
}

type xlsxCell struct {
	Ref       string `xml:"r,attr"`
	Type      string `xml:"t,attr"`
	Value     string `xml:"v"`
	InlineStr string `xml:"is>t"`
}

type xlsxSST struct {
	Items []struct {
		Inner string `xml:",innerxml"`
	} `xml:"si"`
}

func xlsxSharedStrings(zr *zip.Reader) []string {
	for _, f := range zr.File {
		if f.Name != "xl/sharedStrings.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil
		}
		defer rc.Close()
		var sst xlsxSST
		if err := xml.NewDecoder(rc).Decode(&sst); err != nil {
			return nil
		}
		out := make([]string, 0, len(sst.Items))
		for _, item := range sst.Items {
			out = append(out, xmlCharData(item.Inner))
		}
		return out
	}
	return nil
}

func xlsxCellValue(cell xlsxCell, shared []string) string {
	switch cell.Type {
	case "s":
		idx, _ := strconv.Atoi(strings.TrimSpace(cell.Value))
		if idx >= 0 && idx < len(shared) {
			return shared[idx]
		}
	case "inlineStr":
		return strings.TrimSpace(cell.InlineStr)
	}
	return strings.TrimSpace(cell.Value)
}

func xlsxCellIndex(ref string) int {
	idx := 0
	for _, r := range ref {
		if r < 'A' || r > 'Z' {
			break
		}
		idx = idx*26 + int(r-'A'+1)
	}
	if idx == 0 {
		return 0
	}
	return idx - 1
}

func xmlCharData(inner string) string {
	dec := xml.NewDecoder(strings.NewReader("<x>" + inner + "</x>"))
	var parts []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if data, ok := tok.(xml.CharData); ok {
			parts = append(parts, string(data))
		}
	}
	return strings.TrimSpace(strings.Join(parts, ""))
}

func normalizeImportOrderMaps(in []map[string]any) []map[string]any {
	out := make([]map[string]any, 0, len(in))
	for _, raw := range in {
		m := make(map[string]any)
		for k, v := range raw {
			m[normalizeImportHeader(k)] = v
		}
		out = append(out, m)
	}
	return out
}

func normalizeImportHeaders(headers []string) []string {
	out := make([]string, len(headers))
	for i, h := range headers {
		out[i] = normalizeImportHeader(h)
	}
	return out
}

func normalizeImportHeader(header string) string {
	h := strings.ToLower(strings.TrimSpace(header))
	h = strings.NewReplacer(" ", "", "_", "", "-", "", "（", "(", "）", ")").Replace(h)
	switch h {
	case "orderid", "订单号", "订单id", "订单编号":
		return "order_id"
	case "cookieid", "账号id", "账号", "闲鱼账号":
		return "cookie_id"
	case "itemid", "商品id", "商品编号":
		return "item_id"
	case "itemtitle", "商品标题", "商品名称":
		return "item_title"
	case "itemprice", "商品价格":
		return "item_price"
	case "itemdetail", "itemdescription", "商品描述", "商品详情":
		return "item_detail"
	case "buyerid", "买家id":
		return "buyer_id"
	case "status", "orderstatus", "订单状态", "状态":
		return "status"
	case "specname", "规格名":
		return "spec_name"
	case "specvalue", "规格值":
		return "spec_value"
	case "quantity", "数量":
		return "quantity"
	case "amount", "金额", "订单金额":
		return "amount"
	case "receivername", "收件人", "收货人":
		return "receiver_name"
	case "receiverphone", "手机号", "收件电话", "收货电话":
		return "receiver_phone"
	case "receiveraddress", "地址", "收件地址", "收货地址":
		return "receiver_address"
	case "receivercity", "城市", "收件城市", "收货城市":
		return "receiver_city"
	case "chatid", "会话id":
		return "chat_id"
	default:
		return header
	}
}

func firstImportString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if v, ok := m[k]; ok {
			s := strings.TrimSpace(stringFromAny(v))
			if s != "" {
				return s
			}
		}
	}
	return ""
}
