package server

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// itemStockPortStub 是商品库存 HTTP 契约测试使用的窄应用端口替身。
type itemStockPortStub struct {
	// snapshot、outcome 是预设的读取与修改结果。
	snapshot itemapp.StockSnapshot
	outcome  itemapp.StockUpdateOutcome
	// err 是预设的应用层错误。
	err error
	// getCalls、updateCalls 统计两个方法被调用的次数。
	getCalls, updateCalls int
	// userID、cookieID、itemID 记录 Handler 实际传入的归属和商品标识。
	userID   int64
	cookieID string
	itemID   string
	// update 记录 Handler 转换后的库存修改。
	update itemapp.StockUpdate
}

// GetStock 记录标识并返回预设结果。
func (port *itemStockPortStub) GetStock(_ context.Context, userID int64, cookieID, itemID string) (itemapp.StockSnapshot, error) {
	port.getCalls++
	port.userID, port.cookieID, port.itemID = userID, cookieID, itemID
	return port.snapshot, port.err
}

// UpdateStock 记录标识和修改内容并返回预设结果。
func (port *itemStockPortStub) UpdateStock(_ context.Context, userID int64, cookieID, itemID string, update itemapp.StockUpdate) (itemapp.StockUpdateOutcome, error) {
	port.updateCalls++
	port.userID, port.cookieID, port.itemID, port.update = userID, cookieID, itemID, update
	return port.outcome, port.err
}

// TestItemStockHandlerContract 验证真实路由读取与修改库存：响应符合 OpenAPI，并保留当前用户与路径标识。
func TestItemStockHandlerContract(t *testing.T) {
	// srv、cleanup 是使用隔离 SQLite 数据库构造的完整 HTTP Server 和清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 是预设多规格库存与修改成功结果的应用端口替身。
	port := &itemStockPortStub{
		snapshot: itemapp.StockSnapshot{ItemID: "item-1", MultiSpec: true, Quantity: 9, SKUs: []itemapp.StockSKU{{Key: "sku:a", Label: "红色", PriceCents: 100, Quantity: 4}, {Key: "sku:b", Label: "蓝色", PriceCents: 200, Quantity: 5}}},
		outcome:  itemapp.StockUpdateOutcome{ItemID: "item-1", Message: "SUCCESS::调用成功", CookiePersisted: true},
	}
	srv.applications.itemStock = port
	// handler、sessionCookie 是完整路由树和管理员登录会话。
	handler := srv.Router()
	// sessionCookie 是管理员登录后的会话 Cookie。
	sessionCookie := loginHelper(t, handler)

	// getRequest 读取多规格库存。
	getRequest := httptest.NewRequest(http.MethodGet, "/api/v1/items/acc1/item-1/stock", nil)
	getRequest.AddCookie(sessionCookie)
	// getRecorder 捕获读取响应。
	getRecorder := httptest.NewRecorder()
	handler.ServeHTTP(getRecorder, getRequest)
	assertOpenAPISuccessResponse(t, getRequest, getRecorder)
	// got 是解码后的读取响应。
	var got itemStockResponse
	// err 保存本次调用返回的失败原因，供后续断言或错误映射使用。
	if err := json.Unmarshal(getRecorder.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if !got.Success || !got.Data.MultiSpec || got.Data.Quantity != 9 || len(got.Data.SKUs) != 2 || got.Data.SKUs[0].Label != "红色" || got.Data.SKUs[1].PriceCents != 200 {
		t.Fatalf("读取响应异常: %+v", got)
	}
	if port.userID <= 0 || port.cookieID != "acc1" || port.itemID != "item-1" {
		t.Fatalf("读取没有保留当前用户和路径标识: %+v", port)
	}
	// cache 是响应的 Cache-Control 头。
	if cache := getRecorder.Header().Get("Cache-Control"); !strings.Contains(cache, "no-store") {
		t.Fatalf("库存响应应禁止缓存: %q", cache)
	}

	// putRequest 修改多规格库存。
	putRequest := httptest.NewRequest(http.MethodPut, "/api/v1/items/acc1/item-1/stock", strings.NewReader(`{"skus":[{"key":"sku:a","quantity":6,"expected_quantity":4},{"key":"sku:b","quantity":5,"expected_quantity":5}]}`))
	putRequest.AddCookie(sessionCookie)
	// putRecorder 捕获修改响应。
	putRecorder := httptest.NewRecorder()
	handler.ServeHTTP(putRecorder, putRequest)
	assertOpenAPISuccessResponse(t, putRequest, putRecorder)
	if !strings.Contains(putRecorder.Body.String(), `"cookie_persisted":true`) || len(port.update.SKUs) != 2 || port.update.SKUs[0].Quantity != 6 || port.update.SKUs[0].ExpectedQuantity != 4 {
		t.Fatalf("修改响应或转换异常: body=%s update=%+v", putRecorder.Body.String(), port.update)
	}

	// singleRequest 修改单规格库存，快照为单规格时 skus 必须序列化为空数组。
	port.snapshot = itemapp.StockSnapshot{ItemID: "item-2", Quantity: 3}
	// singleGet 是读取单规格库存的请求。
	singleGet := httptest.NewRequest(http.MethodGet, "/api/v1/items/acc1/item-2/stock", nil)
	singleGet.AddCookie(sessionCookie)
	// singleGetRecorder 捕获单规格读取响应。
	singleGetRecorder := httptest.NewRecorder()
	handler.ServeHTTP(singleGetRecorder, singleGet)
	assertOpenAPISuccessResponse(t, singleGet, singleGetRecorder)
	if !strings.Contains(singleGetRecorder.Body.String(), `"skus":[]`) {
		t.Fatalf("单规格 skus 应为空数组: %s", singleGetRecorder.Body.String())
	}
	// singlePut 是修改单规格库存的请求。
	singlePut := httptest.NewRequest(http.MethodPut, "/api/v1/items/acc1/item-2/stock", strings.NewReader(`{"quantity":8,"expected_quantity":3}`))
	singlePut.AddCookie(sessionCookie)
	// singlePutRecorder 捕获单规格修改响应。
	singlePutRecorder := httptest.NewRecorder()
	handler.ServeHTTP(singlePutRecorder, singlePut)
	assertOpenAPISuccessResponse(t, singlePut, singlePutRecorder)
	if port.update.Quantity != 8 || port.update.ExpectedQuantity != 3 || len(port.update.SKUs) != 0 {
		t.Fatalf("单规格转换异常: %+v", port.update)
	}
}

// TestItemStockHandlerErrorMapping 验证应用层错误映射到正确的状态码和稳定错误码，且错误响应符合 OpenAPI。
func TestItemStockHandlerErrorMapping(t *testing.T) {
	// srv、cleanup 是隔离的 HTTP Server 与清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 在各用例中切换预设错误。
	port := &itemStockPortStub{}
	srv.applications.itemStock = port
	// handler 是完整的 HTTP 路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后的会话 Cookie。
	sessionCookie := loginHelper(t, handler)
	// cases 是应用层错误到 HTTP 状态码与错误码的期望映射。
	cases := []struct {
		// name 是用例名称。
		name string
		// err 是端口预设错误。
		err error
		// status、code 是期望的状态码与错误码（code 为空表示不检查）。
		status int
		code   string
	}{
		{"参数无效", itemapp.ErrStockInvalidInput, http.StatusBadRequest, ""},
		{"凭证变化", itemapp.ErrStockCredentialChanged, http.StatusConflict, "account_credential_changed"},
		{"库存变化", itemapp.ErrStockChanged, http.StatusConflict, "item_stock_changed"},
		{"类目不支持", itemapp.ErrStockEditUnsupported, http.StatusUnprocessableEntity, "item_edit_unsupported"},
		{"客户端不支持", itemapp.ErrStockUnsupported, http.StatusNotImplemented, ""},
		{"风控", itemapp.ErrStockRiskVerification, http.StatusBadGateway, "platform_risk_verification"},
		{"会话失效", itemapp.ErrStockSessionExpired, http.StatusBadGateway, "platform_session_expired"},
		{"其他平台错误", errors.New("平台内部细节 cookie=secret"), http.StatusBadGateway, ""},
		{"会话写回失败", errors.Join(itemapp.ErrStockSessionPersistence, errors.New("db")), http.StatusBadGateway, "platform_stock_updated_cookie_persist_failed"},
	}
	// tc 是当前用例。
	for _, tc := range cases {
		port.err = tc.err
		// requests 是同一错误下的读取与修改两个请求。
		requests := []*http.Request{
			httptest.NewRequest(http.MethodGet, "/api/v1/items/acc1/item-1/stock", nil),
			httptest.NewRequest(http.MethodPut, "/api/v1/items/acc1/item-1/stock", strings.NewReader(`{"quantity":2,"expected_quantity":1}`)),
		}
		// request 是当前请求。
		for _, request := range requests {
			request.AddCookie(sessionCookie)
			// recorder 捕获当前请求的响应。
			recorder := httptest.NewRecorder()
			handler.ServeHTTP(recorder, request)
			// 会话写回失败只会由修改接口产生；读取接口把它当作普通平台错误（502）。
			if tc.code == "platform_stock_updated_cookie_persist_failed" && request.Method == http.MethodGet {
				if recorder.Code != http.StatusBadGateway {
					t.Fatalf("%s: GET status=%d", tc.name, recorder.Code)
				}
				continue
			}
			if recorder.Code != tc.status {
				t.Fatalf("%s: %s status=%d body=%s", tc.name, request.Method, recorder.Code, recorder.Body.String())
			}
			assertOpenAPIResponse(t, request, recorder)
			if tc.code != "" && !strings.Contains(recorder.Body.String(), `"code":"`+tc.code+`"`) {
				t.Fatalf("%s: 缺少错误码 %s: %s", tc.name, tc.code, recorder.Body.String())
			}
			if strings.Contains(recorder.Body.String(), "secret") {
				t.Fatalf("%s: 错误响应泄露了平台内部细节: %s", tc.name, recorder.Body.String())
			}
		}
	}
}

// TestItemStockHandlerRejectsBadRequestsBeforeApplication 验证未登录、越权账号和非法请求体不会触达应用层。
func TestItemStockHandlerRejectsBadRequestsBeforeApplication(t *testing.T) {
	// srv、cleanup 是隔离的 HTTP Server 与清理函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// port 记录是否被调用。
	port := &itemStockPortStub{}
	srv.applications.itemStock = port
	// handler 是完整的 HTTP 路由树。
	handler := srv.Router()
	// sessionCookie 是管理员登录后的会话 Cookie。
	sessionCookie := loginHelper(t, handler)

	// anonymous 是未携带会话的请求。
	anonymous := httptest.NewRequest(http.MethodGet, "/api/v1/items/acc1/item-1/stock", nil)
	// anonymousRecorder 捕获未登录请求的响应。
	anonymousRecorder := httptest.NewRecorder()
	handler.ServeHTTP(anonymousRecorder, anonymous)
	if anonymousRecorder.Code != http.StatusUnauthorized {
		t.Fatalf("未登录 status=%d", anonymousRecorder.Code)
	}
	// foreign 是不属于当前用户的账号。
	foreign := httptest.NewRequest(http.MethodGet, "/api/v1/items/not-mine/item-1/stock", nil)
	foreign.AddCookie(sessionCookie)
	// foreignRecorder 捕获越权账号请求的响应。
	foreignRecorder := httptest.NewRecorder()
	handler.ServeHTTP(foreignRecorder, foreign)
	if foreignRecorder.Code != http.StatusNotFound && foreignRecorder.Code != http.StatusForbidden {
		t.Fatalf("越权账号 status=%d", foreignRecorder.Code)
	}
	// badBody 是非法 JSON 请求体。
	badBody := httptest.NewRequest(http.MethodPut, "/api/v1/items/acc1/item-1/stock", strings.NewReader(`{not json`))
	badBody.AddCookie(sessionCookie)
	// badBodyRecorder 捕获非法请求体的响应。
	badBodyRecorder := httptest.NewRecorder()
	handler.ServeHTTP(badBodyRecorder, badBody)
	if badBodyRecorder.Code != http.StatusBadRequest {
		t.Fatalf("非法请求体 status=%d", badBodyRecorder.Code)
	}
	assertOpenAPIResponse(t, badBody, badBodyRecorder)
	if port.getCalls != 0 || port.updateCalls != 0 {
		t.Fatalf("被拒绝的请求不应触达应用层: %+v", port)
	}
}
