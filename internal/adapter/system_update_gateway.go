package adapter

import (
	"context"
	"errors"
	"strings"

	updateapp "xianyu-go/internal/application/systemupdate"
	"xianyu-go/internal/updater"
)

// SystemUpdateGateway 将 Unix Socket updater 协议适配为应用层最小 Gateway。
type SystemUpdateGateway struct {
	// client 是只允许发送固定 updater 命令的宿主机客户端。
	client *updater.Client
}

// NewSystemUpdateGateway 从环境变量提供的固定 Socket 路径创建可选更新 Gateway。
func NewSystemUpdateGateway(socketPath string) *SystemUpdateGateway {
	if strings.TrimSpace(socketPath) == "" {
		return nil
	}
	// client 是只连接固定默认 Socket 的宿主机更新客户端。
	client := updater.NewClient()
	if client == nil {
		return nil
	}
	return &SystemUpdateGateway{client: client}
}

// Status 读取 daemon 当前部署与最近任务状态。
func (gateway *SystemUpdateGateway) Status(ctx context.Context, current updateapp.CurrentVersion) (updateapp.Snapshot, error) {
	// response、commandErr 分别是 daemon 状态响应及其请求错误。
	response, commandErr := gateway.client.Status(ctx)
	if commandErr != nil {
		return updateapp.Snapshot{}, commandErr
	}
	return snapshotFromStatus(response, current), nil
}

// Check 主动读取并转换最新正式版本检查结果。
func (gateway *SystemUpdateGateway) Check(ctx context.Context, current updateapp.CurrentVersion) (updateapp.Snapshot, error) {
	// response、commandErr 分别是 daemon 检查响应及其请求错误。
	response, commandErr := gateway.client.Check(ctx)
	if commandErr != nil {
		return updateapp.Snapshot{}, commandErr
	}
	return snapshotFromCheck(response, current), nil
}

// ApplyLatestStable 发送不含任何用户参数的固定正式版更新命令。
func (gateway *SystemUpdateGateway) ApplyLatestStable(ctx context.Context, current updateapp.CurrentVersion) (updateapp.Snapshot, error) {
	// response、commandErr 分别是 daemon 更新受理响应及其请求错误。
	response, commandErr := gateway.client.ApplyLatestStable(ctx)
	if commandErr != nil {
		// apiErr 是 daemon 返回的结构化拒绝，用状态码区分策略冲突与基础设施失败。
		var apiErr *updater.APIError
		if errors.As(commandErr, &apiErr) && apiErr.StatusCode == 409 {
			return updateapp.Snapshot{}, errors.Join(updateapp.ErrUpdateBlocked, commandErr)
		}
		if strings.Contains(commandErrorCode(commandErr), "connection") || errors.As(commandErr, &apiErr) {
			return updateapp.Snapshot{}, updateapp.ErrUpdaterUnavailable
		}
		return updateapp.Snapshot{}, errors.Join(updateapp.ErrUpdaterUnavailable, commandErr)
	}
	return updateapp.Snapshot{Current: current, CanUpdate: true, Operation: operationFromState(response.Operation)}, nil
}

// snapshotFromStatus 将 daemon 状态转换为不泄露宿主机路径的应用快照。
func snapshotFromStatus(status updater.StatusResponse, current updateapp.CurrentVersion) updateapp.Snapshot {
	// snapshot 是转换后的应用层更新状态。
	snapshot := updateapp.Snapshot{Current: current, Available: status.Available, CanUpdate: status.CanUpdate, BlockedReason: status.BlockedReason, Operation: operationFromState(status.Operation)}
	if status.Error != "" && snapshot.BlockedReason == "" {
		snapshot.BlockedReason = updateapp.BlockedUpdaterUnavailable
		snapshot.Operation.Message = "宿主机更新组件暂时无法读取当前部署状态"
	}
	if status.Current != nil {
		snapshot.Current.DeploymentKind = string(status.Current.Kind)
	}
	if status.Latest != nil {
		snapshot.Latest = latestFromManifest(*status.Latest)
	}
	return snapshot
}

// snapshotFromCheck 将只读检查结果转换为前端契约快照。
func snapshotFromCheck(result updater.CheckResult, current updateapp.CurrentVersion) updateapp.Snapshot {
	// snapshot 是检查结果对应的应用层更新状态。
	return updateapp.Snapshot{Current: updateapp.CurrentVersion{Version: result.Current.Build.Version, Commit: result.Current.Build.Commit, BuildTime: result.Current.Build.BuildTime, DeploymentKind: string(result.Current.Kind)}, Latest: latestFromManifest(result.Latest), Available: result.Available, CanUpdate: result.CanUpdate, BlockedReason: result.BlockedReason, Operation: updateapp.Operation{Status: "idle", Message: "检查完成"}}
}

// latestFromManifest 将 Release manifest 转换成管理员页面需要的非敏感字段。
func latestFromManifest(manifest updater.ReleaseManifest) updateapp.LatestVersion {
	return updateapp.LatestVersion{Version: manifest.Version, Tag: manifest.Tag, ReleaseURL: manifest.ReleaseURL, PublishedAt: manifest.PublishedAt.UTC().Format("2006-01-02T15:04:05Z07:00"), ManifestDigest: manifest.ManifestDigest}
}

// operationFromState 将 daemon 任务状态转换成应用层稳定状态。
func operationFromState(operation updater.OperationState) updateapp.Operation {
	// result 汇总任务标识、状态和可选时间字段。
	result := updateapp.Operation{Status: string(operation.Status), RequestID: operation.RequestID, Message: operation.Message}
	if operation.StartedAt != nil {
		result.StartedAt = operation.StartedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	if operation.FinishedAt != nil {
		result.FinishedAt = operation.FinishedAt.UTC().Format("2006-01-02T15:04:05Z07:00")
	}
	return result
}

// commandErrorCode 提取协议错误码；网络错误按执行器不可用处理。
func commandErrorCode(err error) string {
	// apiErr 是 daemon 返回的 HTTP 错误契约。
	var apiErr *updater.APIError
	if errors.As(err, &apiErr) {
		return apiErr.Code
	}
	return "unavailable_connection"
}
