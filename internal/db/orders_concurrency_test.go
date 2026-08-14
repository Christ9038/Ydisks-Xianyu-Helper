package db

import (
	"context"
	"sync"
	"testing"
)

// TestOrderUpsertConcurrentStatusNeverRegresses 负责Test订单UpsertConcurrent状态NeverRegresses相关处理。
func TestOrderUpsertConcurrentStatusNeverRegresses(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // err 保存err，供当前处理流程使用
	_, err := store.Users.Create(ctx, "order-owner", "order-owner@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// owner 保存所有者，供当前处理流程使用
	owner, _ := store.Users.GetByUsername(ctx, "order-owner")
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.CreateOwned(ctx, "order-account", "cookie", owner.ID); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{CookieID: "order-account", OrderStatus: "paid"}); err != nil {
		t.Fatal(err)
	}

	// start 保存开始，供当前处理流程使用
	start := make(chan struct{})
	// errCh 保存errCh，供当前处理流程使用
	errCh := make(chan error, 200)
	// wg 保存wg，供当前处理流程使用
	var wg sync.WaitGroup
	// status 表示当前遍历过程中的状态
	for _, status := range []string{"paid", "shipped"} {
		// status 保存状态，供当前处理流程使用
		status := status
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for // i 保存i，供当前处理流程使用
			i := 0; i < 100; i++ {
				errCh <- store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{
					CookieID: "order-account", OrderStatus: status,
				})
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	// err 表示当前遍历过程中的err
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent upsert: %v", err)
		}
	}
	// order、err 保存order、err，供当前处理流程使用
	order, err := store.Orders.Get(ctx, "concurrent-order")
	if err != nil {
		t.Fatal(err)
	}
	if // got 保存got，供当前处理流程使用
	got := NormalizeOrderStatus(order.OrderStatus); got != "shipped" {
		t.Fatalf("final status=%q want shipped", got)
	}
	if order.Version <= 1 {
		t.Fatalf("version=%d was not advanced", order.Version)
	}

	if // err 保存err，供当前处理流程使用
	err := store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{CookieID: "order-account", OrderStatus: "completed"}); err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{CookieID: "order-account", OrderStatus: "shipped"}); err != nil {
		t.Fatal(err)
	}
	order, _ = store.Orders.Get(ctx, "concurrent-order")
	if // got 保存got，供当前处理流程使用
	got := NormalizeOrderStatus(order.OrderStatus); got != "completed" {
		t.Fatalf("completed order regressed to %q", got)
	}
}
