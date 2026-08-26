package renewal

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestSleepContextHonorsZeroDelayAndCancellation 覆盖续期调度等待的零延迟与取消路径。
func TestSleepContextHonorsZeroDelayAndCancellation(t *testing.T) {
	// zeroErr 保存零延迟等待结果。
	zeroErr := sleepCtx(context.Background(), 0)
	if zeroErr != nil {
		t.Fatalf("zero delay error=%v", zeroErr)
	}
	// canceledContext 保存已经取消的等待上下文。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	// canceledErr 保存取消后的等待错误。
	canceledErr := sleepCtx(canceledContext, 0)
	if canceledErr != nil {
		t.Fatalf("zero delay should return immediately, err=%v", canceledErr)
	}
	// canceledContextWithDelay 保存带短延迟且已取消的等待上下文。
	canceledContextWithDelay, cancelWithDelay := context.WithCancel(context.Background())
	cancelWithDelay()
	// delayedErr 保存带延迟等待被取消后的错误。
	delayedErr := sleepCtx(canceledContextWithDelay, time.Millisecond)
	if !errors.Is(delayedErr, context.Canceled) {
		t.Fatalf("canceled delay error=%v", delayedErr)
	}
}

// TestSchedulerSessionCooldownDelegatesToManager 验证调度器对账号会话冷却状态的委托读取。
func TestSchedulerSessionCooldownDelegatesToManager(t *testing.T) {
	// scheduler 是使用独立冷却管理器的最小续期调度器。
	scheduler := NewScheduler(nil, nil, nil, nil)
	scheduler.cooldown = NewCooldownManager()
	if scheduler.isSessionCooled("cid") {
		t.Fatal("未标记账号不应处于冷却")
	}
	scheduler.markSessionExpired("cid")
	if !scheduler.isSessionCooled("cid") {
		t.Fatal("标记会话失效后应处于冷却")
	}
}
