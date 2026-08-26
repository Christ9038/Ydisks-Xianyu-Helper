package items

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"
)

// batchRunnerRepository 是批量 worker 的内存仓储替身，用于记录状态写入顺序。
type batchRunnerRepository struct {
	// rows 保存待处理明细。
	rows []BatchRow
	// batch 保存当前批次快照。
	batch BatchInfo
	// failed 保存失败明细的消息和分类。
	failed map[int64]string
	// finalized 保存最终状态收口次数。
	finalized int
	// workerToken 保存最近一次续租收到的令牌，允许协调器测试模拟租约切换。
	workerToken string
	// mu 保护协调器并发测试中的租约令牌和批次快照。
	mu sync.Mutex
}

// PendingRows 返回预置的批量明细。
func (r *batchRunnerRepository) PendingRows(_ context.Context, _ string, _ bool) ([]BatchRow, error) {
	return r.rows, nil
}

// RenewBatchLease 保持内存批次租约有效。
func (r *batchRunnerRepository) RenewBatchLease(_ context.Context, _, workerToken string, _ int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.workerToken = workerToken
	return true, nil
}

// GetBatch 返回当前批次快照。
func (r *batchRunnerRepository) GetBatch(_ context.Context, _ int64, _ string) (BatchInfo, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.workerToken != "" {
		r.batch.WorkerToken = r.workerToken
	}
	return r.batch, nil
}

// ClaimRow 始终允许测试 worker 抢占明细。
func (r *batchRunnerRepository) ClaimRow(_ context.Context, _ int64, _ string) (bool, error) {
	return true, nil
}

// BatchStatus 返回当前批次状态。
func (r *batchRunnerRepository) BatchStatus(_ context.Context, _ string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.batch.Status, nil
}

// MarkClaimedRowFailed 记录失败消息。
func (r *batchRunnerRepository) MarkClaimedRowFailed(ctx context.Context, rowID int64, _, message, _ string) (bool, error) {
	// err 表示状态补偿 Context 已被取消的原因。
	if err := ctx.Err(); err != nil {
		return false, err
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failed == nil {
		r.failed = make(map[int64]string)
	}
	r.failed[rowID] = message
	return true, nil
}

// RecountBatch 表示测试仓储已完成统计重算。
func (r *batchRunnerRepository) RecountBatch(_ context.Context, _ string) error { return nil }

// FinalizeBatch 记录正常收口并返回完成状态。
func (r *batchRunnerRepository) FinalizeBatch(_ context.Context, _, _ string) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finalized++
	return "completed", true, nil
}

// FinalizeCanceled 记录取消收口。
func (r *batchRunnerRepository) FinalizeCanceled(_ context.Context, _, _ string) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finalized++
	return true, nil
}

// FinalizeInterrupted 记录中断收口。
func (r *batchRunnerRepository) FinalizeInterrupted(_ context.Context, _, _, _ string) (string, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.finalized++
	return "failed", true, nil
}

// DeleteUpload 记录上传目录清理调用。
func (r *batchRunnerRepository) DeleteUpload(_ context.Context, _, _ string) error { return nil }

// ReservePublishSlot 测试仓储始终允许当前 worker 预留发布时隙。
func (r *batchRunnerRepository) ReservePublishSlot(_ context.Context, _, _ string, _, startedAtMillis int64) (bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.batch.LastPublishStartedAtMillis = startedAtMillis
	return true, nil
}

// batchRunnerPublisher 是可控的单行发布替身。
type batchRunnerPublisher struct {
	// err 保存发布失败错误。
	err error
	// calls 统计发布调用次数。
	calls int
}

// PublishRow 返回预置错误并统计调用次数。
func (p *batchRunnerPublisher) PublishRow(_ context.Context, _ int64, _ BatchRow, _ string, _ func(context.Context) error) error {
	p.calls++
	return p.err
}

// TestBatchRunnerMarksFailureAndFinalizes 验证失败行记录与批次最终收口。
func TestBatchRunnerMarksFailureAndFinalizes(t *testing.T) {
	// repository 保存批次状态和失败行。
	repository := &batchRunnerRepository{rows: []BatchRow{{ID: 1, BatchID: "batch-1"}}, batch: BatchInfo{ID: "batch-1", UserID: 7, Status: "running", WorkerToken: "worker"}}
	// publisher 模拟平台发布失败。
	publisher := &batchRunnerPublisher{err: errors.New("平台拒绝发布")}
	// runner 是使用零等待策略构造的应用 worker。
	// err 保存应用 worker 构造失败原因。
	runner, err := NewBatchRunner(repository, publisher, BatchRunOptions{Wait: func(context.Context, time.Duration) error { return nil }})
	if err != nil {
		t.Fatalf("构造 worker 失败: %v", err)
	}
	// err 保存 worker 运行失败原因。
	if err := runner.Run(context.Background(), 7, "batch-1", "worker", false); err != nil {
		t.Fatalf("运行 worker 失败: %v", err)
	}
	if publisher.calls != 1 || repository.failed[1] != "平台拒绝发布" || repository.finalized != 1 {
		t.Fatalf("worker 状态异常: calls=%d failed=%v finalized=%d", publisher.calls, repository.failed, repository.finalized)
	}
}

// TestBatchRunnerStopsOnCancellation 验证取消 Context 会触发中断收口且不发布后续明细。
func TestBatchRunnerStopsOnCancellation(t *testing.T) {
	// repository 保存两条待处理明细和运行状态。
	repository := &batchRunnerRepository{rows: []BatchRow{{ID: 1, BatchID: "batch-2"}, {ID: 2, BatchID: "batch-2"}}, batch: BatchInfo{ID: "batch-2", UserID: 7, Status: "running", WorkerToken: "worker"}}
	// publisher 在首行发布时取消 Context。
	publisher := &batchRunnerPublisher{}
	// ctx、cancel 控制 worker 取消生命周期。
	ctx, cancel := context.WithCancel(context.Background())
	publisher.err = context.Canceled
	// runner 使用会返回取消错误的等待替身，避免测试等待真实间隔。
	runner, err := NewBatchRunner(repository, publisher, BatchRunOptions{Wait: func(context.Context, time.Duration) error { cancel(); return context.Canceled }, IsSessionExpired: func(error) bool { return true }})
	if err != nil {
		t.Fatalf("构造 worker 失败: %v", err)
	}
	// runErr 保存 worker 因取消 Context 返回的错误。
	if runErr := runner.Run(ctx, 7, "batch-2", "worker", false); !errors.Is(runErr, context.Canceled) {
		t.Fatalf("取消错误=%v", runErr)
	}
	if publisher.calls != 1 || repository.finalized != 1 {
		t.Fatalf("取消收口异常: calls=%d finalized=%d", publisher.calls, repository.finalized)
	}
}

// TestBatchPublishErrorsAndHelpers 验证批量发布错误包装、等待和失败分类辅助函数的边界语义。
func TestBatchPublishErrorsAndHelpers(t *testing.T) {
	// cause 是后置步骤和远端结果包装使用的原始错误。
	cause := errors.New("本地后置失败")
	// postError 保存后置处理错误包装。
	postError := &PostPublishError{Err: cause}
	if postError.Error() != cause.Error() || !errors.Is(postError, cause) {
		t.Fatalf("后置错误包装异常: %v", postError)
	}
	// uncertainError 保存远端结果未知错误包装。
	uncertainError := &UncertainRemotePublishError{Err: cause}
	if uncertainError.Error() != cause.Error() || !errors.Is(uncertainError, cause) {
		t.Fatalf("远端未知错误包装异常: %v", uncertainError)
	}
	// nilPost、nilUncertain 保存空错误包装的稳定文本结果。
	var nilPost *PostPublishError
	// nilUncertain 保存空远端结果未知错误包装。
	var nilUncertain *UncertainRemotePublishError
	if nilPost.Error() == "" || nilPost.Unwrap() != nil || nilUncertain.Error() == "" || nilUncertain.Unwrap() != nil {
		t.Fatal("空错误包装的默认语义异常")
	}
	// got、want 保存主错误优先级断言的实际值和期望值。
	if got, want := firstNonNil(cause, errors.New("fallback")), cause; got != want {
		t.Fatal("firstNonNil 未优先返回主错误")
	}
	// fallback 是主错误为空时使用的备用错误。
	fallback := errors.New("fallback")
	if firstNonNil(nil, fallback) != fallback {
		t.Fatal("firstNonNil 未返回备用错误")
	}
	// message、kind 保存普通运行状态的失败分类结果。
	if message, kind := defaultFailureClassifier(cause, "running"); message != cause.Error() || kind != "publish" {
		t.Fatalf("普通失败分类异常: message=%q kind=%q", message, kind)
	}
	// message、kind 保存取消状态的失败分类结果。
	if message, kind := defaultFailureClassifier(cause, "canceling"); message != "任务已取消" || kind != "publish" {
		t.Fatalf("取消失败分类异常: message=%q kind=%q", message, kind)
	}
	// err 保存零时长等待的结果。
	if err := waitWithContext(context.Background(), 0); err != nil {
		t.Fatalf("零等待不应失败: %v", err)
	}
	// canceled、cancel 保存等待取消分支使用的上下文。
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	// err 保存取消上下文等待的结果。
	if err := waitWithContext(canceled, time.Second); !errors.Is(err, context.Canceled) {
		t.Fatalf("取消等待错误=%v", err)
	}
	// err 保存短时正常等待的结果。
	if err := waitWithContext(context.Background(), time.Millisecond); err != nil {
		t.Fatalf("短等待不应失败: %v", err)
	}
}

// TestBatchRunnerReservesDefaultPublishSlot 验证历史批次缺少间隔配置时仍使用默认发布时隙。
func TestBatchRunnerReservesDefaultPublishSlot(t *testing.T) {
	// repository 保存可立即成功预留时隙的批次仓储。
	repository := &batchRunnerRepository{batch: BatchInfo{ID: "batch-slot", UserID: 7, Status: "running", WorkerToken: "worker-slot"}}
	// runner、err 保存使用固定时钟的批量 worker。
	runner, err := NewBatchRunner(repository, &batchRunnerPublisher{}, BatchRunOptions{Now: func() time.Time { return time.UnixMilli(1_800_000_000_000) }})
	if err != nil {
		t.Fatal(err)
	}
	// reserveErr 保存默认五秒发布间隔的时隙预留结果。
	if reserveErr := runner.reservePublishSlot(context.Background(), 7, "batch-slot", "worker-slot", 0); reserveErr != nil {
		t.Fatalf("预留默认发布时隙失败: %v", reserveErr)
	}
	if repository.batch.LastPublishStartedAtMillis == 0 {
		t.Fatal("发布时隙没有写入开始时间")
	}
}
