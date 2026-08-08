package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/db"
	"xianyu-go/internal/xianyu/mtop"
)

type fakeAccountTaskClient struct {
	pendingCalls  int
	rateCalls     int
	polishCalls   int
	pending       []mtop.PendingRateOrder
	items         []mtop.ItemListItem
	fetchItemsErr error
	fetchPageSize int
	fetchMaxPages int
	polishErr     error
}

func (f *fakeAccountTaskClient) FetchPendingRateOrders(context.Context, string, int, int) (*mtop.PendingRateResult, error) {
	f.pendingCalls++
	return &mtop.PendingRateResult{Orders: f.pending}, nil
}

func (f *fakeAccountTaskClient) RateBuyer(context.Context, string, string, string) (*mtop.AccountTaskResult, error) {
	f.rateCalls++
	return &mtop.AccountTaskResult{Success: true, Message: "ok"}, nil
}

func (f *fakeAccountTaskClient) FetchAllItems(_ context.Context, _ string, pageSize, maxPages int) (*mtop.ItemListResult, error) {
	f.fetchPageSize, f.fetchMaxPages = pageSize, maxPages
	if f.fetchItemsErr != nil {
		return nil, f.fetchItemsErr
	}
	return &mtop.ItemListResult{Items: f.items}, nil
}

func (f *fakeAccountTaskClient) PolishItem(context.Context, string, string) (*mtop.AccountTaskResult, error) {
	f.polishCalls++
	if f.polishErr != nil {
		return nil, f.polishErr
	}
	return &mtop.AccountTaskResult{Success: true, Message: "ok"}, nil
}

func TestAccountTaskRateIsOrderIdempotent(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	client := &fakeAccountTaskClient{pending: []mtop.PendingRateOrder{{TradeID: "order-1"}, {TradeID: "order-2"}}}
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	ctx := context.Background()
	if err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoRateEnabled: true,
		RateContent: "交易愉快", PolishTime: "03:00"}); err != nil {
		t.Fatal(err)
	}
	first, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if err != nil || first.Success != 2 || client.rateCalls != 2 {
		t.Fatalf("first=%+v calls=%d err=%v", first, client.rateCalls, err)
	}
	second, err := center.RunAccountTask(ctx, "cid", TaskAutoRate)
	if err != nil || second.Skipped != 2 || client.rateCalls != 2 {
		t.Fatalf("second=%+v calls=%d err=%v", second, client.rateCalls, err)
	}
}

func TestAccountTaskPolishRunsOncePerBeijingDay(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-1"}, {ID: "item-2"}}}
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	ctx := context.Background()
	if err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"}); err != nil {
		t.Fatal(err)
	}
	first, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || first.Success != 2 || client.polishCalls != 2 {
		t.Fatalf("first=%+v calls=%d err=%v", first, client.polishCalls, err)
	}
	if client.fetchPageSize != 20 || client.fetchMaxPages != 20 {
		t.Fatalf("unexpected item pagination: pageSize=%d maxPages=%d", client.fetchPageSize, client.fetchMaxPages)
	}
	second, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || second.Skipped != 1 || client.polishCalls != 2 {
		t.Fatalf("second=%+v calls=%d err=%v", second, client.polishCalls, err)
	}
	settings, err := store.AccountTasks.Get(ctx, "cid")
	if err != nil || settings.LastPolishDate != beijingNow().Format("2006-01-02") {
		t.Fatalf("settings=%+v err=%v", settings, err)
	}
}

func TestManualPolishReportsItemFailures(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-1"}}, polishErr: errors.New("both polish APIs failed")}
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	ctx := context.Background()
	if err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"}); err != nil {
		t.Fatal(err)
	}
	summary, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err == nil || summary.Failed != 1 || summary.Success != 0 {
		t.Fatalf("summary=%+v err=%v", summary, err)
	}
}

func TestManualPolishCanRetryImmediatelyAfterFailure(t *testing.T) {
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	client := &fakeAccountTaskClient{items: []mtop.ItemListItem{{ID: "item-1"}}, fetchItemsErr: errors.New("upstream 502")}
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	center.SetAccountTaskClient(client)
	ctx := context.Background()
	if err := store.AccountTasks.Upsert(ctx, db.AccountTaskSettings{CookieID: "cid", AutoPolishEnabled: true,
		RateContent: "交易愉快", PolishTime: "00:00"}); err != nil {
		t.Fatal(err)
	}
	if _, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish); err == nil {
		t.Fatal("first polish should expose the upstream failure")
	}
	client.fetchItemsErr = nil
	second, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || second.Success != 1 || second.Skipped != 0 || client.polishCalls != 1 {
		t.Fatalf("manual retry=%+v calls=%d err=%v", second, client.polishCalls, err)
	}
	third, err := center.RunAccountTask(ctx, "cid", TaskAutoPolish)
	if err != nil || third.Skipped != 1 || client.polishCalls != 1 {
		t.Fatalf("successful day must remain idempotent: summary=%+v calls=%d err=%v", third, client.polishCalls, err)
	}
}

func TestPolishDueHonorsConfiguredTimeAndDate(t *testing.T) {
	now := beijingNow()
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
