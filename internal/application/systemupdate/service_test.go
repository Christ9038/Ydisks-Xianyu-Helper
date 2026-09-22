package systemupdate

import (
	"context"
	"errors"
	"testing"
)

// gatewayFake 是应用服务测试使用的可编程宿主机执行器替身。
type gatewayFake struct {
	// snapshot 是三个命令成功时返回的执行器状态。
	snapshot Snapshot
	// err 是三个命令统一返回的测试错误。
	err error
}

// Status 返回预置状态并验证应用服务会覆盖当前版本字段。
func (gateway gatewayFake) Status(context.Context, CurrentVersion) (Snapshot, error) {
	return gateway.snapshot, gateway.err
}

// Check 返回预置检查结果。
func (gateway gatewayFake) Check(context.Context, CurrentVersion) (Snapshot, error) {
	return gateway.snapshot, gateway.err
}

// ApplyLatestStable 返回预置任务受理结果。
func (gateway gatewayFake) ApplyLatestStable(context.Context, CurrentVersion) (Snapshot, error) {
	return gateway.snapshot, gateway.err
}

// TestServiceUnavailableAndNormalization 验证可选执行器缺失和当前构建信息的信任边界。
func TestServiceUnavailableAndNormalization(t *testing.T) {
	// current 是应用进程冻结的可信构建信息。
	current := CurrentVersion{Version: "1.0.13", Commit: "abc", BuildTime: "now", DeploymentKind: "stable"}
	// disabled 是没有宿主机执行器的普通部署服务。
	disabled := NewService(nil, current)
	// snapshot、statusErr 是只读状态查询结果及不应出现的错误。
	snapshot, statusErr := disabled.Status(context.Background())
	if statusErr != nil || snapshot.BlockedReason != BlockedUpdaterUnavailable || snapshot.CanUpdate {
		t.Fatalf("disabled snapshot=%+v err=%v", snapshot, statusErr)
	}
	if _ /* applyErr 是未安装执行器时固定返回的更新不可用错误。 */, applyErr := disabled.ApplyLatestStable(context.Background()); !errors.Is(applyErr, ErrUpdaterUnavailable) {
		t.Fatalf("apply error=%v", applyErr)
	}

	// enabled 是返回伪造 current 字段的执行器场景，应用层必须覆盖它。
	enabled := NewService(gatewayFake{snapshot: Snapshot{Current: CurrentVersion{Version: "forged", DeploymentKind: "custom"}, CanUpdate: true}}, current)
	// checked、checkErr 是规范化后的正式版检查结果及错误。
	checked, checkErr := enabled.Check(context.Background())
	if checkErr != nil || checked.Current.Version != current.Version || checked.Current.Commit != current.Commit || checked.Current.BuildTime != current.BuildTime || checked.Current.DeploymentKind != "custom" || !checked.CanUpdate || checked.Operation.Status != "idle" {
		t.Fatalf("checked=%+v err=%v", checked, checkErr)
	}
}
