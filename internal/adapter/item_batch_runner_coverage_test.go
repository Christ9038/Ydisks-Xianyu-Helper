package adapter

import (
	"context"
	"errors"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// batchPublishPortFake 是批量发布 worker 测试使用的内存远端端口。
type batchPublishPortFake struct {
	// outcome 保存预置的远端发布结果。
	outcome itemapp.BatchPublishOutcome
	// err 保存预置的远端发布错误。
	err error
	// calls 统计远端发布调用次数。
	calls int
}

// PublishRemoteRow 返回预置的远端发布结果。
func (p *batchPublishPortFake) PublishRemoteRow(context.Context, int64, itemapp.BatchRow, string, func(context.Context) error) (itemapp.BatchPublishOutcome, error) {
	p.calls++
	return p.outcome, p.err
}

// batchLocalPublisherFake 是批量发布 worker 测试使用的内存本地收口端口。
type batchLocalPublisherFake struct {
	// calls 统计本地完成收口调用次数。
	calls int
	// err 保存预置的本地收口错误。
	err error
}

// Complete 记录远端成功后的本地收口请求。
func (p *batchLocalPublisherFake) Complete(context.Context, int64, itemapp.BatchRow, string, *itemapp.BatchPublishResult) error {
	p.calls++
	return p.err
}

// TestItemBatchPublisherPublishRowPaths 验证批量发布 worker 对远端、Cookie 和本地收口结果的错误分类。
func TestItemBatchPublisherPublishRowPaths(t *testing.T) {
	// ctx 是本测试发布 worker 共用的上下文。
	ctx := context.Background()
	// remote、local 保存成功路径使用的两个内存端口。
	remote := &batchPublishPortFake{outcome: itemapp.BatchPublishOutcome{Result: &itemapp.BatchPublishResult{ItemID: "remote"}}}
	// local 保存记录本地收口调用次数的内存端口。
	local := &batchLocalPublisherFake{}
	// publisher、publisherErr 保存批量发布 worker 的构造结果。
	publisher, publisherErr := NewItemBatchPublisher(remote, local)
	if publisherErr != nil || publisher == nil {
		t.Fatalf("批量发布 worker 构造失败 publisher=%v err=%v", publisher, publisherErr)
	}
	// successErr 保存远端成功后本地收口结果。
	successErr := publisher.PublishRow(ctx, 1, itemapp.BatchRow{BatchID: "batch", ID: 1}, "worker", nil)
	if successErr != nil || remote.calls != 1 || local.calls != 1 {
		t.Fatalf("成功发布收口异常 err=%v remote=%d local=%d", successErr, remote.calls, local.calls)
	}
	// remoteErr 保存远端确定失败结果。
	remote.err = errors.New("remote failed")
	// publishErr 保存远端失败后的 worker 错误。
	publishErr := publisher.PublishRow(ctx, 1, itemapp.BatchRow{ID: 2}, "worker", nil)
	if publishErr == nil || local.calls != 1 {
		t.Fatalf("远端失败未正确短路 err=%v local=%d", publishErr, local.calls)
	}
	remote.err = nil
	// remote.outcome 保存没有商品结果的异常成功响应。
	remote.outcome = itemapp.BatchPublishOutcome{}
	// noResultErr 保存远端空结果后的 worker 错误。
	noResultErr := publisher.PublishRow(ctx, 1, itemapp.BatchRow{ID: 3}, "worker", nil)
	if noResultErr == nil {
		t.Fatal("空远端结果应返回错误")
	}
	remote.outcome = itemapp.BatchPublishOutcome{Result: &itemapp.BatchPublishResult{ItemID: "remote"}, ResponseCookieErr: errors.New("cookie persistence failed")}
	// cookieErr 保存远端成功但 Cookie 后置失败的 worker 错误。
	cookieErr := publisher.PublishRow(ctx, 1, itemapp.BatchRow{ID: 4}, "worker", nil)
	if cookieErr == nil {
		t.Fatal("Cookie 后置错误应返回错误")
	}
	remote.outcome = itemapp.BatchPublishOutcome{Result: &itemapp.BatchPublishResult{ItemID: "remote"}}
	// canceledContext 保存本地收口前已取消的上下文。
	canceledContext, cancel := context.WithCancel(ctx)
	cancel()
	// canceledErr 保存取消上下文导致的本地后置错误。
	canceledErr := publisher.PublishRow(canceledContext, 1, itemapp.BatchRow{ID: 5}, "worker", nil)
	if canceledErr == nil {
		t.Fatal("取消上下文应阻止本地收口")
	}
	// fallbackErr 保存租约错误回退值。
	fallbackErr := errors.New("fallback")
	// remoteErr 保存应优先返回的远端错误。
	remoteErr := errors.New("remote selected")
	if firstBatchError(nil, fallbackErr) != fallbackErr || firstBatchError(remoteErr, fallbackErr) != remoteErr {
		t.Fatal("批次首个错误选择异常")
	}
}

// TestItemBatchPublishRecoveryCallback 验证批量远端适配器在平台失败时转发恢复回调。
func TestItemBatchPublishRecoveryCallback(t *testing.T) {
	// recovered 保存恢复回调收到的账号标识。
	recovered := ""
	// port 保存仅用于调用恢复回调的最小批量远端适配器。
	port := NewItemBatchPublishPort(nil, nil, nil, nil, func(_ context.Context, accountID string, _ error) {
		recovered = accountID
	}, nil, nil)
	// platformErr 保存模拟的平台会话错误。
	platformErr := errors.New("session expired")
	port.recoverExpired(context.Background(), "cid", platformErr)
	port.recoverExpired(context.Background(), "cid", nil)
	if recovered != "cid" {
		t.Fatalf("恢复回调未收到账号=%q", recovered)
	}
	// emptyPort 保存 nil receiver 的兼容测试对象。
	var emptyPort *ItemBatchPublishPort
	emptyPort.recoverExpired(context.Background(), "cid", platformErr)
}
