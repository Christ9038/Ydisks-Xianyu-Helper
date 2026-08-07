package server

import (
	"context"
	"net"
	"net/http"
	"testing"
	"time"
)

// freeAddr 获取一个空闲 TCP 端口（立即释放，供测试绑定）。
func freeAddr(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	addr := l.Addr().String()
	l.Close()
	return addr
}

// TestRun_ServesHealthAndShutdowns 启动 HTTP 服务，/health 可访问，ctx 取消后优雅退出。
func TestRun_ServesHealthAndShutdowns(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	srv.Addr = freeAddr(t)

	ctx, cancel := context.WithCancel(context.Background())
	runDone := make(chan error, 1)
	go func() { runDone <- srv.Run(ctx) }()

	// 轮询 /health 直到可用（最多 3s）。
	url := "http://" + srv.Addr + "/health"
	var ok bool
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
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
	case err := <-runDone:
		if err != nil {
			t.Fatalf("Run 应返回 nil，got %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Run 未在 ctx 取消后 5s 内退出")
	}
}

func TestPublishWorkerTrackingWaitsForCompletion(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	doneWorker := srv.beginWorker()
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

func TestPublishRecoveryLifecycleStopsBeforeWorkerWait(t *testing.T) {
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	ctx, cancel := context.WithCancel(context.Background())
	srv.StartPublishBatchRecovery(ctx)
	cancel()
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
