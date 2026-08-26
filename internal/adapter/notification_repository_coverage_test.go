package adapter

import (
	"context"
	"testing"
	"time"

	notificationsapp "xianyu-go/internal/application/notifications"
	"xianyu-go/internal/db"
)

// TestNotificationChannelRepositoryCRUDAndBindings 覆盖通知渠道适配器的完整 CRUD 与账号绑定转换路径。
func TestNotificationChannelRepositoryCRUDAndBindings(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// owner、ownerErr 保存测试用户及用户查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// repository 保存通知渠道数据库适配器。
	repository := NewNotificationChannelRepository(store)
	// channelID、createErr 保存渠道创建结果和数据库错误。
	channelID, createErr := repository.CreateChannel(ctx, owner.ID, notificationsapp.ChannelInput{Name: "测试渠道", Type: "webhook", Config: `{}`, EventTypes: "order", Enabled: true})
	if createErr != nil {
		t.Fatal(createErr)
	}
	// owned、ownedErr 保存渠道归属检查结果和数据库错误。
	owned, ownedErr := repository.OwnsChannel(ctx, channelID, owner.ID)
	if ownedErr != nil || !owned {
		t.Fatalf("owned=%v err=%v", owned, ownedErr)
	}
	// accountOwned、accountOwnedErr 保存账号归属检查结果和数据库错误。
	accountOwned, accountOwnedErr := repository.OwnsAccount(ctx, owner.ID, "cid")
	if accountOwnedErr != nil || !accountOwned {
		t.Fatalf("account owned=%v err=%v", accountOwned, accountOwnedErr)
	}
	// summaries、listErr 保存渠道摘要列表和数据库错误。
	summaries, listErr := repository.ListChannels(ctx, owner.ID)
	if listErr != nil || len(summaries) != 1 || summaries[0].ID != channelID {
		t.Fatalf("summaries=%#v err=%v", summaries, listErr)
	}
	// record、getErr 保存渠道完整更新记录和数据库错误。
	record, getErr := repository.GetChannelForUpdate(ctx, channelID, owner.ID)
	if getErr != nil || record == nil || record.Config != `{}` {
		t.Fatalf("record=%#v err=%v", record, getErr)
	}
	// updateErr 保存渠道更新结果。
	if updateErr := repository.UpdateChannel(ctx, owner.ID, notificationsapp.ChannelRecord{ID: channelID, Name: "更新渠道", Type: "webhook", Config: `{"updated":true}`, EventTypes: "message", Enabled: false}); updateErr != nil {
		t.Fatal(updateErr)
	}
	// setErr 保存账号覆盖式绑定结果。
	if setErr := repository.SetBindings(ctx, "cid", []int64{channelID}); setErr != nil {
		t.Fatal(setErr)
	}
	// bindingIDs、bindingIDsErr 保存账号启用渠道列表和数据库错误。
	bindingIDs, bindingIDsErr := repository.GetBindingIDs(ctx, "cid")
	if bindingIDsErr != nil || len(bindingIDs) != 1 || bindingIDs[0] != channelID {
		t.Fatalf("binding IDs=%v err=%v", bindingIDs, bindingIDsErr)
	}
	// bindings、bindingsErr 保存用户绑定摘要列表和数据库错误。
	bindings, bindingsErr := repository.ListBindings(ctx, owner.ID)
	if bindingsErr != nil || len(bindings) != 1 || bindings[0].ChannelID != channelID {
		t.Fatalf("bindings=%#v err=%v", bindings, bindingsErr)
	}
	// singleErr 保存关闭后重新开启单个绑定的数据库错误。
	if singleErr := repository.SetSingleBinding(ctx, "cid", channelID, false); singleErr != nil {
		t.Fatal(singleErr)
	}
	// reenableErr 保存重新开启单个绑定的数据库错误。
	if reenableErr := repository.SetSingleBinding(ctx, "cid", channelID, true); reenableErr != nil {
		t.Fatal(reenableErr)
	}
	// deleteBindingErr 保存删除单个绑定的结果。
	if deleteBindingErr := repository.DeleteBinding(ctx, owner.ID, bindings[0].ID); deleteBindingErr != nil {
		t.Fatal(deleteBindingErr)
	}
	// restoreErr 保存恢复绑定的结果。
	if restoreErr := repository.SetSingleBinding(ctx, "cid", channelID, true); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	// deleteAccountErr 保存删除账号全部绑定的结果。
	if deleteAccountErr := repository.DeleteAccountBindings(ctx, owner.ID, "cid"); deleteAccountErr != nil {
		t.Fatal(deleteAccountErr)
	}
	// deleteErr 保存渠道删除结果。
	if deleteErr := repository.DeleteChannel(ctx, channelID, owner.ID); deleteErr != nil {
		t.Fatal(deleteErr)
	}
}

// TestNotificationUncertainRepositoryMapsSummaries 覆盖不确定通知摘要的用户/管理员查询和模型转换路径。
func TestNotificationUncertainRepositoryMapsSummaries(t *testing.T) {
	// store、cleanup 保存临时数据库及关闭责任。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 限制本测试所有数据库操作的生命周期。
	ctx := context.Background()
	// owner、ownerErr 保存测试用户及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// channelID、channelErr 保存通知渠道及创建错误。
	channelID, channelErr := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{Name: "不确定渠道", Type: "webhook", Config: `{}`, UserID: owner.ID, Enabled: true})
	if channelErr != nil {
		t.Fatal(channelErr)
	}
	// enqueueErr 保存通知出站记录写入错误。
	if enqueueErr := store.Notifications.EnqueueOutbox(ctx, []db.NotificationOutboxInput{{ChannelID: channelID, EventType: "test", Body: "内部正文"}}); enqueueErr != nil {
		t.Fatal(enqueueErr)
	}
	// claimed、claimErr 保存通知租约领取结果和数据库错误。
	claimed, claimErr := store.Notifications.ClaimOutbox(ctx, "uncertain-adapter-worker", time.Now(), 1)
	if claimErr != nil || len(claimed) != 1 {
		t.Fatalf("claimed=%v err=%v", claimed, claimErr)
	}
	// uncertain、uncertainErr 保存通知进入不确定状态的结果和数据库错误。
	uncertain, uncertainErr := store.Notifications.MarkOutboxUncertain(ctx, claimed[0].ID, "uncertain-adapter-worker", "确认失败")
	if uncertainErr != nil || !uncertain {
		t.Fatalf("uncertain=%v err=%v", uncertain, uncertainErr)
	}
	// repository 保存不确定通知摘要适配器。
	repository := NewNotificationUncertainRepository(store)
	// userSummaries、userListErr 保存用户范围摘要及查询错误。
	userSummaries, userListErr := repository.ListUncertainForUser(ctx, owner.ID, 0)
	if userListErr != nil || len(userSummaries) != 1 || userSummaries[0].ChannelID != channelID || !userSummaries[0].HasError {
		t.Fatalf("user summaries=%#v err=%v", userSummaries, userListErr)
	}
	// userCount、userCountErr 保存用户范围不确定通知数量及数据库错误。
	userCount, userCountErr := repository.CountUncertainForUser(ctx, owner.ID)
	if userCountErr != nil || userCount != 1 {
		t.Fatalf("user count=%d err=%v", userCount, userCountErr)
	}
	// adminSummaries、adminListErr 保存管理员范围摘要及查询错误。
	adminSummaries, adminListErr := repository.ListUncertainForAdmin(ctx, 0)
	if adminListErr != nil || len(adminSummaries) != 1 || adminSummaries[0].OwnerUserID != owner.ID {
		t.Fatalf("admin summaries=%#v err=%v", adminSummaries, adminListErr)
	}
	// adminCount、adminCountErr 保存管理员范围不确定通知数量及数据库错误。
	adminCount, adminCountErr := repository.CountUncertainForAdmin(ctx)
	if adminCountErr != nil || adminCount != 1 {
		t.Fatalf("admin count=%d err=%v", adminCount, adminCountErr)
	}
}
