package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

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

// requireMultiDBEnv 控制多数据库回归是否必须同时连接 MySQL 与 PostgreSQL。
// 未设置时，开发者可以只运行内置 SQLite；设为 1 时，缺少任一外部数据库配置会让门禁失败。
const requireMultiDBEnv = "REQUIRE_MULTIDB"

// TestMain 关闭 goose 默认日志，避免每个目标库的迁移输出刷屏测试结果。
func TestMain(m *testing.M) {
	goose.SetLogger(goose.NopLogger())
	os.Exit(m.Run())
}

// TestMultiDB_TargetMatrix 输出本次运行实际具备的数据库矩阵，避免 SQLite 单库通过被误读为三库证据。
// REQUIRE_MULTIDB=1 时，MySQL 与 PostgreSQL 必须同时配置 TEST_MYSQL_URL 和 TEST_POSTGRES_URL；
// 连接、迁移和敏感设置行为仍由各个 TestMultiDB_* 子测试实际验证。
func TestMultiDB_TargetMatrix(t *testing.T) {
	t.Run("sqlite", func(t *testing.T) {
		t.Log("SQLite：内置目标，所有多数据库回归都会执行")
	})

	// externalTargets 保存外部方言及其连接配置环境变量，供矩阵报告逐项输出。
	externalTargets := []struct {
		name string
		env  string
	}{
		{name: "mysql", env: "TEST_MYSQL_URL"},
		{name: "postgres", env: "TEST_POSTGRES_URL"},
	}
	// target 保存当前正在报告的外部数据库目标。
	for _, target := range externalTargets {
		// target 保存当前子测试闭包独占的数据库目标副本，避免循环变量复用。
		target := target
		t.Run(target.name, func(t *testing.T) {
			if strings.TrimSpace(os.Getenv(target.env)) == "" {
				if multiDBRequired() {
					t.Fatalf("多数据库门禁要求 %s，但环境变量 %s 未设置", target.name, target.env)
				}
				t.Skipf("未配置 %s；本次仅有 SQLite 证据，不能宣称 %s 实测通过", target.env, target.name)
			}
			t.Logf("%s：已配置 %s；实际连通性、迁移和业务回归由 TestMultiDB_* 验证", target.name, target.env)
		})
	}
}

// multiDBRequired 判断当前运行是否要求 MySQL 与 PostgreSQL 都必须可用。
// 只接受明确的 1/true/yes 值，避免普通开发环境中偶然继承的任意字符串改变门禁语义。
func multiDBRequired() bool {
	switch strings.ToLower(strings.TrimSpace(os.Getenv(requireMultiDBEnv))) {
	case "1", "true", "yes":
		return true
	default:
		return false
	}
}

// requireConfiguredExternalTargets 在实际创建测试数据库前检查严格矩阵门禁。
// 它只检查配置是否存在；URL 格式、数据库连通性和迁移结果仍由 mysqlTarget/postgresTarget 验证。
func requireConfiguredExternalTargets(t *testing.T) {
	t.Helper()
	if !multiDBRequired() {
		return
	}
	// missing 保存严格门禁下缺失的外部数据库目标名称。
	missing := make([]string, 0, 2)
	if strings.TrimSpace(os.Getenv("TEST_MYSQL_URL")) == "" {
		missing = append(missing, "MySQL(TEST_MYSQL_URL)")
	}
	if strings.TrimSpace(os.Getenv("TEST_POSTGRES_URL")) == "" {
		missing = append(missing, "PostgreSQL(TEST_POSTGRES_URL)")
	}
	if len(missing) > 0 {
		t.Fatalf("多数据库门禁未满足：缺少 %s；SQLite 通过不能替代外部方言实测", strings.Join(missing, ", "))
	}
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
	requireConfiguredExternalTargets(t)
	// targets 用于本次流程后续判断的targets
	targets := []testTarget{sqliteTarget(t)}
	if // u 用于本次流程后续判断的u
	u := os.Getenv("TEST_MYSQL_URL"); u != "" {
		targets = append(targets, mysqlTarget(t, u))
	}
	if // u 用于本次流程后续判断的u
	u := os.Getenv("TEST_POSTGRES_URL"); u != "" {
		targets = append(targets, postgresTarget(t, u))
	}
	return targets
}

// sqliteTarget 封装sqliteTarget业务协调。
func sqliteTarget(t *testing.T) testTarget {
	t.Helper()
	// dbPath 用于本次流程后续判断的db路径
	dbPath := filepath.Join(t.TempDir(), "multidb.db")
	// db、err 用于本次流程后续判断的db、err
	db, _, err := Open(context.Background(), dbPath)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	return testTarget{name: "sqlite", dialect: DialectSQLite, store: NewStore(db, DialectSQLite), cleanup: func() { db.Close() }}
}

// mysqlTarget 在 MySQL 服务器上创建一次性数据库，跑迁移后返回 store。
// 测试结束 DROP 该库，保证隔离。
// mysqlTarget 封装mysqlTarget业务协调。
func mysqlTarget(t *testing.T, url string) testTarget {
	t.Helper()
	// baseDSN、query 保存 MySQL 连接 authority 与查询参数，供临时库连接复用。
	baseDSN, query := externalTargetURLParts(t, "TEST_MYSQL_URL", url)

	// admin、err 用于本次流程后续判断的admin、err
	adminDSN := baseDSN + "/"
	if query != "" {
		adminDSN += "?" + query
	}
	// admin、err 保存管理员连接及打开错误；连接只用于创建和销毁临时数据库。
	admin, err := sql.Open("mysql", adminDSN)
	if err != nil {
		t.Fatalf("open mysql admin: %v", err)
	}
	// dbName 用于本次流程后续判断的db名称
	dbName := fmt.Sprintf("xytest_%d", atomic.AddUint64(&multidbCounter, 1))
	if // err 用于本次流程后续判断的err
	_, err := admin.Exec("DROP DATABASE IF EXISTS " + dbName); err != nil {
		t.Fatalf("drop stale mysql db: %v", err)
	}
	if // err 用于本次流程后续判断的err
	_, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Fatalf("create mysql db: %v", err)
	}
	// db、err 保存临时 MySQL 数据库连接及打开错误；query 保留 SSL、时区等测试配置。
	db, _, err := Open(context.Background(), externalTargetDSN("mysql", baseDSN, dbName, query))
	if err != nil {
		_, _ = admin.Exec("DROP DATABASE " + dbName)
		t.Fatalf("open mysql test db: %v", err)
	}
	// cleanup 用于本次流程后续判断的cleanup
	cleanup := func() {
		db.Close()
		_, _ = admin.Exec("DROP DATABASE " + dbName)
		admin.Close()
	}
	return testTarget{name: "mysql", dialect: DialectMySQL, store: NewStore(db, DialectMySQL), cleanup: cleanup}
}

// postgresTarget 在 Postgres 服务器上创建一次性数据库。
// 连接到 maintenance 库（postgres）执行 CREATE DATABASE，再连到新库跑迁移。
// postgresTarget 封装postgresTarget业务协调。
func postgresTarget(t *testing.T, url string) testTarget {
	t.Helper()
	// server、query 保存 PostgreSQL 连接 authority 与查询参数，供临时库连接复用。
	server, query := externalTargetURLParts(t, "TEST_POSTGRES_URL", url)

	// adminDSN 保存维护库连接地址；查询参数用于保持外部数据库测试的 SSL 与时区配置。
	adminDSN := externalTargetDSN("postgres", server, "postgres", query)
	// admin、err 保存 PostgreSQL 管理员连接及打开错误；管理员连接只负责临时库生命周期。
	admin, err := sql.Open("pgx_compat", adminDSN)
	if err != nil {
		t.Fatalf("open pg admin: %v", err)
	}
	// dbName 用于本次流程后续判断的db名称
	dbName := fmt.Sprintf("xytest_%d", atomic.AddUint64(&multidbCounter, 1))
	if // err 用于本次流程后续判断的err
	_, err := admin.Exec("DROP DATABASE IF EXISTS " + dbName); err != nil {
		t.Fatalf("drop stale pg db: %v", err)
	}
	if // err 用于本次流程后续判断的err
	_, err := admin.Exec("CREATE DATABASE " + dbName); err != nil {
		t.Fatalf("create pg db: %v", err)
	}
	// db、err 保存临时 PostgreSQL 数据库连接及打开错误；query 保留 sslmode 与时区配置。
	db, _, err := Open(context.Background(), externalTargetDSN("postgres", server, dbName, query))
	if err != nil {
		_, _ = admin.Exec("DROP DATABASE " + dbName)
		t.Fatalf("open pg test db: %v", err)
	}
	// cleanup 用于本次流程后续判断的cleanup
	cleanup := func() {
		db.Close()
		_, _ = admin.Exec("DROP DATABASE " + dbName)
		admin.Close()
	}
	return testTarget{name: "postgres", dialect: DialectPostgres, store: NewStore(db, DialectPostgres), cleanup: cleanup}
}

// externalTargetURLParts 拆分外部数据库 URL 的 authority 与查询参数。
// 它不返回数据库名，只校验 URL 含有数据库名；临时数据库会替换原名称，但保留 sslmode、时区等参数。
func externalTargetURLParts(t *testing.T, envName, rawURL string) (string, string) {
	t.Helper()
	// scheme 保存调用方要求的数据库 URL scheme，避免把环境变量后缀 URL 误当成连接 scheme。
	scheme := strings.TrimPrefix(envName, "TEST_")
	scheme = strings.TrimSuffix(scheme, "_URL")
	scheme = strings.ToLower(scheme)
	// authority、query、err 保存 URL 拆分得到的连接 authority、查询参数及格式错误。
	authority, query, err := splitExternalTargetURL(rawURL, scheme)
	if err != nil {
		t.Fatalf("%s 格式无效：%v", envName, err)
	}
	return authority, query
}

// splitExternalTargetURL 解析测试数据库 URL，返回 authority 和查询参数。
// 错误消息只描述格式，不回显原始 URL，避免测试失败输出泄露连接凭证。
func splitExternalTargetURL(rawURL, scheme string) (string, string, error) {
	// prefix 保存期望的 URL scheme 前缀，供格式校验使用。
	prefix := scheme + "://"
	if !strings.HasPrefix(rawURL, prefix) {
		return "", "", fmt.Errorf("必须使用 %s scheme", scheme)
	}
	// rest 保存去除 scheme 后的 authority、数据库名和查询参数。
	rest := strings.TrimPrefix(rawURL, prefix)
	// slash 保存 authority 与数据库路径之间的分隔位置。
	slash := strings.LastIndex(rest, "/")
	if slash <= 0 || slash == len(rest)-1 {
		return "", "", fmt.Errorf("必须包含 authority 和数据库名")
	}
	// authority 保存用户、密码和主机信息；只在连接字符串内部传递，不写入日志。
	authority := rest[:slash]
	// databasePart 保存数据库名及其查询参数，供后续拆分。
	databasePart := rest[slash+1:]
	// databaseName、query、hasQuery 保存数据库名、查询参数及是否存在问号。
	databaseName, query, hasQuery := strings.Cut(databasePart, "?")
	if databaseName == "" {
		return "", "", fmt.Errorf("数据库名不能为空")
	}
	if hasQuery && query == "" {
		return "", "", fmt.Errorf("查询参数不能为空")
	}
	return authority, query, nil
}

// externalTargetDSN 为外部数据库临时库拼接保留原查询参数的连接 URL。
// databaseName 是每次测试生成的隔离库名；query 可能包含 sslmode、时区或驱动选项。
func externalTargetDSN(scheme, authority, databaseName, query string) string {
	// dsn 保存最终连接 URL；其中 authority 可能包含凭证，但调用方不得打印它。
	dsn := scheme + "://" + authority + "/" + databaseName
	if query != "" {
		dsn += "?" + query
	}
	return dsn
}

// TestSplitExternalTargetURLPreservesQuery 验证三数据库测试 URL 替换临时库名时保留驱动参数。
func TestSplitExternalTargetURLPreservesQuery(t *testing.T) {
	// cases 保存不同方言 URL 及其应保留的 authority、查询参数。
	cases := []struct {
		name      string
		rawURL    string
		scheme    string
		authority string
		query     string
	}{
		{name: "mysql", rawURL: "mysql://user:secret@tcp(host:3306)/xianyu?parseTime=true&loc=UTC", scheme: "mysql", authority: "user:secret@tcp(host:3306)", query: "parseTime=true&loc=UTC"},
		{name: "postgres", rawURL: "postgres://user:secret@host:5432/xianyu?sslmode=disable&timezone=UTC", scheme: "postgres", authority: "user:secret@host:5432", query: "sslmode=disable&timezone=UTC"},
	}
	// testCase 保存当前 URL 解析用例，供子测试闭包使用。
	for _, testCase := range cases {
		// testCase 保存当前子测试闭包独占的用例副本，避免循环变量复用。
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			// authority、query、err 保存 URL 拆分结果及错误。
			authority, query, err := splitExternalTargetURL(testCase.rawURL, testCase.scheme)
			if err != nil {
				t.Fatalf("split URL: %v", err)
			}
			if authority != testCase.authority || query != testCase.query {
				t.Fatalf("split URL 未保留预期的 authority/query 结构")
			}
			// dsn 保存替换临时数据库名后的连接 URL，确认查询参数未丢失。
			dsn := externalTargetDSN(testCase.scheme, authority, "xytest_1", query)
			if !strings.HasSuffix(dsn, "/xytest_1?"+testCase.query) {
				t.Fatalf("temporary DSN 未保留查询参数")
			}
		})
	}
}

// TestSplitExternalTargetURLDoesNotEchoSecrets 验证错误信息不会回显数据库 URL 中的密码。
func TestSplitExternalTargetURLDoesNotEchoSecrets(t *testing.T) {
	// secretURL 保存格式错误且包含密码的测试 URL；错误信息不得包含该密码。
	secretURL := "postgres://user:super-secret@host:5432"
	// _, _, err 保存解析失败结果及错误。
	_, _, err := splitExternalTargetURL(secretURL, "postgres")
	if err == nil {
		t.Fatal("缺少数据库名时应返回错误")
	}
	if strings.Contains(err.Error(), "super-secret") {
		t.Fatalf("格式错误不应回显连接密码: %v", err)
	}
}

// TestMultiDB_CookiesUpsertBool 验证 cookie UPSERT + auto_confirm 布尔读写跨三库一致。
func TestMultiDB_CookiesUpsertBool(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store

			// 先建用户（cookies.user_id 外键）。
			uid := tg.name + "_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if // ok、err 用于本次流程后续判断的ok、err
			ok, err := s.Users.Create(ctx, uid, uid+"@e.com", "pw"); err != nil || !ok {
				t.Fatalf("create user: ok=%v err=%v", ok, err)
			}
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, uid)

			// cid 用于本次流程后续判断的cid
			cid := tg.name + "_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if // err 用于本次流程后续判断的err
			err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
				t.Fatalf("Save: %v", err)
			}
			// 二次 Save 走 UPSERT 分支（更新 value）。
			if err := s.Cookies.Save(ctx, cid, "cv2", user.ID); err != nil {
				t.Fatalf("Save upsert: %v", err)
			}
			if // v、err 用于本次流程后续判断的v、err
			v, err := s.Cookies.GetValue(ctx, cid); err != nil || v != "cv2" {
				t.Fatalf("GetValue=%q err=%v want cv2", v, err)
			}

			// auto_confirm 默认 true，关闭后读 false。
			if enabled, err := s.Cookies.GetAutoConfirm(ctx, cid); err != nil || !enabled {
				t.Fatalf("default auto_confirm=%v err=%v want true", enabled, err)
			}
			if // err 用于本次流程后续判断的err
			_, err := s.DB.ExecContext(ctx,
				`UPDATE cookies SET auto_confirm=0 WHERE id=?`, cid); err != nil {
				t.Fatalf("disable auto_confirm: %v", err)
			}
			if // enabled、err 用于本次流程后续判断的enabled、err
			enabled, err := s.Cookies.GetAutoConfirm(ctx, cid); err != nil || enabled {
				t.Fatalf("after disable auto_confirm=%v err=%v want false", enabled, err)
			}

			// pause_duration NULL → 默认 10。
			if pd := s.Cookies.GetPauseDuration(ctx, cid); pd != 10 {
				t.Fatalf("GetPauseDuration=%d want 10", pd)
			}
		})
	}
}

// TestMultiDB_ReliabilityStateAndSearch 封装TestMultiDBReliability状态And搜索业务协调。
func TestMultiDB_ReliabilityStateAndSearch(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store
			// suffix 用于本次流程后续判断的suffix
			suffix := fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// username 用于本次流程后续判断的username
			username := tg.name + "_reliability_" + suffix
			if // ok、err 用于本次流程后续判断的ok、err
			ok, err := s.Users.Create(ctx, username, username+"@e.com", "pw"); err != nil || !ok {
				t.Fatalf("create user: ok=%v err=%v", ok, err)
			}
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, username)
			// cookieID 用于本次流程后续判断的登录凭证ID
			cookieID := tg.name + "_reliability_cookie_" + suffix
			if // err 用于本次流程后续判断的err
			err := s.Cookies.Save(ctx, cookieID, "unb=test", user.ID); err != nil {
				t.Fatal(err)
			}
			// keywordID、err 用于本次流程后续判断的关键词ID、err
			keywordID, err := s.Keywords.Add(ctx, cookieID, "same", "same reply", "", "text", "")
			if err != nil {
				t.Fatal(err)
			}
			// unchangedKeyword 用于本次流程后续判断的unchanged关键词
			unchangedKeyword := KeywordRow{ID: keywordID, CookieID: cookieID, Keyword: "same", Reply: "same reply", Type: "text"}
			if // err 用于本次流程后续判断的err
			err := s.Keywords.UpdateByID(ctx, unchangedKeyword); err != nil {
				t.Fatalf("no-op keyword update must succeed on %s: %v", tg.name, err)
			}
			if // err 用于本次流程后续判断的err
			_, err := s.Cookies.SetPause(ctx, cookieID, 1); err != nil {
				t.Fatal(err)
			}
			if // paused、err 用于本次流程后续判断的paused、err
			paused, _, err := s.Cookies.IsPaused(ctx, cookieID); err != nil || !paused {
				t.Fatalf("pause state: paused=%v err=%v", paused, err)
			}

			// batchID 用于本次流程后续判断的批次ID
			batchID := tg.name + "_batch_" + suffix
			if // err 用于本次流程后续判断的err
			err := s.PublishBatches.Create(ctx, &ItemPublishBatch{
				ID: batchID, UserID: user.ID, DefaultCookieID: cookieID, Filename: "test.csv", Status: "pending",
			}, []ItemPublishBatchRow{{RowNo: 1, CookieID: cookieID, Title: "item", Price: "1"}}); err != nil {
				t.Fatal(err)
			}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.PublishBatches.ClaimBatch(ctx, batchID, "worker", time.Now().UTC().Add(time.Minute).Unix()); err != nil || !claimed {
				t.Fatalf("claim batch: claimed=%v err=%v", claimed, err)
			}
			// batchRows 用于本次流程后续判断的批次Rows
			batchRows, _ := s.PublishBatches.Rows(ctx, batchID)
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.PublishBatches.ClaimRow(ctx, batchRows[0].ID, "worker"); err != nil || !claimed {
				t.Fatalf("claim row: claimed=%v err=%v", claimed, err)
			}
			if // marked、err 用于本次流程后续判断的marked、err
			marked, err := s.PublishBatches.MarkClaimedRowSuccess(ctx, batchRows[0].ID, "worker", "published", "", "{}"); err != nil || !marked {
				t.Fatalf("mark row: marked=%v err=%v", marked, err)
			}
			if // finished、err 用于本次流程后续判断的finished、err
			finished, err := s.PublishBatches.FinishBatchStatus(ctx, batchID, "worker", "completed"); err != nil || !finished {
				t.Fatalf("finish batch: finished=%v err=%v", finished, err)
			}
			// cancelBatchID 用于本次流程后续判断的取消批次ID
			cancelBatchID := tg.name + "_cancel_batch_" + suffix
			if // err 用于本次流程后续判断的err
			err := s.PublishBatches.Create(ctx, &ItemPublishBatch{
				ID: cancelBatchID, UserID: user.ID, DefaultCookieID: cookieID, Filename: "test.csv", Status: "pending",
			}, []ItemPublishBatchRow{{RowNo: 1, CookieID: cookieID, Title: "cancel", Price: "1"}}); err != nil {
				t.Fatal(err)
			}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.PublishBatches.ClaimBatch(ctx, cancelBatchID, "cancel-worker", time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
				t.Fatalf("claim cancel batch=%v err=%v", claimed, err)
			}
			if // token、running、err 用于本次流程后续判断的token、running、err
			token, running, err := s.PublishBatches.RequestCancel(ctx, cancelBatchID); err != nil || !running || token != "cancel-worker" {
				t.Fatalf("request cancel token=%q running=%v err=%v", token, running, err)
			}
			if // finalized、err 用于本次流程后续判断的finalized、err
			finalized, err := s.PublishBatches.FinalizeCanceled(ctx, cancelBatchID, "cancel-worker"); err != nil || !finalized {
				t.Fatalf("finalize cancel=%v err=%v", finalized, err)
			}

			// uncertainBatchID 用于本次流程后续判断的uncertain批次ID
			uncertainBatchID := tg.name + "_uncertain_batch_" + suffix
			if // err 用于本次流程后续判断的err
			err := s.PublishBatches.Create(ctx, &ItemPublishBatch{
				ID: uncertainBatchID, UserID: user.ID, DefaultCookieID: cookieID, Filename: "test.csv", Status: "pending",
			}, []ItemPublishBatchRow{{RowNo: 1, CookieID: cookieID, Title: "item", Price: "1"}}); err != nil {
				t.Fatal(err)
			}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.PublishBatches.ClaimBatch(ctx, uncertainBatchID, "old", 1); err != nil || !claimed {
				t.Fatalf("claim uncertain batch=%v err=%v", claimed, err)
			}
			// uncertainRows 用于本次流程后续判断的uncertainRows
			uncertainRows, _ := s.PublishBatches.Rows(ctx, uncertainBatchID)
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.PublishBatches.ClaimRow(ctx, uncertainRows[0].ID, "old"); err != nil || !claimed {
				t.Fatalf("claim uncertain row=%v err=%v", claimed, err)
			}
			if // marked、err 用于本次流程后续判断的marked、err
			marked, err := s.PublishBatches.MarkClaimedRemoteStarted(ctx, uncertainRows[0].ID, "old"); err != nil || !marked {
				t.Fatalf("mark remote started=%v err=%v", marked, err)
			}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.PublishBatches.ClaimBatch(ctx, uncertainBatchID, "new", time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
				t.Fatalf("take over uncertain batch=%v err=%v", claimed, err)
			}
			uncertainRows, _ = s.PublishBatches.Rows(ctx, uncertainBatchID)
			if uncertainRows[0].Status != "failed" || uncertainRows[0].FailureKind != "uncertain_remote" {
				t.Fatalf("uncertain row=%+v", uncertainRows[0])
			}

			// ruleID、err 用于本次流程后续判断的规则ID、err
			ruleID, err := s.Automation.Create(ctx, AutomationRuleInput{UserID: user.ID, CookieID: cookieID, Name: "issue",
				TriggerType: "buyer_reviewed", Enabled: true,
				Actions: []AutomationActionInput{{ActionType: "send_text", MessageTemplate: "x", Enabled: true}}})
			if err != nil {
				t.Fatal(err)
			}
			// runID、started、err 用于本次流程后续判断的运行ID、started、err
			runID, started, err := s.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cookieID,
				TriggerType: "buyer_reviewed", TriggerKey: "issue-" + suffix, RawEventJSON: `{}`, LeaseExpiresAt: 1})
			if err != nil || !started {
				t.Fatalf("start issue run=%v err=%v", started, err)
			}
			if // ok、err 用于本次流程后续判断的ok、err
			ok, err := s.Automation.StartRunAction(ctx, runID, 1, 0, 1); err != nil || !ok {
				t.Fatalf("start issue action=%v err=%v", ok, err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.QuarantineRunResult(ctx, runID, 1, 1, "unknown"); err != nil {
				t.Fatal(err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.Delete(ctx, user.ID, ruleID); err != ErrAutomationRunActive {
				t.Fatalf("active rule delete err=%v", err)
			}
			// runIssues、err 用于本次流程后续判断的运行Issues、err
			runIssues, _, err := s.Automation.ListIssues(ctx, user.ID)
			if err != nil || len(runIssues) != 1 {
				t.Fatalf("issues=%+v err=%v", runIssues, err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.ResolveRunIssue(ctx, user.ID, runID, "cancel"); err != nil {
				t.Fatal(err)
			}
			// fencedRunID、started、err 用于本次流程后续判断的fenced运行ID、started、err
			fencedRunID, started, err := s.Automation.TryStartRun(ctx, AutomationRun{RuleID: ruleID, CookieID: cookieID,
				TriggerType: "buyer_reviewed", TriggerKey: "fenced-" + suffix, RawEventJSON: `{}`})
			if err != nil || !started {
				t.Fatalf("start fenced run=%v err=%v", started, err)
			}
			if // err 用于本次流程后续判断的err
			_, err := s.DB.ExecContext(ctx, `UPDATE automation_runs SET lease_expires_at=0 WHERE id=?`, fencedRunID); err != nil {
				t.Fatal(err)
			}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.Automation.ClaimRecoveryRun(ctx, fencedRunID, time.Now().Add(time.Minute).Unix()); err != nil || !claimed {
				t.Fatalf("claim fenced run=%v err=%v", claimed, err)
			}
			// fencedRun、err 用于本次流程后续判断的fencedRun、err
			fencedRun, err := s.Automation.GetRun(ctx, fencedRunID)
			if err != nil || fencedRun.AttemptCount != 2 {
				t.Fatalf("fenced run=%+v err=%v", fencedRun, err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.FinishRun(ctx, fencedRunID, 1, "failed", 0, "stale"); !errors.Is(err, ErrAutomationRunLeaseLost) {
				t.Fatalf("stale finish err=%v", err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.FinishRun(ctx, fencedRunID, fencedRun.AttemptCount, "success", 0, ""); err != nil {
				t.Fatal(err)
			}
			// deferred 用于本次流程后续判断的deferred
			deferred := DeferredAutomationTask{TaskKey: "dead-" + suffix, CookieID: cookieID, TriggerType: "buyer_reviewed", TaskJSON: `{}`, DueAt: 0}
			if // err 用于本次流程后续判断的err
			err := s.Automation.DeferTask(ctx, deferred); err != nil {
				t.Fatal(err)
			}
			_, _ = s.DB.ExecContext(ctx, `UPDATE automation_pending_tasks SET status='dead_letter',attempt_count=5 WHERE task_key=?`, deferred.TaskKey)
			if // err 用于本次流程后续判断的err
			err := s.Automation.DeferTask(ctx, deferred); err != nil {
				t.Fatal(err)
			}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.Automation.ClaimDueDeferredTasks(ctx, 10); err != nil || len(claimed) != 1 {
				t.Fatalf("revived deferred=%+v err=%v", claimed, err)
			}

			// itemID 用于本次流程后续判断的商品ID
			itemID := "search-item-" + suffix
			// orderID 用于本次流程后续判断的订单ID
			orderID := "search-order-" + suffix
			if // err 用于本次流程后续判断的err
			err := s.Items.Upsert(ctx, &ItemInfoRow{CookieID: cookieID, ItemID: itemID, ItemTitle: "Cross Database Search"}); err != nil {
				t.Fatal(err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Orders.Upsert(ctx, orderID, OrderUpsertOpts{CookieID: cookieID, ItemID: itemID, Amount: "9.9"}); err != nil {
				t.Fatal(err)
			}
			// empty 用于本次流程后续判断的empty
			empty := ""
			if // err 用于本次流程后续判断的err
			err := s.Orders.Patch(ctx, orderID, OrderPatch{Amount: &empty}); err != nil {
				t.Fatal(err)
			}
			// orders、total、err 用于本次流程后续判断的orders、total、err
			orders, total, err := s.Orders.ListForUser(ctx, OrderListFilter{UserID: user.ID, Search: "cross database", Limit: 10})
			if err != nil || total != 1 || len(orders) != 1 || orders[0].Amount != "" {
				t.Fatalf("search/patch orders=%+v total=%d err=%v", orders, total, err)
			}
		})
	}
}

// TestMultiDB_SettingsQuoteKey 封装TestMultiDB设置QuoteKey业务协调。
func TestMultiDB_SettingsQuoteKey(t *testing.T) {
	t.Setenv("XIANYU_DATA_KEY", "multidb-settings-secret-key")
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store

			if // err 用于本次流程后续判断的err
			err := s.Settings.Set(ctx, "theme_color", "green"); err != nil {
				t.Fatalf("Settings.Set: %v", err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Settings.SetMany(ctx, map[string]string{"theme_color": "blue", "bulk_key": "bulk_value"}); err != nil {
				t.Fatalf("Settings.SetMany: %v", err)
			}
			if // got 用于本次流程后续判断的got
			got, _ := s.Settings.Get(ctx, "theme_color"); got != "blue" {
				t.Fatalf("SetMany theme_color=%q", got)
			}
			// got、err 用于本次流程后续判断的got、err
			got, err := s.Settings.Get(ctx, "theme_color")
			if err != nil || got != "blue" {
				t.Fatalf("Settings.Get=%q err=%v want blue", got, err)
			}
			// err 是敏感配置 replace 命令的应用错误。
			if err := s.Settings.ApplyChanges(ctx, nil, map[string]SensitiveSettingChange{
				"ai_api_key": {Action: "replace", Value: tg.name + "-secret"},
			}); err != nil {
				t.Fatalf("Settings.ApplyChanges replace: %v", err)
			}
			// secret、err 是敏感配置读取结果及错误。
			secret, err := s.Settings.Get(ctx, "ai_api_key")
			if err != nil || secret != tg.name+"-secret" {
				t.Fatalf("Settings.Get secret=%q err=%v", secret, err)
			}
			// err 是敏感配置 retain 命令的应用错误。
			if err := s.Settings.ApplyChanges(ctx, nil, map[string]SensitiveSettingChange{
				"ai_api_key": {Action: "retain"},
			}); err != nil {
				t.Fatalf("Settings.ApplyChanges retain: %v", err)
			}
			// redacted、err 是敏感配置脱敏视图及错误。
			redacted, err := s.Settings.Redacted(ctx)
			if err != nil || redacted["ai_api_key"] != "" || redacted["ai_api_key_configured"] != "true" {
				t.Fatalf("Settings.Redacted=%#v err=%v", redacted, err)
			}
			// err 是敏感配置 clear 命令的应用错误。
			if err := s.Settings.ApplyChanges(ctx, nil, map[string]SensitiveSettingChange{
				"ai_api_key": {Action: "clear"},
			}); err != nil {
				t.Fatalf("Settings.ApplyChanges clear: %v", err)
			}
			// secret、err 是清除后的敏感配置读取结果及错误。
			secret, err = s.Settings.Get(ctx, "ai_api_key")
			if err != nil || secret != "" {
				t.Fatalf("Settings.Get cleared secret=%q err=%v", secret, err)
			}
			// all、err 用于本次流程后续判断的all、err
			all, err := s.Settings.All(ctx)
			if err != nil || all["theme_color"] != "blue" || all["bulk_key"] != "bulk_value" {
				t.Fatalf("Settings.All=%v err=%v", all, err)
			}

			// username 用于本次流程后续判断的username
			username := tg.name + "_settings_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if // ok、err 用于本次流程后续判断的ok、err
			ok, err := s.Users.Create(ctx, username, username+"@e.com", "pw"); err != nil || !ok {
				t.Fatalf("create user: ok=%v err=%v", ok, err)
			}
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, username)
			// keyCol 用于本次流程后续判断的keyCol
			keyCol := dialectQuote(tg.dialect, "key")
			if // err 用于本次流程后续判断的err
			_, err := s.DB.ExecContext(ctx,
				`INSERT INTO user_settings (user_id, `+keyCol+`, value, updated_at) VALUES (?,?,?,CURRENT_TIMESTAMP)`+
					dialectUpsert(tg.dialect, []string{"user_id", keyCol}, map[string]string{
						"value":      "EXCLUDED.value",
						"updated_at": "CURRENT_TIMESTAMP",
					}),
				user.ID, "dashboard_range", "30"); err != nil {
				t.Fatalf("insert user_settings: %v", err)
			}
			// value 用于本次流程后续判断的值
			var value string
			if // err 用于本次流程后续判断的err
			err := s.DB.QueryRowContext(ctx,
				`SELECT value FROM user_settings WHERE user_id=? AND `+keyCol+`=?`,
				user.ID, "dashboard_range").Scan(&value); err != nil || value != "30" {
				t.Fatalf("select user_settings=%q err=%v want 30", value, err)
			}
		})
	}
}

// TestMultiDB_SecurityAudit 验证敏感访问审计表和读写语义在各数据库方言可用。
func TestMultiDB_SecurityAudit(t *testing.T) {
	// tg 表示当前数据库方言测试目标。
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 保存当前方言审计测试上下文。
			ctx := context.Background()
			// err 保存当前方言审计记录写入错误。
			if err := tg.store.SecurityAudit.Add(ctx, SecurityAuditLog{UserID: 1, Action: "settings.read", Resource: "system_settings", Keys: []string{"ai_api_key"}, CreatedAt: 7}); err != nil {
				t.Fatalf("SecurityAudit.Add: %v", err)
			}
			// records、err 保存当前方言读取到的审计记录及错误。
			records, err := tg.store.SecurityAudit.ListByUser(ctx, 1, 10)
			if err != nil || len(records) != 1 || records[0].Keys[0] != "ai_api_key" {
				t.Fatalf("SecurityAudit.ListByUser records=%+v err=%v", records, err)
			}
		})
	}
}

// TestMultiDB_OrdersUpsertNullScan 验证订单部分字段 Upsert 后 Get 能正确扫描 NULL 列。
func TestMultiDB_OrdersUpsertNullScan(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store

			// oid 用于本次流程后续判断的oid
			oid := tg.name + "_order_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// orders.cookie_id 外键 cookies.id，需先建账号。
			uid := tg.name + "_order_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, uid)
			// cid 用于本次流程后续判断的cid
			cid := tg.name + "_order_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if // err 用于本次流程后续判断的err
			err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
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
			// got、err 用于本次流程后续判断的got、err
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

// TestMultiDB_OrderPatchUpdatesTimestampOnce 验证订单补丁在三方言不会重复赋值同一时间列。
func TestMultiDB_OrderPatchUpdatesTimestampOnce(t *testing.T) {
	// tg 表示当前执行订单补丁回归的数据库方言目标。
	for _, tg := range allTestTargets(t) {
		// tg 保存当前子测试闭包独占的方言目标，避免循环变量复用。
		tg := tg
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 保存当前方言订单补丁调用的数据库上下文。
			ctx := context.Background()
			// suffix 保存当前临时数据库内唯一的测试数据后缀。
			suffix := fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// username 保存测试订单所属用户的唯一登录名。
			username := tg.name + "_patch_" + suffix
			// created、createErr 保存用户创建结果及持久化错误。
			created, createErr := tg.store.Users.Create(ctx, username, username+"@e.com", "pw")
			if createErr != nil || !created {
				t.Fatalf("create user created=%v err=%v", created, createErr)
			}
			// user、userErr 保存测试用户身份及读取错误，用于建立订单账号外键。
			user, userErr := tg.store.Users.GetByUsername(ctx, username)
			if userErr != nil {
				t.Fatalf("get user: %v", userErr)
			}
			// cookieID 保存订单账号外键的唯一标识。
			cookieID := tg.name + "_patch_cookie_" + suffix
			// cookieErr 保存账号写入错误，订单必须绑定已存在账号。
			cookieErr := tg.store.Cookies.Save(ctx, cookieID, "cv", user.ID)
			if cookieErr != nil {
				t.Fatalf("save cookie: %v", cookieErr)
			}
			// orderID 保存待补丁订单的唯一业务标识。
			orderID := tg.name + "_patch_order_" + suffix
			// upsertErr 保存订单初始写入错误。
			upsertErr := tg.store.Orders.Upsert(ctx, orderID, OrderUpsertOpts{CookieID: cookieID, OrderStatus: "pending_ship", Amount: "1.00"})
			if upsertErr != nil {
				t.Fatalf("seed order: %v", upsertErr)
			}
			// patchedStatus 保存用户显式提交的订单状态补丁。
			patchedStatus := "shipped"
			// patchErr 保存跨方言订单补丁的 SQL 执行错误。
			patchErr := tg.store.Orders.Patch(ctx, orderID, OrderPatch{OrderStatus: &patchedStatus})
			if patchErr != nil {
				t.Fatalf("patch order: %v", patchErr)
			}
			// order、getErr 保存补丁后的订单状态及读取错误。
			order, getErr := tg.store.Orders.Get(ctx, orderID)
			if getErr != nil || order.OrderStatus != "shipped" {
				t.Fatalf("patched order=%+v err=%v", order, getErr)
			}
		})
	}
}

// TestMultiDB_OrderReconciliationIdempotency 验证补偿记录的幂等键在三方言都能防止重复人工核对。
func TestMultiDB_OrderReconciliationIdempotency(t *testing.T) {
	// tg 表示当前执行补偿记录回归的数据库方言目标。
	for _, tg := range allTestTargets(t) {
		// tg 保存当前子测试闭包独占的方言目标，避免循环变量复用。
		tg := tg
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 保存当前方言的补偿记录写入上下文。
			ctx := context.Background()
			// firstID、firstErr 保存首次外部成功后本地失败创建的补偿记录标识及错误。
			firstID, firstErr := tg.store.Reconciliations.CreatePending(ctx, "order-reconcile", "cookie-reconcile", "manual_status_ship", "首次本地写入失败")
			if firstErr != nil || firstID == "" {
				t.Fatalf("首次 CreatePending id=%q err=%v", firstID, firstErr)
			}
			// repeatedID、repeatedErr 保存进程重试同一外部动作时复用的补偿记录及错误。
			repeatedID, repeatedErr := tg.store.Reconciliations.CreatePending(ctx, "order-reconcile", "cookie-reconcile", "manual_status_ship", "重复本地写入失败")
			if repeatedErr != nil || repeatedID != firstID {
				t.Fatalf("幂等 CreatePending id=%q want=%q err=%v", repeatedID, firstID, repeatedErr)
			}
			// pending、listErr 保存待人工核对的补偿记录及读取错误。
			pending, listErr := tg.store.Reconciliations.ListPending(ctx, 10)
			if listErr != nil || len(pending) != 1 || pending[0].ID != firstID || pending[0].IdempotencyKey == "" {
				t.Fatalf("pending=%+v err=%v", pending, listErr)
			}
		})
	}
}

// TestMultiDB_ItemsUpsert 验证 item_info Upsert + 布尔开关跨三库一致。
func TestMultiDB_ItemsUpsert(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store

			// cid 用于本次流程后续判断的cid
			cid := tg.name + "_item_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// item_info.cookie_id 外键 cookies.id，需先建账号。
			uid := tg.name + "_item_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, uid)
			if // err 用于本次流程后续判断的err
			err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
				t.Fatalf("Save cookie: %v", err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Items.Upsert(ctx, &ItemInfoRow{
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
			// items、err 用于本次流程后续判断的items、err
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
// TestMultiDB_AutomationTryStartRunDedup 封装TestMultiDB自动化Try开始运行Dedup业务协调。
func TestMultiDB_AutomationTryStartRunDedup(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store

			// uid 用于本次流程后续判断的uid
			uid := tg.name + "_auto_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, uid)
			// cid 用于本次流程后续判断的cid
			cid := tg.name + "_auto_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if // err 用于本次流程后续判断的err
			err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
				t.Fatalf("Save cookie: %v", err)
			}

			// ruleID、err 用于本次流程后续判断的规则ID、err
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

			// run 用于本次流程后续判断的运行
			run := AutomationRun{
				RuleID:      ruleID,
				CookieID:    cid,
				ItemID:      "i1",
				OrderID:     "o1",
				TriggerType: "paid",
				TriggerKey:  "paid:o1",
			}
			// id1、started、err 用于本次流程后续判断的id1、started、err
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
			// id3、started3、err 用于本次流程后续判断的id3、started3、err
			id3, started3, err := s.Automation.TryStartRun(ctx, run)
			if err != nil || !started3 || id3 == 0 {
				t.Fatalf("不同 trigger_key 应启动: id=%d started=%v err=%v", id3, started3, err)
			}

			// FinishRun 标记完成。
			if err := s.Automation.FinishRun(ctx, id1, 1, "done", 1, ""); err != nil {
				t.Fatalf("FinishRun: %v", err)
			}
		})
	}
}

// TestMultiDB_DeferredAutomationCredentialWake 验证延迟任务的失败退避和凭证恢复唤醒
// 在各数据库方言下行为一致，同时确保正常的业务延迟不会被提前唤醒。
// TestMultiDB_DeferredAutomationCredentialWake 封装TestMultiDBDeferred自动化CredentialWake业务协调。
func TestMultiDB_DeferredAutomationCredentialWake(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store
			// suffix 用于本次流程后续判断的suffix
			suffix := fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// username 用于本次流程后续判断的username
			username := tg.name + "_deferred_user_" + suffix
			if // ok、err 用于本次流程后续判断的ok、err
			ok, err := s.Users.Create(ctx, username, username+"@e.com", "pw"); err != nil || !ok {
				t.Fatalf("create user: ok=%v err=%v", ok, err)
			}
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, username)
			// cookieID 用于本次流程后续判断的登录凭证ID
			cookieID := tg.name + "_deferred_cookie_" + suffix
			if // err 用于本次流程后续判断的err
			err := s.Cookies.Save(ctx, cookieID, "cv", user.ID); err != nil {
				t.Fatalf("save cookie: %v", err)
			}

			if // err 用于本次流程后续判断的err
			err := s.Automation.DeferTask(ctx, DeferredAutomationTask{
				TaskKey: "credential-" + suffix, CookieID: cookieID, TriggerType: "order_paid",
				TaskJSON: `{}`, DueAt: 0, ErrorMessage: "FAIL_SYS_SESSION_EXPIRED",
			}); err != nil {
				t.Fatalf("defer credential task: %v", err)
			}
			// claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.Automation.ClaimDueDeferredTasks(ctx, 1)
			if err != nil || len(claimed) != 1 {
				t.Fatalf("claim credential task: tasks=%+v err=%v", claimed, err)
			}
			// before 用于本次流程后续判断的before
			before := time.Now().UTC().Unix()
			if // err 用于本次流程后续判断的err
			err := s.Automation.FinishDeferredTask(ctx, claimed[0].ID, claimed[0].ClaimVersion, false, "session expired"); err != nil {
				t.Fatalf("finish failed task: %v", err)
			}

			// intentionalDue 用于本次流程后续判断的intentionalDue
			intentionalDue := before + 3600
			if // err 用于本次流程后续判断的err
			err := s.Automation.DeferTask(ctx, DeferredAutomationTask{
				TaskKey: "intentional-" + suffix, CookieID: cookieID, TriggerType: "buyer_reviewed",
				TaskJSON: `{}`, DueAt: intentionalDue,
			}); err != nil {
				t.Fatalf("defer intentional task: %v", err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.WakeCredentialBlocked(ctx, cookieID); err != nil {
				t.Fatalf("wake credential tasks: %v", err)
			}

			// credentialDue、normalDue 用于本次流程后续判断的credentialDue、normalDue
			var credentialDue, normalDue int64
			if // err 用于本次流程后续判断的err
			err := s.DB.QueryRowContext(ctx, `SELECT due_at FROM automation_pending_tasks WHERE task_key=?`, "credential-"+suffix).Scan(&credentialDue); err != nil {
				t.Fatalf("read credential due_at: %v", err)
			}
			if // err 用于本次流程后续判断的err
			err := s.DB.QueryRowContext(ctx, `SELECT due_at FROM automation_pending_tasks WHERE task_key=?`, "intentional-"+suffix).Scan(&normalDue); err != nil {
				t.Fatalf("read intentional due_at: %v", err)
			}
			if credentialDue != 0 {
				t.Fatalf("credential task due_at=%d want 0", credentialDue)
			}
			if normalDue != intentionalDue {
				t.Fatalf("intentional task due_at=%d want %d", normalDue, intentionalDue)
			}
		})
	}
}

// TestMultiDB_AutomationSafeCheckpointRetry 封装TestMultiDB自动化SafeCheckpoint重试业务协调。
func TestMultiDB_AutomationSafeCheckpointRetry(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store
			// suffix 用于本次流程后续判断的suffix
			suffix := fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// username 用于本次流程后续判断的username
			username := tg.name + "_checkpoint_user_" + suffix
			if // ok、err 用于本次流程后续判断的ok、err
			ok, err := s.Users.Create(ctx, username, username+"@e.com", "pw"); err != nil || !ok {
				t.Fatalf("create user: ok=%v err=%v", ok, err)
			}
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, username)
			// cookieID 用于本次流程后续判断的登录凭证ID
			cookieID := tg.name + "_checkpoint_cookie_" + suffix
			if // err 用于本次流程后续判断的err
			err := s.Cookies.Save(ctx, cookieID, "cv", user.ID); err != nil {
				t.Fatal(err)
			}
			// ruleID、err 用于本次流程后续判断的规则ID、err
			ruleID, err := s.Automation.Create(ctx, AutomationRuleInput{
				UserID: user.ID, CookieID: cookieID, Name: "checkpoint", TriggerType: "order_paid", Enabled: true,
				Actions: []AutomationActionInput{{ActionType: "send_text", Enabled: true}},
			})
			if err != nil {
				t.Fatal(err)
			}
			// runID、started、err 用于本次流程后续判断的运行ID、started、err
			runID, started, err := s.Automation.TryStartRun(ctx, AutomationRun{
				RuleID: ruleID, CookieID: cookieID, OrderID: "order-" + suffix,
				TriggerType: "order_paid", TriggerKey: "order_paid:order-" + suffix, RawEventJSON: `{}`,
			})
			if err != nil || !started {
				t.Fatalf("start run: started=%v err=%v", started, err)
			}
			if // ok、err 用于本次流程后续判断的ok、err
			ok, err := s.Automation.StartRunAction(ctx, runID, 1, 0, time.Now().Add(time.Minute).Unix()); err != nil || !ok {
				t.Fatalf("start first action: ok=%v err=%v", ok, err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.AdvanceRunAction(ctx, runID, 1, 0, 1); err != nil {
				t.Fatal(err)
			}
			if // ok、err 用于本次流程后续判断的ok、err
			ok, err := s.Automation.StartRunAction(ctx, runID, 1, 1, time.Now().Add(time.Minute).Unix()); err != nil || !ok {
				t.Fatalf("start second action: ok=%v err=%v", ok, err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.AbortRunAction(ctx, runID, 1, 1); err != nil {
				t.Fatal(err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.FinishRun(ctx, runID, 1, "failed", 1, SafeRetryErrorPrefix+"session expired"); err != nil {
				t.Fatal(err)
			}
			if // err 用于本次流程后续判断的err
			_, err := s.DB.ExecContext(ctx, `UPDATE automation_runs SET next_retry_at=0 WHERE id=?`, runID); err != nil {
				t.Fatal(err)
			}
			// due、err 用于本次流程后续判断的due、err
			due, err := s.Automation.DueRecoveryRuns(ctx, 10)
			if err != nil || len(due) != 1 || due[0].ID != runID || due[0].ActionCursor != 1 || due[0].SentCount != 1 {
				t.Fatalf("due=%+v err=%v", due, err)
			}
			// newLease 用于本次流程后续判断的newLease
			newLease := time.Now().Add(5 * time.Minute).Unix()
			// claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.Automation.ClaimRecoveryRun(ctx, runID, newLease)
			if err != nil || !claimed {
				t.Fatalf("claim safe checkpoint: claimed=%v err=%v", claimed, err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.PostponeRecoveryRun(ctx, runID, due[0].AttemptCount, time.Now().Add(time.Minute).Unix()); !errors.Is(err, ErrAutomationRunLeaseLost) {
				t.Fatalf("stale postpone err=%v want lease lost", err)
			}
			// current、err 用于本次流程后续判断的current、err
			current, err := s.Automation.GetRun(ctx, runID)
			if err != nil || current.LeaseExpiresAt != newLease {
				t.Fatalf("claimed lease overwritten: run=%+v err=%v", current, err)
			}
		})
	}
}

// TestMultiDB_Notifications 验证通知渠道创建 + 账号绑定读写。
func TestMultiDB_Notifications(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store

			// uid 用于本次流程后续判断的uid
			uid := tg.name + "_notif_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, uid)
			// cid 用于本次流程后续判断的cid
			cid := tg.name + "_notif_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if // err 用于本次流程后续判断的err
			err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
				t.Fatalf("Save cookie: %v", err)
			}

			// chID、err 用于本次流程后续判断的chID、err
			chID, err := s.Notifications.CreateChannel(ctx, &NotificationChannelRow{
				Name: "wh", Type: "webhook", Config: `{"url":"x"}`, Enabled: true, UserID: user.ID,
			})
			if err != nil || chID == 0 {
				t.Fatalf("CreateChannel: id=%d err=%v", chID, err)
			}
			// channels 用于本次流程后续判断的渠道列表
			channels, _ := s.Notifications.AllChannelsForUser(ctx, user.ID)
			if len(channels) != 1 || !channels[0].Enabled || channels[0].Config != `{"url":"x"}` {
				t.Fatalf("channels = %#v", channels)
			}
			// summaries、summaryErr 保存不解密配置的三库渠道摘要结果。
			summaries, summaryErr := s.Notifications.ListChannelSummariesForUser(ctx, user.ID)
			if summaryErr != nil || len(summaries) != 1 || summaries[0].Name != "wh" || summaries[0].UserID != user.ID {
				t.Fatalf("channel summaries = %+v err=%v", summaries, summaryErr)
			}
			if // err 用于本次流程后续判断的err
			err := s.Notifications.SetBindings(ctx, cid, []int64{chID}); err != nil {
				t.Fatalf("SetBindings: %v", err)
			}
			// bindings 用于本次流程后续判断的bindings
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
			if // err 用于本次流程后续判断的err
			err := s.Notifications.EnqueueOutbox(ctx, []NotificationOutboxInput{{ChannelID: chID, EventType: "test", Body: "body"}}); err != nil {
				t.Fatalf("EnqueueOutbox: %v", err)
			}
			// messages、err 用于本次流程后续判断的messages、err
			messages, err := s.Notifications.ClaimOutbox(ctx, "worker", time.Now(), 10)
			if err != nil || len(messages) != 1 {
				t.Fatalf("ClaimOutbox: messages=%+v err=%v", messages, err)
			}
			// uncertain、err 保存三方言不确定隔离结果和数据库错误。
			uncertain, err := s.Notifications.MarkOutboxUncertain(ctx, messages[0].ID, "worker", "确认落库失败")
			if err != nil || !uncertain {
				t.Fatalf("MarkOutboxUncertain: uncertain=%v err=%v", uncertain, err)
			}
			// afterUncertain、err 保存不确定消息再次领取的结果和数据库错误。
			afterUncertain, err := s.Notifications.ClaimOutbox(ctx, "worker-2", time.Now().Add(time.Minute), 10)
			if err != nil || len(afterUncertain) != 0 {
				t.Fatalf("uncertain outbox was claimable: messages=%+v err=%v", afterUncertain, err)
			}
			// enqueueErr 保存第二条消息重新入队时的数据库错误。
			if enqueueErr := s.Notifications.EnqueueOutbox(ctx, []NotificationOutboxInput{{ChannelID: chID, EventType: "test-complete", Body: "body"}}); enqueueErr != nil {
				t.Fatalf("EnqueueOutbox complete: %v", enqueueErr)
			}
			// completeMessages、err 保存第二条消息领取结果和数据库错误。
			completeMessages, err := s.Notifications.ClaimOutbox(ctx, "worker-3", time.Now(), 10)
			if err != nil || len(completeMessages) != 1 {
				t.Fatalf("ClaimOutbox complete: messages=%+v err=%v", completeMessages, err)
			}
			if // completed、err 用于本次流程后续判断的completed、err
			completed, err := s.Notifications.CompleteOutbox(ctx, completeMessages[0].ID, "worker-3"); err != nil || !completed {
				t.Fatalf("CompleteOutbox: completed=%v err=%v", completed, err)
			}
		})
	}
}

// TestMultiDB_LatestMigrationsDownUp 封装TestMultiDBLatestMigrationsDownUp业务协调。
func TestMultiDB_LatestMigrationsDownUp(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// subdir、gooseDialect 用于本次流程后续判断的subdir、gooseDialect
			subdir, gooseDialect := migrationTestSubdir(t, tg.dialect)
			if // err 用于本次流程后续判断的err
			err := goose.SetDialect(gooseDialect); err != nil {
				t.Fatalf("set goose dialect: %v", err)
			}
			goose.SetBaseFS(migrationsFS)
			// version、err 保存当前数据库迁移版本及读取错误。
			version, err := goose.GetDBVersion(tg.store.DB)
			if err != nil {
				t.Fatalf("get migration version: %v", err)
			}
			// i 表示本次回滚操作序号。
			for i := 0; version >= 14; i++ {
				if // err 保存当前方言迁移回滚错误，失败时保留步骤号诊断。
				err := goose.Down(tg.store.DB, "migrations/"+subdir); err != nil {
					t.Fatalf("migration down #%d: %v", i+1, err)
				}
				version, err = goose.GetDBVersion(tg.store.DB)
				if err != nil {
					t.Fatalf("get migration version after down: %v", err)
				}
			}
			if columnExistsForDialect(t, tg.store.DB, tg.dialect, "notification_channels", "event_types") {
				t.Fatal("notification_channels.event_types should be removed after down")
			}
			if tableExistsForDialect(t, tg.store.DB, tg.dialect, "risk_control_logs") {
				t.Fatal("risk_control_logs should be removed after down")
			}
			if columnExistsForDialect(t, tg.store.DB, tg.dialect, "default_reply_records", "status") {
				t.Fatal("default_reply_records.status should be removed after down")
			}
			if columnExistsForDialect(t, tg.store.DB, tg.dialect, "account_tokens", "cookie_fingerprint") {
				t.Fatal("account_tokens.cookie_fingerprint should be removed after down")
			}
			if columnExistsForDialect(t, tg.store.DB, tg.dialect, "item_publish_batch_rows", "category_json") {
				t.Fatal("item_publish_batch_rows.category_json should be removed after down")
			}
			if columnExistsForDialect(t, tg.store.DB, tg.dialect, "item_publish_batches", "location_json") {
				t.Fatal("item_publish_batches.location_json should be removed after down")
			}
			// table 表示当前遍历过程中的table
			for _, table := range []string{"account_task_settings", "account_task_runs", "chat_sessions", "chat_messages"} {
				if tableExistsForDialect(t, tg.store.DB, tg.dialect, table) {
					t.Fatalf("table should be removed after migration 24 down: %s", table)
				}
			}

			if // err 用于本次流程后续判断的err
			err := goose.Up(tg.store.DB, "migrations/"+subdir); err != nil {
				t.Fatalf("migration up after down: %v", err)
			}
			// c 表示当前遍历过程中的c
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
				{"default_reply_records", "status"},
				{"default_reply_records", "text_sent"},
				{"automation_runs", "action_cursor"},
				{"automation_runs", "action_started"},
				{"account_tokens", "cookie_fingerprint"},
				{"item_publish_batch_rows", "category_json"},
				{"item_publish_batches", "location_json"},
				{"item_info", "deleted_at"},
				{"automation_rules", "deleted_at"},
				{"orders", "deleted_at"},
				{"account_task_settings", "auto_rate_enabled"},
				{"account_task_runs", "run_key"},
				{"chat_sessions", "unread_count"},
				{"chat_sessions", "item_image_url"},
				{"chat_messages", "message_key"},
				{"chat_messages", "read_status"},
				{"chat_messages", "read_at"},
				{"chat_messages", "media_duration"},
			} {
				if !columnExistsForDialect(t, tg.store.DB, tg.dialect, c.table, c.col) {
					t.Fatalf("column missing after re-up: %s.%s", c.table, c.col)
				}
			}
		})
	}
}

// TestMultiDB_ChatAndAccountTasks 封装TestMultiDB聊天And账号任务列表业务协调。
func TestMultiDB_ChatAndAccountTasks(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store
			// suffix 用于本次流程后续判断的suffix
			suffix := fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			// username 用于本次流程后续判断的username
			username := tg.name + "_chat_" + suffix
			if // ok、err 用于本次流程后续判断的ok、err
			ok, err := s.Users.Create(ctx, username, username+"@e.com", "pw"); err != nil || !ok {
				t.Fatalf("create user: ok=%v err=%v", ok, err)
			}
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, username)
			// cookieID 用于本次流程后续判断的登录凭证ID
			cookieID := tg.name + "_chat_cookie_" + suffix
			if // err 用于本次流程后续判断的err
			err := s.Cookies.Save(ctx, cookieID, "unb=1; _m_h5_tk=token_1", user.ID); err != nil {
				t.Fatal(err)
			}

			// settings、err 用于本次流程后续判断的settings、err
			settings, err := s.AccountTasks.Get(ctx, cookieID)
			if err != nil || settings.RateContent == "" || settings.PolishTime != "03:00" {
				t.Fatalf("default settings=%+v err=%v", settings, err)
			}
			settings.AutoRateEnabled = true
			settings.AutoPolishEnabled = true
			settings.RateContent = "交易愉快"
			settings.PolishTime = "04:30"
			if // err 用于本次流程后续判断的err
			err := s.AccountTasks.Upsert(ctx, settings); err != nil {
				t.Fatalf("upsert settings: %v", err)
			}
			// stored、err 用于本次流程后续判断的stored、err
			stored, err := s.AccountTasks.Get(ctx, cookieID)
			if err != nil || !stored.AutoRateEnabled || !stored.AutoPolishEnabled || stored.RateContent != "交易愉快" || stored.PolishTime != "04:30" {
				t.Fatalf("stored settings=%+v err=%v", stored, err)
			}
			// enabled、err 用于本次流程后续判断的enabled、err
			enabled, err := s.AccountTasks.Enabled(ctx)
			if err != nil || len(enabled) != 1 {
				t.Fatalf("enabled=%+v err=%v", enabled, err)
			}

			// run 用于本次流程后续判断的运行
			run := AccountTaskRun{RunKey: "rate:" + cookieID + ":order-1", CookieID: cookieID, TaskType: "auto_rate", TargetID: "order-1"}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.AccountTasks.ClaimRun(ctx, run, 100); err != nil || !claimed {
				t.Fatalf("first claim=%v err=%v", claimed, err)
			}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.AccountTasks.ClaimRun(ctx, run, 100); err != nil || claimed {
				t.Fatalf("duplicate claim=%v err=%v", claimed, err)
			}
			if // err 用于本次流程后续判断的err
			err := s.AccountTasks.FinishRun(ctx, run.RunKey, "failed", 0, 1, "retry", 200); err != nil {
				t.Fatal(err)
			}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.AccountTasks.ClaimRun(ctx, run, 199); err != nil || claimed {
				t.Fatalf("early retry claim=%v err=%v", claimed, err)
			}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.AccountTasks.ClaimRunImmediately(ctx, run, 199); err != nil || !claimed {
				t.Fatalf("manual retry claim=%v err=%v", claimed, err)
			}
			if // err 用于本次流程后续判断的err
			err := s.AccountTasks.FinishRun(ctx, run.RunKey, "failed", 0, 1, "retry", 200); err != nil {
				t.Fatal(err)
			}
			if // claimed、err 用于本次流程后续判断的claimed、err
			claimed, err := s.AccountTasks.ClaimRun(ctx, run, 200); err != nil || !claimed {
				t.Fatalf("due retry claim=%v err=%v", claimed, err)
			}

			// session 用于本次流程后续判断的会话
			session := ChatSession{CookieID: cookieID, ChatID: "chat-1", BuyerID: "buyer-1", BuyerName: "买家甲", ItemID: "item-1"}
			// incoming 用于本次流程后续判断的incoming
			incoming := ChatMessage{MessageKey: "platform-1", Direction: "incoming", SenderID: "buyer-1", SenderName: "买家甲", MessageType: "text", Content: "你好", Status: "received", SentAt: 1000}
			if // inserted、err 用于本次流程后续判断的inserted、err
			_, inserted, err := s.Chats.SaveMessage(ctx, session, incoming, true); err != nil || !inserted {
				t.Fatalf("save incoming inserted=%v err=%v", inserted, err)
			}
			if // inserted、err 用于本次流程后续判断的inserted、err
			_, inserted, err := s.Chats.SaveMessage(ctx, session, incoming, true); err != nil || inserted {
				t.Fatalf("duplicate incoming inserted=%v err=%v", inserted, err)
			}
			// outgoing 用于本次流程后续判断的outgoing
			outgoing := ChatMessage{MessageKey: "local-1", Direction: "outgoing", SenderID: cookieID, SenderName: "我", MessageType: "text", Content: "您好", Status: "sent", SentAt: 2000}
			if // inserted、err 用于本次流程后续判断的inserted、err
			_, inserted, err := s.Chats.SaveMessage(ctx, session, outgoing, false); err != nil || !inserted {
				t.Fatalf("save outgoing inserted=%v err=%v", inserted, err)
			}
			// sessions、err 用于本次流程后续判断的sessions、err
			sessions, err := s.Chats.ListSessions(ctx, user.ID, cookieID, 20)
			if err != nil || len(sessions) != 1 || sessions[0].UnreadCount != 1 || sessions[0].LastMessage != "您好" {
				t.Fatalf("sessions=%+v err=%v", sessions, err)
			}
			// messages、err 用于本次流程后续判断的messages、err
			messages, err := s.Chats.ListMessages(ctx, user.ID, cookieID, "chat-1", 0, 20)
			if err != nil || len(messages) != 2 || messages[0].Content != "你好" || messages[1].Content != "您好" {
				t.Fatalf("messages=%+v err=%v", messages, err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Chats.MarkRead(ctx, user.ID, cookieID, "chat-1"); err != nil {
				t.Fatal(err)
			}
			sessions, _ = s.Chats.ListSessions(ctx, user.ID, cookieID, 20)
			if sessions[0].UnreadCount != 0 {
				t.Fatalf("unread after mark=%d", sessions[0].UnreadCount)
			}
		})
	}
}

// TestMultiDB_CardsCreateGet 验证 cards Create + Get 的 NULL 列扫描（含 nullable 字段）。
func TestMultiDB_CardsCreateGet(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store

			// uid 用于本次流程后续判断的uid
			uid := tg.name + "_card_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, uid)

			// cf 用于本次流程后续判断的cf
			cf := &CardFull{
				Name:        "测试卡密",
				Type:        "text",
				TextContent: "ABC-123",
				Enabled:     true,
				UserID:      user.ID,
			}
			// id、err 用于本次流程后续判断的id、err
			id, err := s.Cards.Create(ctx, cf)
			if err != nil || id == 0 {
				t.Fatalf("Create: id=%d err=%v", id, err)
			}
			// got、err 用于本次流程后续判断的got、err
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

// TestMultiDB_MarkOrderEventTime 验证订单事件时间标记跨三库一致。重点覆盖
// Postgres 回归：CASE WHEN ... THEN CURRENT_TIMESTAMP ELSE field END 会因
// THEN(timestamptz) 与 ELSE(text) 分支类型不可匹配而报 SQLSTATE 42804。
// 语义：字段为空时写入当前时间，已有值时不得覆盖（幂等）。
// TestMultiDB_MarkOrderEventTime 封装TestMultiDBMark订单Event时间业务协调。
func TestMultiDB_MarkOrderEventTime(t *testing.T) {
	// tg 表示当前遍历过程中的tg
	for _, tg := range allTestTargets(t) {
		t.Run(tg.name, func(t *testing.T) {
			defer tg.cleanup()
			// ctx 用于本次流程后续判断的ctx
			ctx := context.Background()
			// s 用于本次流程后续判断的s
			s := tg.store

			// uid 用于本次流程后续判断的uid
			uid := tg.name + "_evt_user_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			s.Users.Create(ctx, uid, uid+"@e.com", "pw")
			// user 用于本次流程后续判断的用户
			user, _ := s.Users.GetByUsername(ctx, uid)
			// cid 用于本次流程后续判断的cid
			cid := tg.name + "_evt_cookie_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if // err 用于本次流程后续判断的err
			err := s.Cookies.Save(ctx, cid, "cv", user.ID); err != nil {
				t.Fatalf("Save cookie: %v", err)
			}
			// oid 用于本次流程后续判断的oid
			oid := tg.name + "_evt_order_" + fmt.Sprintf("%d", atomic.AddUint64(&multidbCounter, 1))
			if // err 用于本次流程后续判断的err
			err := s.Orders.Upsert(ctx, oid, OrderUpsertOpts{ItemID: "i1", BuyerID: "b1", CookieID: cid}); err != nil {
				t.Fatalf("Upsert: %v", err)
			}

			// 白名单字段为空时全部写入当前时间。
			for _, f := range []string{"paid_at", "shipped_at", "completed_at", "buyer_reviewed_at", "last_review_request_at"} {
				if // err 用于本次流程后续判断的err
				err := s.Automation.MarkOrderEventTime(ctx, oid, f); err != nil {
					t.Fatalf("MarkOrderEventTime(%s) on %s: %v", f, tg.name, err)
				}
			}
			// got、err 用于本次流程后续判断的got、err
			got, err := s.Orders.Get(ctx, oid)
			if err != nil {
				t.Fatalf("Get: %v", err)
			}
			if got.PaidAt == "" || got.ShippedAt == "" || got.CompletedAt == "" || got.BuyerReviewedAt == "" || got.LastReviewRequestAt == "" {
				t.Fatalf("event timestamps not set on %s: %#v", tg.name, got)
			}

			// 已有值时不得覆盖。
			const original = "2020-01-02 03:04:05"
			if // err 用于本次流程后续判断的err
			_, err := s.DB.ExecContext(ctx, `UPDATE orders SET shipped_at=? WHERE order_id=?`, original, oid); err != nil {
				t.Fatal(err)
			}
			if // err 用于本次流程后续判断的err
			err := s.Automation.MarkOrderEventTime(ctx, oid, "shipped_at"); err != nil {
				t.Fatalf("MarkOrderEventTime(shipped_at) overwrite on %s: %v", tg.name, err)
			}
			// shippedAt 用于本次流程后续判断的shippedAt
			var shippedAt string
			if // err 用于本次流程后续判断的err
			err := s.DB.QueryRowContext(ctx, `SELECT shipped_at FROM orders WHERE order_id=?`, oid).Scan(&shippedAt); err != nil {
				t.Fatal(err)
			}
			if shippedAt != original {
				t.Fatalf("event timestamp overwritten on %s: %q want %q", tg.name, shippedAt, original)
			}

			// 非法字段拒绝。
			if err := s.Automation.MarkOrderEventTime(ctx, oid, "order_status"); err == nil || !strings.Contains(err.Error(), "不允许") {
				t.Fatalf("非法字段应拒绝 on %s, got %v", tg.name, err)
			}
		})
	}
}

// migrationTestSubdir 封装migrationTestSubdir业务协调。
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

// columnExistsForDialect 封装columnExistsForDialect业务协调。
func columnExistsForDialect(t *testing.T, db *sql.DB, dialect Dialect, table, col string) bool {
	t.Helper()
	// query 用于本次流程后续判断的查询
	var query string
	// args 用于本次流程后续判断的args
	var args []any
	switch dialect {
	case DialectSQLite:
		// rows、err 用于本次流程后续判断的rows、err
		rows, err := db.Query(`SELECT name FROM pragma_table_info(?)`, table)
		if err != nil {
			t.Fatalf("pragma_table_info(%s): %v", table, err)
		}
		defer rows.Close()
		for rows.Next() {
			// name 用于本次流程后续判断的名称
			var name string
			if // err 用于本次流程后续判断的err
			err := rows.Scan(&name); err != nil {
				t.Fatalf("scan column name: %v", err)
			}
			if name == col {
				return true
			}
		}
		if // err 用于本次流程后续判断的err
		err := rows.Err(); err != nil {
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
	// count 用于本次流程后续判断的数量
	var count int
	if // err 用于本次流程后续判断的err
	err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("column exists query %s.%s: %v", table, col, err)
	}
	return count > 0
}

// tableExistsForDialect 封装tableExistsForDialect业务协调。
func tableExistsForDialect(t *testing.T, db *sql.DB, dialect Dialect, table string) bool {
	t.Helper()
	// query 用于本次流程后续判断的查询
	var query string
	// args 用于本次流程后续判断的args
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
	// count 用于本次流程后续判断的数量
	var count int
	if // err 用于本次流程后续判断的err
	err := db.QueryRow(query, args...).Scan(&count); err != nil {
		t.Fatalf("table exists query %s: %v", table, err)
	}
	return count > 0
}

// boolPtr 封装boolPtr业务协调。
func boolPtr(b bool) *bool { return &b }
