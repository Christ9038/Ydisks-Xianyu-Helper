package orders

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRefreshRandomIdentifiersFallback 验证订单刷新标识在系统随机源失败时仍能生成非空降级值。
func TestRefreshRandomIdentifiersFallback(t *testing.T) {
	// originalRandomReader 保存包级随机读取器的原始实现，避免测试影响其他用例。
	originalRandomReader := readRefreshRandomBytes
	readRefreshRandomBytes = func([]byte) (int, error) {
		return 0, errors.New("随机源不可用")
	}
	defer func() { readRefreshRandomBytes = originalRandomReader }()
	// jobID 保存随机任务标识的降级结果。
	jobID := randomRefreshJobID()
	// token 保存随机租约令牌的降级结果。
	token := randomRefreshJobToken()
	if jobID == "" || token == "" {
		t.Fatalf("随机源失败时标识不应为空: jobID=%q token=%q", jobID, token)
	}
}

// TestRefreshRunnerCoversDefensiveCompletionBranches 验证恢复运行器的空 Context、空令牌和补偿写入错误分支。
func TestRefreshRunnerCoversDefensiveCompletionBranches(t *testing.T) {
	// repository 保存恢复扫描和终态补偿使用的内存仓储。
	repository := completeAppliedRepository()
	// runner 保存执行恢复判断的运行器。
	runner, err := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// err 保存空 Context 恢复扫描的保护性错误。
	if err := runner.RunRecovery(nilOrdersContext()); err == nil {
		t.Fatal("RunRecovery 未拒绝空 Context")
	}

	// emptyTokenRepository 保存会被重新入队但无法生成令牌的任务仓储。
	emptyTokenRepository := completeAppliedRepository()
	emptyTokenRepository.jobs = []RefreshJob{{ID: "empty-token"}}
	// emptyTokenRunner 保存令牌生成失败的恢复运行器。
	emptyTokenRunner, err := NewRefreshJobRunner(emptyTokenRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{NewToken: func() string { return " " }})
	if err != nil {
		t.Fatal(err)
	}
	// err 保存空租约令牌恢复扫描的结果错误。
	if err := emptyTokenRunner.RunRecovery(context.Background()); err != nil || len(emptyTokenRepository.claimCalls) != 0 {
		t.Fatalf("空令牌恢复分支异常: err=%v claims=%v", err, emptyTokenRepository.claimCalls)
	}

	// completionError 保存恢复启动失败后的终态补偿错误。
	completionError := errors.New("补偿写入失败")
	// completionRepository 保存启动失败和补偿写入失败的仓储。
	completionRepository := completeAppliedRepository()
	completionRepository.jobs = []RefreshJob{{ID: "completion-error"}}
	completionRepository.completeErr = completionError
	// callbackCount 保存恢复错误回调次数。
	callbackCount := 0
	// completionRunner 保存运行器已停止、启动必然失败的恢复运行器。
	completionRunner, err := NewRefreshJobRunner(completionRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{NewToken: func() string { return "completion-token" }, OnRecoveryError: func(error) { callbackCount++ }})
	if err != nil {
		t.Fatal(err)
	}
	completionRunner.Stop()
	// err 保存恢复启动和终态补偿错误回调的结果。
	if err := completionRunner.RunRecovery(context.Background()); err != nil || callbackCount != 1 || len(completionRepository.completeCalls) != 1 {
		t.Fatalf("补偿错误分支异常: err=%v callbacks=%d completes=%v", err, callbackCount, completionRepository.completeCalls)
	}
}

// TestRefreshRunnerCoversMarshalCompletionError 验证结果序列化失败且失败终态再次写入失败时会合并错误。
func TestRefreshRunnerCoversMarshalCompletionError(t *testing.T) {
	// marshalError 保存结果编码失败原因。
	marshalError := errors.New("结果编码失败")
	// completionError 保存失败终态写入失败原因。
	completionError := errors.New("终态写入失败")
	// repository 保存返回终态写入错误的仓储。
	repository := completeAppliedRepository()
	repository.completeErr = completionError
	// runner 保存注入结果编码错误的运行器。
	runner, err := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{MarshalResult: func(RefreshJobResult) ([]byte, error) { return nil, marshalError }})
	if err != nil {
		t.Fatal(err)
	}
	// resultError 保存合并后的任务执行错误。
	resultError := runner.RunJob(context.Background(), &RefreshJob{ID: "marshal-completion"}, "token")
	if !errors.Is(resultError, marshalError) || !errors.Is(resultError, completionError) {
		t.Fatalf("序列化与终态错误未合并: %v", resultError)
	}
}

// TestRefreshRunnerRecoveryCallbackOnIntervalError 验证恢复循环单轮错误会回调但不会终止后续生命周期。
func TestRefreshRunnerRecoveryCallbackOnIntervalError(t *testing.T) {
	// repository 保存首轮恢复查询错误。
	repository := completeAppliedRepository()
	repository.recoverErr = errors.New("恢复扫描失败")
	// callbackDone 保存错误回调完成信号。
	callbackDone := make(chan struct{}, 1)
	// runner 保存极短轮询间隔的恢复运行器。
	runner, err := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{RecoveryInterval: time.Millisecond, OnRecoveryError: func(error) { callbackDone <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	// err 保存恢复循环首次启动的结果错误。
	if err := runner.StartRecovery(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-callbackDone:
	case <-time.After(time.Second):
		runner.Close(context.Background())
		t.Fatal("恢复轮询错误未触发回调")
	}
	// intervalCallbackDone 保存定时器触发的第二次错误回调，区分启动时的立即扫描。
	intervalCallbackDone := make(chan struct{}, 1)
	// intervalRunner 保存专门验证 ticker 分支的恢复运行器。
	intervalRunner, err := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{RecoveryInterval: time.Millisecond, OnRecoveryError: func(error) { intervalCallbackDone <- struct{}{} }})
	if err != nil {
		t.Fatal(err)
	}
	// err 保存恢复循环定时轮询测试的启动结果。
	if err := intervalRunner.StartRecovery(context.Background()); err != nil {
		t.Fatal(err)
	}
	<-intervalCallbackDone
	select {
	case <-intervalCallbackDone:
	case <-time.After(time.Second):
		intervalRunner.Close(context.Background())
		t.Fatal("恢复定时轮询错误未触发回调")
	}
	// err 保存定时轮询运行器的关闭结果。
	if err := intervalRunner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	// err 保存首次恢复运行器的关闭结果。
	if err := runner.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
}
