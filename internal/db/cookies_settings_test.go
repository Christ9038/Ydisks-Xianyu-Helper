package db

import (
	"context"
	"errors"
	"testing"
)

// TestUpdateAccountSettingsIsAtomic 封装TestUpdate账号设置IsAtomic业务协调。
func TestUpdateAccountSettingsIsAtomic(t *testing.T) {
	// store、cleanup 用于本次流程后续判断的store、cleanup
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// user 表示当前遍历过程中的用户
	for _, user := range []struct{ name, email string }{{"settings-owner", "settings-owner@example.com"}, {"settings-other", "settings-other@example.com"}} {
		if // ok、err 用于本次流程后续判断的ok、err
		ok, err := store.Users.Create(ctx, user.name, user.email, "pw"); err != nil || !ok {
			t.Fatalf("create %s: ok=%v err=%v", user.name, ok, err)
		}
	}
	// owner 用于本次流程后续判断的所有者
	owner, _ := store.Users.GetByUsername(ctx, "settings-owner")
	// other 用于本次流程后续判断的other
	other, _ := store.Users.GetByUsername(ctx, "settings-other")
	if // err 用于本次流程后续判断的err
	err := store.Cookies.CreateOwned(ctx, "settings-cookie", "old-cookie", owner.ID); err != nil {
		t.Fatal(err)
	}
	// channelResult、err 用于本次流程后续判断的渠道Result、err
	channelResult, err := store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,1,?)`, "other", "webhook", `{}`, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	// otherChannelID 用于本次流程后续判断的other渠道ID
	otherChannelID, _ := channelResult.LastInsertId()
	// newCookie、remark 用于本次流程后续判断的newCookie、remark
	newCookie, remark := "new-cookie", "new remark"
	// autoConfirm 用于本次流程后续判断的autoConfirm
	autoConfirm := false
	// badChannels 用于本次流程后续判断的bad渠道列表
	badChannels := []int64{otherChannelID}
	_, err = store.Cookies.UpdateSettings(ctx, "settings-cookie", AccountSettingsUpdate{
		UserID: owner.ID, Value: &newCookie, Remark: &remark, AutoConfirm: &autoConfirm, ChannelIDs: &badChannels,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden channel, got %v", err)
	}
	// detail 用于本次流程后续判断的detail
	detail, _ := store.Cookies.GetDetails(ctx, "settings-cookie")
	if detail.Value != "old-cookie" || detail.Remark != "" || !detail.AutoConfirm {
		t.Fatalf("failed aggregate update partially committed: %+v", detail)
	}

	channelResult, err = store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,1,?)`, "owned", "webhook", `{}`, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	// ownedChannelID 用于本次流程后续判断的owned渠道ID
	ownedChannelID, _ := channelResult.LastInsertId()
	// channels 用于本次流程后续判断的渠道列表
	channels := []int64{ownedChannelID, ownedChannelID}
	// pause 用于本次流程后续判断的pause
	pause := 5
	if // err 用于本次流程后续判断的err
	_, err := store.Cookies.UpdateSettings(ctx, "settings-cookie", AccountSettingsUpdate{
		UserID: owner.ID, Value: &newCookie, Remark: &remark, AutoConfirm: &autoConfirm, PauseDuration: &pause, ChannelIDs: &channels,
	}); err != nil {
		t.Fatal(err)
	}
	detail, _ = store.Cookies.GetDetails(ctx, "settings-cookie")
	// bindings 用于本次流程后续判断的bindings
	bindings, _ := store.Notifications.AccountBindings(ctx, "settings-cookie")
	if detail.Value != newCookie || detail.Remark != remark || detail.AutoConfirm || detail.PauseDuration != pause || detail.PausedUntil == 0 {
		t.Fatalf("aggregate settings not applied: %+v", detail)
	}
	if len(bindings) != 1 || bindings[0] != ownedChannelID {
		t.Fatalf("bindings=%v", bindings)
	}
}
