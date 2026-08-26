package items

import (
	"context"
	"errors"
	"testing"
	"time"
)

// TestBatchManagementCoversConstructorAndGuardBranches 覆盖批次管理构造器、查询和操作入口的参数保护。
func TestBatchManagementCoversConstructorAndGuardBranches(t *testing.T) {
	// service、serviceErr 保存缺少仓储端口的构造结果。
	service, serviceErr := NewBatchManagementService(nil, nil)
	if service != nil || serviceErr == nil {
		t.Fatalf("缺少批次管理仓储未被拒绝: service=%v err=%v", service, serviceErr)
	}
	// nilService 保存 nil 接收者，覆盖所有管理入口的生命周期保护。
	var nilService *BatchManagementService
	// err 保存 nil 服务列表查询错误。
	if _, err := nilService.ListBatches(context.Background(), 1, 10); !errors.Is(err, ErrBatchNotFound) {
		t.Fatalf("nil ListBatches 错误异常: %v", err)
	}
	// err 保存 nil 服务批次查询错误。
	if _, err := nilService.GetBatch(context.Background(), 1, "batch"); !errors.Is(err, ErrBatchNotFound) {
		t.Fatalf("nil GetBatch 错误异常: %v", err)
	}
	// err 保存 nil 服务取消错误。
	if _, err := nilService.CancelBatch(context.Background(), 1, "batch"); !errors.Is(err, ErrBatchNotFound) {
		t.Fatalf("nil CancelBatch 错误异常: %v", err)
	}
	// err 保存 nil 服务删除错误。
	if err := nilService.DeleteBatch(context.Background(), 1, "batch"); !errors.Is(err, ErrBatchNotFound) {
		t.Fatalf("nil DeleteBatch 错误异常: %v", err)
	}
	// err 保存 nil 服务重试错误。
	if _, err := nilService.RetryFailedBatch(context.Background(), 1, "batch", time.Second); !errors.Is(err, ErrBatchNotFound) {
		t.Fatalf("nil RetryFailedBatch 错误异常: %v", err)
	}
	// err 保存 nil 服务清理错误。
	if err := nilService.CleanupExpiredUploads(context.Background(), time.Time{}, 10); err == nil {
		t.Fatal("nil CleanupExpiredUploads 未返回错误")
	}

	// queryErr 保存批次列表查询错误。
	queryErr := errors.New("批次列表失败")
	// repository 保存返回查询错误的批次仓储。
	repository := &batchManagementRepositoryFake{err: queryErr, batch: BatchInfo{ID: "batch", Status: "completed"}}
	// queryService 保存批次管理应用服务。
	queryService := newBatchManagementServiceForTest(t, repository, &batchManagementRuntimeFake{})
	// err 保存列表查询错误。
	if _, err := queryService.ListBatches(context.Background(), 1, 10); !errors.Is(err, queryErr) {
		t.Fatalf("ListBatches 查询错误异常: %v", err)
	}
	// err 保存批次归属查询错误。
	if _, err := queryService.GetBatch(context.Background(), 1, "batch"); !errors.Is(err, ErrBatchNotFound) {
		t.Fatalf("GetBatch 归属错误异常: %v", err)
	}
	// err 保存删除持久化错误。
	deleteErr := errors.New("删除失败")
	// deleteRepository 保存删除错误测试仓储。
	deleteRepository := &batchManagementRepositoryFake{batch: BatchInfo{ID: "batch", Status: "completed"}, deleteErr: deleteErr}
	// deleteService 保存删除错误测试服务。
	deleteService := newBatchManagementServiceForTest(t, deleteRepository, &batchManagementRuntimeFake{})
	// err 保存删除操作错误。
	if err := deleteService.DeleteBatch(context.Background(), 1, "batch"); !errors.Is(err, deleteErr) {
		t.Fatalf("DeleteBatch 删除错误异常: %v", err)
	}
}
