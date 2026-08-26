package server

import (
	"context"
	"errors"
	"testing"
)

// TestTaskRegistryCoversHistoryPruningAndTerminalGuards 覆盖任务历史清理及终态幂等保护。
func TestTaskRegistryCoversHistoryPruningAndTerminalGuards(t *testing.T) {
	// registry 保存限制历史容量的任务注册表。
	registry := newTaskRegistry()
	registry.maxHistory = 1
	// firstID、firstComplete 保存最早任务及其收束函数。
	firstID, firstComplete := registry.start("first", context.Background())
	// secondID、secondComplete 保存第二个任务及其收束函数。
	secondID, secondComplete := registry.start("second", context.Background())
	secondComplete(errors.New("failed"))
	// afterRunningPrune 保存最早任务仍运行时的历史快照。
	afterRunningPrune := registry.list()
	if len(afterRunningPrune) != 2 {
		t.Fatalf("running oldest task should be retained: %+v", afterRunningPrune)
	}
	firstComplete(nil)
	// thirdID、thirdComplete 保存触发已完成历史清理的第三个任务。
	thirdID, thirdComplete := registry.start("third", context.Background())
	thirdComplete(nil)
	// snapshots 保存历史清理后的任务快照。
	snapshots := registry.list()
	if len(snapshots) != 1 || snapshots[0].ID != thirdID {
		t.Fatalf("pruned snapshots=%+v first=%s second=%s", snapshots, firstID, secondID)
	}
	// thirdComplete 的重复调用验证终态不会被覆盖。
	thirdComplete(errors.New("ignored"))
	// unknownRegistry 验证空注册表的 nil 接收者保护。
	var unknownRegistry *taskRegistry
	unknownRegistry.finish("missing", nil)
	if unknownRegistry.list() != nil {
		t.Fatal("nil registry should return no snapshots")
	}
	if stateForContext(nilServerContext()) != taskStateSucceeded {
		t.Fatal("nil context should be treated as succeeded")
	}
}

// nilServerContext 返回用于覆盖任务注册表兼容 nil Context 分支的空上下文接口。
func nilServerContext() context.Context { return nil }

// TestTaskRegistryCoversNilStartAndContextFailure 验证 nil 注册表启动及任务函数失败状态。
func TestTaskRegistryCoversNilStartAndContextFailure(t *testing.T) {
	// registry 是 nil 接收者启动后由实现自动创建的内部注册表。
	var registry *taskRegistry
	// taskID、complete 保存 nil 接收者返回的任务句柄。
	taskID, complete := registry.start("nil registry", nil)
	if taskID == "" || complete == nil {
		t.Fatalf("invalid nil registry task handle: id=%q complete=%v", taskID, complete == nil)
	}
	complete(errors.New("failure"))
}
