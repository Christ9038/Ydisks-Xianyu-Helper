package db

import (
	"context"
	"fmt"
	"sync/atomic"
	"testing"
)

// newOrderContextAccount 在目标数据库中创建一个用户和一个账号，返回账号标识与所属用户主键；名称带唯一后缀以便外部数据库重复执行。
func newOrderContextAccount(t *testing.T, target testTarget, label string) (string, int64) {
	t.Helper()
	// ctx 是创建用户与账号共用的测试上下文。
	ctx := context.Background()
	// suffix 保证外部数据库重复执行时业务标识唯一。
	suffix := fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
	// username 是测试用户名称；cookieID 是测试账号标识。
	username, cookieID := target.name+"_octx_"+label+"_"+suffix, target.name+"_octx_cookie_"+label+"_"+suffix
	// createErr 保存创建测试用户的错误。
	if _, createErr := target.store.Users.Create(ctx, username, username+"@e.com", "pw"); createErr != nil {
		t.Fatalf("create user: %v", createErr)
	}
	// user、userErr 保存账号外键引用的测试用户及读取错误。
	user, userErr := target.store.Users.GetByUsername(ctx, username)
	if userErr != nil {
		t.Fatal(userErr)
	}
	// saveErr 保存创建测试账号的错误。
	if saveErr := target.store.Cookies.Save(ctx, cookieID, "cookie-value", user.ID); saveErr != nil {
		t.Fatal(saveErr)
	}
	return cookieID, user.ID
}

// TestMultiDB_RecentStageFactsForBuyer 验证订单阶段窄查询的买家匹配、会话兜底、商品与账号隔离、软删除排除、排序和条数上限。
func TestMultiDB_RecentStageFactsForBuyer(t *testing.T) {
	// target 表示当前执行查询回归的数据库方言目标。
	for _, target := range allTestTargets(t) {
		// target 是当前子测试独占的数据库目标副本，避免循环变量复用。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 是当前方言内全部订单操作共用的测试上下文。
			ctx := context.Background()
			// account 是被查询的账号。
			account, _ := newOrderContextAccount(t, target, "a")
			// other 是用于验证跨账号隔离的另一个账号。
			other, _ := newOrderContextAccount(t, target, "b")
			// prefix 保证订单号在外部数据库重复执行时唯一。
			prefix := fmt.Sprintf("%s_octx_order_%d_", target.name, atomic.AddUint64(&multidbCounter, 1))
			// seed 写入一笔订单；失败时立即终止当前子测试。
			seed := func(id string, options OrderUpsertOpts) {
				t.Helper()
				// upsertErr 保存订单写入错误。
				if upsertErr := target.store.Orders.Upsert(ctx, prefix+id, options); upsertErr != nil {
					t.Fatalf("upsert %s: %v", id, upsertErr)
				}
			}
			seed("old", OrderUpsertOpts{CookieID: account, ItemID: "item-1", BuyerID: "buyer-1", ChatID: "chat-1", OrderStatus: "completed", CreatedAt: "2026-01-01 00:00:00"})
			seed("suffix", OrderUpsertOpts{CookieID: account, ItemID: "item-1", BuyerID: "buyer-1@goofish", ChatID: "chat-1", OrderStatus: "pending_ship", Quantity: "2", SpecName: "颜色", SpecValue: "红", CreatedAt: "2026-01-02 00:00:00"})
			seed("chat-only", OrderUpsertOpts{CookieID: account, ItemID: "item-1", ChatID: "chat-1", CreatedAt: "2026-01-03 00:00:00"})
			seed("newest", OrderUpsertOpts{CookieID: account, ItemID: "item-1", BuyerID: "buyer-1", CreatedAt: "2026-01-04 00:00:00"})
			seed("other-item", OrderUpsertOpts{CookieID: account, ItemID: "item-2", BuyerID: "buyer-1", ChatID: "chat-1", CreatedAt: "2026-01-05 00:00:00"})
			seed("other-buyer", OrderUpsertOpts{CookieID: account, ItemID: "item-1", BuyerID: "buyer-2", ChatID: "chat-1", CreatedAt: "2026-01-06 00:00:00"})
			seed("other-account", OrderUpsertOpts{CookieID: other, ItemID: "item-1", BuyerID: "buyer-1", ChatID: "chat-1", CreatedAt: "2026-01-07 00:00:00"})
			seed("deleted", OrderUpsertOpts{CookieID: account, ItemID: "item-1", BuyerID: "buyer-1", ChatID: "chat-1", CreatedAt: "2026-01-08 00:00:00"})
			// deleteErr 保存软删除订单的错误；被删除订单不得出现在阶段事实中。
			if _, deleteErr := target.store.Orders.SoftDelete(ctx, prefix+"deleted"); deleteErr != nil {
				t.Fatal(deleteErr)
			}
			// markErr 保存付款时间写入错误；用于验证时间戳随事实一并返回。
			if markErr := target.store.Automation.MarkOrderEventTime(ctx, prefix+"newest", "paid_at"); markErr != nil {
				t.Fatal(markErr)
			}

			// facts、err 保存带后缀买家标识与带后缀会话标识查询到的阶段事实；上限 3 条，应按新到旧返回。
			facts, err := target.store.Orders.RecentStageFactsForBuyer(ctx, account, "chat-1@goofish", "buyer-1@goofish", "item-1", 0)
			if err != nil {
				t.Fatal(err)
			}
			if len(facts) != 3 {
				t.Fatalf("应返回上限 3 条: %+v", facts)
			}
			if facts[0].PaidAt == "" || facts[0].OrderStatus == "pending_ship" {
				t.Fatalf("第一条应是最新订单且带付款时间: %+v", facts[0])
			}
			if facts[1].OrderStatus == "pending_ship" || facts[1].PaidAt != "" {
				t.Fatalf("第二条应是缺少买家标识、按会话匹配的订单: %+v", facts[1])
			}
			if facts[2].OrderStatus != "pending_ship" || facts[2].Quantity != "2" || facts[2].SpecName != "颜色" || facts[2].SpecValue != "红" || facts[2].CreatedAt == "" {
				t.Fatalf("第三条应是带后缀买家标识的待发货订单: %+v", facts[2])
			}

			// limited、limitErr 验证显式条数上限。
			limited, limitErr := target.store.Orders.RecentStageFactsForBuyer(ctx, account, "", "buyer-1", "item-1", 1)
			if limitErr != nil || len(limited) != 1 || limited[0].PaidAt == "" {
				t.Fatalf("limit=1 结果异常: %+v err=%v", limited, limitErr)
			}
			// byChat、chatErr 验证没有买家标识时只返回同样缺少买家标识的同会话订单，不会带出其他买家。
			byChat, chatErr := target.store.Orders.RecentStageFactsForBuyer(ctx, account, "chat-1", "", "item-1", 3)
			if chatErr != nil || len(byChat) != 1 {
				t.Fatalf("仅会话匹配结果异常: %+v err=%v", byChat, chatErr)
			}
			// none、noneErr 验证没有订单的买家返回空结果而不是错误。
			none, noneErr := target.store.Orders.RecentStageFactsForBuyer(ctx, account, "", "buyer-none", "item-1", 3)
			if noneErr != nil || len(none) != 0 {
				t.Fatalf("无订单应返回空: %+v err=%v", none, noneErr)
			}
			// arguments 列出缺少必需约束的参数组合：账号、商品为空，或买家与会话同时为空。
			for _, arguments := range [][4]string{{"", "chat-1", "buyer-1", "item-1"}, {account, "chat-1", "buyer-1", " "}, {account, " ", "@goofish", "item-1"}} {
				// empty、emptyErr 保存缺少约束时的查询结果，必须为空且不报错。
				empty, emptyErr := target.store.Orders.RecentStageFactsForBuyer(ctx, arguments[0], arguments[1], arguments[2], arguments[3], 3)
				if emptyErr != nil || empty != nil {
					t.Fatalf("参数 %v 应直接返回空: %+v err=%v", arguments, empty, emptyErr)
				}
			}
			// cancelled、cancel 构造已取消的上下文；查询错误必须原样返回给调用方。
			cancelled, cancel := context.WithCancel(ctx)
			cancel()
			// cancelErr 保存已取消上下文下的查询错误。
			if _, cancelErr := target.store.Orders.RecentStageFactsForBuyer(cancelled, account, "chat-1", "buyer-1", "item-1", 3); cancelErr == nil {
				t.Fatal("上下文取消应返回错误")
			}
		})
	}
}

// TestMultiDB_ReplyOrderContextSettings 验证两张设置表的订单阶段开关默认关闭，并能跨方言写入、读回和关闭。
func TestMultiDB_ReplyOrderContextSettings(t *testing.T) {
	// target 表示当前执行设置读写回归的数据库方言目标。
	for _, target := range allTestTargets(t) {
		// target 是当前子测试独占的数据库目标副本，避免循环变量复用。
		target := target
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 是当前方言内全部设置操作共用的测试上下文。
			ctx := context.Background()
			// account、ownerID 是保存设置的测试账号及其所属用户主键。
			account, ownerID := newOrderContextAccount(t, target, "s")
			// base 是不含订单阶段开关的 AI 设置，模拟升级前的写入内容。
			base := AIReplySettings{AIEnabled: true, MaxDiscountPercent: 10, MaxDiscountAmount: 20, MaxBargainRounds: 3, AIMode: "full_service"}
			// enabled 遍历开关的关闭、开启、再关闭三个取值，验证默认值与双向更新。
			for _, enabled := range []bool{false, true, false} {
				base.OrderContextEnabled = enabled
				// upsertErr 保存 AI 设置写入错误。
				if upsertErr := target.store.AIReply.UpsertSettings(ctx, account, base); upsertErr != nil {
					t.Fatal(upsertErr)
				}
				// settings、getErr 保存读回的 AI 设置及读取错误。
				settings, getErr := target.store.AIReply.Get(ctx, account)
				if getErr != nil || settings.OrderContextEnabled != enabled {
					t.Fatalf("AI 开关读回异常 want=%v got=%+v err=%v", enabled, settings, getErr)
				}
				// listed、listErr 保存用户维度列表读回的 AI 设置及读取错误，列表与单条读取必须一致。
				listed, listErr := target.store.AIReply.ListForUser(ctx, ownerID)
				if listErr != nil || len(listed) != 1 || listed[0].OrderContextEnabled != enabled {
					t.Fatalf("AI 列表开关异常 want=%v got=%+v err=%v", enabled, listed, listErr)
				}
				// writeErr 保存 Agent 设置写入错误。
				if writeErr := target.store.AgentReply.Upsert(ctx, account, AgentReplyWrite{Enabled: true, EndpointURL: "https://agent.example.com",
					RequestTimeoutSeconds: 10, MaxSessionSeconds: 60, OrderContextEnabled: enabled}); writeErr != nil {
					t.Fatal(writeErr)
				}
				// summary、summaryErr 保存读回的 Agent 摘要及读取错误。
				summary, summaryErr := target.store.AgentReply.Get(ctx, account)
				if summaryErr != nil || summary.OrderContextEnabled != enabled {
					t.Fatalf("Agent 摘要开关异常 want=%v got=%+v err=%v", enabled, summary, summaryErr)
				}
				// runtime、runtimeErr 保存读回的 Agent 运行时配置及读取错误。
				runtime, runtimeErr := target.store.AgentReply.RuntimeConfig(ctx, account)
				if runtimeErr != nil || runtime.OrderContextEnabled != enabled {
					t.Fatalf("Agent 运行时开关异常 want=%v got=%+v err=%v", enabled, runtime, runtimeErr)
				}
			}
			// 直接插入不带新列的行，验证数据库默认值为关闭。
			// legacy 是模拟升级前已有配置的另一个账号。
			legacy, _ := newOrderContextAccount(t, target, "legacy")
			// insertErr 保存插入历史形态 AI 配置行的错误。
			if _, insertErr := target.store.DB.ExecContext(ctx, `INSERT INTO ai_reply_settings (cookie_id, ai_enabled) VALUES (?, 1)`, legacy); insertErr != nil {
				t.Fatal(insertErr)
			}
			// legacySettings、legacyErr 保存历史行读回结果及读取错误。
			legacySettings, legacyErr := target.store.AIReply.Get(ctx, legacy)
			if legacyErr != nil || legacySettings.OrderContextEnabled {
				t.Fatalf("历史行开关应默认关闭: %+v err=%v", legacySettings, legacyErr)
			}
		})
	}
}
