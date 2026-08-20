package db

import (
	"context"
	"testing"
)

// TestAIBargainQuoteLifecycle 验证最新报价替换、四维匹配、订单防重和终态收口。
func TestAIBargainQuoteLifecycle(t *testing.T) {
	// store、cleanup 是迁移到最新结构的测试仓储及清理函数。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是报价生命周期测试上下文。
	ctx := context.Background()
	// err 是创建报价账号所有者时不应出现的错误。
	if _, err := store.Users.Create(ctx, "quote-admin", "quote@example.com", "pw"); err != nil {
		t.Fatal(err)
	}
	// owner 是报价账号的本地所有者。
	owner, err := store.Users.GetByUsername(ctx, "quote-admin")
	if err != nil {
		t.Fatal(err)
	}
	if err = store.Cookies.Save(ctx, "quote-account", "unb=1", owner.ID); err != nil {
		t.Fatal(err)
	}
	// first 是同一买家和商品先收到、随后应被替换的旧报价。
	first := AIBargainQuote{CookieID: "quote-account", ChatID: "chat-1", BuyerID: "buyer-1", ItemID: "item-1", PriceCents: 9500}
	// latest 是应保持有效并被订单领取的最新报价。
	latest := AIBargainQuote{CookieID: "quote-account", ChatID: "chat-1", BuyerID: "buyer-1", ItemID: "item-1", PriceCents: 9000}
	if err = store.AIReply.ReplacePendingQuote(ctx, first, 2000); err != nil {
		t.Fatal(err)
	}
	if err = store.AIReply.ReplacePendingQuote(ctx, latest, 2000); err != nil {
		t.Fatal(err)
	}
	// mismatch 是错误会话的领取结果，必须为空且不能消费正确报价。
	mismatch, err := store.AIReply.ClaimPendingQuote(ctx, "quote-account", "chat-other", "buyer-1", "item-1", "order-1", 1000)
	if err != nil || mismatch != nil {
		t.Fatalf("错误会话不应领取报价: quote=%+v err=%v", mismatch, err)
	}
	// claimed 是四维匹配后领取到的最新报价。
	claimed, err := store.AIReply.ClaimPendingQuote(ctx, "quote-account", "chat-1", "buyer-1", "item-1", "order-1", 1000)
	if err != nil || claimed == nil || claimed.PriceCents != 9000 {
		t.Fatalf("领取最新报价失败: quote=%+v err=%v", claimed, err)
	}
	// duplicate 是同一订单重复事件的领取结果，必须幂等为空。
	duplicate, err := store.AIReply.ClaimPendingQuote(ctx, "quote-account", "chat-1", "buyer-1", "item-1", "order-1", 1000)
	if err != nil || duplicate != nil {
		t.Fatalf("重复订单不应再次领取: quote=%+v err=%v", duplicate, err)
	}
	if err = store.AIReply.FinishQuote(ctx, claimed.ID, "adjusted", ""); err != nil {
		t.Fatal(err)
	}
}
