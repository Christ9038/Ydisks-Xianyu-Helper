package renewal

import (
	"context"
	"testing"

	"xianyu-go/internal/db"
)

// TestSchedulerBoundaryHelpersCoverCanceledPersistenceAndUnknownSettings 验证续期辅助方法的取消、缺失和未知设置分支。
func TestSchedulerBoundaryHelpersCoverCanceledPersistenceAndUnknownSettings(t *testing.T) {
	// store、cleanup 保存本地续期仓储及关闭函数。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// scheduler 保存本地数据库依赖的续期调度器。
	scheduler := NewScheduler(store, nil, nil, nil)
	// account 是数据库中存在的续期账号。
	account := createSchedulerAccount(t, store, "coverage-boundary", "unb=1")
	// canceledContext 表示所有本次数据库操作都应立即取消的上下文。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()

	// missingAccount、missingErr 分别表示重读不存在账号的结果与错误。
	missingAccount, missingErr := scheduler.reloadRenewalAccount(canceledContext, db.RenewalRuntimeAccount{ID: "missing"})
	if missingErr == nil || missingAccount.ID != "" {
		t.Fatalf("不存在账号应返回错误空模型：account=%+v err=%v", missingAccount, missingErr)
	}
	// scheduler.cleanupExpiredLogs 使用取消上下文验证清理失败仅记录日志。
	scheduler.cleanupExpiredLogs(canceledContext)
	// scheduler.wakeCredentialBlockedAutomation 使用取消上下文验证唤醒失败仅记录日志。
	scheduler.wakeCredentialBlockedAutomation(canceledContext, account.ID)

	// unknownSettingErr 表示写入未知开关值的错误。
	unknownSettingErr := store.Settings.Set(context.Background(), "coverage.unknown.enabled", "maybe")
	if unknownSettingErr != nil {
		t.Fatal(unknownSettingErr)
	}
	if !scheduler.settingEnabled(context.Background(), "coverage.unknown.enabled", true) {
		t.Fatal("未知开关值应保留 true 默认值")
	}
	if scheduler.settingEnabled(context.Background(), "coverage.unknown.enabled", false) {
		t.Fatal("未知开关值应保留 false 默认值")
	}
}

// TestSchedulerLifecycleRejectsNilContexts 验证显式生命周期入口拒绝空 Context，并保持空接收者幂等。
func TestSchedulerLifecycleRejectsNilContexts(t *testing.T) {
	// scheduler 是尚未启动的本地调度器。
	scheduler := NewScheduler(nil, nil, nil, nil)
	// nilContext 表示调用方遗漏生命周期 Context 的非法输入。
	var nilContext context.Context
	// waitErr 表示等待入口拒绝空 Context 的错误。
	waitErr := scheduler.WaitContext(nilContext)
	if waitErr == nil {
		t.Fatal("WaitContext 应拒绝空 Context")
	}
	// stopErr 表示停止入口拒绝空 Context 的错误。
	stopErr := scheduler.StopContext(nilContext)
	if stopErr == nil {
		t.Fatal("StopContext 应拒绝空 Context")
	}
	// nilScheduler 验证空接收者的显式等待仍然安全返回。
	var nilScheduler *Scheduler
	// nilSchedulerErr 表示空调度器等待的错误结果。
	nilSchedulerErr := nilScheduler.WaitContext(nilContext)
	if nilSchedulerErr != nil {
		t.Fatalf("空调度器 WaitContext 应幂等：%v", nilSchedulerErr)
	}
}
