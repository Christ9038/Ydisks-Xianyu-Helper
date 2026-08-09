package db

import (
	"context"
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
		t.Fatalf("deleted item still exists: err=%v", err)
	}
}
