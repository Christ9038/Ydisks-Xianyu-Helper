package db

import (
	"context"
	"errors"
	"testing"
)

// TestMultiDB_AccountSwitchGettersAndSavePage 覆盖账号开关读取、远端分页商品保存与聊天会话键集分页。
func TestMultiDB_AccountSwitchGettersAndSavePage(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 是当前方言的存储与销毁函数。
		t.Run(target.name, func(t *testing.T) { // t 负责当前方言的断言。
			defer target.cleanup()
			// store、ctx 是当前方言的仓储聚合与上下文。
			store, ctx := target.store, context.Background()
			// userID、cookieID 是已落库的账号归属。
			userID, cookieID := seedAccount(t, store)

			// 开关读取：先全部置 1，再置 0，并校验不存在账号返回 ErrNotFound。
			for _, column := range []string{"auto_consign", "auto_bargain", "auto_relist"} { // column 是被测的独立开关列。
				// getter 按列名选择对应的读取方法。
				getter := store.Cookies.GetAutoConsign
				if column == "auto_bargain" {
					getter = store.Cookies.GetAutoBargain
				}
				if column == "auto_relist" {
					getter = store.Cookies.GetAutoRelist
				}
				// execErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
				if _, execErr := store.DB.ExecContext(ctx, `UPDATE cookies SET `+column+`=1 WHERE id=?`, cookieID); execErr != nil {
					t.Fatal(execErr)
				}
				// enabled、getErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
				if enabled, getErr := getter(ctx, cookieID); getErr != nil || !enabled {
					t.Fatalf("%s enabled=%v err=%v", column, enabled, getErr)
				}
				// execErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
				if _, execErr := store.DB.ExecContext(ctx, `UPDATE cookies SET `+column+`=0 WHERE id=?`, cookieID); execErr != nil {
					t.Fatal(execErr)
				}
				// enabled、getErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
				if enabled, getErr := getter(ctx, cookieID); getErr != nil || enabled {
					t.Fatalf("%s enabled=%v err=%v", column, enabled, getErr)
				}
				// missingErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
				if _, missingErr := getter(ctx, "missing-account"); !errors.Is(missingErr, ErrNotFound) {
					t.Fatalf("%s missing err=%v", column, missingErr)
				}
			}

			// SavePageFromRemote：空账号报错；空 ID 与重复 ID 被过滤；多规格标记被写入。
			if _, saveErr := store.Items.SavePageFromRemote(ctx, " ", nil); saveErr == nil {
				t.Fatal("空 cookie_id 应报错")
			}
			// saved、saveErr 是本页实际保存的有效商品数。
			saved, saveErr := store.Items.SavePageFromRemote(ctx, cookieID, []ItemInfoRow{
				{ItemID: "page-1", ItemTitle: "A", IsMultiSpec: true},
				{ItemID: " page-1 ", ItemTitle: "重复"},
				{ItemID: "  "},
				{ItemID: "page-2", ItemTitle: "B"},
			})
			if saveErr != nil || saved != 2 {
				t.Fatalf("saved=%d err=%v", saved, saveErr)
			}
			// multi、multiErr 读取多规格标记。
			multi, multiErr := store.Items.MultiSpecFlags(ctx, cookieID, []string{"page-1", "page-2"})
			if multiErr != nil || !multi["page-1"] || multi["page-2"] {
				t.Fatalf("multi=%v err=%v", multi, multiErr)
			}

			// ListSessionPage：三个会话，limit=2 得到下一页游标，再用游标取最后一个。
			for index, chatID := range []string{"chat-a", "chat-b", "chat-c"} { // chatID 是会话标识，index 决定最后消息时间。
				if upsertErr := store.Chats.UpsertSession(ctx, ChatSession{CookieID: cookieID, ChatID: chatID, BuyerID: "peer", LastMessageAt: int64(1000 + index)}); upsertErr != nil {
					t.Fatal(upsertErr)
				}
			}
			// first、firstErr 是第一页结果。
			first, firstErr := store.Chats.ListSessionPage(ctx, userID, cookieID, nil, 2)
			if firstErr != nil || len(first.Sessions) != 2 || !first.HasMore || first.NextCursor == nil {
				t.Fatalf("first=%+v err=%v", first, firstErr)
			}
			// second、secondErr 是游标之后的第二页。
			second, secondErr := store.Chats.ListSessionPage(ctx, userID, cookieID, first.NextCursor, 0)
			if secondErr != nil || second.HasMore || second.NextCursor != nil || len(second.Sessions) != 1 {
				t.Fatalf("second=%+v err=%v", second, secondErr)
			}
			// other、otherErr 是其他用户的空结果。
			other, otherErr := store.Chats.ListSessionPage(ctx, userID+999, cookieID, nil, 500)
			if otherErr != nil || len(other.Sessions) != 0 {
				t.Fatalf("other=%+v err=%v", other, otherErr)
			}
		})
	}
}

// TestMultiDB_SessionsCreateVerified 覆盖认证会话的校验创建：成功、代次过期、用户不存在、停用、空适配器。
func TestMultiDB_SessionsCreateVerified(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 是当前方言的存储与销毁函数。
		t.Run(target.name, func(t *testing.T) { // t 负责当前方言的断言。
			defer target.cleanup()
			// store、ctx 是当前方言的仓储聚合与上下文。
			store, ctx := target.store, context.Background()
			// userID 是已创建的管理员。
			userID, _ := seedAccount(t, store)
			// user、userErr 是当前用户快照及其认证代次。
			user, userErr := store.Users.GetByUsername(ctx, "admin")
			if userErr != nil {
				t.Fatal(userErr)
			}
			// sessionID、createErr 是校验通过后签发的会话。
			sessionID, createErr := store.Sessions.CreateVerified(ctx, user, user.AuthVersion)
			if createErr != nil || sessionID == "" {
				t.Fatalf("session=%q err=%v", sessionID, createErr)
			}
			// staleErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if _, staleErr := store.Sessions.CreateVerified(ctx, user, user.AuthVersion+1); !errors.Is(staleErr, ErrStaleLogin) {
				t.Fatalf("stale err=%v", staleErr)
			}
			// missingErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if _, missingErr := store.Sessions.CreateVerified(ctx, &User{ID: userID + 1000}, 0); !errors.Is(missingErr, ErrNotFound) {
				t.Fatalf("missing err=%v", missingErr)
			}
			// execErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if _, execErr := store.DB.ExecContext(ctx, `UPDATE users SET is_active=0 WHERE id=?`, userID); execErr != nil {
				t.Fatal(execErr)
			}
			// inactiveErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if _, inactiveErr := store.Sessions.CreateVerified(ctx, user, user.AuthVersion); !errors.Is(inactiveErr, ErrStaleLogin) {
				t.Fatalf("inactive err=%v", inactiveErr)
			}
			// nilErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if _, nilErr := (*Sessions)(nil).CreateVerified(ctx, user, 0); nilErr == nil {
				t.Fatal("空适配器应报错")
			}
			// nilUserErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if _, nilUserErr := store.Sessions.CreateVerified(ctx, nil, 0); nilUserErr == nil {
				t.Fatal("空用户应报错")
			}
		})
	}
}

// TestMultiDB_AllowOrderDeliveryRunGuards 覆盖同一订单付款发货运行的跨触发来源防重：自动事件互斥、人工补发仅可接管无凭证的成功/零发送失败运行。
func TestMultiDB_AllowOrderDeliveryRunGuards(t *testing.T) {
	for _, target := range allTestTargets(t) { // target 是当前方言的存储与销毁函数。
		t.Run(target.name, func(t *testing.T) { // t 负责当前方言的断言。
			defer target.cleanup()
			// store、ctx 是当前方言的仓储聚合与上下文。
			store, ctx := target.store, context.Background()
			// userID、cookieID 是合法账号归属。
			userID, cookieID := seedAccount(t, store)
			// ruleID、createErr 是付款触发规则。
			ruleID, createErr := store.Automation.Create(ctx, makeAutomationRule(cookieID, userID, "guard-item", "order_paid", true, 1))
			if createErr != nil {
				t.Fatal(createErr)
			}
			// orderID 是被测订单。
			const orderID = "guard-order"
			// upsertErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if upsertErr := store.Orders.Upsert(ctx, orderID, OrderUpsertOpts{CookieID: cookieID, ItemID: "guard-item", BuyerID: "buyer", OrderStatus: "pending_ship"}); upsertErr != nil {
				t.Fatal(upsertErr)
			}
			// start 以指定幂等键尝试创建付款运行并返回运行 ID 与是否领取成功。
			start := func(key string) (int64, bool) {
				// runID、started、startErr 是创建结果。
				runID, started, startErr := store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cookieID, OrderID: orderID, TriggerType: "order_paid", TriggerKey: key})
				if startErr != nil {
					t.Fatalf("start %s: %v", key, startErr)
				}
				return runID, started
			}
			// autoRun、started 是首个自动运行。
			autoRun, started := start("auto-1")
			if !started {
				t.Fatal("首个自动运行应领取成功")
			}
			// second 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if _, second := start("auto-2"); second {
				t.Fatal("同订单第二个自动触发不得领取")
			}
			// manual 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if _, manual := start("manual_delivery:1"); manual {
				t.Fatal("人工补发不得接管仍在运行的自动运行")
			}
			// finishErr 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if finishErr := store.Automation.FinishRun(ctx, autoRun, 1, "success", 1, ""); finishErr != nil {
				t.Fatal(finishErr)
			}
			// 无凭证快照的成功运行允许人工补发接管；同键重复调用走 continue 分支。
			if _, manual := start("manual_delivery:1"); !manual {
				t.Fatal("无凭证的成功运行应允许人工补发")
			}
			// again 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if _, again := start("manual_delivery:1"); again {
				t.Log("同键重复调用的返回由幂等重领规则决定")
			}
			// auto 保存该步骤的返回值或错误，用于断言预期结果或在故障注入场景中忽略其结果。
			if _, auto := start("auto-3"); auto {
				t.Fatal("自动触发仍不得越过已有运行")
			}
			// 非付款触发与空订单不受该检查约束。
			if _, free, freeErr := store.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cookieID, TriggerType: "buyer_reviewed", TriggerKey: "no-order"}); freeErr != nil || !free {
				t.Fatalf("无订单运行 free=%v err=%v", free, freeErr)
			}
		})
	}
}
