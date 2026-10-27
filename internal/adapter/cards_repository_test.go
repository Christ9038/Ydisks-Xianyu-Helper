package adapter

import (
	"context"
	"errors"
	"testing"

	cardsapp "xianyu-go/internal/application/cards"
	"xianyu-go/internal/db"
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
	// full、fullErr 保存发货路径读取的完整卡券模板及错误。
	full, fullErr := repository.GetFull(ctx, cardID)
	if fullErr != nil || full != input {
		t.Fatalf("完整卡券字段映射不匹配 full=%+v err=%v", full, fullErr)
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
	// fullMissingErr 表示完整发货模板在删除后也应映射为应用层未找到。
	if _, fullMissingErr := repository.GetFull(ctx, cardID); !errors.Is(fullMissingErr, cardsapp.ErrNotFound) {
		t.Fatalf("删除后完整卡券应映射为应用层未找到，err=%v", fullMissingErr)
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

// TestCardsRepositorySummaryPreservesContentType 验证脱敏 API 摘要仍保留公开请求编码类型。
func TestCardsRepositorySummaryPreservesContentType(t *testing.T) {
	// store、cleanup 保存临时 SQLite 数据库和关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是待测卡券适配器。
	repository := NewCardsRepository(store)
	// ctx 是本地 SQLite 读写使用的无截止上下文。
	ctx := context.Background()
	// owner、ownerErr 保存创建 API 卡券需要的本地用户身份及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// cardID、createErr 保存带表单编码配置的 API 卡券创建结果。
	cardID, createErr := repository.Create(ctx, cardsapp.Card{
		Name: "表单 API", Type: "api", UserID: owner.ID,
		APIConfig: `{"url":"https://example.invalid/card","method":"POST","content_type":"application/x-www-form-urlencoded"}`,
	})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// card、getErr 保存脱敏读取结果及错误。
	card, getErr := repository.Get(ctx, cardID)
	if getErr != nil || card.APIConfigSummary == nil || card.APIConfigSummary.ContentType != "application/x-www-form-urlencoded" {
		t.Fatalf("API 摘要丢失 Content-Type card=%+v err=%v", card, getErr)
	}
}

// TestCardsRepositoryDataMetadataPreservesConsumedStock 验证元数据更新不会恢复已经并发消费的库存正文。
func TestCardsRepositoryDataMetadataPreservesConsumedStock(t *testing.T) {
	// store、cleanup 管理隔离数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本地 SQLite 读写使用的上下文。
	ctx := context.Background()
	// owner、ownerErr 保存测试卡券所属用户。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// repository 是待测卡券数据库适配器。
	repository := NewCardsRepository(store)
	// cardID、createErr 保存带两条库存的 data 卡券。
	cardID, createErr := repository.Create(ctx, cardsapp.Card{Name: "库存", Type: "data", DataContent: "A\nB", Enabled: true, UserID: owner.ID})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// consumed、consumeErr 模拟自动发货先消费第一条库存。
	consumed, consumeErr := store.Cards.ConsumeBatchData(ctx, cardID)
	if consumeErr != nil || consumed != "A" {
		t.Fatalf("消费库存失败 consumed=%q err=%v", consumed, consumeErr)
	}
	// updateErr 执行只更新元数据的 SQL，不能覆盖当前剩余库存。
	updateErr := repository.UpdateDataMetadata(ctx, cardsapp.Card{ID: cardID, Name: "库存改名", Type: "data", Enabled: false, UserID: owner.ID})
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	// updated、updatedErr 验证消费后的库存仍只有第二条。
	updated, updatedErr := repository.GetFull(ctx, cardID)
	if updatedErr != nil || updated.DataContent != "B" || updated.Name != "库存改名" || updated.Enabled {
		t.Fatalf("元数据更新覆盖消费库存 updated=%+v err=%v", updated, updatedErr)
	}
}

// TestCardsRepositoryUpdateIfDataUnchangedRejectsStaleStock 验证库存快照守卫：编辑期间被自动发货消费后，旧快照不得把已发出的卡密写回库存。
func TestCardsRepositoryUpdateIfDataUnchangedRejectsStaleStock(t *testing.T) {
	// store、cleanup 管理隔离数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本地 SQLite 读写使用的上下文。
	ctx := context.Background()
	// owner、ownerErr 保存测试卡券所属用户。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// repository 是待测卡券数据库适配器。
	repository := NewCardsRepository(store)
	// cardID、createErr 保存编辑者打开弹窗时库存为 A、B 的 data 卡券。
	cardID, createErr := repository.Create(ctx, cardsapp.Card{Name: "库存", Type: "data", DataContent: "A\nB", Enabled: true, UserID: owner.ID})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// consumed、consumeErr 模拟编辑期间自动发货先发出 A。
	consumed, consumeErr := store.Cards.ConsumeBatchData(ctx, cardID)
	if consumeErr != nil || consumed != "A" {
		t.Fatalf("消费库存失败 consumed=%q err=%v", consumed, consumeErr)
	}
	// edited 是编辑者基于旧快照追加 C 后的完整草稿。
	edited := cardsapp.Card{ID: cardID, Name: "库存", Type: "data", DataContent: "A\nB\nC", Enabled: true, UserID: owner.ID}
	// staleUpdated、staleErr 保存旧快照守卫更新结果，必须拒绝写入。
	staleUpdated, staleErr := repository.UpdateIfDataUnchanged(ctx, edited, "A\nB")
	if staleErr != nil || staleUpdated {
		t.Fatalf("旧库存快照不应覆盖 updated=%v err=%v", staleUpdated, staleErr)
	}
	// current、currentErr 验证被拒绝后库存仍是自动发货后的剩余行。
	current, currentErr := repository.GetFull(ctx, cardID)
	if currentErr != nil || current.DataContent != "B" {
		t.Fatalf("拒绝写入后库存被改动 current=%+v err=%v", current, currentErr)
	}
	// freshUpdated、freshErr 保存基于最新快照重新编辑后的写入结果，应成功。
	freshUpdated, freshErr := repository.UpdateIfDataUnchanged(ctx, cardsapp.Card{ID: cardID, Name: "库存", Type: "data", DataContent: "B\nC", Enabled: true, UserID: owner.ID}, "B")
	if freshErr != nil || !freshUpdated {
		t.Fatalf("最新快照应允许写入 updated=%v err=%v", freshUpdated, freshErr)
	}
	// saved、savedErr 验证最新快照写入后的库存正文。
	saved, savedErr := repository.GetFull(ctx, cardID)
	if savedErr != nil || saved.DataContent != "B\nC" {
		t.Fatalf("库存未按最新快照写入 saved=%+v err=%v", saved, savedErr)
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

// TestCardsRepositoryCoversClosedDatabaseOperations 验证卡券适配器各写入端点统一传播数据库故障。
func TestCardsRepositoryCoversClosedDatabaseOperations(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定已关闭数据库的卡券适配器。
	repository := NewCardsRepository(store)
	// closeErr 表示主动关闭测试数据库连接时的资源释放错误。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// ctx 是本测试全部数据库操作使用的非取消上下文。
	ctx := context.Background()
	// operations 保存需要统一验证底层错误传播的卡券操作结果。
	operations := []struct {
		name string
		err  error
	}{
		{name: "创建", err: func() error {
			// err 表示创建操作在关闭数据库后的底层错误。
			_, err := repository.Create(ctx, cardsapp.Card{})
			return err
		}()},
		{name: "更新", err: repository.Update(ctx, cardsapp.Card{})},
		{name: "删除", err: repository.Delete(ctx, 1)},
		{name: "追加库存", err: func() error {
			// err 表示追加库存操作在关闭数据库后的底层错误。
			_, err := repository.AppendData(ctx, 1, "line")
			return err
		}()},
		{name: "按用户列表", err: func() error {
			// err 表示列表操作在关闭数据库后的底层错误。
			_, err := repository.ListForUser(ctx, 1)
			return err
		}()},
		{name: "完整读取", err: func() error {
			// err 表示完整读取在关闭数据库后的底层错误。
			_, err := repository.GetFull(ctx, 1)
			return err
		}()},
	}
	// operation 表示当前待验证的卡券操作及其底层结果。
	for _, operation := range operations {
		if operation.err == nil {
			t.Errorf("%s 未传播数据库故障", operation.name)
		}
	}
}

// TestCardApplicationModelCopiesAPIConfigSummary 验证卡券摘要转换不会共享数据库摘要指针。
func TestCardApplicationModelCopiesAPIConfigSummary(t *testing.T) {
	// summary 是待转换的数据库 API 配置摘要。
	summary := &db.CardAPIConfigSummary{}
	// record 是携带摘要指针的数据库卡券记录。
	record := db.CardFull{ID: 7, Name: "带摘要", APIConfigSummary: summary}
	// converted 是适配器转换出的应用模型。
	converted := cardApplicationModel(record)
	if converted.APIConfigSummary == nil {
		t.Fatal("数据库摘要存在时应用模型不应丢失摘要")
	}
	summary.URL = "changed-after-conversion"
	if converted.APIConfigSummary.URL == summary.URL {
		t.Fatal("应用模型不应共享数据库摘要字段")
	}
}

// TestCardsRepositorySaleRecordMapping 验证售出台账查询把数据库字段完整映射为应用模型，并保持分页和用户隔离。
func TestCardsRepositorySaleRecordMapping(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是被测服务依赖的内存仓储替身。
	repository := NewCardsRepository(store)
	// ctx 是测试使用的根上下文。
	ctx := context.Background()
	// owner 是卡密组所有者；ownerErr 保存读取卡密组所有者的错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// cardID 是被测卡密组标识；createErr 保存创建测试数据的错误。
	cardID, createErr := repository.Create(ctx, cardsapp.Card{Name: "台账卡", Type: "data", DataContent: "A\nB", Enabled: true, UserID: owner.ID})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// meta 是消费时的订单上下文。
	meta := db.CardSaleMeta{RunID: 3, CookieID: "acc1", OrderID: "o1", ItemID: "i1", BuyerID: "b1"}
	// recordID 是 reserved 台账记录标识；reserveErr 保存扣减库存并记账的错误。
	_, recordID, reserveErr := store.Cards.ReserveBatchData(ctx, cardID, meta)
	if reserveErr != nil {
		t.Fatal(reserveErr)
	}
	// markErr 保存确认售出的错误。
	if markErr := store.Cards.MarkSaleRecordsSold(ctx, recordID); markErr != nil {
		t.Fatal(markErr)
	}

	// countErr 保存统计已售数量的错误；counts 保存按卡密组统计的已售数量。
	counts, countErr := repository.SaleCounts(ctx, owner.ID)
	if countErr != nil || counts[cardID] != 1 {
		t.Fatalf("counts=%v err=%v", counts, countErr)
	}
	// listErr 保存分页读取台账的错误；page 保存被测分页查询的返回结果。
	page, listErr := repository.ListSaleRecords(ctx, owner.ID, cardsapp.SaleListQuery{CardID: cardID, Status: cardsapp.SaleStatusSold, Limit: 10})
	if listErr != nil || page.Total != 1 || len(page.Records) != 1 {
		t.Fatalf("page=%+v err=%v", page, listErr)
	}
	// record 是映射后的应用模型，字段必须与消费上下文一致。
	record := page.Records[0]
	if record.CardID != cardID || record.CardName != "台账卡" || record.CardType != "data" || record.Content != "A" || record.Status != cardsapp.SaleStatusSold ||
		record.CookieID != "acc1" || record.OrderID != "o1" || record.ItemID != "i1" || record.BuyerID != "b1" || record.CreatedAt == 0 || record.SoldAt == 0 {
		t.Fatalf("字段映射错误: %+v", record)
	}
	// all 保存卡密组的全部台账记录；allErr 保存读取全部台账的错误。
	all, allErr := repository.AllSaleRecords(ctx, owner.ID, cardID)
	if allErr != nil || len(all) != 1 || all[0] != record {
		t.Fatalf("all=%+v err=%v", all, allErr)
	}
	// byOrder 保存按订单读取到的台账记录；orderErr 保存按订单读取台账的错误。
	byOrder, orderErr := repository.SaleRecordsByOrder(ctx, owner.ID, "acc1", "o1")
	if orderErr != nil || len(byOrder) != 1 || byOrder[0] != record {
		t.Fatalf("byOrder=%+v err=%v", byOrder, orderErr)
	}
	// 其他用户看不到该订单的台账，且结果为非 nil 空切片。
	empty, emptyErr := repository.SaleRecordsByOrder(ctx, owner.ID+1, "acc1", "o1")
	if emptyErr != nil || empty == nil || len(empty) != 0 {
		t.Fatalf("empty=%#v err=%v", empty, emptyErr)
	}
}
