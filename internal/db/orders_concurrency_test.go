package db

import (
	"context"
	"sync"
	"testing"
)

// TestOrderUpsertConcurrentStatusNeverRegresses 封装Test订单UpsertConcurrent状态NeverRegresses业务协调。
func TestOrderUpsertConcurrentStatusNeverRegresses(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // err 用于本次流程后续判断的err
	_, err := store.Users.Create(ctx, "order-owner", "order-owner@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "order-owner")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.CreateOwned(ctx, "order-account", "cookie", owner.ID); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{CookieID: "order-account", OrderStatus: "paid"}); err != nil {
		t.Fatal(err)
	}

	// start 用于本次流程后续判断的开始
	start := make(chan struct{})
	// errCh 用于本次流程后续判断的errCh
	errCh := make(chan error, 200)
	// wg 用于本次流程后续判断的wg
	var wg sync.WaitGroup
	// status 表示当前遍历过程中的状态
	for _, status := range []string{"paid", "shipped"} {
		// status 用于本次流程后续判断的状态
		status := status
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for // i 用于本次流程后续判断的i
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
	// order、err 用于本次流程后续判断的order、err
	order, err := store.Orders.Get(ctx, "concurrent-order")
	if err != nil {
		t.Fatal(err)
	}
	if // got 用于本次流程后续判断的got
	got := NormalizeOrderStatus(order.OrderStatus); got != "shipped" {
		t.Fatalf("final status=%q want shipped", got)
	}
	if order.Version <= 1 {
		t.Fatalf("version=%d was not advanced", order.Version)
	}

	if // err 用于本次流程后续判断的err
	err := store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{CookieID: "order-account", OrderStatus: "completed"}); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{CookieID: "order-account", OrderStatus: "shipped"}); err != nil {
		t.Fatal(err)
	}
	order, _ = store.Orders.Get(ctx, "concurrent-order")
	if // got 用于本次流程后续判断的got
	got := NormalizeOrderStatus(order.OrderStatus); got != "completed" {
		t.Fatalf("completed order regressed to %q", got)
	}
}
