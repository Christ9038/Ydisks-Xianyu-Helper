// Package db 提供 SQLite 连接管理与迁移。
// 使用 modernc.org/sqlite（纯 Go，免 CGO，跨平台编译友好），WAL 模式 + busy_timeout。
// 迁移用 goose 嵌入式执行，schema 从 Python db_manager.py 收敛而来——把历史上 12+ 处
// 运行时 ALTER TABLE 补齐到 CREATE TABLE，并修复 schema 不一致
// （orders.system_shipped、delivery_rules.user_id 等列在原 CREATE 中缺失却被引用）。
package db

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"time"

	"github.com/pressly/goose/v3"
	_ "modernc.org/sqlite"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

// Open 打开/创建数据库并执行迁移。dbPath 形如 "data/xianyu_data.db"。
func Open(ctx context.Context, dbPath string) (*sql.DB, error) {
	// _pragma 通过 DSN 设置；foreign_keys=ON 让 ON DELETE CASCADE 生效。
	dsn := fmt.Sprintf("file:%s?_pragma=journal_mode(WAL)&_pragma=foreign_keys(ON)&_pragma=busy_timeout(5000)&_pragma=synchronous(NORMAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库: %w", err)
	}

	// SQLite 写串行，连接池主要供读并发；设单写多读的合理上限。
	db.SetMaxOpenConns(8)
	db.SetMaxIdleConns(4)
	db.SetConnMaxLifetime(time.Hour)

	if err := db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("ping 数据库: %w", err)
	}

	if err := Migrate(ctx, db); err != nil {
		db.Close()
		return nil, err
	}
	return db, nil
}

// Migrate 执行嵌入式 goose 迁移。
func Migrate(ctx context.Context, db *sql.DB) error {
	if err := goose.SetDialect("sqlite3"); err != nil {
		return fmt.Errorf("设置 goose dialect: %w", err)
	}
	goose.SetBaseFS(migrationsFS)
	if err := goose.Up(db, "migrations"); err != nil {
		return fmt.Errorf("执行迁移: %w", err)
	}
	return nil
}
