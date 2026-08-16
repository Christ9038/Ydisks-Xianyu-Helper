package adapter

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// TestItemBatchRepositoryCreatesPreviewFromApplicationModels 验证预检应用模型到数据库字段的转换。
func TestItemBatchRepositoryCreatesPreviewFromApplicationModels(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// repository 是绑定测试数据库的预检批次适配器。
	repository := NewItemBatchRepository(store)
	// expectedRaw 保存预检行原始输入，验证 JSON 边界不会丢失字段。
	expectedRaw := map[string]any{"title": "应用模型商品"}
	// rows 保存一条有效行和一条校验失败行。
	rows := []itemapp.BatchPreviewRow{
		{RowNo: 2, CookieID: "cid", Title: "应用模型商品", Price: "10", Quantity: 2, Raw: expectedRaw, Category: itemapp.BatchPreviewCategory{CatID: "cat-1"}},
		{RowNo: 3, CookieID: "cid", Title: "失败商品", Errors: []string{"标题错误"}},
	}
	// batch 保存应用层批次元数据。
	batch := itemapp.BatchPreviewPersistenceBatch{ID: "preview-adapter", UserID: 1, DefaultCookieID: "cid", Filename: "items.csv", UploadDir: t.TempDir(), Location: itemapp.Location{DivisionID: "3301"}}
	// err 保存应用模型落库错误。
	if err := repository.CreateBatch(ctx, batch, rows); err != nil {
		t.Fatalf("创建预检批次失败: %v", err)
	}
	// storedBatch、batchErr 保存数据库批次读取结果。
	storedBatch, batchErr := store.PublishBatches.Get(ctx, 1, batch.ID)
	if batchErr != nil || storedBatch.Status != "preview" || storedBatch.LocationJSON == "{}" {
		t.Fatalf("批次字段映射异常: batch=%+v err=%v", storedBatch, batchErr)
	}
	// storedRows、rowsErr 保存数据库明细读取结果。
	storedRows, rowsErr := store.PublishBatches.Rows(ctx, batch.ID)
	if rowsErr != nil || len(storedRows) != 2 || storedRows[1].Status != "failed" || storedRows[1].FailureKind != "validation" {
		t.Fatalf("明细状态映射异常: rows=%+v err=%v", storedRows, rowsErr)
	}
	// decodedRaw 保存读取后的原始 JSON 对象。
	decodedRaw := map[string]any{}
	if json.Unmarshal([]byte(storedRows[0].RawJSON), &decodedRaw) != nil || decodedRaw["title"] != expectedRaw["title"] {
		t.Fatalf("原始字段 JSON 映射异常: %s", storedRows[0].RawJSON)
	}
}

// TestItemBatchRepositoryRejectsMissingStore 验证批量 worker 适配器缺少数据库时快速失败。
func TestItemBatchRepositoryRejectsMissingStore(t *testing.T) {
	// repository 保存未装配数据库的批量仓储适配器。
	repository := NewItemBatchRepository(nil)
	// ctx 是本测试使用的非取消上下文。
	ctx := context.Background()
	// rowsErr 保存待处理明细查询的装配错误。
	if _, rowsErr := repository.PendingRows(ctx, "batch-1", false); rowsErr == nil {
		t.Fatal("缺少数据库时 PendingRows 不应伪装成功")
	}
	// batchErr 保存批次查询的装配错误。
	if _, batchErr := repository.GetBatch(ctx, 1, "batch-1"); batchErr == nil {
		t.Fatal("缺少数据库时 GetBatch 不应伪装成功")
	}
	// deleteErr 保存上传目录清理的装配错误。
	if deleteErr := repository.DeleteUpload(ctx, "batch-1", "upload"); deleteErr == nil {
		t.Fatal("缺少数据库时 DeleteUpload 不应伪装成功")
	}
}

// TestItemBatchRepositoryMapsAndCleansBatch 验证批次行和状态完整映射及上传目录清理。
func TestItemBatchRepositoryMapsAndCleansBatch(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// admin、adminErr 保存测试用户及读取错误。
	admin, adminErr := store.Users.GetByUsername(ctx, "admin")
	if adminErr != nil {
		t.Fatalf("读取测试用户失败: %v", adminErr)
	}
	// uploadDir 是批次上传目录的临时路径。
	uploadDir := filepath.Join(t.TempDir(), "batch-upload")
	// err 保存创建上传目录的错误。
	if err := os.MkdirAll(uploadDir, 0o750); err != nil {
		t.Fatalf("创建上传目录失败: %v", err)
	}
	// batch 保存待适配的数据库批次。
	batch := &db.ItemPublishBatch{
		ID: "batch-adapter", UserID: admin.ID, DefaultCookieID: "cid", Filename: "items.csv",
		UploadDir: uploadDir, LocationJSON: `{"area":"杭州"}`, Status: "running",
	}
	// rows 保存待适配的批次明细。
	rows := []db.ItemPublishBatchRow{{
		RowNo: 2, CookieID: "cid", Title: "标题", Description: "描述", Price: "12.00",
		OriginalPrice: "15.00", Quantity: 3, PostageMode: "free", Postage: "0",
		ImagesJSON: `["a.jpg"]`, CategoryJSON: `{"cat_id":"1"}`, AutomationJSON: `{"enabled":true}`,
		Status: "pending", ItemID: "item-1", ItemURL: "https://example/item-1", ErrorMessage: "",
		FailureKind: "", WorkerToken: "worker", RawJSON: `{"ok":true}`,
	}}
	// err 保存创建测试批次的错误。
	if err := store.PublishBatches.Create(ctx, batch, rows); err != nil {
		t.Fatalf("创建测试批次失败: %v", err)
	}
	// repository 是绑定测试数据库的应用 Port 适配器。
	repository := NewItemBatchRepository(store)
	// pending、pendingErr 保存应用层待处理明细及查询错误。
	pending, pendingErr := repository.PendingRows(ctx, batch.ID, false)
	if pendingErr != nil || len(pending) != 1 {
		t.Fatalf("读取待处理明细异常：rows=%+v err=%v", pending, pendingErr)
	}
	// row 保存已转换的应用明细。
	row := pending[0]
	if row.RowNo != 2 || row.Title != "标题" || row.ItemID != "item-1" || row.RawJSON != `{"ok":true}` {
		t.Fatalf("批次明细映射不完整：%+v", row)
	}
	// gotBatch、batchErr 保存应用层批次状态及查询错误。
	gotBatch, batchErr := repository.GetBatch(ctx, admin.ID, batch.ID)
	if batchErr != nil || gotBatch.ID != batch.ID || gotBatch.UserID != admin.ID || gotBatch.UploadDir != uploadDir || gotBatch.LocationJSON != batch.LocationJSON {
		t.Fatalf("批次状态映射异常：batch=%+v err=%v", gotBatch, batchErr)
	}
	// updateErr 保存将批次置为过期状态的测试更新结果。
	_, updateErr := store.DB.ExecContext(ctx, `UPDATE item_publish_batches SET status='completed',updated_at='2000-01-01 00:00:00' WHERE id=?`, batch.ID)
	if updateErr != nil {
		t.Fatalf("设置过期批次失败: %v", updateErr)
	}
	// expired、expiredErr 保存过期上传批次查询结果。
	expired, expiredErr := repository.ExpiredUploadBatches(ctx, "2020-01-01 00:00:00", 10)
	if expiredErr != nil || len(expired) != 1 || expired[0].ID != batch.ID || expired[0].UploadDir != uploadDir {
		t.Fatalf("过期上传批次映射异常：batches=%+v err=%v", expired, expiredErr)
	}
	// deleteErr 保存上传目录与数据库字段清理结果。
	if deleteErr := repository.DeleteUpload(ctx, batch.ID, uploadDir); deleteErr != nil {
		t.Fatalf("清理上传目录失败: %v", deleteErr)
	}
	// statErr 保存确认上传目录已删除的文件系统错误。
	if _, statErr := os.Stat(uploadDir); !os.IsNotExist(statErr) {
		t.Fatalf("上传目录仍存在：err=%v", statErr)
	}
	// clearedBatch、clearedErr 保存清理后的批次记录。
	clearedBatch, clearedErr := store.PublishBatches.Get(ctx, admin.ID, batch.ID)
	if clearedErr != nil || clearedBatch.UploadDir != "" {
		t.Fatalf("数据库上传目录未清理：batch=%+v err=%v", clearedBatch, clearedErr)
	}
}

// TestBatchRowApplicationModelPreservesSensitiveFreeBusinessFields 验证明细转换保留业务字段且不引入凭证字段。
func TestBatchRowApplicationModelPreservesSensitiveFreeBusinessFields(t *testing.T) {
	// source 保存带有完整业务字段的数据库明细。
	source := db.ItemPublishBatchRow{
		ID: 4, BatchID: "b", RowNo: 7, CookieID: "cid", Title: "标题", Description: "描述",
		Price: "1", OriginalPrice: "2", Quantity: 4, PostageMode: "buyer", Postage: "3",
		ImagesJSON: "[]", CategoryJSON: "{}", AutomationJSON: "{}", Status: "failed", ItemID: "i",
		ItemURL: "u", ErrorMessage: "失败", FailureKind: "publish", WorkerToken: "w",
		RawJSON: "{}", CreatedAt: "created", UpdatedAt: "updated",
	}
	// converted 保存转换后的应用明细。
	converted := batchRowApplicationModel(source)
	if converted.ID != source.ID || converted.BatchID != source.BatchID || converted.RowNo != source.RowNo || converted.CookieID != source.CookieID || converted.Title != source.Title || converted.Description != source.Description || converted.Price != source.Price || converted.OriginalPrice != source.OriginalPrice || converted.Quantity != source.Quantity || converted.PostageMode != source.PostageMode || converted.Postage != source.Postage || converted.ImagesJSON != source.ImagesJSON || converted.CategoryJSON != source.CategoryJSON || converted.AutomationJSON != source.AutomationJSON || converted.Status != source.Status || converted.ItemID != source.ItemID || converted.ItemURL != source.ItemURL || converted.ErrorMessage != source.ErrorMessage || converted.FailureKind != source.FailureKind || converted.WorkerToken != source.WorkerToken || converted.RawJSON != source.RawJSON || converted.CreatedAt != source.CreatedAt || converted.UpdatedAt != source.UpdatedAt {
		t.Fatalf("明细字段未完整转换：source=%+v converted=%+v", source, converted)
	}
}
