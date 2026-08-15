package engine

import (
	"context"
	"errors"
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

// TestAccountStopContextBoundsTaskWait 验证停止上下文到期时不会无限等待业务任务。
func TestAccountStopContextBoundsTaskWait(t *testing.T) {
	// account 是用于验证停止超时的测试账号 facade。
	account := New(Config{CookieID: "lifecycle-timeout", CookieStr: "unb=1"})
	// runCtx 是测试账号运行上下文；cancel 负责释放运行上下文。
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	account.lifecycle.start(runCtx, cancel)
	// ok 表示测试业务任务是否成功登记。
	if _, ok := account.beginTask(); !ok {
		t.Fatal("业务任务应成功登记")
	}
	// stopCtx 是刻意很短的停止上下文，用于验证有界等待。
	stopCtx, stopCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer stopCancel()
	// started 记录停止开始时间，用于验证上下文确实限制等待时长。
	started := time.Now()
	// err 表示停止上下文到期后的返回错误。
	err := account.StopContext(stopCtx)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("StopContext error=%v, want deadline exceeded", err)
	}
	// elapsed 表示停止调用实际耗时。
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("StopContext waited too long: %s", elapsed)
	}
	account.lifecycle.finishTask()
}
