package db

import (
	"context"
	"errors"
	"testing"
)

// TestItemGreetingRuleLifecycle 覆盖规则创建、列表、商品冲突、更新、按商品命中、停用和删除。
func TestItemGreetingRuleLifecycle(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存规则的账号归属。
	userID, cookieID := seedAccount(t, store)

	// firstID、err 保存覆盖两个商品的第一条规则。
	firstID, err := store.ItemGreetings.Create(ctx, ItemGreetingRule{CookieID: cookieID, Name: "一", Enabled: true, ReplyContent: "你好 {item_id}", ItemIDs: []string{"i1", "i2"}})
	if err != nil || firstID == 0 {
		t.Fatalf("create first id=%d err=%v", firstID, err)
	}
	// conflict 验证同账号商品不能被第二条规则重复占用。
	_, err = store.ItemGreetings.Create(ctx, ItemGreetingRule{CookieID: cookieID, Name: "二", Enabled: true, ReplyContent: "x", ItemIDs: []string{"i2", "i3"}})
	// conflictErr 保存期望的冲突错误类型。
	var conflictErr *ItemGreetingConflictError
	if !errors.As(err, &conflictErr) || len(conflictErr.ItemIDs) != 1 || conflictErr.ItemIDs[0] != "i2" {
		t.Fatalf("conflict err=%v", err)
	}
	// secondID 保存不冲突的第二条规则。
	secondID, err := store.ItemGreetings.Create(ctx, ItemGreetingRule{CookieID: cookieID, Name: "二", Enabled: false, ReplyImageURL: "https://example.test/a.png", ItemIDs: []string{"i3"}})
	if err != nil {
		t.Fatal(err)
	}

	// byAccount、byUser 验证两种列表入口都带出商品集合。
	byAccount, err := store.ItemGreetings.ListByAccount(ctx, cookieID)
	if err != nil || len(byAccount) != 2 || len(byAccount[0].ItemIDs) != 2 || byAccount[1].Enabled {
		t.Fatalf("byAccount=%+v err=%v", byAccount, err)
	}
	// byUser、err 保存 ListForUser 的返回值及错误。
	byUser, err := store.ItemGreetings.ListForUser(ctx, userID)
	if err != nil || len(byUser) != 2 {
		t.Fatalf("byUser=%+v err=%v", byUser, err)
	}

	// hit 验证启用规则按商品命中；停用规则和未配置商品返回 ErrNotFound。
	hit, err := store.ItemGreetings.FindEnabledForItem(ctx, cookieID, "i1")
	if err != nil || hit.ID != firstID {
		t.Fatalf("hit=%+v err=%v", hit, err)
	}
	// itemID 是当前处理的商品标识。
	for _, itemID := range []string{"i3", "missing"} {
		// err 保存 FindEnabledForItem 的返回错误，非空时按该分支处理。
		if _, err := store.ItemGreetings.FindEnabledForItem(ctx, cookieID, itemID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("item %s err=%v", itemID, err)
		}
	}

	// 更新第一条规则：释放 i2、改占 i4，并验证自身商品不算冲突。
	if err := store.ItemGreetings.Update(ctx, ItemGreetingRule{ID: firstID, CookieID: cookieID, Name: "一改", Enabled: true, ReplyContent: "新", ItemIDs: []string{"i1", "i4"}}); err != nil {
		t.Fatal(err)
	}
	// err、updated 保存 Get 的返回值及错误。
	updated, err := store.ItemGreetings.Get(ctx, firstID)
	if err != nil || updated.Name != "一改" || len(updated.ItemIDs) != 2 || updated.ItemIDs[1] != "i4" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	// 更新为已被他人占用的商品应冲突；不存在的规则返回 ErrNotFound。
	if err := store.ItemGreetings.Update(ctx, ItemGreetingRule{ID: firstID, CookieID: cookieID, ItemIDs: []string{"i3"}}); !errors.As(err, &conflictErr) {
		t.Fatalf("update conflict err=%v", err)
	}
	// err 保存 Update 的返回错误，非空时按该分支处理。
	if err := store.ItemGreetings.Update(ctx, ItemGreetingRule{ID: 9999, CookieID: cookieID, ItemIDs: []string{"z"}}); !errors.Is(err, ErrNotFound) {
		t.Fatalf("update missing err=%v", err)
	}
	// err 保存 Get 的返回错误，非空时按该分支处理。
	if _, err := store.ItemGreetings.Get(ctx, 9999); !errors.Is(err, ErrNotFound) {
		t.Fatalf("get missing err=%v", err)
	}

	// 删除后商品被释放，可被新规则占用。
	if err := store.ItemGreetings.Delete(ctx, secondID); err != nil {
		t.Fatal(err)
	}
	// err 保存 Create 的返回错误，非空时按该分支处理。
	if _, err := store.ItemGreetings.Create(ctx, ItemGreetingRule{CookieID: cookieID, Name: "三", Enabled: true, ReplyContent: "y", ItemIDs: []string{"i3"}}); err != nil {
		t.Fatalf("reuse released item err=%v", err)
	}
}

// TestItemGreetingRecordsStateMachine 验证首次回复记录与默认回复记录相互独立，且领取、确认、隔离、清理语义一致。
func TestItemGreetingRecordsStateMachine(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// cookieID 是记录所属账号。
	_, cookieID := seedAccount(t, store)

	// exists 验证未触发的会话没有记录。
	if exists, err := store.ItemGreetings.HasRecord(ctx, cookieID, "chat"); err != nil || exists {
		t.Fatalf("exists=%v err=%v", exists, err)
	}
	// record、claimed 验证首次领取成功，重复领取被租约挡住。
	record, claimed, err := store.ItemGreetings.ClaimRecord(ctx, cookieID, "chat", true, false)
	if err != nil || !claimed || record.Status != defaultReplyStatusSending || record.TextSent || !record.ImageSent {
		t.Fatalf("record=%+v claimed=%v err=%v", record, claimed, err)
	}
	// again、err 保存 ClaimRecord 的返回值及错误。
	if _, again, err := store.ItemGreetings.ClaimRecord(ctx, cookieID, "chat", true, false); err != nil || again {
		t.Fatalf("again=%v err=%v", again, err)
	}
	// 默认回复记录表不受影响。
	if store.DefaultReps.HasRecord(ctx, cookieID, "chat") {
		t.Fatal("default reply records must stay independent")
	}
	// exists 保存 HasRecord 的返回值。
	if exists, _ := store.ItemGreetings.HasRecord(ctx, cookieID, "chat"); !exists {
		t.Fatal("sending record must count as triggered")
	}
	// err 保存 MarkPartSent 的返回错误，非空时按该分支处理。
	if err := store.ItemGreetings.MarkPartSent(ctx, cookieID, "chat", "text"); err != nil {
		t.Fatal(err)
	}
	// err 保存 MarkRecordSent 的返回错误，非空时按该分支处理。
	if err := store.ItemGreetings.MarkRecordSent(ctx, cookieID, "chat"); err != nil {
		t.Fatal(err)
	}
	// again 保存 ClaimRecord 的返回值。
	if _, again, _ := store.ItemGreetings.ClaimRecord(ctx, cookieID, "chat", true, false); again {
		t.Fatal("sent record must not be claimed again")
	}

	// 失败与隔离：failed 可重领，uncertain 不可重领。
	if _, _, err := store.ItemGreetings.ClaimRecord(ctx, cookieID, "chat2", true, false); err != nil {
		t.Fatal(err)
	}
	// err 保存 MarkRecordFailed 的返回错误，非空时按该分支处理。
	if err := store.ItemGreetings.MarkRecordFailed(ctx, cookieID, "chat2", "boom"); err != nil {
		t.Fatal(err)
	}
	// err、retry 保存 ClaimRecord 的返回值及错误。
	if _, retry, err := store.ItemGreetings.ClaimRecord(ctx, cookieID, "chat2", true, false); err != nil || !retry {
		t.Fatalf("failed record should be claimable retry=%v err=%v", retry, err)
	}
	// err 保存 MarkRecordUncertain 的返回错误，非空时按该分支处理。
	if err := store.ItemGreetings.MarkRecordUncertain(ctx, cookieID, "chat2", "unknown"); err != nil {
		t.Fatal(err)
	}
	// retry 保存 ClaimRecord 的返回值。
	if _, retry, _ := store.ItemGreetings.ClaimRecord(ctx, cookieID, "chat2", true, false); retry {
		t.Fatal("uncertain record must stay quarantined")
	}

	// 清理后会话重新可触发。
	if err := store.ItemGreetings.ClearRecords(ctx, cookieID); err != nil {
		t.Fatal(err)
	}
	// exists 保存 HasRecord 的返回值。
	if exists, _ := store.ItemGreetings.HasRecord(ctx, cookieID, "chat"); exists {
		t.Fatal("records should be cleared")
	}
}

// TestDeleteCookieRemovesItemGreetings 验证删除账号会清理规则、商品和记录，账号 ID 复用时不继承旧数据。
func TestDeleteCookieRemovesItemGreetings(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// cookieID 是将被删除的账号。
	_, cookieID := seedAccount(t, store)
	// err 保存 Create 的返回错误，非空时按该分支处理。
	if _, err := store.ItemGreetings.Create(ctx, ItemGreetingRule{CookieID: cookieID, Enabled: true, ReplyContent: "hi", ItemIDs: []string{"i1"}}); err != nil {
		t.Fatal(err)
	}
	// err 保存 ClaimRecord 的返回错误，非空时按该分支处理。
	if _, _, err := store.ItemGreetings.ClaimRecord(ctx, cookieID, "chat", true, false); err != nil {
		t.Fatal(err)
	}
	// err 保存 Delete 的返回错误，非空时按该分支处理。
	if err := store.Cookies.Delete(ctx, cookieID); err != nil {
		t.Fatal(err)
	}
	// n 保存三张表残留的总行数。
	var n int
	// err 保存 QueryRow 的返回错误，非空时按该分支处理。
	if err := store.DB.QueryRow(`SELECT (SELECT COUNT(*) FROM item_greeting_rules)+(SELECT COUNT(*) FROM item_greeting_rule_items)+(SELECT COUNT(*) FROM item_greeting_records)`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("leftover=%d err=%v", n, err)
	}
}
