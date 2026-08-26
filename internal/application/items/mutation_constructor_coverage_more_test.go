package items

import (
	"context"
	"testing"
)

// TestCatalogAndCategoryConstructorsRejectMissingPorts 覆盖商品写入与类目推荐构造器的缺失端口分支。
func TestCatalogAndCategoryConstructorsRejectMissingPorts(t *testing.T) {
	// catalogService、catalogErr 保存商品写入服务构造结果。
	catalogService, catalogErr := NewCatalogMutationService(nil)
	if catalogService != nil || catalogErr == nil {
		t.Fatalf("空商品写入端口未被拒绝: service=%v err=%v", catalogService, catalogErr)
	}
	// categoryService、categoryErr 保存类目推荐服务构造结果。
	categoryService, categoryErr := NewCategoryRecommendationService(nil)
	if categoryService != nil || categoryErr == nil {
		t.Fatalf("空类目推荐端口未被拒绝: service=%v err=%v", categoryService, categoryErr)
	}
	// nilCatalog 保存 nil 商品写入服务，覆盖公开方法的生命周期保护。
	var nilCatalog *CatalogMutationService
	// err 保存 nil 商品写入服务的 Create 错误。
	if err := nilCatalog.Create(context.Background(), "account", CatalogWriteInput{}); err == nil {
		t.Fatal("nil 商品写入服务未拒绝 Create")
	}
	// updateErr 保存 nil 商品写入服务的 Update 错误。
	if updateErr := nilCatalog.Update(context.Background(), "account", "item", CatalogPatchInput{}); updateErr == nil {
		t.Fatal("nil 商品写入服务未拒绝 Update")
	}
	// nilCategory 保存 nil 类目推荐服务，覆盖公开方法的生命周期保护。
	var nilCategory *CategoryRecommendationService
	// err 保存 nil 类目推荐服务的 Recommend 错误。
	if _, err := nilCategory.Recommend(context.Background(), 7, "account", "keyword"); err == nil {
		t.Fatal("nil 类目推荐服务未拒绝 Recommend")
	}
}
