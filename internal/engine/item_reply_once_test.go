package engine

import (
	"context"
	"errors"
	"testing"
	"xianyu-go/internal/db"
)

// itemOnceDeliveryFunc 把确定性投递回调适配为端口，调用与断言均由测试goroutine串行执行。
type itemOnceDeliveryFunc func(ReplyMessage) (ReplySendResult, error)

// SendReply 将 message 转给测试回调；ctx 无需驱动外部网络，仅保持生产端口签名。
func (f itemOnceDeliveryFunc) SendReply(ctx context.Context, message ReplyMessage) (ReplySendResult, error) {
	return f(message)
}

// TestItemDefaultReplyOnceIsolation 验证启用、禁用、重建运行时、账号兜底及商品会话之间的独立性。
func TestItemDefaultReplyOnceIsolation(t *testing.T) {
	// store、cleanup 拥有隔离SQLite库，所有调用复用同一持久化状态。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 是本地测试上下文；delivery 记录实际发送而非只验证配置解析。
	ctx := context.Background()
	// delivery 不访问真实闲鱼，也不加载图片。
	delivery := &localReplyDelivery{}
	if err := store.DefaultReps.Upsert(ctx, "cid", db.DefaultReply{Enabled: true, ReplyContent: "账号", ReplyOnce: true}); err != nil { // err 是账号兜底配置错误。
		t.Fatal(err)
	}
	// itemID 是不同商品的独立配置，repeat保持历史默认每次回复。
	for _, itemID := range []string{"item-a", "item-b", "repeat"} {
		if err := store.ItemReps.UpdateWithCurrent(ctx, "cid", itemID, func(current db.ItemReply) (db.ItemReply, error) { // current 只修改本商品，不继承账号开关。
			current.ReplyContent = "商品"
			current.ReplyOnce = itemID != "repeat"
			return current, nil
		}); err != nil { // err 是测试商品配置持久化错误。
			t.Fatal(err)
		}
	}
	// service 每次从DB解析并通过真实一次性状态机发送。
	service := NewReplyService("cid", store, delivery, nil, nil, nil)
	// message 是按顺序触发的不同商品和会话。
	for _, message := range []ChatMessage{chatMsg("咨询", "missing", "chat"), chatMsg("咨询", "item-a", "chat"), chatMsg("再次", "item-a", "chat"), chatMsg("咨询", "item-b", "chat"), chatMsg("咨询", "item-a", "other-chat"), chatMsg("咨询", "repeat", "chat"), chatMsg("再次", "repeat", "chat")} {
		if err := service.Handle(ctx, message); err != nil { // err 是测试中的实际回复失败。
			t.Fatal(err)
		}
	}
	if len(delivery.messages) != 6 {
		t.Fatalf("独立作用域发送次数=%d，期望6", len(delivery.messages))
	}
	// restarted 模拟重新装配运行时，去重必须来自数据库而非内存。
	restarted := NewReplyService("cid", store, delivery, nil, nil, nil)
	if err := restarted.Handle(ctx, chatMsg("重启后", "item-a", "chat")); err != nil { // err 是重建运行时读取历史状态失败。
		t.Fatal(err)
	}
	if len(delivery.messages) != 6 {
		t.Fatal("重建运行时重发了商品消息")
	}
	if err := restarted.Handle(ctx, chatMsg("无会话", "item-a", "")); err == nil { // err 必须拒绝无法建立一次性身份的消息。
		t.Fatal("缺失会话仍进行了单次回复")
	}
	if len(delivery.messages) != 6 {
		t.Fatal("缺失会话仍发送了消息")
	}
	// enabled 是关闭后重新开启的商品去重设置；关闭期间允许重复，但不清空旧事实。
	for _, enabled := range []bool{false, true} {
		if err := store.ItemReps.UpdateWithCurrent(ctx, "cid", "item-a", func(current db.ItemReply) (db.ItemReply, error) { // current 保留原图文，只更新去重开关。
			current.ReplyOnce = enabled
			return current, nil
		}); err != nil { // err 是开关更新错误。
			t.Fatal(err)
		}
		if err := restarted.Handle(ctx, chatMsg("开关切换后", "item-a", "chat")); err != nil { // err 是开关切换后处理失败。
			t.Fatal(err)
		}
	}
	if len(delivery.messages) != 7 {
		t.Fatal("关闭应允许发送，重新开启必须仍识别原记录")
	}

}

// TestItemDefaultReplyOncePartialAndUncertain 用真实记录检查文字失败只补文字、未知投递不自动重发。
func TestItemDefaultReplyOncePartialAndUncertain(t *testing.T) {
	// store、cleanup 拥有本地SQLite及同步清理责任。
	store, cleanup := newReplyStore(t)
	defer cleanup()
	// ctx 用于本地数据库和投递替身调用。
	ctx := context.Background()
	if err := store.ItemReps.UpdateWithCurrent(ctx, "cid", "item", func(current db.ItemReply) (db.ItemReply, error) { // current 初始化需要两个分段的商品回复。
		current.ReplyContent = "正文"
		current.ReplyImagePath = "image.png"
		current.ReplyOnce = true
		return current, nil
	}); err != nil { // err 是图文once配置写入错误。
		t.Fatal(err)
	}
	// partial 首次图片成功文字失败，后续只补文字。
	partial := &localReplyDelivery{failTextFirst: true}
	// service 走与账号once相同的状态机，但记录属于商品作用域。
	service := NewReplyService("cid", store, partial, nil, nil, nil)
	if err := service.Handle(ctx, chatMsg("咨询", "item", "partial")); err == nil { // err 必须反馈首次文字失败。
		t.Fatal("首次失败未返回")
	}
	if err := service.Handle(ctx, chatMsg("重试", "item", "partial")); err != nil { // err 是补发文字失败。
		t.Fatal(err)
	}
	if err := service.Handle(ctx, chatMsg("再次", "item", "partial")); err != nil { // err 是读取已完成记录失败。
		t.Fatal(err)
	}
	if len(partial.messages) != 2 || partial.messages[1].ImagePath != "" || partial.messages[1].Text != "正文" {
		t.Fatalf("分段重试错误: %+v", partial.messages)
	}
	// calls 是未知结果场景实际进入投递的次数，测试串行访问无需锁。
	calls := 0
	// uncertain 故意报告可能已投递；错误不含平台或凭据数据。
	uncertain := itemOnceDeliveryFunc(func(message ReplyMessage) (ReplySendResult, error) { // message 保持完整发送意图，未知结果不能推断任何分段成功。
		calls++
		return ReplySendResult{Uncertain: true}, errors.New("结果未知")
	})
	service = NewReplyService("cid", store, uncertain, nil, nil, nil)
	if err := service.Handle(ctx, chatMsg("咨询", "item", "uncertain")); err == nil { // err 必须报告未知结果。
		t.Fatal("未知结果未报告")
	}
	if err := service.Handle(ctx, chatMsg("再次", "item", "uncertain")); err != nil { // err 不应触发第二次外部动作。
		t.Fatal(err)
	}
	if calls != 1 {
		t.Fatalf("未知发送被自动重放%d次", calls)
	}
	// record、err 验证结果已持久化且不是仅靠本次实例记忆。
	record, err := store.DefaultReps.RecordsForItem("item").Record(ctx, "cid", "uncertain")
	if err != nil || record.Status != "uncertain" {
		t.Fatalf("未知结果记录=%+v err=%v", record, err)
	}
}

// TestItemDefaultReplyOnceContentKinds 验证纯文字、URL图片、本地图片及图文均按商品会话只投递一次。
func TestItemDefaultReplyOnceContentKinds(t *testing.T) {
	// config 是独立内容类型，测试不请求URL或读取真实素材。
	for _, config := range []db.ItemReply{{ReplyContent: "文字"}, {ReplyImageURL: "https://example.test/image.png"}, {ReplyImagePath: "local.png"}, {ReplyContent: "说明", ReplyImagePath: "local.png"}} {
		// store、cleanup 为当前内容类型建立独立持久化状态。
		store, cleanup := newReplyStore(t)
		// ctx 仅控制本次本地配置和投递调用。
		ctx := context.Background()
		config.ReplyOnce = true
		if err := store.ItemReps.UpdateWithCurrent(ctx, "cid", "item", func(current db.ItemReply) (db.ItemReply, error) { // current 被当前完整内容夹具替换，不改变仓储限定身份。
			return config, nil
		}); err != nil { // err 是内容类型配置写入错误。
			cleanup()
			t.Fatal(err)
		}
		// delivery、service 是可观察的发送替身和真实一次性状态机。
		delivery := &localReplyDelivery{}
		// service 使用商品级记录而不是临时内存标记。
		service := NewReplyService("cid", store, delivery, nil, nil, nil)
		// attempt 连续触发同一个商品会话两次，应只有首次调用投递。
		for attempt := 0; attempt < 2; attempt++ {
			if err := service.Handle(ctx, chatMsg("咨询", "item", "chat")); err != nil { // err 是当前内容类型实际处理失败。
				cleanup()
				t.Fatal(err)
			}
		}
		if len(delivery.messages) != 1 {
			cleanup()
			t.Fatalf("内容类型重复投递: %+v", config)
		}
		cleanup()
	}
}
