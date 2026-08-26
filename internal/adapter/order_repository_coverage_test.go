package adapter

import (
	"context"
	"errors"
	"testing"

	orderapp "xianyu-go/internal/application/orders"
	"xianyu-go/internal/db"
)

// TestOrderRepositoryDelegatesBusinessQueries 覆盖订单适配器的归属、查询、写入和清理路径。
func TestOrderRepositoryDelegatesBusinessQueries(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// owner、ownerErr 保存测试用户及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// repository 保存订单数据库适配器。
	repository := NewOrderRepository(store)
	// owned、ownedErr 保存账号归属检查结果及数据库错误。
	owned, ownedErr := repository.ExistsOwned(ctx, owner.ID, "cid")
	if ownedErr != nil || !owned {
		t.Fatalf("owned account=%v err=%v", owned, ownedErr)
	}
	// cookieIDs、cookieErr 保存当前用户账号列表和数据库错误。
	cookieIDs, cookieErr := repository.ListOwnedIDs(ctx, owner.ID)
	if cookieErr != nil || len(cookieIDs) != 1 || cookieIDs[0] != "cid" {
		t.Fatalf("cookie IDs=%v err=%v", cookieIDs, cookieErr)
	}
	// itemErr 保存商品写入结果。
	if itemErr := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: "cid", ItemID: "item-1", ItemTitle: "商品一", ItemDetail: "详情"}); itemErr != nil {
		t.Fatal(itemErr)
	}
	// upsertErr 保存订单写入结果。
	if upsertErr := repository.UpsertOrder(ctx, "order-1", orderapp.UpsertOptions{CookieID: "cid", ItemID: "item-1", BuyerID: "buyer-1", OrderStatus: "paid", Amount: "12.50", Quantity: "1", ReceiverCity: "上海"}); upsertErr != nil {
		t.Fatal(upsertErr)
	}
	// secondUpsertErr 保存第二条订单写入结果。
	if secondUpsertErr := repository.UpsertOrder(ctx, "order-2", orderapp.UpsertOptions{CookieID: "cid", ItemID: "item-2", BuyerID: "buyer-2", OrderStatus: "shipped", Amount: "2.00", Quantity: "1"}); secondUpsertErr != nil {
		t.Fatal(secondUpsertErr)
	}
	// order、orderErr 保存订单详情及应用层错误。
	order, orderErr := repository.GetOrder(ctx, "order-1")
	if orderErr != nil || order == nil || order.ItemID != "item-1" {
		t.Fatalf("order=%#v err=%v", order, orderErr)
	}
	// missingOrder、missingFound、missingErr 保存不存在订单的兼容查询结果。
	missingOrder, missingFound, missingErr := repository.FindOrder(ctx, "missing-order")
	if missingErr != nil || missingFound || missingOrder != nil {
		t.Fatalf("missing order=%#v found=%v err=%v", missingOrder, missingFound, missingErr)
	}
	// ordersByID、batchErr 保存批量订单查询结果和数据库错误。
	ordersByID, batchErr := repository.FindOrdersByIDs(ctx, []string{"order-1", "order-2", "order-1"})
	if batchErr != nil || len(ordersByID) != 2 {
		t.Fatalf("orders by ID=%#v err=%v", ordersByID, batchErr)
	}
	// item、itemReadErr 保存订单商品查询结果及数据库错误。
	item, itemReadErr := repository.GetItem(ctx, "cid", "item-1")
	if itemReadErr != nil || item == nil || item.ItemTitle != "商品一" {
		t.Fatalf("item=%#v err=%v", item, itemReadErr)
	}
	// listed、total、listErr 保存用户订单列表、总数及数据库错误。
	listed, total, listErr := repository.ListOrdersForUser(ctx, orderapp.ListFilter{UserID: owner.ID, CookieID: "cid", Status: "paid", Limit: 10})
	if listErr != nil || len(listed) != 1 || listed[0].OrderID != "order-1" {
		t.Fatalf("listed=%#v err=%v", listed, listErr)
	}
	if total != 1 {
		t.Fatalf("listed total=%d want 1", total)
	}
	// cursorRows、cursorErr 保存订单游标查询结果及数据库错误。
	cursorRows, cursorErr := repository.ListOrdersByCookieCursor(ctx, "cid", 10, "", "")
	if cursorErr != nil || len(cursorRows) != 2 {
		t.Fatalf("cursor rows=%#v err=%v", cursorRows, cursorErr)
	}
	// patchedStatus 保存事务内订单补丁状态。
	patchedStatus := "completed"
	// transactionErr 保存应用层 Unit of Work 提交结果。
	transactionErr := repository.WithTransaction(ctx, func(writer orderapp.Writer) error {
		// patchErr 保存事务内订单状态更新错误。
		if patchErr := writer.PatchOrder(ctx, "order-1", orderapp.OrderPatch{OrderStatus: &patchedStatus}); patchErr != nil {
			return patchErr
		}
		// batchWriteErr 保存事务内批量订单写入错误。
		return writer.UpsertOrder(ctx, "order-3", orderapp.UpsertOptions{CookieID: "cid", ItemID: "item-3", OrderStatus: "pending_ship", Amount: "1.00"})
	})
	if transactionErr != nil {
		t.Fatal(transactionErr)
	}
	// batchUpsertErr 保存适配器批量订单写入结果。
	if batchUpsertErr := repository.BatchUpsertOrders(ctx, []orderapp.RefreshOrderWrite{{OrderID: "order-4", Options: orderapp.UpsertOptions{CookieID: "cid", OrderStatus: "paid", Amount: "4.00"}}}); batchUpsertErr != nil {
		t.Fatal(batchUpsertErr)
	}
	// platformData、platformErr 保存账号平台运行视图和数据库错误。
	platformData, platformErr := repository.LoadCookiePlatformDetail(ctx, "cid")
	if platformErr != nil || platformData == nil || platformData.ID != "cid" {
		t.Fatalf("platform data=%#v err=%v", platformData, platformErr)
	}
	// renewalErr 保存账号续期 Cookie 更新结果。
	if renewalErr := repository.UpdateRenewalCookie(ctx, "cid", "unb=1; renewed=1", `{}`, 123); renewalErr != nil {
		t.Fatal(renewalErr)
	}
	// deletedMissing、deleteMissingErr 保存远端缺失订单清理结果和数据库错误。
	deletedMissing, deleteMissingErr := repository.SoftDeleteMissingOrders(ctx, "cid", map[string]struct{}{"order-1": {}})
	if deleteMissingErr != nil || deletedMissing < 3 {
		t.Fatalf("deleted missing=%d err=%v", deletedMissing, deleteMissingErr)
	}
	// deletedOrder、deleteOrderErr 保存单条订单逻辑删除结果和数据库错误。
	deletedOrder, deleteOrderErr := repository.SoftDeleteOrder(ctx, "order-1")
	if deleteOrderErr != nil || !deletedOrder {
		t.Fatalf("soft delete order=%v err=%v", deletedOrder, deleteOrderErr)
	}
	// missingGetErr 保存逻辑删除后详情查询的未找到错误。
	_, missingGetErr := repository.GetOrder(ctx, "order-1")
	if !errors.Is(missingGetErr, orderapp.ErrNotFound) {
		t.Fatalf("deleted order error=%v", missingGetErr)
	}
	// unlock 保存凭证锁释放函数，确保适配器锁边界可正常闭合。
	unlock := repository.LockCredentials("cid")
	unlock()
	// job 保存订单刷新后台任务应用模型。
	job := &orderapp.RefreshJob{ID: "refresh-job-1", UserID: owner.ID, CookieID: "cid", FilterStatus: "paid"}
	// createJobErr 保存刷新任务创建错误。
	if createJobErr := repository.Create(ctx, job); createJobErr != nil {
		t.Fatal(createJobErr)
	}
	// loadedJob、loadJobErr 保存刷新任务读取结果及数据库错误。
	loadedJob, loadJobErr := repository.Get(ctx, owner.ID, job.ID)
	if loadJobErr != nil || loadedJob == nil || loadedJob.Status != "queued" || loadedJob.ResultJSON != "{}" {
		t.Fatalf("loaded job=%#v err=%v", loadedJob, loadJobErr)
	}
	// missingJob、missingJobErr 保存跨用户读取结果及应用层错误。
	missingJob, missingJobErr := repository.Get(ctx, owner.ID+1, job.ID)
	if !errors.Is(missingJobErr, orderapp.ErrRefreshJobNotFound) || missingJob != nil {
		t.Fatalf("missing job=%#v err=%v", missingJob, missingJobErr)
	}
	// claimedJob、claimJobErr 保存刷新任务租约领取结果和数据库错误。
	claimedJob, claimJobErr := repository.Claim(ctx, job.ID, "refresh-worker", 100)
	if claimJobErr != nil || !claimedJob {
		t.Fatalf("claim job=%v err=%v", claimedJob, claimJobErr)
	}
	// staleClaimed、staleClaimErr 保存重复领取结果和数据库错误。
	staleClaimed, staleClaimErr := repository.Claim(ctx, job.ID, "stale-worker", 200)
	if staleClaimErr != nil || staleClaimed {
		t.Fatalf("stale claim=%v err=%v", staleClaimed, staleClaimErr)
	}
	// recoverableJobs、recoverableErr 保存过期租约扫描结果和数据库错误。
	recoverableJobs, recoverableErr := repository.Recoverable(ctx, 200, 0)
	if recoverableErr != nil || len(recoverableJobs) != 1 || recoverableJobs[0].ID != job.ID {
		t.Fatalf("recoverable jobs=%#v err=%v", recoverableJobs, recoverableErr)
	}
	// requeueJob、requeueErr 保存过期任务重新入队结果和数据库错误。
	requeueJob, requeueErr := repository.RequeueExpired(ctx, job.ID, 200)
	if requeueErr != nil || !requeueJob {
		t.Fatalf("requeue job=%v err=%v", requeueJob, requeueErr)
	}
	// reclaimedJob、reclaimErr 保存重新领取任务结果和数据库错误。
	reclaimedJob, reclaimErr := repository.Claim(ctx, job.ID, "refresh-worker-2", 300)
	if reclaimErr != nil || !reclaimedJob {
		t.Fatalf("reclaim job=%v err=%v", reclaimedJob, reclaimErr)
	}
	// staleComplete、staleCompleteErr 保存错误 worker 完成结果和数据库错误。
	staleComplete, staleCompleteErr := repository.Complete(ctx, job.ID, "stale-worker", "succeeded", `{}`, "")
	if staleCompleteErr != nil || staleComplete {
		t.Fatalf("stale complete=%v err=%v", staleComplete, staleCompleteErr)
	}
	// completeJob、completeErr 保存当前 worker 完成结果和数据库错误。
	completeJob, completeErr := repository.Complete(ctx, job.ID, "refresh-worker-2", "succeeded", `{"count":1}`, "")
	if completeErr != nil || !completeJob {
		t.Fatalf("complete job=%v err=%v", completeJob, completeErr)
	}
	// cancelJob 保存待取消任务应用模型。
	cancelJob := &orderapp.RefreshJob{ID: "refresh-job-cancel", UserID: owner.ID, CookieID: "cid"}
	// createCancelErr 保存待取消任务创建错误。
	if createCancelErr := repository.Create(ctx, cancelJob); createCancelErr != nil {
		t.Fatal(createCancelErr)
	}
	// canceled、cancelErr 保存按用户取消任务结果和数据库错误。
	canceled, cancelErr := repository.Cancel(ctx, owner.ID, cancelJob.ID)
	if cancelErr != nil || !canceled {
		t.Fatalf("cancel job=%v err=%v", canceled, cancelErr)
	}
	// canceledAgain、cancelAgainErr 保存重复取消结果和数据库错误。
	canceledAgain, cancelAgainErr := repository.Cancel(ctx, owner.ID, cancelJob.ID)
	if cancelAgainErr != nil || canceledAgain {
		t.Fatalf("repeat cancel=%v err=%v", canceledAgain, cancelAgainErr)
	}
}
