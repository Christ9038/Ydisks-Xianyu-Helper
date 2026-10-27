package server

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"xianyu-go/internal/db"
)

// saleFixture 保存售出台账 HTTP 测试的夹具：一个有 3 条库存的 data 卡密组，其中 K1 已售给 order-sale，K2 待核对。
type saleFixture struct {
	// handler 是真实 Router，sessionCookie 是管理员会话。
	handler       http.Handler
	sessionCookie *http.Cookie
	// store 是测试数据库入口；cardID 是夹具卡密组，otherCardID 是其他用户拥有的卡密组。
	store       *db.Store
	cardID      int64
	otherCardID int64
}

// newSaleFixture 构造夹具；cleanup 由调用方延迟执行。
func newSaleFixture(t *testing.T) (saleFixture, func()) {
	t.Helper()
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；srv 是装配了测试依赖的被测 HTTP 服务；store 是隔离的测试数据库。
	srv, store, cleanup := newTestServer(t)
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// admin 是默认管理员，作为卡密组所有者；adminErr 保存读取默认管理员的错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		cleanup()
		t.Fatal(adminErr)
	}
	// cardID 是被测卡密组标识；createErr 保存创建测试数据的错误。
	cardID, createErr := store.Cards.Create(ctx, &db.CardFull{Name: "导出组", Type: "data", UserID: admin.ID, Enabled: true, DataContent: "K1\nK2\nK3"})
	if createErr != nil {
		cleanup()
		t.Fatal(createErr)
	}
	// soldID 是已确认售出的台账记录标识。
	_, soldID, _ := store.Cards.ReserveBatchData(ctx, cardID, db.CardSaleMeta{CookieID: "acc1", OrderID: "order-sale", ItemID: "item-1", BuyerID: "buyer-1"})
	_ = store.Cards.MarkSaleRecordsSold(ctx, soldID)
	_, _, _ = store.Cards.ReserveBatchData(ctx, cardID, db.CardSaleMeta{CookieID: "acc1", OrderID: "order-pending"})
	// insertErr 保存写入测试台账的错误。
	if _, insertErr := store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('order-sale','item-1','buyer-1','shipped','acc1')`); insertErr != nil {
		cleanup()
		t.Fatal(insertErr)
	}
	// userErr 保存创建测试用户的错误。
	if _, userErr := store.Users.Create(ctx, "other", "other@example.com", "other-password"); userErr != nil {
		cleanup()
		t.Fatal(userErr)
	}
	// other 是另一个用户，用于验证数据隔离。
	other, _ := store.Users.GetByUsername(ctx, "other")
	// otherCardID 是另一用户拥有的卡密组标识，用于验证隔离；otherErr 保存创建另一用户测试数据的错误。
	otherCardID, otherErr := store.Cards.Create(ctx, &db.CardFull{Name: "别人的", Type: "data", UserID: other.ID, Enabled: true, DataContent: "X1"})
	if otherErr != nil {
		cleanup()
		t.Fatal(otherErr)
	}
	// handler 是被测 HTTP 处理器。
	handler := srv.Router()
	return saleFixture{handler: handler, sessionCookie: loginHelper(t, handler), store: store, cardID: cardID, otherCardID: otherCardID}, cleanup
}

// get 以管理员会话发送 GET 请求并返回记录器与请求。
func (f saleFixture) get(path string) (*httptest.ResponseRecorder, *http.Request) {
	// request 是发往被测处理器的模拟 HTTP 请求。
	request := httptest.NewRequest(http.MethodGet, path, nil)
	request.AddCookie(f.sessionCookie)
	// recorder 记录被测处理器写出的响应，用于断言状态码、响应头和正文。
	recorder := httptest.NewRecorder()
	f.handler.ServeHTTP(recorder, request)
	return recorder, request
}

// TestCardSaleRecordsEndpoint 验证售出记录分页、筛选、搜索与错误映射，成功响应符合 OpenAPI。
func TestCardSaleRecordsEndpoint(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；fixture 是隔离的测试夹具，包含数据库、任务和卡密组所有者。
	fixture, cleanup := newSaleFixture(t)
	defer cleanup()
	// base 是各用例共用的基准请求路径。
	base := "/api/v1/cards/" + strconv.FormatInt(fixture.cardID, 10) + "/sale-records"

	// recorder 记录被测处理器写出的响应，用于断言状态码、响应头和正文；request 是发往被测处理器的模拟 HTTP 请求。
	recorder, request := fixture.get(base)
	assertOpenAPISuccessResponse(t, request, recorder)
	// list 保存解析出的列表响应。
	var list cardSaleRecordListResponse
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := json.Unmarshal(recorder.Body.Bytes(), &list); err != nil {
		t.Fatal(err)
	}
	if !list.Success || list.Total != 2 || len(list.Data) != 2 || list.TotalPages != 1 || list.Page != 1 {
		t.Fatalf("list=%+v", list)
	}
	if list.Data[1].Content != "K1" || list.Data[1].Status != "sold" || list.Data[1].OrderID != "order-sale" || list.Data[0].Status != "reserved" {
		t.Fatalf("记录内容错误: %+v", list.Data)
	}

	recorder, request = fixture.get(base + "?status=sold&search=order-sale&page=1&page_size=1")
	assertOpenAPISuccessResponse(t, request, recorder)
	_ = json.Unmarshal(recorder.Body.Bytes(), &list)
	if list.Total != 1 || len(list.Data) != 1 || list.PageSize != 1 {
		t.Fatalf("筛选结果错误: %+v", list)
	}

	for /* tc 是各类错误请求及期望状态码 */ _, tc := range []struct {
		path string
		want int
	}{
		{"/api/v1/cards/abc/sale-records", http.StatusBadRequest},
		{base + "?status=bogus", http.StatusBadRequest},
		{"/api/v1/cards/99999/sale-records", http.StatusNotFound},
		{"/api/v1/cards/" + strconv.FormatInt(fixture.otherCardID, 10) + "/sale-records", http.StatusForbidden},
	} {
		// got 保存被测调用的实际返回值，用于与期望值比较。
		if got, _ := fixture.get(tc.path); got.Code != tc.want {
			t.Fatalf("%s status=%d want=%d body=%s", tc.path, got.Code, tc.want, got.Body.String())
		}
	}
	// 未登录必须被拒绝。
	anonymous := httptest.NewRecorder()
	fixture.handler.ServeHTTP(anonymous, httptest.NewRequest(http.MethodGet, base, nil))
	if anonymous.Code != http.StatusUnauthorized {
		t.Fatalf("未登录应 401, got %d", anonymous.Code)
	}
}

// TestCardExportEndpoint 验证未售 TXT 与已售 CSV 导出的内容、响应头和错误映射。
func TestCardExportEndpoint(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；fixture 是隔离的测试夹具，包含数据库、任务和卡密组所有者。
	fixture, cleanup := newSaleFixture(t)
	defer cleanup()
	// base 是各用例共用的基准请求路径。
	base := "/api/v1/cards/" + strconv.FormatInt(fixture.cardID, 10) + "/export"

	// recorder 记录被测处理器写出的响应，用于断言状态码、响应头和正文；request 是发往被测处理器的模拟 HTTP 请求。
	recorder, request := fixture.get(base + "?scope=unsold")
	assertOpenAPISuccessResponse(t, request, recorder)
	if recorder.Body.String() != "K3\n" || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/plain") ||
		recorder.Header().Get("Cache-Control") != "no-store" || !strings.HasPrefix(recorder.Header().Get("Content-Disposition"), "attachment;") ||
		!strings.Contains(recorder.Header().Get("Content-Disposition"), ".txt") {
		t.Fatalf("未售导出错误: headers=%v body=%q", recorder.Header(), recorder.Body.String())
	}

	recorder, request = fixture.get(base + "?scope=sold")
	assertOpenAPISuccessResponse(t, request, recorder)
	// body 保存导出文件或响应的正文。
	body := recorder.Body.String()
	if !strings.HasPrefix(body, "\xEF\xBB\xBF卡密,状态,卡密组") || !strings.Contains(body, "K1,已售,导出组,acc1,order-sale,item-1,buyer-1,") ||
		!strings.Contains(body, "K2,待核对") || !strings.HasPrefix(recorder.Header().Get("Content-Type"), "text/csv") {
		t.Fatalf("已售导出错误: %q", body)
	}

	for /* tc 是各类错误请求及期望状态码 */ _, tc := range []struct {
		path string
		want int
	}{
		{base, http.StatusBadRequest},
		{base + "?scope=all", http.StatusBadRequest},
		{"/api/v1/cards/abc/export?scope=sold", http.StatusBadRequest},
		{"/api/v1/cards/99999/export?scope=sold", http.StatusNotFound},
		{"/api/v1/cards/" + strconv.FormatInt(fixture.otherCardID, 10) + "/export?scope=unsold", http.StatusForbidden},
	} {
		// got 保存被测调用的实际返回值，用于与期望值比较。
		if got, _ := fixture.get(tc.path); got.Code != tc.want {
			t.Fatalf("%s status=%d want=%d body=%s", tc.path, got.Code, tc.want, got.Body.String())
		}
	}
	// 非 data 类型不支持未售导出。
	textID, _ := fixture.store.Cards.Create(context.Background(), &db.CardFull{Name: "文本", Type: "text", UserID: 1, Enabled: true, TextContent: "x"})
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got, _ := fixture.get("/api/v1/cards/" + strconv.FormatInt(textID, 10) + "/export?scope=unsold"); got.Code != http.StatusBadRequest {
		t.Fatalf("非 data 类型未售导出应 400, got %d", got.Code)
	}
}

// TestOrderDeliveredCardsEndpoint 验证订单已发卡密查询、无记录订单、越权与不存在订单。
func TestOrderDeliveredCardsEndpoint(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；fixture 是隔离的测试夹具，包含数据库、任务和卡密组所有者。
	fixture, cleanup := newSaleFixture(t)
	defer cleanup()

	// recorder 记录被测处理器写出的响应，用于断言状态码、响应头和正文；request 是发往被测处理器的模拟 HTTP 请求。
	recorder, request := fixture.get("/api/v1/orders/order-sale/delivered-cards")
	assertOpenAPISuccessResponse(t, request, recorder)
	// result 保存被测调用的返回结果。
	var result orderDeliveredCardsResponse
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := json.Unmarshal(recorder.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}
	if !result.Success || len(result.Data) != 1 || result.Data[0].Content != "K1" || result.Data[0].Status != "sold" || recorder.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("result=%+v headers=%v", result, recorder.Header())
	}

	// 升级前或未自动发货的订单没有记录，应返回空数组而不是 null。
	if _, err := fixture.store.DB.ExecContext(context.Background(), `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('order-old','i','b','shipped','acc1')`); err != nil {
		t.Fatal(err)
	}
	recorder, request = fixture.get("/api/v1/orders/order-old/delivered-cards")
	assertOpenAPISuccessResponse(t, request, recorder)
	if !strings.Contains(recorder.Body.String(), `"data":[]`) {
		t.Fatalf("无记录应返回空数组: %s", recorder.Body.String())
	}

	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got, _ := fixture.get("/api/v1/orders/missing/delivered-cards"); got.Code != http.StatusNotFound {
		t.Fatalf("不存在订单应 404, got %d", got.Code)
	}
	// 其他用户账号下的订单不得读取。
	other, _ := fixture.store.Users.GetByUsername(context.Background(), "other")
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := fixture.store.DB.ExecContext(context.Background(), `INSERT INTO cookies (id, value, user_id) VALUES ('acc-other','v',?)`, other.ID); err != nil {
		t.Fatal(err)
	}
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if _, err := fixture.store.DB.ExecContext(context.Background(), `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('order-other','i','b','shipped','acc-other')`); err != nil {
		t.Fatal(err)
	}
	// got 保存被测调用的实际返回值，用于与期望值比较。
	if got, _ := fixture.get("/api/v1/orders/order-other/delivered-cards"); got.Code != http.StatusForbidden {
		t.Fatalf("越权订单应 403, got %d", got.Code)
	}
}

// TestCardListIncludesSoldCount 验证卡密列表和详情返回已售数量。
func TestCardListIncludesSoldCount(t *testing.T) {
	// cleanup 是释放隔离测试数据库的清理函数，由测试在结束时调用；fixture 是隔离的测试夹具，包含数据库、任务和卡密组所有者。
	fixture, cleanup := newSaleFixture(t)
	defer cleanup()
	// recorder 记录被测处理器写出的响应，用于断言状态码、响应头和正文；request 是发往被测处理器的模拟 HTTP 请求。
	recorder, request := fixture.get("/api/v1/cards/" + strconv.FormatInt(fixture.cardID, 10))
	assertOpenAPISuccessResponse(t, request, recorder)
	// card 保存被测卡密组数据。
	var card cardResponse
	// err 保存被测调用返回的错误，用于断言成功或期望的失败。
	if err := json.Unmarshal(recorder.Body.Bytes(), &card); err != nil {
		t.Fatal(err)
	}
	if card.SoldCount != 1 {
		t.Fatalf("详情已售数应为 1(仅 sold), got %d", card.SoldCount)
	}
	recorder, request = fixture.get("/api/v1/cards")
	assertOpenAPISuccessResponse(t, request, recorder)
	if !strings.Contains(recorder.Body.String(), `"sold_count":1`) {
		t.Fatalf("列表应包含 sold_count: %s", recorder.Body.String())
	}
}
