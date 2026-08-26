package orders

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

// refreshRunnerTestRefresher 是测试用的订单刷新业务端口，可固定返回结果或等待取消。
type refreshRunnerTestRefresher struct {
	// result 保存测试刷新成功时返回的应用结果。
	result RefreshResult
	// err 保存测试刷新失败时返回的业务错误。
	err error
	// waitForCancel 表示是否等待调用方取消 Context 后再返回。
	waitForCancel bool
	// called 保存刷新调用次数，供生命周期断言使用。
	called int
	// mu 保护 called，允许 worker 测试与主 goroutine 并发读取。
	mu sync.Mutex
}

// Refresh 返回固定结果，或在 Context 取消后返回取消错误。
func (refresher *refreshRunnerTestRefresher) Refresh(ctx context.Context, _ int64, _, _ string) (RefreshResult, error) {
	refresher.mu.Lock()
	refresher.called++
	refresher.mu.Unlock()
	if refresher.waitForCancel {
		<-ctx.Done()
		return RefreshResult{}, ctx.Err()
	}
	return refresher.result, refresher.err
}

// refreshRunnerTestRepository 是测试用任务仓储，记录租约操作和终态写入参数。
type refreshRunnerTestRepository struct {
	// jobs 保存恢复扫描需要返回的任务快照。
	jobs []RefreshJob
	// createdJobs 保存 facade 创建的任务快照。
	createdJobs []RefreshJob
	// createErr 控制 facade 创建任务时返回的持久化错误。
	createErr error
	// getJob 保存 facade 查询任务时返回的快照。
	getJob *RefreshJob
	// getErr 控制 facade 查询任务时返回的错误。
	getErr error
	// cancelResult 控制 facade 取消任务是否原子生效。
	cancelResult bool
	// cancelErr 控制 facade 取消任务时返回的错误。
	cancelErr error
	// completeCalls 保存终态写入调用，顺序与 worker 完成顺序一致。
	completeCalls []refreshRunnerCompleteCall
	// recoverCalls 保存恢复扫描收到的截止时间和数量限制。
	recoverCalls []refreshRunnerRecoverCall
	// requeueCalls 保存重新入队的任务标识和扫描时间。
	requeueCalls []string
	// requeueApplied 控制过期任务重新入队是否原子生效。
	requeueApplied *bool
	// requeueErr 控制重新入队时返回的持久化错误。
	requeueErr error
	// claimCalls 保存恢复抢占的任务标识、令牌和租约截止时间。
	claimCalls []refreshRunnerClaimCall
	// claimApplied 控制任务租约是否命中；为空时默认成功。
	claimApplied *bool
	// claimErr 控制任务租约抢占错误。
	claimErr error
	// completeApplied 控制 Complete 是否命中当前 worker 租约。
	completeApplied bool
	// completeErr 控制 Complete 返回的持久化错误。
	completeErr error
	// recoverErr 控制 Recoverable 返回的扫描错误。
	recoverErr error
	// mu 保护测试仓储的可变记录，避免异步 worker 与断言竞争。
	mu sync.Mutex
}

// refreshRunnerCompleteCall 保存一次订单刷新任务终态写入的完整参数。
type refreshRunnerCompleteCall struct {
	// jobID 是终态任务标识。
	jobID string
	// token 是终态写入使用的 worker 租约令牌。
	token string
	// status 是 succeeded 或 failed 终态。
	status string
	// resultJSON 是成功结果或失败时的空对象 JSON。
	resultJSON string
	// errorMessage 是失败终态的错误文本。
	errorMessage string
}

// refreshRunnerRecoverCall 保存一次恢复扫描的查询参数。
type refreshRunnerRecoverCall struct {
	// now 是恢复查询使用的 Unix 秒截止时间。
	now int64
	// limit 是恢复查询的批量上限。
	limit int
}

// refreshRunnerClaimCall 保存一次恢复任务租约抢占参数。
type refreshRunnerClaimCall struct {
	// jobID 是待抢占任务标识。
	jobID string
	// token 是新 worker 租约令牌。
	token string
	// leaseExpiresAt 是新租约截止 Unix 秒。
	leaseExpiresAt int64
}

// Create 满足刷新任务仓储接口；运行器测试不会创建任务。
func (repository *refreshRunnerTestRepository) Create(_ context.Context, job *RefreshJob) error {
	if repository.createErr != nil {
		return repository.createErr
	}
	repository.mu.Lock()
	defer repository.mu.Unlock()
	if job != nil {
		repository.createdJobs = append(repository.createdJobs, *job)
	}
	return nil
}

// Get 满足刷新任务仓储接口；运行器测试不会读取用户任务。
func (repository *refreshRunnerTestRepository) Get(context.Context, int64, string) (*RefreshJob, error) {
	if repository.getErr != nil {
		return nil, repository.getErr
	}
	if repository.getJob == nil {
		return nil, ErrRefreshJobNotFound
	}
	// job 是返回给 facade 的任务快照副本，避免测试调用方改写仓储原值。
	job := *repository.getJob
	return &job, nil
}

// Claim 记录租约抢占并返回成功。
func (repository *refreshRunnerTestRepository) Claim(_ context.Context, jobID, token string, leaseExpiresAt int64) (bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.claimCalls = append(repository.claimCalls, refreshRunnerClaimCall{jobID: jobID, token: token, leaseExpiresAt: leaseExpiresAt})
	if repository.claimErr != nil {
		return false, repository.claimErr
	}
	if repository.claimApplied != nil {
		return *repository.claimApplied, nil
	}
	return true, nil
}

// Cancel 满足刷新任务仓储接口；运行器测试通过 Runner.CancelJob 验证内存取消。
func (repository *refreshRunnerTestRepository) Cancel(context.Context, int64, string) (bool, error) {
	return repository.cancelResult, repository.cancelErr
}

// Complete 记录任务终态并按测试配置返回租约命中结果。
func (repository *refreshRunnerTestRepository) Complete(_ context.Context, jobID, token, status, resultJSON, errorMessage string) (bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.completeCalls = append(repository.completeCalls, refreshRunnerCompleteCall{jobID: jobID, token: token, status: status, resultJSON: resultJSON, errorMessage: errorMessage})
	return repository.completeApplied, repository.completeErr
}

// Recoverable 返回测试预置的过期任务。
func (repository *refreshRunnerTestRepository) Recoverable(_ context.Context, now int64, limit int) ([]RefreshJob, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.recoverCalls = append(repository.recoverCalls, refreshRunnerRecoverCall{now: now, limit: limit})
	if repository.recoverErr != nil {
		return nil, repository.recoverErr
	}
	return append([]RefreshJob(nil), repository.jobs...), nil
}

// RequeueExpired 记录重新入队操作并返回成功。
func (repository *refreshRunnerTestRepository) RequeueExpired(_ context.Context, jobID string, _ int64) (bool, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.requeueCalls = append(repository.requeueCalls, jobID)
	if repository.requeueErr != nil {
		return false, repository.requeueErr
	}
	if repository.requeueApplied != nil {
		return *repository.requeueApplied, nil
	}
	return true, nil
}

// completeAppliedRepository 创建默认命中租约的测试仓储。
func completeAppliedRepository() *refreshRunnerTestRepository {
	return &refreshRunnerTestRepository{completeApplied: true}
}

// TestNewRefreshJobResultUsesStableSnakeCase 验证任务结果持久化模型保持 HTTP 兼容的 snake_case JSON。
func TestNewRefreshJobResultUsesStableSnakeCase(t *testing.T) {
	// zero 是待转换的空刷新结果，确保空集合编码为 [] 而不是 null。
	zero := NewRefreshJobResult(RefreshResult{})
	// encoded、err 保存结果模型的 JSON 编码。
	encoded, err := json.Marshal(zero)
	if err != nil {
		t.Fatalf("编码刷新结果: %v", err)
	}
	if string(encoded) != `{"partial_failure":false,"message":"","summary":{"discovered":0,"list_updated":0,"soft_deleted":0,"detail_total":0,"total":0,"updated":0,"no_change":0,"failed":0},"results":[]}` {
		t.Fatalf("刷新结果 JSON 不兼容: %s", encoded)
	}

	// source 是包含账号和订单结果的应用刷新结果。
	source := RefreshResult{Results: []RefreshOrderResult{{CookieID: "cookie-1", Success: true, Discovered: 2, Updated: 3, SoftDeleted: 1}, {OrderID: "order-1", Stage: "detail"}}}
	// converted 是带可选字段的任务结果模型。
	converted := NewRefreshJobResult(source)
	if converted.Results[0].Discovered == nil || converted.Results[0].SoftDeleted == nil || converted.Results[1].Discovered != nil {
		t.Fatalf("结果可选字段映射不正确: %+v", converted.Results)
	}
	// failedSource 验证失败账号结果不会新增旧响应未提供的 soft_deleted 字段。
	failedSource := NewRefreshJobResult(RefreshResult{Results: []RefreshOrderResult{{CookieID: "cookie-failed", Success: false, SoftDeleted: 1}}})
	if failedSource.Results[0].SoftDeleted != nil {
		t.Fatalf("失败账号不应持久化 soft_deleted: %+v", failedSource.Results[0])
	}
}

// TestRefreshJobRunnerRunJobSuccess 验证成功刷新会写入带稳定 JSON 的 succeeded 终态。
func TestRefreshJobRunnerRunJobSuccess(t *testing.T) {
	// repository 保存本用例的终态写入记录。
	repository := completeAppliedRepository()
	// refresher 返回固定统计，验证 runner 调用应用业务而非 HTTP DTO。
	refresher := &refreshRunnerTestRefresher{result: RefreshResult{Message: "完成", Summary: RefreshSummary{Total: 1}}}
	// runner、err 保存订单刷新运行器及构造错误。
	runner, err := NewRefreshJobRunner(repository, refresher, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造运行器: %v", err)
	}
	// job 是本次执行的应用层任务模型。
	job := &RefreshJob{ID: "job-1", UserID: 7, CookieID: "cookie-1", FilterStatus: "pending_ship"}
	// err 保存同步执行刷新任务的错误。
	if err := runner.RunJob(context.Background(), job, "token-1"); err != nil {
		t.Fatalf("执行刷新任务: %v", err)
	}
	repository.mu.Lock()
	// calls 保存终态写入快照。
	calls := append([]refreshRunnerCompleteCall(nil), repository.completeCalls...)
	repository.mu.Unlock()
	if len(calls) != 1 || calls[0].status != "succeeded" || calls[0].token != "token-1" {
		t.Fatalf("成功终态参数不正确: %+v", calls)
	}
	if !strings.Contains(calls[0].resultJSON, `"partial_failure"`) || strings.Contains(calls[0].resultJSON, `"PartialFailure"`) {
		t.Fatalf("成功终态 JSON 未使用稳定字段: %s", calls[0].resultJSON)
	}
}

// TestRefreshJobRunnerFailurePaths 验证业务失败、序列化失败和租约失配都保留可观测错误并写入失败终态。
func TestRefreshJobRunnerFailurePaths(t *testing.T) {
	// businessErr 是刷新业务返回的根因。
	businessErr := errors.New("平台请求失败")
	// repository 保存业务失败终态记录。
	repository := completeAppliedRepository()
	// refresher 返回预置业务错误。
	refresher := &refreshRunnerTestRefresher{err: businessErr}
	// runner、err 保存业务失败场景的运行器构造结果。
	runner, err := NewRefreshJobRunner(repository, refresher, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造业务失败运行器: %v", err)
	}
	// job 是业务失败场景的任务。
	job := &RefreshJob{ID: "job-failure", UserID: 7}
	// err 保存业务失败任务返回的根因。
	if !errors.Is(runner.RunJob(context.Background(), job, "token-failure"), businessErr) {
		t.Fatalf("业务错误未原样返回")
	}
	repository.mu.Lock()
	// failedCall 保存业务失败终态快照。
	failedCall := repository.completeCalls[len(repository.completeCalls)-1]
	repository.mu.Unlock()
	if failedCall.status != "failed" || failedCall.resultJSON != "{}" || failedCall.errorMessage != businessErr.Error() {
		t.Fatalf("业务失败终态不正确: %+v", failedCall)
	}

	// marshalErr 是结果编码失败的根因。
	marshalErr := errors.New("编码失败")
	// marshalRepository 保存序列化失败场景的终态记录。
	marshalRepository := completeAppliedRepository()
	// marshalRefresher 返回固定成功结果。
	marshalRefresher := &refreshRunnerTestRefresher{result: RefreshResult{Message: "完成"}}
	// marshalRunner、constructErr 保存注入编码器后的运行器。
	marshalRunner, constructErr := NewRefreshJobRunner(marshalRepository, marshalRefresher, RefreshJobRunnerOptions{MarshalResult: func(RefreshJobResult) ([]byte, error) {
		return nil, marshalErr
	}})
	if constructErr != nil {
		t.Fatalf("构造序列化失败运行器: %v", constructErr)
	}
	if !errors.Is(marshalRunner.RunJob(context.Background(), job, "token-marshal"), marshalErr) {
		t.Fatalf("序列化错误未原样返回")
	}

	// lostLeaseRepository 模拟终态写入未命中租约。
	lostLeaseRepository := completeAppliedRepository()
	lostLeaseRepository.completeApplied = false
	// lostLeaseRunner、lostErr 保存租约失配运行器。
	lostLeaseRunner, lostErr := NewRefreshJobRunner(lostLeaseRepository, &refreshRunnerTestRefresher{result: RefreshResult{}}, RefreshJobRunnerOptions{})
	if lostErr != nil {
		t.Fatalf("构造租约失配运行器: %v", lostErr)
	}
	if !errors.Is(lostLeaseRunner.RunJob(context.Background(), job, "token-lost"), ErrRefreshJobCompletionNotApplied) {
		t.Fatalf("租约失配未转换为可观测错误")
	}
}

// TestRefreshJobRunnerCancelCloseAndStop 验证 worker 取消表、Stop/Close 和 Wait 能收口后台任务。
func TestRefreshJobRunnerCancelCloseAndStop(t *testing.T) {
	// repository 保存取消场景的失败终态。
	repository := completeAppliedRepository()
	// refresher 等待 Context 取消后返回，模拟无法立即结束的外部请求。
	refresher := &refreshRunnerTestRefresher{waitForCancel: true}
	// runner、err 保存短超时运行器。
	runner, err := NewRefreshJobRunner(repository, refresher, RefreshJobRunnerOptions{JobTimeout: time.Hour})
	if err != nil {
		t.Fatalf("构造取消运行器: %v", err)
	}
	// job 是等待取消的后台任务。
	job := &RefreshJob{ID: "job-cancel", UserID: 7}
	// err 保存启动后台任务时的生命周期校验错误。
	if err := runner.StartJob(context.Background(), job, "token-cancel"); err != nil {
		t.Fatalf("启动取消任务: %v", err)
	}
	if !runner.CancelJob(job.ID) {
		t.Fatalf("未找到可取消 worker")
	}
	// closeCtx 限制本用例等待后台任务退出的时间。
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// err 保存运行器关闭和 worker 等待过程的错误。
	if err := runner.Close(closeCtx); err != nil {
		t.Fatalf("关闭运行器: %v", err)
	}
	runner.Wait()
	if runner.CancelJob(job.ID) {
		t.Fatalf("worker 退出后不应仍可取消")
	}

	// stoppedRunner 验证 Stop 后不能再次启动任务。
	stoppedRunner, err := NewRefreshJobRunner(completeAppliedRepository(), &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造停止运行器: %v", err)
	}
	stoppedRunner.Stop()
	if !errors.Is(stoppedRunner.StartJob(context.Background(), job, "token-after-stop"), ErrRefreshJobRunnerStopped) {
		t.Fatalf("Stop 后仍接受新 worker")
	}
}

// TestRefreshJobRunnerRecovery 验证恢复扫描会按过期任务重新入队、抢占租约并启动 worker。
func TestRefreshJobRunnerRecovery(t *testing.T) {
	// fixedNow 是恢复扫描使用的固定时间。
	fixedNow := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	// repository 保存恢复任务和调用记录。
	repository := completeAppliedRepository()
	repository.jobs = []RefreshJob{{ID: "job-recover", UserID: 9, CookieID: "cookie-9"}}
	// refresher 返回立即成功的固定结果，使恢复 worker 可确定结束。
	refresher := &refreshRunnerTestRefresher{result: RefreshResult{Message: "恢复完成"}}
	// runner、err 保存注入固定时间和令牌后的运行器。
	runner, err := NewRefreshJobRunner(repository, refresher, RefreshJobRunnerOptions{Now: func() time.Time { return fixedNow }, NewToken: func() string { return "recovery-token" }, LeaseDuration: 2 * time.Minute})
	if err != nil {
		t.Fatalf("构造恢复运行器: %v", err)
	}
	// err 保存单轮恢复扫描返回的错误。
	if err := runner.RunRecovery(context.Background()); err != nil {
		t.Fatalf("执行恢复扫描: %v", err)
	}
	// closeCtx 限制恢复 worker 退出等待时间。
	closeCtx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	// err 保存关闭恢复 worker 的错误。
	if err := runner.Close(closeCtx); err != nil {
		t.Fatalf("关闭恢复运行器: %v", err)
	}
	repository.mu.Lock()
	// claims 保存恢复抢占记录。
	claims := append([]refreshRunnerClaimCall(nil), repository.claimCalls...)
	// requeues 保存恢复重新入队记录。
	requeues := append([]string(nil), repository.requeueCalls...)
	repository.mu.Unlock()
	if len(requeues) != 1 || requeues[0] != "job-recover" || len(claims) != 1 || claims[0].token != "recovery-token" {
		t.Fatalf("恢复接管记录不正确: requeues=%v claims=%v", requeues, claims)
	}
	if claims[0].leaseExpiresAt != fixedNow.Add(2*time.Minute).Unix() {
		t.Fatalf("恢复租约截止时间不正确: %d", claims[0].leaseExpiresAt)
	}
}

// TestRefreshJobRunnerRecoversAfterLifecycleCancellation 验证进程取消导致终态无法写入后，过期租约会在后续恢复扫描中重新接管。
func TestRefreshJobRunnerRecoversAfterLifecycleCancellation(t *testing.T) {
	// fixedNow 是首次取消和后续恢复扫描共享的确定性时钟。
	fixedNow := time.Date(2026, 8, 19, 9, 0, 0, 0, time.UTC)
	// repository 保存终态写入失败及后续恢复队列的测试配置。
	repository := completeAppliedRepository()
	repository.completeErr = context.Canceled
	repository.jobs = []RefreshJob{{ID: "job-recover-after-cancel", UserID: 7, CookieID: "cookie-7"}}
	// refresher 等待进程 Context 取消后返回，使失败终态沿用已取消 Context。
	refresher := &refreshRunnerTestRefresher{waitForCancel: true}
	// runner、runnerErr 保存具有固定租约和令牌的任务运行器。
	runner, runnerErr := NewRefreshJobRunner(repository, refresher, RefreshJobRunnerOptions{Now: func() time.Time { return fixedNow }, NewToken: func() string { return "recovered-token" }})
	if runnerErr != nil {
		t.Fatalf("构造运行器失败: %v", runnerErr)
	}
	// lifecycleCtx、cancelLifecycle 模拟进程关闭取消。
	lifecycleCtx, cancelLifecycle := context.WithCancel(context.Background())
	// startErr 保存初始 worker 启动结果。
	startErr := runner.StartJob(lifecycleCtx, &RefreshJob{ID: "job-recover-after-cancel", UserID: 7, CookieID: "cookie-7"}, "cancelled-token")
	if startErr != nil {
		t.Fatalf("启动取消 worker 失败: %v", startErr)
	}
	cancelLifecycle()
	// workerDone 等待取消 worker 清理内存登记；不能调用 Close，否则运行器会拒绝后续恢复 worker。
	workerDone := make(chan struct{})
	go func() {
		runner.Wait()
		close(workerDone)
	}()
	select {
	case <-workerDone:
	case <-time.After(time.Second):
		t.Fatal("取消 worker 未退出")
	}
	repository.mu.Lock()
	// completionCalls 保存进程取消时失败的终态写入记录。
	completionCalls := append([]refreshRunnerCompleteCall(nil), repository.completeCalls...)
	repository.completeErr = nil
	repository.mu.Unlock()
	if len(completionCalls) == 0 || completionCalls[0].jobID != "job-recover-after-cancel" {
		t.Fatalf("取消后未尝试终态写入: %+v", completionCalls)
	}
	// recoveryErr 保存新的可用生命周期下恢复扫描的结果。
	recoveryErr := runner.RunRecovery(context.Background())
	if recoveryErr != nil {
		t.Fatalf("恢复扫描失败: %v", recoveryErr)
	}
	// recoveryCloseCtx、cancelRecoveryClose 等待恢复 worker 完成。
	recoveryCloseCtx, cancelRecoveryClose := context.WithTimeout(context.Background(), time.Second)
	defer cancelRecoveryClose()
	// closeErr 保存恢复 worker 结束后的关闭结果。
	if closeErr := runner.Close(recoveryCloseCtx); closeErr != nil {
		t.Fatalf("关闭恢复 worker 失败: %v", closeErr)
	}
	repository.mu.Lock()
	// requeues 保存恢复扫描对过期任务的重新入队记录；claims 保存恢复 worker 获得的新租约记录。
	requeues := append([]string(nil), repository.requeueCalls...)
	// claims 是断言恢复 worker 使用新令牌的租约快照。
	claims := append([]refreshRunnerClaimCall(nil), repository.claimCalls...)
	repository.mu.Unlock()
	if len(requeues) != 1 || requeues[0] != "job-recover-after-cancel" || len(claims) != 1 || claims[0].token != "recovered-token" {
		t.Fatalf("取消后的恢复接管不正确: requeues=%v claims=%+v", requeues, claims)
	}
}

// TestRefreshJobRunnerRecoveryError 验证恢复仓储错误直接返回，便于生命周期层观测并决定下一轮重试。
func TestRefreshJobRunnerRecoveryError(t *testing.T) {
	// scanErr 是恢复扫描仓储返回的错误。
	scanErr := errors.New("扫描失败")
	// repository 保存扫描错误配置。
	repository := completeAppliedRepository()
	repository.recoverErr = scanErr
	// runner、err 保存恢复运行器构造结果。
	runner, err := NewRefreshJobRunner(repository, &refreshRunnerTestRefresher{}, RefreshJobRunnerOptions{})
	if err != nil {
		t.Fatalf("构造扫描错误运行器: %v", err)
	}
	if !errors.Is(runner.RunRecovery(context.Background()), scanErr) {
		t.Fatalf("恢复扫描错误未返回")
	}
}
