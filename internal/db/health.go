package db

import (
	"context"
	"database/sql"
	"errors"
)

// HealthProbe 是数据库连接可用性的窄探针，不向上层传递可执行任意 SQL 的连接。
type HealthProbe struct {
	// database 保存被探测的连接池；连接池的关闭责任仍属于启动装配方。
	database *sql.DB
}

// newHealthProbe 构造数据库健康探针。
// database 可以为空，用于保留可诊断但不可用的依赖状态。
func newHealthProbe(database *sql.DB) *HealthProbe {
	return &HealthProbe{database: database}
}

// HealthProbe 返回 Store 对应连接池的健康探针。
// Store 为空时返回不可用探针，使调用方得到稳定错误而不是访问裸连接或发生 panic。
func (s *Store) HealthProbe() *HealthProbe {
	if s == nil {
		return newHealthProbe(nil)
	}
	return newHealthProbe(s.DB)
}

// Ping 在 ctx 的取消期限内探测连接池；未装配连接时返回初始化错误。
func (probe *HealthProbe) Ping(ctx context.Context) error {
	if probe == nil || probe.database == nil {
		return errors.New("数据库健康检查未初始化")
	}
	return probe.database.PingContext(ctx)
}
