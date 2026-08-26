package automation

import (
	"context"
	"errors"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// TestAIPricingModeCoversGuardQuoteAndOverflowBranches 验证 AI 报价模式的门禁、空报价、重复领取及金额边界。
func TestAIPricingModeCoversGuardQuoteAndOverflowBranches(t *testing.T) {
	// store、cleanup 保存 AI 报价边界测试数据库及关闭责任。
	store, cleanup := newAutomationTestStore(t)
	defer cleanup()
	// ctx 是 AI 报价模式测试共用的数据库上下文。
	ctx := context.Background()
	// center 保存默认依赖的自动化中心，用于非订单创建事件门禁。
	center := New(store, nil, nil)
	// inactive、inactiveErr 保存非订单创建事件的处理结果。
	inactive, inactiveErr := center.handleAIPricingMode(ctx, Task{TriggerType: TriggerBuyerReviewed})
	if inactive || inactiveErr != nil {
		t.Fatalf("非订单创建事件结果 active=%v err=%v", inactive, inactiveErr)
	}
	// disabledErr 保存未启用 AI 模式时的处理错误。
	if disabledErr := store.AIReply.UpsertSettings(ctx, "cid", db.AIReplySettings{AIEnabled: false, AutoAdjustPriceEnabled: true}); disabledErr != nil {
		t.Fatal(disabledErr)
	}
	// disabled、disabledResultErr 保存关闭 AI 模式的结果。
	disabled, disabledResultErr := center.handleAIPricingMode(ctx, Task{AccountID: "cid", TriggerType: TriggerOrderCreated})
	if disabled || disabledResultErr != nil {
		t.Fatalf("关闭 AI 模式结果 active=%v err=%v", disabled, disabledResultErr)
	}
	// takeoverErr 保存仅由 AI 接管但未打开真实改价时的设置写入错误。
	if takeoverErr := store.AIReply.UpsertSettings(ctx, "cid", db.AIReplySettings{AIEnabled: true, AutoAdjustPriceEnabled: false}); takeoverErr != nil {
		t.Fatal(takeoverErr)
	}
	// takeover、takeoverResultErr 保存 AI 接管结果。
	takeover, takeoverResultErr := center.handleAIPricingMode(ctx, Task{AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "order"})
	if !takeover || takeoverResultErr != nil {
		t.Fatalf("AI 接管结果 active=%v err=%v", takeover, takeoverResultErr)
	}
	// missing、missingErr 保存真实改价开启但事实不完整时的结果。
	if missing, missingErr := center.handleAIPricingMode(ctx, Task{AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "order", ChatID: "chat", BuyerID: "buyer"}); !missing || missingErr != nil {
		t.Fatalf("事实不完整结果 active=%v err=%v", missing, missingErr)
	}
	// enableErr 保存开启真实 AI 改价时的设置写入错误。
	if enableErr := store.AIReply.UpsertSettings(ctx, "cid", db.AIReplySettings{AIEnabled: true, AutoAdjustPriceEnabled: true}); enableErr != nil {
		t.Fatal(enableErr)
	}
	// noQuote、noQuoteErr 保存无可用报价时的结果。
	noQuote, noQuoteErr := center.handleAIPricingMode(ctx, Task{AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "no-quote", ChatID: "chat", BuyerID: "buyer", ItemID: "item"})
	if !noQuote || noQuoteErr != nil {
		t.Fatalf("无报价结果 active=%v err=%v", noQuote, noQuoteErr)
	}
	// duplicateQuote 保存用于验证重复领取保护的待领取报价。
	duplicateQuote := db.AIBargainQuote{CookieID: "cid", ChatID: "duplicate-chat", BuyerID: "buyer", ItemID: "item", PriceCents: 100}
	// replaceErr 保存待领取报价写入错误。
	if replaceErr := store.AIReply.ReplacePendingQuote(ctx, duplicateQuote, time.Now().Add(time.Hour).Unix()); replaceErr != nil {
		t.Fatal(replaceErr)
	}
	// claimedQuote、claimErr 保存第一次手工领取报价的结果。
	claimedQuote, claimErr := store.AIReply.ClaimPendingQuote(ctx, "cid", "duplicate-chat", "buyer", "item", "duplicate-order", time.Now().Unix())
	if claimErr != nil || claimedQuote == nil {
		t.Fatalf("预领取报价失败: quote=%+v err=%v", claimedQuote, claimErr)
	}
	// duplicate、duplicateErr 保存重复订单事件的处理结果。
	duplicate, duplicateErr := center.handleAIPricingMode(ctx, Task{AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "duplicate-order", ChatID: "duplicate-chat", BuyerID: "buyer", ItemID: "item"})
	if !duplicate || duplicateErr != nil {
		t.Fatalf("重复领取结果 active=%v err=%v", duplicate, duplicateErr)
	}
	// overflowQuote 保存数量折算后超过平台金额上限的报价。
	overflowQuote := db.AIBargainQuote{CookieID: "cid", ChatID: "overflow-chat", BuyerID: "buyer", ItemID: "item", PriceCents: 100000000}
	// overflowReplaceErr 保存超额报价写入错误。
	if overflowReplaceErr := store.AIReply.ReplacePendingQuote(ctx, overflowQuote, time.Now().Add(time.Hour).Unix()); overflowReplaceErr != nil {
		t.Fatal(overflowReplaceErr)
	}
	// overflow、overflowErr 保存超额报价的明确未执行结果。
	overflow, overflowErr := center.handleAIPricingMode(ctx, Task{AccountID: "cid", TriggerType: TriggerOrderCreated, OrderID: "overflow-order", ChatID: "overflow-chat", BuyerID: "buyer", ItemID: "item", Quantity: "2"})
	if !overflow || !errors.Is(overflowErr, errActionNotPerformed) {
		t.Fatalf("超额报价结果 active=%v err=%v", overflow, overflowErr)
	}
}
