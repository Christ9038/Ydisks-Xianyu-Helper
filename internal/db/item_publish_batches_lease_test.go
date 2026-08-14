package db

import (
	"context"
	"testing"
	"time"
)

// TestFailClaimedBatchRequiresCurrentWorker 负责TestFailClaimed批次RequiresCurrent工作器相关处理。
func TestFailClaimedBatchRequiresCurrentWorker(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	if // ok、err 保存ok、err，供当前处理流程使用
	ok, err := store.Users.Create(ctx, "lease-owner", "lease-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner、err 保存owner、err，供当前处理流程使用
	owner, err := store.Users.GetByUsername(ctx, "lease-owner")
	if err != nil {
		t.Fatal(err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.PublishBatches.Create(ctx, &ItemPublishBatch{
		ID: "lease-failure", UserID: owner.ID, Filename: "test.csv", Status: "pending",
	}, []ItemPublishBatchRow{{RowNo: 1, Title: "item", Price: "1"}}); err != nil {
		t.Fatal(err)
	}
	if // claimed、err 保存claimed、err，供当前处理流程使用
	claimed, err := store.PublishBatches.ClaimBatch(ctx, "lease-failure", "current", time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}
	if // released、err 保存released、err，供当前处理流程使用
	released, err := store.PublishBatches.FailClaimedBatch(ctx, "lease-failure", "stale"); err != nil || released {
		t.Fatalf("stale release: released=%v err=%v", released, err)
	}
	if // released、err 保存released、err，供当前处理流程使用
	released, err := store.PublishBatches.FailClaimedBatch(ctx, "lease-failure", "current"); err != nil || !released {
		t.Fatalf("current release: released=%v err=%v", released, err)
	}
	// batch、err 保存batch、err，供当前处理流程使用
	batch, err := store.PublishBatches.Get(ctx, owner.ID, "lease-failure")
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != "failed" || batch.WorkerToken != "" || batch.LeaseExpiresAt != 0 {
		t.Fatalf("unexpected released batch: %+v", batch)
	}
}
