package db

import (
	"context"
	"testing"
)

// TestUpdateAccountSettingsCoversValidationAndOptionalFields 验证账号设置更新的暂停校验、渠道数量上限和可选敏感字段写入。
func TestUpdateAccountSettingsCoversValidationAndOptionalFields(t *testing.T) {
	// store、cleanup 保存本地账号设置测试数据库及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试账号设置操作共用的非取消上下文。
	ctx := context.Background()
	// createOK、createErr 保存测试用户创建结果。
	createOK, createErr := store.Users.Create(ctx, "settings-more", "settings-more@example.com", "pw")
	if createErr != nil || !createOK {
		t.Fatalf("创建测试用户失败：ok=%v err=%v", createOK, createErr)
	}
	// owner、ownerErr 保存测试用户及查询错误。
	owner, ownerErr := store.Users.GetByUsername(ctx, "settings-more")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// cookieErr 保存测试账号创建错误。
	if cookieErr := store.Cookies.CreateOwned(ctx, "settings-more-cookie", "sid=old", owner.ID); cookieErr != nil {
		t.Fatal(cookieErr)
	}
	// negativePause 保存负暂停时长的校验值。
	negativePause := -1
	// _, negativeErr 保存负暂停时长返回的校验错误。
	_, negativeErr := store.Cookies.UpdateSettings(ctx, "settings-more-cookie", AccountSettingsUpdate{UserID: owner.ID, PauseDuration: &negativePause})
	if negativeErr == nil {
		t.Fatal("负暂停时长应被拒绝")
	}
	// excessivePause 保存超过上限的暂停时长。
	excessivePause := 1441
	// _, excessiveErr 保存超过上限的校验错误。
	_, excessiveErr := store.Cookies.UpdateSettings(ctx, "settings-more-cookie", AccountSettingsUpdate{UserID: owner.ID, PauseDuration: &excessivePause})
	if excessiveErr == nil {
		t.Fatal("超过上限的暂停时长应被拒绝")
	}
	// tooManyChannels 保存超过账号绑定上限的渠道 ID 列表。
	tooManyChannels := make([]int64, 101)
	// _, channelLimitErr 保存渠道数量上限校验错误。
	_, channelLimitErr := store.Cookies.UpdateSettings(ctx, "settings-more-cookie", AccountSettingsUpdate{UserID: owner.ID, ChannelIDs: &tooManyChannels})
	if channelLimitErr == nil {
		t.Fatal("超过上限的渠道列表应被拒绝")
	}
	// username、password、showBrowser、resumePause 保存本次可选字段更新值。
	username, password, showBrowser, resumePause := "login-user", "login-password", true, 0
	// noChannels 明确清空账号的通知绑定。
	noChannels := []int64{}
	// _, updateErr 保存可选字段更新错误。
	_, updateErr := store.Cookies.UpdateSettings(ctx, "settings-more-cookie", AccountSettingsUpdate{
		UserID: owner.ID, Username: &username, Password: &password, ShowBrowser: &showBrowser,
		PauseDuration: &resumePause, ChannelIDs: &noChannels,
	})
	if updateErr != nil {
		t.Fatalf("可选账号字段更新失败：%v", updateErr)
	}
	// detail、detailErr 保存更新后的非敏感账号详情。
	detail, detailErr := store.Cookies.GetDetails(ctx, "settings-more-cookie")
	if detailErr != nil || detail.Username != username || !detail.ShowBrowser || detail.PauseDuration != 0 {
		t.Fatalf("可选字段未正确写入：detail=%+v err=%v", detail, detailErr)
	}
}
