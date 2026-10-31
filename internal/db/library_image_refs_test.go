package db

import (
	"context"
	"testing"

	"xianyu-go/internal/deliverytemplate"
)

// TestLibraryImageReferencesRoundTripAndUsages 验证各图片位置写入、读回图片库引用，并被 Usages 全部发现。
func TestLibraryImageReferencesRoundTripAndUsages(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 是图片和账号的共同所有者。
	userID, cookieID := seedAccount(t, store)

	// keywordID、err 保存带图片库图片的关键词写入结果。
	keywordID, err := store.Keywords.AddRow(ctx, KeywordRow{CookieID: cookieID, Expressions: []string{"图"}, Type: "image", LibraryImageID: 11})
	if err != nil || keywordID == 0 {
		t.Fatalf("关键词写入失败 id=%d err=%v", keywordID, err)
	}
	// keywordRows 读回关键词行，验证引用字段持久化。
	keywordRows, err := store.Keywords.AllRows(ctx, cookieID)
	if err != nil || len(keywordRows) != 1 || keywordRows[0].LibraryImageID != 11 {
		t.Fatalf("关键词读回异常 rows=%+v err=%v", keywordRows, err)
	}
	// typed 读回引擎使用的关键词视图。
	typed, err := store.Keywords.AllWithType(ctx, cookieID)
	if err != nil || len(typed) != 1 || typed[0].LibraryImageID != 11 {
		t.Fatalf("引擎关键词视图异常 rows=%+v err=%v", typed, err)
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if err := store.DefaultReps.Upsert(ctx, cookieID, DefaultReply{Enabled: true, ReplyLibraryImageID: 12}); err != nil {
		t.Fatal(err)
	}
	// reply 读回默认回复。
	reply, err := store.DefaultReps.Get(ctx, cookieID)
	if err != nil || reply.ReplyLibraryImageID != 12 {
		t.Fatalf("默认回复读回异常 reply=%+v err=%v", reply, err)
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if _, err := store.ItemGreetings.Create(ctx, ItemGreetingRule{CookieID: cookieID, Name: "首", Enabled: true, ReplyLibraryImageID: 13, ItemIDs: []string{"item-1"}}); err != nil {
		t.Fatal(err)
	}
	// greeting 读回命中商品的首次回复规则。
	greeting, err := store.ItemGreetings.FindEnabledForItem(ctx, cookieID, "item-1")
	if err != nil || greeting.ReplyLibraryImageID != 13 {
		t.Fatalf("首次回复读回异常 rule=%+v err=%v", greeting, err)
	}
	// cardID、err 保存图片卡密写入结果。
	cardID, err := store.Cards.Create(ctx, &CardFull{Name: "图卡", Type: "image", LibraryImageID: 14, Enabled: true, UserID: userID})
	if err != nil {
		t.Fatal(err)
	}
	// card 读回图片卡密。
	card, err := store.Cards.Get(ctx, cardID)
	if err != nil || card.LibraryImageID != 14 {
		t.Fatalf("卡密读回异常 card=%+v err=%v", card, err)
	}
	// quick、err 保存图片快捷回复写入结果。
	quick, err := store.Chats.CreateQuickReplyWithImage(ctx, cookieID, "", 15)
	if err != nil || quick.LibraryImageID != 15 {
		t.Fatalf("快捷回复写入异常 reply=%+v err=%v", quick, err)
	}
	// quickList 读回快捷回复列表。
	quickList, err := store.Chats.ListQuickReplies(ctx, cookieID)
	if err != nil || len(quickList) != 1 || quickList[0].LibraryImageID != 15 {
		t.Fatalf("快捷回复读回异常 list=%+v err=%v", quickList, err)
	}
	// templateID、err 保存含图片消息的发货模板；软删除模板的引用不应计入。
	templateID, err := store.DeliveryTemplates.Create(ctx, DeliveryTemplateInput{UserID: userID, Name: "模", Enabled: true, Messages: []string{"文字", deliverytemplate.ImageToken(16)}})
	if err != nil {
		t.Fatal(err)
	}
	// usages 读取全部引用。
	usages, err := store.LibraryImages.Usages(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	// found 按图片主键保存引用说明。
	found := map[int64]string{}
	// usage 是图片库引用查询返回的一条引用记录。
	for _, usage := range usages {
		found[usage.ImageID] = usage.Label
	}
	// want 是期望出现的引用来源及其标签。
	for _, want := range []int64{11, 12, 13, 14, 15, 16} {
		if found[want] == "" {
			t.Fatalf("图片 %d 的引用未被发现: %+v", want, usages)
		}
	}
	// err 保存被测调用的失败原因，断言时按预期分支检查。
	if err := store.DeliveryTemplates.Delete(ctx, userID, templateID); err != nil {
		t.Fatal(err)
	}
	// afterDelete 读取模板删除后的引用。
	afterDelete, err := store.LibraryImages.Usages(ctx, userID)
	if err != nil {
		t.Fatal(err)
	}
	// usage 是图片库引用查询返回的一条引用记录。
	for _, usage := range afterDelete {
		if usage.ImageID == 16 {
			t.Fatalf("已删除模板不应继续占用图片: %+v", usage)
		}
	}
	// otherUsages 验证其他用户看不到引用。
	otherUsages, err := store.LibraryImages.Usages(ctx, userID+1)
	if err != nil || len(otherUsages) != 0 {
		t.Fatalf("其他用户不应看到引用 usages=%+v err=%v", otherUsages, err)
	}
}
