package engine

import (
	"sync"
	"time"
)

// accountRuntimeState 保存账号连接状态及其诊断计数。
// runtimeMu 只保护本组件字段；调用方不得持有该锁执行数据库、网络或通知 I/O。

// accountRuntimeState 是账号连接运行状态组件。
type accountRuntimeState struct {
	// runtimeMu 保护连接、失败计数和离线告警字段。
	runtimeMu sync.Mutex

	// connFailures 是连续连接或认证失败次数。
	connFailures int
	// networkFailures 是网络断线累计次数。
	networkFailures int
	// shortDisconnects 保存短连接断开的时间窗口。
	shortDisconnects []time.Time
	// lastMsgReceived 是最近收到平台消息的时间。
	lastMsgReceived time.Time
	// runtimeState 是当前账号运行状态枚举。
	runtimeState string
	// runtimeMessage 是当前运行状态给用户展示的说明。
	runtimeMessage string
	// runtimeUpdatedAt 是最近一次状态变更时间。
	runtimeUpdatedAt time.Time
	// conn 是当前已注册的 WebSocket 连接。
	conn WSConn
	// connStartedAt 是当前 WebSocket 连接建立时间。
	connStartedAt time.Time
	// authExpiredAlerted 表示当前凭证失效是否已经发送过告警。
	authExpiredAlerted bool
	// offlineNotified 表示当前离线周期是否已经发送过离线通知。
	offlineNotified bool
	// offlineSince 是当前离线周期的起始时间。
	offlineSince time.Time
	// lastOfflineReason 是当前离线周期最后一次记录的原因。
	lastOfflineReason string
}
