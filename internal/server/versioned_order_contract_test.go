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
	assertOpenAPISuccessResponse(t, listReq, listRecorder)
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
	assertOpenAPISuccessResponse(t, detailReq, detailRecorder)
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
	assertOpenAPISuccessResponse(t, updateReq, updateRecorder)
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

// TestVersionedOrderRefreshAndBatchRoutesPreserveLegacyContracts 验证订单刷新和批量操作版本化入口复用旧 handler。
func TestVersionedOrderRefreshAndBatchRoutesPreserveLegacyContracts(t *testing.T) {
	// srv 是用于验证订单刷新与批量路由的 HTTP 测试服务。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 是写入单订单夹具时使用的独立请求上下文。
	ctx := context.Background()
	// insertErr 是订单夹具写入失败的原因。
	_, insertErr := store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('order-refresh-v1','item-v1','buyer-v1','pending_ship','acc1')`)
	if insertErr != nil {
		t.Fatalf("insert refresh order fixture: %v", insertErr)
	}
	// handler 是当前测试使用的完整路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后得到的认证会话。
	sessionCookie := loginHelper(t, handler)

	// refreshReq 是版本化订单列表刷新请求。
	refreshReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/refresh", nil)
	refreshReq.AddCookie(sessionCookie)
	// refreshRecorder 是捕获版本化刷新响应的记录器。
	refreshRecorder := httptest.NewRecorder()
	handler.ServeHTTP(refreshRecorder, refreshReq)
	assertOpenAPIExpectedStatusResponse(t, refreshReq, refreshRecorder, http.StatusAccepted)
	// refreshStart 是版本化刷新任务创建响应 DTO。
	var refreshStart orderRefreshJobStartResponse
	// refreshDecodeErr 是刷新任务创建响应反序列化失败的原因。
	if refreshDecodeErr := json.Unmarshal(refreshRecorder.Body.Bytes(), &refreshStart); refreshDecodeErr != nil {
		t.Fatalf("decode versioned order refresh: %v", refreshDecodeErr)
	}
	// versionedStatusReq 查询刚创建的版本化刷新任务，覆盖真实 200 状态响应。
	versionedStatusReq := httptest.NewRequest(http.MethodGet, "/api/v1/orders/refresh/"+refreshStart.JobID, nil)
	versionedStatusReq.AddCookie(sessionCookie)
	// versionedStatusRecorder 保存版本化刷新任务查询响应。
	versionedStatusRecorder := httptest.NewRecorder()
	handler.ServeHTTP(versionedStatusRecorder, versionedStatusReq)
	assertOpenAPIRecordedSuccessResponse(t, versionedStatusReq, versionedStatusRecorder)
	// refreshValue 是版本化刷新任务完成后的具名结果。
	refreshValue := waitOrderRefreshJob(t, handler, sessionCookie, refreshStart.JobID)
	if refreshValue.Message == "" {
		t.Fatalf("versioned order refresh should preserve a named success response: %+v", refreshValue)
	}
	// queuedJob 是专门用于版本化取消 operation 的排队任务夹具。
	queuedJob := &db.OrderRefreshJob{ID: "versioned-cancel-job", UserID: 1, Status: "queued"}
	// createErr 表示写入版本化取消任务夹具失败。
	if createErr := store.OrderRefreshJobs.Create(ctx, queuedJob); createErr != nil {
		t.Fatalf("create versioned cancel job: %v", createErr)
	}
	// versionedCancelReq 请求取消该排队任务。
	versionedCancelReq := httptest.NewRequest(http.MethodDelete, "/api/v1/orders/refresh/"+queuedJob.ID, nil)
	versionedCancelReq.AddCookie(sessionCookie)
	// versionedCancelRecorder 保存版本化取消响应。
	versionedCancelRecorder := httptest.NewRecorder()
	handler.ServeHTTP(versionedCancelRecorder, versionedCancelReq)
	assertOpenAPIRecordedSuccessResponse(t, versionedCancelReq, versionedCancelRecorder)

	// legacyRefreshReq 是验证旧订单列表刷新入口仍可用的请求。
	legacyRefreshReq := httptest.NewRequest(http.MethodPost, "/api/orders/refresh", nil)
	legacyRefreshReq.AddCookie(sessionCookie)
	// legacyRefreshRecorder 是捕获旧刷新响应的记录器。
	legacyRefreshRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyRefreshRecorder, legacyRefreshReq)
	if legacyRefreshRecorder.Code != http.StatusAccepted {
		t.Fatalf("legacy order refresh status=%d body=%s", legacyRefreshRecorder.Code, legacyRefreshRecorder.Body.String())
	}
	// legacyRefreshStart 是旧路径刷新任务创建响应 DTO。
	var legacyRefreshStart orderRefreshJobStartResponse
	// err 表示旧路径任务创建响应解析错误。
	if err := json.Unmarshal(legacyRefreshRecorder.Body.Bytes(), &legacyRefreshStart); err != nil {
		t.Fatalf("decode legacy order refresh: %v", err)
	}
	waitOrderRefreshJob(t, handler, sessionCookie, legacyRefreshStart.JobID)

	// singleInsertErr 是单订单刷新夹具写入失败的原因。
	_, singleInsertErr := store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('order-single-v1','item-v1','buyer-v1','pending_ship','acc1')`)
	if singleInsertErr != nil {
		t.Fatalf("insert single refresh order fixture: %v", singleInsertErr)
	}
	// singleReq 是版本化单订单刷新请求。
	singleReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/order-single-v1/refresh", nil)
	singleReq.AddCookie(sessionCookie)
	// singleRecorder 是捕获版本化单订单刷新响应的记录器。
	singleRecorder := httptest.NewRecorder()
	handler.ServeHTTP(singleRecorder, singleReq)
	assertOpenAPISuccessResponse(t, singleReq, singleRecorder)

	// legacySingleReq 是验证旧单订单刷新入口仍可用的请求。
	legacySingleReq := httptest.NewRequest(http.MethodPost, "/api/orders/order-single-v1/refresh", nil)
	legacySingleReq.AddCookie(sessionCookie)
	// legacySingleRecorder 是捕获旧单订单刷新响应的记录器。
	legacySingleRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacySingleRecorder, legacySingleReq)
	if legacySingleRecorder.Code != singleRecorder.Code {
		t.Fatalf("legacy single order refresh status=%d versioned=%d", legacySingleRecorder.Code, singleRecorder.Code)
	}

	// manualReq 是版本化手动发货请求，空订单列表用于验证参数边界而不触发远端调用。
	manualReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/manual-ship", strings.NewReader(`{"order_ids":[]}`))
	manualReq.AddCookie(sessionCookie)
	// manualRecorder 是捕获版本化手动发货响应的记录器。
	manualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(manualRecorder, manualReq)
	if manualRecorder.Code != http.StatusBadRequest {
		t.Fatalf("versioned manual ship status=%d body=%s", manualRecorder.Code, manualRecorder.Body.String())
	}

	// legacyManualReq 是验证旧手动发货入口仍可用的请求。
	legacyManualReq := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(`{"order_ids":[]}`))
	legacyManualReq.AddCookie(sessionCookie)
	// legacyManualRecorder 是捕获旧手动发货响应的记录器。
	legacyManualRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyManualRecorder, legacyManualReq)
	if legacyManualRecorder.Code != manualRecorder.Code {
		t.Fatalf("legacy manual ship status=%d versioned=%d", legacyManualRecorder.Code, manualRecorder.Code)
	}

	// importReq 是版本化空订单导入请求。
	importReq := httptest.NewRequest(http.MethodPost, "/api/v1/orders/import", strings.NewReader(`[]`))
	importReq.AddCookie(sessionCookie)
	// importRecorder 是捕获版本化导入响应的记录器。
	importRecorder := httptest.NewRecorder()
	handler.ServeHTTP(importRecorder, importReq)
	assertOpenAPISuccessResponse(t, importReq, importRecorder)

	// legacyImportReq 是验证旧导入入口仍可用的请求。
	legacyImportReq := httptest.NewRequest(http.MethodPost, "/api/orders/import", strings.NewReader(`[]`))
	legacyImportReq.AddCookie(sessionCookie)
	// legacyImportRecorder 是捕获旧导入响应的记录器。
	legacyImportRecorder := httptest.NewRecorder()
	handler.ServeHTTP(legacyImportRecorder, legacyImportReq)
	if legacyImportRecorder.Code != importRecorder.Code {
		t.Fatalf("legacy order import status=%d versioned=%d", legacyImportRecorder.Code, importRecorder.Code)
	}
}
