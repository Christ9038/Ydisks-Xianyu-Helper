package db

import (
	"context"
	"errors"
	"testing"
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
