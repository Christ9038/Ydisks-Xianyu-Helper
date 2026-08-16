package adapter

import (
	"context"
	"errors"
	"testing"

	cardsapp "xianyu-go/internal/application/cards"
)

// TestCardsRepositoryCRUDMapping 验证 SQLite 卡券 CRUD 与应用模型之间的完整字段映射。
func TestCardsRepositoryCRUDMapping(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是待验证的卡券数据库适配器。
	repository := NewCardsRepository(store)
	// ctx 是本测试全部数据库操作使用的非取消上下文。
	ctx := context.Background()
	// owner 是测试模板中创建的卡券所有者。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// input 是覆盖全部持久化字段的应用卡券模型。
	input := cardsapp.Card{
		Name: "库存卡", Type: "data", APIConfig: "legacy", TextContent: "text", DataContent: "A\nB",
		ImageURL: "https://example.invalid/card.png", Description: "description", Enabled: true,
		DelaySeconds: 12, IsMultiSpec: true, SpecName: "颜色", SpecValue: "蓝", UserID: owner.ID,
	}
	// cardID、createErr 保存创建结果。
	cardID, createErr := repository.Create(ctx, input)
	if createErr != nil || cardID <= 0 {
		t.Fatalf("创建卡券失败 id=%d err=%v", cardID, createErr)
	}
	// got、getErr 保存详情查询结果。
	got, getErr := repository.Get(ctx, cardID)
	if getErr != nil {
		t.Fatal(getErr)
	}
	input.ID = cardID
	if got != input {
		t.Fatalf("详情字段映射不匹配 got=%+v want=%+v", got, input)
	}
	// listed、listErr 保存按用户隔离的列表结果。
	listed, listErr := repository.ListForUser(ctx, owner.ID)
	if listErr != nil || len(listed) != 1 || listed[0] != input {
		t.Fatalf("列表字段映射不匹配 listed=%+v err=%v", listed, listErr)
	}
	got.Name = "更新后"
	got.Enabled = false
	// updateErr 表示更新卡券组时的数据库错误。
	if updateErr := repository.Update(ctx, got); updateErr != nil {
		t.Fatal(updateErr)
	}
	// updated、updatedErr 保存更新后的详情。
	updated, updatedErr := repository.Get(ctx, cardID)
	if updatedErr != nil || updated.Name != "更新后" || updated.Enabled {
		t.Fatalf("更新结果异常 card=%+v err=%v", updated, updatedErr)
	}
	// deleteErr 表示删除卡券组时的数据库错误。
	if deleteErr := repository.Delete(ctx, cardID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
	// missingErr 表示删除后再次读取卡券组的应用层未找到错误。
	if _, missingErr := repository.Get(ctx, cardID); !errors.Is(missingErr, cardsapp.ErrNotFound) {
		t.Fatalf("删除后应映射为应用层未找到，err=%v", missingErr)
	}
}

// TestCardsRepositoryAppendDataMapping 验证卡券适配器把追加库存结果和内容写入数据库。
func TestCardsRepositoryAppendDataMapping(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是待验证的卡券数据库适配器。
	repository := NewCardsRepository(store)
	// ctx 是本测试数据库操作使用的非取消上下文。
	ctx := context.Background()
	// owner、ownerErr 保存测试模板中的卡券所有者及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// cardID、createErr 保存 data 卡券组标识及创建错误。
	cardID, createErr := repository.Create(ctx, cardsapp.Card{Name: "库存卡", Type: "data", DataContent: "A\n", UserID: owner.ID})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// added、appendErr 保存适配器返回的有效新增行数及错误。
	added, appendErr := repository.AppendData(ctx, cardID, "B\n\nC")
	if appendErr != nil || added != 2 {
		t.Fatalf("追加结果异常 added=%d err=%v", added, appendErr)
	}
	// got、getErr 保存追加后的卡券记录及读取错误。
	got, getErr := repository.Get(ctx, cardID)
	if getErr != nil || got.DataContent != "A\n\nB\n\nC" {
		t.Fatalf("追加内容未正确映射 card=%+v err=%v", got, getErr)
	}
}

// TestCardsRepositoryPropagatesInfrastructureErrors 验证数据库不可用时不会伪装成卡券缺失。
func TestCardsRepositoryPropagatesInfrastructureErrors(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定已关闭数据库的卡券适配器。
	repository := NewCardsRepository(store)
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// err 表示关闭数据库后读取卡券组的底层错误。
	if _, err := repository.Get(context.Background(), 1); err == nil || errors.Is(err, cardsapp.ErrNotFound) {
		t.Fatalf("数据库故障应原样返回，err=%v", err)
	}
	// err 表示缺少 Store 时的适配器装配错误。
	if _, err := NewCardsRepository(nil).ListForUser(context.Background(), 1); err == nil {
		t.Fatal("缺少 Store 时应返回装配错误")
	}
}
