package engine

import (
	"context"
	"testing"
	"time"
)

// TestAccountStopWaitsForTaskAndConcurrentStop 验证 Stop 禁止新任务、等待已有任务，并让并发 Stop 等待同一收束结果。
func TestAccountStopWaitsForTaskAndConcurrentStop(t *testing.T) {
	// account 是未连接但已允许任务登记的测试账号 facade。
	account := New(Config{CookieID: "lifecycle-test", CookieStr: "unb=1"})
	// ctx 是测试业务任务使用的账号生命周期上下文。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	account.lifecycle.start(ctx, cancel)
	// taskCtx 是已登记任务拿到的上下文；ok 表示任务成功进入生命周期。
	taskCtx, ok := account.beginTask()
	if !ok || taskCtx == nil {
		t.Fatal("业务任务应在 Stop 前成功登记")
	}
	// firstDone 和 secondDone 分别表示两个并发 Stop 调用已经完整返回。
	firstDone := make(chan struct{})
	// secondDone 是第二个并发 Stop 调用完整返回时关闭的信号。
	secondDone := make(chan struct{})
	go func() {
		account.Stop()
		close(firstDone)
	}()
	go func() {
		account.Stop()
		close(secondDone)
	}()
	select {
	case <-firstDone:
		t.Fatal("Stop 在已登记任务完成前提前返回")
	case <-secondDone:
		t.Fatal("并发 Stop 在已登记任务完成前提前返回")
	case <-time.After(50 * time.Millisecond):
	}
	account.lifecycle.finishTask()
	select {
	case <-firstDone:
	case <-time.After(time.Second):
		t.Fatal("第一次 Stop 未在任务完成后返回")
	}
	select {
	case <-secondDone:
	case <-time.After(time.Second):
		t.Fatal("并发 Stop 未等待第一次 Stop 完成")
	}
	// stoppedCtx 是 Stop 后尝试登记任务得到的上下文；stoppedOK 必须为 false。
	stoppedCtx, stoppedOK := account.beginTask()
	if stoppedOK || stoppedCtx != nil {
		t.Fatal("Stop 后不应再接受业务任务")
	}
}
