// Package db 提供数据库连接管理与迁移。
//
// 支持三种数据库，由连接 URL 的 scheme 决定：
//   - sqlite://<path>     纯 Go modernc.org/sqlite，WAL + foreign_keys（默认，本地开发）
//   - mysql://<dsn>       go-sql-driver/mysql（生产/Docker 外置数据库）
//   - postgres://<dsn>    jackc/pgx（生产/Docker 外置数据库）
//
// 迁移用 goose 嵌入式执行，按方言分目录：migrations/{sqlite,mysql,postgres}。
// 00001 初始 schema 已把历史上运行时 ALTER TABLE 的列补齐到 CREATE TABLE，
// 并修复 schema 不一致（如 orders.system_shipped 原 CREATE 缺失却被引用）。
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"strings"
	"time"

	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

// migrationsFS 用于本次流程后续判断的migrationsFS
//
//go:embed migrations/sqlite/*.sql migrations/mysql/*.sql migrations/postgres/*.sql
var migrationsFS embed.FS

// Dialect 标识数据库方言。
type Dialect string

// DialectSQLite 用于本次流程后续判断的DialectSQLite
const (
	DialectSQLite   Dialect = "sqlite"
	DialectMySQL    Dialect = "mysql"
	DialectPostgres Dialect = "postgres"
)

// driverName 内部 driver 名（传给 sql.Open）。
type driverName string

// driverSQLite 用于本次流程后续判断的driverSQLite
const (
	driverSQLite driverName = "sqlite"
	driverMySQL  driverName = "mysql"
	// driverPgx 走 pgx_compat driver（见 pgx_compat.go），把 ? 占位符重写成 $N。
	driverPgx driverName = pgxCompatDriverName
)

// Open 打开/创建数据库并执行迁移。dbURL 形如：
//
//	sqlite://data/xianyu_data.db
//	mysql://user:pass@tcp(host:3306)/dbname?parseTime=true&loc=Local
//	postgres://user:pass@host:5432/dbname?sslmode=disable
//
// 为向后兼容，传入的 dbURL 若不含 "://"，则按 SQLite 文件路径处理。
// Open 打开当前值。
func Open(ctx context.Context, dbURL string) (*sql.DB, Dialect, error) {
	// driver、dialect、dsn、err 用于本次流程后续判断的driver、dialect、dsn、err
	driver, dialect, dsn, err := parseDBURL(dbURL)
	if err != nil {
		return nil, "", err
	}
	// db 保存打开的数据库连接池。
	var db *sql.DB
	if driver == driverPgx {
		// connector、connectorErr 保存使用 pgx 结构化配置的连接器及解析错误。
		connector, connectorErr := newPgxCompatConnector(dsn)
		if connectorErr != nil {
			return nil, "", fmt.Errorf("解析 PostgreSQL 连接: %w", connectorErr)
		}
		db = sql.OpenDB(connector)
	} else {
		// openErr 保存非 PostgreSQL 驱动初始化错误。
		var openErr error
		db, openErr = sql.Open(string(driver), dsn)
		if openErr != nil {
			return nil, "", fmt.Errorf("打开数据库: %w", openErr)
		}
	}

	// 连接池参数按 driver 调整：SQLite 写串行，单写多读；MySQL/PG 可多写并发。
	switch driver {
	case driverSQLite:
		db.SetMaxOpenConns(8)
		db.SetMaxIdleConns(4)
	default:
		db.SetMaxOpenConns(25)
		db.SetMaxIdleConns(10)
	}
	db.SetConnMaxLifetime(time.Hour)

	if // err 用于本次流程后续判断的err
	err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, "", fmt.Errorf("ping 数据库: %w", err)
	}

	if // err 用于本次流程后续判断的err
	err := Migrate(ctx, db, dialect); err != nil {
		_ = db.Close()
		return nil, "", err
	}
	return db, dialect, nil
}

// parseDBURL 解析连接 URL，返回内部 driver 名、方言、DSN。
func parseDBURL(raw string) (driverName, Dialect, string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", "", "", fmt.Errorf("数据库连接为空")
	}
	// 向后兼容：不含 scheme 视为 SQLite 文件路径。
	if !strings.Contains(raw, "://") {
		// dsn 用于本次流程后续判断的dsn
		dsn := sqliteDSN(raw)
		return driverSQLite, DialectSQLite, dsn, nil
	}
	// idx 用于本次流程后续判断的idx
	idx := strings.Index(raw, "://")
	// scheme 用于本次流程后续判断的scheme
	scheme := raw[:idx]
	// rest 用于本次流程后续判断的rest
	rest := raw[idx+3:]
	switch scheme {
	case "sqlite", "sqlite3":
		return driverSQLite, DialectSQLite, sqliteDSN(rest), nil
	case "mysql":
		// MySQL DSN: user:pass@tcp(host:port)/db?params（无 scheme）
		// goose 多语句迁移需要 multiStatements=true，缺失会静默只执行首条语句。
		return driverMySQL, DialectMySQL, mysqlDSN(rest), nil
	case "postgres", "postgresql", "pgx":
		// pgx 接受完整 postgres:// URL；也接受 libpq key=value DSN。
		// 只有明确的 key=value 形式才去掉伪 scheme；URL 即便省略用户名也必须保留 scheme。
		if strings.Contains(rest, "=") && !strings.Contains(rest, "/") {
			return driverPgx, DialectPostgres, rest, nil
		}
		return driverPgx, DialectPostgres, scheme + "://" + rest, nil
	default:
		return "", "", "", fmt.Errorf("不支持的数据库 scheme: %s（支持 sqlite/mysql/postgres）", scheme)
	}
}

// sqliteDSN 构造 SQLite DSN，开启 WAL/foreign_keys/busy_timeout/synchronous。
func sqliteDSN(path string) string {
	return fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
}

// mysqlDSN 强制启用应用依赖的两个连接选项：
//   - multiStatements：goose 多语句迁移需要，缺失会静默只执行首条语句；
//   - clientFoundRows：RowsAffected 返回匹配行数，使“保存未变化内容”的语义与 SQLite/Postgres 一致。
//
// 其余参数原样保留。不强制 parseTime——本仓库时间列按 string/int64 扫描。
// mysqlDSN 封装mysqlDSN业务协调。
func mysqlDSN(dsn string) string {
	dsn = forceMySQLBoolParam(dsn, "multiStatements")
	dsn = forceMySQLBoolParam(dsn, "clientFoundRows")
	// MySQL 的 TIMESTAMP 默认受会话时区影响；固定会话为 UTC，保证跨机器读取历史订单时间一致。
	return forceMySQLParam(dsn, "time_zone", "%27%2B00%3A00%27")
}

// forceMySQLBoolParam 封装forceMySQLBoolParam业务协调。
func forceMySQLBoolParam(dsn, key string) string {
	return forceMySQLParam(dsn, key, "true")
}

// forceMySQLParam 强制设置 MySQL DSN 参数并保留其他连接选项。
func forceMySQLParam(dsn, key, value string) string {
	// base、rawQuery、hasQuery 用于本次流程后续判断的base、rawQuery、has查询
	base, rawQuery, hasQuery := strings.Cut(dsn, "?")
	// parts 用于本次流程后续判断的parts
	parts := make([]string, 0, 4)
	// found 用于本次流程后续判断的found
	found := false
	if hasQuery {
		// part 表示当前遍历过程中的part
		for _, part := range strings.Split(rawQuery, "&") {
			if part == "" {
				continue
			}
			// name 用于本次流程后续判断的名称
			name, _, _ := strings.Cut(part, "=")
			if name == key {
				if !found {
					parts = append(parts, key+"="+value)
					found = true
				}
				continue
			}
			parts = append(parts, part)
		}
	}
	if !found {
		parts = append(parts, key+"="+value)
	}
	return base + "?" + strings.Join(parts, "&")
}

// Migrate 执行嵌入式 goose 迁移，按方言选择子目录。
func Migrate(ctx context.Context, db *sql.DB, dialect Dialect) error {
	// gooseDialect 用于本次流程后续判断的gooseDialect
	var gooseDialect string
	// subdir 用于本次流程后续判断的subdir
	var subdir string
	switch dialect {
	case DialectSQLite:
		gooseDialect, subdir = "sqlite3", "sqlite"
	case DialectMySQL:
		gooseDialect, subdir = "mysql", "mysql"
	case DialectPostgres:
		gooseDialect, subdir = "postgres", "postgres"
	default:
		return fmt.Errorf("未知方言: %s", dialect)
	}
	if // err 用于本次流程后续判断的err
	err := goose.SetDialect(gooseDialect); err != nil {
		return fmt.Errorf("设置 goose dialect: %w", err)
	}
	goose.SetBaseFS(migrationsFS)
	if // err 用于本次流程后续判断的err
	err := goose.Up(db, "migrations/"+subdir); err != nil {
		return fmt.Errorf("执行迁移: %w", err)
	}
	// err 保存自动化规则规格迁移错误。
	if err := migratePendingAutomationSKURules(ctx, db); err != nil {
		return fmt.Errorf("迁移自动化多 SKU 规则: %w", err)
	}
	return nil
}
