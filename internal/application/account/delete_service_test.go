package account

import (
	"context"
	"errors"
	"testing"
)

// deleteRepositoryStub 是账号删除服务测试使用的非敏感存储替身。
type deleteRepositoryStub struct {
	summary   Summary
	getErr    error
	deleteErr error
	deleted   bool
}

// GetOwnedSummary 返回预置摘要或读取错误，模拟按用户过滤的账号查询。
func (r *deleteRepositoryStub) GetOwnedSummary(context.Context, int64, string) (Summary, error) {
	if r.getErr != nil {
		return Summary{}, r.getErr
	}
	return r.summary, nil
}

// DeleteOwned 记录删除调用并返回预置的持久化错误。
func (r *deleteRepositoryStub) DeleteOwned(context.Context, int64, string) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	r.deleted = true
	return nil
}

// deleteRuntimeStub 记录账号 fencing 与停止调用，便于验证收束顺序。
type deleteRuntimeStub struct {
	beginOK      bool
	stopErr      error
	needDeadline bool
	begin        int
	stop         int
	end          int
	stopOpen     chan struct{}
}

// BeginStopping 返回预置结果并记录建立 fencing 的次数。
func (r *deleteRuntimeStub) BeginStopping(string) bool {
	r.begin++
	return r.beginOK
}

// StopContext 记录停止调用并可阻塞到测试显式释放。
func (r *deleteRuntimeStub) StopContext(ctx context.Context, _ string) error {
	r.stop++
	if r.needDeadline {
		// ok 表示应用服务是否为停止操作注入了截止时间。
		if _, ok := ctx.Deadline(); !ok {
			return errors.New("停止上下文缺少截止时间")
		}
		return r.stopErr
	}
	if r.stopOpen != nil {
		select {
		case <-r.stopOpen:
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	return r.stopErr
}

// TestDeleteServiceBoundsRuntimeStop 验证删除服务为运行时停止提供最多五秒的截止时间。
func TestDeleteServiceBoundsRuntimeStop(t *testing.T) {
	// repository 是账号删除用例的非敏感存储替身。
	repository := &deleteRepositoryStub{summary: Summary{ID: "acc-1", UserID: 7}}
	// runtime 是要求收到带截止时间 Context 的运行时替身。
	runtime := &deleteRuntimeStub{beginOK: true, needDeadline: true}
	// service 是待验证的账号删除应用服务。
	service, err := NewDeleteService(repository, runtime)
	if err != nil {
		t.Fatal(err)
	}
	// deleteErr 保存带截止时间的停止结果。
	if deleteErr := service.Delete(context.Background(), 7, "acc-1"); deleteErr != nil {
		t.Fatalf("带截止时间的停止不应失败: %v", deleteErr)
	}
}

// EndStopping 记录释放 fencing 的次数。
func (r *deleteRuntimeStub) EndStopping(string) {
	r.end++
}

// TestDeleteServiceStopsBeforeOwnedDelete 验证账号先停止运行时再执行持久化删除。
func TestDeleteServiceStopsBeforeOwnedDelete(t *testing.T) {
	// repository 是账号删除用例的非敏感存储替身。
	repository := &deleteRepositoryStub{summary: Summary{ID: "acc-1", UserID: 7}}
	// runtime 是记录 fencing 顺序的运行时替身。
	runtime := &deleteRuntimeStub{beginOK: true}
	// service 是待验证的账号删除应用服务。
	service, err := NewDeleteService(repository, runtime)
	if err != nil {
		t.Fatal(err)
	}
	// deleteErr 保存账号停止并删除用例的执行结果。
	if err := service.Delete(context.Background(), 7, "acc-1"); err != nil {
		t.Fatalf("Delete error=%v", err)
	}
	if !repository.deleted || runtime.begin != 1 || runtime.stop != 1 || runtime.end != 1 {
		t.Fatalf("删除收束不完整: deleted=%v begin=%d stop=%d end=%d", repository.deleted, runtime.begin, runtime.stop, runtime.end)
	}
}

// TestDeleteServiceKeepsFencingOnStopFailure 验证停止失败时不会删除账号且一定释放 fencing。
func TestDeleteServiceKeepsFencingOnStopFailure(t *testing.T) {
	// repository 是用于确认删除未发生的存储替身。
	repository := &deleteRepositoryStub{summary: Summary{ID: "acc-1", UserID: 7}}
	// runtime 是返回停止错误的运行时替身。
	runtime := &deleteRuntimeStub{beginOK: true, stopErr: errors.New("stop failed")}
	// service 是待验证的账号删除应用服务。
	service, err := NewDeleteService(repository, runtime)
	if err != nil {
		t.Fatal(err)
	}
	// deleteErr 保存停止失败后的应用错误。
	if err := service.Delete(context.Background(), 7, "acc-1"); !errors.Is(err, ErrDeleteConflict) {
		t.Fatalf("停止失败应转换为删除冲突: %v", err)
	}
	if repository.deleted || runtime.end != 1 {
		t.Fatalf("停止失败后仍删除或未释放 fencing: deleted=%v end=%d", repository.deleted, runtime.end)
	}
}

// TestDeleteServiceRejectsFencingConflict 验证已有删除流程时不会进入停止或删除路径。
func TestDeleteServiceRejectsFencingConflict(t *testing.T) {
	// repository 是用于确认冲突短路的存储替身。
	repository := &deleteRepositoryStub{summary: Summary{ID: "acc-1", UserID: 7}}
	// runtime 是拒绝重复 fencing 的运行时替身。
	runtime := &deleteRuntimeStub{beginOK: false}
	// service 是待验证的账号删除应用服务。
	service, err := NewDeleteService(repository, runtime)
	if err != nil {
		t.Fatal(err)
	}
	// deleteErr 保存 fencing 冲突后的应用错误。
	if err := service.Delete(context.Background(), 7, "acc-1"); !errors.Is(err, ErrDeleteConflict) {
		t.Fatalf("重复 fencing 应返回冲突: %v", err)
	}
	if repository.deleted || runtime.stop != 0 || runtime.end != 0 {
		t.Fatalf("fencing 冲突未短路: deleted=%v stop=%d end=%d", repository.deleted, runtime.stop, runtime.end)
	}
}

// TestDeleteServiceHonorsCancellation 验证停止等待受调用方 Context 限制且不删除账号。
func TestDeleteServiceHonorsCancellation(t *testing.T) {
	// repository 是用于确认取消后不删除的存储替身。
	repository := &deleteRepositoryStub{summary: Summary{ID: "acc-1", UserID: 7}}
	// runtime 是等待关闭信号或 Context 取消的运行时替身。
	runtime := &deleteRuntimeStub{beginOK: true, stopOpen: make(chan struct{})}
	// service 是待验证的账号删除应用服务。
	service, err := NewDeleteService(repository, runtime)
	if err != nil {
		t.Fatal(err)
	}
	// ctx、cancel 将停止等待限制在确定的短超时内。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// deleteErr 保存取消停止等待后的应用错误。
	if err := service.Delete(ctx, 7, "acc-1"); !errors.Is(err, ErrDeleteConflict) {
		t.Fatalf("取消停止应转换为删除冲突: %v", err)
	}
	if repository.deleted || runtime.end != 1 {
		t.Fatalf("取消后删除或 fencing 收束错误: deleted=%v end=%d", repository.deleted, runtime.end)
	}
}
