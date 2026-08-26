package orders

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRefreshJobRunnerCoversConstructionAndGuards 覆盖运行器构造及同步入口的参数校验。
func TestRefreshJobRunnerCoversConstructionAndGuards(t *testing.T) {
	// refresher 保存最小可执行刷新业务。
	refresher := &refreshRunnerTestRefresher{}
	// repository 保存最小任务仓储。
	repository := completeAppliedRepository()
	// err 保存缺少仓储端口的构造错误。
	if _, err := NewRefreshJobRunner(nil, refresher, RefreshJobRunnerOptions{}); err == nil {
		t.Fatal("空仓储未被拒绝")
	}
	// err 保存缺少刷新业务端口的构造错误。
	if _, err := NewRefreshJobRunner(repository, nil, RefreshJobRunnerOptions{}); err == nil {
		t.Fatal("空刷新业务端口未被拒绝")
	}
	// runner 保存构造完成的运行器。
	runner, err := NewRefreshJobRunner(repository, refresher, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造运行器失败: %v", err)
	}
	// job 保存用于参数校验的任务。
	job := &RefreshJob{ID: "job-1"}
	// err 保存空生命周期 Context 的启动错误。
	if err := runner.StartJob(nilOrdersContext(), job, "token"); err == nil {
		t.Fatal("StartJob 未拒绝空生命周期 Context")
	}
	// err 保存空任务的启动错误。
	if err := runner.StartJob(context.Background(), nil, "token"); !errors.Is(err, ErrRefreshJobRunnerInvalidJob) {
		t.Fatalf("StartJob 空任务错误异常: %v", err)
	}
	// err 保存空令牌的启动错误。
	if err := runner.StartJob(context.Background(), job, ""); !errors.Is(err, ErrRefreshJobRunnerInvalidJob) {
		t.Fatalf("StartJob 空令牌错误异常: %v", err)
	}
	// err 保存空执行 Context 的任务错误。
	if err := runner.RunJob(nilOrdersContext(), job, "token"); err == nil {
		t.Fatal("RunJob 未拒绝空 Context")
	}
	// err 保存空任务的同步执行错误。
	if err := runner.RunJob(context.Background(), nil, "token"); !errors.Is(err, ErrRefreshJobRunnerInvalidJob) {
		t.Fatalf("RunJob 空任务错误异常: %v", err)
	}
	// err 保存空令牌的同步执行错误。
	if err := runner.RunJob(context.Background(), job, ""); !errors.Is(err, ErrRefreshJobRunnerInvalidJob) {
		t.Fatalf("RunJob 空令牌错误异常: %v", err)
	}
	// nilRunner 保存用于 nil 接收者方法覆盖的运行器。
	var nilRunner *RefreshJobRunner
	// err 保存 nil 运行器启动错误。
	if err := nilRunner.StartJob(context.Background(), job, "token"); err == nil || nilRunner.CancelJob("job-1") {
		t.Fatal("nil 运行器方法未返回保护性结果")
	}
	// err 保存 nil 运行器同步任务错误。
	if err := nilRunner.RunJob(context.Background(), job, "token"); err == nil {
		t.Fatal("nil 运行器同步方法未返回错误")
	}
	// err 保存 nil 运行器恢复扫描错误。
	if err := nilRunner.RunRecovery(context.Background()); err == nil {
		t.Fatal("nil 运行器恢复方法未返回错误")
	}
	// err 保存 nil 运行器恢复启动错误。
	if err := nilRunner.StartRecovery(context.Background()); err == nil {
		t.Fatal("nil 运行器恢复方法未返回错误")
	}
	if nilRunner.Close(context.Background()) != nil {
		t.Fatal("nil 运行器 Close 不应报错")
	}
	nilRunner.Stop()
	nilRunner.Wait()
}

// TestRefreshJobRunnerCoversRunJobCompletionFailures 覆盖刷新业务失败、序列化失败和终态仓储错误。
func TestRefreshJobRunnerCoversRunJobCompletionFailures(t *testing.T) {
	// job 保存各同步执行用例复用的任务。
	job := &RefreshJob{ID: "job-1", UserID: 7}
	// businessErr 保存刷新业务根因。
	businessErr := errors.New("业务刷新失败")
	// businessRepository 保存可命中失败终态的仓储。
	businessRepository := completeAppliedRepository()
	// businessRunner 保存返回业务错误的运行器。
	businessRunner, err := NewRefreshJobRunner(businessRepository, &refreshRunnerTestRefresher{err: businessErr}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造业务失败运行器: %v", err)
	}
	if !errors.Is(businessRunner.RunJob(context.Background(), job, "token-business"), businessErr) {
		t.Fatal("业务错误未从 RunJob 返回")
	}

	// marshalErr 保存结果编码错误。
	marshalErr := errors.New("结果编码失败")
	// marshalRunner 保存注入编码失败函数的运行器。
	marshalRunner, err := NewRefreshJobRunner(completeAppliedRepository(), &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{MarshalResult: func(RefreshJobResult) ([]byte, error) {
		return nil, marshalErr
	}})
	if err != nil {
		t.Fatalf("构造编码失败运行器: %v", err)
	}
	if !errors.Is(marshalRunner.RunJob(context.Background(), job, "token-marshal"), marshalErr) {
		t.Fatal("编码错误未从 RunJob 返回")
	}

	// completeErr 保存终态写入仓储错误。
	completeErr := errors.New("终态保存失败")
	// completeRepository 保存返回终态错误的仓储。
	completeRepository := completeAppliedRepository()
	completeRepository.completeErr = completeErr
	// completeRunner 保存业务成功但终态失败的运行器。
	completeRunner, err := NewRefreshJobRunner(completeRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造终态失败运行器: %v", err)
	}
	if !errors.Is(completeRunner.RunJob(context.Background(), job, "token-complete"), completeErr) {
		t.Fatal("终态仓储错误未从 RunJob 返回")
	}
	// err 保存空 Context 的终态写入错误。
	if err := completeRunner.complete(nilOrdersContext(), "job-1", "token", "failed", "{}", "error"); err == nil {
		t.Fatal("complete 未拒绝空 Context")
	}
}

// TestRefreshJobRunnerCoversRecoveryDecisions 覆盖恢复扫描的查询、重新入队、抢占和启动补偿分支。
func TestRefreshJobRunnerCoversRecoveryDecisions(t *testing.T) {
	// fixedNow 保存恢复扫描使用的确定性时间。
	fixedNow := time.Date(2026, 8, 26, 12, 0, 0, 0, time.UTC)
	// recoverErr 保存恢复查询错误。
	recoverErr := errors.New("恢复查询失败")
	// recoverRepository 保存返回恢复查询错误的仓储。
	recoverRepository := completeAppliedRepository()
	recoverRepository.recoverErr = recoverErr
	// recoverRunner 保存恢复查询错误运行器。
	recoverRunner, err := NewRefreshJobRunner(recoverRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{Now: func() time.Time { return fixedNow }, RecoveryBatchSize: 3})
	if err != nil {
		t.Fatalf("构造恢复查询错误运行器: %v", err)
	}
	if !errors.Is(recoverRunner.RunRecovery(context.Background()), recoverErr) {
		t.Fatal("恢复查询错误未返回")
	}

	// jobs 保存恢复扫描要逐个决策的任务。
	jobs := []RefreshJob{{ID: "requeue-false"}, {ID: "claim-false"}, {ID: "claim-error"}}
	// requeueFalseRepository 保存重新入队未生效的仓储。
	requeueFalseRepository := completeAppliedRepository()
	requeueFalseRepository.jobs = jobs[:1]
	requeueFalseRepository.requeueApplied = boolPtr(false)
	// requeueFalseRunner 保存重新入队未生效运行器。
	requeueFalseRunner, err := NewRefreshJobRunner(requeueFalseRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatalf("构造重新入队失败运行器: %v", err)
	}
	// err 保存重新入队未生效扫描错误。
	if err := requeueFalseRunner.RunRecovery(context.Background()); err != nil || len(requeueFalseRepository.claimCalls) != 0 {
		t.Fatalf("重新入队未生效分支异常: err=%v claims=%v", err, requeueFalseRepository.claimCalls)
	}

	// claimFalseRepository 保存抢占未命中的仓储。
	claimFalseRepository := completeAppliedRepository()
	claimFalseRepository.jobs = jobs[1:2]
	claimFalseRepository.claimApplied = boolPtr(false)
	// claimFalseRunner 保存抢占未命中运行器。
	claimFalseRunner, err := NewRefreshJobRunner(claimFalseRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatalf("构造抢占未命中运行器: %v", err)
	}
	// err 保存抢占未命中扫描错误。
	if err := claimFalseRunner.RunRecovery(context.Background()); err != nil || len(claimFalseRepository.claimCalls) != 1 {
		t.Fatalf("抢占未命中分支异常: err=%v claims=%v", err, claimFalseRepository.claimCalls)
	}

	// claimErr 保存抢占错误。
	claimErr := errors.New("抢占失败")
	// claimErrorRepository 保存抢占错误仓储。
	claimErrorRepository := completeAppliedRepository()
	claimErrorRepository.jobs = jobs[2:]
	claimErrorRepository.claimErr = claimErr
	// claimErrorRunner 保存抢占错误运行器。
	claimErrorRunner, err := NewRefreshJobRunner(claimErrorRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{Now: func() time.Time { return fixedNow }})
	if err != nil {
		t.Fatalf("构造抢占错误运行器: %v", err)
	}
	// err 保存抢占错误扫描结果。
	if err := claimErrorRunner.RunRecovery(context.Background()); err != nil {
		t.Fatalf("抢占错误应被本轮跳过: %v", err)
	}

	// callbackCalls 保存恢复启动失败回调次数。
	callbackCalls := 0
	// startFailureRepository 保存已停止运行器的恢复任务仓储。
	startFailureRepository := completeAppliedRepository()
	startFailureRepository.jobs = []RefreshJob{{ID: "start-failure"}}
	// startFailureRunner 保存启动失败并带补偿回调的运行器。
	startFailureRunner, err := NewRefreshJobRunner(startFailureRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{Now: func() time.Time { return fixedNow }, NewToken: func() string { return "token-start-failure" }, OnRecoveryError: func(error) { callbackCalls++ }})
	if err != nil {
		t.Fatalf("构造启动失败运行器: %v", err)
	}
	startFailureRunner.Stop()
	// err 保存恢复任务启动补偿结果。
	if err := startFailureRunner.RunRecovery(context.Background()); err != nil || callbackCalls != 1 || len(startFailureRepository.completeCalls) != 1 {
		t.Fatalf("恢复启动失败补偿异常: err=%v callbacks=%d completes=%v", err, callbackCalls, startFailureRepository.completeCalls)
	}

	// canceledContext 保存已取消的恢复扫描 Context。
	canceledContext, cancel := context.WithCancel(context.Background())
	cancel()
	recoverRepository.recoverErr = nil
	recoverRepository.jobs = []RefreshJob{{ID: "cancel-check"}}
	// err 保存已取消 Context 的恢复扫描错误。
	if err := recoverRunner.RunRecovery(canceledContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("已取消恢复扫描未返回 Context 错误: %v", err)
	}
}

// TestRefreshJobRunnerCoversRecoveryLifecycle 覆盖恢复循环启动、重复启动、取消和关闭超时分支。
func TestRefreshJobRunnerCoversRecoveryLifecycle(t *testing.T) {
	// repository 保存恢复循环的最小仓储。
	repository := completeAppliedRepository()
	// runner 保存短恢复间隔运行器。
	runner, err := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{RecoveryInterval: time.Millisecond})
	if err != nil {
		t.Fatalf("构造生命周期运行器: %v", err)
	}
	// err 保存空恢复生命周期 Context 错误。
	if err := runner.StartRecovery(nilOrdersContext()); err == nil {
		t.Fatal("StartRecovery 未拒绝空 Context")
	}
	// err 保存首次启动恢复循环错误。
	if err := runner.StartRecovery(context.Background()); err != nil {
		t.Fatalf("启动恢复循环失败: %v", err)
	}
	// err 保存重复启动恢复循环错误。
	if err := runner.StartRecovery(context.Background()); err != nil {
		t.Fatalf("重复启动恢复循环不应失败: %v", err)
	}
	// closeCtx 限制恢复循环关闭等待时间。
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// err 保存恢复循环关闭错误。
	if err := runner.Close(closeCtx); err != nil {
		t.Fatalf("关闭恢复循环失败: %v", err)
	}

	// stoppedRunner 保存已经停止的运行器。
	stoppedRunner, err := NewRefreshJobRunner(completeAppliedRepository(), &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造停止运行器失败: %v", err)
	}
	stoppedRunner.Stop()
	// err 保存停止运行器的恢复启动错误。
	if err := stoppedRunner.StartRecovery(context.Background()); !errors.Is(err, ErrRefreshJobRunnerStopped) {
		t.Fatalf("停止后恢复启动错误异常: %v", err)
	}
	// err 保存空 Context 的关闭错误。
	if err := stoppedRunner.Close(nilOrdersContext()); err == nil {
		t.Fatal("Close 未拒绝空 Context")
	}

	// timeoutRunner 保存仍在执行的后台 worker。
	timeoutRunner, err := NewRefreshJobRunner(completeAppliedRepository(), &refreshRunnerTestRefresher{waitForCancel: true}, RefreshJobRunnerOptions{JobTimeout: time.Hour})
	if err != nil {
		t.Fatalf("构造超时运行器失败: %v", err)
	}
	// err 保存超时 worker 的启动错误。
	if err := timeoutRunner.StartJob(context.Background(), &RefreshJob{ID: "timeout-job"}, "timeout-token"); err != nil {
		t.Fatalf("启动超时 worker 失败: %v", err)
	}
	// expiredContext 保存立即超时的关闭 Context。
	expiredContext, expire := context.WithCancel(context.Background())
	expire()
	// err 保存已过期关闭 Context 的错误。
	if err := timeoutRunner.Close(expiredContext); !errors.Is(err, context.Canceled) {
		t.Fatalf("Close 超时错误异常: %v", err)
	}
	// err 保存二次关闭运行器的错误。
	if err := timeoutRunner.Close(context.Background()); err != nil {
		t.Fatalf("二次 Close 未完成: %v", err)
	}
}
