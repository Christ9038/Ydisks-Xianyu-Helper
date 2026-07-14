package db

import (
	"context"
	"errors"
	"testing"
)

func TestUpdateAccountSettingsIsAtomic(t *testing.T) {
	store, cleanup := newTestDB(t)
	defer cleanup()
	ctx := context.Background()
	for _, user := range []struct{ name, email string }{{"settings-owner", "settings-owner@example.com"}, {"settings-other", "settings-other@example.com"}} {
		if ok, err := store.Users.Create(ctx, user.name, user.email, "pw"); err != nil || !ok {
			t.Fatalf("create %s: ok=%v err=%v", user.name, ok, err)
		}
	}
	owner, _ := store.Users.GetByUsername(ctx, "settings-owner")
	other, _ := store.Users.GetByUsername(ctx, "settings-other")
	if err := store.Cookies.CreateOwned(ctx, "settings-cookie", "old-cookie", owner.ID); err != nil {
		t.Fatal(err)
	}
	channelResult, err := store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,1,?)`, "other", "webhook", `{}`, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	otherChannelID, _ := channelResult.LastInsertId()
	newCookie, remark := "new-cookie", "new remark"
	autoConfirm := false
	badChannels := []int64{otherChannelID}
	_, err = store.Cookies.UpdateSettings(ctx, "settings-cookie", AccountSettingsUpdate{
		UserID: owner.ID, Value: &newCookie, Remark: &remark, AutoConfirm: &autoConfirm, ChannelIDs: &badChannels,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden channel, got %v", err)
	}
	detail, _ := store.Cookies.GetDetails(ctx, "settings-cookie")
	if detail.Value != "old-cookie" || detail.Remark != "" || !detail.AutoConfirm {
		t.Fatalf("failed aggregate update partially committed: %+v", detail)
	}

	channelResult, err = store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,1,?)`, "owned", "webhook", `{}`, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	ownedChannelID, _ := channelResult.LastInsertId()
	channels := []int64{ownedChannelID, ownedChannelID}
	pause := 5
	if _, err := store.Cookies.UpdateSettings(ctx, "settings-cookie", AccountSettingsUpdate{
		UserID: owner.ID, Value: &newCookie, Remark: &remark, AutoConfirm: &autoConfirm, PauseDuration: &pause, ChannelIDs: &channels,
	}); err != nil {
		t.Fatal(err)
	}
	detail, _ = store.Cookies.GetDetails(ctx, "settings-cookie")
	bindings, _ := store.Notifications.AccountBindings(ctx, "settings-cookie")
	if detail.Value != newCookie || detail.Remark != remark || detail.AutoConfirm || detail.PauseDuration != pause || detail.PausedUntil == 0 {
		t.Fatalf("aggregate settings not applied: %+v", detail)
	}
	if len(bindings) != 1 || bindings[0] != ownedChannelID {
		t.Fatalf("bindings=%v", bindings)
	}
}
