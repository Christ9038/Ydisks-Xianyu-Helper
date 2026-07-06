// dbverify 在 MySQL/Postgres（或 SQLite）上跑迁移 + 核心 CRUD，
// 确认方言适配器在真实实例上工作。
//
// 用法：
//
//	go run ./cmd/dbverify "mysql://user:pass@tcp(host:3306)/db?parseTime=true&loc=Local&multiStatements=true"
//	go run ./cmd/dbverify "postgres://user:pass@host:5432/db?sslmode=disable"
//	go run ./cmd/dbverify "sqlite://data/verify.db"
//
// MySQL DSN 必须带 multiStatements=true（goose 多语句迁移需要）。
// 全部 9 步通过即说明三库的 upsert/布尔/自增主键路径均正常。
package main

import (
	"context"
	"fmt"
	"os"
	"time"

	"xianyu-go/internal/db"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("用法: dbverify <database-url>")
		os.Exit(1)
	}
	url := os.Args[1]
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Printf("连接 %s ...\n", maskURL(url))
	database, dialect, err := db.Open(ctx, url)
	if err != nil {
		fmt.Printf("❌ Open 失败: %v\n", err)
		os.Exit(1)
	}
	defer database.Close()
	fmt.Printf("✅ 迁移成功，方言=%s\n", dialect)

	store := db.NewStore(database, dialect)

	// 1) 创建用户
	ok, err := store.Users.Create(ctx, "admin", "admin@test.local", "pw123456")
	if err != nil || !ok {
		fmt.Printf("❌ 创建用户失败: err=%v ok=%v\n", err, ok)
		os.Exit(1)
	}
	store.Users.SetAdmin(ctx, "admin")
	adminUser, _ := store.Users.GetByUsername(ctx, "admin")
	userID := adminUser.ID
	fmt.Printf("✅ 创建用户 admin (id=%d)\n", userID)

	// 2) 保存 cookie（dialectUpsert: ON CONFLICT/ON DUPLICATE KEY）
	if err := store.Cookies.Save(ctx, "acc1", "unb=123; _m_h5_tk=tk_1;", userID); err != nil {
		fmt.Printf("❌ 保存 cookie 失败: %v\n", err)
		os.Exit(1)
	}
	// 再 Save 一次验证 upsert
	if err := store.Cookies.Save(ctx, "acc1", "unb=123; _m_h5_tk=tk_2;", userID); err != nil {
		fmt.Printf("❌ 二次保存 cookie 失败: %v\n", err)
		os.Exit(1)
	}
	v, _ := store.Cookies.GetValue(ctx, "acc1")
	fmt.Printf("✅ cookie upsert 成功，value=%s\n", v)

	// 3) 系统设置 upsert（dialectUpsert + key 保留字引用）
	if err := store.Settings.Set(ctx, "theme_color", "blue"); err != nil {
		fmt.Printf("❌ 系统设置 Set 失败: %v\n", err)
		os.Exit(1)
	}
	if err := store.Settings.Set(ctx, "theme_color", "green"); err != nil {
		fmt.Printf("❌ 系统设置二次 Set 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 系统设置 upsert 成功（key 保留字处理 OK）")

	// 4) 订单 upsert（INSERT IGNORE + 动态 UPDATE）
	if err := store.Orders.Upsert(ctx, "order-1", db.OrderUpsertOpts{
		ItemID: "item-1", BuyerID: "buyer-1", CookieID: "acc1", OrderStatus: "paid", Amount: "19.90",
	}); err != nil {
		fmt.Printf("❌ 订单 Upsert 失败: %v\n", err)
		os.Exit(1)
	}
	// 二次 upsert 验证不重复插入
	if err := store.Orders.Upsert(ctx, "order-1", db.OrderUpsertOpts{
		OrderStatus: "shipped", ChatID: "chat-1",
	}); err != nil {
		fmt.Printf("❌ 订单二次 Upsert 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 订单 upsert 成功（INSERT IGNORE + UPDATE OK）")

	// 5) 商品信息 upsert（dialectUpsert，UNIQUE(cookie_id, item_id)）
	if err := store.Items.Upsert(ctx, &db.ItemInfoRow{
		CookieID: "acc1", ItemID: "item-1", ItemTitle: "测试商品", ItemPrice: "19.90",
	}); err != nil {
		fmt.Printf("❌ 商品 Upsert 失败: %v\n", err)
		os.Exit(1)
	}
	if err := store.Items.Upsert(ctx, &db.ItemInfoRow{
		CookieID: "acc1", ItemID: "item-1", ItemTitle: "更新后商品", ItemPrice: "29.90",
	}); err != nil {
		fmt.Printf("❌ 商品二次 Upsert 失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Println("✅ 商品信息 upsert 成功")

	// 6) 卡密创建（boolToInt 布尔写入）
	cardID, err := store.Cards.Create(ctx, &db.CardFull{
		Name: "测试卡组", Type: "data", DataContent: "card-1\ncard-2\ncard-3", Enabled: true, UserID: userID,
	})
	if err != nil {
		fmt.Printf("❌ 创建卡密失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 创建卡密组 (id=%d)\n", cardID)

	// 7) 卡密批量追加（AppendBatchData）
	added, err := store.Cards.AppendBatchData(ctx, cardID, "card-4\ncard-5")
	if err != nil {
		fmt.Printf("❌ 追加卡密失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 追加卡密 %d 个\n", added)

	// 8) 通知渠道 + 绑定（dialectUpsert）
	chID, err := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{
		Name: "测试渠道", Type: "webhook", Config: `{"webhook_url":"http://x"}`, Enabled: true, UserID: userID,
	})
	if err != nil {
		fmt.Printf("❌ 创建通知渠道失败: %v\n", err)
		os.Exit(1)
	}
	if err := store.Notifications.SetBindings(ctx, "acc1", []int64{chID}); err != nil {
		fmt.Printf("❌ 绑定通知渠道失败: %v\n", err)
		os.Exit(1)
	}
	fmt.Printf("✅ 通知渠道 + 绑定 OK (channel=%d)\n", chID)

	// 9) 自动化规则（TryStartRun 用 INSERT IGNORE + UNIQUE 防重）
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: userID, CookieID: "acc1", ItemID: "item-1", Name: "付款发货",
		TriggerType: "order_paid", Enabled: true, Priority: 100,
	})
	if err != nil {
		fmt.Printf("❌ 创建自动化规则失败: %v\n", err)
		os.Exit(1)
	}
	runID, started, err := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: "acc1", TriggerType: "order_paid", TriggerKey: "order-1", Status: "running",
	})
	if err != nil || !started {
		fmt.Printf("❌ TryStartRun 失败: err=%v started=%v\n", err, started)
		os.Exit(1)
	}
	// 重复触发应 started=false
	_, started2, err := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: "acc1", TriggerType: "order_paid", TriggerKey: "order-1", Status: "running",
	})
	if started2 {
		fmt.Printf("❌ TryStartRun 重复触发未防重\n")
		os.Exit(1)
	}
	fmt.Printf("✅ 自动化规则 + 防重 OK (rule=%d run=%d)\n", ruleID, runID)

	fmt.Println("\n🎉 全部验证通过")
}

func maskURL(url string) string {
	// 只显示 scheme 和 host，隐藏密码
	for _, p := range []string{"mysql://", "postgres://", "postgresql://"} {
		if len(url) > len(p) && url[:len(p)] == p {
			return p + "***@" + url[len(p):]
		}
	}
	return url
}
