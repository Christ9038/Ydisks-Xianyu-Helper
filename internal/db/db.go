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

	"github.com/pressly/goose/v3"
	_ "github.com/go-sql-driver/mysql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

//go:embed migrations/sqlite/*.sql migrations/mysql/*.sql migrations/postgres/*.sql
var migrationsFS embed.FS

// Dialect 标识数据库方言。
type Dialect string

const (
	DialectSQLite   Dialect = "sqlite"
	DialectMySQL    Dialect = "mysql"
	DialectPostgres Dialect = "postgres"
)

// driverName 内部 driver 名（传给 sql.Open）。
type driverName string

const (
	driverSQLite   driverName = "sqlite"
	driverMySQL    driverName = "mysql"
	driverPgx      driverName = "pgx"
)

// Open 打开/创建数据库并执行迁移。dbURL 形如：
//
//	sqlite://data/xianyu_data.db
//	mysql://user:pass@tcp(host:3306)/dbname?parseTime=true&loc=Local
//	postgres://user:pass@host:5432/dbname?sslmode=disable
//
// 为向后兼容，传入的 dbURL 若不含 "://"，则按 SQLite 文件路径处理。
func Open(ctx context.Context, dbURL string) (*sql.DB, Dialect, error) {
	driver, dialect, dsn, err := parseDBURL(dbURL)
	if err != nil {
		return nil, "", err
	}
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

	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, "", fmt.Errorf("ping 数据库: %w", err)
	}

	if err := Migrate(ctx, db, dialect); err != nil {
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
		dsn := sqliteDSN(raw)
		return driverSQLite, DialectSQLite, dsn, nil
	}
	idx := strings.Index(raw, "://")
	scheme := raw[:idx]
	rest := raw[idx+3:]
	switch scheme {
	case "sqlite", "sqlite3":
		return driverSQLite, DialectSQLite, sqliteDSN(rest), nil
	case "mysql":
		return driverMySQL, DialectMySQL, rest, nil
	case "postgres", "postgresql", "pgx":
		return driverPgx, DialectPostgres, rest, nil
	default:
		return "", "", "", fmt.Errorf("不支持的数据库 scheme: %s（支持 sqlite/mysql/postgres）", scheme)
	}
}

// sqliteDSN 构造 SQLite DSN，开启 WAL/foreign_keys/busy_timeout/synchronous。
func sqliteDSN(path string) string {
	return fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", path)
}

// Migrate 执行嵌入式 goose 迁移，按方言选择子目录。
func Migrate(ctx context.Context, db *sql.DB, dialect Dialect) error {
	var gooseDialect string
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
	if err := goose.SetDialect(gooseDialect); err != nil {
		return fmt.Errorf("设置 goose dialect: %w", err)
	}
	goose.SetBaseFS(migrationsFS)
	if err := goose.Up(db, "migrations/"+subdir); err != nil {
		return fmt.Errorf("执行迁移: %w", err)
	}
	return nil
}
