package adapter

import (
	"context"
	"errors"
	"testing"

	keywordsapp "xianyu-go/internal/application/keywords"
)

// TestKeywordRepositoryCRUDMapping 验证关键词和指定商品回复的 SQLite 映射及成功路径。
func TestKeywordRepositoryCRUDMapping(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是待验证的关键词数据库适配器。
	repository := NewKeywordRepository(store)
	// ctx 是本测试数据库操作共用的非取消上下文。
	ctx := context.Background()
	// owner 是测试模板中的关键词所有者。
	owner, ownerErr := store.Users.GetByUsername(ctx, "admin")
	if ownerErr != nil {
		t.Fatal(ownerErr)
	}
	// keywordID、createErr 保存关键词创建结果。
	keywordID, createErr := repository.Add(ctx, owner.ID, "cid", keywordsapp.Draft{Keyword: "价格", Reply: "50元", Type: "text", ItemID: "item-1"})
	if createErr != nil || keywordID <= 0 {
		t.Fatalf("创建关键词失败 id=%d err=%v", keywordID, createErr)
	}
	// rows、listErr 保存关键词列表结果。
	rows, listErr := repository.List(ctx, owner.ID, "cid")
	if listErr != nil || len(rows) != 1 || rows[0].Keyword != "价格" {
		t.Fatalf("关键词列表异常 rows=%+v err=%v", rows, listErr)
	}
	// updateErr 保存关键词更新结果。
	updateErr := repository.Update(ctx, owner.ID, "cid", keywordID, keywordsapp.Draft{Keyword: "新价格", Reply: "60元", Type: "text"})
	if updateErr != nil {
		t.Fatal(updateErr)
	}
	// replaceErr 保存批量替换结果。
	replaceErr := repository.Replace(ctx, owner.ID, "cid", []keywordsapp.Draft{{Keyword: "图片", Type: "image", ImageURL: "https://example.invalid/a.png"}})
	if replaceErr != nil {
		t.Fatal(replaceErr)
	}
	// replacedRows、replacedErr 保存批量替换后的列表结果。
	replacedRows, replacedErr := repository.List(ctx, owner.ID, "cid")
	if replacedErr != nil || len(replacedRows) != 1 || replacedRows[0].Type != "image" {
		t.Fatalf("替换结果异常 rows=%+v err=%v", replacedRows, replacedErr)
	}
	// setErr 保存指定商品回复写入结果。
	if setErr := repository.SetItemReply(ctx, owner.ID, "cid", "item-1", "专属回复"); setErr != nil {
		t.Fatal(setErr)
	}
	// itemReply、getErr 保存指定商品回复读取结果。
	itemReply, getErr := repository.GetItemReply(ctx, owner.ID, "cid", "item-1")
	if getErr != nil || itemReply.ReplyContent != "专属回复" {
		t.Fatalf("商品回复读取异常 row=%+v err=%v", itemReply, getErr)
	}
	// itemRows、itemListErr 保存指定商品回复列表结果。
	itemRows, itemListErr := repository.ListItemReplies(ctx, owner.ID)
	if itemListErr != nil || len(itemRows) != 1 || itemRows[0].CookieID != "cid" {
		t.Fatalf("商品回复列表异常 rows=%+v err=%v", itemRows, itemListErr)
	}
	// deleteReplyErr 保存指定商品回复删除结果。
	if deleteReplyErr := repository.DeleteItemReply(ctx, owner.ID, "cid", "item-1"); deleteReplyErr != nil {
		t.Fatal(deleteReplyErr)
	}
	// deleteErr 保存关键词 ID 删除结果。
	if deleteErr := repository.DeleteByID(ctx, owner.ID, "cid", keywordID); deleteErr != nil && !errors.Is(deleteErr, keywordsapp.ErrNotFound) {
		t.Fatal(deleteErr)
	}
}

// TestKeywordRepositoryRejectsCrossUserAccess 验证跨用户账号无法读写关键词或商品回复。
func TestKeywordRepositoryRejectsCrossUserAccess(t *testing.T) {
	// store 是使用临时 SQLite 数据库的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// ctx 是本测试数据库操作共用的非取消上下文。
	ctx := context.Background()
	// created、createErr 保存第二个用户的创建结果。
	created, createErr := store.Users.Create(ctx, "other", "other@example.invalid", "pw")
	if createErr != nil || !created {
		t.Fatal(createErr)
	}
	// other 是第二个用户的非敏感身份摘要。
	other, otherErr := store.Users.GetByUsername(ctx, "other")
	if otherErr != nil {
		t.Fatal(otherErr)
	}
	// saveErr 保存第二个用户账号的创建结果。
	if saveErr := store.Cookies.Save(ctx, "other-cid", "cookie", other.ID); saveErr != nil {
		t.Fatal(saveErr)
	}
	// repository 是待验证的关键词数据库适配器。
	repository := NewKeywordRepository(store)
	// err 保存跨用户读取结果。
	_, err := repository.List(ctx, 1, "other-cid")
	if !errors.Is(err, keywordsapp.ErrForbidden) {
		t.Fatalf("跨用户读取应返回 ErrForbidden，err=%v", err)
	}
	// writeErr 保存跨用户写入结果。
	writeErr := repository.SetItemReply(ctx, 1, "other-cid", "item", "reply")
	if !errors.Is(writeErr, keywordsapp.ErrForbidden) {
		t.Fatalf("跨用户写入应返回 ErrForbidden，err=%v", writeErr)
	}
}

// TestKeywordRepositoryPropagatesInfrastructureErrors 验证数据库故障不伪装为资源不存在。
func TestKeywordRepositoryPropagatesInfrastructureErrors(t *testing.T) {
	// store 是随后主动关闭数据库连接的测试存储。
	store, cleanup := newAdapterTestStore(t)
	defer cleanup()
	// repository 是绑定测试存储的关键词适配器。
	repository := NewKeywordRepository(store)
	// closeErr 保存关闭测试数据库的结果。
	if closeErr := store.DB.Close(); closeErr != nil {
		t.Fatal(closeErr)
	}
	// err 保存数据库关闭后的列表结果。
	_, err := repository.List(context.Background(), 1, "cid")
	if err == nil || errors.Is(err, keywordsapp.ErrNotFound) || errors.Is(err, keywordsapp.ErrForbidden) {
		t.Fatalf("数据库故障应原样返回，err=%v", err)
	}
	// missingErr 保存缺少数据库适配器依赖时的装配错误。
	_, missingErr := NewKeywordRepository(nil).List(context.Background(), 1, "cid")
	if missingErr == nil {
		t.Fatal("缺少 Store 时应返回装配错误")
	}
}

var _ keywordsapp.Repository = (*KeywordRepository)(nil)
