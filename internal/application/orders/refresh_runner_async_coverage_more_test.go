package orders

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestRefreshRunnerCoversAsyncErrorCallbacksAndReplacement 覆盖异步 worker 错误回调、恢复循环错误回调和同任务替换。
func TestRefreshRunnerCoversAsyncErrorCallbacksAndReplacement(t *testing.T) {
	// service 保存默认分片大小的订单刷新服务。
	service := NewRefreshService(nil, nil, 0)
	if service == nil || service.detailChunkSize != 100 {
		t.Fatalf("订单刷新服务默认分片大小异常: %+v", service)
	}
	// workerErrors 保存异步 worker 错误回调。
	workerErrors := make(chan error, 1)
	// workerRepository 保存异步 worker 终态仓储。
	workerRepository := completeAppliedRepository()
	// workerRunner 保存返回业务错误的运行器。
	workerRunner, err := NewRefreshJobRunner(workerRepository, &refreshRunnerTestRefresher{err: errors.New("worker 失败")}, RefreshJobRunnerOptions{OnWorkerError: func(_ string, callbackErr error) { workerErrors <- callbackErr }})
	if err != nil {
		t.Fatalf("构造异步错误运行器失败: %v", err)
	}
	// job 保存异步 worker 任务。
	job := &RefreshJob{ID: "async-error"}
	// err 保存异步错误 worker 启动结果。
	if err := workerRunner.StartJob(context.Background(), job, "worker-token"); err != nil {
		t.Fatalf("启动异步错误 worker 失败: %v", err)
	}
	// waitContext 保存等待异步 worker 收口的 Context。
	waitContext, cancelWait := context.WithTimeout(context.Background(), time.Second)
	defer cancelWait()
	// err 保存异步错误 worker 关闭结果。
	if err := workerRunner.Close(waitContext); err != nil {
		t.Fatalf("关闭异步错误 worker 失败: %v", err)
	}
	select {
	// callbackErr 保存 worker 错误回调收到的业务错误。
	case callbackErr := <-workerErrors:
		if callbackErr == nil {
			t.Fatal("worker 错误回调未携带错误")
		}
	default:
		t.Fatal("worker 错误回调未触发")
	}

	// replacementRunner 保存可被同任务第二次启动替换的运行器。
	replacementRunner, err := NewRefreshJobRunner(completeAppliedRepository(), &refreshRunnerTestRefresher{waitForCancel: true}, RefreshJobRunnerOptions{JobTimeout: time.Hour})
	if err != nil {
		t.Fatalf("构造替换运行器失败: %v", err)
	}
	// err 保存首个同任务 worker 启动结果。
	if err := replacementRunner.StartJob(context.Background(), &RefreshJob{ID: "same-job"}, "first-token"); err != nil {
		t.Fatalf("启动首个 worker 失败: %v", err)
	}
	// err 保存替换同任务 worker 启动结果。
	if err := replacementRunner.StartJob(context.Background(), &RefreshJob{ID: "same-job"}, "second-token"); err != nil {
		t.Fatalf("启动替换 worker 失败: %v", err)
	}
	// err 保存替换 worker 关闭结果。
	if err := replacementRunner.Close(waitContext); err != nil {
		t.Fatalf("关闭替换 worker 失败: %v", err)
	}

	// recoveryErrors 保存恢复循环错误回调。
	recoveryErrors := make(chan error, 2)
	// recoveryRepository 保存每次扫描都返回错误的仓储。
	recoveryRepository := completeAppliedRepository()
	recoveryRepository.recoverErr = errors.New("恢复轮询失败")
	// recoveryContext、cancelRecovery 控制恢复循环在首次错误回调后退出。
	recoveryContext, cancelRecovery := context.WithCancel(context.Background())
	// recoveryRunner 保存带恢复错误回调的运行器。
	recoveryRunner, err := NewRefreshJobRunner(recoveryRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{RecoveryInterval: time.Millisecond, OnRecoveryError: func(callbackErr error) { recoveryErrors <- callbackErr; cancelRecovery() }})
	if err != nil {
		t.Fatalf("构造恢复错误运行器失败: %v", err)
	}
	// err 保存恢复错误循环启动结果。
	if err := recoveryRunner.StartRecovery(recoveryContext); err != nil {
		t.Fatalf("启动恢复错误循环失败: %v", err)
	}
	// err 保存恢复错误循环关闭结果。
	if err := recoveryRunner.Close(waitContext); err != nil {
		t.Fatalf("关闭恢复错误循环失败: %v", err)
	}
	select {
	// callbackErr 保存恢复错误回调收到的仓储错误。
	case callbackErr := <-recoveryErrors:
		if callbackErr == nil {
			t.Fatal("恢复错误回调未携带错误")
		}
	default:
		t.Fatal("恢复错误回调未触发")
	}

	// zeroRunner 保存用于覆盖活动计数保护分支的运行器。
	zeroRunner := &RefreshJobRunner{}
	zeroRunner.finishActiveLocked()
}
