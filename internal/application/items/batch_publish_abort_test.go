package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestBatchRunnerAbortsBatchOnlyWhenPolicyRequires 验证整批中止策略：命中中止条件（风控、会话失效、Token 耗尽）时
// 不再发布剩余明细并按中断收口；普通失败只记录当前行并继续处理下一行。
func TestBatchRunnerAbortsBatchOnlyWhenPolicyRequires(t *testing.T) {
	// abortErr 模拟平台风控等必须终止本批次的错误。
	abortErr := errors.New("RGV587 风控验证")
	// cases 是中止策略与期望发布次数的对应关系。
	cases := []struct {
		name      string
		abort     bool
		wantCalls int
	}{
		{name: "命中中止条件", abort: true, wantCalls: 1},
		{name: "普通失败继续", abort: false, wantCalls: 3},
	}
	// item 是当前策略用例。
	for _, item := range cases {
		// repository 保存三条待处理明细和运行状态。
		repository := &batchRunnerRepository{rows: []BatchRow{{ID: 1, BatchID: "b"}, {ID: 2, BatchID: "b"}, {ID: 3, BatchID: "b"}}, batch: BatchInfo{ID: "b", UserID: 7, Status: "running", WorkerToken: "worker"}}
		// publisher 每行都返回同一个平台错误。
		publisher := &batchRunnerPublisher{err: abortErr}
		// abort 是本用例的中止判定结果，闭包捕获当前值。
		abort := item.abort
		// runner 使用零等待与可控中止判定构造。
		runner, err := NewBatchRunner(repository, publisher, BatchRunOptions{Wait: func(context.Context, time.Duration) error { return nil },
			ShouldAbortBatch: func(rowErr error) bool { return abort && errors.Is(rowErr, abortErr) }})
		if err != nil {
			t.Fatal(err)
		}
		// runErr 保存 worker 的返回值：中止时向上返回平台错误，继续时正常收口。
		runErr := runner.Run(context.Background(), 7, "b", "worker", false)
		if publisher.calls != item.wantCalls || repository.finalized != 1 {
			t.Fatalf("%s: calls=%d finalized=%d", item.name, publisher.calls, repository.finalized)
		}
		if item.abort != errors.Is(runErr, abortErr) {
			t.Fatalf("%s: runErr=%v", item.name, runErr)
		}
		if len(repository.failed) != item.wantCalls {
			t.Fatalf("%s: 失败记录=%v", item.name, repository.failed)
		}
	}
}
