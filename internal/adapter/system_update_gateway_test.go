package adapter

import (
	"testing"

	updateapp "xianyu-go/internal/application/systemupdate"
	"xianyu-go/internal/updater"
)

// TestSnapshotFromStatusPreservesUnavailableReason 验证 daemon 探测失败不会在应用层静默成空阻断原因。
func TestSnapshotFromStatusPreservesUnavailableReason(t *testing.T) {
	// snapshot 是 daemon 无法读取当前部署时转换出的应用状态。
	snapshot := snapshotFromStatus(updater.StatusResponse{
		Error:     "当前部署探测失败",
		Operation: updater.OperationState{Status: updater.OperationIdle},
	}, updateapp.CurrentVersion{Version: "1.0.0", Commit: "abc1234", BuildTime: "now", DeploymentKind: "stable"})
	if snapshot.BlockedReason != updateapp.BlockedUpdaterUnavailable || snapshot.CanUpdate {
		t.Fatalf("snapshot=%+v", snapshot)
	}
	if snapshot.Operation.Message == "" {
		t.Fatal("状态探测失败时应保留用户可读的阻断说明")
	}
}
