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

// TestCatalogMutationServiceUpdateAppliesEveryPatchField 验证商品局部更新的全部可选字段都会覆盖旧值。
func TestCatalogMutationServiceUpdateAppliesEveryPatchField(t *testing.T) {
	// repository 保存待合并的商品旧值及最终写入模型。
	repository := &catalogMutationRepositoryStub{item: CatalogItem{ItemID: "item-1", ItemTitle: "旧", ItemDescription: "旧", ItemCategory: "旧", ItemPrice: "1", ItemDetail: "旧", IsMultiSpec: false, MultiQuantityDelivery: false}}
	// service 是绑定测试仓储的商品写应用服务。
	service, serviceErr := NewCatalogMutationService(repository)
	if serviceErr != nil {
		t.Fatalf("NewCatalogMutationService error: %v", serviceErr)
	}
	// title、description、category、price、detail 保存待替换的文本字段。
	title, description, category, price, detail := "新标题", "新描述", "新类目", "2.00", "新详情"
	// multiSpec、multiQuantity 保存待替换的交付开关。
	multiSpec, multiQuantity := true, true
	// updateErr 保存应用全部局部字段后的写入错误。
	updateErr := service.Update(context.Background(), "account-1", "item-1", CatalogPatchInput{ItemTitle: &title, ItemDescription: &description, ItemCategory: &category, ItemPrice: &price, ItemDetail: &detail, IsMultiSpec: &multiSpec, MultiQuantityDelivery: &multiQuantity})
	if updateErr != nil {
		t.Fatalf("Update error: %v", updateErr)
	}
	// got 保存应用服务合并后的完整商品模型。
	got := repository.upsertInput
	if got.ItemID != "item-1" || got.ItemTitle != title || got.ItemDescription != description || got.ItemCategory != category || got.ItemPrice != price || got.ItemDetail != detail || !got.IsMultiSpec || !got.MultiQuantityDelivery {
		t.Fatalf("全部字段未合并：%+v", got)
	}
	// writeErr 保存更新阶段完整写入端口返回的错误。
	writeErr := errors.New("更新写入失败")
	// failingRepository 是更新完整商品时返回错误的测试仓储。
	failingRepository := &catalogMutationRepositoryStub{item: repository.item, upsertErr: writeErr}
	// failingService 是绑定更新写入失败仓储的应用服务。
	failingService, failingServiceErr := NewCatalogMutationService(failingRepository)
	if failingServiceErr != nil {
		t.Fatalf("New failing service error: %v", failingServiceErr)
	}
	// failedUpdateErr 保存更新写入错误的透传结果。
	failedUpdateErr := failingService.Update(context.Background(), "account-1", "item-1", CatalogPatchInput{})
	if !errors.Is(failedUpdateErr, writeErr) {
		t.Fatalf("Update write error=%v want %v", failedUpdateErr, writeErr)
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

// TestCatalogMutationServiceDelegatesDeleteAndSwitches 验证商品删除及两个交付开关都透传到仓储端口。
func TestCatalogMutationServiceDelegatesDeleteAndSwitches(t *testing.T) {
	// repository 是记录商品写操作的测试仓储。
	repository := &catalogMutationRepositoryStub{}
	// service、err 保存商品写应用服务及构造错误。
	service, err := NewCatalogMutationService(repository)
	if err != nil {
		t.Fatalf("NewCatalogMutationService error=%v", err)
	}
	// deleteErr 保存商品删除端口错误。
	if deleteErr := service.Delete(context.Background(), "account-1", "item-1"); deleteErr != nil {
		t.Fatalf("Delete error=%v", deleteErr)
	}
	// multiSpecErr 保存多规格开关端口错误。
	if multiSpecErr := service.SetMultiSpec(context.Background(), "account-1", "item-1", true); multiSpecErr != nil {
		t.Fatalf("SetMultiSpec error=%v", multiSpecErr)
	}
	// multiQuantityErr 保存多数量开关端口错误。
	if multiQuantityErr := service.SetMultiQuantity(context.Background(), "account-1", "item-1", false); multiQuantityErr != nil {
		t.Fatalf("SetMultiQuantity error=%v", multiQuantityErr)
	}
	// wantErr 是所有商品写操作需要透传的底层错误。
	wantErr := errors.New("商品写入失败")
	// failingRepository 是各写端口都返回错误的测试仓储。
	failingRepository := &catalogMutationRepositoryStub{deleteErr: wantErr, multiSpecErr: wantErr, multiQuantityErr: wantErr}
	// failingService 是绑定错误仓储的商品写应用服务。
	failingService, err := NewCatalogMutationService(failingRepository)
	if err != nil {
		t.Fatalf("New failing service error=%v", err)
	}
	// failedDeleteErr 保存错误仓储删除端口的错误。
	if failedDeleteErr := failingService.Delete(context.Background(), "account-1", "item-1"); !errors.Is(failedDeleteErr, wantErr) {
		t.Fatalf("Delete error=%v", failedDeleteErr)
	}
	// failedMultiSpecErr 保存错误仓储多规格端口的错误。
	if failedMultiSpecErr := failingService.SetMultiSpec(context.Background(), "account-1", "item-1", true); !errors.Is(failedMultiSpecErr, wantErr) {
		t.Fatalf("SetMultiSpec error=%v", failedMultiSpecErr)
	}
	// failedMultiQuantityErr 保存错误仓储多数量端口的错误。
	if failedMultiQuantityErr := failingService.SetMultiQuantity(context.Background(), "account-1", "item-1", true); !errors.Is(failedMultiQuantityErr, wantErr) {
		t.Fatalf("SetMultiQuantity error=%v", failedMultiQuantityErr)
	}
	// nilService 表示未初始化的商品写服务指针。
	var nilService *CatalogMutationService
	if nilService.Create(context.Background(), "account-1", CatalogWriteInput{}) == nil || nilService.Delete(context.Background(), "account-1", "item-1") == nil || nilService.SetMultiSpec(context.Background(), "account-1", "item-1", true) == nil || nilService.SetMultiQuantity(context.Background(), "account-1", "item-1", true) == nil {
		t.Fatal("nil mutation service should reject all operations")
	}
}
