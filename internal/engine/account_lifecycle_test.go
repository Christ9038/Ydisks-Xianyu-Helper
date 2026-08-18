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
	// taskCtx、finish、ok 分别是已登记任务的上下文、释放函数与生命周期接纳结果。
	taskCtx, finish, ok := account.beginTask()
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
	finish()
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
	// stoppedCtx、stoppedFinish、stoppedOK 分别是停止后任务的上下文、释放函数与接纳结果。
	stoppedCtx, stoppedFinish, stoppedOK := account.beginTask()
	if stoppedOK || stoppedCtx != nil || stoppedFinish != nil {
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
	// taskCtx、finish、ok 分别是测试业务任务的上下文、释放函数与接纳结果。
	taskCtx, finish, ok := account.beginTask()
	if !ok || taskCtx == nil || finish == nil {
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
	// retryDone 表示第一次超时后的第二次 Stop 是否仍等待原任务收束。
	retryDone := make(chan error, 1)
	go func() {
		retryDone <- account.StopContext(context.Background())
	}()
	select {
	case <-retryDone:
		t.Fatal("第一次 Stop 超时后，重试不应在任务完成前返回")
	case <-time.After(20 * time.Millisecond):
	}
	finish()
	select {
	// retryErr 表示第一次超时后再次停止账号的等待结果。
	case retryErr := <-retryDone:
		if retryErr != nil {
			t.Fatalf("重试 StopContext error=%v", retryErr)
		}
	case <-time.After(time.Second):
		t.Fatal("第一次 Stop 超时后，重试未在任务完成后返回")
	}
}

// TestAccountLifecycleWaitContextCanRetryAfterTimeout 验证超时等待不占用额外协程，且后续调用能继续等待同一任务收束信号。
func TestAccountLifecycleWaitContextCanRetryAfterTimeout(t *testing.T) {
	// lifecycle 是独立的账号业务任务生命周期组件。
	lifecycle := accountLifecycle{}
	// runCtx 是任务共享的运行上下文；cancel 用于测试结束时释放资源。
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if !lifecycle.start(runCtx, cancel) {
		t.Fatal("生命周期应允许首次启动")
	}
	// taskCtx、finish、accepted 分别是登记任务的上下文、释放函数与停止收束接纳结果。
	taskCtx, finish, accepted := lifecycle.beginTask()
	if !accepted || taskCtx == nil || finish == nil {
		t.Fatal("业务任务应成功登记")
	}
	// shouldStop 表示本次调用创建了停止收束信号；stopErr 表示停止切换错误。
	_, shouldStop, stopErr := lifecycle.stopContext(context.Background())
	if stopErr != nil || !shouldStop {
		t.Fatalf("停止生命周期失败：shouldStop=%v err=%v", shouldStop, stopErr)
	}
	// timeoutCtx 是短超时等待上下文；timeoutCancel 释放其计时器。
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer timeoutCancel()
	if lifecycle.waitContext(timeoutCtx) {
		t.Fatal("未完成任务时 waitContext 应受上下文超时限制")
	}
	// waited 表示第二次等待是否在任务完成前返回，证明它继续订阅原停止信号。
	waited := make(chan bool, 1)
	go func() {
		waited <- lifecycle.waitContext(context.Background())
	}()
	select {
	case <-waited:
		t.Fatal("任务未完成时，重试等待不应提前返回")
	case <-time.After(20 * time.Millisecond):
	}
	finish()
	select {
	// completed 表示第二次等待在任务收束后得到正常完成结果。
	case completed := <-waited:
		if !completed {
			t.Fatal("任务完成后 waitContext 应返回 true")
		}
	case <-time.After(time.Second):
		t.Fatal("任务完成后 waitContext 未返回")
	}
}

// TestAccountLifecycleRejectsLateStart 验证 Stop 先于 Run 时不会重新开放已停止的生命周期。
func TestAccountLifecycleRejectsLateStart(t *testing.T) {
	// lifecycle 是尚未运行但允许被停止的账号生命周期组件。
	lifecycle := accountLifecycle{accepting: true}
	// stopErr 表示预先停止生命周期时的错误结果。
	_, shouldStop, stopErr := lifecycle.stopContext(context.Background())
	if stopErr != nil || !shouldStop {
		t.Fatalf("预先停止生命周期失败：shouldStop=%v err=%v", shouldStop, stopErr)
	}
	// runCtx 是迟到 Run 尝试注册的运行上下文；cancel 负责释放该上下文。
	runCtx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if lifecycle.start(runCtx, cancel) {
		t.Fatal("Stop 先于 Run 时不应重新启动生命周期")
	}
	// taskCtx、finish、accepted 分别是停止后任务的上下文、释放函数与接纳结果。
	taskCtx, finish, accepted := lifecycle.beginTask()
	if accepted || taskCtx != nil || finish != nil {
		t.Fatalf("停止后的生命周期不应接收任务：ctx=%v accepted=%v", taskCtx, accepted)
	}
}
