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
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"time"

	"xianyu-go/internal/db"
)

// main 负责main相关处理。
func main() {
	// cleanup 保存cleanup，供当前处理流程使用
	var cleanup func() error
	// fail 保存fail，供当前处理流程使用
	fail := func(format string, args ...any) {
		if cleanup != nil {
			if // err 保存err，供当前处理流程使用
			err := cleanup(); err != nil {
				fmt.Printf("⚠️ 清理验证数据失败: %v\n", err)
			}
		}
		fmt.Printf(format, args...)
		os.Exit(1)
	}

	if len(os.Args) < 2 {
		fmt.Println("用法: dbverify <database-url>")
		os.Exit(1)
	}
	// url 保存地址，供当前处理流程使用
	url := os.Args[1]
	// ctx、cancel 保存ctx、cancel，供当前处理流程使用
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	fmt.Printf("连接 %s ...\n", maskURL(url))
	// database、dialect、err 保存database、dialect、err，供当前处理流程使用
	database, dialect, err := db.Open(ctx, url)
	if err != nil {
		fail("❌ Open 失败: %v\n", err)
	}
	defer database.Close()
	fmt.Printf("✅ 迁移成功，方言=%s\n", dialect)

	// store 保存store，供当前处理流程使用
	store := db.NewStore(database, dialect)

	// 1) 创建用户（用唯一用户名，避免在已有数据的库上因 admin 重名而失败）。
	ids := newVerifyIDs(time.Now().UnixNano())
	// username 保存username，供当前处理流程使用
	username := ids.username
	// accountID 保存账号ID，供当前处理流程使用
	accountID := ids.accountID
	// orderID 保存订单ID，供当前处理流程使用
	orderID := ids.orderID
	// itemID 保存商品ID，供当前处理流程使用
	itemID := ids.itemID
	// buyerID 保存买家ID，供当前处理流程使用
	buyerID := ids.buyerID
	cleanup = func() error {
		// cleanupCtx、cleanupCancel 保存cleanupCtx、cleanup取消，供当前处理流程使用
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		return cleanupVerifyData(cleanupCtx, store, ids)
	}
	// password、err 保存password、err，供当前处理流程使用
	password, err := newVerifyPassword()
	if err != nil {
		fail("❌ 生成验证密码失败: %v\n", err)
	}
	// ok、err 保存ok、err，供当前处理流程使用
	ok, err := store.Users.Create(ctx, username, username+"@test.local", password)
	if err != nil || !ok {
		fail("❌ 创建用户失败: err=%v ok=%v\n", err, ok)
	}
	// adminUser、err 保存adminUser、err，供当前处理流程使用
	adminUser, err := store.Users.GetByUsername(ctx, username)
	if err != nil {
		fail("❌ 查询验证用户失败: %v\n", err)
	}
	// userID 保存用户ID，供当前处理流程使用
	userID := adminUser.ID
	fmt.Printf("✅ 创建验证用户 %s (id=%d)\n", username, userID)

	// 2) 保存 cookie（dialectUpsert: ON CONFLICT/ON DUPLICATE KEY）
	if err := store.Cookies.Save(ctx, accountID, "unb=123; _m_h5_tk=tk_1;", userID); err != nil {
		fail("❌ 保存 cookie 失败: %v\n", err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Cookies.SetStatus(ctx, accountID, false); err != nil {
		fail("❌ 禁用验证账号失败: %v\n", err)
	}
	// 再 Save 一次验证 upsert
	if err := store.Cookies.Save(ctx, accountID, "unb=123; _m_h5_tk=tk_2;", userID); err != nil {
		fail("❌ 二次保存 cookie 失败: %v\n", err)
	}
	// v 保存v，供当前处理流程使用
	v, _ := store.Cookies.GetValue(ctx, accountID)
	fmt.Printf("✅ cookie upsert 成功，value=%s\n", v)

	// 3) 系统设置 upsert（dialectUpsert + key 保留字引用）
	if err := store.Settings.Set(ctx, "theme_color", "blue"); err != nil {
		fail("❌ 系统设置 Set 失败: %v\n", err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Settings.Set(ctx, "theme_color", "green"); err != nil {
		fail("❌ 系统设置二次 Set 失败: %v\n", err)
	}
	fmt.Println("✅ 系统设置 upsert 成功（key 保留字处理 OK）")

	// 4) 订单 upsert（INSERT IGNORE + 动态 UPDATE）
	if err := store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{
		ItemID: itemID, BuyerID: buyerID, CookieID: accountID, OrderStatus: "paid", Amount: "19.90",
	}); err != nil {
		fail("❌ 订单 Upsert 失败: %v\n", err)
	}
	// 二次 upsert 验证不重复插入
	if err := store.Orders.Upsert(ctx, orderID, db.OrderUpsertOpts{
		OrderStatus: "shipped", ChatID: "chat-1",
	}); err != nil {
		fail("❌ 订单二次 Upsert 失败: %v\n", err)
	}
	fmt.Println("✅ 订单 upsert 成功（INSERT IGNORE + UPDATE OK）")

	// 5) 商品信息 upsert（dialectUpsert，UNIQUE(cookie_id, item_id)）
	if err := store.Items.Upsert(ctx, &db.ItemInfoRow{
		CookieID: accountID, ItemID: itemID, ItemTitle: "测试商品", ItemPrice: "19.90",
	}); err != nil {
		fail("❌ 商品 Upsert 失败: %v\n", err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Items.Upsert(ctx, &db.ItemInfoRow{
		CookieID: accountID, ItemID: itemID, ItemTitle: "更新后商品", ItemPrice: "29.90",
	}); err != nil {
		fail("❌ 商品二次 Upsert 失败: %v\n", err)
	}
	fmt.Println("✅ 商品信息 upsert 成功")

	// 6) 卡密创建（boolToInt 布尔写入）
	cardID, err := store.Cards.Create(ctx, &db.CardFull{
		Name: "测试卡组", Type: "data", DataContent: "card-1\ncard-2\ncard-3", Enabled: true, UserID: userID,
	})
	if err != nil {
		fail("❌ 创建卡密失败: %v\n", err)
	}
	fmt.Printf("✅ 创建卡密组 (id=%d)\n", cardID)

	// 7) 卡密批量追加（AppendBatchData）
	added, err := store.Cards.AppendBatchData(ctx, cardID, "card-4\ncard-5")
	if err != nil {
		fail("❌ 追加卡密失败: %v\n", err)
	}
	fmt.Printf("✅ 追加卡密 %d 个\n", added)

	// 8) 通知渠道 + 绑定（dialectUpsert）
	chID, err := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{
		Name: "测试渠道", Type: "webhook", Config: `{"webhook_url":"http://x"}`, Enabled: false, UserID: userID,
	})
	if err != nil {
		fail("❌ 创建通知渠道失败: %v\n", err)
	}
	if // err 保存err，供当前处理流程使用
	err := store.Notifications.SetBindings(ctx, accountID, []int64{chID}); err != nil {
		fail("❌ 绑定通知渠道失败: %v\n", err)
	}
	fmt.Printf("✅ 通知渠道 + 绑定 OK (channel=%d)\n", chID)

	// 9) 自动化规则（TryStartRun 用 INSERT IGNORE + UNIQUE 防重）
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: userID, CookieID: accountID, ItemID: itemID, Name: "付款发货",
		TriggerType: "order_paid", Enabled: false, Priority: 100,
	})
	if err != nil {
		fail("❌ 创建自动化规则失败: %v\n", err)
	}
	// runID、started、err 保存运行ID、started、err，供当前处理流程使用
	runID, started, err := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: accountID, TriggerType: "order_paid", TriggerKey: orderID, Status: "running",
	})
	if err != nil || !started {
		fail("❌ TryStartRun 失败: err=%v started=%v\n", err, started)
	}
	// 重复触发应 started=false
	_, started2, err := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: accountID, TriggerType: "order_paid", TriggerKey: orderID, Status: "running",
	})
	if err != nil {
		fail("❌ TryStartRun 二次调用失败: %v\n", err)
	}
	if started2 {
		fail("❌ TryStartRun 重复触发未防重\n")
	}
	fmt.Printf("✅ 自动化规则 + 防重 OK (rule=%d run=%d)\n", ruleID, runID)

	if // err 保存err，供当前处理流程使用
	err := cleanup(); err != nil {
		fmt.Printf("❌ 清理验证数据失败: %v\n", err)
		os.Exit(1)
	}
	cleanup = nil
	fmt.Println("✅ 验证数据已清理")
	fmt.Println("\n🎉 全部验证通过")
}

// maskURL 负责maskURL相关处理。
func maskURL(url string) string {
	// 只显示 scheme 和 host，把 scheme 后到首个 '@' 之间的凭证替换为 ***。
	for _, p := range []string{"mysql://", "postgres://", "postgresql://"} {
		if len(url) > len(p) && url[:len(p)] == p {
			// rest 保存rest，供当前处理流程使用
			rest := url[len(p):]
			if // at 保存at，供当前处理流程使用
			at := strings.Index(rest, "@"); at >= 0 {
				return p + "***@" + rest[at+1:]
			}
			return url
		}
	}
	return url
}

// verifyIDs 保存verifyIDs，供当前处理流程使用
type verifyIDs struct {
	username  string
	accountID string
	orderID   string
	itemID    string
	buyerID   string
}

// newVerifyIDs 负责newVerifyIDs相关处理。
func newVerifyIDs(n int64) verifyIDs {
	// suffix 保存suffix，供当前处理流程使用
	suffix := fmt.Sprintf("%d", n)
	return verifyIDs{
		username:  "verify_" + suffix,
		accountID: "acc_" + suffix,
		orderID:   "order_" + suffix,
		itemID:    "item_" + suffix,
		buyerID:   "buyer_" + suffix,
	}
}

// newVerifyPassword 负责newVerify密码相关处理。
func newVerifyPassword() (string, error) {
	// b 保存b，供当前处理流程使用
	var b [24]byte
	if // err 保存err，供当前处理流程使用
	_, err := rand.Read(b[:]); err != nil {
		return "", err
	}
	return "verify_" + hex.EncodeToString(b[:]), nil
}

// cleanupVerifyData 负责cleanupVerify数据相关处理。
func cleanupVerifyData(ctx context.Context, store *db.Store, ids verifyIDs) error {
	// userID 保存用户ID，供当前处理流程使用
	userID := int64(0)
	if // user、err 保存user、err，供当前处理流程使用
	user, err := store.Users.GetByUsername(ctx, ids.username); err == nil {
		userID = user.ID
	}
	// queries 保存queries，供当前处理流程使用
	queries := []struct {
		query string
		args  []any
	}{
		{`DELETE FROM automation_runs WHERE trigger_key=? OR cookie_id=?`, []any{ids.orderID, ids.accountID}},
		{`DELETE FROM automation_rule_actions WHERE rule_id IN (SELECT id FROM automation_rules WHERE cookie_id=? OR user_id=?)`, []any{ids.accountID, userID}},
		{`DELETE FROM automation_rules WHERE cookie_id=? OR user_id=?`, []any{ids.accountID, userID}},
		{`DELETE FROM message_notifications WHERE cookie_id=? OR channel_id IN (SELECT id FROM notification_channels WHERE user_id=? AND name=?)`, []any{ids.accountID, userID, "测试渠道"}},
		{`DELETE FROM notification_channels WHERE user_id=? AND name=?`, []any{userID, "测试渠道"}},
		{`DELETE FROM cards WHERE user_id=? AND name=?`, []any{userID, "测试卡组"}},
		{`DELETE FROM item_info WHERE cookie_id=? AND item_id=?`, []any{ids.accountID, ids.itemID}},
		{`DELETE FROM orders WHERE order_id=? OR cookie_id=?`, []any{ids.orderID, ids.accountID}},
		{`DELETE FROM cookie_status WHERE cookie_id=?`, []any{ids.accountID}},
		{`DELETE FROM cookies WHERE id=?`, []any{ids.accountID}},
		{`DELETE FROM sessions WHERE user_id=?`, []any{userID}},
		{`DELETE FROM users WHERE username=? AND email=?`, []any{ids.username, ids.username + "@test.local"}},
	}
	// q 表示当前遍历过程中的q
	for _, q := range queries {
		if // err 保存err，供当前处理流程使用
		_, err := store.DB.ExecContext(ctx, q.query, q.args...); err != nil {
			return err
		}
	}
	return nil
}
