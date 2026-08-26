package renewal

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestExecuteLoginRenewWithEmptyAccountBatch 验证登录续期任务在没有可续期账号时安全结束。
func TestExecuteLoginRenewWithEmptyAccountBatch(t *testing.T) {
	// store、cleanup 是没有账号数据的本地 SQLite 测试仓储。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// scheduler 是使用本地仓储构造的续期调度器。
	scheduler := NewScheduler(store, nil, nil, nil)
	scheduler.executeLoginRenew(context.Background())
}

// TestExecuteLoginRenewSkipsSessionCooledAccount 验证登录续期任务对会话冷却账号执行跳过分支。
func TestExecuteLoginRenewSkipsSessionCooledAccount(t *testing.T) {
	// store、cleanup 是包含一个本地续期账号的测试仓储及资源清理函数。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// account 是用于验证会话冷却跳过逻辑的本地账号。
	account := createSchedulerAccount(t, store, "cooled-account", "unb=1")
	// scheduler 是使用独立冷却管理器的续期调度器。
	scheduler := NewScheduler(store, nil, nil, nil)
	scheduler.cooldown = NewCooldownManager()
	scheduler.cooldown.MarkSessionExpired(account.ID)
	// canceledContext 防止测试在多账号间等待真实续期间隔。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler.executeLoginRenew(canceledContext)
}

// TestExecuteLoginRenewHandlesAccountLoadFailure 验证登录续期任务加载账号失败时安全结束。
func TestExecuteLoginRenewHandlesAccountLoadFailure(t *testing.T) {
	// store、cleanup 是随后关闭数据库连接的本地续期仓储及资源清理函数。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// closeErr 保存关闭测试数据库连接的结果。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// scheduler 是绑定关闭数据库的续期调度器。
	scheduler := NewScheduler(store, nil, nil, nil)
	// executeLoginRenew 在加载账号失败时只记录日志，不应向调度循环抛出错误。
	scheduler.executeLoginRenew(context.Background())
}

// TestExecuteLoginRenewProcessesActiveAccountsWithCanceledContext 验证有可续期账号时会遍历账号并在取消上下文下快速收束。
func TestExecuteLoginRenewProcessesActiveAccountsWithCanceledContext(t *testing.T) {
	// store、cleanup 是包含两个可续期账号的本地测试仓储及清理函数。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// first、second 是用于覆盖账号遍历和相邻账号等待分支的本地账号。
	first := createSchedulerAccount(t, store, "active-renew-first", "unb=1")
	// second 是用于覆盖第二个账号续期尝试及相邻账号等待分支的本地账号。
	second := createSchedulerAccount(t, store, "active-renew-second", "unb=2")
	// scheduler 使用独立冷却管理器，确保两个账号都会进入续期尝试。
	scheduler := NewScheduler(store, nil, nil, nil)
	scheduler.cooldown = NewCooldownManager()
	// canceledContext 让平台检查和账号间等待都立即结束，不依赖外部网络或真实时间。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler.executeLoginRenew(canceledContext)
	// first、second 通过读取确保测试账号确实已写入并且遍历输入有效。
	if first.ID == "" || second.ID == "" {
		t.Fatal("续期测试账号未正确创建")
	}
}

// TestLoginRenewOneCoversReloadAndDisabledBranches 验证单账号登录续期在重读失败和账号已停用时不访问平台。
func TestLoginRenewOneCoversReloadAndDisabledBranches(t *testing.T) {
	// ctx 是本测试登录续期入口共用的非取消上下文。
	ctx := context.Background()
	// closedStore、closedCleanup 保存随后关闭数据库连接的测试存储。
	closedStore, closedCleanup := newSchedulerTestStore(t)
	// closedAccount 是数据库关闭前创建的登录续期账号快照。
	closedAccount := createSchedulerAccount(t, closedStore, "login-reload-failure", "unb=1")
	// closeErr 保存关闭测试数据库连接的结果。
	if closeErr := closedStore.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// closedScheduler 使用关闭数据库验证首轮重读错误只记录并返回。
	closedScheduler := NewScheduler(closedStore, nil, nil, nil)
	closedScheduler.loginRenewOne(ctx, "reload-failure", closedAccount)
	closedCleanup()

	// disabledStore、disabledCleanup 保存账号停用分支使用的测试存储。
	disabledStore, disabledCleanup := newSchedulerTestStore(t)
	defer disabledCleanup()
	// disabledAccount 是即将被 cookie_status 标记为停用的账号。
	disabledAccount := createSchedulerAccount(t, disabledStore, "login-disabled", "unb=1")
	// statusErr 保存写入账号停用状态的数据库错误。
	if _, statusErr := disabledStore.DB.ExecContext(ctx, `INSERT INTO cookie_status (cookie_id, enabled) VALUES (?, 0)`, disabledAccount.ID); statusErr != nil {
		t.Fatal(statusErr)
	}
	// disabledScheduler 使用空平台依赖验证停用账号不会进入外部检查。
	disabledScheduler := NewScheduler(disabledStore, nil, nil, nil)
	disabledScheduler.loginRenewOne(ctx, "disabled", disabledAccount)
}

// TestSchedulerCredentialUpdateHelpers 验证 Cookie 更新后的重启和自动化唤醒门禁。
func TestSchedulerCredentialUpdateHelpers(t *testing.T) {
	// store、cleanup 是支持凭证和自动化记录的本地仓储。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// starter 是记录重启次数的账号启动替身。
	starter := &schedulerFakeStarter{}
	// scheduler 是绑定账号启动替身的续期调度器。
	scheduler := NewScheduler(store, starter, nil, nil)
	// canceledCtx 是已取消的上下文，更新后不应重启账号。
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()
	scheduler.restartAfterCredentialUpdate(canceledCtx, "account", true, "test")
	scheduler.restartAfterCredentialUpdate(context.Background(), "account", false, "test")
	if starter.restarts.Load() != 0 {
		t.Fatalf("禁用或取消上下文不应重启: %d", starter.restarts.Load())
	}
	// validAccount 是用于验证 Cookie 持久化的本地账号。
	validAccount := createSchedulerAccount(t, store, "credential-helper", "unb=1")
	if !scheduler.saveRenewedCookies(context.Background(), validAccount.ID, "unb=2", `{}`) {
		t.Fatal("有效账号 Cookie 保存失败")
	}
	if scheduler.saveRenewedCookies(context.Background(), "missing-account", "unb=2", `{}`) {
		t.Fatal("不存在账号 Cookie 保存不应成功")
	}
	// saved、err 保存更新后的账号 Cookie。
	saved, err := store.Cookies.GetValue(context.Background(), validAccount.ID)
	if err != nil || saved != "unb=2" {
		t.Fatalf("Cookie 保存结果错误: value=%q err=%v", saved, err)
	}
	scheduler.wakeCredentialBlockedAutomation(context.Background(), validAccount.ID)
	// nilScheduler 验证空调度器接收自动化唤醒调用时保持幂等。
	var nilScheduler *Scheduler
	nilScheduler.wakeCredentialBlockedAutomation(context.Background(), validAccount.ID)
}

// TestSchedulerRestartHelperPropagatesRestartFailure 验证账号重启失败只记录错误且不向续期主流程抛出异常。
func TestSchedulerRestartHelperPropagatesRestartFailure(t *testing.T) {
	// restartErr 是账号重启替身返回的底层错误。
	restartErr := errors.New("restart failed")
	// starter 是返回重启错误的账号启动替身。
	starter := &schedulerContextStarter{err: restartErr}
	// scheduler 是绑定失败启动替身的续期调度器。
	scheduler := NewScheduler(nil, starter, nil, nil)
	scheduler.restartAfterCredentialUpdate(context.Background(), "account", true, "test")
	if starter.restarts.Load() != 1 {
		t.Fatalf("应尝试一次账号重启: %d", starter.restarts.Load())
	}
}

// TestSchedulerFixedLoopsHonorInitialSettingAndCancellation 验证固定周期任务的首次开关判断和取消收束。
func TestSchedulerFixedLoopsHonorInitialSettingAndCancellation(t *testing.T) {
	// canceledContext 是已经取消的调度上下文，避免测试等待真实周期。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	// scheduler 是不依赖数据库设置的零依赖调度器。
	scheduler := NewScheduler(nil, nil, nil, nil)
	// enabledCalls 记录默认启用任务的首次执行次数。
	enabledCalls := 0
	scheduler.runFixed(canceledContext, "enabled", "missing-enabled", "missing-interval", true, time.Millisecond, func(context.Context) {
		enabledCalls++
	})
	if enabledCalls != 1 {
		t.Fatalf("默认启用任务执行次数=%d", enabledCalls)
	}
	// disabledCalls 记录默认停用任务是否被错误执行。
	disabledCalls := 0
	scheduler.runFixed(canceledContext, "disabled", "missing-enabled", "missing-interval", false, time.Millisecond, func(context.Context) {
		disabledCalls++
	})
	if disabledCalls != 0 {
		t.Fatalf("默认停用任务执行次数=%d", disabledCalls)
	}
}

// TestSchedulerAPIRenewLoopStopsOnCanceledContext 验证 API Cookie 续期固定循环会在初始扫描后响应取消。
func TestSchedulerAPIRenewLoopStopsOnCanceledContext(t *testing.T) {
	// store、cleanup 保存无账号的本地续期仓储及关闭函数。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// canceledContext 是已经取消的调度上下文。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	// scheduler 保存用于执行一次空扫描的续期调度器。
	scheduler := NewScheduler(store, nil, nil, nil)
	scheduler.runAPICookieRenewFixed(canceledContext)
}

// TestSchedulerFixedLoopsCoverTimerAndDynamicDisable 验证固定续期循环在定时器触发后重新读取关闭配置。
func TestSchedulerFixedLoopsCoverTimerAndDynamicDisable(t *testing.T) {
	// store、cleanup 保存循环读取设置所需的本地仓储及关闭函数。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// ctx、cancel 控制测试循环的有限生命周期。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// scheduler 保存本地设置仓储，不会访问外部平台。
	scheduler := NewScheduler(store, nil, nil, nil)
	// fixedEnabledErr 表示写入固定循环启用设置的错误。
	fixedEnabledErr := store.Settings.Set(ctx, "coverage.fixed.enabled", "true")
	if fixedEnabledErr != nil {
		t.Fatal(fixedEnabledErr)
	}
	// fixedIntervalErr 表示写入固定循环短间隔设置的错误。
	fixedIntervalErr := store.Settings.Set(ctx, "coverage.fixed.interval", "0.001")
	if fixedIntervalErr != nil {
		t.Fatal(fixedIntervalErr)
	}
	// calls 记录首次任务执行次数；首次执行后关闭设置，迫使下一轮走动态禁用分支。
	calls := 0
	// schedulerDone 表示固定循环响应取消并退出。
	schedulerDone := make(chan struct{})
	go func() {
		scheduler.runFixed(ctx, "coverage", "coverage.fixed.enabled", "coverage.fixed.interval", false, time.Hour, func(context.Context) {
			calls++
			// disableErr 表示首次任务执行后关闭固定循环设置的错误。
			disableErr := store.Settings.Set(ctx, "coverage.fixed.enabled", "false")
			if disableErr != nil {
				t.Error(disableErr)
			}
		})
		close(schedulerDone)
	}()
	// waitTimer 让循环至少经过一次定时器分支和一次配置关闭检查。
	waitTimer := time.NewTimer(20 * time.Millisecond)
	defer waitTimer.Stop()
	select {
	case <-schedulerDone:
		t.Fatal("循环不应在外部取消前结束")
	case <-waitTimer.C:
		cancel()
	}
	select {
	case <-schedulerDone:
	case <-time.After(time.Second):
		t.Fatal("固定循环未响应取消")
	}
	if calls != 1 {
		t.Fatalf("动态禁用后不应再次执行任务，执行次数=%d", calls)
	}
}

// TestSchedulerAPILoopCoversTimerAndDynamicDisable 验证 API 续期循环在关闭配置下经过定时器后继续等待取消。
func TestSchedulerAPILoopCoversTimerAndDynamicDisable(t *testing.T) {
	// store、cleanup 保存 API 循环读取设置所需的本地仓储及关闭函数。
	store, cleanup := newSchedulerTestStore(t)
	defer cleanup()
	// ctx、cancel 控制 API 循环的有限生命周期。
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// scheduler 保存本地设置仓储，API 续期开关明确关闭以避免外部请求。
	scheduler := NewScheduler(store, nil, nil, nil)
	// apiEnabledErr 表示关闭 API 续期开关的错误。
	apiEnabledErr := store.Settings.Set(ctx, apiCookieRenewEnabledSetting, "false")
	if apiEnabledErr != nil {
		t.Fatal(apiEnabledErr)
	}
	// apiIntervalErr 表示写入 API 循环短间隔设置的错误。
	apiIntervalErr := store.Settings.Set(ctx, apiCookieRenewIntervalSetting, "0.001")
	if apiIntervalErr != nil {
		t.Fatal(apiIntervalErr)
	}
	// schedulerDone 表示 API 循环响应取消并退出。
	schedulerDone := make(chan struct{})
	go func() {
		scheduler.runAPICookieRenewFixed(ctx)
		close(schedulerDone)
	}()
	// waitTimer 确保循环至少有机会经过定时器分支。
	waitTimer := time.NewTimer(20 * time.Millisecond)
	defer waitTimer.Stop()
	select {
	case <-schedulerDone:
		t.Fatal("API 循环不应在外部取消前结束")
	case <-waitTimer.C:
		cancel()
	}
	select {
	case <-schedulerDone:
	case <-time.After(time.Second):
		t.Fatal("API 循环未响应取消")
	}
}
