package server

import (
	"errors"

	itemapp "xianyu-go/internal/application/items"
)

// serverBatchManagementRuntime 将批次管理应用端口接到 Server 的 worker 生命周期边界。
type serverBatchManagementRuntime struct {
	// server 保存批次 worker 启动与取消所需的 Server 生命周期入口。
	server *Server
	// coordinator 保存应用层批次 worker 生命周期协调器。
	coordinator *itemapp.BatchWorkerCoordinator
}

// StartBatch 启动已由应用服务声明租约的批量 worker，并返回协调器登记失败。
func (runtime serverBatchManagementRuntime) StartBatch(userID int64, batchID, workerToken string) error {
	if runtime.server == nil || runtime.coordinator == nil {
		return errors.New("批量 worker 协调器未装配")
	}
	return runtime.coordinator.Start(runtime.server.lifecycleContext(), userID, batchID, workerToken)
}

// CancelBatch 取消指定批次的后台 worker。
func (runtime serverBatchManagementRuntime) CancelBatch(batchID, workerToken string) {
	if runtime.coordinator == nil {
		return
	}
	_ = runtime.coordinator.Cancel(batchID, workerToken)
}
