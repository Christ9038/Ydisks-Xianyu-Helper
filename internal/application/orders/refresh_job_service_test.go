package orders

import (
	"context"
	"errors"
	"testing"
	"time"
)

// refreshJobOwnerTestDouble 是刷新任务 facade 使用的最小账号归属测试端口。
type refreshJobOwnerTestDouble struct {
	// owned 控制账号归属查询结果。
	owned bool
	// err 控制账号归属查询错误。
	err error
	// calls 保存归属查询调用次数。
	calls int
}

// OwnsAccount 返回预置的账号归属结果。
func (owner *refreshJobOwnerTestDouble) OwnsAccount(context.Context, int64, string) (bool, error) {
	owner.calls++
	return owner.owned, owner.err
}

// TestRefreshJobServiceCreateAndStart 验证 facade 会先校验账号、创建任务、声明租约再启动 worker。
func TestRefreshJobServiceCreateAndStart(t *testing.T) {
	// repository 记录 facade 的创建、抢占和 worker 终态写入。
	repository := completeAppliedRepository()
	// owner 返回账号属于当前用户，允许继续创建任务。
	owner := &refreshJobOwnerTestDouble{owned: true}
	// refresher 返回立即完成的结果，便于 Close 确定收口。
	refresher := &refreshRunnerTestRefresher{result: RefreshResult{Message: "完成"}}
	// runner、err 保存 facade 依赖的 worker 运行器。
	runner, err := NewRefreshJobRunner(repository, refresher, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造运行器: %v", err)
	}
	// fixedNow 是租约计算使用的固定时间。
	fixedNow := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	// service、constructErr 保存 facade 及其构造错误。
	service, constructErr := NewRefreshJobService(repository, owner, runner, RefreshJobServiceOptions{
		LeaseDuration: time.Minute,
		Now:           func() time.Time { return fixedNow },
		NewJobID:      func() string { return "job-created" },
		NewToken:      func() string { return "token-created" },
	})
	if constructErr != nil {
		t.Fatalf("构造 facade: %v", constructErr)
	}
	// result、startErr 保存创建任务并启动 worker 的结果。
	result, startErr := service.CreateAndStart(context.Background(), context.Background(), 7, "cookie-1", "pending_ship")
	if startErr != nil {
		t.Fatalf("创建并启动任务: %v", startErr)
	}
	if owner.calls != 1 || result.Job == nil || result.Job.ID != "job-created" || result.Job.Status != "running" || result.Token != "token-created" {
		t.Fatalf("创建结果不正确: owner=%d result=%+v", owner.calls, result)
	}
	repository.mu.Lock()
	// createdJobs 保存 facade 交给仓储的初始任务。
	createdJobs := append([]RefreshJob(nil), repository.createdJobs...)
	// claimCalls 保存 facade 声明 worker 租约的参数。
	claimCalls := append([]refreshRunnerClaimCall(nil), repository.claimCalls...)
	repository.mu.Unlock()
	if len(createdJobs) != 1 || createdJobs[0].UserID != 7 || len(claimCalls) != 1 || claimCalls[0].leaseExpiresAt != fixedNow.Add(time.Minute).Unix() {
		t.Fatalf("创建或抢占参数不正确: jobs=%v claims=%v", createdJobs, claimCalls)
	}
	// closeCtx 限制测试等待异步 worker 收口的时间。
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// closeErr 保存运行器关闭错误。
	if closeErr := runner.Close(closeCtx); closeErr != nil {
		t.Fatalf("关闭运行器: %v", closeErr)
	}
}

// TestRefreshJobServiceWorkerOutlivesRequestContext 验证任务创建成功后 HTTP 请求结束不会取消进程生命周期拥有的刷新 worker。
func TestRefreshJobServiceWorkerOutlivesRequestContext(t *testing.T) {
	// repository 保存可完成终态写入的测试任务仓储。
	repository := completeAppliedRepository()
	// owner 允许当前用户操作测试账号。
	owner := &refreshJobOwnerTestDouble{owned: true}
	// started 通知后台刷新已取得进程生命周期 Context。
	started := make(chan context.Context, 1)
	// release 控制后台刷新何时正常结束。
	release := make(chan struct{})
	// refresher 保存会等待 release 的刷新端口，便于在 HTTP Context 取消后检查 worker 是否仍活跃。
	refresher := refreshJobServiceBlockingRefresher{started: started, release: release}
	// runner、runnerErr 保存应用层 worker 运行器及其构造错误。
	runner, runnerErr := NewRefreshJobRunner(repository, refresher, RefreshJobRunnerOptions{})
	if runnerErr != nil {
		t.Fatalf("构造运行器失败: %v", runnerErr)
	}
	// service、serviceErr 保存创建和启动任务的应用服务。
	service, serviceErr := NewRefreshJobService(repository, owner, runner, RefreshJobServiceOptions{})
	if serviceErr != nil {
		t.Fatalf("构造刷新任务服务失败: %v", serviceErr)
	}
	// requestCtx、cancelRequest 模拟在 HTTP 响应返回后立即结束的请求 Context。
	requestCtx, cancelRequest := context.WithCancel(context.Background())
	defer cancelRequest()
	// lifecycleCtx、cancelLifecycle 是独立于请求的进程生命周期 Context。
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	defer cancelLifecycle()
	// _, startErr 保存创建并启动后台任务的结果。
	_, startErr := service.CreateAndStart(requestCtx, lifecycleCtx, 7, "cookie-1", "")
	if startErr != nil {
		t.Fatalf("创建并启动刷新任务失败: %v", startErr)
	}
	// workerCtx 保存后台刷新收到的 Context；必须不是 requestCtx。
	workerCtx := <-started
	cancelRequest()
	if workerCtx == requestCtx || workerCtx.Err() != nil {
		t.Fatalf("请求结束后 worker 被误取消: worker=%p request=%p err=%v", workerCtx, requestCtx, workerCtx.Err())
	}
	close(release)
	// closeCtx、cancelClose 限制异步任务正常退出的等待时间。
	closeCtx, cancelClose := context.WithTimeout(context.Background(), time.Second)
	defer cancelClose()
	// closeErr 保存正常完成后运行器关闭与等待的结果。
	if closeErr := runner.Close(closeCtx); closeErr != nil {
		t.Fatalf("关闭后台刷新 worker 失败: %v", closeErr)
	}
}

// refreshJobServiceBlockingRefresher 是请求与后台生命周期分离测试使用的阻塞刷新端口。
type refreshJobServiceBlockingRefresher struct {
	// started 返回 worker 实际收到的生命周期 Context。
	started chan<- context.Context
	// release 控制测试中的刷新业务何时结束。
	release <-chan struct{}
}

// Refresh 等待测试放行，并在调用方生命周期取消时提前退出。
func (refresher refreshJobServiceBlockingRefresher) Refresh(ctx context.Context, _ int64, _, _ string) (RefreshResult, error) {
	refresher.started <- ctx
	select {
	case <-refresher.release:
		return RefreshResult{Message: "完成"}, nil
	case <-ctx.Done():
		return RefreshResult{}, ctx.Err()
	}
}

// TestRefreshJobServiceCreateRejectsUnownedAccount 验证账号不属于用户时不会创建或抢占任务。
func TestRefreshJobServiceCreateRejectsUnownedAccount(t *testing.T) {
	// repository 记录不应发生的创建和抢占调用。
	repository := completeAppliedRepository()
	// owner 返回账号不属于当前用户。
	owner := &refreshJobOwnerTestDouble{}
	// runner、err 保存最小运行器。
	runner, err := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造运行器: %v", err)
	}
	// service、constructErr 保存 facade 构造结果。
	service, constructErr := NewRefreshJobService(repository, owner, runner, RefreshJobServiceOptions{NewJobID: func() string { return "unused" }, NewToken: func() string { return "unused" }})
	if constructErr != nil {
		t.Fatalf("构造 facade: %v", constructErr)
	}
	// _, err 保存归属失败返回的创建结果和应用错误。
	_, err = service.CreateAndStart(context.Background(), context.Background(), 7, "cookie-1", "")
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("无权账号未被拒绝")
	}
	if len(repository.createdJobs) != 0 || len(repository.claimCalls) != 0 {
		t.Fatalf("无权账号不应创建或抢占任务")
	}
}

// TestRefreshJobServiceGetAndCancel 验证查询和取消都通过用户归属仓储端口完成。
func TestRefreshJobServiceGetAndCancel(t *testing.T) {
	// repository 预置用户任务，供 facade 查询和取消。
	repository := completeAppliedRepository()
	repository.getJob = &RefreshJob{ID: "job-existing", UserID: 7, Status: "succeeded"}
	// runner、err 保存取消通知所需的运行器。
	runner, err := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造运行器: %v", err)
	}
	// service、constructErr 保存 facade 构造结果。
	service, constructErr := NewRefreshJobService(repository, &refreshJobOwnerTestDouble{owned: true}, runner, RefreshJobServiceOptions{})
	if constructErr != nil {
		t.Fatalf("构造 facade: %v", constructErr)
	}
	// job、getErr 保存 facade 查询结果。
	job, getErr := service.GetJob(context.Background(), 7, "job-existing")
	if getErr != nil || job == nil || job.Status != "succeeded" {
		t.Fatalf("查询任务不正确: job=%+v err=%v", job, getErr)
	}
	// result、cancelErr 保存取消未生效时的当前状态。
	result, cancelErr := service.CancelForUser(context.Background(), 7, "job-existing")
	if cancelErr != nil || result.Cancelled || result.Job == nil || result.Job.Status != "succeeded" {
		t.Fatalf("结束任务不应被取消: result=%+v err=%v", result, cancelErr)
	}
	repository.cancelResult = true
	// cancelled、cancelErr 保存原子取消成功结果。
	cancelled, cancelErr := service.CancelForUser(context.Background(), 7, "job-existing")
	if cancelErr != nil || !cancelled.Cancelled || cancelled.Job == nil || cancelled.Job.Status != "cancelled" {
		t.Fatalf("取消结果不正确: result=%+v err=%v", cancelled, cancelErr)
	}
}
