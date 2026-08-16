package automation

import (
	"context"
	"errors"
	"testing"
)

// fakeIssueRepository 保存自动化异常应用服务测试使用的端口调用记录和预设结果。
type fakeIssueRepository struct {
	// runs 是模拟返回的异常运行摘要。
	runs []RunIssue
	// tasks 是模拟返回的死信延期任务摘要。
	tasks []DeferredIssue
	// listErr 是模拟异常列表查询失败。
	listErr error
	// runErr 是模拟异常运行处理失败。
	runErr error
	// taskErr 是模拟延期任务处理失败。
	taskErr error
	// listedUserID 保存最近一次列表查询的用户归属。
	listedUserID int64
	// resolvedRun 保存最近一次异常运行处理参数。
	resolvedRun struct {
		// userID 是处理请求所属用户。
		userID int64
		// runID 是待处理运行标识。
		runID int64
		// resolution 是人工处理动作。
		resolution string
	}
	// resolvedTask 保存最近一次延期任务处理参数。
	resolvedTask struct {
		// userID 是处理请求所属用户。
		userID int64
		// taskID 是待处理延期任务标识。
		taskID int64
		// retry 表示是否重新放回待执行队列。
		retry bool
	}
}

// ListIssues 返回预设异常摘要并记录用户归属。
func (f *fakeIssueRepository) ListIssues(_ context.Context, userID int64) ([]RunIssue, []DeferredIssue, error) {
	f.listedUserID = userID
	return f.runs, f.tasks, f.listErr
}

// ResolveRunIssue 返回预设错误并记录异常运行处理参数。
func (f *fakeIssueRepository) ResolveRunIssue(_ context.Context, userID, runID int64, resolution string) error {
	f.resolvedRun.userID, f.resolvedRun.runID, f.resolvedRun.resolution = userID, runID, resolution
	return f.runErr
}

// ResolveDeferredIssue 返回预设错误并记录延期任务处理参数。
func (f *fakeIssueRepository) ResolveDeferredIssue(_ context.Context, userID, taskID int64, retry bool) error {
	f.resolvedTask.userID, f.resolvedTask.taskID, f.resolvedTask.retry = userID, taskID, retry
	return f.taskErr
}

// TestServiceListIssuesDelegatesUserScope 验证异常列表查询保留用户归属并返回非敏感应用模型。
func TestServiceListIssuesDelegatesUserScope(t *testing.T) {
	// repository 是记录调用参数的自动化异常测试端口。
	repository := &fakeIssueRepository{
		runs:  []RunIssue{{ID: 7, CookieID: "account-1", IssueKind: "partial_failure"}},
		tasks: []DeferredIssue{{ID: 8, CookieID: "account-1", AttemptCount: 3}},
	}
	// service 是待验证的自动化异常应用服务。
	service := NewIssueService(repository)
	// runs、tasks、err 保存应用服务返回的异常摘要和错误。
	runs, tasks, err := service.ListIssues(context.Background(), 42)
	if err != nil || repository.listedUserID != 42 {
		t.Fatalf("list result=%+v tasks=%+v user=%d err=%v", runs, tasks, repository.listedUserID, err)
	}
	if len(runs) != 1 || runs[0].IssueKind != "partial_failure" || len(tasks) != 1 || tasks[0].AttemptCount != 3 {
		t.Fatalf("应用摘要未按预期返回: runs=%+v tasks=%+v", runs, tasks)
	}
}

// TestServiceResolveRunTrimsAndPropagates 验证异常运行处理动作会裁剪空白并保留端口错误。
func TestServiceResolveRunTrimsAndPropagates(t *testing.T) {
	// expectedErr 是模拟持久化处理失败的错误哨兵。
	expectedErr := errors.New("resolve failed")
	// repository 是记录异常运行处理参数的测试端口。
	repository := &fakeIssueRepository{runErr: expectedErr}
	// service 是待验证的自动化异常应用服务。
	service := NewIssueService(repository)
	// err 保存应用服务返回的处理错误。
	err := service.ResolveRunIssue(context.Background(), 42, 7, "  cancel ")
	if !errors.Is(err, expectedErr) || repository.resolvedRun.resolution != "cancel" || repository.resolvedRun.userID != 42 || repository.resolvedRun.runID != 7 {
		t.Fatalf("resolve run params=%+v err=%v", repository.resolvedRun, err)
	}
}

// TestServiceResolveDeferredValidatesAndMapsResolution 验证延期任务只接受 retry/dismiss 并正确转换重试语义。
func TestServiceResolveDeferredValidatesAndMapsResolution(t *testing.T) {
	// repository 是记录延期任务处理参数的测试端口。
	repository := &fakeIssueRepository{}
	// service 是待验证的自动化异常应用服务。
	service := NewIssueService(repository)
	if // err 保存重试处理结果。
	err := service.ResolveDeferredIssue(context.Background(), 42, 8, " retry "); err != nil || !repository.resolvedTask.retry {
		t.Fatalf("retry resolution params=%+v err=%v", repository.resolvedTask, err)
	}
	if // err 保存驳回处理结果。
	err := service.ResolveDeferredIssue(context.Background(), 42, 8, "dismiss"); err != nil || repository.resolvedTask.retry {
		t.Fatalf("dismiss resolution params=%+v err=%v", repository.resolvedTask, err)
	}
	// invalidErr 保存不支持处理动作的应用层校验错误。
	invalidErr := service.ResolveDeferredIssue(context.Background(), 42, 8, "continue")
	if !errors.Is(invalidErr, ErrInvalidDeferredResolution) {
		t.Fatalf("invalid resolution err=%v", invalidErr)
	}
}

// TestServiceRejectsInvalidScope 验证无效用户或资源标识不会触发持久化端口。
func TestServiceRejectsInvalidScope(t *testing.T) {
	// repository 是记录调用次数的自动化异常测试端口。
	repository := &fakeIssueRepository{}
	// service 是待验证的自动化异常应用服务。
	service := NewIssueService(repository)
	// listErr 保存无效用户列表查询返回的参数错误。
	_, _, listErr := service.ListIssues(context.Background(), 0)
	if !errors.Is(listErr, ErrInvalidInput) {
		t.Fatalf("invalid list scope err=%v", listErr)
	}
	// runErr 保存无效运行标识返回的参数错误。
	runErr := service.ResolveRunIssue(context.Background(), 1, 0, "cancel")
	if !errors.Is(runErr, ErrInvalidInput) {
		t.Fatalf("invalid run scope err=%v", runErr)
	}
	// taskErr 保存无效延迟任务标识返回的参数错误。
	taskErr := service.ResolveDeferredIssue(context.Background(), 1, 0, "retry")
	if !errors.Is(taskErr, ErrInvalidInput) {
		t.Fatalf("invalid task scope err=%v", taskErr)
	}
	if repository.listedUserID != 0 || repository.resolvedRun.runID != 0 || repository.resolvedTask.taskID != 0 {
		t.Fatal("无效标识不应调用持久化端口")
	}
}
