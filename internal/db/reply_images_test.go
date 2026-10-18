package db

import (
	"context"
	"errors"
	"testing"
)

// TestReplyImageConfigurationRoundTrip 验证账号和商品本地图持久化、旧商品文字更新保留图片，以及失败回调不写入。
func TestReplyImageConfigurationRoundTrip(t *testing.T) {
	// store、cleanup 拥有隔离 SQLite 数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// cookieID 是拥有本次所有配置的账号。
	_, cookieID := seedAccount(t, store)
	// ctx 用于本地确定性数据库操作。
	ctx := context.Background()
	if err := store.DefaultReps.Upsert(ctx, cookieID, DefaultReply{Enabled: true, ReplyContent: "账号", ReplyImagePath: "默认.png", ReplyOnce: true}); err != nil { // err 是配置创建错误。
		t.Fatal(err)
	}
	if err := store.DefaultReps.UpdateWithCurrent(ctx, cookieID, func(current DefaultReply) (DefaultReply, error) { // current 是受账号行锁保护的最新配置。
		current.ReplyContent = "更新账号"
		return current, nil
	}); err != nil { // err 是仅改正文的事务执行错误。
		t.Fatal(err)
	}
	// reply、readErr 用于断言本地图与一次性语义均被保留。
	reply, readErr := store.DefaultReps.Get(ctx, cookieID)
	if readErr != nil || reply.ReplyImagePath != "默认.png" || !reply.ReplyOnce || reply.ReplyContent != "更新账号" {
		t.Fatalf("账号配置=%+v err=%v", reply, readErr)
	}
	if err := store.ItemReps.UpdateWithCurrent(ctx, cookieID, "item-1", func(current ItemReply) (ItemReply, error) { // current 允许缺失配置，以零值创建商品覆盖。
		current.ReplyContent, current.ReplyImagePath = "商品一", "商品/详情.png"
		return current, nil
	}); err != nil { // err 是商品图文创建错误。
		t.Fatal(err)
	}
	if err := store.ItemReps.Set(ctx, cookieID, "item-1", "旧客户端更新"); err != nil { // err 是旧方法更新正文的结果。
		t.Fatal(err)
	}
	// item、itemErr 验证旧入口没有擦除新增图片字段。
	item, itemErr := store.ItemReps.Get(ctx, cookieID, "item-1")
	if itemErr != nil || item.ReplyContent != "旧客户端更新" || item.ReplyImagePath != "商品/详情.png" {
		t.Fatalf("商品配置=%+v err=%v", item, itemErr)
	}
	// rejected 模拟应用在事务内检测到来源冲突。
	rejected := errors.New("图片来源冲突")
	if err := store.ItemReps.UpdateWithCurrent(ctx, cookieID, "item-1", func(current ItemReply) (ItemReply, error) { // current 不得在返回错误时落库。
		current.ReplyContent = "不能写入"
		return current, rejected
	}); !errors.Is(err, rejected) { // err 必须保留应用的错误分类。
		t.Fatalf("错误未透传: %v", err)
	}
	// rows、listErr 验证列表含图片字段且失败更新未覆盖原值。
	rows, listErr := store.ItemReps.AllForUser(ctx, cookieID)
	if listErr != nil || len(rows) != 1 || rows[0].ReplyImagePath != "商品/详情.png" || rows[0].ReplyContent != "旧客户端更新" {
		t.Fatalf("列表=%+v err=%v", rows, listErr)
	}
}
