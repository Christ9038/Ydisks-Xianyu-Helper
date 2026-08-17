package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// waitOrderRefreshJob 等待订单刷新后台任务完成并返回具名刷新结果。
func waitOrderRefreshJob(t *testing.T, handler http.Handler, cookie *http.Cookie, jobID string) orderRefreshResponse {
	t.Helper()
	// deadline 保存测试任务允许的最长等待时间。
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		// req 保存当前任务状态查询请求。
		req := httptest.NewRequest(http.MethodGet, "/api/orders/refresh/"+jobID, nil)
		req.AddCookie(cookie)
		// rec 保存当前任务状态响应。
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("refresh job status=%d body=%s", rec.Code, rec.Body.String())
		}
		// status 保存任务状态响应。
		var status orderRefreshJobStatusResponse
		// err 表示任务状态 JSON 解析错误。
		if err := json.Unmarshal(rec.Body.Bytes(), &status); err != nil {
			t.Fatalf("decode refresh job status: %v", err)
		}
		if status.Status == "succeeded" {
			if status.Result == nil {
				t.Fatalf("succeeded refresh job has no result: %+v", status)
			}
			return *status.Result
		}
		if status.Status == "failed" {
			t.Fatalf("refresh job failed: %+v", status)
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("refresh job %s did not complete", jobID)
	return orderRefreshResponse{}
}

// TestCancelOrderRefreshJob 验证用户可取消排队任务且任务终态不会被旧 worker 覆盖。
func TestCancelOrderRefreshJob(t *testing.T) {
	// srv、store、cleanup 保存测试服务、数据库和清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// handler 保存订单刷新路由处理器。
	handler := srv.Router()
	// cookie 保存管理员会话 Cookie。
	cookie := loginHelper(t, handler)
	// admin、err 保存任务所属用户及查询错误。
	admin, err := store.Users.GetByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("get admin: %v", err)
	}
	// job 保存待取消的排队任务。
	job := &db.OrderRefreshJob{ID: "cancel-http-job", UserID: admin.ID, Status: "queued"}
	// err 保存任务创建错误。
	if err := store.OrderRefreshJobs.Create(context.Background(), job); err != nil {
		t.Fatalf("create refresh job: %v", err)
	}
	// req、rec 保存取消请求及响应记录器。
	req := httptest.NewRequest(http.MethodDelete, "/api/orders/refresh/"+job.ID, nil)
	req.AddCookie(cookie)
	// rec 保存取消请求响应记录器。
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("cancel status=%d body=%s", rec.Code, rec.Body.String())
	}
	// cancelled、err 保存取消后任务及查询错误。
	cancelled, err := store.OrderRefreshJobs.Get(context.Background(), admin.ID, job.ID)
	if err != nil || cancelled.Status != "cancelled" {
		t.Fatalf("cancelled job=%+v err=%v", cancelled, err)
	}
	// repeatRec 保存重复取消请求的响应记录器。
	repeatReq := httptest.NewRequest(http.MethodDelete, "/api/orders/refresh/"+job.ID, nil)
	repeatReq.AddCookie(cookie)
	// repeatRec 保存重复取消响应记录器。
	repeatRec := httptest.NewRecorder()
	handler.ServeHTTP(repeatRec, repeatReq)
	if repeatRec.Code != http.StatusOK {
		t.Fatalf("repeat cancel status=%d body=%s", repeatRec.Code, repeatRec.Body.String())
	}
	// versionedJob 保存用于验证版本化路由的任务。
	versionedJob := &db.OrderRefreshJob{ID: "cancel-versioned-job", UserID: admin.ID, Status: "queued"}
	// err 保存版本化任务创建错误。
	if err := store.OrderRefreshJobs.Create(context.Background(), versionedJob); err != nil {
		t.Fatalf("create versioned cancel job: %v", err)
	}
	// versionedReq、versionedRec 保存版本化取消请求及响应记录器。
	versionedReq := httptest.NewRequest(http.MethodDelete, "/api/v1/orders/refresh/"+versionedJob.ID, nil)
	versionedReq.AddCookie(cookie)
	// versionedRec 保存版本化取消响应记录器。
	versionedRec := httptest.NewRecorder()
	handler.ServeHTTP(versionedRec, versionedReq)
	if versionedRec.Code != http.StatusOK {
		t.Fatalf("versioned cancel status=%d body=%s", versionedRec.Code, versionedRec.Body.String())
	}
}

// TestRefreshOrdersNoBrowser 浏览器未启用时仍应完成订单列表发现。
func TestRefreshOrdersNoBrowser(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/refresh", strings.NewReader(""))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("无浏览器仍应同步列表，got %d body=%s", rec.Code, rec.Body.String())
	}
	// start 保存后台任务创建响应。
	var start orderRefreshJobStartResponse
	// err 表示任务创建响应 JSON 解析错误。
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	// result 保存完成后的订单刷新结果。
	result := waitOrderRefreshJob(t, h, cookie, start.JobID)
	if !strings.Contains(result.Message, "订单列表同步完成") {
		t.Fatalf("refresh result=%+v", result)
	}
}

// TestRefreshOrdersDiscoversNewOrdersWithoutBrowser 负责TestRefresh订单列表DiscoversNew订单列表Without浏览器相关处理。
func TestRefreshOrdersDiscoversNewOrdersWithoutBrowser(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Query().Get("api"), "order.detail") {
			// body 保存请求体，供当前处理流程使用
			body := `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"待发货"},"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"2"},"priceInfo":{"amount":{"value":"19.90"}}}}]}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}
		// body 保存请求体，供当前处理流程使用
		body := `{"ret":["SUCCESS::调用成功"],"data":{"module":{"nextPage":"false","totalCount":"1","items":[{` +
			`"commonData":{"orderId":"sold-new-1","itemId":"item-new","orderStatus":"待发货","inRefund":"false"},` +
			`"buyerInfoVO":{"buyerId":"buyer-1","name":"张三","phone":"13800000000","address":"上海市"},` +
			`"priceVO":{"totalPrice":"19.90","buyNum":"2"},` +
			`"rightVO":{"btnList":[{"tradeAction":"SKIP_PIN"}]}}]}}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}))
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/refresh", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// start 保存后台任务创建响应。
	var start orderRefreshJobStartResponse
	// err 表示任务创建响应 JSON 解析错误。
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	// result 保存完成后的订单刷新结果。
	result := waitOrderRefreshJob(t, h, cookie, start.JobID)
	if result.Summary.Discovered != 1 {
		t.Fatalf("refresh result=%+v", result)
	}
	// order、err 保存order、err，供当前处理流程使用
	order, err := store.Orders.Get(context.Background(), "sold-new-1")
	if err != nil {
		t.Fatal(err)
	}
	if order.CookieID != "acc1" || order.ItemID != "item-new" || order.OrderStatus != "pending_ship" ||
		order.Amount != "19.90" || order.Quantity != "2" || order.IsBargain != 1 || order.ReceiverName != "张三" {
		t.Fatalf("discovered order=%+v", order)
	}
}

// TestRefreshOrdersReleasesCredentialLockDuringDiscovery 验证订单发现远端请求期间不会占用账号凭证锁。
func TestRefreshOrdersReleasesCredentialLockDuringDiscovery(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// started 表示订单发现请求已经进入阻塞点。
	started := make(chan struct{})
	// release 允许测试释放阻塞的订单发现请求。
	release := make(chan struct{})
	// once 保证 started 只关闭一次。
	var once sync.Once
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		once.Do(func() { close(started) })
		<-release
		// body 是空订单列表的成功响应。
		body := `{"ret":["SUCCESS::调用成功"],"data":{"module":{"nextPage":"false","totalCount":"0","items":[]}}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}))
	// h 保存 HTTP 路由处理器。
	h := srv.Router()
	// cookie 保存登录凭证。
	cookie := loginHelper(t, h)
	// requestDone 表示订单刷新请求已经返回。
	requestDone := make(chan struct{})
	go func() {
		defer close(requestDone)
		// req 是触发订单刷新的测试请求。
		req := httptest.NewRequest(http.MethodPost, "/api/orders/refresh", nil)
		req.AddCookie(cookie)
		// rec 保存订单刷新请求的测试响应。
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
	}()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("订单发现请求未进入阻塞点")
	}
	// lockAcquired 表示另一个操作已成功取得同账号凭证锁。
	lockAcquired := make(chan struct{})
	go func() {
		// unlock 释放测试 goroutine 取得的账号凭证锁。
		unlock := store.LockAccountCredentials("acc1")
		close(lockAcquired)
		unlock()
	}()
	select {
	case <-lockAcquired:
	case <-time.After(time.Second):
		t.Fatal("订单发现期间凭证锁仍被占用")
	}
	close(release)
	select {
	case <-requestDone:
	case <-time.After(3 * time.Second):
		t.Fatal("订单刷新请求未收束")
	}
}

// TestRefreshOrdersSoftDeletesOrdersMissingFromSellerList 负责TestRefresh订单列表SoftDeletes订单列表MissingFromSellerList相关处理。
func TestRefreshOrdersSoftDeletesOrdersMissingFromSellerList(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,item_id,buyer_id,cookie_id,order_status) VALUES ('buyer-order','buy-item','seller-account','acc1','pending_ship')`)
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Query().Get("api"), "order.detail") {
			// body 保存请求体，供当前处理流程使用
			body := `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"待发货"},"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"1"},"priceInfo":{"amount":{"value":"10.00"}}}}]}}`
			return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
		}
		// body 保存请求体，供当前处理流程使用
		body := `{"ret":["SUCCESS::调用成功"],"data":{"module":{"nextPage":"false","totalCount":"1","items":[{` +
			`"commonData":{"orderId":"seller-order","itemId":"seller-item","orderStatus":"待发货"},` +
			`"buyerInfoVO":{"buyerId":"buyer-1"},"priceVO":{"totalPrice":"10.00","buyNum":"1"}}]}}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}))
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/refresh", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// start 保存后台任务创建响应。
	var start orderRefreshJobStartResponse
	// err 表示任务创建响应 JSON 解析错误。
	if err := json.Unmarshal(rec.Body.Bytes(), &start); err != nil {
		t.Fatal(err)
	}
	// result 保存完成后的订单刷新结果。
	result := waitOrderRefreshJob(t, h, cookie, start.JobID)
	if result.Summary.SoftDeleted != 1 {
		t.Fatalf("refresh result=%+v", result)
	}
	// deletedAt 保存deletedAt，供当前处理流程使用
	var deletedAt string
	if // err 保存err，供当前处理流程使用
	err := store.DB.QueryRowContext(ctx, `SELECT COALESCE(deleted_at,'') FROM orders WHERE order_id=?`, "buyer-order").Scan(&deletedAt); err != nil || deletedAt == "" {
		t.Fatalf("缺失订单应逻辑删除，deleted_at=%q err=%v", deletedAt, err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := store.Orders.Get(ctx, "buyer-order"); err == nil {
		t.Fatal("逻辑删除的购买订单不应再出现在活动订单查询中")
	}
}

// TestMissingRefreshResultsAreCounted 负责TestMissingRefreshResultsAreCounted相关处理。
func TestMissingRefreshResultsAreCounted(t *testing.T) {
	// targets 保存targets，供当前处理流程使用
	targets := []refreshTarget{{OrderID: "a"}, {OrderID: "b"}, {OrderID: "c"}}
	// missing 保存missing，供当前处理流程使用
	missing := missingRefreshTargetIDs(targets, map[string]struct{}{"b": {}})
	if len(missing) != 2 || missing[0] != "a" || missing[1] != "c" {
		t.Fatalf("missing=%v", missing)
	}
}

// TestRefreshSingleOrderUsesGoMTop 单订单刷新不依赖浏览器。
func TestRefreshSingleOrderUsesGoMTop(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	srv.MTop = withMTopTransport(roundTripFunc(func(req *http.Request) (*http.Response, error) {
		// body 保存请求体，供当前处理流程使用
		body := `{"ret":["SUCCESS::调用成功"],"data":{"utArgs":{"orderStatus":"待发货"},"components":[{"render":"orderInfoVO","data":{"itemInfo":{"buyAmount":"2","specName":"套餐","specValue":"30天"},"priceInfo":{"amount":{"value":"19.90"}}}}]}}`
		return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: io.NopCloser(strings.NewReader(body)), Request: req}, nil
	}))
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, cookie_id, order_status) VALUES ('ord-x','item1','acc1','2')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/ord-x/refresh", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("Go MTOP 刷新应成功，got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestRefreshSingleOrderNotFound 单订单刷新不存在订单 404。
func TestRefreshSingleOrderNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// 注入非 nil Browser 但内部 playwright 不可用，订单查询先行 404。
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/no-such/refresh", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	// Browser==nil → 503；先校验此路径不 panic。
	if rec.Code != http.StatusServiceUnavailable && rec.Code != http.StatusNotFound {
		t.Fatalf("expected 503/404, got %d body=%s", rec.Code, rec.Body.String())
	}
}

// TestUpdateOrder 更新订单字段（status 归一）。
func TestUpdateOrder(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id) VALUES ('ord-u','item1','b1','2','acc1')`)
	store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('acc1','item1','旧标题')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"status":"shipped","receiver_name":"张三","receiver_phone":"13800000000","receiver_address":"北京","item_title":"新标题"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/api/orders/ord-u", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("update status=%d body=%s", rec.Code, rec.Body.String())
	}

	// 验证已写入。
	req2 := httptest.NewRequest(http.MethodGet, "/api/orders/ord-u", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	// got 保存got，供当前处理流程使用
	var got map[string]any
	json.Unmarshal(rec2.Body.Bytes(), &got)
	if got["order_status"] != "shipped" || got["receiver_name"] != "张三" {
		t.Fatalf("更新未生效: %+v", got)
	}
	// item、err 保存item、err，供当前处理流程使用
	item, err := store.Items.Get(ctx, "acc1", "item1")
	if err != nil || item.ItemTitle != "新标题" {
		t.Fatalf("商品标题未保存: item=%+v err=%v", item, err)
	}
}

// TestUpdateOrderUsesNewItemIDForTitle 负责TestUpdate订单UsesNew商品IDFor标题相关处理。
func TestUpdateOrderUsesNewItemIDForTitle(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,item_id,order_status,cookie_id) VALUES ('ord-new-item','old-item','2','acc1')`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('acc1','old-item','旧商品')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/api/orders/ord-new-item", strings.NewReader(`{"item_id":" new-item ","item_title":"新商品"}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// order 保存订单，供当前处理流程使用
	order, _ := store.Orders.Get(ctx, "ord-new-item")
	if order.ItemID != "new-item" {
		t.Fatalf("order item_id=%q", order.ItemID)
	}
	// newItem、err 保存newItem、err，供当前处理流程使用
	newItem, err := store.Items.Get(ctx, "acc1", "new-item")
	if err != nil || newItem.ItemTitle != "新商品" {
		t.Fatalf("new item=%+v err=%v", newItem, err)
	}
	// oldItem 保存old商品，供当前处理流程使用
	oldItem, _ := store.Items.Get(ctx, "acc1", "old-item")
	if oldItem.ItemTitle != "旧商品" {
		t.Fatalf("old item title changed: %+v", oldItem)
	}
}

// TestImportOrdersRejectsInvalidAmountWithoutWritingOrder 负责TestImport订单列表RejectsInvalidAmountWithoutWriting订单相关处理。
func TestImportOrdersRejectsInvalidAmountWithoutWritingOrder(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/import", strings.NewReader(
		`[{"order_id":"bad-import-amount","cookie_id":"acc1","amount":"1e3"}]`,
	))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "普通格式") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if // err 保存err，供当前处理流程使用
	_, err := store.Orders.Get(context.Background(), "bad-import-amount"); err == nil {
		t.Fatal("invalid imported amount must not create an order")
	}
}

// TestImportOrdersRejectsUnknownStatusWithoutWritingOrder 负责TestImport订单列表RejectsUnknown状态WithoutWriting订单相关处理。
func TestImportOrdersRejectsUnknownStatusWithoutWritingOrder(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/import", strings.NewReader(
		`[{"order_id":"bad-import-status","cookie_id":"acc1","status":"anything","amount":"10"}]`,
	))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "不支持的订单状态") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if // err 保存err，供当前处理流程使用
	_, err := store.Orders.Get(context.Background(), "bad-import-status"); err == nil {
		t.Fatal("unknown imported status must not create an order")
	}
}

// TestImportOrdersRollsBackOrderWhenItemWriteFails 负责TestImport订单列表RollsBack订单When商品WriteFails相关处理。
func TestImportOrdersRollsBackOrderWhenItemWriteFails(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `CREATE TRIGGER reject_import_item BEFORE INSERT ON item_info
		WHEN NEW.item_id='reject-import-item' BEGIN SELECT RAISE(ABORT,'forced item failure'); END`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/import", strings.NewReader(
		`[{"order_id":"import-tx","cookie_id":"acc1","item_id":"reject-import-item","item_title":"商品","amount":"¥1,200.50"}]`,
	))
	req.Header.Set("Content-Type", "application/json")
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "补全商品信息失败") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if // err 保存err，供当前处理流程使用
	_, err := store.Orders.Get(ctx, "import-tx"); err == nil {
		t.Fatal("order must roll back when imported item write fails")
	}
}

// TestUpdateOrderRejectsInvalidAmount 负责TestUpdate订单RejectsInvalidAmount相关处理。
func TestUpdateOrderRejectsInvalidAmount(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,item_id,amount,order_status,cookie_id) VALUES ('ord-amount','item1','9.9','2','acc1')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/api/orders/ord-amount", strings.NewReader(`{"amount":"abc"}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// order 保存订单，供当前处理流程使用
	order, _ := store.Orders.Get(ctx, "ord-amount")
	if order.Amount != "9.9" {
		t.Fatalf("invalid amount was stored: %q", order.Amount)
	}
}

// TestValidOrderAmountMatchesAnalyticsDecimalFormat 负责Test有效订单AmountMatchesAnalyticsDecimalFormat相关处理。
func TestValidOrderAmountMatchesAnalyticsDecimalFormat(t *testing.T) {
	// invalid 表示当前遍历过程中的invalid
	for _, invalid := range []string{"abc", "1e3", "+Inf", "NaN", "-1", "1.", "1,2", "12,34", "1,,000"} {
		if validOrderAmount(invalid) {
			t.Fatalf("%q must be rejected", invalid)
		}
	}
	// input、want 表示当前遍历过程中的input、want
	for input, want := range map[string]string{"": "", "0": "0", "12.50": "12.50", "¥1,200.50": "1200.50", "￥12.50": "12.50"} {
		// got、ok 保存got、ok，供当前处理流程使用
		got, ok := normalizeOrderAmount(input)
		if !ok || got != want {
			t.Fatalf("normalize(%q)=%q,%v want %q,true", input, got, ok, want)
		}
	}
}

// TestChunkRefreshTargetsBoundsBrowserBatchSize 负责TestChunkRefreshTargetsBounds浏览器批次数量相关处理。
func TestChunkRefreshTargetsBoundsBrowserBatchSize(t *testing.T) {
	// targets 保存targets，供当前处理流程使用
	targets := make([]refreshTarget, 205)
	// i 表示当前遍历过程中的i
	for i := range targets {
		targets[i].OrderID = fmt.Sprintf("o-%d", i)
	}
	// chunks 保存chunks，供当前处理流程使用
	chunks := chunkRefreshTargets(targets, 100)
	if len(chunks) != 3 || len(chunks[0]) != 100 || len(chunks[1]) != 100 || len(chunks[2]) != 5 {
		t.Fatalf("unexpected chunk sizes: %d/%d/%d total=%d", len(chunks[0]), len(chunks[1]), len(chunks[2]), len(chunks))
	}
}

// TestUpdateOrderAndItemTitleRollBackTogether 负责TestUpdate订单And商品标题RollBackTogether相关处理。
func TestUpdateOrderAndItemTitleRollBackTogether(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,item_id,amount,order_status,cookie_id) VALUES ('ord-tx','old-item','9.9','2','acc1')`)
	_, _ = store.DB.ExecContext(ctx, `CREATE TRIGGER reject_tx_item BEFORE INSERT ON item_info
		WHEN NEW.item_id='tx-fail' BEGIN SELECT RAISE(ABORT,'forced item failure'); END`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/api/orders/ord-tx", strings.NewReader(`{"item_id":"tx-fail","item_title":"new","amount":"20"}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// order 保存订单，供当前处理流程使用
	order, _ := store.Orders.Get(ctx, "ord-tx")
	if order.ItemID != "old-item" || order.Amount != "9.9" {
		t.Fatalf("order must roll back with item failure: %+v", order)
	}
}

// TestUpdateOrderRejectsUnknownStatus 负责TestUpdate订单RejectsUnknown状态相关处理。
func TestUpdateOrderRejectsUnknownStatus(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,item_id,order_status,cookie_id) VALUES ('ord-status','item1','2','acc1')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/api/orders/ord-status", strings.NewReader(`{"status":"anything"}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// order 保存订单，供当前处理流程使用
	order, _ := store.Orders.Get(ctx, "ord-status")
	if order.OrderStatus != "2" {
		t.Fatalf("invalid status changed order: %+v", order)
	}
}

// TestUpdateOrderCanExplicitlyClearFields 负责TestUpdate订单CanExplicitlyClear字段列表相关处理。
func TestUpdateOrderCanExplicitlyClearFields(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders
		(order_id,item_id,buyer_id,order_status,cookie_id,amount,receiver_phone)
		VALUES ('ord-clear','item1','b1','2','acc1','99.9','13800000000')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/api/orders/ord-clear", strings.NewReader(`{"amount":"","receiver_phone":""}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", rec.Code, rec.Body.String())
	}
	// order、err 保存order、err，供当前处理流程使用
	order, err := store.Orders.Get(ctx, "ord-clear")
	if err != nil {
		t.Fatal(err)
	}
	if order.Amount != "" || order.ReceiverPhone != "" || order.ItemID != "item1" {
		t.Fatalf("explicit clear mismatch: %+v", order)
	}
}

// TestListOrdersSearchUsesBackendPaginationScope 负责TestList订单列表搜索UsesBackendPaginationScope相关处理。
func TestListOrdersSearchUsesBackendPaginationScope(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,item_id,buyer_id,order_status,cookie_id) VALUES
		('ord-search-1','item-search','buyer-a','2','acc1'),
		('ord-search-2','item-other','buyer-b','2','acc1')`)
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO item_info (cookie_id,item_id,item_title) VALUES ('acc1','item-search','Unique Product Name')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodGet, "/api/orders?search=unique&page=1&page_size=1", nil)
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("search status=%d body=%s", rec.Code, rec.Body.String())
	}
	// response 保存响应，供当前处理流程使用
	var response struct {
		Total int              `json:"total"`
		Data  []map[string]any `json:"data"`
	}
	if // err 保存err，供当前处理流程使用
	err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response.Total != 1 || len(response.Data) != 1 || response.Data[0]["order_id"] != "ord-search-1" {
		t.Fatalf("backend search response=%+v", response)
	}
}

// TestUpdateOrderBadJSON 非法 JSON 应 400。
func TestUpdateOrderBadJSON(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, cookie_id) VALUES ('ord-bad','item1','acc1')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPut, "/api/orders/ord-bad", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestManualShipOrdersStatusOnlySuccess mtop ConsignContext 成功路径。
func TestManualShipOrdersStatusOnlySuccess(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id) VALUES ('ord-m','item1','b1','2','acc1','chat1')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"order_ids":["ord-m"],"ship_mode":"status_only"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("manual ship status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["partial_failure"] != false {
		t.Fatalf("应成功: %+v", res)
	}
	// results 保存results，供当前处理流程使用
	results, _ := res["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("应1条结果，got %d", len(results))
	}
	// 订单状态应已变为 shipped。
	var ord map[string]any
	// req2 保存req2，供当前处理流程使用
	req2 := httptest.NewRequest(http.MethodGet, "/api/orders/ord-m", nil)
	req2.AddCookie(cookie)
	// rec2 保存rec2，供当前处理流程使用
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	json.Unmarshal(rec2.Body.Bytes(), &ord)
	if ord["order_status"] != "shipped" {
		t.Errorf("订单状态应为 shipped，got %v", ord["order_status"])
	}
}

// TestManualShipOrdersRejectsNonPendingStatus 负责TestManualShip订单列表RejectsNonPending状态相关处理。
func TestManualShipOrdersRejectsNonPendingStatus(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	_, _ = store.DB.ExecContext(ctx, `INSERT INTO orders (order_id,item_id,buyer_id,order_status,cookie_id,chat_id) VALUES ('ord-cancelled','item1','b1','cancelled','acc1','chat1')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(`{"order_ids":["ord-cancelled"],"ship_mode":"status_only"}`))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK || !strings.Contains(rec.Body.String(), "仅待发货订单") {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// order 保存订单，供当前处理流程使用
	order, _ := store.Orders.Get(ctx, "ord-cancelled")
	if order.OrderStatus != "cancelled" {
		t.Fatalf("status changed: %+v", order)
	}
}

// TestManualShipOrdersConsignFail mtop ConsignContext 失败（非 success ret）。
func TestManualShipOrdersConsignFail(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id) VALUES ('ord-f','item1','b1','2','acc1','chat1')`)

	// 覆盖 mtop client：返回非 success ret。
	prev := srv.MTop
	srv.MTop = newMockMTop(t, mtopResp{ret: []string{"FAIL_BIZ_ORDER_NOT_FOUND::订单不存在"}})
	defer func() { srv.MTop = prev }()

	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)
	// body 保存请求体，供当前处理流程使用
	body := `{"order_ids":["ord-f"],"ship_mode":"status_only"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["partial_failure"] != true {
		t.Fatalf("整体应失败: %+v", res)
	}
}

// TestManualShipOrdersOrderNotFound 订单不存在 → failed。
func TestManualShipOrdersOrderNotFound(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"order_ids":["no-such-ord"],"ship_mode":"status_only"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d", rec.Code)
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["failed_count"] != float64(1) {
		t.Fatalf("应1失败，got %+v", res)
	}
}

// TestManualShipOrdersBadMode 非法发货模式 400。
func TestManualShipOrdersBadMode(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"order_ids":["ord-x"],"ship_mode":"bogus"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法模式应 400，got %d", rec.Code)
	}
}

// TestManualShipOrdersEmpty 缺少订单 ID 400。
func TestManualShipOrdersEmpty(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"order_ids":[],"ship_mode":"status_only"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("空订单应 400，got %d", rec.Code)
	}
}

// TestManualShipOrdersBadJSON 非法 JSON 400。
func TestManualShipOrdersBadJSON(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader("not-json"))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("非法 JSON 应 400，got %d", rec.Code)
	}
}

// TestManualShipOrdersFullDeliveryNoAutomation full_delivery 但自动化未初始化 → failed。
func TestManualShipOrdersFullDeliveryNoAutomation(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	store.DB.ExecContext(ctx, `INSERT INTO orders (order_id, item_id, buyer_id, order_status, cookie_id, chat_id) VALUES ('ord-full','item1','b1','2','acc1','chat1')`)
	// h 保存h，供当前处理流程使用
	h := srv.Router()
	// cookie 保存登录凭证，供当前处理流程使用
	cookie := loginHelper(t, h)

	// body 保存请求体，供当前处理流程使用
	body := `{"order_ids":["ord-full"],"ship_mode":"full_delivery"}`
	// req 保存req，供当前处理流程使用
	req := httptest.NewRequest(http.MethodPost, "/api/orders/manual-ship", strings.NewReader(body))
	req.AddCookie(cookie)
	// rec 保存rec，供当前处理流程使用
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != 200 {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	// res 保存响应，供当前处理流程使用
	var res map[string]any
	json.Unmarshal(rec.Body.Bytes(), &res)
	if res["failed_count"] != float64(1) {
		t.Fatalf("应1失败，got %+v", res)
	}
	// results 保存results，供当前处理流程使用
	results, _ := res["results"].([]any)
	if len(results) != 1 {
		t.Fatalf("结果数异常: %d", len(results))
	}
	// r0 保存r0，供当前处理流程使用
	r0, _ := results[0].(map[string]any)
	if !strings.Contains(r0["message"].(string), "自动化") {
		t.Fatalf("应提示自动化未初始化，got %v", r0["message"])
	}
}

// TestIsStableOrderStatus 稳定状态判定。
func TestIsStableOrderStatus(t *testing.T) {
	// stable 保存stable，供当前处理流程使用
	stable := map[string]bool{"shipped": true, "completed": true, "cancelled": true}
	// unstable 保存unstable，供当前处理流程使用
	unstable := map[string]bool{"pending_ship": false, "processing": false, "": false, "unknown": false}
	// s、want 表示当前遍历过程中的s、want
	for s, want := range stable {
		if // got 保存got，供当前处理流程使用
		got := isStableOrderStatus(s); got != want {
			t.Errorf("isStableOrderStatus(%q)=%v want %v", s, got, want)
		}
	}
	// s、want 表示当前遍历过程中的s、want
	for s, want := range unstable {
		if // got 保存got，供当前处理流程使用
		got := isStableOrderStatus(s); got != want {
			t.Errorf("isStableOrderStatus(%q)=%v want %v", s, got, want)
		}
	}
}

// TestOrderApplicationServiceUsesTypedBusinessInputs 验证订单应用服务可脱离 HTTP 适配直接执行核心用例。
func TestOrderApplicationServiceUsesTypedBusinessInputs(t *testing.T) {
	// srv、store、cleanup 保存srv、store、cleanup，供当前处理流程使用
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// service 保存service，供当前处理流程使用
	service := srv.orders()
	// list、err 保存list、err，供当前处理流程使用
	list, err := service.List(ctx, orderListQuery{UserID: 1, Page: 0, PageSize: 999})
	if err != nil {
		t.Fatalf("订单服务列表查询失败: %v", err)
	}
	if list.Page != 1 || list.PageSize != 200 {
		t.Fatalf("订单服务分页规范化异常: %+v", list)
	}
	if // err 保存err，供当前处理流程使用
	_, err := service.Import(ctx, 1, []map[string]any{{"order_id": "service-order", "cookie_id": "acc1", "status": "pending_ship", "amount": "12.50"}}); err != nil {
		t.Fatalf("订单服务导入失败: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := service.Update(ctx, 1, "service-order", orderUpdateRequest{Amount: stringPtrForOrderTest("13.00")}); err != nil {
		t.Fatalf("订单服务更新失败: %v", err)
	}
	// order、err 保存order、err，供当前处理流程使用
	order, err := store.Orders.Get(ctx, "service-order")
	if err != nil || order.Amount != "13.00" {
		t.Fatalf("订单服务更新结果异常: order=%+v err=%v", order, err)
	}
	if // err 保存err，供当前处理流程使用
	err := service.Delete(ctx, 1, "service-order"); err != nil {
		t.Fatalf("订单服务删除失败: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := store.Orders.Get(ctx, "service-order"); err == nil {
		t.Fatal("订单服务删除后仍可查询活动订单")
	}
}

// stringPtrForOrderTest 返回订单服务测试使用的字符串指针。
func stringPtrForOrderTest(value string) *string {
	return &value
}

// TestOrderResponseMappingAndErrorClassification 验证订单响应映射和业务错误分类不依赖 HTTP。
func TestOrderResponseMappingAndErrorClassification(t *testing.T) {
	// row 保存row，供当前处理流程使用
	// row 保存应用层订单列表模型，模拟适配器已经完成数据库转换。
	row := orderapp.OrderRow{OrderID: "mapped-order", ItemID: "mapped-item", ItemTitle: "测试商品", ItemDetail: `{"pic_info":{"picUrl":"https://img.example/mapped.png"}}`, OrderStatus: "2"}
	// view 保存view，供当前处理流程使用
	view := orderDTOFromRow(row)
	if view.OrderStatus != "pending_ship" || view.Status != "pending_ship" || view.ItemImage != "https://img.example/mapped.png" {
		t.Fatalf("订单响应映射异常: %+v", view)
	}
	// validationErr 保存validationErr，供当前处理流程使用
	validationErr := newOrderBadRequest("不支持的订单状态")
	if // kind、ok 保存kind、ok，供当前处理流程使用
	kind, ok := orderErrorKindOf(validationErr); !ok || kind != orderErrorBadRequest {
		t.Fatalf("订单错误分类异常: kind=%v ok=%v", kind, ok)
	}
}

// TestAtoiDefault atoiDefault 表驱动。
func TestAtoiDefault(t *testing.T) {
	// cases 保存cases，供当前处理流程使用
	cases := map[string]int{"": 5, "abc": 5, "3": 3, "12": 12}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 保存got，供当前处理流程使用
		got := atoiDefault(in, 5); got != want {
			t.Errorf("atoiDefault(%q)=%d want %d", in, got, want)
		}
	}
}
