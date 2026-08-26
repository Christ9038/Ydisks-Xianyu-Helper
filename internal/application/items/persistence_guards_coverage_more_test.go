package items

import (
	"context"
	"errors"
	"testing"
)

// TestBatchPreviewPersistenceCoversConstructorAndInputGuards 覆盖预检持久化服务的构造和输入保护。
func TestBatchPreviewPersistenceCoversConstructorAndInputGuards(t *testing.T) {
	// nilService、constructErr 保存 nil repository 构造结果。
	nilService, constructErr := NewBatchPreviewPersistenceService(nil)
	if nilService != nil || constructErr == nil {
		t.Fatalf("nil repository service=%v err=%v", nilService, constructErr)
	}
	// service 保存正常构造的预检持久化服务。
	service, serviceErr := NewBatchPreviewPersistenceService(&batchPreviewPersistenceRepositoryStub{})
	if serviceErr != nil {
		t.Fatal(serviceErr)
	}
	// emptyRowsErr 保存空预检行输入错误。
	if _, emptyRowsErr := service.Persist(context.Background(), BatchPreviewPersistenceBatch{ID: "empty"}, nil); !errors.Is(emptyRowsErr, ErrBatchPreviewNoRows) {
		t.Fatalf("empty rows error=%v", emptyRowsErr)
	}
	// nilServiceErr 保存 nil 服务接收者错误。
	var nilPersistenceService *BatchPreviewPersistenceService
	// nilServiceErr 保存 nil 持久化服务错误。
	if _, nilServiceErr := nilPersistenceService.Persist(context.Background(), BatchPreviewPersistenceBatch{}, []BatchPreviewRow{{RowNo: 1}}); nilServiceErr == nil {
		t.Fatal("nil persistence service should fail")
	}
}
