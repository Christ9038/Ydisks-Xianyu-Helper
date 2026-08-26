package automation

import (
	"context"
	"testing"
)

// TestSchedulerCoversStorageFailureAndNilLifecycleGuards 验证调度器在存储失效及生命周期空对象边界下安全返回。
func TestSchedulerCoversStorageFailureAndNilLifecycleGuards(t *testing.T) {
	// store、cleanup 保存本次测试使用的自动化存储及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是调度器错误分支共用的数据库上下文。
	ctx := context.Background()
	// center 保存仍引用已关闭数据库的自动化中心，用于模拟存储层不可用。
	center := New(store, testSenderProvider{sender: &testSender{}}, nil)
	// closeErr 保存数据库关闭错误；正常关闭 SQLite 数据库应不返回错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// recoveryErr 保存恢复运行扫描的存储错误。
	if recoveryErr := (&Scheduler{center: center}).runRecoveryTasks(ctx); recoveryErr == nil {
		t.Fatal("恢复运行扫描失败时不应返回 nil")
	}
	// deferredErr 保存延迟任务扫描的存储错误。
	if deferredErr := (&Scheduler{center: center}).runDeferredTasks(ctx); deferredErr == nil {
		t.Fatal("延迟任务扫描失败时不应返回 nil")
	}
	// scheduler 保存已关闭存储的完整调度器，用于覆盖分钟级扫描的错误收口路径。
	scheduler := NewScheduler(center)
	scheduler.scan(ctx)
	scheduler.scanDeferredTasks(ctx)
	// nilScheduler 验证空接收者不会因兼容入口而触发 panic。
	var nilScheduler *Scheduler
	nilScheduler.Run(ctx)
	// waitErr 保存空调度器等待入口的返回值。
	if waitErr := nilScheduler.WaitContext(ctx); waitErr != nil {
		t.Fatalf("空调度器等待不应失败: %v", waitErr)
	}
	// emptyScheduler 验证缺少自动化中心时不会启动后台循环。
	emptyScheduler := &Scheduler{done: make(chan struct{})}
	emptyScheduler.Run(ctx)
	select {
	case <-emptyScheduler.done:
		t.Fatal("空自动化中心不应启动调度循环")
	default:
	}
	// noContextScheduler 验证等待入口拒绝无法取消的空 Context。
	noContextScheduler := &Scheduler{done: make(chan struct{})}
	// nilContext 表示故意传入的缺失取消信号。
	var nilContext context.Context
	// waitErr 保存缺失 Context 时的等待错误。
	if waitErr := noContextScheduler.WaitContext(nilContext); waitErr == nil {
		t.Fatal("nil Context 应被调度器等待入口拒绝")
	}
}
