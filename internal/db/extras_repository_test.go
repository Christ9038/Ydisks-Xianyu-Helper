package db

import (
	"context"
	"database/sql"
	"testing"
)

func TestExtraRepositoriesCRUD(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "user1", "u1@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create user: %v, %v", ok, err)
	}
	user, _ := store.Users.GetByUsername(ctx, "user1")
	if err := store.Cookies.Save(ctx, "acc1", "unb=1", user.ID); err != nil {
		t.Fatal(err)
	}

	id, err := store.Keywords.Add(ctx, "acc1", "hello", "world", "item1", "text", "")
	if err != nil || id == 0 {
		t.Fatalf("add keyword: %d, %v", id, err)
	}
	keywords, _ := store.Keywords.AllRows(ctx, "acc1")
	if len(keywords) != 1 || keywords[0].Reply != "world" {
		t.Fatalf("keywords = %#v", keywords)
	}
	if err := store.Keywords.DeleteByIndex(ctx, "acc1", 0); err != nil {
		t.Fatal(err)
	}
	if err := store.Keywords.DeleteByIndex(ctx, "acc1", 0); err != ErrNotFound {
		t.Fatalf("delete missing keyword = %v", err)
	}

	if err := store.ItemReps.Set(ctx, "acc1", "item1", "reply"); err != nil {
		t.Fatal(err)
	}
	replies, _ := store.ItemReps.AllForUser(ctx, "acc1")
	if len(replies) != 1 || replies[0].ReplyContent != "reply" {
		t.Fatalf("item replies = %#v", replies)
	}
	if err := store.ItemReps.Delete(ctx, "acc1", "item1"); err != nil {
		t.Fatal(err)
	}

	item := &ItemInfoRow{CookieID: "acc1", ItemID: "item1", ItemTitle: "title", ItemPrice: "9.90"}
	if err := store.Items.Upsert(ctx, item); err != nil {
		t.Fatal(err)
	}
	if err := store.Items.SetMultiSpec(ctx, "acc1", "item1", true); err != nil {
		t.Fatal(err)
	}
	if err := store.Items.SetMultiQuantity(ctx, "acc1", "item1", true); err != nil {
		t.Fatal(err)
	}
	items, _ := store.Items.AllForCookie(ctx, "acc1")
	if len(items) != 1 || !items[0].IsMultiSpec || !items[0].MultiQuantityDelivery {
		t.Fatalf("items = %#v", items)
	}
	if err := store.Items.UpsertBasic(ctx, &ItemInfoRow{CookieID: "acc1", ItemID: "item1", ItemTitle: "updated"}); err != nil {
		t.Fatal(err)
	}
	items, _ = store.Items.AllForCookie(ctx, "acc1")
	if items[0].ItemTitle != "updated" || !items[0].IsMultiSpec {
		t.Fatalf("upsert basic overwrote flags: %#v", items[0])
	}

	channelID, err := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "webhook", Type: "webhook", Config: `{}`, Enabled: true, UserID: user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Notifications.SetBindings(ctx, "acc1", []int64{channelID}); err != nil {
		t.Fatal(err)
	}
	bindings, _ := store.Notifications.AccountBindings(ctx, "acc1")
	if len(bindings) != 1 || bindings[0] != channelID {
		t.Fatalf("bindings = %#v", bindings)
	}
	channels, _ := store.Notifications.AllChannelsForUser(ctx, user.ID)
	if len(channels) != 1 || !channels[0].Enabled {
		t.Fatalf("channels = %#v", channels)
	}
	channels[0].Name = "updated"
	if err := store.Notifications.UpdateChannel(ctx, &channels[0]); err != nil {
		t.Fatal(err)
	}
	if err := store.Notifications.DeleteChannel(ctx, channelID); err != nil {
		t.Fatal(err)
	}

	if err := store.Settings.Set(ctx, "theme_color", "blue"); err != nil {
		t.Fatal(err)
	}
	if err := store.Settings.Set(ctx, "private_key", "secret"); err != nil {
		t.Fatal(err)
	}
	public, err := store.Settings.Public(ctx)
	if err != nil || public["theme_color"] != "blue" || public["private_key"] != "" {
		t.Fatalf("public settings = %#v, %v", public, err)
	}
	if err := store.Items.Delete(ctx, "acc1", "item1"); err != nil {
		t.Fatal(err)
	}
}

func TestItemsSyncFromRemoteReconcilesAndPreservesLocalSettings(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	ok, err := store.Users.Create(ctx, "sync-user", "sync@example.com", "password")
	if err != nil || !ok {
		t.Fatalf("create test user: ok=%v err=%v", ok, err)
	}
	user, err := store.Users.GetByUsername(ctx, "sync-user")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Cookies.Save(ctx, "acc1", "unb=1", user.ID); err != nil {
		t.Fatal(err)
	}

	if err := store.Items.Upsert(ctx, &ItemInfoRow{
		CookieID: "acc1", ItemID: "existing", ItemTitle: "旧标题", ItemDescription: "本地描述", ItemPrice: "¥9.90",
	}); err != nil {
		t.Fatal(err)
	}
	if err := store.Items.SetMultiSpec(ctx, "acc1", "existing", true); err != nil {
		t.Fatal(err)
	}
	if err := store.Items.SetMultiQuantity(ctx, "acc1", "existing", true); err != nil {
		t.Fatal(err)
	}
	if err := store.Items.Upsert(ctx, &ItemInfoRow{CookieID: "acc1", ItemID: "deleted"}); err != nil {
		t.Fatal(err)
	}
	ruleID, err := store.Automation.Create(ctx, AutomationRuleInput{
		UserID: user.ID, CookieID: "acc1", ItemID: "deleted", Name: "删除商品规则",
		TriggerType: "paid", Enabled: true, Priority: 100,
		Actions: []AutomationActionInput{{ActionType: "send_msg", MessageTemplate: "hello", Enabled: true, SortOrder: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	result, err := store.Items.SyncFromRemote(ctx, "acc1", []ItemInfoRow{
		{CookieID: "wrong-cookie", ItemID: "existing", ItemTitle: "新标题", ItemPrice: "¥19.90", IsMultiSpec: true},
		{ItemID: "new", ItemTitle: "新商品", ItemPrice: "¥3.00"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.Saved != 2 || result.Deleted != 1 {
		t.Fatalf("sync result=%+v, want saved=2 deleted=1", result)
	}

	items, err := store.Items.AllForCookie(ctx, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items=%+v, want 2 rows", items)
	}
	item, err := store.Items.Get(ctx, "acc1", "existing")
	if err != nil {
		t.Fatal(err)
	}
	if item.ItemTitle != "新标题" || item.ItemPrice != "¥19.90" || item.ItemDescription != "本地描述" ||
		!item.IsMultiSpec || !item.MultiQuantityDelivery {
		t.Fatalf("existing item was not updated/preserved: %+v", item)
	}
	if _, err := store.Items.Get(ctx, "acc1", "deleted"); err != ErrNotFound {
		t.Fatalf("deleted item should be hidden from active lookup: err=%v", err)
	}
	var itemDeletedAt sql.NullString
	if err := store.DB.QueryRowContext(ctx,
		`SELECT deleted_at FROM item_info WHERE cookie_id=? AND item_id=?`, "acc1", "deleted").Scan(&itemDeletedAt); err != nil {
		t.Fatalf("商品逻辑删除后原始行不存在: %v", err)
	}
	if !itemDeletedAt.Valid || itemDeletedAt.String == "" {
		t.Fatalf("商品 deleted_at 未写入: %#v", itemDeletedAt)
	}
	var ruleDeletedAt sql.NullString
	var ruleEnabled int
	if err := store.DB.QueryRowContext(ctx,
		`SELECT deleted_at, enabled FROM automation_rules WHERE id=?`, ruleID).Scan(&ruleDeletedAt, &ruleEnabled); err != nil {
		t.Fatalf("关联规则逻辑删除后原始行不存在: %v", err)
	}
	if !ruleDeletedAt.Valid || ruleDeletedAt.String == "" || ruleEnabled != 0 {
		t.Fatalf("关联规则未逻辑删除并禁用: deleted_at=%#v enabled=%d", ruleDeletedAt, ruleEnabled)
	}
	rules, err := store.Automation.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("已删除商品的规则不应出现在管理列表: %#v", rules)
	}
	matched, err := store.Automation.Match(ctx, "acc1", "deleted", "paid")
	if err != nil {
		t.Fatal(err)
	}
	if len(matched) != 0 {
		t.Fatalf("已删除商品的规则不应再匹配: %#v", matched)
	}

	// 商品再次出现在远端时只恢复商品，不自动恢复已删除规则，避免旧规则误复活。
	if _, err := store.Items.SyncFromRemote(ctx, "acc1", []ItemInfoRow{{ItemID: "existing"}, {ItemID: "deleted"}}); err != nil {
		t.Fatal(err)
	}
	if _, err := store.Items.Get(ctx, "acc1", "deleted"); err != nil {
		t.Fatalf("商品重新同步后应恢复商品记录: %v", err)
	}
	rules, err = store.Automation.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("商品恢复后不应自动恢复旧规则: %#v", rules)
	}
}

func TestItemsDeleteSoftDeletesRelatedAutomationRule(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	if ok, err := store.Users.Create(ctx, "item-delete-user", "item-delete@example.com", "password"); err != nil || !ok {
		t.Fatalf("create test user: ok=%v err=%v", ok, err)
	}
	user, err := store.Users.GetByUsername(ctx, "item-delete-user")
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Cookies.Save(ctx, "item-delete-cookie", "unb=1", user.ID); err != nil {
		t.Fatal(err)
	}
	if err := store.Items.Upsert(ctx, &ItemInfoRow{CookieID: "item-delete-cookie", ItemID: "item-1", ItemTitle: "待删除商品"}); err != nil {
		t.Fatal(err)
	}
	ruleID, err := store.Automation.Create(ctx, AutomationRuleInput{
		UserID: user.ID, CookieID: "item-delete-cookie", ItemID: "item-1", Name: "商品规则",
		TriggerType: "paid", Enabled: true, Priority: 100,
		Actions: []AutomationActionInput{{ActionType: "send_msg", MessageTemplate: "hello", Enabled: true, SortOrder: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := store.Items.Delete(ctx, "item-delete-cookie", "item-1"); err != nil {
		t.Fatal(err)
	}
	var itemDeletedAt, ruleDeletedAt string
	var ruleEnabled int
	if err := store.DB.QueryRowContext(ctx, `SELECT deleted_at FROM item_info WHERE cookie_id=? AND item_id=?`, "item-delete-cookie", "item-1").Scan(&itemDeletedAt); err != nil {
		t.Fatalf("商品行未保留: %v", err)
	}
	if err := store.DB.QueryRowContext(ctx, `SELECT deleted_at, enabled FROM automation_rules WHERE id=?`, ruleID).Scan(&ruleDeletedAt, &ruleEnabled); err != nil {
		t.Fatalf("规则行未保留: %v", err)
	}
	if itemDeletedAt == "" || ruleDeletedAt == "" || ruleEnabled != 0 {
		t.Fatalf("商品和规则未完成逻辑删除: item_deleted_at=%q rule_deleted_at=%q enabled=%d", itemDeletedAt, ruleDeletedAt, ruleEnabled)
	}
}
