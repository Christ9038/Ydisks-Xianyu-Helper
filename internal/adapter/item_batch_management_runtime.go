package adapter

import (
	"context"
	"errors"

	itemapp "xianyu-go/internal/application/items"
)

// BatchManagementRuntime 将由组合根拥有的 worker 协调器适配为批次管理应用 Port。
type BatchManagementRuntime struct {
	// lifecycleContext 返回进程协调器拥有的 worker 父 Context，批次管理不保存 HTTP Server。
	lifecycleContext func() context.Context
	// coordinator 持有批量 worker 的启动、取消、恢复和等待语义。
	coordinator *itemapp.BatchWorkerCoordinator
}

// NewBatchManagementRuntime 构造批次管理运行时 Port；依赖缺失在应用服务创建时转换为可诊断错误。
func NewBatchManagementRuntime(lifecycleContext func() context.Context, coordinator *itemapp.BatchWorkerCoordinator) itemapp.BatchManagementRuntime {
	return BatchManagementRuntime{lifecycleContext: lifecycleContext, coordinator: coordinator}
}

// StartBatch 在组合根提供的生命周期 Context 下启动已获租约的批量 worker。
func (runtime BatchManagementRuntime) StartBatch(userID int64, batchID, workerToken string) error {
	if runtime.lifecycleContext == nil || runtime.coordinator == nil {
		return errors.New("批量 worker 协调器未装配")
	}
	return runtime.coordinator.Start(runtime.lifecycleContext(), userID, batchID, workerToken)
}

// CancelBatch 取消指定租约对应的 worker；协调器缺失时保持幂等无副作用。
func (runtime BatchManagementRuntime) CancelBatch(batchID, workerToken string) {
	if runtime.coordinator == nil {
		return
	}
	_ = runtime.coordinator.Cancel(batchID, workerToken)
}
