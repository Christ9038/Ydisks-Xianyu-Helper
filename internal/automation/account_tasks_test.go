package automation

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

// fakeAccountTaskClient 保存fake账号任务Client，供当前处理流程使用
type fakeAccountTaskClient struct {
	pendingCalls  int
	rateCalls     int
	polishCalls   int
	pending       []mtop.PendingRateOrder
	pendingErr    error
	rateErr       error
	items         []mtop.ItemListItem
	fetchItemsErr error
	fetchPageSize int
	fetchMaxPages int
	polishErr     error
}

// FetchPendingRateOrders 负责FetchPendingRate订单列表相关处理。
func (f *fakeAccountTaskClient) FetchPendingRateOrders(context.Context, string, int, int) (*mtop.PendingRateResult, error) {
	f.pendingCalls++
	if f.pendingErr != nil {
		return nil, f.pendingErr
	}
	return &mtop.PendingRateResult{Orders: f.pending}, nil
}

// RateBuyer 负责Rate买家相关处理。
func (f *fakeAccountTaskClient) RateBuyer(context.Context, string, string, string) (*mtop.AccountTaskResult, error) {
	f.rateCalls++
	if f.rateErr != nil {
		return nil, f.rateErr
	}
	return &mtop.AccountTaskResult{Success: true, Message: "ok"}, nil
}

// FetchAllItems 负责FetchAll商品列表相关处理。
func (f *fakeAccountTaskClient) FetchAllItems(_ context.Context, _ string, pageSize, maxPages int) (*mtop.ItemListResult, error) {
	f.fetchPageSize, f.fetchMaxPages = pageSize, maxPages
	if f.fetchItemsErr != nil {
		return nil, f.fetchItemsErr
	}
	return &mtop.ItemListResult{Items: f.items}, nil
}

// PolishItem 负责Polish商品相关处理。
func (f *fakeAccountTaskClient) PolishItem(context.Context, string, string) (*mtop.AccountTaskResult, error) {
	f.polishCalls++
	if f.polishErr != nil {
		return nil, f.polishErr
	}
	return &mtop.AccountTaskResult{Success: true, Message: "ok"}, nil
}

// TestAccountTaskRateIsOrderIdempotent 负责Test账号任务RateIs订单Idempotent相关处理。
func TestAccountTaskRateIsOrderIdempotent(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 保存client，供当前处理流程使用
	client := &fakeAccountTaskClient{pending: []mtop.PendingRateOrder{{TradeID: "order-1"}, {TradeID: "order-2"}}}
	// center 保存center，供当前处理流程使用
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	// first、err 保存first、err，供当前处理流程使用
	first, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if err != nil || first.Success != 2 || client.rateCalls != 2 {
		t.Fatalf("first=%+v calls=%d err=%v", first, client.rateCalls, err)
	}
	// second、err 保存second、err，供当前处理流程使用
	second, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if err != nil || second.Skipped != 2 || client.rateCalls != 2 {
		t.Fatalf("second=%+v calls=%d err=%v", second, client.rateCalls, err)
	}
}

// TestAccountTaskSessionExpiredRecoversOnceAndBlocksFurtherAPIRequests 负责Test账号任务会话ExpiredRecoversOnceAndBlocksFurtherAPI请求列表相关处理。
func TestAccountTaskSessionExpiredRecoversOnceAndBlocksFurtherAPIRequests(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// sessionErr 保存会话Err，供当前处理流程使用
	sessionErr := &mtop.SessionExpiredError{API: "自动评价接口", Ret: []string{"FAIL_SYS_SESSION_EXPIRED::Session过期"}}
	// client 保存client，供当前处理流程使用
	client := &fakeAccountTaskClient{pendingErr: sessionErr}
	// recoverer 保存recoverer，供当前处理流程使用
	recoverer := &fakeCredentialRecoverer{store: store, fail: true}
	// center 保存center，供当前处理流程使用
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	center.SetOrderDetailFetcher(recoverer)
	if // err 保存err，供当前处理流程使用
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}

	if // err 保存err，供当前处理流程使用
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err == nil || !mtop.IsSessionExpiredErr(err) {
		t.Fatalf("首次 session 失效应触发续期并返回原始分类错误: %v", err)
	}
	if client.pendingCalls != 1 || recoverer.calls != 1 {
		t.Fatalf("first calls: api=%d recover=%d want 1/1", client.pendingCalls, recoverer.calls)
	}
	if // err 保存err，供当前处理流程使用
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err == nil || !strings.Contains(err.Error(), "已停止自动化 API 请求") {
		t.Fatalf("未更新凭证时应保持阻断: %v", err)
	}
	if client.pendingCalls != 1 || recoverer.calls != 1 {
		t.Fatalf("blocked run must not call API/recovery again: api=%d recover=%d", client.pendingCalls, recoverer.calls)
	}

	if // err 保存err，供当前处理流程使用
	err := store.Cookies.UpdateValueExisting(ctx, "cid", "unb=1; _m_h5_tk=fresh_1; renewed=1"); err != nil {
		t.Fatal(err)
	}
	client.pendingErr = nil
	if // err 保存err，供当前处理流程使用
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err != nil {
		t.Fatalf("凭证变化后应自动解除阻断: %v", err)
	}
	if client.pendingCalls != 2 {
		t.Fatalf("api calls after credential update=%d want 2", client.pendingCalls)
	}
}

// TestAccountTaskStopsRemainingOrdersOnSessionExpired 负责Test账号任务StopsRemaining订单列表On会话Expired相关处理。
func TestAccountTaskStopsRemainingOrdersOnSessionExpired(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// sessionErr 保存会话Err，供当前处理流程使用
	sessionErr := &mtop.SessionExpiredError{API: "评价接口", Ret: []string{"FAIL_SYS_SESSION_EXPIRED::Session过期"}}
	// client 保存client，供当前处理流程使用
	client := &fakeAccountTaskClient{
		pending: []mtop.PendingRateOrder{{TradeID: "order-1"}, {TradeID: "order-2"}},
		rateErr: sessionErr,
	}
	// recoverer 保存recoverer，供当前处理流程使用
	recoverer := &fakeCredentialRecoverer{store: store, fail: true}
	// center 保存center，供当前处理流程使用
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	center.SetOrderDetailFetcher(recoverer)
	if // err 保存err，供当前处理流程使用
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}

	if // err 保存err，供当前处理流程使用
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoRate); err == nil {
		t.Fatal("session expiry must be returned")
	}
	if client.rateCalls != 1 || recoverer.calls != 1 {
		t.Fatalf("remaining orders must stop immediately: rate=%d recover=%d", client.rateCalls, recoverer.calls)
	}
}

// TestAccountTaskPolishRunsOncePerBeijingDay 负责Test账号任务Polish运行记录OncePerBeijingDay相关处理。
func TestAccountTaskPolishRunsOncePerBeijingDay(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 保存client，供当前处理流程使用
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-1"}, {ID: "item-2"}}}
	// center 保存center，供当前处理流程使用
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"}); err != nil {
		t.Fatal(err)
	}
	// first、err 保存first、err，供当前处理流程使用
	first, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || first.Success != 2 || client.polishCalls != 2 {
		t.Fatalf("first=%+v calls=%d err=%v", first, client.polishCalls, err)
	}
	if client.fetchPageSize != 20 || client.fetchMaxPages != 20 {
		t.Fatalf("unexpected item pagination: pageSize=%d maxPages=%d", client.fetchPageSize, client.fetchMaxPages)
	}
	// second、err 保存second、err，供当前处理流程使用
	second, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || second.Skipped != 1 || client.polishCalls != 2 {
		t.Fatalf("second=%+v calls=%d err=%v", second, client.polishCalls, err)
	}
	// settings、err 保存settings、err，供当前处理流程使用
	settings, err := store.AccountTasks.Get(ctx, "cid")
	if err != nil || settings.LastPolishDate != beijingNow().Format("2006-01-02") {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
}

// TestManualPolishReportsItemFailures 负责TestManualPolishReports商品Failures相关处理。
func TestManualPolishReportsItemFailures(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 保存client，供当前处理流程使用
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-1"}}, polishErr: errors.New("both polish APIs failed")}
	// center 保存center，供当前处理流程使用
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"}); err != nil {
		t.Fatal(err)
	}
	// summary、err 保存summary、err，供当前处理流程使用
	summary, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err == nil || summary.Failed != 1 || summary.Success != 0 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

// TestManualPolishCanRetryImmediatelyAfterFailure 负责TestManualPolishCan重试ImmediatelyAfterFailure相关处理。
func TestManualPolishCanRetryImmediatelyAfterFailure(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// client 保存client，供当前处理流程使用
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-1"}}, fetchItemsErr: errors.New("upstream 502")}
	// center 保存center，供当前处理流程使用
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"}); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	_, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish); err == nil {
		t.Fatal("first polish should expose the upstream failure")
	}
	client.fetchItemsErr = nil
	// second、err 保存second、err，供当前处理流程使用
	second, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || second.Success != 1 || second.Skipped != 0 || client.polishCalls != 1 {
		t.Fatalf("manual retry=%+v calls=%d err=%v", second, client.polishCalls, err)
	}
	// third、err 保存third、err，供当前处理流程使用
	third, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || third.Skipped != 1 || client.polishCalls != 1 {
		t.Fatalf("successful day must remain idempotent: summary=%+v calls=%d err=%v", third, client.polishCalls, err)
	}
}

// TestPolishDueHonorsConfiguredTimeAndDate 负责TestPolishDueHonorsConfigured时间And日期相关处理。
func TestPolishDueHonorsConfiguredTimeAndDate(t *testing.T) {
	// now 保存now，供当前处理流程使用
	now := beijingNow()
	// settings 保存设置，供当前处理流程使用
	settings := db.AccountTaskSettings{PolishTime: now.Add(2 * time.Hour).Format("15:04")}
	if polishDue(settings, now) && now.Hour() < 22 {
		t.Fatal("task must not run before configured time")
	}
	settings.PolishTime = "00:00"
	if !polishDue(settings, now) {
		t.Fatal("task should be due after midnight")
	}
	settings.LastPolishDate = now.Format("2006-01-02")
	if polishDue(settings, now) {
		t.Fatal("task must not run twice on the same Beijing date")
	}
}
