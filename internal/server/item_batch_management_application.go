package server

// serverBatchManagementRuntime 将批次管理应用端口接到 Server 的 worker 生命周期边界。
type serverBatchManagementRuntime struct {
	// server 保存批次 worker 启动与取消所需的 Server 生命周期入口。
	server *Server
}

// StartBatch 启动已由应用服务声明租约的批量 worker。
func (runtime serverBatchManagementRuntime) StartBatch(userID int64, batchID, workerToken string) {
	if runtime.server == nil {
		return
	}
	runtime.server.startPublishBatchWorker(runtime.server.lifecycleContext(), userID, batchID, workerToken)
}

// CancelBatch 取消指定批次的后台 worker。
func (runtime serverBatchManagementRuntime) CancelBatch(batchID, workerToken string) {
	if runtime.server == nil {
		return
	}
	runtime.server.cancelPublishBatch(batchID, workerToken)
}
