package db

import (
	"context"
	"testing"
	"time"
)

// TestFailClaimedBatchRequiresCurrentWorker 封装TestFailClaimed批次RequiresCurrent工作器业务协调。
func TestFailClaimedBatchRequiresCurrentWorker(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "lease-owner", "lease-owner@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create owner: ok=%v err=%v", ok, err)
	}
	// owner、err 用于本次流程后续判断的owner、err
	owner, err := store.Users.GetByUsername(ctx, "lease-owner")
	if err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.PublishBatches.Create(ctx, &ItemPublishBatch{
		ID: "lease-failure", UserID: owner.ID, Filename: "test.csv", Status: "pending",
	}, []ItemPublishBatchRow{{RowNo: 1, Title: "item", Price: "1"}}); err != nil {
		t.Fatal(err)
	}
	if // claimed、err 用于本次流程后续判断的claimed、err
	claimed, err := store.PublishBatches.ClaimBatch(ctx, "lease-failure", "current", time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
		t.Fatalf("claim: claimed=%v err=%v", claimed, err)
	}
	if // released、err 用于本次流程后续判断的released、err
	released, err := store.PublishBatches.FailClaimedBatch(ctx, "lease-failure", "stale"); err != nil || released {
		t.Fatalf("stale release: released=%v err=%v", released, err)
	}
	if // released、err 用于本次流程后续判断的released、err
	released, err := store.PublishBatches.FailClaimedBatch(ctx, "lease-failure", "current"); err != nil || !released {
		t.Fatalf("current release: released=%v err=%v", released, err)
	}
	// batch、err 用于本次流程后续判断的batch、err
	batch, err := store.PublishBatches.Get(ctx, owner.ID, "lease-failure")
	if err != nil {
		t.Fatal(err)
	}
	if batch.Status != "failed" || batch.WorkerToken != "" || batch.LeaseExpiresAt != 0 {
		t.Fatalf("unexpected released batch: %+v", batch)
	}
}
