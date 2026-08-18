package adapter

import (
	"testing"
	"time"
)

// TestNewItemBatchRunnerApplicationRejectsIncompleteDependencies 验证组合期拒绝缺失的批量 worker 依赖，避免请求期才发现半初始化实例。
func TestNewItemBatchRunnerApplicationRejectsIncompleteDependencies(t *testing.T) {
	// runner、buildErr 分别保存构造结果及缺失依赖产生的错误。
	runner, buildErr := NewItemBatchRunnerApplication(nil, nil, time.Minute, nil)
	if buildErr == nil {
		t.Fatal("缺失依赖时应返回构造错误")
	}
	if runner != nil {
		t.Fatal("构造失败时不得返回可运行 worker")
	}
}

// TestNewItemBatchPublisherRejectsIncompleteDependencies 验证 publisher 在组合期拒绝缺失端口，避免 worker 启动后才暴露配置错误。
func TestNewItemBatchPublisherRejectsIncompleteDependencies(t *testing.T) {
	// publisher、buildErr 分别保存构造结果及缺失发布端口产生的错误。
	publisher, buildErr := NewItemBatchPublisher(nil, nil)
	if buildErr == nil {
		t.Fatal("缺失发布端口时应返回构造错误")
	}
	if publisher != nil {
		t.Fatal("构造失败时不得返回可执行 publisher")
	}
}

// TestBatchManagementRuntimeRejectsMissingCoordinator 验证缺失 worker 协调器时不会启动半初始化后台任务。
func TestBatchManagementRuntimeRejectsMissingCoordinator(t *testing.T) {
	// runtime 是缺失协调器的批次管理 Port，用于覆盖启动期失败分支。
	runtime := NewBatchManagementRuntime(nil, nil)
	// startErr 保存启动半初始化 worker 时的组合错误。
	startErr := runtime.StartBatch(1, "batch-1", "worker-1")
	if startErr == nil {
		t.Fatal("缺失协调器时应拒绝启动批次 worker")
	}
	// CancelBatch 在缺失协调器时必须保持幂等且不 panic。
	runtime.CancelBatch("batch-1", "worker-1")
}
