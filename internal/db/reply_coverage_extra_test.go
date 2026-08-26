package db

import (
	"context"
	"errors"
	"testing"
)

// TestDefaultReplyLifecycle 覆盖默认回复配置、投递租约、部分成功和终态收口。
func TestDefaultReplyLifecycle(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID、cookieID 保存默认回复的账号归属。
	userID, cookieID := seedAccount(t, store)

	// missing、missingErr 验证未配置默认回复返回领域未找到错误。
	missing, missingErr := store.DefaultReps.Get(ctx, cookieID)
	if missing != nil || !errors.Is(missingErr, ErrNotFound) {
		t.Fatalf("missing=%+v err=%v", missing, missingErr)
	}
	// reply 保存同时包含文本和图片的默认回复配置。
	reply := DefaultReply{Enabled: true, ReplyContent: "你好", ReplyImageURL: "https://example.test/a.png", ReplyOnce: true}
	// err 表示默认回复配置写入错误。
	if err := store.DefaultReps.Upsert(ctx, cookieID, reply); err != nil {
		t.Fatal(err)
	}
	// loaded、loadedErr 保存读取后的默认回复配置。
	loaded, loadedErr := store.DefaultReps.Get(ctx, cookieID)
	if loadedErr != nil || loaded == nil || !loaded.Enabled || loaded.ReplyContent != "你好" || loaded.ReplyImageURL == "" || !loaded.ReplyOnce {
		t.Fatalf("loaded=%+v err=%v", loaded, loadedErr)
	}
	// listed、listErr 保存按用户聚合的默认回复配置。
	listed, listErr := store.DefaultReps.ListForUser(ctx, userID)
	if listErr != nil || len(listed) != 1 || listed[0].CookieID != cookieID {
		t.Fatalf("listed=%+v err=%v", listed, listErr)
	}
	// emptyImage 验证空图片地址转换为数据库 NULL。
	emptyImage := defaultReplyNullableString("")
	if emptyImage != nil {
		t.Fatalf("empty image=%v", emptyImage)
	}

	// first、firstClaimed、firstErr 保存首次领取结果及其状态。
	first, firstClaimed, firstErr := store.DefaultReps.ClaimRecord(ctx, cookieID, "chat", true, true)
	if firstErr != nil || !firstClaimed || first.Status != "pending" || first.TextSent || first.ImageSent {
		t.Fatalf("first=%+v claimed=%v err=%v", first, firstClaimed, firstErr)
	}
	// duplicate、duplicateClaimed 验证未完成租约阻止并发重复投递。
	duplicate, duplicateClaimed, duplicateErr := store.DefaultReps.ClaimRecord(ctx, cookieID, "chat", true, true)
	if duplicateErr != nil || duplicateClaimed || duplicate.Status != "pending" {
		t.Fatalf("duplicate=%+v claimed=%v err=%v", duplicate, duplicateClaimed, duplicateErr)
	}
	// err 表示未知默认回复部分的校验结果。
	if err := store.DefaultReps.MarkPartSent(ctx, cookieID, "chat", "invalid"); err == nil {
		t.Fatal("invalid reply part should fail")
	}
	// err 表示文本部分成功标记的数据库错误。
	if err := store.DefaultReps.MarkPartSent(ctx, cookieID, "chat", "text"); err != nil {
		t.Fatal(err)
	}
	// err 表示失败状态写入错误。
	if err := store.DefaultReps.MarkRecordFailed(ctx, cookieID, "chat", "图片发送失败"); err != nil {
		t.Fatal(err)
	}
	// retry、retryClaimed、retryErr 验证失败记录可被后续调用重新领取。
	retry, retryClaimed, retryErr := store.DefaultReps.ClaimRecord(ctx, cookieID, "chat", false, true)
	if retryErr != nil || !retryClaimed || retry.Status != "failed" {
		t.Fatalf("retry=%+v claimed=%v err=%v", retry, retryClaimed, retryErr)
	}
	// err 表示图片部分成功标记的数据库错误。
	if err := store.DefaultReps.MarkPartSent(ctx, cookieID, "chat", "image"); err != nil {
		t.Fatal(err)
	}
	// err 表示发送完成状态写入错误。
	if err := store.DefaultReps.MarkRecordSent(ctx, cookieID, "chat"); err != nil {
		t.Fatal(err)
	}
	// record、recordErr 保存已完成投递记录。
	record, recordErr := store.DefaultReps.Record(ctx, cookieID, "chat")
	if recordErr != nil || record.Status != "sent" || !record.TextSent || !record.ImageSent || !store.DefaultReps.HasRecord(ctx, cookieID, "chat") {
		t.Fatalf("record=%+v err=%v", record, recordErr)
	}
	// sentDuplicate、sentClaimed 验证已发送记录继续阻止重复领取。
	sentDuplicate, sentClaimed, sentErr := store.DefaultReps.ClaimRecord(ctx, cookieID, "chat", true, true)
	if sentErr != nil || sentClaimed || sentDuplicate.Status != "sent" {
		t.Fatalf("sent duplicate=%+v claimed=%v err=%v", sentDuplicate, sentClaimed, sentErr)
	}
	// err 表示清空默认回复记录的数据库错误。
	if err := store.DefaultReps.ClearRecords(ctx, cookieID); err != nil {
		t.Fatal(err)
	}
	if store.DefaultReps.HasRecord(ctx, cookieID, "chat") {
		t.Fatal("cleared record should not exist")
	}
	// err 表示兼容旧接口写入已回复记录的数据库错误。
	if err := store.DefaultReps.AddRecord(ctx, cookieID, "legacy-chat"); err != nil {
		t.Fatal(err)
	}
	if !store.DefaultReps.HasRecord(ctx, cookieID, "legacy-chat") {
		t.Fatal("legacy record should exist")
	}
	// err 表示删除默认回复配置的数据库错误。
	if err := store.DefaultReps.Delete(ctx, cookieID); err != nil {
		t.Fatal(err)
	}
	// deleted、deletedErr 验证配置删除后的读取结果。
	deleted, deletedErr := store.DefaultReps.Get(ctx, cookieID)
	if deleted != nil || !errors.Is(deletedErr, ErrNotFound) {
		t.Fatalf("deleted=%+v err=%v", deleted, deletedErr)
	}
}
