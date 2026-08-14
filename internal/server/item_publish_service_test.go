package server

import (
	"context"
	"testing"

	"xianyu-go/internal/xianyu/mtop"
)

// TestItemPublishServicePersistPreview 验证预检结果由应用服务统一持久化并可再次读取。
func TestItemPublishServicePersistPreview(t *testing.T) {
	// srv、store 和 cleanup 构造并释放测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// service 是待验证的商品发布应用服务。
	service := srv.itemPublishApplication()
	// preview 和 err 保存预检持久化结果。
	preview, err := service.PersistPreview(context.Background(), itemPublishPreviewInput{
		UserID: 1, BatchID: "batch_service_test", DefaultCookieID: "acc1",
		Filename: "products.csv", UploadDir: "/tmp/publish-service-test",
		Location: mtop.PublishLocation{DivisionID: "3301"},
		Rows: []publishBatchParsedRow{{
			RowNo: 2, CookieID: "acc1", Title: "服务层商品", Price: "12.50", Quantity: 1,
			Images: []string{"img/a.png"}, Category: mtop.PublishCategory{CatID: "5001", CatName: "虚拟商品"},
		}},
	})
	if err != nil {
		t.Fatalf("PersistPreview error: %v", err)
	}
	if !preview.Success || preview.PreviewID != "batch_service_test" || preview.Valid != 1 || preview.Invalid != 0 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	// got 和 err 保存从数据库读取的批次视图。
	got, err := service.GetBatch(context.Background(), 1, preview.PreviewID)
	if err != nil {
		t.Fatalf("GetBatch error: %v", err)
	}
	if got.ID != preview.PreviewID || len(got.Rows) != 1 || got.Rows[0].Title != "服务层商品" {
		t.Fatalf("unexpected batch: %+v", got)
	}
}

// TestItemPublishServiceStartBatchNotFound 验证应用服务对越权或不存在批次返回统一领域错误。
func TestItemPublishServiceStartBatchNotFound(t *testing.T) {
	// srv、store 和 cleanup 构造并释放测试服务。
	srv, _, cleanup := newTestServer(t)
	defer cleanup()
	// err 保存不存在批次的业务错误。
	_, err := srv.itemPublishApplication().StartBatch(context.Background(), 1, "missing-batch")
	if err != errItemPublishBatchNotFound {
		t.Fatalf("error=%v want %v", err, errItemPublishBatchNotFound)
	}
}
