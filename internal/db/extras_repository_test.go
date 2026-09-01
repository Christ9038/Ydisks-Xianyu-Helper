package db

import (
	"context"
	"database/sql"
	"testing"
)

// TestExtraRepositoriesCRUD 封装TestExtraRepositoriesCRUD业务协调。
func TestExtraRepositoriesCRUD(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "user1", "u1@example.com", "pw"); err != nil || !ok {
		t.Fatalf("create user: %v, %v", ok, err)
	}
	// user 用于本次流程后续判断的用户
	user, _ := store.Users.GetByUsername(ctx, "user1")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(ctx, "acc1", "unb=1", user.ID); err != nil {
		t.Fatal(err)
	}

	// id、err 用于本次流程后续判断的id、err
	id, err := store.Keywords.Add(ctx, "acc1", "hello", "world", "item1", "text", "")
	if err != nil || id == 0 {
		t.Fatalf("add keyword: %d, %v", id, err)
	}
	// keywords 用于本次流程后续判断的keywords
	keywords, _ := store.Keywords.AllRows(ctx, "acc1")
	if len(keywords) != 1 || keywords[0].Reply != "world" {
		t.Fatalf("keywords = %#v", keywords)
	}
	if // err 用于本次流程后续判断的err
	err := store.Keywords.DeleteByIndex(ctx, "acc1", 0); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Keywords.DeleteByIndex(ctx, "acc1", 0); err != ErrNotFound {
		t.Fatalf("delete missing keyword = %v", err)
	}

	if // err 用于本次流程后续判断的err
	err := store.ItemReps.Set(ctx, "acc1", "item1", "reply"); err != nil {
		t.Fatal(err)
	}
	// replies 用于本次流程后续判断的回复列表
	replies, _ := store.ItemReps.AllForUser(ctx, "acc1")
	if len(replies) != 1 || replies[0].ReplyContent != "reply" {
		t.Fatalf("item replies = %#v", replies)
	}
	if // err 用于本次流程后续判断的err
	err := store.ItemReps.Delete(ctx, "acc1", "item1"); err != nil {
		t.Fatal(err)
	}

	// item 用于本次流程后续判断的商品
	item := &ItemInfoRow{CookieID: "acc1", ItemID: "item1", ItemTitle: "title", ItemPrice: "9.90"}
	if // err 用于本次流程后续判断的err
	err := store.Items.Upsert(ctx, item); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Items.SetMultiSpec(ctx, "acc1", "item1", true); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Items.SetMultiQuantity(ctx, "acc1", "item1", true); err != nil {
		t.Fatal(err)
	}
	// items 用于本次流程后续判断的商品列表
	items, _ := store.Items.AllForCookie(ctx, "acc1")
	if len(items) != 1 || !items[0].IsMultiSpec || !items[0].MultiQuantityDelivery {
		t.Fatalf("items = %#v", items)
	}
	if // err 用于本次流程后续判断的err
	err := store.Items.UpsertBasic(ctx, &ItemInfoRow{CookieID: "acc1", ItemID: "item1", ItemTitle: "updated"}); err != nil {
		t.Fatal(err)
	}
	items, _ = store.Items.AllForCookie(ctx, "acc1")
	if items[0].ItemTitle != "updated" || !items[0].IsMultiSpec {
		t.Fatalf("upsert basic overwrote flags: %#v", items[0])
	}

	// channelID、err 用于本次流程后续判断的渠道ID、err
	channelID, err := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "webhook", Type: "webhook", Config: `{}`, Enabled: true, UserID: user.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Notifications.SetBindings(ctx, "acc1", []int64{channelID}); err != nil {
		t.Fatal(err)
	}
	// bindings 用于本次流程后续判断的bindings
	bindings, _ := store.Notifications.AccountBindings(ctx, "acc1")
	if len(bindings) != 1 || bindings[0] != channelID {
		t.Fatalf("bindings = %#v", bindings)
	}
	// channels 用于本次流程后续判断的渠道列表
	channels, _ := store.Notifications.AllChannelsForUser(ctx, user.ID)
	if len(channels) != 1 || !channels[0].Enabled {
		t.Fatalf("channels = %#v", channels)
	}
	channels[0].Name = "updated"
	if // err 用于本次流程后续判断的err
	err := store.Notifications.UpdateChannel(ctx, &channels[0]); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Notifications.DeleteChannel(ctx, channelID); err != nil {
		t.Fatal(err)
	}

	if // err 用于本次流程后续判断的err
	err := store.Settings.Set(ctx, "theme_color", "blue"); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Settings.Set(ctx, "private_key", "secret"); err != nil {
		t.Fatal(err)
	}
	// public、err 用于本次流程后续判断的public、err
	public, err := store.Settings.Public(ctx)
	if err != nil || public["theme_color"] != "blue" || public["private_key"] != "" {
		t.Fatalf("public settings = %#v, %v", public, err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Items.Delete(ctx, "acc1", "item1"); err != nil {
		t.Fatal(err)
	}
}

// TestNotificationChannelSummaryDoesNotDecryptConfig 验证渠道列表摘要不会因损坏密文而解密失败。
func TestNotificationChannelSummaryDoesNotDecryptConfig(t *testing.T) {
	// store、cleanup 保存隔离数据库及其释放函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本次摘要查询使用的请求上下文。
	ctx := context.Background()
	// created、createErr 保存测试用户创建结果。
	created, createErr := store.Users.Create(ctx, "summary-user", "summary@example.com", "password")
	if createErr != nil || !created {
		t.Fatalf("create user: created=%v err=%v", created, createErr)
	}
	// user、userErr 保存渠道所属用户及查询错误。
	user, userErr := store.Users.GetByUsername(ctx, "summary-user")
	if userErr != nil {
		t.Fatal(userErr)
	}
	// channelID、channelErr 保存渠道创建结果。
	channelID, channelErr := store.Notifications.CreateChannel(ctx, &NotificationChannelRow{
		Name: "webhook", Type: "webhook", Config: `{"url":"secret"}`, UserID: user.ID,
	})
	if channelErr != nil {
		t.Fatal(channelErr)
	}
	// corruptErr 保存故意写入损坏密文的错误；摘要查询不应读取该字段。
	if _, corruptErr := store.DB.ExecContext(ctx, `UPDATE notification_channels SET config=? WHERE id=?`, "not-a-ciphertext", channelID); corruptErr != nil {
		t.Fatal(corruptErr)
	}
	// summaries、summaryErr 保存摘要查询结果及错误。
	summaries, summaryErr := store.Notifications.ListChannelSummariesForUser(ctx, user.ID)
	if summaryErr != nil || len(summaries) != 1 || summaries[0].Name != "webhook" {
		t.Fatalf("summaries=%+v err=%v", summaries, summaryErr)
	}
}

// TestItemsSyncFromRemoteUpdatesMultiSpecBothDirections 验证商品同步会覆盖多规格标记的双向变化。
func TestItemsSyncFromRemoteUpdatesMultiSpecBothDirections(t *testing.T) {
	// store、cleanup 保存隔离数据库和清理责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存本测试共用的数据库上下文。
	ctx := context.Background()
	// created、err 保存测试用户创建结果。
	created, err := store.Users.Create(ctx, "multi-spec-sync-user", "multi-spec-sync@example.com", "password")
	if err != nil || !created {
		t.Fatalf("创建测试用户失败 created=%v err=%v", created, err)
	}
	// user、err 保存测试用户及其查询错误。
	user, err := store.Users.GetByUsername(ctx, "multi-spec-sync-user")
	if err != nil {
		t.Fatal(err)
	}
	// err 保存测试账号凭证写入错误；商品表通过该账号满足外键约束。
	if err := store.Cookies.Save(ctx, "acc1", "unb=1", user.ID); err != nil {
		t.Fatal(err)
	}
	// err 保存初始多规格商品写入错误。
	if err := store.Items.Upsert(ctx, &ItemInfoRow{CookieID: "acc1", ItemID: "changing-item", ItemTitle: "变更商品", IsMultiSpec: true}); err != nil {
		t.Fatal(err)
	}
	// result、err 保存多规格转单规格同步结果。
	result, err := store.Items.SyncFromRemote(ctx, "acc1", []ItemInfoRow{{ItemID: "changing-item", IsMultiSpec: false}})
	if err != nil || result.Saved != 1 {
		t.Fatalf("多规格转单规格同步失败 result=%+v err=%v", result, err)
	}
	// item、err 保存多规格转单规格后的商品记录。
	item, err := store.Items.Get(ctx, "acc1", "changing-item")
	if err != nil {
		t.Fatal(err)
	}
	if item.IsMultiSpec {
		t.Fatal("同步单规格结果不应继续保留多规格标记")
	}
	// result、err 保存单规格转多规格同步结果。
	result, err = store.Items.SyncFromRemote(ctx, "acc1", []ItemInfoRow{{ItemID: "changing-item", IsMultiSpec: true}})
	if err != nil || result.Saved != 1 {
		t.Fatalf("单规格转多规格同步失败 result=%+v err=%v", result, err)
	}
	// item、err 保存单规格转多规格后的商品记录。
	item, err = store.Items.Get(ctx, "acc1", "changing-item")
	if err != nil {
		t.Fatal(err)
	}
	if !item.IsMultiSpec {
		t.Fatal("同步多规格结果未写入多规格标记")
	}
}

// TestItemsSyncFromRemoteReconcilesAndPreservesLocalSettings 封装Test商品列表SyncFromRemoteReconcilesAndPreservesLocal设置业务协调。
func TestItemsSyncFromRemoteReconcilesAndPreservesLocalSettings(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "sync-user", "sync@example.com", "password")
	if err != nil || !ok {
		t.Fatalf("create test user: ok=%v err=%v", ok, err)
	}
	// user、err 用于本次流程后续判断的user、err
	user, err := store.Users.GetByUsername(ctx, "sync-user")
	if err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(ctx, "acc1", "unb=1", user.ID); err != nil {
		t.Fatal(err)
	}

	if // err 用于本次流程后续判断的err
	err := store.Items.Upsert(ctx, &ItemInfoRow{
		CookieID: "acc1", ItemID: "existing", ItemTitle: "旧标题", ItemDescription: "本地描述", ItemPrice: "¥9.90",
	}); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Items.SetMultiSpec(ctx, "acc1", "existing", true); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Items.SetMultiQuantity(ctx, "acc1", "existing", true); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Items.Upsert(ctx, &ItemInfoRow{CookieID: "acc1", ItemID: "deleted"}); err != nil {
		t.Fatal(err)
	}
	// ruleID、err 用于本次流程后续判断的规则ID、err
	ruleID, err := store.Automation.Create(ctx, AutomationRuleInput{
		UserID: user.ID, CookieID: "acc1", ItemID: "deleted", Name: "删除商品规则",
		TriggerType: "paid", Enabled: true, Priority: 100,
		Actions: []AutomationActionInput{{ActionType: "send_msg", MessageTemplate: "hello", Enabled: true, SortOrder: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}

	// result、err 用于本次流程后续判断的result、err
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

	// items、err 用于本次流程后续判断的items、err
	items, err := store.Items.AllForCookie(ctx, "acc1")
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 2 {
		t.Fatalf("items=%+v, want 2 rows", items)
	}
	// item、err 用于本次流程后续判断的item、err
	item, err := store.Items.Get(ctx, "acc1", "existing")
	if err != nil {
		t.Fatal(err)
	}
	if item.ItemTitle != "新标题" || item.ItemPrice != "¥19.90" || item.ItemDescription != "本地描述" ||
		!item.IsMultiSpec || !item.MultiQuantityDelivery {
		t.Fatalf("existing item was not updated/preserved: %+v", item)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.Items.Get(ctx, "acc1", "deleted"); err != ErrNotFound {
		t.Fatalf("deleted item should be hidden from active lookup: err=%v", err)
	}
	// itemDeletedAt 用于本次流程后续判断的商品DeletedAt
	var itemDeletedAt sql.NullString
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx,
		`SELECT deleted_at FROM item_info WHERE cookie_id=? AND item_id=?`, "acc1", "deleted").Scan(&itemDeletedAt); err != nil {
		t.Fatalf("商品逻辑删除后原始行不存在: %v", err)
	}
	if !itemDeletedAt.Valid || itemDeletedAt.String == "" {
		t.Fatalf("商品 deleted_at 未写入: %#v", itemDeletedAt)
	}
	// ruleDeletedAt 用于本次流程后续判断的规则DeletedAt
	var ruleDeletedAt sql.NullString
	// ruleEnabled 用于本次流程后续判断的规则启用状态
	var ruleEnabled int
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx,
		`SELECT deleted_at, enabled FROM automation_rules WHERE id=?`, ruleID).Scan(&ruleDeletedAt, &ruleEnabled); err != nil {
		t.Fatalf("关联规则逻辑删除后原始行不存在: %v", err)
	}
	if !ruleDeletedAt.Valid || ruleDeletedAt.String == "" || ruleEnabled != 0 {
		t.Fatalf("关联规则未逻辑删除并禁用: deleted_at=%#v enabled=%d", ruleDeletedAt, ruleEnabled)
	}
	// rules、err 用于本次流程后续判断的rules、err
	rules, err := store.Automation.ListForUser(ctx, user.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(rules) != 0 {
		t.Fatalf("已删除商品的规则不应出现在管理列表: %#v", rules)
	}
	// matched、err 用于本次流程后续判断的matched、err
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
	if // err 用于本次流程后续判断的err
	_, err := store.Items.Get(ctx, "acc1", "deleted"); err != nil {
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

// TestItemsDeleteSoftDeletesRelatedAutomationRule 封装Test商品列表DeleteSoftDeletesRelated自动化规则业务协调。
func TestItemsDeleteSoftDeletesRelatedAutomationRule(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	if // ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, "item-delete-user", "item-delete@example.com", "password"); err != nil || !ok {
		t.Fatalf("create test user: ok=%v err=%v", ok, err)
	}
	// user、err 用于本次流程后续判断的user、err
	user, err := store.Users.GetByUsername(ctx, "item-delete-user")
	if err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(ctx, "item-delete-cookie", "unb=1", user.ID); err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Items.Upsert(ctx, &ItemInfoRow{CookieID: "item-delete-cookie", ItemID: "item-1", ItemTitle: "待删除商品"}); err != nil {
		t.Fatal(err)
	}
	// ruleID、err 用于本次流程后续判断的规则ID、err
	ruleID, err := store.Automation.Create(ctx, AutomationRuleInput{
		UserID: user.ID, CookieID: "item-delete-cookie", ItemID: "item-1", Name: "商品规则",
		TriggerType: "paid", Enabled: true, Priority: 100,
		Actions: []AutomationActionInput{{ActionType: "send_msg", MessageTemplate: "hello", Enabled: true, SortOrder: 1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Items.Delete(ctx, "item-delete-cookie", "item-1"); err != nil {
		t.Fatal(err)
	}
	// itemDeletedAt、ruleDeletedAt 用于本次流程后续判断的商品DeletedAt、ruleDeletedAt
	var itemDeletedAt, ruleDeletedAt string
	// ruleEnabled 用于本次流程后续判断的规则启用状态
	var ruleEnabled int
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT deleted_at FROM item_info WHERE cookie_id=? AND item_id=?`, "item-delete-cookie", "item-1").Scan(&itemDeletedAt); err != nil {
		t.Fatalf("商品行未保留: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT deleted_at, enabled FROM automation_rules WHERE id=?`, ruleID).Scan(&ruleDeletedAt, &ruleEnabled); err != nil {
		t.Fatalf("规则行未保留: %v", err)
	}
	if itemDeletedAt == "" || ruleDeletedAt == "" || ruleEnabled != 0 {
		t.Fatalf("商品和规则未完成逻辑删除: item_deleted_at=%q rule_deleted_at=%q enabled=%d", itemDeletedAt, ruleDeletedAt, ruleEnabled)
	}
}
