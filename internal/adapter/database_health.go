package adapter

import (
	"context"

	"xianyu-go/internal/db"
)

// DatabaseHealth 将数据库连通性探测收口为 Server 健康检查所需的最小适配器。
type DatabaseHealth struct {
	// probe 保存仅支持 Ping 的数据库健康探针，适配器不持有裸 SQL 连接。
	probe *db.HealthProbe
}

// NewDatabaseHealth 创建数据库健康检查适配器，缺少 Store 时保留可诊断的空依赖实例。
func NewDatabaseHealth(store *db.Store) *DatabaseHealth {
	if store == nil {
		return &DatabaseHealth{}
	}
	return &DatabaseHealth{probe: store.HealthProbe()}
}

// Ping 在调用方 Context 限制内探测数据库连接，避免 HTTP 层直接访问 SQL 连接。
func (health *DatabaseHealth) Ping(ctx context.Context) error {
	if health == nil {
		return (*db.HealthProbe)(nil).Ping(ctx)
	}
	return health.probe.Ping(ctx)
}
