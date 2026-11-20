package server

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
)

// adjustPriceSuccessResp 是平台改价成功的 MTOP 响应：ret 为 SUCCESS 且 data.success 为 true。
var adjustPriceSuccessResp = mtopResp{body: `{"ret":["SUCCESS::调用成功"],"data":{"success":true}}`}

// postAdjustPrice 通过完整 Router 发送改价请求，并对响应做 OpenAPI 契约校验；session 为空表示未登录。
func postAdjustPrice(t *testing.T, handler http.Handler, session *http.Cookie, orderID, body string) *httptest.ResponseRecorder {
	t.Helper()
	// request 是版本化改价请求。
	request := httptest.NewRequest(http.MethodPost, "/api/v1/orders/"+orderID+"/adjust-price", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	if session != nil {
		request.AddCookie(session)
	}
	// recorder 捕获完整响应。
	recorder := httptest.NewRecorder()
	handler.ServeHTTP(recorder, request)
	if recorder.Code >= http.StatusOK && recorder.Code < http.StatusMultipleChoices {
		// 成功响应同时登记 operationId，作为 OpenAPI 成功覆盖门禁的证据。
		assertOpenAPIRecordedSuccessResponse(t, request, recorder)
	} else if recorder.Code != http.StatusUnauthorized {
		assertOpenAPIResponse(t, request, recorder)
	}
	return recorder
}

// readOrderAmountAndStatus 读取订单当前的本地金额与状态；订单不存在或查询失败时让测试失败。
func readOrderAmountAndStatus(t *testing.T, database *sql.DB, orderID string) (amount string, status string) {
	t.Helper()
	if // err 是订单行查询或扫描错误。
	err := database.QueryRowContext(context.Background(), `SELECT COALESCE(amount,''), order_status FROM orders WHERE order_id=?`, orderID).Scan(&amount, &status); err != nil {
		t.Fatalf("读取订单 %s: %v", orderID, err)
	}
	return amount, status
}

// TestAdjustOrderPriceEndToEnd 用真实应用服务、数据库和模拟 MTOP 验证改价成功路径与各状态码映射。
func TestAdjustOrderPriceEndToEnd(t *testing.T) {
	// srv、store、cleanup 分别是测试服务、数据库存储和资源释放函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// row 是订单夹具的 VALUES 元组：未付款（状态 1）、已付款待发货（状态 2）和另一笔未付款订单。
	for _, row := range []string{
		`('adj-pending','item1','b1','1','acc1','chat1')`,
		`('adj-paid','item1','b1','2','acc1','chat1')`,
		`('adj-reject','item1','b1','1','acc1','chat1')`,
	} {
		if // err 是订单夹具写入错误。
		_, err := store.DB.ExecContext(context.Background(), `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id) VALUES `+row); err != nil {
			t.Fatalf("写入订单夹具: %v", err)
		}
	}
	// handler 是完整路由树。
	handler := srv.Router()
	// session 是管理员登录会话。
	session := loginHelper(t, handler)
	// previous 是测试结束时需恢复的原平台客户端。
	previous := testMTop(srv)
	defer func() { setTestMTop(srv, previous) }()

	// 未登录：被认证中间件拦截。
	if unauthenticated := postAdjustPrice(t, handler, nil, "adj-pending", `{"price":"8.80"}`); unauthenticated.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401: %d", unauthenticated.Code)
	}
	// 请求体非 JSON、金额非法：400，且不会触达平台。
	setTestMTop(srv, newMockMTop(t, adjustPriceSuccessResp))
	// malformed 是请求体不是合法 JSON 时的响应。
	malformed := postAdjustPrice(t, handler, session, "adj-pending", `not-json`)
	if malformed.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400: %d", malformed.Code)
	}
	// price 是被拒绝的非法金额 JSON 字面量。
	for _, price := range []string{`""`, `"0"`, `"-3"`, `"1.234"`, `"abc"`} {
		// invalid 是非法金额请求的响应。
		invalid := postAdjustPrice(t, handler, session, "adj-pending", `{"price":`+price+`}`)
		if invalid.Code != http.StatusBadRequest {
			t.Fatalf("price=%s 应 400: %d %s", price, invalid.Code, invalid.Body.String())
		}
	}
	// 订单不存在：404。
	if missing := postAdjustPrice(t, handler, session, "adj-missing", `{"price":"8.80"}`); missing.Code != http.StatusNotFound {
		t.Fatalf("订单不存在应 404: %d", missing.Code)
	}
	// 已付款订单：409。
	if paid := postAdjustPrice(t, handler, session, "adj-paid", `{"price":"8.80"}`); paid.Code != http.StatusConflict {
		t.Fatalf("已付款应 409: %d", paid.Code)
	}
	// 平台拒绝：502，本地金额不变。
	setTestMTop(srv, newMockMTop(t, mtopResp{ret: []string{"FAIL_BIZ_CANNOT_MODIFY_FEE::订单已付款"}}))
	// rejected 是平台拒绝改价时的响应。
	rejected := postAdjustPrice(t, handler, session, "adj-reject", `{"price":"8.80"}`)
	if rejected.Code != http.StatusBadGateway || !strings.Contains(rejected.Body.String(), "订单改价失败") {
		t.Fatalf("平台拒绝应 502: %d %s", rejected.Code, rejected.Body.String())
	}
	// rejectedAmount 是平台拒绝后订单的本地金额，应保持空值。
	rejectedAmount, _ := readOrderAmountAndStatus(t, store.DB, "adj-reject")
	if rejectedAmount != "" {
		t.Fatalf("平台拒绝不得写本地金额: %q", rejectedAmount)
	}

	// 成功：200，响应通过契约校验，本地金额与状态正确。
	setTestMTop(srv, newMockMTop(t, adjustPriceSuccessResp))
	// succeeded 是改价成功的响应。
	succeeded := postAdjustPrice(t, handler, session, "adj-pending", `{"price":"8.8"}`)
	if succeeded.Code != http.StatusOK {
		t.Fatalf("改价应成功: %d %s", succeeded.Code, succeeded.Body.String())
	}
	// response 是解码后的改价响应 DTO。
	var response adjustOrderPriceResponseDTO
	if // err 是响应反序列化错误。
	err := json.Unmarshal(succeeded.Body.Bytes(), &response); err != nil {
		t.Fatalf("解码响应: %v", err)
	}
	if !response.Success || response.OrderID != "adj-pending" || response.Amount != "8.80" || response.Status != "succeeded" {
		t.Fatalf("响应=%+v", response)
	}
	// amount、status 是改价后订单的本地金额与状态。
	amount, status := readOrderAmountAndStatus(t, store.DB, "adj-pending")
	if amount != "8.80" || status != "processing" {
		t.Fatalf("本地金额/状态=%q/%q，应为 8.80/processing", amount, status)
	}
}

// TestAdjustOrderPriceErrorMapping 用可编程订单 Port 覆盖应用层错误到 HTTP 状态码的映射，包括越权、平台未装配和未知错误。
func TestAdjustOrderPriceErrorMapping(t *testing.T) {
	// srv、cleanup 分别是测试服务和资源释放函数。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// handler 是完整路由树。
	handler := srv.Router()
	// session 是管理员登录会话。
	session := loginHelper(t, handler)
	// cases 是应用层错误到 HTTP 状态码的映射用例。
	cases := []struct {
		// name 是用例名称，仅用于失败输出。
		name string
		// err 是订单 Port 返回的应用层错误。
		err error
		// want 是期望的 HTTP 状态码。
		want int
	}{
		{"越权", orderapp.ErrForbidden, http.StatusForbidden},
		{"平台未装配", orderapp.ErrPriceAdjustUnavailable, http.StatusServiceUnavailable},
		{"非法金额", orderapp.ErrInvalidPrice, http.StatusBadRequest},
		{"非未付款", orderapp.ErrOrderNotAwaitingPayment, http.StatusConflict},
		{"平台拒绝", errors.Join(orderapp.ErrPriceAdjustRejected, errors.New("x")), http.StatusBadGateway},
		{"未知错误", errors.New("db down"), http.StatusInternalServerError},
	}
	// tc 是当前待验证的映射用例。
	for _, tc := range cases {
		// returned 固定当前用例的错误，避免闭包捕获循环变量的后续值。
		returned := tc.err
		srv.applications.orders = orderAdapterPortFake{adjustPriceFn: func(_ context.Context, request orderapp.AdjustPriceRequest) (orderapp.AdjustPriceResult, error) {
			if request.OrderID != "o1" || request.Price != "5" || request.UserID == 0 {
				t.Fatalf("请求映射错误: %+v", request)
			}
			return orderapp.AdjustPriceResult{}, returned
		}}
		// recorder 是当前错误映射的响应。
		recorder := postAdjustPrice(t, handler, session, "o1", `{"price":"5"}`)
		if recorder.Code != tc.want {
			t.Fatalf("%s: status=%d want=%d body=%s", tc.name, recorder.Code, tc.want, recorder.Body.String())
		}
		// 未知错误不得把内部错误文本暴露给客户端。
		if tc.want == http.StatusInternalServerError && strings.Contains(recorder.Body.String(), "db down") {
			t.Fatalf("500 不应泄漏内部错误: %s", recorder.Body.String())
		}
	}
}
