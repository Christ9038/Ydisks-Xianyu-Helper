package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"xianyu-go/internal/db"
)

// TestMaskURL 验证带凭证的数据库 URL 脱敏：保留 scheme + host，密码替换为 ***。
func TestMaskURL(t *testing.T) {
	// cases 用于本次流程后续判断的cases
	cases := map[string]string{
		"mysql://user:secret@tcp(host:3306)/db?x=1": "mysql://***@tcp(host:3306)/db?x=1",
		"postgres://user:pass@host:5432/db":         "postgres://***@host:5432/db",
		"postgresql://u:p@h:5432/d":                 "postgresql://***@h:5432/d",
		"sqlite://data/x.db":                        "sqlite://data/x.db", // 无密码，原样
		"/local/path.db":                            "/local/path.db",     // 非 URL，原样
	}
	// in、want 表示当前遍历过程中的in、want
	for in, want := range cases {
		if // got 用于本次流程后续判断的got
		got := maskURL(in); got != want {
			t.Errorf("maskURL(%q) = %q; want %q", in, got, want)
		}
	}
}

// TestNewVerifyIDsAreUniqueForPersistentRuns 封装TestNewVerifyIDsAreUniqueForPersistent运行记录业务协调。
func TestNewVerifyIDsAreUniqueForPersistentRuns(t *testing.T) {
	// ids 用于本次流程后续判断的ids
	ids := newVerifyIDs(12345)
	// want 用于本次流程后续判断的want
	want := map[string]string{
		"username":  "verify_12345",
		"accountID": "acc_12345",
		"orderID":   "order_12345",
		"itemID":    "item_12345",
		"buyerID":   "buyer_12345",
	}
	// got 用于本次流程后续判断的got
	got := map[string]string{
		"username":  ids.username,
		"accountID": ids.accountID,
		"orderID":   ids.orderID,
		"itemID":    ids.itemID,
		"buyerID":   ids.buyerID,
	}
	// key、wantValue 表示当前遍历过程中的key、want值
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("%s=%q want %q", key, got[key], wantValue)
		}
	}
	if ids.username == ids.accountID || ids.accountID == ids.orderID || ids.orderID == ids.itemID {
		t.Fatalf("验证用 ID 不应互相复用: %+v", ids)
	}
}

// TestNewVerifyPasswordIsNotFixedDefault 封装TestNewVerify密码IsNotFixedDefault业务协调。
func TestNewVerifyPasswordIsNotFixedDefault(t *testing.T) {
	// first、err 用于本次流程后续判断的first、err
	first, err := newVerifyPassword()
	if err != nil {
		t.Fatalf("newVerifyPassword: %v", err)
	}
	// second、err 用于本次流程后续判断的second、err
	second, err := newVerifyPassword()
	if err != nil {
		t.Fatalf("newVerifyPassword second: %v", err)
	}
	if first == "pw123456" || second == "pw123456" {
		t.Fatal("验证用户不应使用固定弱密码")
	}
	if first == second {
		t.Fatalf("验证密码应随机生成，got %q", first)
	}
}

// TestCleanupVerifyDataRemovesRowsAndVerifyDataIsDisabled 封装TestCleanupVerify数据RemovesRowsAndVerify数据IsDisabled业务协调。
func TestCleanupVerifyDataRemovesRowsAndVerifyDataIsDisabled(t *testing.T) {
	// ctx 用于本次流程后续判断的ctx
	ctx := context.Background()
	// database、dialect、err 用于本次流程后续判断的database、dialect、err
	database, dialect, err := db.Open(ctx, filepath.Join(t.TempDir(), "verify.db"))
	if err != nil {
		t.Fatalf("Open: %v", err)
	}
	defer database.Close()
	// store 用于本次流程后续判断的store
	store := db.NewStore(database, dialect)
	// ids 用于本次流程后续判断的ids
	ids := newVerifyIDs(987654321)

	// password、err 用于本次流程后续判断的password、err
	password, err := newVerifyPassword()
	if err != nil {
		t.Fatalf("newVerifyPassword: %v", err)
	}
	// ok、err 用于本次流程后续判断的ok、err
	ok, err := store.Users.Create(ctx, ids.username, ids.username+"@test.local", password)
	if err != nil || !ok {
		t.Fatalf("Create user: ok=%v err=%v", ok, err)
	}
	// user、err 用于本次流程后续判断的user、err
	user, err := store.Users.GetByUsername(ctx, ids.username)
	if err != nil {
		t.Fatalf("GetByUsername: %v", err)
	}
	if user.IsAdmin {
		t.Fatal("dbverify 创建的验证用户不应是管理员")
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.Save(ctx, ids.accountID, "unb=123; _m_h5_tk=tk_1;", user.ID); err != nil {
		t.Fatalf("Save cookie: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Cookies.SetStatus(ctx, ids.accountID, false); err != nil {
		t.Fatalf("SetStatus false: %v", err)
	}
	if store.Cookies.GetStatus(ctx, ids.accountID) {
		t.Fatal("dbverify 验证账号应默认禁用，避免被服务启动")
	}
	if // err 用于本次流程后续判断的err
	err := store.Orders.Upsert(ctx, ids.orderID, db.OrderUpsertOpts{ItemID: ids.itemID, BuyerID: ids.buyerID, CookieID: ids.accountID}); err != nil {
		t.Fatalf("Orders.Upsert: %v", err)
	}
	if // err 用于本次流程后续判断的err
	err := store.Items.Upsert(ctx, &db.ItemInfoRow{CookieID: ids.accountID, ItemID: ids.itemID, ItemTitle: "测试商品"}); err != nil {
		t.Fatalf("Items.Upsert: %v", err)
	}
	// cardID、err 用于本次流程后续判断的卡密ID、err
	cardID, err := store.Cards.Create(ctx, &db.CardFull{Name: "测试卡组", Type: "data", DataContent: "card", Enabled: true, UserID: user.ID})
	if err != nil || cardID == 0 {
		t.Fatalf("Cards.Create: id=%d err=%v", cardID, err)
	}
	// channelID、err 用于本次流程后续判断的渠道ID、err
	channelID, err := store.Notifications.CreateChannel(ctx, &db.NotificationChannelRow{
		Name: "测试渠道", Type: "webhook", Config: `{"webhook_url":"http://x"}`, Enabled: false, UserID: user.ID,
	})
	if err != nil {
		t.Fatalf("CreateChannel: %v", err)
	}
	// channelEnabled 用于本次流程后续判断的渠道启用状态
	var channelEnabled int
	if // err 用于本次流程后续判断的err
	err := store.DB.QueryRowContext(ctx, `SELECT enabled FROM notification_channels WHERE id=?`, channelID).Scan(&channelEnabled); err != nil {
		t.Fatalf("query channel enabled: %v", err)
	}
	if channelEnabled != 0 {
		t.Fatal("dbverify 验证通知渠道应默认禁用")
	}
	if // err 用于本次流程后续判断的err
	err := store.Notifications.SetBindings(ctx, ids.accountID, []int64{channelID}); err != nil {
		t.Fatalf("SetBindings: %v", err)
	}
	// ruleID、err 用于本次流程后续判断的规则ID、err
	ruleID, err := store.Automation.Create(ctx, db.AutomationRuleInput{
		UserID: user.ID, CookieID: ids.accountID, ItemID: ids.itemID, Name: "付款发货",
		TriggerType: "order_paid", Enabled: false, Priority: 100,
	})
	if err != nil {
		t.Fatalf("Automation.Create: %v", err)
	}
	if // started、err 用于本次流程后续判断的started、err
	_, started, err := store.Automation.TryStartRun(ctx, db.AutomationRun{
		RuleID: ruleID, CookieID: ids.accountID, TriggerType: "order_paid", TriggerKey: ids.orderID, Status: "running",
	}); err != nil || !started {
		t.Fatalf("TryStartRun: started=%v err=%v", started, err)
	}

	if // err 用于本次流程后续判断的err
	err := cleanupVerifyData(ctx, store, ids); err != nil {
		t.Fatalf("cleanupVerifyData: %v", err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.Users.GetByUsername(ctx, ids.username); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("验证用户应被清理，err=%v", err)
	}
	if // err 用于本次流程后续判断的err
	_, err := store.Cookies.GetValue(ctx, ids.accountID); !errors.Is(err, db.ErrNotFound) {
		t.Fatalf("验证账号应被清理，err=%v", err)
	}
	// count 用于本次流程后续判断的数量
	var count int
	// name、query 表示当前遍历过程中的name、query
	for name, query := range map[string]string{
		"automation_runs":    `SELECT COUNT(*) FROM automation_runs WHERE cookie_id=? OR trigger_key=?`,
		"automation_rules":   `SELECT COUNT(*) FROM automation_rules WHERE cookie_id=? OR item_id=?`,
		"notification_links": `SELECT COUNT(*) FROM message_notifications WHERE cookie_id=?`,
		"orders":             `SELECT COUNT(*) FROM orders WHERE cookie_id=? OR order_id=?`,
		"items":              `SELECT COUNT(*) FROM item_info WHERE cookie_id=? OR item_id=?`,
	} {
		// err 用于本次流程后续判断的err
		var err error
		switch name {
		case "automation_runs", "orders":
			err = store.DB.QueryRowContext(ctx, query, ids.accountID, ids.orderID).Scan(&count)
		case "automation_rules", "items":
			err = store.DB.QueryRowContext(ctx, query, ids.accountID, ids.itemID).Scan(&count)
		default:
			err = store.DB.QueryRowContext(ctx, query, ids.accountID).Scan(&count)
		}
		if err != nil {
			t.Fatalf("count %s: %v", name, err)
		}
		if count != 0 {
			t.Fatalf("%s 未清理干净，count=%d", name, count)
		}
	}
}
