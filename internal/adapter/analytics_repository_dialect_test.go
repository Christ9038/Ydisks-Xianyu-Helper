package adapter

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"regexp"
	"testing"
	"time"

	"xianyu-go/internal/db"
)

// externalURLPattern 把 `scheme://authority/database?query` 形式的测试连接串拆成各部分；authority 可能含凭证，不得打印。
var externalURLPattern = regexp.MustCompile(`^([a-z]+)://(.*?)/([^?]*)(?:\?(.*))?$`)

// TestAnalyticsRepositoryExternalDialects 在真实 MySQL 与 PostgreSQL 上验证订单分析 SQL：整数分求和、范围上限与超长数字文本排除。
// 仅当设置了 TEST_MYSQL_URL / TEST_POSTGRES_URL 时运行对应方言，否则跳过；每次运行创建独立的临时库并在结束后删除。
func TestAnalyticsRepositoryExternalDialects(t *testing.T) {
	// target 是一个待验证的外部方言：环境变量名、管理连接驱动名与方言。
	for _, target := range []struct {
		env, driver string
		dialect     db.Dialect
	}{
		{"TEST_MYSQL_URL", "mysql", db.DialectMySQL},
		{"TEST_POSTGRES_URL", "pgx_compat", db.DialectPostgres},
	} {
		// raw 是外部连接串；为空表示当前环境没有该数据库。
		raw := os.Getenv(target.env)
		t.Run(string(target.dialect), func(t *testing.T) {
			if raw == "" {
				t.Skipf("未设置 %s，跳过 %s 实测", target.env, target.dialect)
			}
			// parts 是拆分后的连接串；scheme 与 authority 用于拼接临时库连接。
			parts := externalURLPattern.FindStringSubmatch(raw)
			if parts == nil {
				t.Fatalf("%s 格式无效", target.env)
			}
			// scheme、authority、query 分别是协议、含凭证的主机部分和驱动参数。
			scheme, authority, query := parts[1], parts[2], parts[4]
			// dsnFor 生成指向 databaseName 的连接串；管理连接使用各驱动自己的 DSN 形式。
			dsnFor := func(driverDSN bool, databaseName string) string {
				// suffix 是带前导问号的驱动参数。
				suffix := ""
				if query != "" {
					suffix = "?" + query
				}
				if driverDSN && target.driver == "mysql" {
					return authority + "/" + databaseName + suffix
				}
				return scheme + "://" + authority + "/" + databaseName + suffix
			}
			// adminDatabase 是用于创建和删除临时库的维护库。
			adminDatabase := ""
			if target.dialect == db.DialectPostgres {
				adminDatabase = "postgres"
			}
			// admin、err 是管理连接及打开错误。
			admin, err := sql.Open(target.driver, dsnFor(true, adminDatabase))
			if err != nil {
				t.Fatalf("打开管理连接失败: %v", err)
			}
			defer admin.Close()
			// name 是本次运行独有的临时库名。
			name := fmt.Sprintf("xytest_analytics_%d", time.Now().UnixNano())
			// err 是创建临时库的错误，应为空。
			if _, err := admin.Exec("CREATE DATABASE " + name); err != nil {
				t.Fatalf("创建临时库失败: %v", err)
			}
			defer func() { _, _ = admin.Exec("DROP DATABASE " + name) }()
			// conn 是临时库连接；Open 会执行全部迁移。
			conn, _, err := db.Open(context.Background(), dsnFor(false, name))
			if err != nil {
				t.Fatalf("打开临时库失败: %v", err)
			}
			defer conn.Close()
			assertAnalyticsCentsScenario(t, db.NewStore(conn, target.dialect), 0)
		})
	}
}
