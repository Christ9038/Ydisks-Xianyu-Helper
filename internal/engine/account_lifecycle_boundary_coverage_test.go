package engine

import (
	"context"
	"strings"
	"testing"
)

// TestAccountLifecycleRejectsNilAndCanceledContexts 验证生命周期边界拒绝空 Context 与已取消任务。
func TestAccountLifecycleRejectsNilAndCanceledContexts(t *testing.T) {
	// lifecycle 保存尚未启动但允许测试任务登记的生命周期组件。
	lifecycle := accountLifecycle{accepting: true}
	// nilContext 表示调用方未提供停止上下文的非法输入。
	var nilContext context.Context
	// cancelErr 表示空停止上下文应返回的参数错误。
	_, shouldStop, cancelErr := lifecycle.stopContext(nilContext)
	if shouldStop || cancelErr == nil {
		t.Fatalf("空 Context 应拒绝停止：shouldStop=%v err=%v", shouldStop, cancelErr)
	}

	// runContext 是用于验证已取消任务不会被接纳的运行上下文。
	runContext, runCancel := context.WithCancel(context.Background())
	runCancel()
	// started 表示生命周期是否成功进入运行状态。
	if !lifecycle.start(runContext, runCancel) {
		t.Fatal("生命周期首次启动应成功")
	}
	// taskContext、finish、accepted 分别是被拒绝任务的上下文、释放函数与接纳结果。
	taskContext, finish, accepted := lifecycle.beginTask()
	if accepted || taskContext != nil || finish != nil {
		t.Fatalf("已取消运行上下文不应接纳任务：ctx=%v finishNil=%v accepted=%v", taskContext, finish == nil, accepted)
	}

	// stopContext 再次验证可正常完成停止切换，避免前置非法输入改变生命周期状态。
	_, shouldStop, stopErr := lifecycle.stopContext(context.Background())
	if !shouldStop || stopErr != nil {
		t.Fatalf("有效停止应成功：shouldStop=%v err=%v", shouldStop, stopErr)
	}
	// waitingLifecycle 保存另一个仍有活动任务的生命周期，用于验证等待上下文取消分支。
	waitingLifecycle := accountLifecycle{}
	// waitingRunContext 是等待生命周期使用的共享运行上下文。
	waitingRunContext, waitingRunCancel := context.WithCancel(context.Background())
	defer waitingRunCancel()
	if !waitingLifecycle.start(waitingRunContext, waitingRunCancel) {
		t.Fatal("等待生命周期应成功启动")
	}
	// waitingTaskContext、waitingFinish、waitingAccepted 分别是等待任务的上下文、释放函数与接纳结果。
	waitingTaskContext, waitingFinish, waitingAccepted := waitingLifecycle.beginTask()
	if !waitingAccepted || waitingTaskContext == nil || waitingFinish == nil {
		t.Fatal("等待任务应成功登记")
	}
	// waitingStopErr 表示等待生命周期进入停止状态时的错误结果。
	_, waitingShouldStop, waitingStopErr := waitingLifecycle.stopContext(context.Background())
	if !waitingShouldStop || waitingStopErr != nil {
		t.Fatalf("等待生命周期停止失败：shouldStop=%v err=%v", waitingShouldStop, waitingStopErr)
	}
	// canceledStopContext 表示首次停止尚未收束时再次停止所使用的已取消上下文。
	canceledStopContext, canceledStopCancel := context.WithCancel(context.Background())
	canceledStopCancel()
	// canceledStopErr 表示并发停止等待被取消后的错误。
	_, repeatedShouldStop, canceledStopErr := waitingLifecycle.stopContext(canceledStopContext)
	if repeatedShouldStop || canceledStopErr != context.Canceled {
		t.Fatalf("重复停止应响应已取消上下文：shouldStop=%v err=%v", repeatedShouldStop, canceledStopErr)
	}
	// canceledWaitContext 表示活动任务尚未完成时已取消的等待上下文。
	canceledWaitContext, waitCancel := context.WithCancel(context.Background())
	waitCancel()
	if waitingLifecycle.waitContext(canceledWaitContext) {
		t.Fatal("已取消等待上下文不应伪造任务完成")
	}
	if canceledWaitContext.Err() != context.Canceled {
		t.Fatalf("等待上下文错误=%v", canceledWaitContext.Err())
	}
	waitingFinish()
	// nilWaitLifecycle 验证没有停止信号的生命周期可立即视为已完成。
	nilWaitLifecycle := accountLifecycle{}
	if !nilWaitLifecycle.waitContext(context.Background()) {
		t.Fatal("没有停止信号的生命周期应立即完成等待")
	}
	// nilWaitContext 表示等待入口缺少 Context 的非法输入。
	var nilWaitContext context.Context
	if waitingLifecycle.waitContext(nilWaitContext) {
		t.Fatal("空等待 Context 不应返回完成")
	}
}

// TestAccountStopContextRejectsNilContext 验证账号停止入口不接受空 Context。
func TestAccountStopContextRejectsNilContext(t *testing.T) {
	// account 是用于验证停止参数边界的未启动账号。
	account := New(Config{CookieID: "stop-nil", CookieStr: "unb=1"})
	// nilContext 表示调用方遗漏停止上下文的非法输入。
	var nilContext context.Context
	// stopErr 表示停止入口返回的参数错误。
	stopErr := account.StopContext(nilContext)
	if stopErr == nil || !strings.Contains(stopErr.Error(), "停止账号需要关闭 Context") {
		t.Fatalf("空 Context 应返回参数错误，got %v", stopErr)
	}
}

// TestAccountStopContextWaitsForRecorderAndSupportsUnstartedAccount 验证记录器等待超时与未启动账号停止路径。
func TestAccountStopContextWaitsForRecorderAndSupportsUnstartedAccount(t *testing.T) {
	// account 是尚未 Run 但已经装配阻塞记录器的账号。
	account := New(Config{CookieID: "recorder-stop", CookieStr: "unb=1"})
	// recorder 保存一个永不主动关闭的记录器，用于验证停止上下文确实限制等待。
	account.recorder = &wsRecorder{done: make(chan struct{})}
	account.recorder.started.Store(true)
	// stopContext 是限制记录器等待时间的短停止上下文。
	stopContext, stopCancel := context.WithCancel(context.Background())
	stopCancel()
	// stopErr 表示记录器尚未退出时的停止错误。
	stopErr := account.StopContext(stopContext)
	if stopErr != context.Canceled {
		t.Fatalf("记录器等待应返回 Context 错误：got=%v", stopErr)
	}

	// unstartedAccount 是没有记录器且尚未进入 Run 的独立账号。
	unstartedAccount := New(Config{CookieID: "unstarted-stop", CookieStr: "unb=1"})
	// unstartedStopErr 表示未启动账号直接停止的错误结果。
	unstartedStopErr := unstartedAccount.StopContext(context.Background())
	if unstartedStopErr != nil {
		t.Fatalf("未启动账号应可直接停止：%v", unstartedStopErr)
	}
}
