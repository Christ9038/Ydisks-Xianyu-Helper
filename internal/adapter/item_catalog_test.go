package adapter

import (
	"context"
	"errors"
	"testing"

	itemapp "xianyu-go/internal/application/items"
	"xianyu-go/internal/db"
)

// TestItemCatalogRepositoryRejectsMissingStore 验证商品读取适配器缺少数据库时快速失败。
func TestItemCatalogRepositoryRejectsMissingStore(t *testing.T) {
	// repository 保存缺少数据库依赖的商品读取适配器。
	repository := NewItemCatalogRepository(nil)
	// err 保存用户范围读取的装配错误。
	if _, err := repository.ListForUser(context.Background(), 1, "account-1"); err == nil {
		t.Fatal("缺少数据库时 ListForUser 不应伪装成功")
	}
	// err 保存账号范围读取的装配错误。
	if _, err := repository.ListByCookie(context.Background(), "account-1"); err == nil {
		t.Fatal("缺少数据库时 ListByCookie 不应伪装成功")
	}
	// err 保存详情读取的装配错误。
	if _, err := repository.Get(context.Background(), "account-1", "item-1"); err == nil {
		t.Fatal("缺少数据库时 Get 不应伪装成功")
	}
	// writeErr 保存创建商品时的装配错误。
	if writeErr := repository.Upsert(context.Background(), "account-1", itemapp.CatalogWriteInput{ItemID: "item-1"}); writeErr == nil {
		t.Fatal("缺少数据库时 Upsert 不应伪装成功")
	}
	// deleteErr 保存删除商品时的装配错误。
	if deleteErr := repository.Delete(context.Background(), "account-1", "item-1"); deleteErr == nil {
		t.Fatal("缺少数据库时 Delete 不应伪装成功")
	}
	// specErr 保存多规格更新时的装配错误。
	if specErr := repository.SetMultiSpec(context.Background(), "account-1", "item-1", true); specErr == nil {
		t.Fatal("缺少数据库时 SetMultiSpec 不应伪装成功")
	}
	// quantityErr 保存多数量更新时的装配错误。
	if quantityErr := repository.SetMultiQuantity(context.Background(), "account-1", "item-1", true); quantityErr == nil {
		t.Fatal("缺少数据库时 SetMultiQuantity 不应伪装成功")
	}
}

// TestCatalogItemsFromRowsConvertsAllFields 验证数据库商品行完整转换为应用模型。
func TestCatalogItemsFromRowsConvertsAllFields(t *testing.T) {
	// rows 保存带有全部业务字段的数据库商品行。
	rows := []db.ItemInfoRow{{ID: 9, CookieID: "account-1", ItemID: "item-1", ItemTitle: "标题", ItemDescription: "描述", ItemCategory: "cat", ItemPrice: "1.00", ItemDetail: "{}", IsMultiSpec: true, MultiQuantityDelivery: true}}
	// items 保存适配器转换后的商品模型。
	items := catalogItemsFromRows(rows)
	if len(items) != 1 || items[0].ID != 9 || items[0].CookieID != "account-1" || !items[0].IsMultiSpec || !items[0].MultiQuantityDelivery {
		t.Fatalf("catalogItemsFromRows()=%+v", items)
	}
}

// TestItemCatalogRepositoryWritesAndDeletesItem 验证商品写适配器覆盖创建、开关更新和逻辑删除。
func TestItemCatalogRepositoryWritesAndDeletesItem(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试数据库的商品读写适配器。
	repository := NewItemCatalogRepository(store)
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// upsertErr 保存创建商品的写入错误。
	upsertErr := repository.Upsert(ctx, "cid", itemapp.CatalogWriteInput{ItemID: "item-write", ItemTitle: "商品", IsMultiSpec: true})
	if upsertErr != nil {
		t.Fatalf("Upsert error: %v", upsertErr)
	}
	// publishedErr 保存批量发布成功后商品目录收口的写入错误。
	publishedErr := repository.UpsertPublishedItem(ctx, itemapp.BatchPublishedItem{CookieID: "cid", ItemID: "item-published", ItemTitle: "已发布商品", ItemPrice: "12.00", MultiQuantityDelivery: true})
	if publishedErr != nil {
		t.Fatalf("UpsertPublishedItem error: %v", publishedErr)
	}
	// specErr 保存多规格开关更新错误。
	if specErr := repository.SetMultiSpec(ctx, "cid", "item-write", false); specErr != nil {
		t.Fatalf("SetMultiSpec error: %v", specErr)
	}
	// quantityErr 保存多数量开关更新错误。
	if quantityErr := repository.SetMultiQuantity(ctx, "cid", "item-write", true); quantityErr != nil {
		t.Fatalf("SetMultiQuantity error: %v", quantityErr)
	}
	// row、getErr 保存开关更新后的商品记录。
	row, getErr := store.Items.Get(ctx, "cid", "item-write")
	if getErr != nil || row.IsMultiSpec || !row.MultiQuantityDelivery {
		t.Fatalf("商品写入或开关更新异常：row=%+v err=%v", row, getErr)
	}
	// deleteErr 保存逻辑删除错误。
	if deleteErr := repository.Delete(ctx, "cid", "item-write"); deleteErr != nil {
		t.Fatalf("Delete error: %v", deleteErr)
	}
	// missingErr 保存删除后再次读取的错误。
	if _, missingErr := store.Items.Get(ctx, "cid", "item-write"); !errors.Is(missingErr, db.ErrNotFound) {
		t.Fatalf("删除后商品应不可见，err=%v", missingErr)
	}
	// publishedItem、publishedItemErr 保存批量发布商品的读取结果。
	publishedItem, publishedItemErr := repository.Get(ctx, "cid", "item-published")
	if publishedItemErr != nil || publishedItem.ItemTitle != "已发布商品" || !publishedItem.MultiQuantityDelivery {
		t.Fatalf("published item=%+v err=%v", publishedItem, publishedItemErr)
	}
}

// TestItemCatalogRepositoryListsAndMapsErrors 验证商品目录的用户范围、账号范围查询及错误转换。
func TestItemCatalogRepositoryListsAndMapsErrors(t *testing.T) {
	// store、cleanup 保存当前测试使用的 SQLite 存储及清理函数。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试共用的非取消上下文。
	ctx := context.Background()
	// owner、ownerErr 保存测试账号所属用户及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatalf("GetByUsername error: %v", ownerErr)
	}
	// first、second 是用于确认字段映射和排序的两条商品记录。
	first := &db.ItemInfoRow{CookieID: "cid", ItemID: "catalog-first", ItemTitle: "第一件", ItemDescription: "描述", ItemCategory: "分类", ItemPrice: "9.90", ItemDetail: "详情", IsMultiSpec: true, MultiQuantityDelivery: true}
	// second 是用于确认查询排序的第二条商品记录。
	second := &db.ItemInfoRow{CookieID: "cid", ItemID: "catalog-second", ItemTitle: "第二件"}
	// firstErr 保存第一条商品写入错误。
	firstErr := store.Items.Upsert(ctx, first)
	if firstErr != nil {
		t.Fatalf("Upsert first error: %v", firstErr)
	}
	// secondErr 保存第二条商品写入错误。
	secondErr := store.Items.Upsert(ctx, second)
	if secondErr != nil {
		t.Fatalf("Upsert second error: %v", secondErr)
	}
	// repository 是绑定测试数据库的商品目录适配器。
	repository := NewItemCatalogRepository(store)
	// listed、listErr 保存用户范围商品查询结果及错误。
	listed, listErr := repository.ListForUser(ctx, owner.ID, "")
	if listErr != nil || len(listed) != 2 || listed[0].ItemID != "catalog-second" || listed[1].ItemDetail != "详情" || !listed[1].IsMultiSpec {
		t.Fatalf("ListForUser()=%+v err=%v", listed, listErr)
	}
	// filtered、filterErr 保存按账号筛选后的商品查询结果及错误。
	filtered, filterErr := repository.ListForUser(ctx, owner.ID, "cid")
	if filterErr != nil || len(filtered) != 2 {
		t.Fatalf("ListForUser filtered=%+v err=%v", filtered, filterErr)
	}
	// byCookie、cookieErr 保存账号范围商品查询结果及错误。
	byCookie, cookieErr := repository.ListByCookie(ctx, "cid")
	if cookieErr != nil || len(byCookie) != 2 {
		t.Fatalf("ListByCookie()=%+v err=%v", byCookie, cookieErr)
	}
	// missingErr 保存不存在商品被适配器转换后的领域错误。
	_, missingErr := repository.Get(ctx, "cid", "does-not-exist")
	if !errors.Is(missingErr, itemapp.ErrCatalogNotFound) {
		t.Fatalf("Get missing error=%v", missingErr)
	}
	// canceled 是已经取消的上下文，用于覆盖数据库查询错误传播路径。
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	// canceledChecks 保存取消上下文下各查询返回的错误，避免错误分支被静态简化。
	canceledChecks := []error{
		func() error {
			// err 保存取消上下文下用户范围查询的数据库错误。
			_, err := repository.ListForUser(canceled, owner.ID, "")
			return err
		}(),
		func() error {
			// err 保存取消上下文下账号范围查询的数据库错误。
			_, err := repository.ListByCookie(canceled, "cid")
			return err
		}(),
		func() error {
			// err 保存取消上下文下单商品查询的数据库错误。
			_, err := repository.Get(canceled, "cid", "catalog-first")
			return err
		}(),
	}
	// canceledErr 表示当前遍历到的一项取消查询错误。
	for _, canceledErr := range canceledChecks {
		if canceledErr == nil {
			t.Fatal("取消上下文下商品查询不应成功")
		}
	}
}
