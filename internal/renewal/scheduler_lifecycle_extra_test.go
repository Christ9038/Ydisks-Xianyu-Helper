package renewal

import (
	"context"
	"testing"
)

// TestSchedulerLegacyLifecycleEntrypoints 覆盖兼容 Stop、Wait 和空调度器入口的生命周期语义。
func TestSchedulerLegacyLifecycleEntrypoints(t *testing.T) {
	// scheduler 保存尚未运行的调度器，验证停止请求会关闭完成信号。
	scheduler := NewScheduler(nil, nil, nil, nil)
	scheduler.Stop()
	scheduler.Stop()
	scheduler.Wait()
	// err 表示已停止调度器在显式上下文等待下的完成结果。
	if err := scheduler.WaitContext(context.Background()); err != nil {
		t.Fatalf("WaitContext: %v", err)
	}
	// nilScheduler 覆盖兼容生命周期入口对空接收者的安全语义。
	var nilScheduler *Scheduler
	nilScheduler.Stop()
	nilScheduler.Wait()
	// err 表示空调度器停止上下文入口的返回结果。
	if err := nilScheduler.StopContext(context.Background()); err != nil {
		t.Fatalf("nil StopContext: %v", err)
	}
}
