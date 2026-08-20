package db

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// TestOrderUpsertRetryDelay 验证订单乐观锁冲突的退避曲线有界，避免高并发下立即重试形成活锁。
func TestOrderUpsertRetryDelay(t *testing.T) {
	// cases 保存覆盖首次冲突、指数增长、负数输入和上限截断的退避时间测试数据。
	cases := []struct {
		// name 标识当前退避场景；attempt 表示已连续失败次数；want 表示期望等待时间。
		name    string
		attempt int
		want    time.Duration
	}{
		{name: "首次冲突", attempt: 0, want: time.Millisecond},
		{name: "第三次冲突", attempt: 2, want: 4 * time.Millisecond},
		{name: "负数按首次处理", attempt: -1, want: time.Millisecond},
		{name: "超过上限", attempt: 20, want: maxOrderUpsertRetryDelay},
	}
	// testCase 表示当前要验证的一组退避输入与期望输出。
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			// got 保存实际计算得到的重试等待时间。
			got := orderUpsertRetryDelay(testCase.attempt)
			if got != testCase.want {
				t.Fatalf("attempt=%d delay=%s want %s", testCase.attempt, got, testCase.want)
			}
		})
	}
}

// TestWaitOrderUpsertRetryHonorsCancellation 验证请求取消后不会继续等待订单重试窗口。
func TestWaitOrderUpsertRetryHonorsCancellation(t *testing.T) {
	// ctx、cancel 分别控制重试等待的生命周期，并由本测试在调用前触发取消。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// err 保存取消后的重试等待结果。
	err := waitOrderUpsertRetry(ctx, 0)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled retry error=%v want %v", err, context.Canceled)
	}
}

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
