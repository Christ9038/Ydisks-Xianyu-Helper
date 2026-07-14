package db

import (
	"context"
	"sync"
	"testing"
)

func TestOrderUpsertConcurrentStatusNeverRegresses(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if _, err := store.Users.Create(ctx, "order-owner", "order-owner@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	owner, _ := store.Users.GetByUsername(ctx, "order-owner")
	if err := store.Cookies.CreateOwned(ctx, "order-account", "cookie", owner.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{CookieID: "order-account", OrderStatus: "paid"}); err != nil {
		t.Fatal(err)
	}

	start := make(chan struct{})
	errCh := make(chan error, 200)
	var wg sync.WaitGroup
	for _, status := range []string{"paid", "shipped"} {
		status := status
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			for i := 0; i < 100; i++ {
				errCh <- store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{
					CookieID: "order-account", OrderStatus: status,
				})
			}
		}()
	}
	close(start)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("concurrent upsert: %v", err)
		}
	}
	order, err := store.Orders.Get(ctx, "concurrent-order")
	if err != nil {
		t.Fatal(err)
	}
	if got := NormalizeOrderStatus(order.OrderStatus); got != "shipped" {
		t.Fatalf("final status=%q want shipped", got)
	}
	if order.Version <= 1 {
		t.Fatalf("version=%d was not advanced", order.Version)
	}

	if err := store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{CookieID: "order-account", OrderStatus: "completed"}); err != nil {
		t.Fatal(err)
	}
	if err := store.Orders.Upsert(ctx, "concurrent-order", OrderUpsertOpts{CookieID: "order-account", OrderStatus: "shipped"}); err != nil {
		t.Fatal(err)
	}
	order, _ = store.Orders.Get(ctx, "concurrent-order")
	if got := NormalizeOrderStatus(order.OrderStatus); got != "completed" {
		t.Fatalf("completed order regressed to %q", got)
	}
}
