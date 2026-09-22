// Package systemupdate 编排管理员查询、检查和触发 Docker 正式版更新的用例。
package systemupdate

import (
	"context"
	"errors"
	"strings"
)

const (
	// DeploymentUnknown 表示无法确认当前部署来源。
	DeploymentUnknown = "unknown"
	// BlockedUpdaterUnavailable 表示宿主机未安装或未挂载受限更新执行器。
	BlockedUpdaterUnavailable = "updater_unavailable"
)

var (
	// ErrUpdaterUnavailable 表示更新执行器或其外部依赖当前不可用。
	ErrUpdaterUnavailable = errors.New("宿主机更新组件不可用")
	// ErrUpdateBlocked 表示执行器按稳定策略拒绝当前更新请求。
	ErrUpdateBlocked = errors.New("当前部署不允许自动更新")
)

// CurrentVersion 描述当前运行服务的非敏感构建信息和部署来源。
type CurrentVersion struct {
	// Version 是构建时注入的应用版本。
	Version string `json:"version"`
	// Commit 是构建对应的短提交标识。
	Commit string `json:"commit"`
	// BuildTime 是构建时间文本。
	BuildTime string `json:"build_time"`
	// DeploymentKind 是 stable、stable_unpinned、dev、custom 或 unknown。
	DeploymentKind string `json:"deployment_kind"`
}

// LatestVersion 描述更新执行器已验证的最新正式版本。
type LatestVersion struct {
	// Version 是不带 v 前缀的正式语义版本。
	Version string `json:"version"`
	// Tag 是对应的 GitHub Release 标签。
	Tag string `json:"tag"`
	// ReleaseURL 是供管理员查看发布说明的 HTTPS 地址。
	ReleaseURL string `json:"release_url"`
	// PublishedAt 是 Release 的 RFC3339 发布时间。
	PublishedAt string `json:"published_at"`
	// ManifestDigest 是更新目标的不可变 OCI manifest 摘要。
	ManifestDigest string `json:"manifest_digest"`
}

// Operation 描述最近一次检查或更新任务的宿主机执行状态。
type Operation struct {
	// Status 是 idle、checking、queued、running、succeeded、failed 或 rolled_back。
	Status string `json:"status"`
	// RequestID 是更新执行器生成或接受的幂等任务标识。
	RequestID string `json:"request_id"`
	// StartedAt 是任务开始时间；空值表示尚未开始。
	StartedAt string `json:"started_at"`
	// FinishedAt 是任务结束时间；空值表示尚未结束。
	FinishedAt string `json:"finished_at"`
	// Message 是不包含宿主机路径、凭据或命令输出的用户可见摘要。
	Message string `json:"message"`
}

// Snapshot 是管理界面消费的完整更新状态。
type Snapshot struct {
	// Current 是当前运行版本。
	Current CurrentVersion `json:"current"`
	// Latest 是最近一次检查得到的正式版本；尚未检查时字段为空。
	Latest LatestVersion `json:"latest"`
	// Available 表示最新正式版本高于当前稳定版本。
	Available bool `json:"available"`
	// CanUpdate 表示宿主机策略允许立即执行一键更新。
	CanUpdate bool `json:"can_update"`
	// BlockedReason 是 custom_image、dev_image、migration_incompatible 等稳定原因码。
	BlockedReason string `json:"blocked_reason"`
	// Operation 是最近一次宿主机任务状态。
	Operation Operation `json:"operation"`
}

// Gateway 定义应用服务对受限宿主机更新执行器消费的最小能力。
type Gateway interface {
	// Status 读取执行器缓存的当前状态，不主动访问发布服务。
	Status(context.Context, CurrentVersion) (Snapshot, error)
	// Check 主动检查最新正式版本，但不修改容器或数据库。
	Check(context.Context, CurrentVersion) (Snapshot, error)
	// ApplyLatestStable 请求执行器更新到其自行验证的最新正式版。
	ApplyLatestStable(context.Context, CurrentVersion) (Snapshot, error)
}

// Service 负责补齐当前构建信息并将固定更新命令委托给宿主机执行器。
type Service struct {
	// gateway 是可选的宿主机受限执行器；nil 表示当前部署只支持查看版本。
	gateway Gateway
	// current 保存进程启动时冻结的非敏感构建元数据。
	current CurrentVersion
}

// NewService 构造系统更新用例；gateway 可以为空以支持桌面版和普通源码部署。
func NewService(gateway Gateway, current CurrentVersion) *Service {
	current.Version = fallbackText(current.Version, "dev")
	current.Commit = fallbackText(current.Commit, "unknown")
	current.BuildTime = fallbackText(current.BuildTime, "unknown")
	current.DeploymentKind = fallbackText(current.DeploymentKind, DeploymentUnknown)
	return &Service{gateway: gateway, current: current}
}

// Status 返回当前更新状态；执行器缺失时仍返回可展示的阻断快照。
func (service *Service) Status(ctx context.Context) (Snapshot, error) {
	if service == nil || service.gateway == nil {
		return service.unavailableSnapshot(), nil
	}
	// snapshot、statusErr 分别是执行器状态及其连接或协议错误。
	snapshot, statusErr := service.gateway.Status(ctx, service.current)
	if statusErr != nil {
		return service.unavailableSnapshot(), nil
	}
	return service.normalize(snapshot), nil
}

// Check 请求执行器重新检查正式版本；执行器缺失时返回只读阻断快照。
func (service *Service) Check(ctx context.Context) (Snapshot, error) {
	if service == nil || service.gateway == nil {
		return service.unavailableSnapshot(), nil
	}
	// snapshot、checkErr 分别是检查结果及其失败原因。
	snapshot, checkErr := service.gateway.Check(ctx, service.current)
	if checkErr != nil {
		return Snapshot{}, checkErr
	}
	return service.normalize(snapshot), nil
}

// ApplyLatestStable 只发送固定更新命令，不接受镜像、版本、路径或 Shell 参数。
func (service *Service) ApplyLatestStable(ctx context.Context) (Snapshot, error) {
	if service == nil || service.gateway == nil {
		return Snapshot{}, ErrUpdaterUnavailable
	}
	// snapshot、applyErr 分别是任务受理后的状态及执行器拒绝原因。
	snapshot, applyErr := service.gateway.ApplyLatestStable(ctx, service.current)
	if applyErr != nil {
		return Snapshot{}, applyErr
	}
	return service.normalize(snapshot), nil
}

// unavailableSnapshot 构造不泄露本机路径的执行器缺失状态。
func (service *Service) unavailableSnapshot() Snapshot {
	// current 是 nil receiver 场景也可安全展示的默认构建信息。
	current := CurrentVersion{Version: "dev", Commit: "unknown", BuildTime: "unknown", DeploymentKind: DeploymentUnknown}
	if service != nil {
		current = service.current
	}
	return Snapshot{
		Current:       current,
		CanUpdate:     false,
		BlockedReason: BlockedUpdaterUnavailable,
		Operation:     Operation{Status: "idle", Message: "宿主机更新组件未安装或未连接"},
	}
}

// normalize 强制构建身份来自应用进程，同时保留宿主机根据 Compose 与 Docker 判定的部署来源。
func (service *Service) normalize(snapshot Snapshot) Snapshot {
	// deploymentKind 是执行器从实际镜像来源得出的部署类型，空值时才退回进程启动配置。
	deploymentKind := fallbackText(snapshot.Current.DeploymentKind, service.current.DeploymentKind)
	snapshot.Current = service.current
	snapshot.Current.DeploymentKind = deploymentKind
	snapshot.Operation.Status = fallbackText(snapshot.Operation.Status, "idle")
	return snapshot
}

// fallbackText 把空白文本替换为稳定默认值，避免前端重复猜测缺省语义。
func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return strings.TrimSpace(value)
}
