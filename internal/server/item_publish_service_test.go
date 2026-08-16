package server

import (
	"context"
	"testing"

	itemapp "xianyu-go/internal/application/items"
)

// TestBatchManagementReadsPersistedPreview 验证批次管理应用服务可以读取预检持久化结果。
func TestBatchManagementReadsPersistedPreview(t *testing.T) {
	// srv、store 和 cleanup 构造并释放测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// service 是待验证的预检持久化应用服务。
	service := srv.itemBatchPreviewPersistenceApplication()
	// preview 和 err 保存预检持久化结果。
	preview, err := service.Persist(context.Background(), itemapp.BatchPreviewPersistenceBatch{
		UserID: 1, ID: "batch_service_test", DefaultCookieID: "acc1",
		Filename: "products.csv", UploadDir: "/tmp/publish-service-test",
		Location: itemapp.Location{DivisionID: "3301"},
	}, []itemapp.BatchPreviewRow{{
		RowNo: 2, CookieID: "acc1", Title: "服务层商品", Price: "12.50", Quantity: 1,
		Images: []string{"img/a.png"}, Category: itemapp.BatchPreviewCategory{CatID: "5001", CatName: "虚拟商品"},
	}},
	)
	if err != nil {
		t.Fatalf("PersistPreview error: %v", err)
	}
	if !preview.Success || preview.PreviewID != "batch_service_test" || preview.Valid != 1 || preview.Invalid != 0 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	// got 和 err 保存从数据库读取的批次视图。
	details, err := srv.itemBatchManagementApplication().GetBatch(context.Background(), 1, preview.PreviewID)
	if err != nil {
		t.Fatalf("GetBatch error: %v", err)
	}
	if details.Batch.ID != preview.PreviewID || len(details.Rows) != 1 || details.Rows[0].Title != "服务层商品" {
		t.Fatalf("unexpected batch: %+v", details)
	}
}

// TestBatchManagementStartBatchNotFound 验证批次管理服务对越权或不存在批次返回统一领域错误。
func TestBatchManagementStartBatchNotFound(t *testing.T) {
	// srv、store 和 cleanup 构造并释放测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// err 保存不存在批次的业务错误。
	_, err := srv.itemBatchManagementApplication().StartBatch(context.Background(), 1, "missing-batch", publishBatchLease)
	if err != itemapp.ErrBatchNotFound {
		t.Fatalf("error=%v want %v", err, itemapp.ErrBatchNotFound)
	}
}
