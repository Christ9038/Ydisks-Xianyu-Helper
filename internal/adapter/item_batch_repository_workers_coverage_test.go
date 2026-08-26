package adapter

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// TestItemBatchRepositoryWorkerStatePaths 验证批量发布仓储适配器的租约、明细收口和取消流程。
func TestItemBatchRepositoryWorkerStatePaths(t *testing.T) {
	// store、cleanup 保存本测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试所有数据库调用共用的非取消上下文。
	ctx := context.Background()
	// repository 是绑定测试存储的批量发布适配器。
	repository := NewItemBatchRepository(store)
	// batchID、workerToken 标识主流程使用的批次和 worker 租约。
	batchID, workerToken := "adapter-worker-batch", "adapter-worker"
	// batch 保存主流程的批次元数据。
	batch := itemapp.BatchPreviewPersistenceBatch{ID: batchID, UserID: 1, DefaultCookieID: "cid", Filename: "items.csv", UploadDir: t.TempDir(), Status: "pending"}
	// rows 保存主流程的一条待发布商品明细。
	rows := []itemapp.BatchPreviewRow{{RowNo: 1, CookieID: "cid", Title: "批量商品", Price: "10", Quantity: 1}}
	// createErr 保存批次创建结果。
	createErr := repository.CreateBatch(ctx, batch, rows)
	if createErr != nil {
		t.Fatal(createErr)
	}
	// listedBatches、listedErr 保存批次列表查询结果。
	listedBatches, listedErr := repository.ListBatchesForUser(ctx, 1, 10)
	if listedErr != nil || len(listedBatches) != 1 || listedBatches[0].ID != batchID {
		t.Fatalf("listed batches=%+v err=%v", listedBatches, listedErr)
	}
	// listedRows、listedRowsErr 保存批次全部明细查询结果。
	listedRows, listedRowsErr := repository.ListBatchRows(ctx, batchID)
	if listedRowsErr != nil || len(listedRows) != 1 {
		t.Fatalf("listed rows=%+v err=%v", listedRows, listedRowsErr)
	}
	// pendingRows、pendingRowsErr 保存待处理明细查询结果。
	pendingRows, pendingRowsErr := repository.PendingRows(ctx, batchID, false)
	if pendingRowsErr != nil || len(pendingRows) != 1 {
		t.Fatalf("pending rows=%+v err=%v", pendingRows, pendingRowsErr)
	}
	// leaseExpiresAt 保存本次测试租约的未来截止时间。
	leaseExpiresAt := time.Now().UTC().Add(time.Minute).Unix()
	// claimed、claimErr 保存批次抢占结果。
	claimed, claimErr := repository.ClaimBatch(ctx, batchID, workerToken, leaseExpiresAt)
	if claimErr != nil || !claimed {
		t.Fatalf("claimed=%v err=%v", claimed, claimErr)
	}
	// renewed、renewErr 保存批次租约续期结果。
	renewed, renewErr := repository.RenewBatchLease(ctx, batchID, workerToken, leaseExpiresAt+60)
	if renewErr != nil || !renewed {
		t.Fatalf("renewed=%v err=%v", renewed, renewErr)
	}
	// reserved、reserveErr 保存发布时间槽预留结果。
	reserved, reserveErr := repository.ReservePublishSlot(ctx, batchID, workerToken, 0, 1000)
	if reserveErr != nil || !reserved {
		t.Fatalf("reserved=%v err=%v", reserved, reserveErr)
	}
	// batchStatus、batchStatusErr 保存批次当前状态。
	batchStatus, batchStatusErr := repository.BatchStatus(ctx, batchID)
	if batchStatusErr != nil || batchStatus != "running" {
		t.Fatalf("status=%q err=%v", batchStatus, batchStatusErr)
	}
	// rowID 保存唯一批次明细的数据库标识。
	rowID := listedRows[0].ID
	// rowClaimed、rowClaimErr 保存明细抢占结果。
	rowClaimed, rowClaimErr := repository.ClaimRow(ctx, rowID, workerToken)
	if rowClaimErr != nil || !rowClaimed {
		t.Fatalf("row claimed=%v err=%v", rowClaimed, rowClaimErr)
	}
	// remoteStarted、remoteStartedErr 保存远端副作用前检查点结果。
	remoteStarted, remoteStartedErr := store.PublishBatches.MarkClaimedRemoteStarted(ctx, rowID, workerToken)
	if remoteStartedErr != nil || !remoteStarted {
		t.Fatalf("remote started=%v err=%v", remoteStarted, remoteStartedErr)
	}
	// failedRow、failedRowErr 保存当前 worker 标记明细失败的结果。
	failedRow, failedRowErr := repository.MarkClaimedRowFailed(ctx, rowID, workerToken, "发布失败", "publish")
	if failedRowErr != nil || !failedRow {
		t.Fatalf("failed row=%v err=%v", failedRow, failedRowErr)
	}
	// recountErr 保存批次统计重算结果。
	recountErr := repository.RecountBatch(ctx, batchID)
	if recountErr != nil {
		t.Fatal(recountErr)
	}
	// resetErr 保存可重试失败明细重置结果。
	resetErr := repository.ResetFailed(ctx, batchID)
	if resetErr != nil {
		t.Fatal(resetErr)
	}
	// rowClaimedAgain、rowClaimAgainErr 保存重置后再次抢占明细的结果。
	rowClaimedAgain, rowClaimAgainErr := repository.ClaimRow(ctx, rowID, workerToken)
	if rowClaimAgainErr != nil || !rowClaimedAgain {
		t.Fatalf("row claimed again=%v err=%v", rowClaimedAgain, rowClaimAgainErr)
	}
	// successRow、successRowErr 保存远端成功检查点结果。
	successRow, successRowErr := repository.MarkClaimedRowSuccess(ctx, rowID, workerToken, "remote-1", "https://example/item", "")
	if successRowErr != nil || !successRow {
		t.Fatalf("success row=%v err=%v", successRow, successRowErr)
	}
	// finalStatus、finalized、finalizeErr 保存批次完成收口结果。
	finalStatus, finalized, finalizeErr := repository.FinalizeBatch(ctx, batchID, workerToken)
	if finalizeErr != nil || !finalized || finalStatus != "completed" {
		t.Fatalf("final status=%q finalized=%v err=%v", finalStatus, finalized, finalizeErr)
	}
	// canceledToken、running、cancelErr 保存已完成批次的直接取消结果。
	canceledToken, running, cancelErr := repository.RequestCancel(ctx, batchID)
	if cancelErr != nil || running || canceledToken != "" {
		t.Fatalf("cancel token=%q running=%v err=%v", canceledToken, running, cancelErr)
	}
	// recoverableBatches、recoverableErr 保存恢复批次查询结果。
	recoverableBatches, recoverableErr := repository.RecoverableBatches(ctx, time.Now().Unix(), 10)
	if recoverableErr != nil || recoverableBatches == nil {
		t.Fatalf("recoverable batches=%+v err=%v", recoverableBatches, recoverableErr)
	}

	// interruptedBatchID 标识中断收口流程使用的批次。
	interruptedBatchID := "adapter-interrupted-batch"
	// interruptedBatch 保存中断流程的批次元数据。
	interruptedBatch := &db.ItemPublishBatch{ID: interruptedBatchID, UserID: 1, DefaultCookieID: "cid", Status: "pending"}
	// interruptedCreateErr 保存中断批次创建错误。
	interruptedCreateErr := store.PublishBatches.Create(ctx, interruptedBatch, []db.ItemPublishBatchRow{{RowNo: 1, CookieID: "cid", Title: "中断商品", Status: "pending"}})
	if interruptedCreateErr != nil {
		t.Fatal(interruptedCreateErr)
	}
	// interruptedClaimed、interruptedClaimErr 保存中断批次抢占结果。
	interruptedClaimed, interruptedClaimErr := repository.ClaimBatch(ctx, interruptedBatchID, "interrupted-worker", leaseExpiresAt)
	if interruptedClaimErr != nil || !interruptedClaimed {
		t.Fatalf("interrupted claimed=%v err=%v", interruptedClaimed, interruptedClaimErr)
	}
	// interruptedStatus、interruptedFinalized、interruptedErr 保存中断批次收口结果。
	interruptedStatus, interruptedFinalized, interruptedErr := repository.FinalizeInterrupted(ctx, interruptedBatchID, "interrupted-worker", "进程中断")
	if interruptedErr != nil || !interruptedFinalized || interruptedStatus != "failed" {
		t.Fatalf("interrupted status=%q finalized=%v err=%v", interruptedStatus, interruptedFinalized, interruptedErr)
	}

	// cancelBatchID 标识两阶段取消流程使用的批次。
	cancelBatchID := "adapter-cancel-batch"
	// cancelBatch 保存两阶段取消流程的批次元数据。
	cancelBatch := &db.ItemPublishBatch{ID: cancelBatchID, UserID: 1, DefaultCookieID: "cid", Status: "pending"}
	// cancelCreateErr 保存取消批次创建错误。
	cancelCreateErr := store.PublishBatches.Create(ctx, cancelBatch, []db.ItemPublishBatchRow{{RowNo: 1, CookieID: "cid", Title: "取消商品", Status: "pending"}})
	if cancelCreateErr != nil {
		t.Fatal(cancelCreateErr)
	}
	// cancelClaimed、cancelClaimErr 保存取消批次抢占结果。
	cancelClaimed, cancelClaimErr := repository.ClaimBatch(ctx, cancelBatchID, "cancel-worker", leaseExpiresAt)
	if cancelClaimErr != nil || !cancelClaimed {
		t.Fatalf("cancel claimed=%v err=%v", cancelClaimed, cancelClaimErr)
	}
	// cancelToken、cancelRunning、cancelRequestErr 保存进入取消中状态的结果。
	cancelToken, cancelRunning, cancelRequestErr := repository.RequestCancel(ctx, cancelBatchID)
	if cancelRequestErr != nil || !cancelRunning || cancelToken != "cancel-worker" {
		t.Fatalf("cancel request token=%q running=%v err=%v", cancelToken, cancelRunning, cancelRequestErr)
	}
	// canceled、canceledErr 保存取消批次最终收口结果。
	canceled, canceledErr := repository.FinalizeCanceled(ctx, cancelBatchID, "cancel-worker")
	if canceledErr != nil || !canceled {
		t.Fatalf("canceled=%v err=%v", canceled, canceledErr)
	}
	// expiredCanceled、expiredCancelErr 保存过期取消扫描结果。
	expiredCanceled, expiredCancelErr := repository.FinalizeExpiredCancellation(ctx, cancelBatchID, time.Now().Unix()+1)
	if expiredCancelErr != nil || expiredCanceled {
		t.Fatalf("expired canceled=%v err=%v", expiredCanceled, expiredCancelErr)
	}
	// failedClaimed、failClaimErr 保存已完成取消批次的租约释放结果。
	failedClaimed, failClaimErr := repository.FailClaimedBatch(ctx, cancelBatchID, "cancel-worker")
	if failClaimErr != nil || failedClaimed {
		t.Fatalf("已收口批次不应再次失败 claimed=%v err=%v", failedClaimed, failClaimErr)
	}
	// resetInterruptedErr 保存已完成取消批次的中断重置结果。
	if resetInterruptedErr := repository.ResetInterrupted(ctx, cancelBatchID); resetInterruptedErr != nil {
		t.Fatal(resetInterruptedErr)
	}

	// deleteBatchID、deleteUploadDir 标识删除流程使用的批次及受控目录。
	deleteBatchID, deleteUploadDir := "adapter-delete-batch", filepath.Join(t.TempDir(), "upload")
	// mkdirErr 保存删除测试目录创建结果。
	mkdirErr := os.MkdirAll(deleteUploadDir, 0o750)
	if mkdirErr != nil {
		t.Fatal(mkdirErr)
	}
	// deleteBatch 保存删除流程的批次元数据。
	deleteBatch := &db.ItemPublishBatch{ID: deleteBatchID, UserID: 1, DefaultCookieID: "cid", UploadDir: deleteUploadDir, Status: "pending"}
	// deleteCreateErr 保存删除批次创建错误。
	deleteCreateErr := store.PublishBatches.Create(ctx, deleteBatch, nil)
	if deleteCreateErr != nil {
		t.Fatal(deleteCreateErr)
	}
	// deleteErr 保存用户范围内批次和目录删除结果。
	deleteErr := repository.DeleteBatch(ctx, 1, deleteBatchID)
	if deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// _, statErr 保存删除测试目录的文件系统查询结果。
	if _, statErr := os.Stat(deleteUploadDir); !os.IsNotExist(statErr) {
		t.Fatalf("delete upload dir err=%v", statErr)
	}
}
