package db

import (
	"context"
	"errors"
	"testing"
)

// TestLibraryImagesLifecycle 覆盖图片元数据的写入去重、按用户隔离读取和删除。
func TestLibraryImagesLifecycle(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID 是图片所属用户。
	userID, _ := seedAccount(t, store)

	// first、created、err 保存首次写入结果。
	first, created, err := store.LibraryImages.Insert(ctx, LibraryImage{UserID: userID, Filename: "a.png", StorageKey: "1/aa.png", ContentType: "image/png", SizeBytes: 10, SHA256: "aa"})
	if err != nil || !created || first.ID == 0 || first.CreatedAt == "" {
		t.Fatalf("first=%+v created=%v err=%v", first, created, err)
	}
	// dup、dupCreated、dupErr 保存同摘要的重复写入结果，应返回已有记录。
	dup, dupCreated, dupErr := store.LibraryImages.Insert(ctx, LibraryImage{UserID: userID, Filename: "b.png", StorageKey: "1/aa.png", ContentType: "image/png", SizeBytes: 10, SHA256: "aa"})
	if dupErr != nil || dupCreated || dup.ID != first.ID {
		t.Fatalf("dup=%+v created=%v err=%v", dup, dupCreated, dupErr)
	}
	// second 保存不同内容的第二张图片。
	second, _, err := store.LibraryImages.Insert(ctx, LibraryImage{UserID: userID, Filename: "c.png", StorageKey: "1/bb.png", ContentType: "image/png", SizeBytes: 20, SHA256: "bb"})
	if err != nil {
		t.Fatal(err)
	}
	// list 验证列表包含两张图片。
	list, err := store.LibraryImages.List(ctx, userID)
	if err != nil || len(list) != 2 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	// many 验证批量读取忽略他人图片和不存在的主键。
	many, err := store.LibraryImages.GetMany(ctx, userID, []int64{second.ID, first.ID, 9999})
	if err != nil || len(many) != 2 || many[0].ID != second.ID {
		t.Fatalf("many=%+v err=%v", many, err)
	}
	// getErr 保存 Get 调用的失败原因。
	if _, getErr := store.LibraryImages.Get(ctx, userID+1, first.ID); !errors.Is(getErr, ErrNotFound) {
		t.Fatalf("other user get err=%v", getErr)
	}
	// findErr 保存 FindBySHA256 调用的失败原因。
	if _, findErr := store.LibraryImages.FindBySHA256(ctx, userID, "zz"); !errors.Is(findErr, ErrNotFound) {
		t.Fatalf("find missing err=%v", findErr)
	}
	// delErr 保存 Delete 调用的失败原因。
	if delErr := store.LibraryImages.Delete(ctx, userID, first.ID); delErr != nil {
		t.Fatal(delErr)
	}
	// delErr 保存 Delete 调用的失败原因。
	if delErr := store.LibraryImages.Delete(ctx, userID, first.ID); !errors.Is(delErr, ErrNotFound) {
		t.Fatalf("second delete err=%v", delErr)
	}
}

// TestProductTemplatesLifecycle 覆盖模板的创建、默认值补齐、更新、隔离读取和删除。
func TestProductTemplatesLifecycle(t *testing.T) {
	// store、cleanup 提供迁移后的 SQLite 测试数据库。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// userID 是模板所属用户。
	userID, _ := seedAccount(t, store)

	// id、err 保存新建模板主键；JSON 字段留空以验证默认值补齐。
	id, err := store.ProductTemplates.Create(ctx, ProductTemplate{UserID: userID, Name: "模板", Title: "标题", PriceCents: 1000, Quantity: 3})
	if err != nil || id == 0 {
		t.Fatalf("create id=%d err=%v", id, err)
	}
	// got 验证默认 JSON 与邮费模式已补齐。
	got, err := store.ProductTemplates.Get(ctx, userID, id)
	if err != nil || got.SpecsJSON != "{}" || got.ImageIDsJSON != "[]" || got.PostageMode != "free" {
		t.Fatalf("got=%+v err=%v", got, err)
	}
	got.Title = "新标题"
	got.ImageIDsJSON = "[1,2]"
	// updErr 保存 Update 调用的失败原因。
	if updErr := store.ProductTemplates.Update(ctx, *got); updErr != nil {
		t.Fatal(updErr)
	}
	// updated 验证更新已落库。
	updated, err := store.ProductTemplates.Get(ctx, userID, id)
	if err != nil || updated.Title != "新标题" || updated.ImageIDsJSON != "[1,2]" {
		t.Fatalf("updated=%+v err=%v", updated, err)
	}
	// 他人用户读取、更新、删除都应返回 ErrNotFound。
	if _, getErr := store.ProductTemplates.Get(ctx, userID+1, id); !errors.Is(getErr, ErrNotFound) {
		t.Fatalf("other get err=%v", getErr)
	}
	// foreign 构造指向他人模板的更新。
	foreign := *updated
	foreign.UserID = userID + 1
	// updErr 保存 Update 调用的失败原因。
	if updErr := store.ProductTemplates.Update(ctx, foreign); !errors.Is(updErr, ErrNotFound) {
		t.Fatalf("other update err=%v", updErr)
	}
	// list 验证列表仅包含本人模板。
	list, err := store.ProductTemplates.List(ctx, userID)
	if err != nil || len(list) != 1 {
		t.Fatalf("list=%+v err=%v", list, err)
	}
	// delErr 保存 Delete 调用的失败原因。
	if delErr := store.ProductTemplates.Delete(ctx, userID, id); delErr != nil {
		t.Fatal(delErr)
	}
	// delErr 保存 Delete 调用的失败原因。
	if delErr := store.ProductTemplates.Delete(ctx, userID, id); !errors.Is(delErr, ErrNotFound) {
		t.Fatalf("second delete err=%v", delErr)
	}
}
