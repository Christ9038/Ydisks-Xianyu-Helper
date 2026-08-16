package items

import (
	"context"
	"errors"
	"testing"
)

// catalogMutationRepositoryStub 是商品写应用服务测试使用的可控仓储替身。
type catalogMutationRepositoryStub struct {
	// item 保存当前账号下的商品记录。
	item CatalogItem
	// getErr 保存读取现有商品时的预设错误。
	getErr error
	// upsertErr 保存完整商品写入时的预设错误。
	upsertErr error
	// upsertInput 保存最后一次收到的完整写入模型。
	upsertInput CatalogWriteInput
	// deleteErr 保存逻辑删除时的预设错误。
	deleteErr error
	// multiSpecErr 保存多规格开关更新时的预设错误。
	multiSpecErr error
	// multiQuantityErr 保存多数量开关更新时的预设错误。
	multiQuantityErr error
}

// Get 返回测试预设的商品记录或读取错误。
func (repository *catalogMutationRepositoryStub) Get(context.Context, string, string) (CatalogItem, error) {
	return repository.item, repository.getErr
}

// Upsert 保存测试收到的完整商品写入模型。
func (repository *catalogMutationRepositoryStub) Upsert(_ context.Context, _ string, input CatalogWriteInput) error {
	repository.upsertInput = input
	return repository.upsertErr
}

// Delete 返回测试预设的逻辑删除错误。
func (repository *catalogMutationRepositoryStub) Delete(context.Context, string, string) error {
	return repository.deleteErr
}

// SetMultiSpec 返回测试预设的多规格更新错误。
func (repository *catalogMutationRepositoryStub) SetMultiSpec(context.Context, string, string, bool) error {
	return repository.multiSpecErr
}

// SetMultiQuantity 返回测试预设的多数量更新错误。
func (repository *catalogMutationRepositoryStub) SetMultiQuantity(context.Context, string, string, bool) error {
	return repository.multiQuantityErr
}

// TestCatalogMutationServiceUpdateMergesExplicitAndOmittedFields 验证局部更新保留未提交字段并允许显式清空或关闭。
func TestCatalogMutationServiceUpdateMergesExplicitAndOmittedFields(t *testing.T) {
	// repository 保存待验证的现有商品及写入记录。
	repository := &catalogMutationRepositoryStub{item: CatalogItem{
		ItemID: "item-1", ItemTitle: "旧标题", ItemDescription: "旧描述", ItemCategory: "旧类目",
		ItemPrice: "1.00", ItemDetail: "旧详情", IsMultiSpec: true, MultiQuantityDelivery: true,
	}}
	// service 保存绑定测试仓储的商品写应用服务。
	service, err := NewCatalogMutationService(repository)
	if err != nil {
		t.Fatalf("NewCatalogMutationService error: %v", err)
	}
	// emptyDescription、disabledMultiSpec 保存显式清空和关闭开关的更新值。
	emptyDescription := ""
	// disabledMultiSpec 保存显式关闭多规格交付的更新值。
	disabledMultiSpec := false
	// updateErr 保存商品局部更新结果。
	updateErr := service.Update(context.Background(), "account-1", "item-1", CatalogPatchInput{ItemDescription: &emptyDescription, IsMultiSpec: &disabledMultiSpec})
	if updateErr != nil {
		t.Fatalf("Update error: %v", updateErr)
	}
	// got 保存应用服务合并后的完整写入模型。
	got := repository.upsertInput
	if got.ItemID != "item-1" || got.ItemTitle != "旧标题" || got.ItemDescription != "" || got.ItemCategory != "旧类目" || got.ItemPrice != "1.00" || got.ItemDetail != "旧详情" || got.IsMultiSpec || !got.MultiQuantityDelivery {
		t.Fatalf("Update merge result=%+v", got)
	}
}

// TestCatalogMutationServicePropagatesReadAndWriteErrors 验证商品写入应用服务不吞掉仓储错误。
func TestCatalogMutationServicePropagatesReadAndWriteErrors(t *testing.T) {
	// readErr 是更新读取阶段的底层错误。
	readErr := errors.New("读取失败")
	// readRepository 保存读取失败的测试仓储。
	readRepository := &catalogMutationRepositoryStub{getErr: readErr}
	// readService 保存绑定读取失败仓储的应用服务。
	readService, err := NewCatalogMutationService(readRepository)
	if err != nil {
		t.Fatalf("NewCatalogMutationService read error: %v", err)
	}
	// updateErr 保存商品更新读取阶段的错误。
	if updateErr := readService.Update(context.Background(), "account-1", "item-1", CatalogPatchInput{}); !errors.Is(updateErr, readErr) {
		t.Fatalf("Update read error=%v want %v", updateErr, readErr)
	}
	// writeErr 是完整商品写入阶段的底层错误。
	writeErr := errors.New("写入失败")
	// writeRepository 保存写入失败的测试仓储。
	writeRepository := &catalogMutationRepositoryStub{upsertErr: writeErr}
	// writeService 保存绑定写入失败仓储的应用服务。
	writeService, err := NewCatalogMutationService(writeRepository)
	if err != nil {
		t.Fatalf("NewCatalogMutationService write error: %v", err)
	}
	// createErr 保存商品创建阶段的写入错误。
	if createErr := writeService.Create(context.Background(), "account-1", CatalogWriteInput{ItemID: "item-1"}); !errors.Is(createErr, writeErr) {
		t.Fatalf("Create write error=%v want %v", createErr, writeErr)
	}
}
