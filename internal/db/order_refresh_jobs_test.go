package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestOrderRefreshJobsLifecycle 验证订单刷新任务的创建、租约、终态和过期恢复生命周期。
func TestOrderRefreshJobsLifecycle(t *testing.T) {
	// store、cleanup 保存当前测试数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存当前任务测试使用的上下文。
	ctx := context.Background()
	// userID、cookieID 保存测试账号归属信息。
	userID, cookieID := seedAccount(t, store)
	// job 保存待测试的订单刷新任务。
	job := &OrderRefreshJob{ID: "refresh-job-1", UserID: userID, CookieID: cookieID, FilterStatus: "pending_ship"}
	// err 表示创建任务的数据库错误。
	if err := store.OrderRefreshJobs.Create(ctx, job); err != nil {
		t.Fatalf("create job: %v", err)
	}
	// loaded、err 保存创建后的任务读取结果及错误。
	loaded, err := store.OrderRefreshJobs.Get(ctx, userID, job.ID)
	if err != nil || loaded.Status != "queued" || loaded.ResultJSON != "{}" {
		t.Fatalf("created job=%+v err=%v", loaded, err)
	}
	// claimed、err 保存首次租约抢占结果及错误。
	claimed, err := store.OrderRefreshJobs.Claim(ctx, job.ID, "worker-1", time.Now().Add(time.Minute).Unix())
	if err != nil || !claimed {
		t.Fatalf("claim job: claimed=%v err=%v", claimed, err)
	}
	// err 表示重复抢占的数据库错误。
	if claimed, err = store.OrderRefreshJobs.Claim(ctx, job.ID, "worker-2", time.Now().Add(time.Minute).Unix()); err != nil || claimed {
		t.Fatalf("duplicate claim should fail: claimed=%v err=%v", claimed, err)
	}
	// completed、err 表示错误 worker 的终态写入结果及错误。
	if completed, err := store.OrderRefreshJobs.Complete(ctx, job.ID, "worker-2", "succeeded", `{"message":"wrong worker"}`, ""); err != nil || completed {
		t.Fatalf("wrong worker must not complete: completed=%v err=%v", completed, err)
	}
	// completed、err 表示合法 worker 的终态写入结果及错误。
	if completed, err := store.OrderRefreshJobs.Complete(ctx, job.ID, "worker-1", "succeeded", `{"message":"ok"}`, ""); err != nil || !completed {
		t.Fatalf("complete job: completed=%v err=%v", completed, err)
	}
	// completedJob、err 保存成功任务的终态读取结果及错误。
	completedJob, err := store.OrderRefreshJobs.Get(ctx, userID, job.ID)
	if err != nil || completedJob.Status != "succeeded" || completedJob.WorkerToken != "" {
		t.Fatalf("completed job=%+v err=%v", completedJob, err)
	}

	// cancelJob 保存用于验证用户隔离取消和重复取消的任务。
	cancelJob := &OrderRefreshJob{ID: "refresh-job-cancel", UserID: userID, CookieID: cookieID}
	// err 表示取消任务创建的数据库错误。
	if err := store.OrderRefreshJobs.Create(ctx, cancelJob); err != nil {
		t.Fatalf("create cancel job: %v", err)
	}
	// cancelled、err 保存排队任务的取消结果及错误。
	cancelled, err := store.OrderRefreshJobs.Cancel(ctx, userID, cancelJob.ID)
	if err != nil || !cancelled {
		t.Fatalf("cancel queued job: cancelled=%v err=%v", cancelled, err)
	}
	// cancelled、err 保存重复取消已终止任务的结果及错误。
	if cancelled, err = store.OrderRefreshJobs.Cancel(ctx, userID, cancelJob.ID); err != nil || cancelled {
		t.Fatalf("repeat cancel should return false: cancelled=%v err=%v", cancelled, err)
	}
	// cancelled、err 保存跨用户取消任务的结果及错误。
	if cancelled, err = store.OrderRefreshJobs.Cancel(ctx, userID+1, cancelJob.ID); err != nil || cancelled {
		t.Fatalf("cross-user cancel must be rejected: cancelled=%v err=%v", cancelled, err)
	}
	// cancelledJob、err 保存取消后的任务及读取错误。
	cancelledJob, err := store.OrderRefreshJobs.Get(ctx, userID, cancelJob.ID)
	if err != nil || cancelledJob.Status != "cancelled" || cancelledJob.WorkerToken != "" {
		t.Fatalf("cancelled job=%+v err=%v", cancelledJob, err)
	}
	// runningCancelJob 保存用于验证运行中 worker fencing 的任务。
	runningCancelJob := &OrderRefreshJob{ID: "refresh-job-cancel-running", UserID: userID, CookieID: cookieID}
	// err 表示运行中取消任务的创建错误。
	if err := store.OrderRefreshJobs.Create(ctx, runningCancelJob); err != nil {
		t.Fatalf("create running cancel job: %v", err)
	}
	// claimed、err 保存运行中任务的抢占结果及错误。
	claimed, err = store.OrderRefreshJobs.Claim(ctx, runningCancelJob.ID, "worker-cancel", time.Now().Add(time.Minute).Unix())
	if err != nil || !claimed {
		t.Fatalf("claim running cancel job: claimed=%v err=%v", claimed, err)
	}
	// cancelled、err 保存运行中任务的取消结果及错误。
	cancelled, err = store.OrderRefreshJobs.Cancel(ctx, userID, runningCancelJob.ID)
	if err != nil || !cancelled {
		t.Fatalf("cancel running job: cancelled=%v err=%v", cancelled, err)
	}
	// completed、err 保存旧 worker 终态写入结果及错误。
	completed, err := store.OrderRefreshJobs.Complete(ctx, runningCancelJob.ID, "worker-cancel", "succeeded", `{}`, "")
	if err != nil || completed {
		t.Fatalf("cancelled worker must not complete: completed=%v err=%v", completed, err)
	}

	// expiredJob 保存用于验证租约过期恢复的任务。
	expiredJob := &OrderRefreshJob{ID: "refresh-job-expired", UserID: userID, CookieID: cookieID}
	// err 表示创建过期任务的数据库错误。
	if err := store.OrderRefreshJobs.Create(ctx, expiredJob); err != nil {
		t.Fatalf("create expired job: %v", err)
	}
	// claimed、err 表示过期任务的租约抢占结果及错误。
	if claimed, err := store.OrderRefreshJobs.Claim(ctx, expiredJob.ID, "worker-old", time.Now().Add(-time.Minute).Unix()); err != nil || !claimed {
		t.Fatalf("claim expired job: claimed=%v err=%v", claimed, err)
	}
	// recoverable、err 保存过期任务扫描结果及错误。
	recoverable, err := store.OrderRefreshJobs.Recoverable(ctx, time.Now().Unix(), 10)
	if err != nil || len(recoverable) != 1 || recoverable[0].ID != expiredJob.ID {
		t.Fatalf("recoverable=%+v err=%v", recoverable, err)
	}
	// requeued、err 表示过期任务重新入队结果及错误。
	if requeued, err := store.OrderRefreshJobs.RequeueExpired(ctx, expiredJob.ID, time.Now().Unix()); err != nil || !requeued {
		t.Fatalf("requeue expired job: requeued=%v err=%v", requeued, err)
	}
	// requeuedJob、err 保存重新入队后的任务及错误。
	requeuedJob, err := store.OrderRefreshJobs.Get(ctx, userID, expiredJob.ID)
	if err != nil || requeuedJob.Status != "queued" || requeuedJob.WorkerToken != "" {
		t.Fatalf("requeued job=%+v err=%v", requeuedJob, err)
	}
	// err 表示跨用户读取任务的查询错误。
	if _, err := store.OrderRefreshJobs.Get(ctx, userID+1, job.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("cross-user job lookup should be hidden, err=%v", err)
	}
}
