package items

import (
	"context"
	"errors"
	"testing"
)

// categoryRecommendationPortStub 记录应用层类目推荐调用。
type categoryRecommendationPortStub struct {
	// category 保存端口返回的测试类目。
	category BatchPreviewCategory
	// err 保存端口返回的测试错误。
	err error
	// calls 保存端口被调用次数。
	calls int
}

// RecommendCategory 返回预设类目并记录调用次数。
func (stub *categoryRecommendationPortStub) RecommendCategory(context.Context, int64, string, string) (BatchPreviewCategory, error) {
	stub.calls++
	return stub.category, stub.err
}

// TestCategoryRecommendationServiceDelegatesValidatedInput 验证应用服务转发合法输入。
func TestCategoryRecommendationServiceDelegatesValidatedInput(t *testing.T) {
	// port 保存可控的类目推荐端口。
	port := &categoryRecommendationPortStub{category: BatchPreviewCategory{CatID: "1", CatName: "资料"}}
	// service 保存待测试的应用服务。
	service, err := NewCategoryRecommendationService(port)
	if err != nil {
		t.Fatalf("构造服务失败: %v", err)
	}
	// category 保存应用服务返回的类目结果。
	category, err := service.Recommend(context.Background(), 1, " acc ", " 资料 ")
	if err != nil || category.CatID != "1" || port.calls != 1 {
		t.Fatalf("类目推荐结果异常: category=%+v err=%v calls=%d", category, err, port.calls)
	}
}

// TestCategoryRecommendationServiceRejectsInvalidInput 验证空输入不会调用基础设施端口。
func TestCategoryRecommendationServiceRejectsInvalidInput(t *testing.T) {
	// port 保存不应被调用的测试端口。
	port := &categoryRecommendationPortStub{}
	// service 保存待测试的应用服务。
	service, err := NewCategoryRecommendationService(port)
	if err != nil {
		t.Fatalf("构造服务失败: %v", err)
	}
	// invalidErr 保存空关键词导致的校验错误。
	_, invalidErr := service.Recommend(context.Background(), 1, "acc", " ")
	if invalidErr == nil || port.calls != 0 {
		t.Fatalf("空关键词校验异常: err=%v calls=%d", invalidErr, port.calls)
	}
}

// TestBatchPreviewPersistenceServiceCountsAndPropagatesErrors 验证预检计数及落库错误传播。
func TestBatchPreviewPersistenceServiceCountsAndPropagatesErrors(t *testing.T) {
	// repository 保存测试用的预检持久化端口。
	repository := &batchPreviewPersistenceRepositoryStub{}
	// service 保存待测试的预检持久化服务。
	service, err := NewBatchPreviewPersistenceService(repository)
	if err != nil {
		t.Fatalf("构造服务失败: %v", err)
	}
	// rows 保存一条有效行和一条校验失败行。
	rows := []BatchPreviewRow{{RowNo: 2}, {RowNo: 3, Errors: []string{"标题为空"}}}
	// result 保存预检持久化结果。
	result, err := service.Persist(context.Background(), BatchPreviewPersistenceBatch{ID: "batch-1"}, rows)
	if err != nil || result.Valid != 1 || result.Invalid != 1 || repository.created.ID != "batch-1" {
		t.Fatalf("预检结果异常: result=%+v err=%v batch=%+v", result, err, repository.created)
	}
	// expectedErr 保存模拟的数据库错误。
	expectedErr := errors.New("db down")
	repository.err = expectedErr
	_, err = service.Persist(context.Background(), BatchPreviewPersistenceBatch{ID: "batch-2"}, rows)
	if !errors.Is(err, expectedErr) {
		t.Fatalf("数据库错误未传播: %v", err)
	}
}

// batchPreviewPersistenceRepositoryStub 是预检持久化服务的内存端口替身。
type batchPreviewPersistenceRepositoryStub struct {
	// created 保存最近一次创建的批次元数据。
	created BatchPreviewPersistenceBatch
	// err 保存模拟的创建错误。
	err error
}

// CreateBatch 记录批次并返回预设错误。
func (stub *batchPreviewPersistenceRepositoryStub) CreateBatch(_ context.Context, batch BatchPreviewPersistenceBatch, _ []BatchPreviewRow) error {
	stub.created = batch
	return stub.err
}

// RecountBatch 返回预检统计重算成功。
func (*batchPreviewPersistenceRepositoryStub) RecountBatch(context.Context, string) error {
	return nil
}
