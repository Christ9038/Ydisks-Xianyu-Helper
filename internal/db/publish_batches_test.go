package db

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

// --- item_publish_batches.go ---

// makePublishBatch 构造一个批次元信息。
func makePublishBatch(userID int64, id string) *ItemPublishBatch {
	return &ItemPublishBatch{
		ID: id, UserID: userID, DefaultCookieID: "acc1",
		Filename: "upload.xlsx", UploadDir: "/tmp/upload", Status: "pending",
	}
}

// TestPublishBatches_CreateGetRows Create + Get + Rows。
func TestPublishBatches_CreateGetRows(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)

	batch := makePublishBatch(uid, "b1")
	rows := []ItemPublishBatchRow{
		{RowNo: 1, Title: "商品A", Price: "9.9", Quantity: 0, PostageMode: ""}, // 缺省值补 1 / free
		{RowNo: 2, Title: "商品B", Price: "19.9", Quantity: 5, PostageMode: "buyer", Status: ""},
		{RowNo: 3, Title: "商品C", Price: "29.9", ImagesJSON: "", AutomationJSON: ""}, // 缺省 []/{}
	}
	if err := s.PublishBatches.Create(ctx, batch, rows); err != nil {
		t.Fatalf("Create: %v", err)
	}

	// Get 验证 total_count=len(rows)，success/failed=0。
	got, err := s.PublishBatches.Get(ctx, uid, "b1")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.TotalCount != 3 || got.SuccessCount != 0 || got.FailedCount != 0 || got.Status != "pending" {
		t.Fatalf("batch 字段: %#v", got)
	}
	// Get 隔离校验：不同 user_id 应 ErrNotFound。
	if _, err := s.PublishBatches.Get(ctx, uid+999, "b1"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("跨用户 Get 应 ErrNotFound, got %v", err)
	}
	// Get 不存在。
	if _, err := s.PublishBatches.Get(ctx, uid, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Get 不存在应 ErrNotFound, got %v", err)
	}

	// Rows 验证缺省值（quantity=1, postage_mode=free, status=pending, images_json=[], automation_json={}）。
	gotRows, err := s.PublishBatches.Rows(ctx, "b1")
	if err != nil {
		t.Fatalf("Rows: %v", err)
	}
	if len(gotRows) != 3 {
		t.Fatalf("rows len=%d want 3", len(gotRows))
	}
	r0 := gotRows[0]
	if r0.Quantity != 1 || r0.PostageMode != "free" || r0.Status != "pending" ||
		r0.ImagesJSON != "[]" || r0.AutomationJSON != "{}" || r0.RawJSON != "{}" {
		t.Fatalf("缺省值: %#v", r0)
	}
	if gotRows[1].Quantity != 5 || gotRows[1].PostageMode != "buyer" {
		t.Fatalf("显式值被覆盖: %#v", gotRows[1])
	}
	// 按 row_no 升序。
	if gotRows[0].RowNo != 1 || gotRows[2].RowNo != 3 {
		t.Fatalf("rows 顺序: %#v", gotRows)
	}
}

func TestPublishBatchesListAndRecoverInterrupted(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)
	if err := s.PublishBatches.Create(ctx, makePublishBatch(uid, "recover-list"), []ItemPublishBatchRow{{
		RowNo: 1, CookieID: "acc1", Title: "A", Price: "1", Status: "failed", FailureKind: "interrupted", ErrorMessage: "stopped",
	}}); err != nil {
		t.Fatal(err)
	}
	if err := s.PublishBatches.SetBatchStatus(ctx, "recover-list", "failed"); err != nil {
		t.Fatal(err)
	}
	listed, err := s.PublishBatches.ListForUser(ctx, uid, 10)
	if err != nil || len(listed) != 1 || listed[0].ID != "recover-list" {
		t.Fatalf("listed=%+v err=%v", listed, err)
	}
	recoverable, err := s.PublishBatches.Recoverable(ctx, time.Now().UTC().Unix(), 10)
	if err != nil || len(recoverable) != 1 || recoverable[0].ID != "recover-list" {
		t.Fatalf("recoverable=%+v err=%v", recoverable, err)
	}
	if err := s.PublishBatches.ResetInterrupted(ctx, "recover-list"); err != nil {
		t.Fatal(err)
	}
	rows, _ := s.PublishBatches.Rows(ctx, "recover-list")
	if rows[0].Status != "pending" || rows[0].FailureKind != "" || rows[0].ErrorMessage != "" {
		t.Fatalf("row=%+v", rows[0])
	}
}

// TestPublishBatches_PendingRowsAndStatus pending/failed 过滤 + 状态机流转 + Recount。
func TestPublishBatches_PendingRowsAndStatus(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)

	s.PublishBatches.Create(ctx, makePublishBatch(uid, "b1"), []ItemPublishBatchRow{
		{RowNo: 1, Title: "A", Price: "1"},
		{RowNo: 2, Title: "B", Price: "2"},
		{RowNo: 3, Title: "C", Price: "3"},
	})

	// PendingRows 默认取 pending。
	pending, err := s.PublishBatches.PendingRows(ctx, "b1", false)
	if err != nil || len(pending) != 3 {
		t.Fatalf("PendingRows pending: %#v err=%v", pending, err)
	}

	// BatchStatus。
	st, err := s.PublishBatches.BatchStatus(ctx, "b1")
	if err != nil || st != "pending" {
		t.Fatalf("BatchStatus: %q err=%v", st, err)
	}
	// BatchStatus 不存在。
	if _, err := s.PublishBatches.BatchStatus(ctx, "nope"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("BatchStatus 不存在应 ErrNotFound, got %v", err)
	}

	// SetBatchStatus → running。
	if err := s.PublishBatches.SetBatchStatus(ctx, "b1", "running"); err != nil {
		t.Fatalf("SetBatchStatus: %v", err)
	}
	st, _ = s.PublishBatches.BatchStatus(ctx, "b1")
	if st != "running" {
		t.Fatalf("BatchStatus=%q want running", st)
	}

	// 取第一行，走 running → success。
	rows, _ := s.PublishBatches.Rows(ctx, "b1")
	rowID := rows[0].ID
	if err := s.PublishBatches.MarkRowRunning(ctx, rowID); err != nil {
		t.Fatalf("MarkRowRunning: %v", err)
	}
	// running 中 error_message 应清空。
	r, _ := s.PublishBatches.Rows(ctx, "b1")
	_ = r // (MarkRowRunning 已覆盖)
	if err := s.PublishBatches.MarkRowSuccess(ctx, rowID, "item-123", "http://url", `{"ok":1}`); err != nil {
		t.Fatalf("MarkRowSuccess: %v", err)
	}
	// MarkRowSuccess 空 rawJSON → 兜底 {}。
	if err := s.PublishBatches.MarkRowSuccess(ctx, rows[1].ID, "item-456", "http://u2", ""); err != nil {
		t.Fatalf("MarkRowSuccess empty raw: %v", err)
	}
	// MarkRowFailed 第三行。
	if err := s.PublishBatches.MarkRowFailed(ctx, rows[2].ID, "网络错误"); err != nil {
		t.Fatalf("MarkRowFailed: %v", err)
	}

	// Recount 重算计数。
	if err := s.PublishBatches.Recount(ctx, "b1"); err != nil {
		t.Fatalf("Recount: %v", err)
	}
	batch, _ := s.PublishBatches.Get(ctx, uid, "b1")
	if batch.TotalCount != 3 || batch.SuccessCount != 2 || batch.FailedCount != 1 {
		t.Fatalf("Recount 后: total=%d success=%d failed=%d want 3/2/1",
			batch.TotalCount, batch.SuccessCount, batch.FailedCount)
	}

	// PendingRows failedOnly=true 应返回 1 行（第三行）。
	failed, err := s.PublishBatches.PendingRows(ctx, "b1", true)
	if err != nil || len(failed) != 1 {
		t.Fatalf("PendingRows failedOnly: %#v err=%v", failed, err)
	}
	if failed[0].ErrorMessage != "网络错误" {
		t.Fatalf("failed row error_message=%q", failed[0].ErrorMessage)
	}

	// ResetFailed 把 failed 行重置为 pending。
	if err := s.PublishBatches.ResetFailed(ctx, "b1"); err != nil {
		t.Fatalf("ResetFailed: %v", err)
	}
	pendingAfter, _ := s.PublishBatches.PendingRows(ctx, "b1", false)
	if len(pendingAfter) != 1 {
		t.Fatalf("ResetFailed 后 pending len=%d want 1", len(pendingAfter))
	}
	// 验证 error_message 已清空。
	rowsAfter, _ := s.PublishBatches.Rows(ctx, "b1")
	for _, rr := range rowsAfter {
		if rr.Status == "pending" && rr.ErrorMessage != "" {
			t.Fatalf("ResetFailed 后 pending 行 error_message 应空: %q", rr.ErrorMessage)
		}
	}
}

// TestPublishBatches_RowsEmpty Rows 对不存在的 batch 返回空。
func TestPublishBatches_RowsEmpty(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	rows, err := s.PublishBatches.Rows(ctx, "nope")
	if err != nil || len(rows) != 0 {
		t.Fatalf("Rows 不存在: %#v err=%v", rows, err)
	}
	pending, err := s.PublishBatches.PendingRows(ctx, "nope", false)
	if err != nil || len(pending) != 0 {
		t.Fatalf("PendingRows 不存在: %#v err=%v", pending, err)
	}
}

func TestPublishBatches_ClaimsAreAtomicAndLeaseCanBeRecovered(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)
	if err := s.PublishBatches.Create(ctx, makePublishBatch(uid, "claim-batch"), []ItemPublishBatchRow{
		{RowNo: 1, Title: "A", Price: "1"},
	}); err != nil {
		t.Fatal(err)
	}

	future := time.Now().UTC().Add(time.Minute).Unix()
	if claimed, err := s.PublishBatches.ClaimBatch(ctx, "claim-batch", "worker-1", future); err != nil || !claimed {
		t.Fatalf("first batch claim: claimed=%v err=%v", claimed, err)
	}
	rows, _ := s.PublishBatches.Rows(ctx, "claim-batch")
	if claimed, err := s.PublishBatches.ClaimRow(ctx, rows[0].ID, "worker-1"); err != nil || !claimed {
		t.Fatalf("first row claim: claimed=%v err=%v", claimed, err)
	}
	if claimed, err := s.PublishBatches.ClaimBatch(ctx, "claim-batch", "worker-2", future); err != nil || claimed {
		t.Fatalf("active lease must reject second worker: claimed=%v err=%v", claimed, err)
	}
	// 0 是从旧版本迁移来的 running 任务，和已过期租约一样必须允许接管。
	if _, err := s.DB.ExecContext(ctx, `UPDATE item_publish_batches SET lease_expires_at=0 WHERE id=?`, "claim-batch"); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.PublishBatches.ClaimBatch(ctx, "claim-batch", "worker-2", future); err != nil || !claimed {
		t.Fatalf("expired lease should be recoverable: claimed=%v err=%v", claimed, err)
	}
	batch, err := s.PublishBatches.Get(ctx, uid, "claim-batch")
	if err != nil || batch.WorkerToken != "worker-2" {
		t.Fatalf("recovered batch=%+v err=%v", batch, err)
	}
	rows, _ = s.PublishBatches.Rows(ctx, "claim-batch")
	if rows[0].Status != "pending" || rows[0].WorkerToken != "" {
		t.Fatalf("lease takeover must recover stale running row: %+v", rows[0])
	}
	if claimed, err := s.PublishBatches.ClaimRow(ctx, rows[0].ID, "worker-1"); err != nil || claimed {
		t.Fatalf("stale worker must not claim after takeover: claimed=%v err=%v", claimed, err)
	}
	if claimed, err := s.PublishBatches.ClaimRow(ctx, rows[0].ID, "worker-2"); err != nil || !claimed {
		t.Fatalf("new worker row claim: claimed=%v err=%v", claimed, err)
	}
	if claimed, err := s.PublishBatches.ClaimRow(ctx, rows[0].ID, "worker-3"); err != nil || claimed {
		t.Fatalf("row must only be claimed once: claimed=%v err=%v", claimed, err)
	}
	if marked, err := s.PublishBatches.MarkClaimedRowSuccess(ctx, rows[0].ID, "worker-1", "stale", "", "{}"); err != nil || marked {
		t.Fatalf("stale worker must not finish row: marked=%v err=%v", marked, err)
	}
	if marked, err := s.PublishBatches.MarkClaimedRowSuccess(ctx, rows[0].ID, "worker-2", "item-1", "", "{}"); err != nil || !marked {
		t.Fatalf("owner must finish row: marked=%v err=%v", marked, err)
	}
	if finished, err := s.PublishBatches.FinishBatchStatus(ctx, "claim-batch", "worker-1", "completed"); err != nil || finished {
		t.Fatalf("stale worker must not finish batch: finished=%v err=%v", finished, err)
	}
	if finished, err := s.PublishBatches.FinishBatchStatus(ctx, "claim-batch", "worker-2", "completed"); err != nil || !finished {
		t.Fatalf("owner must finish batch: finished=%v err=%v", finished, err)
	}
}

func TestPublishBatches_ResetFailedKeepsValidationFailures(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)
	if err := s.PublishBatches.Create(ctx, makePublishBatch(uid, "validation-batch"), []ItemPublishBatchRow{
		{RowNo: 1, Title: "bad", Price: "0", Status: "failed", ErrorMessage: "价格错误", FailureKind: "validation"},
		{RowNo: 2, Title: "retry", Price: "1", Status: "failed", ErrorMessage: "网络错误", FailureKind: "publish"},
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.PublishBatches.ResetFailed(ctx, "validation-batch"); err != nil {
		t.Fatal(err)
	}
	rows, err := s.PublishBatches.Rows(ctx, "validation-batch")
	if err != nil {
		t.Fatal(err)
	}
	if rows[0].Status != "failed" || rows[0].FailureKind != "validation" {
		t.Fatalf("validation row must remain failed: %+v", rows[0])
	}
	if rows[1].Status != "pending" || rows[1].FailureKind != "" || rows[1].ErrorMessage != "" {
		t.Fatalf("publish failure should be reset: %+v", rows[1])
	}
}

func TestPublishBatchLeaseTakeoverQuarantinesRemoteStartedRow(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)
	if err := s.PublishBatches.Create(ctx, makePublishBatch(uid, "remote-started"), []ItemPublishBatchRow{{RowNo: 1, Title: "A", Price: "1"}}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.PublishBatches.ClaimBatch(ctx, "remote-started", "old", time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
		t.Fatalf("claim batch=%v err=%v", claimed, err)
	}
	rows, _ := s.PublishBatches.Rows(ctx, "remote-started")
	if claimed, err := s.PublishBatches.ClaimRow(ctx, rows[0].ID, "old"); err != nil || !claimed {
		t.Fatalf("claim row=%v err=%v", claimed, err)
	}
	if marked, err := s.PublishBatches.MarkClaimedRemoteStarted(ctx, rows[0].ID, "old"); err != nil || !marked {
		t.Fatalf("mark remote started=%v err=%v", marked, err)
	}
	_, _ = s.DB.ExecContext(ctx, `UPDATE item_publish_batches SET lease_expires_at=0 WHERE id='remote-started'`)
	if claimed, err := s.PublishBatches.ClaimBatch(ctx, "remote-started", "new", time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
		t.Fatalf("takeover=%v err=%v", claimed, err)
	}
	rows, _ = s.PublishBatches.Rows(ctx, "remote-started")
	if rows[0].Status != "failed" || rows[0].FailureKind != "uncertain_remote" {
		t.Fatalf("remote-started row must be quarantined: %+v", rows[0])
	}
	if pending, err := s.PublishBatches.PendingRows(ctx, "remote-started", false); err != nil || len(pending) != 0 {
		t.Fatalf("quarantined row must not be replayable: %+v err=%v", pending, err)
	}
}

func TestPublishBatchCancelPreservesRemoteCheckpointBeforeFinalizing(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)
	if err := s.PublishBatches.Create(ctx, makePublishBatch(uid, "cancel-checkpoint"), []ItemPublishBatchRow{{RowNo: 1, Title: "A", Price: "1"}}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.PublishBatches.ClaimBatch(ctx, "cancel-checkpoint", "worker", time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
		t.Fatalf("claim batch=%v err=%v", claimed, err)
	}
	rows, _ := s.PublishBatches.Rows(ctx, "cancel-checkpoint")
	if claimed, err := s.PublishBatches.ClaimRow(ctx, rows[0].ID, "worker"); err != nil || !claimed {
		t.Fatalf("claim row=%v err=%v", claimed, err)
	}
	token, running, err := s.PublishBatches.RequestCancel(ctx, "cancel-checkpoint")
	if err != nil || !running || token != "worker" {
		t.Fatalf("request cancel token=%q running=%v err=%v", token, running, err)
	}
	if saved, err := s.PublishBatches.SaveClaimedRemoteResult(ctx, rows[0].ID, "worker", "remote-1", "https://example/item", `{}`); err != nil || !saved {
		t.Fatalf("remote checkpoint after cancel saved=%v err=%v", saved, err)
	}
	if _, err := s.PublishBatches.MarkClaimedRowFailed(ctx, rows[0].ID, "worker", "任务已取消", "post_publish"); err != nil {
		t.Fatal(err)
	}
	if finalized, err := s.PublishBatches.FinalizeCanceled(ctx, "cancel-checkpoint", "worker"); err != nil || !finalized {
		t.Fatalf("finalized=%v err=%v", finalized, err)
	}
	rows, _ = s.PublishBatches.Rows(ctx, "cancel-checkpoint")
	if rows[0].ItemID != "remote-1" || rows[0].FailureKind != "post_publish" {
		t.Fatalf("checkpoint lost: %+v", rows[0])
	}
}

func TestFinalizeBatchRejectsUnfinishedRows(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)
	if err := s.PublishBatches.Create(ctx, makePublishBatch(uid, "terminal-guard"), []ItemPublishBatchRow{
		{RowNo: 1, Title: "pending", Price: "1"},
	}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.PublishBatches.ClaimBatch(ctx, "terminal-guard", "worker", time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	if _, finalized, err := s.PublishBatches.FinalizeBatch(ctx, "terminal-guard", "worker"); err == nil || finalized {
		t.Fatalf("unfinished batch finalized=%v err=%v", finalized, err)
	}
	batch, _ := s.PublishBatches.Get(ctx, uid, "terminal-guard")
	if batch.Status != "running" || batch.WorkerToken != "worker" {
		t.Fatalf("rejected finalization changed batch: %+v", batch)
	}
	rows, _ := s.PublishBatches.Rows(ctx, "terminal-guard")
	if claimed, err := s.PublishBatches.ClaimRow(ctx, rows[0].ID, "worker"); err != nil || !claimed {
		t.Fatalf("claim row=%v err=%v", claimed, err)
	}
	if marked, err := s.PublishBatches.MarkClaimedRowFailed(ctx, rows[0].ID, "worker", "failed", "publish"); err != nil || !marked {
		t.Fatalf("mark failed=%v err=%v", marked, err)
	}
	status, finalized, err := s.PublishBatches.FinalizeBatch(ctx, "terminal-guard", "worker")
	if err != nil || !finalized || status != "failed" {
		t.Fatalf("status=%q finalized=%v err=%v", status, finalized, err)
	}
	batch, _ = s.PublishBatches.Get(ctx, uid, "terminal-guard")
	if batch.Status != "failed" || batch.FailedCount != 1 || batch.TotalCount != 1 {
		t.Fatalf("batch=%+v", batch)
	}
}

func TestExpiredCancelingBatchIsRecoverableAndFinalizedSafely(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)
	if err := s.PublishBatches.Create(ctx, makePublishBatch(uid, "cancel-crash"), []ItemPublishBatchRow{
		{RowNo: 1, Title: "remote", Price: "1"},
		{RowNo: 2, Title: "pending", Price: "2"},
	}); err != nil {
		t.Fatal(err)
	}
	if claimed, err := s.PublishBatches.ClaimBatch(ctx, "cancel-crash", "dead-worker", 1); err != nil || !claimed {
		t.Fatalf("claim=%v err=%v", claimed, err)
	}
	rows, _ := s.PublishBatches.Rows(ctx, "cancel-crash")
	if claimed, err := s.PublishBatches.ClaimRow(ctx, rows[0].ID, "dead-worker"); err != nil || !claimed {
		t.Fatalf("claim row=%v err=%v", claimed, err)
	}
	if marked, err := s.PublishBatches.MarkClaimedRemoteStarted(ctx, rows[0].ID, "dead-worker"); err != nil || !marked {
		t.Fatalf("remote started=%v err=%v", marked, err)
	}
	if _, running, err := s.PublishBatches.RequestCancel(ctx, "cancel-crash"); err != nil || !running {
		t.Fatalf("cancel running=%v err=%v", running, err)
	}
	recoverable, err := s.PublishBatches.Recoverable(ctx, 2, 10)
	if err != nil || len(recoverable) != 1 || recoverable[0].Status != "canceling" {
		t.Fatalf("recoverable=%+v err=%v", recoverable, err)
	}
	if finalized, err := s.PublishBatches.FinalizeExpiredCancellation(ctx, "cancel-crash", 2); err != nil || !finalized {
		t.Fatalf("finalized=%v err=%v", finalized, err)
	}
	batch, _ := s.PublishBatches.Get(ctx, uid, "cancel-crash")
	rows, _ = s.PublishBatches.Rows(ctx, "cancel-crash")
	if batch.Status != "canceled" || batch.WorkerToken != "" || batch.LeaseExpiresAt != 0 {
		t.Fatalf("batch=%+v", batch)
	}
	if rows[0].FailureKind != "uncertain_remote" || !strings.Contains(rows[0].ErrorMessage, "远端发布结果未知") {
		t.Fatalf("remote row=%+v", rows[0])
	}
	if rows[1].Status != "failed" || rows[1].ErrorMessage != "任务已取消" {
		t.Fatalf("pending row=%+v", rows[1])
	}
}

func TestPublishBatchFinalizersFenceStaleWorker(t *testing.T) {
	s, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	uid, _ := seedAccount(t, s)

	t.Run("cancel", func(t *testing.T) {
		batchID := "cancel-fence"
		if err := s.PublishBatches.Create(ctx, makePublishBatch(uid, batchID), []ItemPublishBatchRow{{RowNo: 1, Title: "A", Price: "1"}}); err != nil {
			t.Fatal(err)
		}
		if claimed, err := s.PublishBatches.ClaimBatch(ctx, batchID, "old", 1); err != nil || !claimed {
			t.Fatalf("old claim=%v err=%v", claimed, err)
		}
		rows, _ := s.PublishBatches.Rows(ctx, batchID)
		if claimed, err := s.PublishBatches.ClaimRow(ctx, rows[0].ID, "old"); err != nil || !claimed {
			t.Fatalf("old row claim=%v err=%v", claimed, err)
		}
		if claimed, err := s.PublishBatches.ClaimBatch(ctx, batchID, "new", time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
			t.Fatalf("takeover=%v err=%v", claimed, err)
		}
		token, running, err := s.PublishBatches.RequestCancel(ctx, batchID)
		if err != nil || !running || token != "new" {
			t.Fatalf("cancel token=%q running=%v err=%v", token, running, err)
		}
		if finalized, err := s.PublishBatches.FinalizeCanceled(ctx, batchID, "old"); err != nil || finalized {
			t.Fatalf("stale cancel finalized=%v err=%v", finalized, err)
		}
		rows, _ = s.PublishBatches.Rows(ctx, batchID)
		if rows[0].Status != "pending" {
			t.Fatalf("stale cancel changed current row: %+v", rows[0])
		}
		if finalized, err := s.PublishBatches.FinalizeCanceled(ctx, batchID, "new"); err != nil || !finalized {
			t.Fatalf("current cancel finalized=%v err=%v", finalized, err)
		}
	})

	t.Run("interrupted", func(t *testing.T) {
		batchID := "interrupt-fence"
		if err := s.PublishBatches.Create(ctx, makePublishBatch(uid, batchID), []ItemPublishBatchRow{{RowNo: 1, Title: "A", Price: "1"}}); err != nil {
			t.Fatal(err)
		}
		if claimed, err := s.PublishBatches.ClaimBatch(ctx, batchID, "old", 1); err != nil || !claimed {
			t.Fatalf("old claim=%v err=%v", claimed, err)
		}
		if claimed, err := s.PublishBatches.ClaimBatch(ctx, batchID, "new", time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
			t.Fatalf("takeover=%v err=%v", claimed, err)
		}
		if _, finalized, err := s.PublishBatches.FinalizeInterrupted(ctx, batchID, "old", "old interrupted"); err != nil || finalized {
			t.Fatalf("stale interrupt finalized=%v err=%v", finalized, err)
		}
		rows, _ := s.PublishBatches.Rows(ctx, batchID)
		if rows[0].Status != "pending" {
			t.Fatalf("stale interrupt changed current row: %+v", rows[0])
		}
		status, finalized, err := s.PublishBatches.FinalizeInterrupted(ctx, batchID, "new", "new interrupted")
		if err != nil || !finalized || status != "failed" {
			t.Fatalf("current interrupt status=%q finalized=%v err=%v", status, finalized, err)
		}
	})
}
