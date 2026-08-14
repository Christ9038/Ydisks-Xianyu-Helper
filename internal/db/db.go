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

// migrationsFS 保存migrationsFS，供当前处理流程使用
//
//go:embed migrations/sqlite/*.sql migrations/mysql/*.sql migrations/postgres/*.sql
var migrationsFS embed.FS

// Dialect 标识数据库方言。
type Dialect string

// DialectSQLite 保存DialectSQLite，供当前处理流程使用
const (
	DialectSQLite   Dialect = "sqlite"
	DialectMySQL    Dialect = "mysql"
	DialectPostgres Dialect = "postgres"
)

// driverName 内部 driver 名（传给 sql.Open）。
type driverName string

// driverSQLite 保存driverSQLite，供当前处理流程使用
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
	// driver、dialect、dsn、err 保存driver、dialect、dsn、err，供当前处理流程使用
	driver, dialect, dsn, err := parseDBURL(dbURL)
	if err != nil {
		return nil, "", err
	}
	// db、err 保存db、err，供当前处理流程使用
	db, err := sql.Open(string(driver), dsn)
	if err != nil {
		return nil, "", fmt.Errorf("打开数据库: %w", err)
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

	if // err 保存err，供当前处理流程使用
	err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, "", fmt.Errorf("ping 数据库: %w", err)
	}

	if // err 保存err，供当前处理流程使用
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
		// dsn 保存dsn，供当前处理流程使用
		dsn := sqliteDSN(raw)
		return driverSQLite, DialectSQLite, dsn, nil
	}
	// idx 保存idx，供当前处理流程使用
	idx := strings.Index(raw, "://")
	// scheme 保存scheme，供当前处理流程使用
	scheme := raw[:idx]
	// rest 保存rest，供当前处理流程使用
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
// mysqlDSN 负责mysqlDSN相关处理。
func mysqlDSN(dsn string) string {
	dsn = forceMySQLBoolParam(dsn, "multiStatements")
	return forceMySQLBoolParam(dsn, "clientFoundRows")
}

// forceMySQLBoolParam 负责forceMySQLBoolParam相关处理。
func forceMySQLBoolParam(dsn, key string) string {
	// base、rawQuery、hasQuery 保存base、rawQuery、has查询，供当前处理流程使用
	base, rawQuery, hasQuery := strings.Cut(dsn, "?")
	// parts 保存parts，供当前处理流程使用
	parts := make([]string, 0, 4)
	// found 保存found，供当前处理流程使用
	found := false
	if hasQuery {
		// part 表示当前遍历过程中的part
		for _, part := range strings.Split(rawQuery, "&") {
			if part == "" {
				continue
			}
			// name 保存名称，供当前处理流程使用
			name, _, _ := strings.Cut(part, "=")
			if name == key {
				if !found {
					parts = append(parts, key+"=true")
					found = true
				}
				continue
			}
			parts = append(parts, part)
		}
	}
	if !found {
		parts = append(parts, key+"=true")
	}
	return base + "?" + strings.Join(parts, "&")
}

// Migrate 执行嵌入式 goose 迁移，按方言选择子目录。
func Migrate(ctx context.Context, db *sql.DB, dialect Dialect) error {
	// gooseDialect 保存gooseDialect，供当前处理流程使用
	var gooseDialect string
	// subdir 保存subdir，供当前处理流程使用
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
	if // err 保存err，供当前处理流程使用
	err := goose.SetDialect(gooseDialect); err != nil {
		return fmt.Errorf("设置 goose dialect: %w", err)
	}
	goose.SetBaseFS(migrationsFS)
	if // err 保存err，供当前处理流程使用
	err := goose.Up(db, "migrations/"+subdir); err != nil {
		return fmt.Errorf("执行迁移: %w", err)
	}
	return nil
}
