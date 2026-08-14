package db

import (
	"context"
	"errors"
	"testing"
)

// TestUpdateAccountSettingsIsAtomic 负责TestUpdate账号设置IsAtomic相关处理。
func TestUpdateAccountSettingsIsAtomic(t *testing.T) {
	// store、cleanup 保存store、cleanup，供当前处理流程使用
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 保存ctx，供当前处理流程使用
	ctx := context.Background()
	// user 表示当前遍历过程中的用户
	for _, user := range []struct{ name, email string }{{"settings-owner", "settings-owner@example.com"}, {"settings-other", "settings-other@example.com"}} {
		if // ok、err 保存ok、err，供当前处理流程使用
		ok, err := store.Users.Create(ctx, user.name, user.email, "pw"); err != nil || !ok {
			t.Fatalf("create %s: ok=%v err=%v", user.name, ok, err)
		}
	}
	// owner 保存所有者，供当前处理流程使用
	owner, _ := store.Users.GetByUsername(ctx, "settings-owner")
	// other 保存other，供当前处理流程使用
	other, _ := store.Users.GetByUsername(ctx, "settings-other")
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.CreateOwned(ctx, "settings-cookie", "old-cookie", owner.ID); err != nil {
		t.Fatal(err)
	}
	// channelResult、err 保存渠道Result、err，供当前处理流程使用
	channelResult, err := store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,1,?)`, "other", "webhook", `{}`, other.ID)
	if err != nil {
		t.Fatal(err)
	}
	// otherChannelID 保存other渠道ID，供当前处理流程使用
	otherChannelID, _ := channelResult.LastInsertId()
	// newCookie、remark 保存newCookie、remark，供当前处理流程使用
	newCookie, remark := "new-cookie", "new remark"
	// autoConfirm 保存autoConfirm，供当前处理流程使用
	autoConfirm := false
	// badChannels 保存bad渠道列表，供当前处理流程使用
	badChannels := []int64{otherChannelID}
	_, err = store.Cookies.UpdateSettings(ctx, "settings-cookie", AccountSettingsUpdate{
		UserID: owner.ID, Value: &newCookie, Remark: &remark, AutoConfirm: &autoConfirm, ChannelIDs: &badChannels,
	})
	if !errors.Is(err, ErrForbidden) {
		t.Fatalf("expected forbidden channel, got %v", err)
	}
	// detail 保存detail，供当前处理流程使用
	detail, _ := store.Cookies.GetDetails(ctx, "settings-cookie")
	if detail.Value != "old-cookie" || detail.Remark != "" || !detail.AutoConfirm {
		t.Fatalf("failed aggregate update partially committed: %+v", detail)
	}

	channelResult, err = store.DB.ExecContext(ctx, `INSERT INTO notification_channels (name,type,config,enabled,user_id) VALUES (?,?,?,1,?)`, "owned", "webhook", `{}`, owner.ID)
	if err != nil {
		t.Fatal(err)
	}
	// ownedChannelID 保存owned渠道ID，供当前处理流程使用
	ownedChannelID, _ := channelResult.LastInsertId()
	// channels 保存渠道列表，供当前处理流程使用
	channels := []int64{ownedChannelID, ownedChannelID}
	// pause 保存pause，供当前处理流程使用
	pause := 5
	if // err 保存err，供当前处理流程使用
	_, err := store.Cookies.UpdateSettings(ctx, "settings-cookie", AccountSettingsUpdate{
		UserID: owner.ID, Value: &newCookie, Remark: &remark, AutoConfirm: &autoConfirm, PauseDuration: &pause, ChannelIDs: &channels,
	}); err != nil {
		t.Fatal(err)
	}
	detail, _ = store.Cookies.GetDetails(ctx, "settings-cookie")
	// bindings 保存bindings，供当前处理流程使用
	bindings, _ := store.Notifications.AccountBindings(ctx, "settings-cookie")
	if detail.Value != newCookie || detail.Remark != remark || detail.AutoConfirm || detail.PauseDuration != pause || detail.PausedUntil == 0 {
		t.Fatalf("aggregate settings not applied: %+v", detail)
	}
	if len(bindings) != 1 || bindings[0] != ownedChannelID {
		t.Fatalf("bindings=%v", bindings)
	}
}
