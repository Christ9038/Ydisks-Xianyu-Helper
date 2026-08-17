package engine

import (
	"context"
	"log/slog"
	"testing"
	"time"
)

// TestPendingRenewalCoordinatorWaitContextBoundsAndRetries 验证迟到续期任务未响应取消时，等待受调用方预算约束，
// 且超时不会创建旁路等待协程，后续调用仍能订阅同一个完成信号并在任务收束后成功 Join。
func TestPendingRenewalCoordinatorWaitContextBoundsAndRetries(t *testing.T) {
	// coordinator 是待验证的迟到续期任务 owner。
	coordinator := pendingRenewalCoordinator{}
	coordinator.beginTask()
	// timeoutCtx、timeoutCancel 提供刻意很短的关闭预算，用于模拟底层等待不响应取消。
	timeoutCtx, timeoutCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer timeoutCancel()
	// waitStarted 记录本次有限等待的起始时间，以确认它不会无限阻塞。
	waitStarted := time.Now()
	if coordinator.waitContext(timeoutCtx) {
		t.Fatal("未完成的迟到续期任务不应在关闭预算内被误判为已退出")
	}
	// elapsed 是有限等待实际占用的时长，必须明显小于测试允许的上界。
	elapsed := time.Since(waitStarted)
	if elapsed > 300*time.Millisecond {
		t.Fatalf("迟到续期 Join 未受上下文限制: elapsed=%s", elapsed)
	}
	// retried 表示第二次 Join 的结果；它必须继续等待原任务完成，而非复用已超时的等待结果。
	retried := make(chan bool, 1)
	go func() {
		retried <- coordinator.waitContext(context.Background())
	}()
	select {
	case <-retried:
		t.Fatal("迟到续期任务未完成时，重试 Join 不应提前返回")
	case <-time.After(20 * time.Millisecond):
	}
	coordinator.finishTask()
	select {
	// joined 是重试 Join 在任务完成后收到的结果。
	case joined := <-retried:
		if !joined {
			t.Fatal("迟到续期任务收束后，重试 Join 应返回成功")
		}
	case <-time.After(time.Second):
		t.Fatal("迟到续期任务收束后，重试 Join 未返回")
	}
}

// TestConnectionCoordinatorWaitForOwnedWorkersUsesSingleBoundedContext 验证连接协调器对 recorder 与迟到续期任务
// 使用同一个关闭 Context；任一不可响应任务都不能令 Run 的 defer 永久阻塞。
func TestConnectionCoordinatorWaitForOwnedWorkersUsesSingleBoundedContext(t *testing.T) {
	// recorder 是模拟底层写入无法响应取消的记录 worker；done 不关闭表示它尚未退出。
	recorder := &wsRecorder{done: make(chan struct{})}
	recorder.started.Store(true)
	// account 只装配等待逻辑所需的组件与日志依赖，不启动真实账号运行时。
	account := &Account{
		accountRuntimeComponents: accountRuntimeComponents{},
		accountDependencies:      accountDependencies{logger: slog.Default(), recorder: recorder},
	}
	// account.pendingRenewal 是模拟仍在等待外部迟到响应的任务 owner。
	account.pendingRenewal.beginTask()
	// coordinator 是待验证的连接生命周期协调器。
	coordinator := connectionCoordinator{account: account}
	// shutdownCtx、shutdownCancel 提供总关闭预算；两个 worker 都不结束时应由该预算统一终止等待。
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer shutdownCancel()
	// waitStarted 记录协调器开始收束自有 worker 的时间。
	waitStarted := time.Now()
	coordinator.waitForOwnedWorkers(shutdownCtx)
	// elapsed 是总 Join 时间；共享 Context 要求它不能叠加成两倍等待预算。
	elapsed := time.Since(waitStarted)
	if elapsed > 300*time.Millisecond {
		t.Fatalf("连接协调器等待自有 worker 超出总预算: elapsed=%s", elapsed)
	}
	// recorderDone 解除模拟的 recorder worker，避免测试保留未关闭的完成信号。
	recorderDone := recorder.done
	close(recorderDone)
	account.pendingRenewal.finishTask()
}
