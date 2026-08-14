package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// TestVersionedOrderRoutesPreserveLegacyContracts 验证订单列表、详情和更新入口复用旧 handler 并保留旧路径。
func TestVersionedOrderRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证版本化订单路由的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是写入订单夹具时使用的独立请求上下文。
	ctx := context.Background()
	// insertErr 是订单夹具写入失败的原因。
	_, insertErr := store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('order-v1','item-v1','buyer-v1','pending_ship','acc1')`)
	if insertErr != nil {
		t.Fatalf("insert order fixture: %v", insertErr)
	}
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)

	// listReq 是读取版本化订单列表的请求。
	listReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders?page=1&page_size=20", nil)
	listReq.AddCookie(sessionCookie)
	// listRecorder 是捕获版本化订单列表响应的记录器。
	listRecorder := httptest.NewRecorder()
	handler.ServeHTTP(listRecorder, listReq)
	if listRecorder.Code != http.StatusOK {
		t.Fatalf("versioned order list status=%d body=%s", listRecorder.Code, listRecorder.Body.String())
	}
	// listValue 是版本化订单列表响应 DTO。
	var listValue orderListResponse
	// listDecodeErr 是订单列表响应反序列化失败的原因。
	if listDecodeErr := json.Unmarshal(listRecorder.Body.Bytes(), &listValue); listDecodeErr != nil {
		t.Fatalf("decode versioned order list: %v", listDecodeErr)
	}
	if !listValue.Success || listValue.Total != 1 || len(listValue.Data) != 1 || listValue.Data[0].OrderID != "order-v1" {
		t.Fatalf("versioned order list=%+v", listValue)
	}

	// detailReq 是读取版本化订单详情的请求。
	detailReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders/order-v1", nil)
	detailReq.AddCookie(sessionCookie)
	// detailRecorder 是捕获版本化订单详情响应的记录器。
	detailRecorder := httptest.NewRecorder()
	handler.ServeHTTP(detailRecorder, detailReq)
	if detailRecorder.Code != http.StatusOK {
		t.Fatalf("versioned order detail status=%d body=%s", detailRecorder.Code, detailRecorder.Body.String())
	}
	// detailValue 是版本化订单详情响应 DTO。
	var detailValue orderDetailResponse
	// detailDecodeErr 是订单详情响应反序列化失败的原因。
	if detailDecodeErr := json.Unmarshal(detailRecorder.Body.Bytes(), &detailValue); detailDecodeErr != nil {
		t.Fatalf("decode versioned order detail: %v", detailDecodeErr)
	}
	if !detailValue.Success || detailValue.Data.OrderID != "order-v1" || detailValue.OrderID != "order-v1" {
		t.Fatalf("versioned order detail=%+v", detailValue)
	}

	// updateReq 是通过版本化入口更新订单状态的请求。
	updateReq := httptest.NewRequest(http.MethodPut, "/api/v1/orders/order-v1", strings.NewReader(`{"status":"shipped"}`))
	updateReq.AddCookie(sessionCookie)
	// updateRecorder 是捕获版本化订单更新响应的记录器。
	updateRecorder := httptest.NewRecorder()
	handler.ServeHTTP(updateRecorder, updateReq)
	if updateRecorder.Code != http.StatusOK {
		t.Fatalf("versioned order update status=%d body=%s", updateRecorder.Code, updateRecorder.Body.String())
	}
	// updateValue 是版本化订单更新响应 DTO。
	var updateValue operationResponse
	// updateDecodeErr 是订单更新响应反序列化失败的原因。
	if updateDecodeErr := json.Unmarshal(updateRecorder.Body.Bytes(), &updateValue); updateDecodeErr != nil {
		t.Fatalf("decode versioned order update: %v", updateDecodeErr)
	}
	if !updateValue.Success {
		t.Fatalf("versioned order update=%+v", updateValue)
	}

	// legacyReq 是验证旧订单更新入口仍可用的请求。
	legacyReq := httptest.NewRequest(http.MethodPut, "/api/orders/order-v1", strings.NewReader(`{"status":"completed"}`))
	legacyReq.AddCookie(sessionCookie)
	// legacyRecorder 是捕获旧订单更新响应的记录器。
	legacyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyRecorder, legacyReq)
	if legacyRecorder.Code != http.StatusOK {
		t.Fatalf("legacy order update status=%d body=%s", legacyRecorder.Code, legacyRecorder.Body.String())
	}
	// orderValue 是数据库中用于确认旧入口更新结果的订单记录。
	orderValue, orderErr := store.Orders.Get(ctx, "order-v1")
	if orderErr != nil || orderValue == nil || db.NormalizeOrderStatus(orderValue.OrderStatus) != "completed" {
		t.Fatalf("legacy order update not persisted: order=%+v err=%v", orderValue, orderErr)
	}
}
