package adapter

import (
	"context"
	"testing"
	"time"

	"xianyu-go/internal/xianyu/mtop"
)

// blockingOrderDetailClient 在外部请求期间阻塞，用于验证凭证锁不会跨越慢速 I/O。
type blockingOrderDetailClient struct {
	// started 表示外部订单详情请求已经开始。
	started chan struct{}
	// release 允许测试结束外部请求阻塞。
	release chan struct{}
	// detail 是请求结束后返回的订单详情。
	detail *mtop.OrderDetailResult
}

// FetchOrderDetail 阻塞到测试显式释放后返回订单详情。
func (c *blockingOrderDetailClient) FetchOrderDetail(ctx context.Context, _, _ string) (*mtop.OrderDetailResult, error) {
	close(c.started)
	select {
	case <-c.release:
		return c.detail, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestFetchOrderDetailReleasesCredentialLockDuringSlowIO 验证订单详情外部调用不持有共享凭证锁。
func TestFetchOrderDetailReleasesCredentialLockDuringSlowIO(t *testing.T) {
	// store 是订单详情协调器测试使用的账号数据库。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// client 是阻塞外部 I/O 的订单详情桩。
	client := &blockingOrderDetailClient{
		started: make(chan struct{}), release: make(chan struct{}),
		detail: &mtop.OrderDetailResult{Quantity: "1", Amount: "9.9"},
	}
	// adapter 是待验证凭证快照协调行为的适配器。
	adapter := New(store, nil, nil)
	adapter.SetOrderDetailClient(client)
	// result 是订单详情调用完成的同步通道。
	result := make(chan struct{})
	go func() {
		// orderErr 是订单详情调用的错误结果。
		_, orderErr := adapter.FetchOrderDetail(context.Background(), "cid", "slow-order", "item", "buyer", "")
		if orderErr != nil {
			t.Errorf("订单详情调用失败: %v", orderErr)
		}
		close(result)
	}()
	select {
	case <-client.started:
	case <-time.After(time.Second):
		t.Fatal("订单详情请求未开始")
	}
	// lockReleased 是另一个调用方成功取得并释放凭证锁的同步信号。
	lockReleased := make(chan struct{})
	go func() {
		// unlock 是探测调用方取得的凭证锁释放函数。
		unlock := store.LockAccountCredentials("cid")
		unlock()
		close(lockReleased)
	}()
	select {
	case <-lockReleased:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("慢速订单详情调用仍占用共享凭证锁")
	}
	close(client.release)
	select {
	case <-result:
	case <-time.After(time.Second):
		t.Fatal("订单详情调用未完成")
	}
}
