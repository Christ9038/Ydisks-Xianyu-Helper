package server

import (
	"context"
	"errors"
	"net"
	"net/http"
	"testing"
	"time"
)

// freeAddr 获取一个空闲 TCP 端口（立即释放，供测试绑定）。
func freeAddr(t *testing.T) string {
	t.Helper()
	// l、err 保存l、err，供当前处理流程使用
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	// addr 保存addr，供当前处理流程使用
	addr := l.Addr().String()
	l.Close()
	return addr
}

// TestRun_ServesHealthAndShutdowns 启动 HTTP 服务，/health 可访问，ctx 取消后优雅退出。
func TestRun_ServesHealthAndShutdowns(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.Addr = freeAddr(t)

	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(context.Background())
	// runDone 保存运行Done，供当前处理流程使用
	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	// 轮询 /health 直到可用（最多 3s）。
	url := "http://" + srv.Addr + "/health"
	// ok 保存ok，供当前处理流程使用
	var ok bool
	// deadline 保存deadline，供当前处理流程使用
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		// resp、err 保存resp、err，供当前处理流程使用
		resp, err := http.Get(url)
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode == 200 {
				ok = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !ok {
		cancel()
		t.Fatal("Run 启动后 /health 3s 内不可访问")
	}

	// 取消 ctx → Run 应优雅退出。
	cancel()
	select {
	case // err 保存err，供当前处理流程使用
	err := <-runDone:
		if err != nil {
			t.Fatalf("Run 应返回 nil，got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run 未在 ctx 取消后 5s 内退出")
	}
}

// TestPublishWorkerTrackingWaitsForCompletion 负责Test发布工作器TrackingWaitsForCompletion相关处理。
func TestPublishWorkerTrackingWaitsForCompletion(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// doneWorker 保存done工作器，供当前处理流程使用
	doneWorker := srv.beginWorker()
	// waited 保存waited，供当前处理流程使用
	waited := make(chan struct{})
	go func() {
		srv.waitForWorkers(time.Second)
		close(waited)
	}()
	select {
	case <-waited:
		t.Fatal("wait returned while worker was still active")
	case <-time.After(20 * time.Millisecond):
	}
	doneWorker()
	select {
	case <-waited:
	case <-time.After(time.Second):
		t.Fatal("wait did not return after worker completed")
	}
}

// TestPublishRecoveryLifecycleStopsBeforeWorkerWait 负责Test发布RecoveryLifecycleStopsBefore工作器Wait相关处理。
func TestPublishRecoveryLifecycleStopsBeforeWorkerWait(t *testing.T) {
	// srv、cleanup 保存srv、cleanup，供当前处理流程使用
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithCancel(context.Background())
	srv.StartPublishBatchRecovery(ctx)
	cancel()
	// done 保存done，供当前处理流程使用
	done := make(chan struct{})
	go func() {
		srv.WaitForBackground()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("批量发布恢复扫描器关闭后没有退出")
	}
}

// TestNewRejectsMissingRequiredDependencies 确保 HTTP 服务构造阶段拒绝缺失核心依赖。
func TestNewRejectsMissingRequiredDependencies(t *testing.T) {
	// err 是缺少 Store 时的构造校验错误。
	if _, err := New(nil, nil, false, "", ":0", nil, nil, nil); err == nil {
		t.Fatal("缺少 db.Store 时应返回构造错误")
	}
	// srv 是用于提供合法 Store 的测试服务；cleanup 负责释放测试数据库。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	if srv == nil {
		t.Fatal("测试服务不应为空")
	}
	// err 是缺少 Manager 时的构造校验错误。
	if _, err := New(srv.Store, nil, false, "", ":0", nil, nil, nil); err == nil {
		t.Fatal("缺少 account.Manager 时应返回构造错误")
	}
}

// TestServerStartStopIsIdempotentAndWaitsForWorkers 验证 Start/Stop 可重复调用且 Stop 等待 worker。
func TestServerStartStopIsIdempotentAndWaitsForWorkers(t *testing.T) {
	// srv 是待验证幂等生命周期的 HTTP 服务；cleanup 负责释放测试资源。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.Addr = freeAddr(t)
	if // err 保存err，供当前处理流程使用
	err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	if // err 保存err，供当前处理流程使用
	err := srv.Start(context.Background()); err != nil {
		t.Fatalf("重复 Start: %v", err)
	}
	// workerDone 是模拟批量发布 worker 完成时调用的释放函数。
	workerDone := srv.beginWorker()
	// stopDone 是显式 Stop 完成时关闭的测试信号。
	stopDone := make(chan struct{})
	go func() {
		// 显式停止过程的错误不影响本测试对等待语义的判断。
		_ = srv.Stop(context.Background())
		close(stopDone)
	}()
	select {
	case <-stopDone:
		t.Fatal("Stop 在 worker 完成前提前返回")
	case <-time.After(50 * time.Millisecond):
	}
	workerDone()
	select {
	case <-stopDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Stop 未在 worker 完成后返回")
	}
	// err 是重复 Stop 返回的关闭错误。
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("重复 Stop: %v", err)
	}
}

// TestServerStopContextBoundsWorkerWait 验证关闭上下文到期时不会无限等待后台 worker。
func TestServerStopContextBoundsWorkerWait(t *testing.T) {
	// srv 是用于验证关闭超时的 HTTP 服务；cleanup 负责释放测试数据库。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.Addr = freeAddr(t)
	// err 表示 HTTP 服务启动失败。
	if err := srv.Start(context.Background()); err != nil {
		t.Fatalf("Start: %v", err)
	}
	// workerDone 保持 worker 未完成，模拟不响应关闭的后台任务。
	workerDone := srv.beginWorker()
	// stopCtx 是刻意很短的关闭上下文，用于验证 Stop 的等待边界。
	stopCtx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	// started 记录停止开始时间，用于验证上下文确实限制等待时长。
	started := time.Now()
	// err 表示 worker 未完成时停止上下文到期返回的错误。
	err := srv.Stop(stopCtx)
	if err == nil || !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Stop error=%v, want deadline exceeded", err)
	}
	// elapsed 表示停止调用实际耗时。
	if elapsed := time.Since(started); elapsed > time.Second {
		t.Fatalf("Stop waited too long: %s", elapsed)
	}
	workerDone()
	// err 表示释放 worker 后第二次幂等停止的返回错误。
	if err := srv.Stop(context.Background()); err != nil {
		t.Fatalf("second Stop: %v", err)
	}
}
