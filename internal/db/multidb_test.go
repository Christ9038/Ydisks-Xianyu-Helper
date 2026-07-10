package db

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/pressly/goose/v3"
)

// 本文件用真实 MySQL/Postgres 验证方言相关 SQL（UPSERT/INSERT IGNORE/RETURNING/
// NULL 扫描/布尔读写）。SQLite 始终内联运行；MySQL/Postgres 在对应环境变量提供时
// 自动创建一次性独立数据库运行，无则 t.Skip。
//
//	env TEST_MYSQL_URL=mysql://root:test123@tcp(localhost:3306)/xianyu
//	env TEST_POSTGRES_URL=postgres://xianyu:test123@localhost:5432/xianyu
//
// MySQL 连接需有 CREATE/DROP DATABASE 权限（用 root 或授权用户）；Postgres 用
// 初始化超户即可。独立数据库在测试结束自动 DROP，互不污染。

var multidbCounter uint64 // 生成一次性数据库名的原子计数器。

// TestMain 关闭 goose 默认日志，避免每个目标库的迁移输出刷屏测试结果。
func TestMain(m *testing.M) {
	goose.SetLogger(goose.NopLogger())
	os.Exit(m.Run())
}

// testTarget 是一个可被测试的数据库目标。
type testTarget struct {
	name    string
	dialect Dialect
	store   *Store
	cleanup func()
}

// allTestTargets 返回所有可用的测试目标。SQLite 永远包含；MySQL/Postgres 按环境变量追加。
func allTestTargets(t *testing.T) []testTarget {
	t.Helper()
	targets := []testTarget{sqliteTarget(t)}
	if u := os.Getenv("TEST_MYSQL_URL"); u != "" {
		targets = append(targets, mysqlTarget(t, u))
	}
	if u := os.Getenv("TEST_POSTGRES_URL"); u != "" {
		targets = append(targets, postgresTarget(t, u))
	}
	return targets
}

func sqliteTarget(t *testing.T) testTarget {
	t.Helper()
	dbPath := filepath.Join(t.TempDir(), "multidb.db")
	db, _, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return testTarget{name: "sqlite", dialect: DialectSQLite, store: NewStore(db, DialectSQLite), cleanup: func() { db.Close() }}
}

// mysqlTarget 在 MySQL 服务器上创建一次性数据库，跑迁移后返回 store。
// 测试结束 DROP 该库，保证隔离。
func mysqlTarget(t *testing.T, url string) testTarget {
	t.Helper()
	dsn := strings.TrimPrefix(url, "mysql://")
	slash := strings.LastIndex(dsn, "/")
	if slash < 0 {
		t.Fatalf("TEST_MYSQL_URL 缺少 /dbname: %s", url)
	}
	baseDSN := dsn[:slash] // user:pass@tcp(host:port)

	admin, err := sql.Open("mysql", baseDSN+"/")
	if err != nil {
		t.Fatalf("open mysql admin: %v", err)
	}
	dbName := fmt.Sprintf("xytest_%d", atomic.AddUint64(&multidbCounter, 1))
	if _, err := admin.Exec("DROP DATABASE IF EXISTS " + dbName); err != nil {
		t.Fatalf("drop stale mysql db: %v", err)
	}
	if _, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Fatalf("create mysql db: %v", err)
	}
	db, _, err := Open(context.Background(), "mysql://"+baseDSN+"/"+dbName)
	if err != nil {
		_, _ = admin.Exec("DROP DATABASE " + dbName)
		t.Fatalf("open mysql test db: %v", err)
	}
	cleanup := func() {
		db.Close()
		_, _ = admin.Exec("DROP DATABASE " + dbName)
		admin.Close()
	}
	return testTarget{name: "mysql", dialect: DialectMySQL, store: NewStore(db, DialectMySQL), cleanup: cleanup}
}

// postgresTarget 在 Postgres 服务器上创建一次性数据库。
// 连接到 maintenance 库（postgres）执行 CREATE DATABASE，再连到新库跑迁移。
func postgresTarget(t *testing.T, url string) testTarget {
	t.Helper()
	rest := strings.TrimPrefix(url, "postgres://")
	// rest = user:pass@host:port/xianyu
	slash := strings.LastIndex(rest, "/")
	if slash < 0 {
		t.Fatalf("TEST_POSTGRES_URL 缺少 /dbname: %s", url)
	}
	server := rest[:slash] // user:pass@host:port

	admin, err := sql.Open("pgx_compat", "postgres://"+server+"/postgres")
	if err != nil {
		t.Fatalf("open pg admin: %v", err)
	}
	dbName := fmt.Sprintf("xytest_%d", atomic.AddUint64(&multidbCounter, 1))
	if _, err := admin.Exec("DROP DATABASE IF EXISTS " + dbName); err != nil {
		t.Fatalf("drop stale pg db: %v", err)
	}
	if _, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Fatalf("create pg db: %v", err)
	}
	db, _, err := Open(context.Background(), "postgres://"+server+"/"+dbName)
	if err != nil {
		_, _ = admin.Exec("DROP DATABASE " + dbName)
		t.Fatalf("open pg test db: %v", err)
	}
	cleanup := func() {
		db.Close()
		_, _ = admin.Exec("DROP DATABASE " + dbName)
		admin.Close()
	}
	return testTarget{name: "postgres", dialect: DialectPostgres, store: NewStore(db, DialectPostgres), cleanup: cleanup}
}

// TestMultiDB_CookiesUpsertBool 验证 cookie UPSERT + auto_confirm 布尔读写跨三库一致。
func TestMultiDB_CookiesUpsertBool(t *testing.T) {
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			ctx := context.Background()
			s := tg.store

			// 先建用户（cookies.user_id 外键）。
			uid := tg.name + "_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if ok, err := s.Users.Create(ctx, uid, uid+"@e.com", "pw"); err != nil || !ok {
				t.Fatalf("create user: ok=%v err=%v", ok, err)
			}
			user, _ := s.Users.GetByUsername(ctx, uid)

			cid := tg.name + "_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
				t.Fatalf("Save: %v", err)
			}
			// 二次 Save 走 UPSERT 分支（更新 value）。
			if err := s.Cookies.Save(ctx, cid, "cv2", user.ID); err != nil {
				t.Fatalf("Save upsert: %v", err)
			}
			if v, err := s.Cookies.GetValue(ctx, cid); err != nil || v != "cv2" {
				t.Fatalf("GetValue=%q err=%v want cv2", v, err)
			}

			// auto_confirm 默认 true，关闭后读 false。
			if enabled, err := s.Cookies.GetAutoConfirm(ctx, cid); err != nil || !enabled {
				t.Fatalf("default auto_confirm=%v err=%v want true", enabled, err)
			}
			if _, err := s.DB.ExecContext(ctx,
				`UPDATE cookies SET auto_confirm=0 WHERE id=?`, cid); err != nil {
				t.Fatalf("disable auto_confirm: %v", err)
			}
			if enabled, err := s.Cookies.GetAutoConfirm(ctx, cid); err != nil || enabled {
				t.Fatalf("after disable auto_confirm=%v err=%v want false", enabled, err)
			}

			// pause_duration NULL → 默认 10。
			if pd := s.Cookies.GetPauseDuration(ctx, cid); pd != 10 {
				t.Fatalf("GetPauseDuration=%d want 10", pd)
			}
		})
	}
}

func TestMultiDB_SettingsQuoteKey(t *testing.T) {
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			ctx := context.Background()
			s := tg.store

			if err := s.Settings.Set(ctx, "theme_color", "green"); err != nil {
				t.Fatalf("Settings.Set: %v", err)
			}
			got, err := s.Settings.Get(ctx, "theme_color")
			if err != nil || got != "green" {
				t.Fatalf("Settings.Get=%q err=%v want green", got, err)
			}
			all, err := s.Settings.All(ctx)
			if err != nil || all["theme_color"] != "green" {
				t.Fatalf("Settings.All=%v err=%v", all, err)
			}

			username := tg.name + "_settings_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if ok, err := s.Users.Create(ctx, username, username+"@e.com", "pw"); err != nil || !ok {
				t.Fatalf("create user: ok=%v err=%v", ok, err)
			}
			user, _ := s.Users.GetByUsername(ctx, username)
			keyCol := dialectQuote(tg.dialect, "key")
			if _, err := s.DB.ExecContext(ctx,
				`INSERT INTO user_settings (user_id, `+keyCol+`, value, updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP)`+
					dialectUpsert(tg.dialect, []string{"user_id", keyCol}, map[string]string{
						"value":      "EXCLUDED.value",
						"updated_at": "CURRENT_TIMESTAMP",
					}),
				user.ID, "dashboard_range", "30"); err != nil {
				t.Fatalf("insert user_settings: %v", err)
			}
			var value string
			if err := s.DB.QueryRowContext(ctx,
				`SELECT value FROM user_settings WHERE user_id=? AND `+keyCol+`=?`,
				user.ID, "dashboard_range").Scan(&value); err != nil || value != "30" {
				t.Fatalf("select user_settings=%q err=%v want 30", value, err)
			}
		})
	}
}

// TestMultiDB_OrdersUpsertNullScan 验证订单部分字段 Upsert 后 Get 能正确扫描 NULL 列。
func TestMultiDB_OrdersUpsertNullScan(t *testing.T) {
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			ctx := context.Background()
			s := tg.store

			oid := tg.name + "_order_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// orders.cookie_id 外键 cookies.id，需先建账号。
			uid := tg.name + "_order_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			user, _ := s.Users.GetByUsername(ctx, uid)
			cid := tg.name + "_order_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
				t.Fatalf("Save cookie: %v", err)
			}
			// 首次 Upsert 只给少量字段，其余列留 NULL/默认。
			if err := s.Orders.Upsert(ctx, oid, OrderUpsertOpts{
				ItemID:   "i1",
				BuyerID:  "b1",
				Amount:   "12.50",
				CookieID: cid,
			}); err != nil {
				t.Fatalf("Upsert insert: %v", err)
			}
			got, err := s.Orders.Get(ctx, oid)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.ItemID != "i1" || got.Amount != "12.50" || got.OrderStatus != "unknown" {
				t.Fatalf("after insert order = %#v", got)
			}
			// 未提供的可空列应安全扫描为空串。
			if got.SpecName != "" || got.ReceiverCity != "" || got.ChatID != "" {
				t.Fatalf("NULL 列扫描异常: spec=%q city=%q chat=%q", got.SpecName, got.ReceiverCity, got.ChatID)
			}

			// 二次 Upsert 补字段（验证 UPDATE 分支不覆盖已有值）。
			if err := s.Orders.Upsert(ctx, oid, OrderUpsertOpts{
				OrderStatus:   "paid",
				ReceiverCity:  "杭州",
				ChatID:        "chat_1",
				SystemShipped: boolPtr(true),
			}); err != nil {
				t.Fatalf("Upsert update: %v", err)
			}
			got, err = s.Orders.Get(ctx, oid)
			if err != nil {
				t.Fatalf("Get after update: %v", err)
			}
			if got.OrderStatus != "paid" || got.ReceiverCity != "杭州" || got.ChatID != "chat_1" || !got.SystemShipped {
				t.Fatalf("after update order = %#v", got)
			}
			// 原有字段应保留。
			if got.ItemID != "i1" || got.Amount != "12.50" {
				t.Fatalf("更新覆盖了原值: item=%q amount=%q", got.ItemID, got.Amount)
			}
		})
	}
}

// TestMultiDB_ItemsUpsert 验证 item_info Upsert + 布尔开关跨三库一致。
func TestMultiDB_ItemsUpsert(t *testing.T) {
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			ctx := context.Background()
			s := tg.store

			cid := tg.name + "_item_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// item_info.cookie_id 外键 cookies.id，需先建账号。
			uid := tg.name + "_item_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			user, _ := s.Users.GetByUsername(ctx, uid)
			if err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
				t.Fatalf("Save cookie: %v", err)
			}
			if err := s.Items.Upsert(ctx, &ItemInfoRow{
				CookieID: cid, ItemID: "i1", ItemTitle: "标题", ItemPrice: "9.9",
			}); err != nil {
				t.Fatalf("Upsert: %v", err)
			}
			// 二次 Upsert 更新（同主键）。
			if err := s.Items.Upsert(ctx, &ItemInfoRow{
				CookieID: cid, ItemID: "i1", ItemTitle: "新标题", ItemPrice: "19.9", IsMultiSpec: true,
			}); err != nil {
				t.Fatalf("Upsert update: %v", err)
			}
			items, err := s.Items.AllForCookie(ctx, cid)
			if err != nil {
				t.Fatalf("AllForCookie: %v", err)
			}
			if len(items) != 1 || items[0].ItemTitle != "新标题" || items[0].ItemPrice != "19.9" || !items[0].IsMultiSpec {
				t.Fatalf("items = %#v", items)
			}
			// UpsertBasic 不覆盖已置的布尔开关。
			if err := s.Items.UpsertBasic(ctx, &ItemInfoRow{CookieID: cid, ItemID: "i1", ItemTitle: "basic标题"}); err != nil {
				t.Fatalf("UpsertBasic: %v", err)
			}
			items, _ = s.Items.AllForCookie(ctx, cid)
			if items[0].ItemTitle != "basic标题" || !items[0].IsMultiSpec {
				t.Fatalf("UpsertBasic 覆盖了 IsMultiSpec: %#v", items[0])
			}
		})
	}
}

// TestMultiDB_AutomationTryStartRunDedup 验证 TryStartRun 的 UNIQUE 防重：
// 同 rule_id + trigger_key 第二次插入应返回 started=false。
func TestMultiDB_AutomationTryStartRunDedup(t *testing.T) {
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			ctx := context.Background()
			s := tg.store

			uid := tg.name + "_auto_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			user, _ := s.Users.GetByUsername(ctx, uid)
			cid := tg.name + "_auto_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
				t.Fatalf("Save cookie: %v", err)
			}

			ruleID, err := s.Automation.Create(ctx, AutomationRuleInput{
				UserID:      user.ID,
				CookieID:    cid,
				ItemID:      "i1",
				Name:        "rule",
				TriggerType: "paid",
				Enabled:     true,
				Priority:    100,
				Actions: []AutomationActionInput{{
					ActionType:    "send_card",
					DeliveryCount: 1,
					Enabled:       true,
				}},
			})
			if err != nil {
				t.Fatalf("Create rule: %v", err)
			}

			run := AutomationRun{
				RuleID:      ruleID,
				CookieID:    cid,
				ItemID:      "i1",
				OrderID:     "o1",
				TriggerType: "paid",
				TriggerKey:  "paid:o1",
			}
			id1, started, err := s.Automation.TryStartRun(ctx, run)
			if err != nil || !started || id1 == 0 {
				t.Fatalf("首次 TryStartRun: id=%d started=%v err=%v", id1, started, err)
			}
			// 同 trigger_key 第二次必须被防重。
			id2, started2, err := s.Automation.TryStartRun(ctx, run)
			if err != nil || started2 || id2 != 0 {
				t.Fatalf("重复 TryStartRun 应被防重: id=%d started=%v err=%v", id2, started2, err)
			}
			// 不同 trigger_key 可再次启动。
			run.TriggerKey = "paid:o2"
			id3, started3, err := s.Automation.TryStartRun(ctx, run)
			if err != nil || !started3 || id3 == 0 {
				t.Fatalf("不同 trigger_key 应启动: id=%d started=%v err=%v", id3, started3, err)
			}

			// FinishRun 标记完成。
			if err := s.Automation.FinishRun(ctx, id1, "done", 1, ""); err != nil {
				t.Fatalf("FinishRun: %v", err)
			}
		})
	}
}

// TestMultiDB_Notifications 验证通知渠道创建 + 账号绑定读写。
func TestMultiDB_Notifications(t *testing.T) {
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			ctx := context.Background()
			s := tg.store

			uid := tg.name + "_notif_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			user, _ := s.Users.GetByUsername(ctx, uid)
			cid := tg.name + "_notif_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
				t.Fatalf("Save cookie: %v", err)
			}

			chID, err := s.Notifications.CreateChannel(ctx, &NotificationChannelRow{
				Name: "wh", Type: "webhook", Config: `{"url":"x"}`, Enabled: true, UserID: user.ID,
			})
			if err != nil || chID == 0 {
				t.Fatalf("CreateChannel: id=%d err=%v", chID, err)
			}
			channels, _ := s.Notifications.AllChannelsForUser(ctx, user.ID)
			if len(channels) != 1 || !channels[0].Enabled || channels[0].Config != `{"url":"x"}` {
				t.Fatalf("channels = %#v", channels)
			}
			if err := s.Notifications.SetBindings(ctx, cid, []int64{chID}); err != nil {
				t.Fatalf("SetBindings: %v", err)
			}
			bindings, _ := s.Notifications.AccountBindings(ctx, cid)
			if len(bindings) != 1 || bindings[0] != chID {
				t.Fatalf("bindings = %#v", bindings)
			}
			// 覆盖式重置绑定。
			if err := s.Notifications.SetBindings(ctx, cid, nil); err != nil {
				t.Fatalf("SetBindings clear: %v", err)
			}
			bindings, _ = s.Notifications.AccountBindings(ctx, cid)
			if len(bindings) != 0 {
				t.Fatalf("清空后 bindings = %#v", bindings)
			}
		})
	}
}

func TestMultiDB_Migration14DownUp(t *testing.T) {
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			subdir, gooseDialect := migrationTestSubdir(t, tg.dialect)
			if err := goose.SetDialect(gooseDialect); err != nil {
				t.Fatalf("set goose dialect: %v", err)
			}
			goose.SetBaseFS(migrationsFS)
			if err := goose.Down(tg.store.DB, "migrations/"+subdir); err != nil {
				t.Fatalf("migration 14 down: %v", err)
			}
			if columnExistsForDialect(t, tg.store.DB, tg.dialect, "notification_channels", "event_types") {
				t.Fatal("notification_channels.event_types should be removed after down")
			}
			if tableExistsForDialect(t, tg.store.DB, tg.dialect, "risk_control_logs") {
				t.Fatal("risk_control_logs should be removed after down")
			}

			if err := goose.Up(tg.store.DB, "migrations/"+subdir); err != nil {
				t.Fatalf("migration up after down: %v", err)
			}
			for _, c := range []struct {
				table string
				col   string
			}{
				{"notification_channels", "event_types"},
				{"message_notifications", "event_types"},
				{"scheduled_cookies_refresh_log", "step_details"},
				{"scheduled_login_renew_log", "updated_cookie_count"},
				{"scheduled_api_cookie_renew_log", "request_count"},
				{"risk_control_logs", "processing_status"},
			} {
				if !columnExistsForDialect(t, tg.store.DB, tg.dialect, c.table, c.col) {
					t.Fatalf("column missing after re-up: %s.%s", c.table, c.col)
				}
			}
		})
	}
}

// TestMultiDB_CardsCreateGet 验证 cards Create + Get 的 NULL 列扫描（含 nullable 字段）。
func TestMultiDB_CardsCreateGet(t *testing.T) {
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			ctx := context.Background()
			s := tg.store

			uid := tg.name + "_card_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			user, _ := s.Users.GetByUsername(ctx, uid)

			cf := &CardFull{
				Name:        "测试卡密",
				Type:        "text",
				TextContent: "ABC-123",
				Enabled:     true,
				UserID:      user.ID,
			}
			id, err := s.Cards.Create(ctx, cf)
			if err != nil || id == 0 {
				t.Fatalf("Create: id=%d err=%v", id, err)
			}
			got, err := s.Cards.Get(ctx, id)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.Name != "测试卡密" || got.TextContent != "ABC-123" || !got.Enabled {
				t.Fatalf("card = %#v", got)
			}
			// 未设置的可空列应安全扫描为空串。
			if got.ImageURL != "" || got.SpecName != "" || got.APIConfig != "" {
				t.Fatalf("NULL 列扫描异常: image=%q spec=%q api=%q", got.ImageURL, got.SpecName, got.APIConfig)
			}
		})
	}
}

func migrationTestSubdir(t *testing.T, dialect Dialect) (string, string) {
	t.Helper()
	switch dialect {
	case DialectSQLite:
		return "sqlite", "sqlite3"
	case DialectMySQL:
		return "mysql", "mysql"
	case DialectPostgres:
		return "postgres", "postgres"
	default:
		t.Fatalf("unknown dialect: %s", dialect)
		return "", ""
	}
}

func columnExistsForDialect(t *testing.T, db *sql.DB, dialect Dialect, table, col string) bool {
	t.Helper()
	var query string
	var args []any
	switch dialect {
	case DialectSQLite:
		rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatalf("pragma_table_info(%s): %v", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			var name string
			if err := rows.Scan(&name); err != nil {
				t.Fatalf("scan column name: %v", err)
			}
			if name == col {
				return true
			}
		}
		if err := rows.Err(); err != nil {
			t.Fatalf("column rows: %v", err)
		}
		return false
	case DialectMySQL:
		query = `SELECT COUNT(*) FROM information_schema.COLUMNS WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=? AND COLUMN_NAME=?`
		args = []any{table, col}
	case DialectPostgres:
		query = `SELECT COUNT(*) FROM information_schema.columns WHERE table_schema='public' AND table_name=? AND column_name=?`
		args = []any{table, col}
	default:
		t.Fatalf("unknown dialect: %s", dialect)
	}
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("column exists query %s.%s: %v", table, col, err)
	}
	return count > 0
}

func tableExistsForDialect(t *testing.T, db *sql.DB, dialect Dialect, table string) bool {
	t.Helper()
	var query string
	var args []any
	switch dialect {
	case DialectSQLite:
		query = `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`
		args = []any{table}
	case DialectMySQL:
		query = `SELECT COUNT(*) FROM information_schema.TABLES WHERE TABLE_SCHEMA=DATABASE() AND TABLE_NAME=?`
		args = []any{table}
	case DialectPostgres:
		query = `SELECT COUNT(*) FROM information_schema.tables WHERE table_schema='public' AND table_name=?`
		args = []any{table}
	default:
		t.Fatalf("unknown dialect: %s", dialect)
	}
	var count int
	if err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("table exists query %s: %v", table, err)
	}
	return count > 0
}

func boolPtr(b bool) *bool { return &b }
