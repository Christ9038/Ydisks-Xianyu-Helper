package server

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// orderRefreshLifecycleContextKey 是生命周期测试专用的 Context 键类型，避免与生产或其他测试键冲突。
type orderRefreshLifecycleContextKey struct{}

// newOrderRefreshJobLifecycleFixture 创建生命周期测试共用的应用任务模型。
func newOrderRefreshJobLifecycleFixture() *orderapp.RefreshJob {
	return &orderapp.RefreshJob{ID: "refresh-lifecycle-job", UserID: 7, CookieID: "cookie-1", FilterStatus: "pending_ship"}
}

// TestRunOrderRefreshJobWithSuccess 验证业务成功后会写入成功终态和序列化结果。
func TestRunOrderRefreshJobWithSuccess(t *testing.T) {
	// ctx 是本用例传递给业务和终态写入的生命周期上下文。
	ctx := context.WithValue(context.Background(), orderRefreshLifecycleContextKey{}, "lifecycle")
	// job 是待执行的订单刷新任务。
	job := newOrderRefreshJobLifecycleFixture()
	// result 是业务刷新返回的确定性结果。
	result := orderRefreshResponse{Message: "刷新完成", Summary: orderRefreshSummary{Total: 1}}
	// completed 表示终态写入是否被调用。
	completed := false
	// status 保存终态写入的目标状态。
	status := ""
	// resultJSON 保存终态写入的序列化结果。
	resultJSON := ""
	// refresh 是成功返回固定结果的业务回调。
	refresh := func(gotCtx context.Context, userID int64, cookieID, filterStatus string) (orderRefreshResponse, error) {
		if gotCtx != ctx || userID != job.UserID || cookieID != job.CookieID || filterStatus != job.FilterStatus {
			t.Fatalf("刷新回调参数不正确: ctx=%p user=%d cookie=%s status=%s", gotCtx, userID, cookieID, filterStatus)
		}
		return result, nil
	}
	// marshal 是使用标准 JSON 编码器的确定性序列化回调。
	marshal := func(value any) ([]byte, error) { return json.Marshal(value) }
	// complete 是记录终态写入参数的成功回调。
	complete := func(gotCtx context.Context, jobID, token, gotStatus, gotResultJSON, errorMessage string) (bool, error) {
		if gotCtx != ctx || jobID != job.ID || token != "token-1" || errorMessage != "" {
			t.Fatalf("终态写入参数不正确: ctx=%p job=%s token=%s status=%s error=%s", gotCtx, jobID, token, gotStatus, errorMessage)
		}
		completed, status, resultJSON = true, gotStatus, gotResultJSON
		return true, nil
	}

	// err 保存 worker 执行错误。
	err := runOrderRefreshJobWith(ctx, job, "token-1", refresh, marshal, complete)
	if err != nil {
		t.Fatalf("成功刷新不应返回错误: %v", err)
	}
	if !completed || status != "succeeded" || resultJSON == "" {
		t.Fatalf("成功终态未写入: completed=%v status=%s result=%s", completed, status, resultJSON)
	}
}

// TestRunOrderRefreshJobWithBusinessFailure 验证业务错误会写入失败终态并原样返回。
func TestRunOrderRefreshJobWithBusinessFailure(t *testing.T) {
	// ctx 是本用例传递给业务和终态写入的上下文。
	ctx := context.Background()
	// job 是待执行的订单刷新任务。
	job := newOrderRefreshJobLifecycleFixture()
	// businessErr 是业务刷新失败的根因。
	businessErr := errors.New("订单平台请求失败")
	// failedStatus 保存失败终态写入状态。
	failedStatus := ""
	// failedMessage 保存失败终态写入的业务错误文本。
	failedMessage := ""
	// refresh 是返回预置业务错误的刷新回调。
	refresh := func(context.Context, int64, string, string) (orderRefreshResponse, error) {
		return orderRefreshResponse{}, businessErr
	}
	// marshal 是不会被业务失败路径调用的序列化回调。
	marshal := func(any) ([]byte, error) {
		t.Fatal("业务失败不应尝试序列化成功结果")
		return nil, nil
	}
	// complete 是记录失败终态的回调。
	complete := func(_ context.Context, jobID, token, status, resultJSON, errorMessage string) (bool, error) {
		if jobID != job.ID || token != "token-1" || resultJSON != "{}" {
			t.Fatalf("失败终态参数不正确: job=%s token=%s result=%s", jobID, token, resultJSON)
		}
		failedStatus, failedMessage = status, errorMessage
		return true, nil
	}

	// err 保存 worker 执行错误。
	err := runOrderRefreshJobWith(ctx, job, "token-1", refresh, marshal, complete)
	if !errors.Is(err, businessErr) || failedStatus != "failed" || failedMessage != businessErr.Error() {
		t.Fatalf("业务失败终态不正确: err=%v status=%s message=%s", err, failedStatus, failedMessage)
	}
}

// TestRunOrderRefreshJobWithSerializationFailure 验证序列化失败会写入失败终态。
func TestRunOrderRefreshJobWithSerializationFailure(t *testing.T) {
	// ctx 是本用例传递给业务和终态写入的上下文。
	ctx := context.Background()
	// job 是待执行的订单刷新任务。
	job := newOrderRefreshJobLifecycleFixture()
	// marshalErr 是结果序列化失败的根因。
	marshalErr := errors.New("结果序列化失败")
	// failedStatus 保存序列化失败后的终态状态。
	failedStatus := ""
	// refresh 是返回固定成功结果的刷新回调。
	refresh := func(context.Context, int64, string, string) (orderRefreshResponse, error) {
		return orderRefreshResponse{Message: "完成"}, nil
	}
	// marshal 是返回预置序列化错误的回调。
	marshal := func(any) ([]byte, error) { return nil, marshalErr }
	// complete 是记录失败终态的回调。
	complete := func(_ context.Context, jobID, token, status, resultJSON, errorMessage string) (bool, error) {
		if jobID != job.ID || token != "token-1" || resultJSON != "{}" || errorMessage != marshalErr.Error() {
			t.Fatalf("序列化失败终态参数不正确: job=%s token=%s result=%s error=%s", jobID, token, resultJSON, errorMessage)
		}
		failedStatus = status
		return true, nil
	}

	// err 保存 worker 执行错误。
	err := runOrderRefreshJobWith(ctx, job, "token-1", refresh, marshal, complete)
	if !errors.Is(err, marshalErr) || failedStatus != "failed" {
		t.Fatalf("序列化失败未正确收束: err=%v status=%s", err, failedStatus)
	}
}

// TestRunOrderRefreshJobWithCompletionFailure 验证终态数据库错误和租约失配不会被吞掉。
func TestRunOrderRefreshJobWithCompletionFailure(t *testing.T) {
	// ctx 是本用例传递给业务和终态写入的上下文。
	ctx := context.Background()
	// job 是待执行的订单刷新任务。
	job := newOrderRefreshJobLifecycleFixture()
	// writeErr 是终态持久化返回的数据库错误。
	writeErr := errors.New("数据库写入失败")
	// refresh 是返回固定成功结果的刷新回调。
	refresh := func(context.Context, int64, string, string) (orderRefreshResponse, error) {
		return orderRefreshResponse{Message: "完成"}, nil
	}
	// marshal 是使用标准 JSON 编码器的序列化回调。
	marshal := func(value any) ([]byte, error) { return json.Marshal(value) }
	// completeWithError 是返回数据库错误的终态写入回调。
	completeWithError := func(context.Context, string, string, string, string, string) (bool, error) {
		return false, writeErr
	}
	// err 保存数据库错误场景的 worker 执行错误。
	err := runOrderRefreshJobWith(ctx, job, "token-1", refresh, marshal, completeWithError)
	if !errors.Is(err, writeErr) {
		t.Fatalf("终态数据库错误未返回: %v", err)
	}

	// completeUnapplied 是返回租约未命中的终态写入回调。
	completeUnapplied := func(context.Context, string, string, string, string, string) (bool, error) {
		return false, nil
	}
	// unappliedErr 保存租约未命中场景的 worker 执行错误。
	unappliedErr := runOrderRefreshJobWith(ctx, job, "token-1", refresh, marshal, completeUnapplied)
	if !errors.Is(unappliedErr, errOrderRefreshJobCompletionNotApplied) {
		t.Fatalf("终态未生效未转换为可观测错误: %v", unappliedErr)
	}
}

// TestRunOrderRefreshJobWithCancellation 验证取消后的业务和终态写入都继承同一个已取消 Context。
func TestRunOrderRefreshJobWithCancellation(t *testing.T) {
	// ctx、cancel 是 Server 生命周期取消信号及其触发函数。
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	// job 是待执行的订单刷新任务。
	job := newOrderRefreshJobLifecycleFixture()
	// completionCtx 保存终态写入收到的上下文。
	var completionCtx context.Context
	// refresh 是确认业务收到取消信号的回调。
	refresh := func(gotCtx context.Context, _ int64, _, _ string) (orderRefreshResponse, error) {
		if gotCtx != ctx || !errors.Is(gotCtx.Err(), context.Canceled) {
			t.Fatalf("业务未继承取消 Context: ctx=%p err=%v", gotCtx, gotCtx.Err())
		}
		return orderRefreshResponse{}, context.Canceled
	}
	// marshal 是不会被取消业务路径调用的序列化回调。
	marshal := func(any) ([]byte, error) {
		t.Fatal("取消业务不应尝试序列化成功结果")
		return nil, nil
	}
	// complete 是模拟数据库遵循 Context 取消的终态写入回调。
	complete := func(gotCtx context.Context, _ string, _ string, _ string, _ string, _ string) (bool, error) {
		completionCtx = gotCtx
		return false, gotCtx.Err()
	}

	// err 保存取消场景的 worker 执行错误。
	err := runOrderRefreshJobWith(ctx, job, "token-1", refresh, marshal, complete)
	if completionCtx != ctx || !errors.Is(err, context.Canceled) {
		t.Fatalf("取消错误或 Context 未保留: ctx=%p completion=%p err=%v", ctx, completionCtx, err)
	}
}

// TestOrderRefreshWorkerUsesCanceledServerLifecycle 验证 Server 关闭后 worker 不会使用脱离生命周期的 Context 写终态。
func TestOrderRefreshWorkerUsesCanceledServerLifecycle(t *testing.T) {
	// srv、store、cleanup 保存测试服务、数据库和清理函数。
	srv, store, cleanup := newTestServer(t)
	defer cleanup()
	// lifecycleCtx、cancel 是已取消的 Server 生命周期上下文。
	lifecycleCtx, cancel := context.WithCancel(context.Background())
	cancel()
	srv.lifecycleMu.Lock()
	srv.lifecycleCtx = lifecycleCtx
	srv.lifecycleMu.Unlock()
	// admin、err 保存任务所属用户及查询错误。
	admin, err := store.Users.GetByUsername(context.Background(), "admin")
	if err != nil {
		t.Fatalf("读取管理员: %v", err)
	}
	// dbJob 保存数据库层待执行任务。
	dbJob := &db.OrderRefreshJob{ID: "refresh-canceled-lifecycle", UserID: admin.ID}
	// err 保存创建任务错误。
	if err := store.OrderRefreshJobs.Create(context.Background(), dbJob); err != nil {
		t.Fatalf("创建刷新任务: %v", err)
	}
	// token 是当前 worker 的租约令牌。
	token := "refresh-canceled-token"
	// claimed、err 保存任务抢占结果及错误。
	claimed, err := store.OrderRefreshJobs.Claim(context.Background(), dbJob.ID, token, 9999999999)
	if err != nil || !claimed {
		t.Fatalf("抢占刷新任务: claimed=%v err=%v", claimed, err)
	}
	// job 是传给后台 worker 的应用层任务模型。
	job := &orderapp.RefreshJob{ID: dbJob.ID, UserID: dbJob.UserID}
	srv.startOrderRefreshWorker(job, token)
	srv.WaitForBackground()
	// persisted、err 保存 worker 结束后的任务状态及查询错误。
	persisted, err := store.OrderRefreshJobs.Get(context.Background(), admin.ID, dbJob.ID)
	if err != nil {
		t.Fatalf("读取取消后的任务: %v", err)
	}
	if persisted.Status != "running" {
		t.Fatalf("取消生命周期后不应使用后台 Context 覆盖租约状态: %+v", persisted)
	}
}

// TestStartBackgroundTaskResultTracksFailure 验证后台任务错误会进入 Server 任务状态。
func TestStartBackgroundTaskResultTracksFailure(t *testing.T) {
	// srv 是只装配任务注册表的最小 Server 生命周期实例。
	srv := &Server{taskRegistry: newTaskRegistry()}
	// taskErr 是后台任务返回的确定性错误。
	taskErr := errors.New("后台任务失败")
	// taskID 是登记后用于读取任务状态的进程内标识。
	taskID := srv.startBackgroundTaskResult("订单刷新测试任务", context.Background(), func() error {
		return taskErr
	})
	srv.WaitForBackground()
	// snapshots 保存任务注册表返回的状态快照。
	snapshots := srv.taskRegistry.list()
	if len(snapshots) != 1 || snapshots[0].ID != taskID || snapshots[0].State != taskStateFailed {
		t.Fatalf("后台任务失败状态未记录: %+v", snapshots)
	}
}
