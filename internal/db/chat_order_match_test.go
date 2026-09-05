package db

import (
	"context"
	"slices"
	"testing"
)

// TestMultiDB_ChatBuyerSuffixMatch 用 t 验证三方言对历史买家后缀双向匹配，保留多候选歧义及账号、商品隔离。
func TestMultiDB_ChatBuyerSuffixMatch(t *testing.T) {
	// target 是独立迁移后的方言数据库，清理由该目标子测试负责。
	for _, target := range allTestTargets(t) {
		// t 接收当前数据库的匹配断言，所有会话均为本地合成数据。
		t.Run(target.name, func(t *testing.T) {
			defer target.cleanup()
			// ctx 只用于当前测试的本地仓储调用。
			ctx := context.Background()
			// userID、cookieID 是会话所属的测试管理用户和账号，用于构造有效外键。
			userID, cookieID := seedAccount(t, target.store)
			// saveErr 保存另一账号初始化失败，空凭证避免访问任何真实账号。
			if saveErr := target.store.Cookies.Save(ctx, "suffix-other-account", "", userID); saveErr != nil {
				t.Fatal(saveErr)
			}
			// session 逐项构造裸 ID、历史后缀、多候选及不应命中的边界会话。
			for _, session := range []ChatSession{
				{CookieID: cookieID, ChatID: "legacy", BuyerID: "legacy-buyer@goofish", ItemID: "item"},
				{CookieID: cookieID, ChatID: "bare", BuyerID: "bare-buyer", ItemID: "item"},
				{CookieID: cookieID, ChatID: "ambiguous-bare", BuyerID: "ambiguous-buyer", ItemID: "item"},
				{CookieID: cookieID, ChatID: "ambiguous-suffix", BuyerID: "ambiguous-buyer@goofish", ItemID: "item"},
				{CookieID: cookieID, ChatID: "other-item", BuyerID: "legacy-buyer@goofish", ItemID: "other"},
				{CookieID: "suffix-other-account", ChatID: "other-account", BuyerID: "legacy-buyer@goofish", ItemID: "item"},
			} {
				// writeErr 保存当前会话种子失败，确保空结果不会来自缺失夹具。
				if writeErr := target.store.Chats.UpsertSession(ctx, session); writeErr != nil {
					t.Fatal(writeErr)
				}
			}
			// buyerID、want 分别是两种订单买家格式和必须完整返回的候选集合。
			for buyerID, want := range map[string][]string{
				"legacy-buyer": {"legacy"}, "legacy-buyer@goofish": {"legacy"},
				"bare-buyer": {"bare"}, "bare-buyer@goofish": {"bare"},
				"ambiguous-buyer":         {"ambiguous-bare", "ambiguous-suffix"},
				"ambiguous-buyer@goofish": {"ambiguous-bare", "ambiguous-suffix"},
			} {
				// ids、queryErr 保存真实 SQL 查询结果及错误；排序仅用于无序集合比较。
				ids, queryErr := target.store.Chats.FindChatIDsByBuyerAndItem(ctx, cookieID, buyerID, "item")
				slices.Sort(ids)
				if queryErr != nil || !slices.Equal(ids, want) {
					t.Errorf("买家 %s 匹配=%v，预期=%v，查询错误=%v", buyerID, ids, want, queryErr)
				}
			}
		})
	}
}

// TestFindChatIDsByBuyerAndItem 验证订单同步的会话匹配按账号、买家和商品隔离，并拒绝多会话歧义。
func TestFindChatIDsByBuyerAndItem(t *testing.T) {
	// store、cleanup 保存迁移后的临时 SQLite 数据库及关闭责任。
	store, cleanup := newTestDB(t)
	defer cleanup()
	// ctx 是本测试共用的数据库上下文。
	ctx := context.Background()
	// _, cookieID 保存测试账号归属；用户标识只用于构造外键关系。
	_, cookieID := seedAccount(t, store)
	// uniqueSession 保存唯一可确认的账号、买家和商品会话。
	uniqueSession := ChatSession{CookieID: cookieID, ChatID: "chat-unique", BuyerID: "buyer-1", ItemID: "item-1"}
	// err 保存唯一会话初始化写入错误。
	if err := store.Chats.UpsertSession(ctx, uniqueSession); err != nil {
		t.Fatal(err)
	}
	// uniqueIDs、uniqueErr 保存唯一匹配查询结果。
	uniqueIDs, uniqueErr := store.Chats.FindChatIDsByBuyerAndItem(ctx, cookieID, "buyer-1", "item-1")
	if uniqueErr != nil || len(uniqueIDs) != 1 || uniqueIDs[0] != "chat-unique" {
		t.Fatalf("唯一会话匹配异常: ids=%v err=%v", uniqueIDs, uniqueErr)
	}
	// suffixSession 保存带平台后缀的历史买家会话，用于验证订单买家标识的兼容匹配。
	suffixSession := ChatSession{CookieID: cookieID, ChatID: "chat-suffix", BuyerID: "buyer-2@goofish", ItemID: "item-2"}
	// suffixWriteErr 保存带后缀会话初始化写入错误。
	if suffixWriteErr := store.Chats.UpsertSession(ctx, suffixSession); suffixWriteErr != nil {
		t.Fatal(suffixWriteErr)
	}
	// suffixIDs、suffixErr 保存带平台后缀买家标识的匹配结果。
	suffixIDs, suffixErr := store.Chats.FindChatIDsByBuyerAndItem(ctx, cookieID, "buyer-2@goofish", "item-2")
	if suffixErr != nil || len(suffixIDs) != 1 || suffixIDs[0] != "chat-suffix" {
		t.Fatalf("买家后缀兼容匹配异常: ids=%v err=%v", suffixIDs, suffixErr)
	}
	// hiddenErr 保存隐藏会话仍可作为发货目标的查询结果。
	if hiddenErr := store.Chats.SetSessionVisible(ctx, cookieID, "chat-unique", false); hiddenErr != nil {
		t.Fatal(hiddenErr)
	}
	// hiddenIDs、hiddenQueryErr 保存隐藏会话的再次匹配结果。
	hiddenIDs, hiddenQueryErr := store.Chats.FindChatIDsByBuyerAndItem(ctx, cookieID, "buyer-1", "item-1")
	if hiddenQueryErr != nil || len(hiddenIDs) != 1 || hiddenIDs[0] != "chat-unique" {
		t.Fatalf("隐藏会话不应丢失: ids=%v err=%v", hiddenIDs, hiddenQueryErr)
	}
	// ambiguousSession 保存同一订单上下文下的第二个会话，用于验证不自动选择任一会话。
	ambiguousSession := ChatSession{CookieID: cookieID, ChatID: "chat-second", BuyerID: "buyer-1", ItemID: "item-1"}
	// err 保存歧义会话初始化写入错误。
	if err := store.Chats.UpsertSession(ctx, ambiguousSession); err != nil {
		t.Fatal(err)
	}
	// ambiguousIDs、ambiguousErr 保存多候选匹配查询结果。
	ambiguousIDs, ambiguousErr := store.Chats.FindChatIDsByBuyerAndItem(ctx, cookieID, "buyer-1", "item-1")
	if ambiguousErr != nil || len(ambiguousIDs) != 2 {
		t.Fatalf("多会话匹配结果异常: ids=%v err=%v", ambiguousIDs, ambiguousErr)
	}
	// emptyIDs、emptyErr 保存缺少商品条件时的安全空结果。
	emptyIDs, emptyErr := store.Chats.FindChatIDsByBuyerAndItem(ctx, cookieID, "buyer-1", "")
	if emptyErr != nil || len(emptyIDs) != 0 {
		t.Fatalf("缺少商品条件不应扩大匹配: ids=%v err=%v", emptyIDs, emptyErr)
	}
}
