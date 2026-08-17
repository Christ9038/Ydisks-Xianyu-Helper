package orders

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// reconciliationRecoveryFake 是可观测补偿扫描器测试替身。
type reconciliationRecoveryFake struct {
	// started 通知扫描循环已经启动。
	started chan struct{}
	// stopped 通知扫描循环收到取消并退出。
	stopped chan struct{}
	// once 防止测试替身重复关闭通知 channel。
	once sync.Once
}

// Run 等待 Context 取消，模拟真实补偿服务的长期扫描循环。
func (f *reconciliationRecoveryFake) Run(ctx context.Context) {
	close(f.started)
	<-ctx.Done()
	f.once.Do(func() { close(f.stopped) })
}

// TestReconciliationRecoveryCoordinatorLifecycle 验证补偿扫描器启动、停止、等待和重复关闭语义。
func TestReconciliationRecoveryCoordinatorLifecycle(t *testing.T) {
	// recovery 保存可观测补偿扫描器替身。
	recovery := &reconciliationRecoveryFake{started: make(chan struct{}), stopped: make(chan struct{})}
	// coordinator 保存当前补偿扫描生命周期协调器。
	// coordinator 保存补偿扫描生命周期协调器；err 保存构造失败原因。
	coordinator, err := NewReconciliationRecoveryCoordinator(recovery)
	if err != nil {
		t.Fatalf("构造补偿扫描协调器失败: %v", err)
	}
	// err 保存首次启动补偿扫描器的生命周期错误。
	if err := coordinator.Start(context.Background()); err != nil {
		t.Fatalf("启动补偿扫描失败: %v", err)
	}
	select {
	case <-recovery.started:
	case <-time.After(time.Second):
		t.Fatal("补偿扫描未启动")
	}
	// err 保存重复启动补偿扫描器的生命周期错误。
	if err := coordinator.Start(context.Background()); err != nil {
		t.Fatalf("重复启动应保持幂等: %v", err)
	}
	// err 保存首次关闭补偿扫描器的生命周期错误。
	if err := coordinator.Close(context.Background()); err != nil {
		t.Fatalf("关闭补偿扫描失败: %v", err)
	}
	select {
	case <-recovery.stopped:
	case <-time.After(time.Second):
		t.Fatal("补偿扫描未收到停止信号")
	}
	coordinator.Wait()
	if !errors.Is(coordinator.Start(context.Background()), ErrReconciliationRecoveryStopped) {
		t.Fatal("关闭后的补偿扫描器应拒绝再次启动")
	}
	// err 保存重复关闭补偿扫描器的生命周期错误。
	if err := coordinator.Close(context.Background()); err != nil {
		t.Fatalf("重复关闭应保持幂等: %v", err)
	}
}

// TestReconciliationRecoveryCoordinatorRequiresDependencies 验证缺失依赖、Context 和关闭参数会快速失败。
func TestReconciliationRecoveryCoordinatorRequiresDependencies(t *testing.T) {
	// missingErr 保存缺少补偿扫描器时的构造错误。
	_, missingErr := NewReconciliationRecoveryCoordinator(nil)
	if missingErr == nil {
		t.Fatal("缺失补偿扫描器应构造失败")
	}
	// missingContext 表示调用方未提供生命周期上下文，验证协调器的 fail-closed 行为。
	var missingContext context.Context
	// coordinator、err 保存可观测补偿扫描器替身及其构造错误。
	coordinator, err := NewReconciliationRecoveryCoordinator(&reconciliationRecoveryFake{started: make(chan struct{}), stopped: make(chan struct{})})
	if err != nil {
		t.Fatalf("构造补偿扫描协调器失败: %v", err)
	}
	// err 保存缺少启动 Context 时的生命周期错误。
	if err := coordinator.Start(missingContext); err == nil {
		t.Fatal("缺失启动 Context 应失败")
	}
	// err 保存缺少关闭 Context 时的生命周期错误。
	if err := coordinator.Close(missingContext); err == nil {
		t.Fatal("缺失关闭 Context 应失败")
	}
}
