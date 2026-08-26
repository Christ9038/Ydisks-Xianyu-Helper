package db

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestPublishBatchWorkerMaintenancePaths 覆盖批次过期清理、租约续期、删除和中断行归类路径。
func TestPublishBatchWorkerMaintenancePaths(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// userID 保存批次所属用户标识。
	userID, _ := seedAccount(t, store)
	// expiredID、createErr 保存待过期批次创建结果和数据库错误。
	expiredID := "expired-maintenance"
	// createErr 保存待过期批次创建时的数据库错误。
	createErr := store.PublishBatches.Create(ctx, makePublishBatch(userID, expiredID), []ItemPublishBatchRow{{RowNo: 1, Title: "过期商品"}})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// updateErr 保存将批次更新时间调整到过期窗口之前的数据库错误。
	if _, updateErr := store.PublishBatches.DB.ExecContext(ctx, "UPDATE item_publish_batches SET updated_at=? WHERE id=?", "2000-01-01 00:00:00", expiredID); updateErr != nil {
		t.Fatal(updateErr)
	}
	// expired、expiredErr 保存过期上传目录扫描结果和数据库错误。
	expired, expiredErr := store.PublishBatches.ExpiredUploads(ctx, "2020-01-01 00:00:00", 0)
	if expiredErr != nil || len(expired) != 1 || expired[0].ID != expiredID {
		t.Fatalf("expired batches=%#v err=%v", expired, expiredErr)
	}
	// clearErr 保存清理批次上传目录结果。
	if clearErr := store.PublishBatches.ClearUploadDir(ctx, expiredID); clearErr != nil {
		t.Fatal(clearErr)
	}
	// deleteErr 保存删除已清理批次结果。
	if deleteErr := store.PublishBatches.Delete(ctx, userID, expiredID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// deletedAgainErr 保存重复删除批次时的资源不存在错误。
	deletedAgainErr := store.PublishBatches.Delete(ctx, userID, expiredID)
	if !errors.Is(deletedAgainErr, ErrNotFound) {
		t.Fatalf("repeated batch delete error=%v", deletedAgainErr)
	}
	// batchID 保存行状态维护测试批次标识。
	batchID := "running-maintenance"
	// batchErr 保存批次及两条明细创建结果。
	batchErr := store.PublishBatches.Create(ctx, makePublishBatch(userID, batchID), []ItemPublishBatchRow{{RowNo: 1, Title: "远端未确认"}, {RowNo: 2, Title: "本地处理中"}})
	if batchErr != nil {
		t.Fatal(batchErr)
	}
	// claimed、claimErr 保存批次租约领取结果和数据库错误。
	claimed, claimErr := store.PublishBatches.ClaimBatch(ctx, batchID, "maintenance-worker", time.Now().Add(time.Minute).Unix())
	if claimErr != nil || !claimed {
		t.Fatalf("claim batch: claimed=%v err=%v", claimed, claimErr)
	}
	// batchRows、rowsErr 保存批次明细及数据库错误。
	batchRows, rowsErr := store.PublishBatches.Rows(ctx, batchID)
	if rowsErr != nil || len(batchRows) != 2 {
		t.Fatalf("batch rows=%#v err=%v", batchRows, rowsErr)
	}
	// firstClaimed、firstClaimErr 保存第一条明细领取结果和数据库错误。
	firstClaimed, firstClaimErr := store.PublishBatches.ClaimRow(ctx, batchRows[0].ID, "maintenance-worker")
	if firstClaimErr != nil || !firstClaimed {
		t.Fatalf("first row claim: claimed=%v err=%v", firstClaimed, firstClaimErr)
	}
	// remoteStarted、remoteStartErr 保存远端发布前断点写入结果和数据库错误。
	remoteStarted, remoteStartErr := store.PublishBatches.MarkClaimedRemoteStarted(ctx, batchRows[0].ID, "maintenance-worker")
	if remoteStartErr != nil || !remoteStarted {
		t.Fatalf("remote-start checkpoint: started=%v err=%v", remoteStarted, remoteStartErr)
	}
	// secondClaimed、secondClaimErr 保存第二条明细领取结果和数据库错误。
	secondClaimed, secondClaimErr := store.PublishBatches.ClaimRow(ctx, batchRows[1].ID, "maintenance-worker")
	if secondClaimErr != nil || !secondClaimed {
		t.Fatalf("second row claim: claimed=%v err=%v", secondClaimed, secondClaimErr)
	}
	// renewOK、renewErr 保存当前 worker 续期结果和数据库错误。
	renewOK, renewErr := store.PublishBatches.RenewBatchLease(ctx, batchID, "maintenance-worker", time.Now().Add(2*time.Minute).Unix())
	if renewErr != nil || !renewOK {
		t.Fatalf("renew lease: ok=%v err=%v", renewOK, renewErr)
	}
	// staleRenewOK、staleRenewErr 保存错误 worker 续期结果和数据库错误。
	staleRenewOK, staleRenewErr := store.PublishBatches.RenewBatchLease(ctx, batchID, "stale-worker", time.Now().Add(2*time.Minute).Unix())
	if staleRenewErr != nil || staleRenewOK {
		t.Fatalf("stale renew: ok=%v err=%v", staleRenewOK, staleRenewErr)
	}
	// failRunningErr 保存将运行中明细统一归类为中断失败的结果。
	if failRunningErr := store.PublishBatches.MarkRunningFailed(ctx, batchID, "进程中断"); failRunningErr != nil {
		t.Fatal(failRunningErr)
	}
	// failedRows、failedRowsErr 保存第一批次的失败明细及数据库错误。
	failedRows, failedRowsErr := store.PublishBatches.Rows(ctx, batchID)
	if failedRowsErr != nil || len(failedRows) != 2 || failedRows[0].FailureKind != "uncertain_remote" || failedRows[1].FailureKind != "interrupted" {
		t.Fatalf("failed rows=%#v err=%v", failedRows, failedRowsErr)
	}
	// unfinishedID 保存待测 pending/running 统一失败批次标识。
	unfinishedID := "unfinished-maintenance"
	// unfinishedCreateErr 保存第二个批次创建结果和数据库错误。
	unfinishedCreateErr := store.PublishBatches.Create(ctx, makePublishBatch(userID, unfinishedID), []ItemPublishBatchRow{{RowNo: 1, Title: "远端未确认"}, {RowNo: 2, Title: "待处理"}})
	if unfinishedCreateErr != nil {
		t.Fatal(unfinishedCreateErr)
	}
	// unfinishedClaimed、unfinishedClaimErr 保存第二批次领取结果和数据库错误。
	unfinishedClaimed, unfinishedClaimErr := store.PublishBatches.ClaimBatch(ctx, unfinishedID, "unfinished-worker", time.Now().Add(time.Minute).Unix())
	if unfinishedClaimErr != nil || !unfinishedClaimed {
		t.Fatalf("unfinished claim: claimed=%v err=%v", unfinishedClaimed, unfinishedClaimErr)
	}
	// unfinishedRows、unfinishedRowsErr 保存第二批次明细及数据库错误。
	unfinishedRows, unfinishedRowsErr := store.PublishBatches.Rows(ctx, unfinishedID)
	if unfinishedRowsErr != nil || len(unfinishedRows) != 2 {
		t.Fatalf("unfinished rows=%#v err=%v", unfinishedRows, unfinishedRowsErr)
	}
	// unfinishedRowClaimed、unfinishedRowClaimErr 保存第二批次第一行领取结果和数据库错误。
	unfinishedRowClaimed, unfinishedRowClaimErr := store.PublishBatches.ClaimRow(ctx, unfinishedRows[0].ID, "unfinished-worker")
	if unfinishedRowClaimErr != nil || !unfinishedRowClaimed {
		t.Fatalf("unfinished row claim: claimed=%v err=%v", unfinishedRowClaimed, unfinishedRowClaimErr)
	}
	// unfinishedRemoteStarted、unfinishedRemoteErr 保存第二批次远端断点结果和数据库错误。
	unfinishedRemoteStarted, unfinishedRemoteErr := store.PublishBatches.MarkClaimedRemoteStarted(ctx, unfinishedRows[0].ID, "unfinished-worker")
	if unfinishedRemoteErr != nil || !unfinishedRemoteStarted {
		t.Fatalf("unfinished remote checkpoint: started=%v err=%v", unfinishedRemoteStarted, unfinishedRemoteErr)
	}
	// markUnfinishedErr 保存 pending/running 明细统一失败的结果。
	if markUnfinishedErr := store.PublishBatches.MarkUnfinishedFailed(ctx, unfinishedID, "任务中断"); markUnfinishedErr != nil {
		t.Fatal(markUnfinishedErr)
	}
	// finalRows、finalRowsErr 保存第二批次统一失败后的明细及数据库错误。
	finalRows, finalRowsErr := store.PublishBatches.Rows(ctx, unfinishedID)
	if finalRowsErr != nil || len(finalRows) != 2 || finalRows[0].FailureKind != "uncertain_remote" || finalRows[1].FailureKind != "interrupted" {
		t.Fatalf("final failed rows=%#v err=%v", finalRows, finalRowsErr)
	}
}
