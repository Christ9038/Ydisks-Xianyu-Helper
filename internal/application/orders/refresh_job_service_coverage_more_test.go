package orders

import (
	"context"
	"errors"
	"testing"
)

// nilOrdersContext 返回用于覆盖订单任务兼容 nil Context 分支的空上下文接口。
func nilOrdersContext() context.Context { return nil }

// TestRefreshJobServiceCoversCreationGuards 验证刷新任务创建的输入、归属、标识和持久化错误分支。
func TestRefreshJobServiceCoversCreationGuards(t *testing.T) {
	// owner 是允许账号归属查询通过的测试端口。
	owner := &refreshJobOwnerTestDouble{owned: true}
	// newService 构造指定仓储和固定任务标识的服务。
	newService := func(t *testing.T, repository *refreshRunnerTestRepository, runner *RefreshJobRunner) *RefreshJobService {
		// service、err 保存刷新任务服务构造结果。
		service, err := NewRefreshJobService(repository, owner, runner, RefreshJobServiceOptions{NewJobID: func() string { return "job" }, NewToken: func() string { return "token" }})
		if err != nil {
			t.Fatal(err)
		}
		return service
	}
	// invalidService 是未装配刷新任务服务。
	invalidService := &RefreshJobService{}
	// invalidCreateErr 保存未装配服务的创建错误。
	_, invalidCreateErr := invalidService.CreateAndStart(context.Background(), context.Background(), 1, "", "")
	if invalidCreateErr == nil {
		t.Fatal("未装配服务应拒绝创建")
	}
	// baseRepository 是创建任务使用的内存仓储。
	baseRepository := completeAppliedRepository()
	// baseRunner 是创建任务使用的运行器。
	baseRunner, err := NewRefreshJobRunner(baseRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// service 是完整装配的刷新任务服务。
	service := newService(t, baseRepository, baseRunner)
	// nilRequestErr 保存空请求上下文错误。
	_, nilRequestErr := service.CreateAndStart(nilOrdersContext(), context.Background(), 1, "", "")
	if nilRequestErr == nil {
		t.Fatal("空请求上下文应失败")
	}
	// nilLifecycleErr 保存空生命周期上下文错误。
	_, nilLifecycleErr := service.CreateAndStart(context.Background(), nil, 1, "", "")
	if nilLifecycleErr == nil {
		t.Fatal("空生命周期上下文应失败")
	}
	// forbiddenErr 保存无效用户标识错误。
	_, forbiddenErr := service.CreateAndStart(context.Background(), context.Background(), 0, "", "")
	if !errors.Is(forbiddenErr, ErrForbidden) {
		t.Fatalf("无效用户错误=%v", forbiddenErr)
	}
	// ownerError 是账号归属查询错误。
	ownerError := errors.New("owner unavailable")
	// ownerErrorService 是归属查询错误场景的服务。
	ownerErrorService, err := NewRefreshJobService(baseRepository, &refreshJobOwnerTestDouble{owned: true, err: ownerError}, baseRunner, RefreshJobServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// ownerResultErr 保存归属查询错误。
	_, ownerResultErr := ownerErrorService.CreateAndStart(context.Background(), context.Background(), 1, "account", "")
	if !errors.Is(ownerResultErr, ownerError) {
		t.Fatalf("归属错误=%v", ownerResultErr)
	}
	// emptyIDService 是任务 ID 生成空值场景的服务。
	emptyIDService, err := NewRefreshJobService(baseRepository, owner, baseRunner, RefreshJobServiceOptions{NewJobID: func() string { return " " }, NewToken: func() string { return "token" }})
	if err != nil {
		t.Fatal(err)
	}
	// emptyIDErr 保存任务 ID 生成错误。
	_, emptyIDErr := emptyIDService.CreateAndStart(context.Background(), context.Background(), 1, "", "")
	if emptyIDErr == nil {
		t.Fatal("空任务 ID 应失败")
	}
	// emptyTokenService 是租约令牌生成空值场景的服务。
	emptyTokenService, err := NewRefreshJobService(baseRepository, owner, baseRunner, RefreshJobServiceOptions{NewJobID: func() string { return "job" }, NewToken: func() string { return " " }})
	if err != nil {
		t.Fatal(err)
	}
	// emptyTokenErr 保存租约令牌生成错误。
	_, emptyTokenErr := emptyTokenService.CreateAndStart(context.Background(), context.Background(), 1, "", "")
	if emptyTokenErr == nil {
		t.Fatal("空租约令牌应失败")
	}
	// createError 是任务创建持久化错误。
	createError := errors.New("create failed")
	// createService 是任务创建失败场景的服务。
	createRepository := completeAppliedRepository()
	createRepository.createErr = createError
	// createRunner、err 保存任务创建失败场景的运行器构造结果。
	createRunner, err := NewRefreshJobRunner(createRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// createService 是任务创建失败场景的服务。
	createService := newService(t, createRepository, createRunner)
	// createResultErr 保存任务创建持久化错误。
	_, createResultErr := createService.CreateAndStart(context.Background(), context.Background(), 1, "", "")
	if !errors.Is(createResultErr, createError) {
		t.Fatalf("任务创建错误=%v", createResultErr)
	}
	// claimError 是任务租约抢占错误。
	claimError := errors.New("claim failed")
	// claimRepository 是租约抢占失败场景的仓储。
	claimRepository := completeAppliedRepository()
	claimRepository.claimErr = claimError
	// claimRunner、err 保存租约抢占失败场景的运行器构造结果。
	claimRunner, err := NewRefreshJobRunner(claimRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// claimService 是租约抢占失败场景的服务。
	claimService := newService(t, claimRepository, claimRunner)
	// claimResultErr 保存租约抢占错误。
	_, claimResultErr := claimService.CreateAndStart(context.Background(), context.Background(), 1, "", "")
	if !errors.Is(claimResultErr, claimError) {
		t.Fatalf("租约抢占错误=%v", claimResultErr)
	}
	// claimFalse 保存数据库未命中租约的结果。
	claimFalse := false
	// claimFalseRepository 保存未命中租约的仓储。
	claimFalseRepository := completeAppliedRepository()
	claimFalseRepository.claimApplied = &claimFalse
	// claimFalseRunner、err 保存未命中租约场景的运行器构造结果。
	claimFalseRunner, err := NewRefreshJobRunner(claimFalseRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// claimFalseService 是未命中租约场景的服务。
	claimFalseService := newService(t, claimFalseRepository, claimFalseRunner)
	// claimFalseErr 保存未命中租约的业务错误。
	_, claimFalseErr := claimFalseService.CreateAndStart(context.Background(), context.Background(), 1, "", "")
	if !errors.Is(claimFalseErr, ErrRefreshJobCompletionNotApplied) {
		t.Fatalf("未命中租约错误=%v", claimFalseErr)
	}
}

// TestRefreshJobServiceCoversCancelAndRunnerFailure 验证取消错误、任务查询错误和 worker 启动补偿。
func TestRefreshJobServiceCoversCancelAndRunnerFailure(t *testing.T) {
	// repository 是取消和终态补偿使用的仓储。
	repository := completeAppliedRepository()
	repository.getJob = &RefreshJob{ID: "job", UserID: 1, Status: "running"}
	// runner 是任务生命周期运行器。
	runner, err := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// service 是完整装配的任务服务。
	service, err := NewRefreshJobService(repository, &refreshJobOwnerTestDouble{owned: true}, runner, RefreshJobServiceOptions{})
	if err != nil {
		t.Fatal(err)
	}
	// cancelError 是数据库取消错误。
	cancelError := errors.New("cancel failed")
	repository.cancelErr = cancelError
	// cancelResultErr 保存数据库取消错误。
	_, cancelResultErr := service.CancelForUser(context.Background(), 1, "job")
	if !errors.Is(cancelResultErr, cancelError) {
		t.Fatalf("取消错误=%v", cancelResultErr)
	}
	repository.cancelErr = nil
	// getError 是取消未生效后读取任务的错误。
	getError := errors.New("get failed")
	repository.getErr = getError
	// getResultErr 保存取消未生效后的任务查询错误。
	_, getResultErr := service.CancelForUser(context.Background(), 1, "job")
	if !errors.Is(getResultErr, getError) {
		t.Fatalf("取消后查询错误=%v", getResultErr)
	}
	repository.getErr = nil
	// stoppedRunner 是已经停止、无法登记新 worker 的运行器。
	stoppedRunner, err := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	stoppedRunner.Stop()
	// stoppedService 是 worker 登记失败场景的任务服务。
	stoppedService, err := NewRefreshJobService(repository, &refreshJobOwnerTestDouble{owned: true}, stoppedRunner, RefreshJobServiceOptions{NewJobID: func() string { return "stopped-job" }, NewToken: func() string { return "stopped-token" }})
	if err != nil {
		t.Fatal(err)
	}
	// stoppedResultErr 保存 worker 登记失败后的错误。
	// releaseError 保存 worker 启动失败后的租约收口错误。
	releaseError := errors.New("租约收口失败")
	repository.completeErr = releaseError
	// stoppedResultErr 保存 worker 启动与租约收口错误的合并结果。
	_, stoppedResultErr := stoppedService.CreateAndStart(context.Background(), context.Background(), 1, "", "")
	if !errors.Is(stoppedResultErr, ErrRefreshJobRunnerStopped) || !errors.Is(stoppedResultErr, releaseError) {
		t.Fatalf("worker 登记错误=%v", stoppedResultErr)
	}
	// noReleaseErrorRepository 保存只返回 worker 启动错误的仓储。
	noReleaseErrorRepository := completeAppliedRepository()
	// noReleaseRunner 保存已停止的运行器。
	noReleaseRunner, err := NewRefreshJobRunner(noReleaseErrorRepository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatal(err)
	}
	noReleaseRunner.Stop()
	// noReleaseService 保存租约收口成功时的服务。
	noReleaseService, err := NewRefreshJobService(noReleaseErrorRepository, &refreshJobOwnerTestDouble{owned: true}, noReleaseRunner, RefreshJobServiceOptions{NewJobID: func() string { return "no-release-job" }, NewToken: func() string { return "no-release-token" }})
	if err != nil {
		t.Fatal(err)
	}
	// noReleaseResultErr 保存未合并补偿错误的 worker 启动错误。
	_, noReleaseResultErr := noReleaseService.CreateAndStart(context.Background(), context.Background(), 1, "", "")
	if !errors.Is(noReleaseResultErr, ErrRefreshJobRunnerStopped) {
		t.Fatalf("无补偿错误的 worker 启动错误异常: %v", noReleaseResultErr)
	}
	// invalidGetErr 保存无效任务查询参数错误。
	_, invalidGetErr := service.GetJob(context.Background(), 0, "")
	if !errors.Is(invalidGetErr, ErrRefreshJobNotFound) {
		t.Fatalf("无效查询错误=%v", invalidGetErr)
	}
	// invalidCancelErr 保存无效取消参数错误。
	_, invalidCancelErr := service.CancelForUser(nilOrdersContext(), 1, "job")
	if !errors.Is(invalidCancelErr, ErrRefreshJobNotFound) {
		t.Fatalf("无效取消错误=%v", invalidCancelErr)
	}
}
