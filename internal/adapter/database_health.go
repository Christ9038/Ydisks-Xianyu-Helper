package adapter

import (
	"context"
	"database/sql"
	"errors"

	"xianyu-go/internal/db"
)

// DatabaseHealth 将数据库连通性探测收口为 Server 健康检查所需的最小适配器。
type DatabaseHealth struct {
	// database 保存待探测的数据库连接；连接生命周期仍由数据库装配方负责关闭。
	database *sql.DB
}

// NewDatabaseHealth 创建数据库健康检查适配器，缺少 Store 时保留可诊断的空依赖实例。
func NewDatabaseHealth(store *db.Store) *DatabaseHealth {
	if store == nil {
		return &DatabaseHealth{}
	}
	return &DatabaseHealth{database: store.DB}
}

// Ping 在调用方 Context 限制内探测数据库连接，避免 HTTP 层直接访问 SQL 连接。
func (health *DatabaseHealth) Ping(ctx context.Context) error {
	if health == nil || health.database == nil {
		return errors.New("数据库健康检查未初始化")
	}
	return health.database.PingContext(ctx)
}
